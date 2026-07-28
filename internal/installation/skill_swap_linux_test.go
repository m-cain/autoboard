//go:build linux

package installation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicSwapSkillDirectoriesExchangesDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.Mkdir(first, 0o700); err != nil {
		t.Fatalf("create first directory: %v", err)
	}
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatalf("create second directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "first.txt"), []byte("first"), 0o600); err != nil {
		t.Fatalf("write first marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(second, "second.txt"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write second marker: %v", err)
	}

	if err := atomicSwapSkillDirectories(first, second); err != nil {
		t.Fatalf("atomic exchange: %v", err)
	}
	assertFileContent(t, filepath.Join(first, "second.txt"), "second")
	assertFileContent(t, filepath.Join(second, "first.txt"), "first")
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Errorf("%s content = %q, want %q", path, content, want)
	}
}
