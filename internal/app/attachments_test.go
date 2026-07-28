package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m-cain/autoboard/internal/app"
)

func TestAttachmentCopiesMetadataAndReadsSmallTextInline(t *testing.T) {
	service, dataDir := openAttachmentService(t, 50*1024*1024)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, project.ID, "Attached")
	source := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	attachment, ticket, err := service.AddAttachmentFromPath(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, source)
	if err != nil {
		t.Fatalf("add attachment: %v", err)
	}
	if attachment.OriginalFilename != "note.txt" ||
		attachment.MediaType != "text/plain" ||
		attachment.ByteSize != 5 ||
		attachment.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Errorf("attachment metadata = %#v", attachment)
	}
	if ticket.Revision != 2 {
		t.Errorf("ticket revision = %d, want 2", ticket.Revision)
	}
	info, err := os.Stat(attachment.ManagedPath)
	if err != nil {
		t.Fatalf("stat managed attachment: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("managed file permissions = %o, want 600", info.Mode().Perm())
	}
	for _, directory := range []string{
		filepath.Join(dataDir, "attachments"),
		filepath.Join(dataDir, "attachments", "tmp"),
	} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatalf("stat managed directory: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("%s permissions = %o, want 700", directory, info.Mode().Perm())
		}
	}
	read, err := service.ReadAttachment(ctx, attachment.ID)
	if err != nil {
		t.Fatalf("read attachment: %v", err)
	}
	if read.Content == nil || *read.Content != "hello" || read.ManagedPath != nil {
		t.Errorf("attachment read = %#v, want inline hello", read)
	}
}

func TestAttachmentRejectsUnsafePathsAndConfiguredSize(t *testing.T) {
	service, _ := openAttachmentService(t, 3)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, project.ID, "Attached")
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "source.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	symlink := filepath.Join(sourceDir, "source-link.txt")
	if err := os.Symlink(source, symlink); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	for _, path := range []string{"source.txt", sourceDir, symlink, source} {
		_, _, err := service.AddAttachmentFromPath(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, ticket.ID, path)
		var domainErr *app.Error
		if !errors.As(err, &domainErr) || domainErr.Kind != app.ErrorValidationFailed {
			t.Errorf("path %q error = %v, want validation_failed", path, err)
		}
	}
	unchanged, err := service.GetTicket(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("reload ticket: %v", err)
	}
	if unchanged.Revision != ticket.Revision {
		t.Errorf("ticket revision = %d, want %d", unchanged.Revision, ticket.Revision)
	}
}

func TestAttachmentReadUsesInlineBoundaryAndUTF8Guard(t *testing.T) {
	service, _ := openAttachmentService(t, 2*1024*1024)
	ctx := context.Background()
	project, err := service.CreateProject(ctx, app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex}, app.CreateProjectInput{Key: "AUTO", Name: "Auto"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	ticket := createTicket(t, service, project.ID, "Attached")
	for _, fixture := range []struct {
		name         string
		content      []byte
		expectInline bool
	}{
		{name: "exact.txt", content: []byte(strings.Repeat("a", 262_144)), expectInline: true},
		{name: "over.txt", content: []byte(strings.Repeat("b", 262_145))},
		{name: "invalid.txt", content: []byte{0xff, 0xfe}},
	} {
		source := filepath.Join(t.TempDir(), fixture.name)
		if err := os.WriteFile(source, fixture.content, 0o600); err != nil {
			t.Fatalf("write %s: %v", fixture.name, err)
		}
		attachment, updated, err := service.AddAttachmentFromPath(
			ctx,
			app.Attribution{PerformedBy: app.PrincipalCodex, InitiatedBy: app.PrincipalCodex},
			ticket.ID,
			source,
		)
		if err != nil {
			t.Fatalf("add %s: %v", fixture.name, err)
		}
		ticket = updated
		read, err := service.ReadAttachment(ctx, attachment.ID)
		if err != nil {
			t.Fatalf("read %s: %v", fixture.name, err)
		}
		if fixture.expectInline != (read.Content != nil) {
			t.Errorf("%s inline = %v, want %v", fixture.name, read.Content != nil, fixture.expectInline)
		}
		if !fixture.expectInline &&
			(read.ManagedPath == nil || *read.ManagedPath != attachment.ManagedPath) {
			t.Errorf("%s managed path = %#v", fixture.name, read.ManagedPath)
		}
	}
}

func TestStartupCleansTemporaryAttachmentsAndReportsOrphans(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	tempDir := filepath.Join(dataDir, "attachments", "tmp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatalf("create attachment directories: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(tempDir, "staged-old"),
		[]byte("old"),
		0o600,
	); err != nil {
		t.Fatalf("write staged file: %v", err)
	}
	orphanPath := filepath.Join(dataDir, "attachments", "orphan")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	service, err := app.Open(context.Background(), app.Config{
		DatabasePath: filepath.Join(dataDir, "autoboard.db"),
		DataDir:      dataDir,
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	defer service.Close()
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("read temporary directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temporary entries = %d, want 0", len(entries))
	}
	report, err := service.HealthReport(context.Background())
	if err != nil {
		t.Fatalf("health report: %v", err)
	}
	if report.SchemaVersion != 1 ||
		!report.AttachmentWritable ||
		report.OrphanAttachmentFiles != 1 {
		t.Errorf("health report = %#v", report)
	}
}

func openAttachmentService(t *testing.T, maxBytes int64) (*app.Service, string) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	service, err := app.Open(context.Background(), app.Config{
		DatabasePath:       filepath.Join(root, "autoboard.db"),
		DataDir:            dataDir,
		MaxAttachmentBytes: maxBytes,
	})
	if err != nil {
		t.Fatalf("open app: %v", err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close app: %v", err)
		}
	})
	return service, dataDir
}
