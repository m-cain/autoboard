package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ticketIdentifierPattern = regexp.MustCompile(
	`^([A-Za-z][A-Za-z0-9]{1,7})-([1-9][0-9]{0,9})$`,
)

type ProjectList struct {
	Active   []Project `json:"active"`
	Archived []Project `json:"archived"`
}

type BoardColumns struct {
	Backlog    []Ticket `json:"backlog"`
	Ready      []Ticket `json:"ready"`
	InProgress []Ticket `json:"in_progress"`
	Done       []Ticket `json:"done"`
}

type ProjectBoard struct {
	Project Project      `json:"project"`
	Columns BoardColumns `json:"columns"`
}

type TicketDetail struct {
	Ticket
	Project        Project         `json:"project"`
	Parent         *Ticket         `json:"parent" jsonschema:"nullable"`
	Subtasks       []Ticket        `json:"subtasks"`
	Blockers       []Ticket        `json:"blockers"`
	BlockedTickets []Ticket        `json:"blocked_tickets"`
	Comments       []Comment       `json:"comments"`
	Attachments    []Attachment    `json:"attachments"`
	Activity       []ActivityEvent `json:"activity"`
}

func (s *Service) ListProjects(ctx context.Context) (ProjectList, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, key, name, description, state, revision, created_performed_by, created_initiated_by, inserted_at, updated_at
		 FROM projects
		 ORDER BY lower(name), id`,
	)
	if err != nil {
		return ProjectList{}, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()
	result := ProjectList{
		Active:   []Project{},
		Archived: []Project{},
	}
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return ProjectList{}, err
		}
		if project.State == ProjectArchived {
			result.Archived = append(result.Archived, project)
		} else {
			result.Active = append(result.Active, project)
		}
	}
	if err := rows.Err(); err != nil {
		return ProjectList{}, fmt.Errorf("iterate projects: %w", err)
	}
	return result, nil
}

func (s *Service) GetProject(
	ctx context.Context,
	projectRef string,
) (Project, error) {
	if id, err := uuid.Parse(projectRef); err == nil {
		return loadProject(ctx, s.db, id.String())
	}
	var projectID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id FROM projects WHERE key = ?`,
		strings.ToUpper(projectRef),
	).Scan(&projectID)
	if err == sql.ErrNoRows {
		return Project{}, domainError(ErrorNotFound, "project not found")
	}
	if err != nil {
		return Project{}, fmt.Errorf("resolve project: %w", err)
	}
	return loadProject(ctx, s.db, projectID)
}

func (s *Service) ListTriageTickets(ctx context.Context) ([]Ticket, error) {
	return s.loadTicketsByQuery(
		ctx,
		`SELECT tickets.id
		 FROM tickets
		 JOIN projects ON projects.id = tickets.project_id
		 WHERE tickets.status = 'triage' AND projects.state = 'active'
		 ORDER BY tickets.inserted_at, tickets.id`,
	)
}

func (s *Service) GetProjectBoard(
	ctx context.Context,
	projectRef string,
) (ProjectBoard, error) {
	project, err := s.GetProject(ctx, projectRef)
	if err != nil {
		return ProjectBoard{}, err
	}
	tickets, err := s.loadTicketsByQuery(
		ctx,
		`SELECT id FROM tickets
		 WHERE project_id = ?
		   AND status IN ('backlog', 'ready', 'in_progress', 'done')
		 ORDER BY inserted_at, id`,
		project.ID,
	)
	if err != nil {
		return ProjectBoard{}, err
	}
	board := ProjectBoard{
		Project: project,
		Columns: BoardColumns{
			Backlog:    []Ticket{},
			Ready:      []Ticket{},
			InProgress: []Ticket{},
			Done:       []Ticket{},
		},
	}
	for _, ticket := range tickets {
		switch ticket.Status {
		case TicketBacklog:
			board.Columns.Backlog = append(board.Columns.Backlog, ticket)
		case TicketReady:
			board.Columns.Ready = append(board.Columns.Ready, ticket)
		case TicketInProgress:
			board.Columns.InProgress = append(
				board.Columns.InProgress,
				ticket,
			)
		case TicketDone:
			board.Columns.Done = append(board.Columns.Done, ticket)
		case TicketTriage, TicketCanceled:
			continue
		}
	}
	return board, nil
}

func (s *Service) ListCanceledTickets(
	ctx context.Context,
	projectRef string,
) ([]Ticket, error) {
	project, err := s.GetProject(ctx, projectRef)
	if err != nil {
		return nil, err
	}
	return s.loadTicketsByQuery(
		ctx,
		`SELECT id FROM tickets
		 WHERE project_id = ? AND status = 'canceled'
		 ORDER BY inserted_at, id`,
		project.ID,
	)
}

func (s *Service) SearchTickets(
	ctx context.Context,
	search string,
	projectID string,
	limit int,
) ([]Ticket, error) {
	if limit < 1 || limit > 100 {
		return nil, validationError("limit", "must be an integer from 1 to 100")
	}
	pattern := "%" + escapeLike(search) + "%"
	if projectID == "" {
		return s.loadTicketsByQuery(
			ctx,
			`SELECT id FROM tickets
			 WHERE lower(title) LIKE lower(?) ESCAPE '\'
			    OR lower(description) LIKE lower(?) ESCAPE '\'
			 ORDER BY inserted_at, id
			 LIMIT ?`,
			pattern,
			pattern,
			limit,
		)
	}
	return s.loadTicketsByQuery(
		ctx,
		`SELECT id FROM tickets
		 WHERE project_id = ?
		   AND (
		     lower(title) LIKE lower(?) ESCAPE '\'
		     OR lower(description) LIKE lower(?) ESCAPE '\'
		   )
		 ORDER BY inserted_at, id
		 LIMIT ?`,
		projectID,
		pattern,
		pattern,
		limit,
	)
}

func (s *Service) ListActionableTickets(
	ctx context.Context,
	projectID string,
	limit int,
) ([]Ticket, error) {
	if limit < 1 || limit > 100 {
		return nil, validationError("limit", "must be an integer from 1 to 100")
	}
	query := `SELECT tickets.id
		FROM tickets
		JOIN projects ON projects.id = tickets.project_id
		WHERE tickets.status = 'ready'
		  AND tickets.assignee = 'codex'
		  AND projects.state = 'active'
		  AND NOT EXISTS (
		    SELECT 1
		    FROM ticket_dependencies
		    JOIN tickets AS blocker
		      ON blocker.id = ticket_dependencies.blocker_ticket_id
		    WHERE ticket_dependencies.blocked_ticket_id = tickets.id
		      AND blocker.status NOT IN ('done', 'canceled')
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM tickets AS child
		    WHERE child.parent_ticket_id = tickets.id
		      AND child.status NOT IN ('done', 'canceled')
		  )`
	args := []any{}
	if projectID != "" {
		query += ` AND tickets.project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY
		CASE tickets.priority
		  WHEN 'urgent' THEN 0
		  WHEN 'high' THEN 1
		  WHEN 'medium' THEN 2
		  WHEN 'low' THEN 3
		  ELSE 4
		END,
		tickets.inserted_at,
		tickets.id
		LIMIT ?`
	args = append(args, limit)
	return s.loadTicketsByQuery(ctx, query, args...)
}

func (s *Service) GetTicketDetail(
	ctx context.Context,
	ticketRef string,
) (TicketDetail, error) {
	ticketID, err := s.resolveTicketID(ctx, ticketRef)
	if err != nil {
		return TicketDetail{}, err
	}
	ticket, err := loadTicket(ctx, s.db, ticketID)
	if err != nil {
		return TicketDetail{}, err
	}
	project, err := loadProject(ctx, s.db, ticket.ProjectID)
	if err != nil {
		return TicketDetail{}, err
	}
	detail := TicketDetail{
		Ticket:         ticket,
		Project:        project,
		Subtasks:       []Ticket{},
		Blockers:       []Ticket{},
		BlockedTickets: []Ticket{},
		Comments:       []Comment{},
		Attachments:    []Attachment{},
		Activity:       []ActivityEvent{},
	}
	if ticket.ParentTicketID != nil {
		parent, err := loadTicket(ctx, s.db, *ticket.ParentTicketID)
		if err != nil {
			return TicketDetail{}, err
		}
		detail.Parent = &parent
	}
	detail.Subtasks, err = s.loadTicketsByQuery(
		ctx,
		`SELECT id FROM tickets
		 WHERE parent_ticket_id = ?
		 ORDER BY inserted_at, id`,
		ticket.ID,
	)
	if err != nil {
		return TicketDetail{}, err
	}
	detail.Blockers, err = s.loadTicketsByQuery(
		ctx,
		`SELECT blocker.id
		 FROM tickets AS blocker
		 JOIN ticket_dependencies AS dependency
		   ON dependency.blocker_ticket_id = blocker.id
		 WHERE dependency.blocked_ticket_id = ?
		 ORDER BY blocker.inserted_at, blocker.id`,
		ticket.ID,
	)
	if err != nil {
		return TicketDetail{}, err
	}
	detail.BlockedTickets, err = s.loadTicketsByQuery(
		ctx,
		`SELECT blocked.id
		 FROM tickets AS blocked
		 JOIN ticket_dependencies AS dependency
		   ON dependency.blocked_ticket_id = blocked.id
		 WHERE dependency.blocker_ticket_id = ?
		 ORDER BY blocked.inserted_at, blocked.id`,
		ticket.ID,
	)
	if err != nil {
		return TicketDetail{}, err
	}
	detail.Comments, err = s.listTicketComments(ctx, ticket.ID)
	if err != nil {
		return TicketDetail{}, err
	}
	detail.Attachments, err = s.listTicketAttachments(ctx, ticket.ID)
	if err != nil {
		return TicketDetail{}, err
	}
	detail.Activity, err = s.listTicketActivity(ctx, ticket.ID, 100)
	if err != nil {
		return TicketDetail{}, err
	}
	return detail, nil
}

func (s *Service) ResolveTicketID(
	ctx context.Context,
	ticketRef string,
) (string, error) {
	return s.resolveTicketID(ctx, ticketRef)
}

func (s *Service) resolveTicketID(
	ctx context.Context,
	ticketRef string,
) (string, error) {
	if id, err := uuid.Parse(ticketRef); err == nil {
		var ticketID string
		err := s.db.QueryRowContext(
			ctx,
			`SELECT id FROM tickets WHERE id = ?`,
			id.String(),
		).Scan(&ticketID)
		if err == nil {
			return ticketID, nil
		}
		if err != sql.ErrNoRows {
			return "", fmt.Errorf("resolve ticket: %w", err)
		}
		return "", domainError(ErrorNotFound, "ticket not found")
	}
	matches := ticketIdentifierPattern.FindStringSubmatch(ticketRef)
	if len(matches) != 3 {
		return "", domainError(ErrorNotFound, "ticket not found")
	}
	number, err := strconv.ParseInt(matches[2], 10, 32)
	if err != nil {
		return "", domainError(ErrorNotFound, "ticket not found")
	}
	var ticketID string
	err = s.db.QueryRowContext(
		ctx,
		`SELECT tickets.id
		 FROM tickets
		 JOIN projects ON projects.id = tickets.project_id
		 WHERE projects.key = ? AND tickets.number = ?`,
		strings.ToUpper(matches[1]),
		number,
	).Scan(&ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domainError(ErrorNotFound, "ticket not found")
	}
	if err != nil {
		return "", fmt.Errorf("resolve ticket identifier: %w", err)
	}
	return ticketID, nil
}

func (s *Service) loadTicketsByQuery(
	ctx context.Context,
	query string,
	args ...any,
) ([]Ticket, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ticket IDs: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan ticket ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket IDs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close ticket IDs: %w", err)
	}
	tickets := make([]Ticket, 0, len(ids))
	for _, id := range ids {
		ticket, err := loadTicket(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, nil
}

func (s *Service) listTicketComments(
	ctx context.Context,
	ticketID string,
) ([]Comment, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, ticket_id, project_id, body, performed_by, initiated_by, inserted_at
		 FROM comments
		 WHERE ticket_id = ?
		 ORDER BY inserted_at, id`,
		ticketID,
	)
	if err != nil {
		return nil, fmt.Errorf("query ticket comments: %w", err)
	}
	defer rows.Close()
	comments := []Comment{}
	for rows.Next() {
		var comment Comment
		var insertedAt string
		if err := rows.Scan(
			&comment.ID,
			&comment.TicketID,
			&comment.ProjectID,
			&comment.Body,
			&comment.Attribution.PerformedBy,
			&comment.Attribution.InitiatedBy,
			&insertedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ticket comment: %w", err)
		}
		comment.InsertedAt, err = time.Parse(time.RFC3339Nano, insertedAt)
		if err != nil {
			return nil, fmt.Errorf("parse comment timestamp: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket comments: %w", err)
	}
	return comments, nil
}

func (s *Service) listTicketAttachments(
	ctx context.Context,
	ticketID string,
) ([]Attachment, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id
		 FROM attachments
		 WHERE ticket_id = ?
		 ORDER BY inserted_at, id`,
		ticketID,
	)
	if err != nil {
		return nil, fmt.Errorf("query ticket attachments: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan attachment ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment IDs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close attachment IDs: %w", err)
	}
	attachments := make([]Attachment, 0, len(ids))
	for _, id := range ids {
		attachment, err := loadAttachment(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func (s *Service) listTicketActivity(
	ctx context.Context,
	ticketID string,
	limit int,
) ([]ActivityEvent, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, event_type, performed_by, initiated_by, project_id, ticket_id, payload, inserted_at
		 FROM activity_events
		 WHERE ticket_id = ?
		 ORDER BY id DESC
		 LIMIT ?`,
		ticketID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query ticket activity: %w", err)
	}
	defer rows.Close()
	events := []ActivityEvent{}
	for rows.Next() {
		event, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket activity: %w", err)
	}
	return events, nil
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	var insertedAt string
	var updatedAt string
	if err := row.Scan(
		&project.ID,
		&project.Key,
		&project.Name,
		&project.Description,
		&project.State,
		&project.Revision,
		&project.CreatedAttribution.PerformedBy,
		&project.CreatedAttribution.InitiatedBy,
		&insertedAt,
		&updatedAt,
	); err != nil {
		return Project{}, fmt.Errorf("scan project: %w", err)
	}
	var err error
	project.InsertedAt, err = time.Parse(time.RFC3339Nano, insertedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse project inserted timestamp: %w", err)
	}
	project.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parse project updated timestamp: %w", err)
	}
	return project, nil
}

func scanActivity(row rowScanner) (ActivityEvent, error) {
	var event ActivityEvent
	var payload string
	var insertedAt string
	if err := row.Scan(
		&event.ID,
		&event.EventType,
		&event.Attribution.PerformedBy,
		&event.Attribution.InitiatedBy,
		&event.ProjectID,
		&event.TicketID,
		&payload,
		&insertedAt,
	); err != nil {
		return ActivityEvent{}, fmt.Errorf("scan activity: %w", err)
	}
	if err := json.Unmarshal([]byte(payload), &event.Payload); err != nil {
		return ActivityEvent{}, fmt.Errorf("decode activity payload: %w", err)
	}
	var err error
	event.InsertedAt, err = time.Parse(time.RFC3339Nano, insertedAt)
	if err != nil {
		return ActivityEvent{}, fmt.Errorf("parse activity timestamp: %w", err)
	}
	return event, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
