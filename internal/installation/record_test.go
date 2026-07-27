package installation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallationRecordPreservesCheckoutAndBinaryFingerprints(t *testing.T) {
	checkout := t.TempDir()
	for _, arguments := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		runTestGit(t, checkout, arguments...)
	}
	if err := os.WriteFile(
		filepath.Join(checkout, "go.mod"),
		[]byte("module example.test/autoboard\n"),
		0o644,
	); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	runTestGit(t, checkout, "add", "go.mod")
	runTestGit(t, checkout, "commit", "-m", "initial")
	executable := filepath.Join(t.TempDir(), "autoboard")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "outer.git"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	record, err := NewRecord(
		context.Background(),
		checkout,
		executable,
		"1.2.3",
	)
	if err != nil {
		t.Fatalf("new record: %v", err)
	}
	path := filepath.Join(t.TempDir(), "install.json")
	if err := WriteRecord(path, record); err != nil {
		t.Fatalf("write record: %v", err)
	}
	read, err := ReadRecord(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if read.Checkout != checkout ||
		read.CheckoutRevision == "" ||
		read.BinarySHA256 == "" ||
		read.Version != "1.2.3" {
		t.Errorf("record = %#v", read)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat record: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record mode = %o, want 600", info.Mode().Perm())
	}
}

func runTestGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = withoutTestGitRepositoryEnvironment(os.Environ())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func withoutTestGitRepositoryEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, variable := range environment {
		name, _, _ := strings.Cut(variable, "=")
		switch name {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
			"GIT_CONFIG",
			"GIT_CONFIG_PARAMETERS",
			"GIT_CONFIG_COUNT",
			"GIT_OBJECT_DIRECTORY",
			"GIT_DIR",
			"GIT_WORK_TREE",
			"GIT_IMPLICIT_WORK_TREE",
			"GIT_GRAFT_FILE",
			"GIT_INDEX_FILE",
			"GIT_NO_REPLACE_OBJECTS",
			"GIT_REPLACE_REF_BASE",
			"GIT_PREFIX",
			"GIT_SHALLOW_FILE",
			"GIT_COMMON_DIR":
			continue
		default:
			clean = append(clean, variable)
		}
	}
	return clean
}
