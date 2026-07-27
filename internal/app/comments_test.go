package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestAddCommentAppendsAndAdvancesTicketRevision(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, project.ID, "Commented")

	comment, ticket, err := service.AddComment(ctx, ticket.ID, "A useful note")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if comment.Body != "A useful note" ||
		comment.Actor != app.ActorCodex ||
		comment.TicketID != ticket.ID ||
		comment.ProjectID != project.ID {
		t.Errorf("comment = %#v", comment)
	}
	if ticket.Revision != 2 {
		t.Errorf("ticket revision = %d, want 2", ticket.Revision)
	}
	events, err := service.ListActivityAfter(ctx, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if got := events[len(events)-1].EventType; got != "comment.added" {
		t.Errorf("last event = %q, want comment.added", got)
	}
}

func TestAddCommentRejectsBlankAndArchivedProject(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, project.ID, "Commented")

	_, _, err = service.AddComment(ctx, ticket.ID, " \n\t ")
	var domainErr *app.Error
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorValidationFailed {
		t.Fatalf("blank comment error = %v, want validation_failed", err)
	}
	_, err = service.ArchiveProject(ctx, project.ID, project.Revision)
	if err != nil {
		t.Fatalf("archive project: %v", err)
	}
	_, _, err = service.AddComment(ctx, ticket.ID, "Nope")
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorInvalidTransition {
		t.Fatalf("archived comment error = %v, want invalid_transition", err)
	}
	unchanged, err := service.GetTicket(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("reload ticket: %v", err)
	}
	if unchanged.Revision != ticket.Revision {
		t.Errorf("ticket revision = %d, want %d", unchanged.Revision, ticket.Revision)
	}
}
