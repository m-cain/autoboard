package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type UpdateProjectInput struct {
	Name        *string
	Description *string
}

func (s *Service) UpdateProject(
	ctx context.Context,
	attribution Attribution,
	projectID string,
	expectedRevision int,
	input UpdateProjectInput,
) (Project, error) {
	if err := attribution.Validate(); err != nil {
		return Project{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project update: %w", err)
	}
	defer tx.Rollback()
	project, err := loadProject(ctx, tx, projectID)
	if err != nil {
		return Project{}, err
	}
	if project.Revision != expectedRevision {
		current := project
		err := domainError(ErrorRevisionConflict, "project revision does not match")
		err.CurrentProject = &current
		return Project{}, err
	}
	if project.State == ProjectArchived {
		return Project{}, domainError(
			ErrorInvalidTransition,
			"archived projects are read-only",
		)
	}

	changes := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return Project{}, &Error{
				Kind:    ErrorValidationFailed,
				Message: "project validation failed",
				Fields:  map[string][]string{"name": {"must not be blank"}},
			}
		}
		if utf8.RuneCountInString(name) > 200 {
			return Project{}, validationError(
				"name",
				"must be at most 200 characters",
			)
		}
		if name != project.Name {
			changes["name"] = map[string]any{"from": project.Name, "to": name}
			project.Name = name
		}
	}
	if input.Description != nil &&
		utf8.RuneCountInString(*input.Description) > 100_000 {
		return Project{}, validationError(
			"description",
			"must be at most 100000 characters",
		)
	}
	if input.Description != nil && *input.Description != project.Description {
		changes["description"] = map[string]any{
			"from": project.Description,
			"to":   *input.Description,
		}
		project.Description = *input.Description
	}
	if len(changes) == 0 {
		return project, nil
	}

	project.Revision++
	project.UpdatedAt = time.Now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE projects
		 SET name = ?, description = ?, revision = ?, updated_at = ?
		 WHERE id = ?`,
		project.Name,
		project.Description,
		project.Revision,
		formatTime(project.UpdatedAt),
		project.ID,
	); err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		attribution,
		"project.updated",
		project.ID,
		nil,
		changes,
		project.UpdatedAt,
	); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project update: %w", err)
	}
	return project, nil
}

func (s *Service) ArchiveProject(
	ctx context.Context,
	attribution Attribution,
	projectID string,
	expectedRevision int,
) (Project, error) {
	return s.changeProjectState(
		ctx,
		attribution,
		projectID,
		expectedRevision,
		ProjectActive,
		ProjectArchived,
		"project.archived",
	)
}

func (s *Service) RestoreProject(
	ctx context.Context,
	attribution Attribution,
	projectID string,
	expectedRevision int,
) (Project, error) {
	return s.changeProjectState(
		ctx,
		attribution,
		projectID,
		expectedRevision,
		ProjectArchived,
		ProjectActive,
		"project.restored",
	)
}

func (s *Service) changeProjectState(
	ctx context.Context,
	attribution Attribution,
	projectID string,
	expectedRevision int,
	from ProjectState,
	to ProjectState,
	eventType string,
) (Project, error) {
	if err := attribution.Validate(); err != nil {
		return Project{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project state change: %w", err)
	}
	defer tx.Rollback()
	project, err := loadProject(ctx, tx, projectID)
	if err != nil {
		return Project{}, err
	}
	if project.Revision != expectedRevision {
		current := project
		err := domainError(ErrorRevisionConflict, "project revision does not match")
		err.CurrentProject = &current
		return Project{}, err
	}
	if project.State != from {
		return Project{}, domainError(
			ErrorInvalidTransition,
			fmt.Sprintf("project must be %s", from),
		)
	}
	project.State = to
	project.Revision++
	project.UpdatedAt = time.Now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE projects SET state = ?, revision = ?, updated_at = ? WHERE id = ?`,
		project.State,
		project.Revision,
		formatTime(project.UpdatedAt),
		project.ID,
	); err != nil {
		return Project{}, fmt.Errorf("update project state: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		attribution,
		eventType,
		project.ID,
		nil,
		map[string]any{"state": map[string]any{"from": from, "to": to}},
		project.UpdatedAt,
	); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project state: %w", err)
	}
	return project, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadProject(
	ctx context.Context,
	query queryRower,
	projectID string,
) (Project, error) {
	var project Project
	var insertedAt string
	var updatedAt string
	err := query.QueryRowContext(
		ctx,
		`SELECT id, key, name, description, state, revision, created_performed_by, created_initiated_by, inserted_at, updated_at
		 FROM projects WHERE id = ?`,
		projectID,
	).Scan(
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, domainError(ErrorNotFound, "project not found")
	}
	if err != nil {
		return Project{}, fmt.Errorf("load project: %w", err)
	}
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

func insertActivity(
	ctx context.Context,
	tx *sql.Tx,
	attribution Attribution,
	eventType string,
	projectID string,
	ticketID *string,
	payloadValue map[string]any,
	insertedAt time.Time,
) error {
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return fmt.Errorf("encode activity: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO activity_events
		 (event_type, performed_by, initiated_by, project_id, ticket_id, payload, inserted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventType,
		attribution.PerformedBy,
		attribution.InitiatedBy,
		projectID,
		ticketID,
		string(payload),
		formatTime(insertedAt),
	); err != nil {
		return fmt.Errorf("insert activity: %w", err)
	}
	return nil
}
