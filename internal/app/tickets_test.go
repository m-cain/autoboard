package app_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestTicketLabelsAreNormalizedAndReplaced(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket, err := service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Label me",
		Labels:    []string{"  Needs   Review ", "needs review", "Backend"},
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	if got := labelNames(ticket.Labels); !equalStrings(got, []string{"Backend", "Needs Review"}) {
		t.Errorf("labels = %v, want Backend and Needs Review", got)
	}

	labels := []string{" backend ", "Urgent"}
	ticket, err = service.UpdateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, ticket.Revision, app.UpdateTicketInput{
		Labels: &labels,
	})
	if err != nil {
		t.Fatalf("update ticket: %v", err)
	}
	if got := labelNames(ticket.Labels); !equalStrings(got, []string{"Backend", "Urgent"}) {
		t.Errorf("labels = %v, want Backend and Urgent", got)
	}
	if ticket.Revision != 2 {
		t.Errorf("revision = %d, want 2", ticket.Revision)
	}
}

func TestSubtaskDepthAndTerminalParentGuards(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	parent, err := service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Parent",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateTicketInput{
		ProjectID:      project.ID,
		Title:          "Child",
		ParentTicketID: &parent.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	_, err = service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateTicketInput{
		ProjectID:      project.ID,
		Title:          "Grandchild",
		ParentTicketID: &child.ID,
	})
	var domainErr *app.Error
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorValidationFailed {
		t.Fatalf("grandchild error = %v, want validation_failed", err)
	}
	_, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, parent.ID, parent.Revision, app.TicketDone)
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorInvalidTransition {
		t.Fatalf("parent transition error = %v, want invalid_transition", err)
	}
	child, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, child.ID, child.Revision, app.TicketDone)
	if err != nil {
		t.Fatalf("complete child: %v", err)
	}
	parent, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, parent.ID, parent.Revision, app.TicketDone)
	if err != nil {
		t.Fatalf("complete parent: %v", err)
	}
	if parent.Status != app.TicketDone {
		t.Errorf("parent status = %q, want done", parent.Status)
	}
}

func TestDependenciesRejectCyclesAndControlBlocking(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	first := createTicket(t, service, project.ID, "First")
	second := createTicket(t, service, project.ID, "Second")
	third := createTicket(t, service, project.ID, "Third")

	first, err = service.AddDependency(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, first.ID, second.ID, first.Revision)
	if err != nil {
		t.Fatalf("add first dependency: %v", err)
	}
	if !first.Blocked || first.Revision != 2 {
		t.Errorf("blocked ticket = %#v, want blocked revision 2", first)
	}
	_, err = service.AddDependency(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, second.ID, first.ID, second.Revision)
	var domainErr *app.Error
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorDependencyCycle {
		t.Fatalf("direct cycle error = %v, want dependency_cycle", err)
	}
	second, err = service.AddDependency(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, second.ID, third.ID, second.Revision)
	if err != nil {
		t.Fatalf("add second dependency: %v", err)
	}
	_, err = service.AddDependency(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, third.ID, first.ID, third.Revision)
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorDependencyCycle {
		t.Fatalf("multi-hop cycle error = %v, want dependency_cycle", err)
	}

	_, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, first.ID, first.Revision, app.TicketDone)
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorBlockedByDependency {
		t.Fatalf("blocked transition error = %v, want blocked_by_dependency", err)
	}
	_, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, second.ID, second.Revision, app.TicketDone)
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorBlockedByDependency {
		t.Fatalf("second blocked transition error = %v, want blocked_by_dependency", err)
	}
	_, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, third.ID, third.Revision, app.TicketDone)
	if err != nil {
		t.Fatalf("complete third: %v", err)
	}
	second, err = service.GetTicket(ctx, second.ID)
	if err != nil {
		t.Fatalf("reload second: %v", err)
	}
	_, err = service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, second.ID, second.Revision, app.TicketDone)
	if err != nil {
		t.Fatalf("complete second: %v", err)
	}
	first, err = service.GetTicket(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first: %v", err)
	}
	if first.Blocked {
		t.Errorf("first remains blocked after blocker completes")
	}
	if _, err := service.TransitionTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, first.ID, first.Revision, app.TicketDone); err != nil {
		t.Fatalf("complete first: %v", err)
	}
}

func TestUpdateTicketFieldsAndRevisionConflict(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, project.ID, "Original")
	description := "Updated description"
	priority := app.PriorityHigh
	assignee := app.AssigneeCodex
	ticket, err = service.UpdateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, ticket.Revision, app.UpdateTicketInput{
		Title:       pointer("Renamed"),
		Description: &description,
		Priority:    &priority,
		Assignee:    &assignee,
	})
	if err != nil {
		t.Fatalf("update ticket: %v", err)
	}
	if ticket.Title != "Renamed" ||
		ticket.Description != description ||
		ticket.Priority != priority ||
		ticket.Assignee != assignee ||
		ticket.Revision != 2 {
		t.Errorf("updated ticket = %#v", ticket)
	}

	_, err = service.UpdateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, 1, app.UpdateTicketInput{
		Title: pointer("Stale"),
	})
	var domainErr *app.Error
	if !errors.As(err, &domainErr) ||
		domainErr.Kind != app.ErrorRevisionConflict ||
		domainErr.CurrentTicket == nil ||
		domainErr.CurrentTicket.Revision != 2 {
		t.Fatalf("stale update error = %#v, want current revision 2", err)
	}
	_, err = service.UpdateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, ticket.Revision, app.UpdateTicketInput{
		Title: pointer("Renamed"),
	})
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorValidationFailed {
		t.Fatalf("no-op error = %v, want validation_failed", err)
	}
}

func TestRemoveDependencyRequiresEdgeAndAdvancesRevision(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	blocked := createTicket(t, service, project.ID, "Blocked")
	blocker := createTicket(t, service, project.ID, "Blocker")

	_, err = service.RemoveDependency(
		ctx,
		app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex},
		blocked.ID,
		blocker.ID,
		blocked.Revision,
	)
	var domainErr *app.Error
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorValidationFailed {
		t.Fatalf("missing edge error = %v, want validation_failed", err)
	}
	blocked, err = service.AddDependency(
		ctx,
		app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex},
		blocked.ID,
		blocker.ID,
		blocked.Revision,
	)
	if err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	blocked, err = service.RemoveDependency(
		ctx,
		app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex},
		blocked.ID,
		blocker.ID,
		blocked.Revision,
	)
	if err != nil {
		t.Fatalf("remove dependency: %v", err)
	}
	if blocked.Blocked || blocked.Revision != 3 {
		t.Errorf("removed dependency ticket = %#v, want unblocked revision 3", blocked)
	}
}

func TestTicketCreateValidatesEnumsAndTitleLength(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, input := range []app.CreateTicketInput{
		{ProjectID: project.ID, Title: "Bad", Status: "invalid"},
		{ProjectID: project.ID, Title: "Bad", Priority: "invalid"},
		{ProjectID: project.ID, Title: "Bad", Assignee: "invalid"},
		{ProjectID: project.ID, Title: strings.Repeat("x", 501)},
	} {
		_, err := service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, input)
		var domainErr *app.Error
		if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorValidationFailed {
			t.Errorf("create error for %#v = %v, want validation_failed", input, err)
		}
	}
	ticket, err := service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     strings.Repeat("x", 500),
	})
	if err != nil {
		t.Fatalf("create 500-character title: %v", err)
	}
	if len(ticket.Title) != 500 {
		t.Errorf("title length = %d, want 500", len(ticket.Title))
	}
}

func TestTicketDescriptionsAndLabelsEnforceDomainBounds(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{
		Key:  "AUTO",
		Name: "Auto",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, input := range []app.CreateTicketInput{
		{
			ProjectID:   project.ID,
			Title:       "Long description",
			Description: strings.Repeat("x", 100_001),
		},
		{
			ProjectID: project.ID,
			Title:     "Long label",
			Labels:    []string{strings.Repeat("x", 101)},
		},
		{
			ProjectID: project.ID,
			Title:     "Too many labels",
			Labels:    make([]string, 101),
		},
	} {
		for index := range input.Labels {
			if input.Labels[index] == "" {
				input.Labels[index] = fmt.Sprintf("label-%d", index)
			}
		}
		_, err := service.CreateTicket(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, input)
		var domainErr *app.Error
		if !errors.As(err, &domainErr) ||
			domainErr.Kind != app.ErrorValidationFailed {
			t.Errorf("create %#v error = %v, want validation_failed", input, err)
		}
	}
}

func createTicket(
	t *testing.T,
	service *app.Service,
	projectID string,
	title string,
) app.Ticket {
	t.Helper()
	ticket, err := service.CreateTicket(context.Background(), app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateTicketInput{
		ProjectID: projectID,
		Title:     title,
	})
	if err != nil {
		t.Fatalf("create ticket %q: %v", title, err)
	}
	return ticket
}

func labelNames(labels []app.Label) []string {
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		names = append(names, label.Name)
	}
	sort.Strings(names)
	return names
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
