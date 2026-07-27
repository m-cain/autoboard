package app

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Comment struct {
	ID         string    `json:"id" jsonschema_extras:"format=uuid"`
	TicketID   string    `json:"ticket_id" jsonschema_extras:"format=uuid"`
	ProjectID  string    `json:"project_id" jsonschema_extras:"format=uuid"`
	Body       string    `json:"body" jsonschema_extras:"minLength=1,maxLength=100000"`
	Actor      Actor     `json:"actor" jsonschema_extras:"enum=me,enum=codex,enum=system"`
	InsertedAt time.Time `json:"inserted_at"`
}

func (s *Service) AddComment(
	ctx context.Context,
	ticketID string,
	body string,
) (Comment, Ticket, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if strings.TrimSpace(body) == "" {
		return Comment{}, Ticket{}, validationError("body", "must not be blank")
	}
	if utf8.RuneCountInString(body) > 100_000 {
		return Comment{}, Ticket{}, validationError(
			"body",
			"must be at most 100000 characters",
		)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, Ticket{}, fmt.Errorf("begin comment add: %w", err)
	}
	defer tx.Rollback()
	ticket, err := loadTicket(ctx, tx, ticketID)
	if err != nil {
		return Comment{}, Ticket{}, err
	}
	if err := requireActiveTicketProject(ctx, tx, ticket.ProjectID); err != nil {
		return Comment{}, Ticket{}, err
	}
	now := time.Now().UTC()
	comment := Comment{
		ID:         uuid.NewString(),
		TicketID:   ticket.ID,
		ProjectID:  ticket.ProjectID,
		Body:       body,
		Actor:      ActorCodex,
		InsertedAt: now,
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO comments
		 (id, ticket_id, project_id, body, actor, inserted_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		comment.ID,
		comment.TicketID,
		comment.ProjectID,
		comment.Body,
		comment.Actor,
		formatTime(comment.InsertedAt),
	); err != nil {
		return Comment{}, Ticket{}, fmt.Errorf("insert comment: %w", err)
	}
	ticket.Revision++
	ticket.UpdatedAt = now
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE tickets SET revision = ?, updated_at = ? WHERE id = ?`,
		ticket.Revision,
		formatTime(now),
		ticket.ID,
	); err != nil {
		return Comment{}, Ticket{}, fmt.Errorf("advance comment ticket revision: %w", err)
	}
	if err := insertActivity(
		ctx,
		tx,
		"comment.added",
		ticket.ProjectID,
		&ticket.ID,
		map[string]any{"comment_id": comment.ID, "body": comment.Body},
		now,
	); err != nil {
		return Comment{}, Ticket{}, err
	}
	if err := tx.Commit(); err != nil {
		return Comment{}, Ticket{}, fmt.Errorf("commit comment add: %w", err)
	}
	return comment, ticket, nil
}
