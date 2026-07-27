package launchagent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandLauncherRunsAndInspectsLaunchctl(t *testing.T) {
	bin := t.TempDir()
	script := filepath.Join(bin, "launchctl")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
case "$1" in
  bootstrap-ok) exit 0 ;;
  bootstrap-fail) printf '%s\n' 'bootstrap denied' >&2; exit 7 ;;
  run) printf '%s\n' 'run stdout'; printf '%s\n' 'run stderr' >&2; exit 0 ;;
  loaded) exit 0 ;;
  missing) printf '%s\n' 'Could not find service' >&2; exit 3 ;;
  broken) printf '%s\n' 'permission denied' >&2; exit 4 ;;
esac
`), 0o700); err != nil {
		t.Fatalf("write fake launchctl: %v", err)
	}
	t.Setenv("PATH", bin)
	ctx := context.Background()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	launcher := CommandLauncher{Stdout: &stdout, Stderr: &stderr}

	if err := launcher.Bootstrap(ctx, "bootstrap-ok"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := launcher.Bootstrap(ctx, "bootstrap-fail"); err == nil ||
		!strings.Contains(err.Error(), "bootstrap denied") {
		t.Fatalf("bootstrap failure = %v", err)
	}
	if err := launcher.Run(ctx, "run"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "run stdout\n" || stderr.String() != "run stderr\n" {
		t.Fatalf("run output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	loaded, err := launcher.Loaded(ctx, "loaded")
	if err != nil || !loaded {
		t.Fatalf("loaded = %v, %v; want true, nil", loaded, err)
	}
	loaded, err = launcher.Loaded(ctx, "missing")
	if err != nil || loaded {
		t.Fatalf("missing = %v, %v; want false, nil", loaded, err)
	}
	loaded, err = launcher.Loaded(ctx, "broken")
	if err == nil || loaded || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("broken = %v, %v; want false and diagnostic", loaded, err)
	}
}
