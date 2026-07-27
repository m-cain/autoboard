package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRunReturnsTheFailedChildExitCodeAndStopsItsPeer(t *testing.T) {
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "go"), "#!/bin/sh\nexit 7\n")
	writeExecutable(
		t,
		filepath.Join(bin, "corepack"),
		"#!/bin/sh\ntrap 'exit 0' TERM INT HUP\nwhile :; do /bin/sleep 1; done\n",
	)
	t.Setenv("PATH", bin)

	if code := run(); code != 7 {
		t.Fatalf("run exit code = %d, want 7", code)
	}
}

func TestRunTreatsAnUnexpectedSuccessfulChildExitAsFailure(t *testing.T) {
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "go"), "#!/bin/sh\nexit 0\n")
	writeExecutable(
		t,
		filepath.Join(bin, "corepack"),
		"#!/bin/sh\ntrap 'exit 0' TERM INT HUP\nwhile :; do /bin/sleep 1; done\n",
	)
	t.Setenv("PATH", bin)

	if code := run(); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
}

func TestRunReportsChildStartFailure(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if code := run(); code != 1 {
		t.Fatalf("run exit code = %d, want 1", code)
	}
}

func TestStopChildrenIgnoresCommandsThatNeverStarted(t *testing.T) {
	command := exec.Command("/bin/sleep", "30")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	stopChildren(
		map[string]*exec.Cmd{
			"started": command,
			"pending": exec.Command("/bin/false"),
		},
		syscall.SIGTERM,
	)
	if err := command.Wait(); err == nil {
		t.Fatal("stopped child exited successfully")
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
