# Autoboard Linux Atomic Skill Swap Design

## Summary

Autoboard will support atomic Codex skill directory exchanges on Linux, making
the existing copied-skill refresh and rollback primitives ready for a future
NixOS host. Linux will use `renameat2` with `RENAME_EXCHANGE`, the direct
equivalent of Darwin's `renameatx_np` with `RENAME_SWAP`.

This change prepares the filesystem transaction only. The current service
installer and LaunchAgent lifecycle remain macOS-only until a separate NixOS
service-lifecycle design is approved.

## Platform Boundary

- Darwin keeps its existing `renameatx_np(RENAME_SWAP)` implementation.
- Linux adds `renameat2(RENAME_EXCHANGE)` implementations for both skill
  refresh and integration rollback.
- Other operating systems continue to fail closed before changing the
  destination.
- A Linux filesystem or kernel that does not support `RENAME_EXCHANGE` returns
  an error. Autoboard must preserve the previous skill and must not fall back
  to a non-atomic rename or copy sequence.

The two existing swap consumers remain synchronized:

- `internal/installation` exchanges a staged skill with an outdated installed
  skill.
- `cmd/autoboard` exchanges the installed skill with a transaction snapshot
  during rollback.

## Implementation Shape

Each consumer gains a Linux build-tagged adapter that calls
`unix.Renameat2(unix.AT_FDCWD, first, unix.AT_FDCWD, second,
unix.RENAME_EXCHANGE)`.

The existing non-Darwin adapter becomes an unsupported-platform adapter built
only when neither Darwin nor Linux applies. Its error names macOS and Linux as
the supported atomic-exchange platforms.

No skill instructions, metadata, MCP tools, HTTP APIs, database schema,
approval behavior, or browser behavior change.

## Failure Semantics

- Both directories must be sibling entries on the same filesystem, as they are
  in the existing staging and snapshot designs.
- A successful call exchanges the complete directory entries atomically; the
  caller cleans up the displaced directory at its existing temporary path.
- A failed call leaves both directory entries unchanged and returns an error.
- Unsupported kernels or filesystems fail the installation/update transaction
  without exposing a missing or partially copied skill.

## Verification

- Add Linux-only tests that create two temporary directories, invoke the real
  Linux adapter, and verify their contents exchange atomically.
- Preserve the existing injected failure tests that prove refresh and rollback
  leave the prior destination intact.
- Cross-compile the affected Go test packages for Linux from macOS so both
  Linux build-tagged adapters remain buildable before a NixOS runner exists.
- Run macOS focused tests, Go lint and coverage, `just pre-commit`, and
  `just verify`.
- Document that atomic skill exchange supports macOS and Linux while the
  production service lifecycle remains macOS-only.
