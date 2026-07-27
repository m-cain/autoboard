package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareReplacesEmbeddedAssetsAndPreservesPlaceholder(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	target := filepath.Join(root, "embedded")
	for _, directory := range []string{
		filepath.Join(source, "assets"),
		target,
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %s: %v", directory, err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(source, "index.html"):       "new index",
		filepath.Join(source, "assets", "app.js"): "new asset",
		filepath.Join(target, ".placeholder"):     "",
		filepath.Join(target, "old.js"):           "old asset",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	var stderr bytes.Buffer
	if code := run(source, target, &stderr); code != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", code, stderr.String())
	}
	for _, path := range []string{
		filepath.Join(target, ".placeholder"),
		filepath.Join(target, "index.html"),
		filepath.Join(target, "assets", "app.js"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "old.js")); !os.IsNotExist(err) {
		t.Errorf("old asset still exists: %v", err)
	}
}

func TestPrepareRejectsMissingBrowserBuild(t *testing.T) {
	err := prepare(
		filepath.Join(t.TempDir(), "missing"),
		filepath.Join(t.TempDir(), "embedded"),
	)
	if err == nil || !strings.Contains(err.Error(), "browser build is missing") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestPrepareReportsInvalidEmbeddedAssetDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web")
	target := filepath.Join(root, "embedded")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatalf("create browser build: %v", err)
	}
	if err := os.WriteFile(target, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write invalid target: %v", err)
	}

	err := prepare(source, target)
	if err == nil ||
		!strings.Contains(err.Error(), "create embedded asset directory") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestRunReportsPreparationFailure(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	if code := run(
		filepath.Join(root, "missing"),
		filepath.Join(root, "embedded"),
		&stderr,
	); code != 1 {
		t.Fatalf("run exit code = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "browser build is missing") {
		t.Fatalf("run stderr = %q", stderr.String())
	}
}
