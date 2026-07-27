package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestProjectCreateAndUpdateEnforceDomainBounds(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	for _, input := range []app.CreateProjectInput{
		{Key: "x", Name: "Too short"},
		{Key: "BAD-KEY", Name: "Punctuation"},
		{Key: "TOOLONG99", Name: "Too long"},
		{Key: "GOOD", Name: strings.Repeat("x", 201)},
		{Key: "GOOD", Name: "Good", Description: strings.Repeat("x", 100_001)},
	} {
		_, err := service.CreateProject(ctx, input)
		var domainErr *app.Error
		if !errors.As(err, &domainErr) ||
			domainErr.Kind != app.ErrorValidationFailed {
			t.Errorf("create %#v error = %v, want validation_failed", input, err)
		}
	}
	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "GOOD",
		Name: "Good",
	})
	if err != nil {
		t.Fatalf("create valid project: %v", err)
	}
	_, err = service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "good",
		Name: "Duplicate",
	})
	var domainErr *app.Error
	if !errors.As(err, &domainErr) ||
		domainErr.Kind != app.ErrorValidationFailed {
		t.Errorf("duplicate error = %v, want validation_failed", err)
	}
	name := strings.Repeat("x", 201)
	_, err = service.UpdateProject(
		ctx,
		project.ID,
		project.Revision,
		app.UpdateProjectInput{Name: &name},
	)
	if !errors.As(err, &domainErr) ||
		domainErr.Kind != app.ErrorValidationFailed {
		t.Errorf("oversize update error = %v, want validation_failed", err)
	}
}

func TestUpdateProjectReturnsCurrentProjectOnRevisionConflict(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "auto",
		Name: "Autoboard",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	_, err = service.UpdateProject(ctx, project.ID, 99, app.UpdateProjectInput{
		Name: pointer("Renamed"),
	})
	var domainErr *app.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("error = %v, want domain error", err)
	}
	if domainErr.Kind != app.ErrorRevisionConflict {
		t.Errorf("kind = %q, want revision_conflict", domainErr.Kind)
	}
	if domainErr.CurrentProject == nil || domainErr.CurrentProject.Revision != 1 {
		t.Errorf("current project = %#v, want revision 1", domainErr.CurrentProject)
	}
}

func TestUpdateProjectNoOpDoesNotAdvanceRevisionOrActivity(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "auto",
		Name: "Autoboard",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	updated, err := service.UpdateProject(ctx, project.ID, 1, app.UpdateProjectInput{
		Name: pointer("Autoboard"),
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.Revision != 1 {
		t.Errorf("revision = %d, want 1", updated.Revision)
	}
	events, err := service.ListActivityAfter(ctx, 0)
	if err != nil {
		t.Fatalf("list activity: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("activity count = %d, want 1", len(events))
	}
}

func TestArchivedProjectRejectsTicketCreationUntilRestored(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "auto",
		Name: "Autoboard",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	project, err = service.ArchiveProject(ctx, project.ID, project.Revision)
	if err != nil {
		t.Fatalf("archive project: %v", err)
	}

	_, err = service.CreateTicket(ctx, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Rejected",
	})
	var domainErr *app.Error
	if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorInvalidTransition {
		t.Fatalf("create ticket error = %v, want invalid_transition", err)
	}

	project, err = service.RestoreProject(ctx, project.ID, project.Revision)
	if err != nil {
		t.Fatalf("restore project: %v", err)
	}
	if project.State != app.ProjectActive || project.Revision != 3 {
		t.Errorf("restored project = %#v, want active revision 3", project)
	}
	if _, err := service.CreateTicket(ctx, app.CreateTicketInput{
		ProjectID: project.ID,
		Title:     "Accepted",
	}); err != nil {
		t.Fatalf("create ticket after restore: %v", err)
	}
}

func TestProjectStateChangesRejectStaleAndRepeatedRequests(t *testing.T) {
	service := openService(t)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.CreateProjectInput{
		Key:  "AUTO",
		Name: "Autoboard",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	_, err = service.RestoreProject(ctx, project.ID, project.Revision)
	var domainErr *app.Error
	if !errors.As(err, &domainErr) ||
		domainErr.Kind != app.ErrorInvalidTransition {
		t.Fatalf("restore active project error = %v, want invalid_transition", err)
	}
	_, err = service.ArchiveProject(ctx, project.ID, 99)
	if !errors.As(err, &domainErr) ||
		domainErr.Kind != app.ErrorRevisionConflict ||
		domainErr.CurrentProject == nil {
		t.Fatalf("stale archive error = %#v, want revision_conflict", err)
	}
	project, err = service.ArchiveProject(ctx, project.ID, project.Revision)
	if err != nil {
		t.Fatalf("archive project: %v", err)
	}
	_, err = service.UpdateProject(
		ctx,
		project.ID,
		project.Revision,
		app.UpdateProjectInput{Name: pointer("Renamed")},
	)
	if !errors.As(err, &domainErr) ||
		domainErr.Kind != app.ErrorInvalidTransition {
		t.Fatalf("update archived project error = %v, want invalid_transition", err)
	}
}

func openService(t *testing.T) *app.Service {
	t.Helper()
	service, err := app.Open(context.Background(), app.Config{
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
	return service
}

func pointer[T any](value T) *T {
	return &value
}
