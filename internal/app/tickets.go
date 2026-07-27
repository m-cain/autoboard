package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type UpdateTicketInput struct {
	Title       *string
	Description *string
	Priority    *Priority
	Assignee    *Assignee
	Labels      *[]string
}

func (s *Service) GetTicket(ctx context.Context, ticketID string) (Ticket, error) {
	return loadTicket(ctx, s.db, ticketID)
}

func (s *Service) UpdateTicket(
	ctx context.Context,
	ticketID string,
	expectedRevision int,
	input UpdateTicketInput,
) (Ticket, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, fmt.Errorf("begin ticket update: %w", err)
	}
	defer tx.Rollback()
	ticket, err := loadTicket(ctx, tx, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if err := requireTicketRevision(ticket, expectedRevision); err != nil {
		return Ticket{}, err
	}
	if err := requireActiveTicketProject(ctx, tx, ticket.ProjectID); err != nil {
		return Ticket{}, err
	}
	if input.Title == nil &&
		input.Description == nil &&
		input.Priority == nil &&
		input.Assignee == nil &&
		input.Labels == nil {
		return Ticket{}, validationError("base", "must change at least one field")
	}

	changes := map[string]any{}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return Ticket{}, validationError("title", "must not be blank")
		}
		if utf8.RuneCountInString(title) > 500 {
			return Ticket{}, validationError(
				"title",
				"must be at most 500 characters",
			)
		}
		if title != ticket.Title {
			changes["title"] = map[string]any{"from": ticket.Title, "to": title}
			ticket.Title = title
		}
	}
	if input.Description != nil && *input.Description != ticket.Description {
		if utf8.RuneCountInString(*input.Description) > 100_000 {
			return Ticket{}, validationError(
				"description",
				"must be at most 100000 characters",
			)
		}
		changes["description"] = map[string]any{
			"from": ticket.Description,
			"to":   *input.Description,
		}
		ticket.Description = *input.Description
	}
	if input.Priority != nil {
		if !validPriority(*input.Priority) {
			return Ticket{}, validationError("priority", "is invalid")
		}
		if *input.Priority != ticket.Priority {
			changes["priority"] = map[string]any{
				"from": ticket.Priority,
				"to":   *input.Priority,
			}
			ticket.Priority = *input.Priority
		}
	}
	if input.Assignee != nil {
		if !validAssignee(*input.Assignee) {
			return Ticket{}, validationError("assignee", "is invalid")
		}
		if *input.Assignee != ticket.Assignee {
			changes["assignee"] = map[string]any{
				"from": ticket.Assignee,
				"to":   *input.Assignee,
			}
			ticket.Assignee = *input.Assignee
		}
	}
	var names []string
	labelsChanged := false
	if input.Labels != nil {
		if err := validateLabelValues(*input.Labels); err != nil {
			return Ticket{}, err
		}
		names = normalizeLabelNames(*input.Labels)
		labelsChanged = !equalLabelNames(labelNameValues(ticket.Labels), names)
	}
	if len(changes) == 0 && !labelsChanged {
		return Ticket{}, validationError("base", "must change at least one field")
	}

	now := time.Now().UTC()
	if labelsChanged {
		oldLabels := labelNameValues(ticket.Labels)
		ticket.Labels, err = replaceTicketLabels(
			ctx,
			tx,
			ticket.ProjectID,
			ticket.ID,
			names,
			now,
		)
		if err != nil {
			return Ticket{}, err
		}
		changes["labels"] = map[string]any{
			"from": oldLabels,
			"to":   labelNameValues(ticket.Labels),
		}
	}
	ticket.Revision++
	ticket.UpdatedAt = now
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tickets
		 SET title = ?, description = ?, priority = ?, assignee = ?,
		     revision = ?, updated_at = ?
		 WHERE id = ?`,
		ticket.Title,
		ticket.Description,
		ticket.Priority,
		ticket.Assignee,
		ticket.Revision,
		formatTime(now),
		ticket.ID,
	); err != nil {
		return Ticket{}, fmt.Errorf("update ticket revision: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		"ticket.updated",
		ticket.ProjectID,
		&ticket.ID,
		changes,
		now,
	); err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit ticket update: %w", err)
	}
	return ticket, nil
}

func (s *Service) TransitionTicket(
	ctx context.Context,
	ticketID string,
	expectedRevision int,
	status TicketStatus,
) (Ticket, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if !validTicketStatus(status) {
		return Ticket{}, validationError("status", "is invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, fmt.Errorf("begin ticket transition: %w", err)
	}
	defer tx.Rollback()
	ticket, err := loadTicket(ctx, tx, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if err := requireTicketRevision(ticket, expectedRevision); err != nil {
		return Ticket{}, err
	}
	if err := requireActiveTicketProject(ctx, tx, ticket.ProjectID); err != nil {
		return Ticket{}, err
	}
	if ticket.Status == status {
		return Ticket{}, validationError("status", "must change the status")
	}
	if terminalStatus(status) {
		if status == TicketDone && ticket.Blocked {
			return Ticket{}, domainError(
				ErrorBlockedByDependency,
				"ticket has unresolved blockers",
			)
		}
		hasNonterminalChild, err := ticketHasNonterminalChild(ctx, tx, ticket.ID)
		if err != nil {
			return Ticket{}, err
		}
		if hasNonterminalChild {
			return Ticket{}, domainError(
				ErrorInvalidTransition,
				"ticket has non-terminal subtasks",
			)
		}
	}

	oldStatus := ticket.Status
	now := time.Now().UTC()
	ticket.Status = status
	ticket.Revision++
	ticket.UpdatedAt = now
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tickets SET status = ?, revision = ?, updated_at = ? WHERE id = ?`,
		ticket.Status,
		ticket.Revision,
		formatTime(now),
		ticket.ID,
	); err != nil {
		return Ticket{}, fmt.Errorf("transition ticket: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		"ticket.transitioned",
		ticket.ProjectID,
		&ticket.ID,
		map[string]any{
			"status": map[string]any{"from": oldStatus, "to": status},
		},
		now,
	); err != nil {
		return Ticket{}, err
	}
	if terminalStatus(oldStatus) != terminalStatus(status) {
		if err := notifyDirectlyBlockedTickets(
			ctx,
			tx,
			ticket,
			oldStatus,
			status,
			now,
		); err != nil {
			return Ticket{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit ticket transition: %w", err)
	}
	return ticket, nil
}

func (s *Service) AddDependency(
	ctx context.Context,
	blockedTicketID string,
	blockerTicketID string,
	expectedRevision int,
) (Ticket, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, fmt.Errorf("begin dependency add: %w", err)
	}
	defer tx.Rollback()
	blocked, err := loadTicket(ctx, tx, blockedTicketID)
	if err != nil {
		return Ticket{}, err
	}
	if err := requireTicketRevision(blocked, expectedRevision); err != nil {
		return Ticket{}, err
	}
	if err := requireActiveTicketProject(ctx, tx, blocked.ProjectID); err != nil {
		return Ticket{}, err
	}
	blocker, err := loadTicket(ctx, tx, blockerTicketID)
	if err != nil {
		return Ticket{}, validationError(
			"blocker_ticket_id",
			"must reference an existing ticket",
		)
	}
	if blocked.ID == blocker.ID {
		return Ticket{}, validationError(
			"blocker_ticket_id",
			"must not equal blocked_ticket_id",
		)
	}
	if blocked.ProjectID != blocker.ProjectID {
		return Ticket{}, validationError(
			"blocker_ticket_id",
			"must belong to the same project",
		)
	}
	exists, err := dependencyExists(ctx, tx, blocked.ID, blocker.ID)
	if err != nil {
		return Ticket{}, err
	}
	if exists {
		return Ticket{}, validationError(
			"blocker_ticket_id",
			"already blocks this ticket",
		)
	}
	reachable, err := dependencyReachable(
		ctx,
		tx,
		blocked.ProjectID,
		blocked.ID,
		blocker.ID,
	)
	if err != nil {
		return Ticket{}, err
	}
	if reachable {
		err := domainError(ErrorDependencyCycle, "dependency would create a cycle")
		err.Fields = map[string][]string{
			"blocker_ticket_id": {"must not create a cycle"},
		}
		return Ticket{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO ticket_dependencies
		 (blocker_ticket_id, blocked_ticket_id, inserted_at)
		 VALUES (?, ?, ?)`,
		blocker.ID,
		blocked.ID,
		formatTime(now),
	); err != nil {
		return Ticket{}, fmt.Errorf("insert dependency: %w", err)
	}
	blocked.Revision++
	blocked.UpdatedAt = now
	blocked.Blocked = blocked.Blocked || !terminalStatus(blocker.Status)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tickets SET revision = ?, updated_at = ? WHERE id = ?`,
		blocked.Revision,
		formatTime(now),
		blocked.ID,
	); err != nil {
		return Ticket{}, fmt.Errorf("advance blocked ticket revision: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		"dependency.added",
		blocked.ProjectID,
		&blocked.ID,
		map[string]any{"blocker_ticket_id": blocker.ID},
		now,
	); err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit dependency add: %w", err)
	}
	return blocked, nil
}

func (s *Service) RemoveDependency(
	ctx context.Context,
	blockedTicketID string,
	blockerTicketID string,
	expectedRevision int,
) (Ticket, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, fmt.Errorf("begin dependency removal: %w", err)
	}
	defer tx.Rollback()
	blocked, err := loadTicket(ctx, tx, blockedTicketID)
	if err != nil {
		return Ticket{}, err
	}
	if err := requireTicketRevision(blocked, expectedRevision); err != nil {
		return Ticket{}, err
	}
	if err := requireActiveTicketProject(ctx, tx, blocked.ProjectID); err != nil {
		return Ticket{}, err
	}
	blocker, err := loadTicket(ctx, tx, blockerTicketID)
	if err != nil || blocker.ProjectID != blocked.ProjectID {
		return Ticket{}, validationError(
			"blocker_ticket_id",
			"must reference an existing dependency",
		)
	}
	result, err := tx.ExecContext(
		ctx,
		`DELETE FROM ticket_dependencies
		 WHERE blocked_ticket_id = ? AND blocker_ticket_id = ?`,
		blocked.ID,
		blocker.ID,
	)
	if err != nil {
		return Ticket{}, fmt.Errorf("remove dependency: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return Ticket{}, fmt.Errorf("count removed dependencies: %w", err)
	}
	if removed == 0 {
		return Ticket{}, validationError(
			"blocker_ticket_id",
			"must reference an existing dependency",
		)
	}
	now := time.Now().UTC()
	blocked.Revision++
	blocked.UpdatedAt = now
	blocked.Blocked, err = unresolvedBlockerExists(ctx, tx, blocked.ID)
	if err != nil {
		return Ticket{}, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tickets SET revision = ?, updated_at = ? WHERE id = ?`,
		blocked.Revision,
		formatTime(now),
		blocked.ID,
	); err != nil {
		return Ticket{}, fmt.Errorf("advance dependency removal revision: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		"dependency.removed",
		blocked.ProjectID,
		&blocked.ID,
		map[string]any{"blocker_ticket_id": blocker.ID},
		now,
	); err != nil {
		return Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit dependency removal: %w", err)
	}
	return blocked, nil
}

type ticketQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadTicket(
	ctx context.Context,
	query ticketQuery,
	ticketID string,
) (Ticket, error) {
	var ticket Ticket
	var insertedAt string
	var updatedAt string
	var parentTicketID sql.NullString
	err := query.QueryRowContext(
		ctx,
		`SELECT tickets.id,
		        projects.key || '-' || tickets.number,
		        tickets.project_id,
		        tickets.title,
		        tickets.description,
		        tickets.status,
		        tickets.priority,
		        tickets.assignee,
		        tickets.revision,
		        tickets.parent_ticket_id,
		        EXISTS (
		          SELECT 1
		          FROM ticket_dependencies
		          JOIN tickets AS blocker
		            ON blocker.id = ticket_dependencies.blocker_ticket_id
		          WHERE ticket_dependencies.blocked_ticket_id = tickets.id
		            AND blocker.status NOT IN ('done', 'canceled')
		        ),
		        (SELECT count(*) FROM comments WHERE comments.ticket_id = tickets.id),
		        (SELECT count(*) FROM attachments WHERE attachments.ticket_id = tickets.id),
		        tickets.inserted_at,
		        tickets.updated_at
		 FROM tickets
		 JOIN projects ON projects.id = tickets.project_id
		 WHERE tickets.id = ?`,
		ticketID,
	).Scan(
		&ticket.ID,
		&ticket.Identifier,
		&ticket.ProjectID,
		&ticket.Title,
		&ticket.Description,
		&ticket.Status,
		&ticket.Priority,
		&ticket.Assignee,
		&ticket.Revision,
		&parentTicketID,
		&ticket.Blocked,
		&ticket.CommentCount,
		&ticket.AttachmentCount,
		&insertedAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, domainError(ErrorNotFound, "ticket not found")
	}
	if err != nil {
		return Ticket{}, fmt.Errorf("load ticket: %w", err)
	}
	if parentTicketID.Valid {
		ticket.ParentTicketID = &parentTicketID.String
	}
	ticket.InsertedAt, err = time.Parse(time.RFC3339Nano, insertedAt)
	if err != nil {
		return Ticket{}, fmt.Errorf("parse ticket inserted timestamp: %w", err)
	}
	ticket.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Ticket{}, fmt.Errorf("parse ticket updated timestamp: %w", err)
	}
	ticket.Labels, err = loadTicketLabels(ctx, query, ticket.ID)
	if err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

func loadTicketLabels(
	ctx context.Context,
	query ticketQuery,
	ticketID string,
) ([]Label, error) {
	rows, err := query.QueryContext(
		ctx,
		`SELECT labels.id, labels.name, labels.project_id
		 FROM labels
		 JOIN ticket_labels ON ticket_labels.label_id = labels.id
		 WHERE ticket_labels.ticket_id = ?
		 ORDER BY labels.name COLLATE NOCASE, labels.id`,
		ticketID,
	)
	if err != nil {
		return nil, fmt.Errorf("query ticket labels: %w", err)
	}
	defer rows.Close()
	labels := []Label{}
	for rows.Next() {
		var label Label
		if err := rows.Scan(&label.ID, &label.Name, &label.ProjectID); err != nil {
			return nil, fmt.Errorf("scan ticket label: %w", err)
		}
		labels = append(labels, label)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket labels: %w", err)
	}
	return labels, nil
}

func replaceTicketLabels(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
	ticketID string,
	values []string,
	now time.Time,
) ([]Label, error) {
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM ticket_labels WHERE ticket_id = ?`,
		ticketID,
	); err != nil {
		return nil, fmt.Errorf("clear ticket labels: %w", err)
	}
	names := normalizeLabelNames(values)
	labels := make([]Label, 0, len(names))
	for _, name := range names {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO labels (id, project_id, name, inserted_at, updated_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(project_id, name) DO NOTHING`,
			uuid.NewString(),
			projectID,
			name,
			formatTime(now),
			formatTime(now),
		); err != nil {
			return nil, fmt.Errorf("resolve label %q: %w", name, err)
		}
		var label Label
		if err := tx.QueryRowContext(
			ctx,
			`SELECT id, name, project_id
			 FROM labels WHERE project_id = ? AND name = ?`,
			projectID,
			name,
		).Scan(&label.ID, &label.Name, &label.ProjectID); err != nil {
			return nil, fmt.Errorf("load label %q: %w", name, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO ticket_labels (ticket_id, label_id) VALUES (?, ?)`,
			ticketID,
			label.ID,
		); err != nil {
			return nil, fmt.Errorf("attach label %q: %w", name, err)
		}
		labels = append(labels, label)
	}
	return labels, nil
}

func normalizeLabelNames(values []string) []string {
	byFoldedName := map[string]string{}
	for _, value := range values {
		name := strings.Join(strings.Fields(value), " ")
		if name == "" {
			continue
		}
		folded := strings.ToLower(name)
		if _, exists := byFoldedName[folded]; !exists {
			byFoldedName[folded] = name
		}
	}
	names := make([]string, 0, len(byFoldedName))
	for _, name := range byFoldedName {
		names = append(names, name)
	}
	slices.SortFunc(names, func(left string, right string) int {
		return strings.Compare(strings.ToLower(left), strings.ToLower(right))
	})
	return names
}

func validateLabelValues(values []string) error {
	if len(values) > 100 {
		return validationError("labels", "must contain at most 100 labels")
	}
	for _, value := range values {
		name := strings.Join(strings.Fields(value), " ")
		if name != "" && utf8.RuneCountInString(name) > 100 {
			return validationError(
				"labels",
				"each label must be at most 100 characters",
			)
		}
	}
	return nil
}

func equalLabelNames(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !strings.EqualFold(left[index], right[index]) {
			return false
		}
	}
	return true
}

func labelNameValues(labels []Label) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, label.Name)
	}
	return names
}

func validateParent(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
	parentTicketID *string,
) error {
	if parentTicketID == nil {
		return nil
	}
	parent, err := loadTicket(ctx, tx, *parentTicketID)
	if err != nil {
		return validationError(
			"parent_ticket_id",
			"must reference an existing ticket",
		)
	}
	if parent.ProjectID != projectID {
		return validationError("parent_ticket_id", "must belong to the same project")
	}
	if parent.ParentTicketID != nil {
		return validationError("parent_ticket_id", "must not create a grandchild")
	}
	return nil
}

func requireTicketRevision(ticket Ticket, expectedRevision int) error {
	if ticket.Revision == expectedRevision {
		return nil
	}
	current := ticket
	err := domainError(ErrorRevisionConflict, "ticket revision does not match")
	err.CurrentTicket = &current
	return err
}

func requireActiveTicketProject(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
) error {
	project, err := loadProject(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if project.State == ProjectArchived {
		return domainError(ErrorInvalidTransition, "archived projects are read-only")
	}
	return nil
}

func validationError(field string, message string) *Error {
	return &Error{
		Kind:    ErrorValidationFailed,
		Message: "ticket validation failed",
		Fields:  map[string][]string{field: {message}},
	}
}

func validTicketStatus(status TicketStatus) bool {
	return status == TicketTriage ||
		status == TicketBacklog ||
		status == TicketReady ||
		status == TicketInProgress ||
		status == TicketDone ||
		status == TicketCanceled
}

func validPriority(priority Priority) bool {
	return priority == PriorityNone ||
		priority == PriorityLow ||
		priority == PriorityMedium ||
		priority == PriorityHigh ||
		priority == PriorityUrgent
}

func validAssignee(assignee Assignee) bool {
	return assignee == AssigneeUnassigned ||
		assignee == AssigneeMe ||
		assignee == AssigneeCodex
}

func terminalStatus(status TicketStatus) bool {
	return status == TicketDone || status == TicketCanceled
}

func ticketHasNonterminalChild(
	ctx context.Context,
	tx *sql.Tx,
	ticketID string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM tickets
		   WHERE parent_ticket_id = ?
		     AND status NOT IN ('done', 'canceled')
		 )`,
		ticketID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check ticket subtasks: %w", err)
	}
	return exists, nil
}

func dependencyExists(
	ctx context.Context,
	tx *sql.Tx,
	blockedTicketID string,
	blockerTicketID string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM ticket_dependencies
		   WHERE blocked_ticket_id = ? AND blocker_ticket_id = ?
		 )`,
		blockedTicketID,
		blockerTicketID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check dependency: %w", err)
	}
	return exists, nil
}

func unresolvedBlockerExists(
	ctx context.Context,
	tx *sql.Tx,
	ticketID string,
) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS (
		   SELECT 1
		   FROM ticket_dependencies
		   JOIN tickets AS blocker
		     ON blocker.id = ticket_dependencies.blocker_ticket_id
		   WHERE ticket_dependencies.blocked_ticket_id = ?
		     AND blocker.status NOT IN ('done', 'canceled')
		 )`,
		ticketID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check unresolved blockers: %w", err)
	}
	return exists, nil
}

func dependencyReachable(
	ctx context.Context,
	tx *sql.Tx,
	projectID string,
	fromTicketID string,
	toTicketID string,
) (bool, error) {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT dependency.blocker_ticket_id, dependency.blocked_ticket_id
		 FROM ticket_dependencies AS dependency
		 JOIN tickets AS blocked ON blocked.id = dependency.blocked_ticket_id
		 WHERE blocked.project_id = ?`,
		projectID,
	)
	if err != nil {
		return false, fmt.Errorf("query dependency graph: %w", err)
	}
	defer rows.Close()
	edges := map[string][]string{}
	for rows.Next() {
		var from string
		var to string
		if err := rows.Scan(&from, &to); err != nil {
			return false, fmt.Errorf("scan dependency graph: %w", err)
		}
		edges[from] = append(edges[from], to)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate dependency graph: %w", err)
	}
	pending := []string{fromTicketID}
	visited := map[string]bool{}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		if current == toTicketID {
			return true, nil
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		pending = append(pending, edges[current]...)
	}
	return false, nil
}

func notifyDirectlyBlockedTickets(
	ctx context.Context,
	tx *sql.Tx,
	blocker Ticket,
	from TicketStatus,
	to TicketStatus,
	now time.Time,
) error {
	rows, err := tx.QueryContext(
		ctx,
		`SELECT DISTINCT blocked_ticket_id
		 FROM ticket_dependencies
		 WHERE blocker_ticket_id = ?`,
		blocker.ID,
	)
	if err != nil {
		return fmt.Errorf("query directly blocked tickets: %w", err)
	}
	defer rows.Close()
	var blockedTicketIDs []string
	for rows.Next() {
		var ticketID string
		if err := rows.Scan(&ticketID); err != nil {
			return fmt.Errorf("scan directly blocked ticket: %w", err)
		}
		blockedTicketIDs = append(blockedTicketIDs, ticketID)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close directly blocked tickets: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate directly blocked tickets: %w", err)
	}
	for _, ticketID := range blockedTicketIDs {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE tickets SET revision = revision + 1, updated_at = ? WHERE id = ?`,
			formatTime(now),
			ticketID,
		); err != nil {
			return fmt.Errorf("advance blocked ticket revision: %w", err)
		}
		if err := insertActivity(
			ctx,
			tx,
			"dependency.blocking_changed",
			blocker.ProjectID,
			&ticketID,
			map[string]any{
				"blocker_ticket_id": blocker.ID,
				"status":            map[string]any{"from": from, "to": to},
			},
			now,
		); err != nil {
			return err
		}
	}
	return nil
}
