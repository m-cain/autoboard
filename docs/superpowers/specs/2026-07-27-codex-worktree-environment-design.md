# Codex Worktree Environment Design

## Goal

Make Codex-managed worktrees for Autoboard ready for development automatically
while keeping setup fast, repository-owned, and consistent with the existing
developer workflow.

## Configuration

Add the default local environment at
`.codex/environments/environment.toml`. Codex will discover this tracked file
when Autoboard is opened as a project and select it as the default environment.

The environment will be named `Autoboard` and use version 1 of the local
environment schema.

## Setup

The setup script will run:

```sh
just setup
```

This existing idempotent recipe installs the pinned JavaScript dependencies and
Git hooks, downloads Go modules, installs the repository-pinned Go linter, and
installs Playwright Chromium. Builds remain demand-driven through the existing
Just recipes.

No platform-specific override or wrapper script is needed because `just setup`
already owns the cross-platform bootstrap contract.

## Actions

The environment will expose two focused actions in Codex:

- `Dev` runs `just dev` with the `run` icon.
- `Verify` runs `just verify` with the `test` icon.

These cover the common interactive and handoff workflows without duplicating
the full Just command surface.

## Cleanup and Local Files

The environment will not define a cleanup script. The setup recipe does not
create shared external resources that Codex must remove when deleting a
worktree.

The change will not add `.worktreeinclude`. Autoboard does not require ignored
credentials or local configuration to bootstrap a fresh worktree; dependencies,
generated output, and development data can be recreated in each checkout.

## Failure Behavior

If setup fails, Codex will surface the command output and keep the worktree
available for diagnosis. The environment will not hide errors or continue with
a partial custom bootstrap.

## Verification

Verification will confirm:

1. The environment TOML parses with the current Codex local-environment schema.
2. The setup and action commands map exactly to existing Just recipes.
3. The setup recipe succeeds from a clean worktree.
4. Repository formatting and the complete `just verify` handoff gate pass.
