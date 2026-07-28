# Autoboard Linux Atomic Skill Swap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add fail-closed, atomic Codex skill directory exchange support on Linux so the existing refresh and rollback transactions are ready for a future NixOS host.

**Architecture:** Keep the existing Darwin `renameatx_np(RENAME_SWAP)` adapters and add Linux build-tagged adapters that call `renameat2(RENAME_EXCHANGE)` in both swap consumers. Linux-only tests exercise the real filesystem syscall; macOS verification cross-compiles those tests and retains the existing injected transaction-failure coverage.

**Tech Stack:** Go 1.25, `golang.org/x/sys/unix`, Darwin and Linux build constraints, Docker Desktop for local Linux test execution, Markdown documentation.

## Global Constraints

- Darwin keeps its existing `renameatx_np(RENAME_SWAP)` implementation.
- Linux must use `unix.Renameat2(unix.AT_FDCWD, first, unix.AT_FDCWD, second, unix.RENAME_EXCHANGE)`.
- Other operating systems must fail closed before changing either directory.
- Unsupported Linux kernels or filesystems must return the syscall error; never fall back to a non-atomic rename or copy sequence.
- Both swap consumers must stay synchronized: `internal/installation` handles refresh and `cmd/autoboard` handles rollback.
- The current LaunchAgent service lifecycle remains macOS-only; this work prepares only the filesystem transaction for a future NixOS lifecycle.
- After Go changes, run `gofmt`, relevant Go tests, and `just lint-go`.
- Go coverage must remain at least 80% overall and at least 70% in every first-party package.
- After Markdown changes, run `corepack pnpm format:prettier`.
- Before each commit, run `just pre-commit`; before handoff, run `corepack pnpm format:check`, relevant tests, and `just verify`.

---

### Task 1: Add Linux atomic exchange for installed-skill refresh

**Files:**

- Create: `internal/installation/skill_swap_linux.go`
- Create: `internal/installation/skill_swap_linux_test.go`
- Modify: `internal/installation/skill_swap_other.go`

**Interfaces:**

- Consumes: `SkillManager.Ensure()` through the existing `atomicSwapSkillDirectories(first string, second string) error` adapter.
- Produces: a Linux implementation of `atomicSwapSkillDirectories(first string, second string) error`; non-Darwin/non-Linux builds retain a fail-closed implementation.

- [ ] **Step 1: Write the Linux filesystem behavior test**

Create `internal/installation/skill_swap_linux_test.go` with the `linux` build constraint. The test must create sibling `first` and `second` directories, write distinct marker files, call `atomicSwapSkillDirectories(first, second)`, and assert that both directory entries exchanged their complete contents:

```go
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
```

- [ ] **Step 2: Run the Linux test to verify it fails**

Ensure Docker Desktop is running, then run:

```bash
docker run --rm \
  --mount type=bind,src="$PWD",dst=/workspace \
  --workdir /workspace \
  golang:1.25 \
  go test ./internal/installation -run TestAtomicSwapSkillDirectoriesExchangesDirectoryEntries -count=1
```

Expected: FAIL with `atomic skill directory exchange requires macOS`, proving the existing non-Darwin stub does not provide Linux atomic exchange.

- [ ] **Step 3: Implement the Linux adapter and narrow the unsupported adapter**

Create `internal/installation/skill_swap_linux.go`:

```go
//go:build linux

package installation

import "golang.org/x/sys/unix"

// atomicSwapSkillDirectories exchanges the staged and installed skills without
// leaving the Codex skill path absent during a refresh.
func atomicSwapSkillDirectories(first string, second string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		first,
		unix.AT_FDCWD,
		second,
		unix.RENAME_EXCHANGE,
	)
}
```

Change `internal/installation/skill_swap_other.go` to:

```go
//go:build !darwin && !linux

package installation

import "errors"

func atomicSwapSkillDirectories(string, string) error {
	return errors.New("atomic skill directory exchange requires macOS or Linux")
}
```

- [ ] **Step 4: Run Linux and macOS-focused verification**

Run:

```bash
gofmt -w internal/installation/skill_swap_linux.go internal/installation/skill_swap_linux_test.go internal/installation/skill_swap_other.go
docker run --rm \
  --mount type=bind,src="$PWD",dst=/workspace \
  --workdir /workspace \
  golang:1.25 \
  go test ./internal/installation -run 'TestAtomicSwapSkillDirectoriesExchangesDirectoryEntries|TestSkillManagerEnsure' -count=1
go test ./internal/installation -run TestSkillManagerEnsure -count=1
linux_test_dir=$(mktemp -d)
GOOS=linux GOARCH=arm64 go test -c ./internal/installation -o "$linux_test_dir/installation.test"
rm -rf "$linux_test_dir"
just lint-go
```

Expected: all tests, the Linux cross-build, and lint pass. The Linux test must use the real `renameat2(RENAME_EXCHANGE)` adapter.

- [ ] **Step 5: Run the commit gate and commit**

Temporarily stop the managed service so tests can bind port 4040, with a shell trap that restarts it:

```bash
set -e
trap 'just start-service >/dev/null' EXIT
just stop-service >/dev/null
just pre-commit
git add internal/installation/skill_swap_linux.go internal/installation/skill_swap_linux_test.go internal/installation/skill_swap_other.go
git commit -m "feat: support Linux skill refresh exchange"
```

Expected: the gate passes and the service is restarted after the commit.

### Task 2: Add Linux rollback exchange and document the platform boundary

**Files:**

- Create: `cmd/autoboard/skill_swap_linux.go`
- Create: `cmd/autoboard/skill_swap_linux_test.go`
- Modify: `cmd/autoboard/skill_swap_other.go`
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/codex-mcp-config.md`

**Interfaces:**

- Consumes: `restoreSkill()` through the existing `atomicSwapDirectories(first string, second string) error` adapter.
- Produces: a Linux implementation of `atomicSwapDirectories(first string, second string) error`, synchronized with Task 1; documentation explicitly distinguishes cross-platform atomic skill exchange from the macOS-only LaunchAgent lifecycle.

- [ ] **Step 1: Write the Linux rollback-adapter behavior test**

Create `cmd/autoboard/skill_swap_linux_test.go` with the `linux` build constraint. Use the same sibling-directory setup as Task 1, but call `atomicSwapDirectories(first, second)` and keep the assertion helper local to package `main`:

```go
//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicSwapDirectoriesExchangesDirectoryEntries(t *testing.T) {
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

	if err := atomicSwapDirectories(first, second); err != nil {
		t.Fatalf("atomic exchange: %v", err)
	}
	assertSwappedFileContent(t, filepath.Join(first, "second.txt"), "second")
	assertSwappedFileContent(t, filepath.Join(second, "first.txt"), "first")
}

func assertSwappedFileContent(t *testing.T, path string, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Errorf("%s content = %q, want %q", path, content, want)
	}
}
```

- [ ] **Step 2: Run the Linux test to verify it fails**

Run:

```bash
docker run --rm \
  --mount type=bind,src="$PWD",dst=/workspace \
  --workdir /workspace \
  golang:1.25 \
  go test ./cmd/autoboard -run TestAtomicSwapDirectoriesExchangesDirectoryEntries -count=1
```

Expected: FAIL with `atomic skill directory exchange requires macOS`.

- [ ] **Step 3: Implement the Linux rollback adapter**

Create `cmd/autoboard/skill_swap_linux.go`:

```go
//go:build linux

package main

import "golang.org/x/sys/unix"

func atomicSwapDirectories(first string, second string) error {
	return unix.Renameat2(
		unix.AT_FDCWD,
		first,
		unix.AT_FDCWD,
		second,
		unix.RENAME_EXCHANGE,
	)
}
```

Change `cmd/autoboard/skill_swap_other.go` to:

```go
//go:build !darwin && !linux

package main

import "errors"

func atomicSwapDirectories(string, string) error {
	return errors.New("atomic skill directory exchange requires macOS or Linux")
}
```

- [ ] **Step 4: Document Linux atomic exchange without claiming Linux service support**

Update:

- `README.md` to state near the macOS lifecycle documentation that the Codex skill refresh/rollback primitive supports atomic directory exchange on macOS and Linux, while install/update service commands remain macOS-only.
- `docs/architecture.md` to name Darwin `renameatx_np(RENAME_SWAP)` and Linux `renameat2(RENAME_EXCHANGE)` as the two fail-closed skill transaction implementations.
- `docs/codex-mcp-config.md` to state that atomic skill refresh is implemented for macOS and Linux, but the documented production service lifecycle currently requires macOS.

Do not describe a systemd unit, NixOS module, Linux install command, or Linux service manager.

- [ ] **Step 5: Format and run focused Linux and macOS verification**

Run:

```bash
gofmt -w cmd/autoboard/skill_swap_linux.go cmd/autoboard/skill_swap_linux_test.go cmd/autoboard/skill_swap_other.go
corepack pnpm format:prettier
docker run --rm \
  --mount type=bind,src="$PWD",dst=/workspace \
  --workdir /workspace \
  golang:1.25 \
  go test ./cmd/autoboard -run 'TestAtomicSwapDirectoriesExchangesDirectoryEntries|TestRestoreIntegrationSnapshotsRestoresPreviousArtifacts' -count=1
go test ./cmd/autoboard -run TestRestoreIntegrationSnapshotsRestoresPreviousArtifacts -count=1
linux_test_dir=$(mktemp -d)
GOOS=linux GOARCH=arm64 go test -c ./cmd/autoboard -o "$linux_test_dir/autoboard.test"
rm -rf "$linux_test_dir"
just lint-go
```

Expected: all tests, the Linux cross-build, formatting, and lint pass.

- [ ] **Step 6: Run all quality gates and commit**

Temporarily stop the managed service, restart it automatically, and run:

```bash
set -e
trap 'just start-service >/dev/null' EXIT
just stop-service >/dev/null
corepack pnpm format:check
just pre-commit
just verify
git add cmd/autoboard/skill_swap_linux.go cmd/autoboard/skill_swap_linux_test.go cmd/autoboard/skill_swap_other.go README.md docs/architecture.md docs/codex-mcp-config.md
git commit -m "feat: support Linux skill rollback exchange"
```

Expected: formatting, relevant tests, coverage, race tests, builds, E2E verification, and the commit hook all pass; the managed macOS service is restarted.
