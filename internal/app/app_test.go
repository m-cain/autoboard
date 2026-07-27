package app_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestCreateProjectPersistsDefaultsAndActivity(t *testing.T) {
	ctx := context.Background()
	service, err := app.Open(ctx, app.Config{
		DatabasePath: filepath.Join(t.TempDir(), "autoboard.db"),
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})

	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "auto",
		Name: "Autoboard",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if project.Key != "AUTO" {
		t.Errorf("key = %q, want AUTO", project.Key)
	}
	if project.Description != "" {
		t.Errorf("description = %q, want empty", project.Description)
	}
	if project.State != app.ProjectActive {
		t.Errorf("state = %q, want active", project.State)
	}
	if project.Revision != 1 {
		t.Errorf("revision = %d, want 1", project.Revision)
	}

	events, err := service.ListActivityAfter(ctx, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("activity count = %d, want 1", len(events))
	}
	if events[0].EventType != "project.created" {
		t.Errorf("event type = %q, want project.created", events[0].EventType)
	}
	if events[0].Actor != app.ActorCodex {
		t.Errorf("actor = %q, want codex", events[0].Actor)
	}
	if events[0].ProjectID != project.ID {
		t.Errorf("event project = %q, want %q", events[0].ProjectID, project.ID)
	}
}

func TestClosedServiceRejectsAllOperationsWithoutPanicking(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	service, err := app.Open(ctx, app.Config{
		DatabasePath: filepath.Join(root, "autoboard.db"),
		DataDir:      filepath.Join(root, "data"),
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}

	const id = "11111111-1111-1111-1111-111111111111"
	name := "Updated"
	title := "Updated"
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("attachment"), 0o600); err != nil {
		t.Fatalf("write attachment source: %v", err)
	}
	operations := map[string]func() error{
		"health": func() error {
			return service.Health(ctx)
		},
		"list projects": func() error {
			_, operationErr := service.ListProjects(ctx)
			return operationErr
		},
		"get project": func() error {
			_, operationErr := service.GetProject(ctx, "AUTO")
			return operationErr
		},
		"project board": func() error {
			_, operationErr := service.GetProjectBoard(ctx, "AUTO")
			return operationErr
		},
		"triage": func() error {
			_, operationErr := service.ListTriageTickets(ctx)
			return operationErr
		},
		"canceled": func() error {
			_, operationErr := service.ListCanceledTickets(ctx, "AUTO")
			return operationErr
		},
		"search": func() error {
			_, operationErr := service.SearchTickets(ctx, "query", "", 25)
			return operationErr
		},
		"actionable": func() error {
			_, operationErr := service.ListActionableTickets(ctx, "", 25)
			return operationErr
		},
		"ticket detail": func() error {
			_, operationErr := service.GetTicketDetail(ctx, "AUTO-1")
			return operationErr
		},
		"activity": func() error {
			_, operationErr := service.ListActivityAfter(ctx, 0)
			return operationErr
		},
		"activity page": func() error {
			_, operationErr := service.ListActivityPage(ctx, 0, 10, 10)
			return operationErr
		},
		"activity high water": func() error {
			_, operationErr := service.HighWaterActivityID(ctx)
			return operationErr
		},
		"create project": func() error {
			_, operationErr := service.CreateProject(ctx, app.CreateProjectInput{
				Key:  "AUTO",
				Name: "Autoboard",
			})
			return operationErr
		},
		"update project": func() error {
			_, operationErr := service.UpdateProject(
				ctx,
				id,
				1,
				app.UpdateProjectInput{Name: &name},
			)
			return operationErr
		},
		"archive project": func() error {
			_, operationErr := service.ArchiveProject(ctx, id, 1)
			return operationErr
		},
		"restore project": func() error {
			_, operationErr := service.RestoreProject(ctx, id, 1)
			return operationErr
		},
		"create ticket": func() error {
			_, operationErr := service.CreateTicket(ctx, app.CreateTicketInput{
				ProjectID: id,
				Title:     "Ticket",
			})
			return operationErr
		},
		"get ticket": func() error {
			_, operationErr := service.GetTicket(ctx, id)
			return operationErr
		},
		"update ticket": func() error {
			_, operationErr := service.UpdateTicket(
				ctx,
				id,
				1,
				app.UpdateTicketInput{Title: &title},
			)
			return operationErr
		},
		"transition ticket": func() error {
			_, operationErr := service.TransitionTicket(
				ctx,
				id,
				1,
				app.TicketDone,
			)
			return operationErr
		},
		"add dependency": func() error {
			_, operationErr := service.AddDependency(ctx, id, id, 1)
			return operationErr
		},
		"remove dependency": func() error {
			_, operationErr := service.RemoveDependency(ctx, id, id, 1)
			return operationErr
		},
		"add comment": func() error {
			_, _, operationErr := service.AddComment(ctx, id, "Comment")
			return operationErr
		},
		"get attachment": func() error {
			_, operationErr := service.GetAttachment(ctx, id)
			return operationErr
		},
		"read attachment": func() error {
			_, operationErr := service.ReadAttachment(ctx, id)
			return operationErr
		},
		"add attachment": func() error {
			_, _, operationErr := service.AddAttachmentFromPath(ctx, id, source)
			return operationErr
		},
	}
	for operation, invoke := range operations {
		if operationErr := invoke(); operationErr == nil {
			t.Errorf("%s succeeded after service close", operation)
		}
	}

	domainErr := &app.Error{
		Kind:    app.ErrorNotFound,
		Message: "ticket not found",
	}
	if domainErr.Error() != "not_found: ticket not found" {
		t.Errorf("domain error string = %q", domainErr.Error())
	}
}

func TestCreateTicketAllocatesProjectLocalNumbersConcurrently(t *testing.T) {
	ctx := context.Background()
	service, err := app.Open(ctx, app.Config{
		DatabasePath: filepath.Join(t.TempDir(), "autoboard.db"),
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "auto",
		Name: "Autoboard",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	const count = 12
	tickets := make(chan app.Ticket, count)
	errors := make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Go(func() {
			ticket, err := service.CreateTicket(ctx, app.CreateTicketInput{
				ProjectID: project.ID,
				Title:     "Concurrent ticket",
			})
			if err != nil {
				errors <- err
				return
			}
			tickets <- ticket
		})
	}
	workers.Wait()
	close(errors)
	close(tickets)

	for err := range errors {
		t.Errorf("create ticket: %v", err)
	}
	var identifiers []string
	for ticket := range tickets {
		if ticket.Status != app.TicketTriage {
			t.Errorf("status = %q, want triage", ticket.Status)
		}
		if ticket.Priority != app.PriorityNone {
			t.Errorf("priority = %q, want none", ticket.Priority)
		}
		if ticket.Assignee != app.AssigneeUnassigned {
			t.Errorf("assignee = %q, want unassigned", ticket.Assignee)
		}
		if ticket.Revision != 1 {
			t.Errorf("revision = %d, want 1", ticket.Revision)
		}
		identifiers = append(identifiers, ticket.Identifier)
	}
	sort.Strings(identifiers)
	want := []string{
		"AUTO-1", "AUTO-10", "AUTO-11", "AUTO-12", "AUTO-2", "AUTO-3",
		"AUTO-4", "AUTO-5", "AUTO-6", "AUTO-7", "AUTO-8", "AUTO-9",
	}
	if len(identifiers) != len(want) {
		t.Fatalf("ticket count = %d, want %d", len(identifiers), len(want))
	}
	for index := range want {
		if identifiers[index] != want[index] {
			t.Errorf("identifier[%d] = %q, want %q", index, identifiers[index], want[index])
		}
	}
}
