package app_test

import (
	"context"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestReadModelsListBoardSearchAndCanceled(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	active, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{
		Key:  "AUTO",
		Name: "Zulu",
	})
	if err != nil {
		t.Fatalf("create active project: %v", err)
	}
	archived, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{
		Key:  "OLD",
		Name: "Alpha",
	})
	if err != nil {
		t.Fatalf("create archived project: %v", err)
	}
	archived, err = service.ArchiveProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, archived.ID, archived.Revision)
	if err != nil {
		t.Fatalf("archive project: %v", err)
	}
	triage := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: active.ID,
		Title:     "Find a special phrase",
	})
	backlog := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: active.ID,
		Title:     "Backlog",
		Status:    app.TicketBacklog,
	})
	canceled := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: active.ID,
		Title:     "Canceled",
		Status:    app.TicketCanceled,
	})

	projects, err := service.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects.Active) != 1 ||
		projects.Active[0].ID != active.ID ||
		len(projects.Archived) != 1 ||
		projects.Archived[0].ID != archived.ID {
		t.Errorf("projects = %#v", projects)
	}
	triageTickets, err := service.ListTriageTickets(ctx)
	if err != nil {
		t.Fatalf("list triage: %v", err)
	}
	if len(triageTickets) != 1 || triageTickets[0].ID != triage.ID {
		t.Errorf("triage tickets = %#v", triageTickets)
	}
	board, err := service.GetProjectBoard(ctx, "auto")
	if err != nil {
		t.Fatalf("get board: %v", err)
	}
	if board.Project.ID != active.ID ||
		len(board.Columns.Backlog) != 1 ||
		board.Columns.Backlog[0].ID != backlog.ID {
		t.Errorf("board = %#v", board)
	}
	canceledTickets, err := service.ListCanceledTickets(ctx, "AUTO")
	if err != nil {
		t.Fatalf("list canceled: %v", err)
	}
	if len(canceledTickets) != 1 || canceledTickets[0].ID != canceled.ID {
		t.Errorf("canceled tickets = %#v", canceledTickets)
	}
	found, err := service.SearchTickets(ctx, "SPECIAL", active.ID, 25)
	if err != nil {
		t.Fatalf("search tickets: %v", err)
	}
	if len(found) != 1 || found[0].ID != triage.ID {
		t.Errorf("search result = %#v", found)
	}
}

func TestTicketDetailHydratesRelationshipsCountsAndActivity(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	parent := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Parent",
		Labels:    []string{"Backend"},
	})
	child := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID:      project.ID,
		Title:          "Child",
		ParentTicketID: &parent.ID,
	})
	blocker := createTicket(t, service, project.ID, "Blocker")
	blocked := createTicket(t, service, project.ID, "Blocked")
	parent, err = service.AddDependency(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, parent.ID, blocker.ID, parent.Revision)
	if err != nil {
		t.Fatalf("add parent dependency: %v", err)
	}
	_, err = service.AddDependency(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, blocked.ID, parent.ID, blocked.Revision)
	if err != nil {
		t.Fatalf("add blocked dependency: %v", err)
	}
	_, parent, err = service.AddComment(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, parent.ID, "A note")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}

	detail, err := service.GetTicketDetail(ctx, "auto-1")
	if err != nil {
		t.Fatalf("get ticket detail: %v", err)
	}
	if detail.ID != parent.ID ||
		detail.Project.ID != project.ID ||
		detail.Parent != nil ||
		len(detail.Subtasks) != 1 ||
		detail.Subtasks[0].ID != child.ID ||
		len(detail.Blockers) != 1 ||
		detail.Blockers[0].ID != blocker.ID ||
		len(detail.BlockedTickets) != 1 ||
		detail.BlockedTickets[0].ID != blocked.ID ||
		len(detail.Comments) != 1 ||
		len(detail.Activity) < 3 {
		t.Errorf("ticket detail = %#v", detail)
	}
	if !detail.Blocked ||
		detail.CommentCount != 1 ||
		detail.AttachmentCount != 0 ||
		len(detail.Labels) != 1 ||
		detail.Ticket.Labels[0].ProjectID != project.ID {
		t.Errorf("hydrated ticket = %#v", detail.Ticket)
	}
}

func TestActionableTicketsExcludeBlockedAndParentWork(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	urgent := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Urgent",
		Status:    app.TicketReady,
		Priority:  app.PriorityUrgent,
		Assignee:  app.AssigneeCodex,
	})
	normal := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Normal",
		Status:    app.TicketReady,
		Assignee:  app.AssigneeCodex,
	})
	blocker := createTicket(t, service, project.ID, "Blocker")
	blocked := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Blocked",
		Status:    app.TicketReady,
		Assignee:  app.AssigneeCodex,
	})
	if _, err := service.AddDependency(
		ctx,
		app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex},
		blocked.ID,
		blocker.ID,
		blocked.Revision,
	); err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	parent := createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Parent",
		Status:    app.TicketReady,
		Assignee:  app.AssigneeCodex,
	})
	createTicketWithInput(t, service, app.CreateTicketInput{
		ProjectID:      project.ID,
		Title:          "Child",
		ParentTicketID: &parent.ID,
	})

	actionable, err := service.ListActionableTickets(ctx, project.ID, 25)
	if err != nil {
		t.Fatalf("list actionable: %v", err)
	}
	if len(actionable) != 2 ||
		actionable[0].ID != urgent.ID ||
		actionable[1].ID != normal.ID {
		t.Errorf("actionable tickets = %#v", actionable)
	}
}

func createTicketWithInput(
	t *testing.T,
	service *app.Service,
	input app.CreateTicketInput,
) app.Ticket {
	t.Helper()
	ticket, err := service.CreateTicket(context.Background(), app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, input)
	if err != nil {
		t.Fatalf("create ticket %q: %v", input.Title, err)
	}
	return ticket
}
