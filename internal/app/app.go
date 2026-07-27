package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/m-cain/autoboard/internal/store"
)

var projectKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,7}$`)

type ProjectState string

const (
	ProjectActive   ProjectState = "active"
	ProjectArchived ProjectState = "archived"
)

type Actor string

const (
	ActorMe     Actor = "me"
	ActorCodex  Actor = "codex"
	ActorSystem Actor = "system"
)

type Config struct {
	DatabasePath       string
	DataDir            string
	MaxAttachmentBytes int64
}

type Project struct {
	ID          string       `json:"id" jsonschema_extras:"format=uuid"`
	Key         string       `json:"key" jsonschema_extras:"pattern=^[A-Z][A-Z0-9]*$,minLength=2,maxLength=8"`
	Name        string       `json:"name" jsonschema_extras:"minLength=1,maxLength=200"`
	Description string       `json:"description"`
	State       ProjectState `json:"state" jsonschema_extras:"enum=active,enum=archived"`
	Revision    int          `json:"revision" jsonschema_extras:"minimum=1"`
	InsertedAt  time.Time    `json:"inserted_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type ActivityEvent struct {
	ID         int64          `json:"id" jsonschema_extras:"minimum=1"`
	EventType  string         `json:"event_type" jsonschema_extras:"minLength=1"`
	Actor      Actor          `json:"actor" jsonschema_extras:"enum=me,enum=codex,enum=system"`
	ProjectID  string         `json:"project_id" jsonschema_extras:"format=uuid"`
	TicketID   *string        `json:"ticket_id" jsonschema:"nullable" jsonschema_extras:"format=uuid"`
	Payload    map[string]any `json:"payload"`
	InsertedAt time.Time      `json:"inserted_at"`
}

type CreateProjectInput struct {
	Key         string
	Name        string
	Description string
}

type TicketStatus string

const (
	TicketTriage     TicketStatus = "triage"
	TicketBacklog    TicketStatus = "backlog"
	TicketReady      TicketStatus = "ready"
	TicketInProgress TicketStatus = "in_progress"
	TicketDone       TicketStatus = "done"
	TicketCanceled   TicketStatus = "canceled"
)

type Priority string

const (
	PriorityNone   Priority = "none"
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

type Assignee string

const (
	AssigneeUnassigned Assignee = "unassigned"
	AssigneeMe         Assignee = "me"
	AssigneeCodex      Assignee = "codex"
)

type Ticket struct {
	ID              string       `json:"id" jsonschema_extras:"format=uuid"`
	Identifier      string       `json:"identifier" jsonschema_extras:"minLength=1"`
	ProjectID       string       `json:"project_id" jsonschema_extras:"format=uuid"`
	Title           string       `json:"title" jsonschema_extras:"minLength=1,maxLength=500"`
	Description     string       `json:"description"`
	Status          TicketStatus `json:"status" jsonschema_extras:"enum=triage,enum=backlog,enum=ready,enum=in_progress,enum=done,enum=canceled"`
	Priority        Priority     `json:"priority" jsonschema_extras:"enum=none,enum=low,enum=medium,enum=high,enum=urgent"`
	Assignee        Assignee     `json:"assignee" jsonschema_extras:"enum=unassigned,enum=me,enum=codex"`
	Revision        int          `json:"revision" jsonschema_extras:"minimum=1"`
	ParentTicketID  *string      `json:"parent_ticket_id" jsonschema:"nullable" jsonschema_extras:"format=uuid"`
	Labels          []Label      `json:"labels"`
	Blocked         bool         `json:"blocked"`
	CommentCount    int          `json:"comment_count" jsonschema_extras:"minimum=0"`
	AttachmentCount int          `json:"attachment_count" jsonschema_extras:"minimum=0"`
	InsertedAt      time.Time    `json:"inserted_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Label struct {
	ID        string `json:"id" jsonschema_extras:"format=uuid"`
	Name      string `json:"name" jsonschema_extras:"minLength=1,maxLength=100"`
	ProjectID string `json:"project_id" jsonschema_extras:"format=uuid"`
}

type CreateTicketInput struct {
	ProjectID      string
	Title          string
	Description    string
	Status         TicketStatus
	Priority       Priority
	Assignee       Assignee
	ParentTicketID *string
	Labels         []string
}

type Service struct {
	db                 *sql.DB
	writeMu            sync.Mutex
	dataDir            string
	maxAttachmentBytes int64
	orphanAttachments  int
}

func Open(ctx context.Context, config Config) (*Service, error) {
	if config.DatabasePath == "" {
		return nil, errors.New("database path is required")
	}
	if config.DataDir == "" {
		config.DataDir = filepath.Join(filepath.Dir(config.DatabasePath), "data")
	}
	if config.MaxAttachmentBytes == 0 {
		config.MaxAttachmentBytes = 50 * 1024 * 1024
	}
	if config.MaxAttachmentBytes < 0 {
		return nil, errors.New("maximum attachment bytes must be positive")
	}
	db, err := store.Open(ctx, config.DatabasePath)
	if err != nil {
		return nil, err
	}
	service := &Service{
		db:                 db,
		dataDir:            config.DataDir,
		maxAttachmentBytes: config.MaxAttachmentBytes,
	}
	if err := service.prepareAttachmentStorage(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) Close() error {
	return s.db.Close()
}

func (s *Service) CreateProject(
	ctx context.Context,
	input CreateProjectInput,
) (Project, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	key := strings.ToUpper(strings.TrimSpace(input.Key))
	name := strings.TrimSpace(input.Name)
	if !projectKeyPattern.MatchString(key) {
		return Project{}, validationError(
			"key",
			"must start with a letter and contain 2 to 8 letters or digits",
		)
	}
	if name == "" {
		return Project{}, validationError("name", "must not be blank")
	}
	if utf8.RuneCountInString(name) > 200 {
		return Project{}, validationError(
			"name",
			"must be at most 200 characters",
		)
	}
	if utf8.RuneCountInString(input.Description) > 100_000 {
		return Project{}, validationError(
			"description",
			"must be at most 100000 characters",
		)
	}

	now := time.Now().UTC()
	project := Project{
		ID:          uuid.NewString(),
		Key:         key,
		Name:        name,
		Description: input.Description,
		State:       ProjectActive,
		Revision:    1,
		InsertedAt:  now,
		UpdatedAt:   now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project transaction: %w", err)
	}
	defer tx.Rollback()
	var keyExists bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE key = ?)`,
		key,
	).Scan(&keyExists); err != nil {
		return Project{}, fmt.Errorf("check project key: %w", err)
	}
	if keyExists {
		return Project{}, validationError("key", "has already been taken")
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO projects
		 (id, key, name, description, state, revision, next_ticket_number, inserted_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		project.ID,
		project.Key,
		project.Name,
		project.Description,
		project.State,
		project.Revision,
		formatTime(project.InsertedAt),
		formatTime(project.UpdatedAt),
	); err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"key":  project.Key,
		"name": project.Name,
	})
	if err != nil {
		return Project{}, fmt.Errorf("encode project activity: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO activity_events
		 (event_type, actor, project_id, ticket_id, payload, inserted_at)
		 VALUES ('project.created', ?, ?, NULL, ?, ?)`,
		ActorCodex,
		project.ID,
		string(payload),
		formatTime(now),
	); err != nil {
		return Project{}, fmt.Errorf("insert project activity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project: %w", err)
	}
	return project, nil
}

func (s *Service) CreateTicket(
	ctx context.Context,
	input CreateTicketInput,
) (Ticket, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	title := strings.TrimSpace(input.Title)
	if input.ProjectID == "" {
		return Ticket{}, validationError("project_id", "is required")
	}
	if title == "" {
		return Ticket{}, validationError("title", "must not be blank")
	}
	if utf8.RuneCountInString(title) > 500 {
		return Ticket{}, validationError("title", "must be at most 500 characters")
	}
	if utf8.RuneCountInString(input.Description) > 100_000 {
		return Ticket{}, validationError(
			"description",
			"must be at most 100000 characters",
		)
	}
	if err := validateLabelValues(input.Labels); err != nil {
		return Ticket{}, err
	}
	if input.Status == "" {
		input.Status = TicketTriage
	}
	if !validTicketStatus(input.Status) {
		return Ticket{}, validationError("status", "is invalid")
	}
	if input.Priority == "" {
		input.Priority = PriorityNone
	}
	if !validPriority(input.Priority) {
		return Ticket{}, validationError("priority", "is invalid")
	}
	if input.Assignee == "" {
		input.Assignee = AssigneeUnassigned
	}
	if !validAssignee(input.Assignee) {
		return Ticket{}, validationError("assignee", "is invalid")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, fmt.Errorf("begin ticket transaction: %w", err)
	}
	defer tx.Rollback()

	var projectKey string
	var projectState ProjectState
	var number int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT key, state, next_ticket_number FROM projects WHERE id = ?`,
		input.ProjectID,
	).Scan(&projectKey, &projectState, &number); err != nil {
		if err == sql.ErrNoRows {
			return Ticket{}, domainError(ErrorNotFound, "project not found")
		}
		return Ticket{}, fmt.Errorf("load project: %w", err)
	}
	if projectState == ProjectArchived {
		return Ticket{}, domainError(
			ErrorInvalidTransition,
			"archived projects are read-only",
		)
	}
	if err := validateParent(ctx, tx, input.ProjectID, input.ParentTicketID); err != nil {
		return Ticket{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE projects SET next_ticket_number = ?, updated_at = ? WHERE id = ?`,
		number+1,
		formatTime(now),
		input.ProjectID,
	); err != nil {
		return Ticket{}, fmt.Errorf("allocate ticket number: %w", err)
	}

	ticket := Ticket{
		ID:             uuid.NewString(),
		Identifier:     fmt.Sprintf("%s-%d", projectKey, number),
		ProjectID:      input.ProjectID,
		Title:          title,
		Description:    input.Description,
		Status:         input.Status,
		Priority:       input.Priority,
		Assignee:       input.Assignee,
		Revision:       1,
		ParentTicketID: input.ParentTicketID,
		InsertedAt:     now,
		UpdatedAt:      now,
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO tickets
		 (id, project_id, number, title, description, status, priority, assignee,
		  revision, parent_ticket_id, inserted_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticket.ID,
		ticket.ProjectID,
		number,
		ticket.Title,
		ticket.Description,
		ticket.Status,
		ticket.Priority,
		ticket.Assignee,
		ticket.Revision,
		ticket.ParentTicketID,
		formatTime(ticket.InsertedAt),
		formatTime(ticket.UpdatedAt),
	); err != nil {
		return Ticket{}, fmt.Errorf("insert ticket: %w", err)
	}
	ticket.Labels, err = replaceTicketLabels(
		ctx,
		tx,
		ticket.ProjectID,
		ticket.ID,
		input.Labels,
		now,
	)
	if err != nil {
		return Ticket{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"assignee":         ticket.Assignee,
		"identifier":       ticket.Identifier,
		"parent_ticket_id": ticket.ParentTicketID,
		"priority":         ticket.Priority,
		"status":           ticket.Status,
		"title":            ticket.Title,
		"labels":           labelNameValues(ticket.Labels),
	})
	if err != nil {
		return Ticket{}, fmt.Errorf("encode ticket activity: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO activity_events
		 (event_type, actor, project_id, ticket_id, payload, inserted_at)
		 VALUES ('ticket.created', ?, ?, ?, ?, ?)`,
		ActorCodex,
		ticket.ProjectID,
		ticket.ID,
		string(payload),
		formatTime(now),
	); err != nil {
		return Ticket{}, fmt.Errorf("insert ticket activity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit ticket: %w", err)
	}
	return ticket, nil
}

func (s *Service) ListActivityAfter(
	ctx context.Context,
	afterID int64,
) ([]ActivityEvent, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, event_type, actor, project_id, ticket_id, payload, inserted_at
		 FROM activity_events
		 WHERE id > ?
		 ORDER BY id`,
		afterID,
	)
	if err != nil {
		return nil, fmt.Errorf("query activity: %w", err)
	}
	defer rows.Close()

	var events []ActivityEvent
	for rows.Next() {
		var event ActivityEvent
		var payload string
		var insertedAt string
		if err := rows.Scan(
			&event.ID,
			&event.EventType,
			&event.Actor,
			&event.ProjectID,
			&event.TicketID,
			&payload,
			&insertedAt,
		); err != nil {
			return nil, fmt.Errorf("scan activity: %w", err)
		}
		if err := json.Unmarshal([]byte(payload), &event.Payload); err != nil {
			return nil, fmt.Errorf("decode activity payload: %w", err)
		}
		event.InsertedAt, err = time.Parse(time.RFC3339Nano, insertedAt)
		if err != nil {
			return nil, fmt.Errorf("parse activity timestamp: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity: %w", err)
	}
	return events, nil
}

func (s *Service) HighWaterActivityID(ctx context.Context) (int64, error) {
	var id int64
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(max(id), 0) FROM activity_events`,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("query activity high water: %w", err)
	}
	return id, nil
}

func (s *Service) ListActivityPage(
	ctx context.Context,
	afterID int64,
	throughID int64,
	limit int,
) ([]ActivityEvent, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, event_type, actor, project_id, ticket_id, payload, inserted_at
		 FROM activity_events
		 WHERE id > ? AND id <= ?
		 ORDER BY id
		 LIMIT ?`,
		afterID,
		throughID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query activity page: %w", err)
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
		return nil, fmt.Errorf("iterate activity page: %w", err)
	}
	return events, nil
}

type HealthReport struct {
	Status                string `json:"status"`
	SchemaVersion         int64  `json:"schema_version"`
	ActivityHighWater     int64  `json:"activity_high_water"`
	AttachmentWritable    bool   `json:"attachment_writable"`
	OrphanAttachmentFiles int    `json:"orphan_attachment_files"`
}

func (s *Service) HealthReport(ctx context.Context) (HealthReport, error) {
	var value int
	if err := s.db.QueryRowContext(ctx, `SELECT 1`).Scan(&value); err != nil {
		return HealthReport{}, fmt.Errorf("database health check: %w", err)
	}
	if value != 1 {
		return HealthReport{}, fmt.Errorf(
			"database health check returned %d",
			value,
		)
	}
	version, err := store.MigrationVersion(ctx, s.db)
	if err != nil {
		return HealthReport{}, err
	}
	highWater, err := s.HighWaterActivityID(ctx)
	if err != nil {
		return HealthReport{}, err
	}
	attachmentDir := filepath.Join(s.dataDir, "attachments")
	probe, err := os.CreateTemp(attachmentDir, ".health-*")
	if err != nil {
		return HealthReport{}, fmt.Errorf(
			"attachment directory health check: %w",
			err,
		)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return HealthReport{}, fmt.Errorf(
			"close attachment health probe: %w",
			err,
		)
	}
	if err := os.Remove(probePath); err != nil {
		return HealthReport{}, fmt.Errorf(
			"remove attachment health probe: %w",
			err,
		)
	}
	return HealthReport{
		Status:                "ok",
		SchemaVersion:         version,
		ActivityHighWater:     highWater,
		AttachmentWritable:    true,
		OrphanAttachmentFiles: s.orphanAttachments,
	}, nil
}

func (s *Service) Health(ctx context.Context) error {
	_, err := s.HealthReport(ctx)
	return err
}

func (s *Service) prepareAttachmentStorage(ctx context.Context) error {
	attachmentDir := filepath.Join(s.dataDir, "attachments")
	tempDir := filepath.Join(attachmentDir, "tmp")
	if err := ensurePrivateDirectory(attachmentDir); err != nil {
		return fmt.Errorf("prepare attachment directory: %w", err)
	}
	if err := ensurePrivateDirectory(tempDir); err != nil {
		return fmt.Errorf("prepare attachment temporary directory: %w", err)
	}
	temporaryEntries, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("scan attachment temporary directory: %w", err)
	}
	for _, entry := range temporaryEntries {
		if err := os.RemoveAll(filepath.Join(tempDir, entry.Name())); err != nil {
			return fmt.Errorf("remove stale attachment temporary file: %w", err)
		}
	}
	entries, err := os.ReadDir(attachmentDir)
	if err != nil {
		return fmt.Errorf("scan attachment directory: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == "tmp" {
			continue
		}
		path := filepath.Join(attachmentDir, entry.Name())
		var exists int
		err := s.db.QueryRowContext(
			ctx,
			`SELECT 1 FROM attachments WHERE managed_path = ?`,
			path,
		).Scan(&exists)
		switch {
		case err == sql.ErrNoRows:
			s.orphanAttachments++
			slog.Warn("orphaned managed attachment", "path", path)
		case err != nil:
			return fmt.Errorf("check managed attachment record: %w", err)
		}
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
