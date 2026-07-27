package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesAndChecksGeneratedContracts(t *testing.T) {
	root := t.TempDir()
	schemaDirectory := filepath.Join(root, "schemas")
	modulePath := filepath.Join(root, "generated", "browser-schemas.ts")
	var stderr bytes.Buffer

	if code := run([]string{schemaDirectory, modulePath}, &stderr); code != 0 {
		t.Fatalf("write exit code = %d, stderr=%s", code, stderr.String())
	}
	if code := run(
		[]string{"--check", schemaDirectory, modulePath},
		&stderr,
	); code != 0 {
		t.Fatalf("check exit code = %d, stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(
		schemaDirectory,
		"project-board.schema.json",
	)); err != nil {
		t.Fatalf("generated project schema: %v", err)
	}
	if _, err := os.Stat(modulePath); err != nil {
		t.Fatalf("generated TypeScript module: %v", err)
	}
}

func TestRunReportsUsageAndStaleContracts(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Fatalf("usage exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage: autoboard-schema") {
		t.Fatalf("usage stderr = %q", stderr.String())
	}

	root := t.TempDir()
	schemaDirectory := filepath.Join(root, "schemas")
	modulePath := filepath.Join(root, "browser-schemas.ts")
	stderr.Reset()
	if code := run([]string{schemaDirectory, modulePath}, &stderr); code != 0 {
		t.Fatalf("write exit code = %d, stderr=%s", code, stderr.String())
	}
	stalePath := filepath.Join(schemaDirectory, "ticket-list.schema.json")
	if err := os.WriteFile(stalePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("make generated schema stale: %v", err)
	}
	stderr.Reset()
	if code := run(
		[]string{"--check", schemaDirectory, modulePath},
		&stderr,
	); code != 1 {
		t.Fatalf("stale check exit code = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "is stale") {
		t.Fatalf("stale stderr = %q", stderr.String())
	}

	stderr.Reset()
	if code := run(
		[]string{"--check", filepath.Join(root, "missing"), modulePath},
		&stderr,
	); code != 1 {
		t.Fatalf("missing check exit code = %d, stderr=%s", code, stderr.String())
	}

	invalidDirectory := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(invalidDirectory, []byte("file"), 0o600); err != nil {
		t.Fatalf("write invalid output directory: %v", err)
	}
	stderr.Reset()
	if code := run(
		[]string{filepath.Join(invalidDirectory, "schemas"), modulePath},
		&stderr,
	); code != 1 {
		t.Fatalf("invalid write exit code = %d, stderr=%s", code, stderr.String())
	}
}
