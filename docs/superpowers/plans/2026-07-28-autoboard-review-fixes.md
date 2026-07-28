# Autoboard Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the two review findings by making Codex config rollback atomic and classifying non-regular skill artifacts as conflicts before reading them.

**Architecture:** Keep the fixes local to the existing transaction and skill-validation boundaries. The rollback path will stage, sync, and rename restored config content; skill path inspection will require terminal files to be regular files in both read and conflict-detection paths.

**Tech Stack:** Go 1.25, standard-library filesystem APIs, Unix-domain socket test fixture, existing Go test and Just quality gates.

## Global Constraints

- Preserve unrelated Codex TOML content and file permissions.
- A failed config rollback replacement must leave the current config content unchanged.
- Config rollback must create its temporary file in the destination directory, sync and close it, then atomically rename it over the destination.
- Skill path components must reject symlinks; intermediate components must be directories; terminal components must be regular files.
- Non-regular installed skill artifacts must report `SkillConflicting` without reading the artifact.
- Preserve the existing Darwin/Linux atomic skill-directory exchange behavior.
- Follow TDD: each production fix must be preceded by a focused test that fails for the reviewed defect.
- After Go changes, run `gofmt`, relevant Go tests, and `just lint-go`.
- Go coverage must remain at least 80% overall and at least 70% in every first-party package.
- Before each commit, run `just pre-commit`; before handoff, run `corepack pnpm format:check`, relevant tests, and `just verify`.

---

### Task 1: Make Codex config rollback atomic

**Files:**

- Modify: `cmd/autoboard/main.go`
- Modify: `cmd/autoboard/main_test.go`

**Interfaces:**

- Consumes: `restoreFile(path string, snapshot fileSnapshot) error`.
- Produces: `restoreFile` retains its signature but replaces existing snapshots through a same-directory temporary file and an injectable package-level rename function used by the focused failure test.

- [ ] **Step 1: Write the failing rollback test**

Add `TestRestoreFilePreservesCurrentConfigWhenAtomicReplaceFails` to `cmd/autoboard/main_test.go`. The test must:

1. Write `current\n` to a temporary `config.toml`.
2. Save and restore the package-level rename function with `t.Cleanup`.
3. Replace the rename function with one that returns `errors.New("simulated atomic replace failure")`.
4. Call `restoreFile` with an existing snapshot containing `previous\n` and mode `0o600`.
5. Assert the call fails, the config still contains `current\n`, and no `.autoboard-config-rollback-*` temporary entry remains.

The test must exercise real temporary-file creation, writing, syncing, closing, cleanup, and config-file observation; only the final rename failure is injected.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./cmd/autoboard -run TestRestoreFilePreservesCurrentConfigWhenAtomicReplaceFails -count=1
```

Expected: FAIL because the current `restoreFile` calls `os.WriteFile` directly, returns success, and replaces `current\n`.

- [ ] **Step 3: Implement atomic restore**

In `cmd/autoboard/main.go`, add:

```go
var restoreFileRename = os.Rename
```

For an existing snapshot, retain parent creation and replace the direct `os.WriteFile` call with a private helper:

```go
func writeRestoredFileAtomic(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".autoboard-config-rollback-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return restoreFileRename(temporaryPath, path)
}
```

Wrap helper errors with the existing `restore <path>:` context. Do not change missing-snapshot removal behavior.

- [ ] **Step 4: Verify GREEN and regression coverage**

Run:

```bash
gofmt -w cmd/autoboard/main.go cmd/autoboard/main_test.go
go test ./cmd/autoboard -run 'TestRestoreFilePreservesCurrentConfigWhenAtomicReplaceFails|TestRestoreIntegrationSnapshotsRestoresPreviousArtifacts|TestInstallRestoresCodexConfigWhenRegistrationReadBackFails' -count=1
just lint-go
```

Expected: all focused tests and lint pass with pristine output.

- [ ] **Step 5: Run the commit gate and commit**

Temporarily stop the managed service so tests can bind port 4040, with a trap that restarts it:

```bash
set -e
trap 'just start-service >/dev/null' EXIT
just stop-service >/dev/null
just pre-commit
git add cmd/autoboard/main.go cmd/autoboard/main_test.go
git commit -m "fix: restore Codex config atomically"
```

Expected: the gate and commit hook pass, and the service restarts.

### Task 2: Reject non-regular skill artifacts

**Files:**

- Modify: `internal/installation/skill.go`
- Modify: `internal/installation/skill_test.go`

**Interfaces:**

- Consumes: `SkillManager.Status()`, `readSkillFile`, and `hasUnsafeSkillPath`.
- Produces: terminal skill path components must satisfy `info.Mode().IsRegular()` in both reading and conflict detection; a socket at `agents/openai.yaml` yields `SkillConflicting` with no read attempt.

- [ ] **Step 1: Write the failing installed-skill test**

Extend `TestSkillManagerStatusReportsSkillStates` in `internal/installation/skill_test.go` with a `conflicting socket metadata` case. Import `net` and `runtime`. Its setup must:

1. Skip only on Windows because Unix-domain sockets are unavailable there.
2. Copy the valid skill source to the destination.
3. Remove `agents/openai.yaml`.
4. Listen on network `unix` at the removed file path.
5. Register listener cleanup with `t.Cleanup`.
6. Expect `SkillConflicting`.

The production change that must make this test pass is terminal-component `IsRegular` validation in `hasUnsafeSkillPath`; without it, status proceeds to `os.ReadFile` and returns an error instead of the conflict state.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./internal/installation -run 'TestSkillManagerStatusReportsSkillStates/conflicting_socket_metadata' -count=1
```

Expected: FAIL because status returns a read error for the Unix socket instead of `SkillConflicting`.

- [ ] **Step 3: Require regular terminal files**

In `readSkillFile`, change both terminal-component checks to reject anything where:

```go
!info.Mode().IsRegular()
```

In `hasUnsafeSkillPath`, change the terminal condition from `info.IsDir()` to:

```go
!info.Mode().IsRegular()
```

Keep the explicit symlink checks and intermediate-directory checks intact. Do not add platform-specific production code.

- [ ] **Step 4: Verify GREEN and regression coverage**

Run:

```bash
gofmt -w internal/installation/skill.go internal/installation/skill_test.go
go test ./internal/installation -run 'TestSkillManagerStatusReportsSkillStates|TestSkillManagerValidateRejectsInvalidSource|TestSkillManagerEnsure|TestSkillManagerRemove' -count=1
just lint-go
```

Expected: all focused tests and lint pass with pristine output.

- [ ] **Step 5: Run all quality gates and commit**

Temporarily stop the managed service, restart it automatically, and run:

```bash
set -e
trap 'just start-service >/dev/null' EXIT
just stop-service >/dev/null
corepack pnpm format:check
just pre-commit
just verify
git add internal/installation/skill.go internal/installation/skill_test.go
git commit -m "fix: reject non-regular skill files"
```

Expected: formatting, coverage, race tests, builds, E2E verification, and the commit hook all pass; the managed service restarts.
