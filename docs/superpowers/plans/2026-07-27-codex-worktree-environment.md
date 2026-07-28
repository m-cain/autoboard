# Codex Worktree Environment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tracked Codex local environment that prepares every new
Autoboard worktree with the existing lean bootstrap and exposes the two approved
development actions.

**Architecture:** Codex reads one declarative version 1 TOML file from
`.codex/environments/environment.toml`. The file delegates setup, development,
and verification to the repository's existing Just recipes, keeping lifecycle
logic in one place.

**Tech Stack:** Codex local-environment TOML, Just, Node.js test runner

## Global Constraints

- The environment name is exactly `Autoboard`.
- The setup script is exactly `just setup`.
- The only actions are `Dev` (`run`, `just dev`) and `Verify` (`test`,
  `just verify`).
- Do not add cleanup hooks, platform overrides, wrapper scripts,
  `.worktreeinclude`, or dependencies.
- Preserve unrelated working-tree changes.
- Run `corepack pnpm format:prettier` after changing JavaScript, Markdown, or
  TOML-adjacent repository files owned by Prettier.
- Before handoff, run `corepack pnpm format:check`, the relevant tests, and
  `just verify`.

---

### Task 1: Add the Codex worktree environment

**Files:**

- Create: `.codex/environments/environment.toml`
- Modify: `test/developer-workflow.test.mjs`

**Interfaces:**

- Consumes: Existing `just setup`, `just dev`, and `just verify` recipes.
- Produces: A version 1 Codex local environment selected by default for new
  managed worktrees.

- [ ] **Step 1: Write the failing environment-contract test**

Append this test to `test/developer-workflow.test.mjs`:

```js
test("Codex worktrees use the canonical local environment", async () => {
  const environment = await readFile(
    resolve(root, ".codex/environments/environment.toml"),
    "utf8",
  );

  assert.equal(
    environment,
    `version = 1
name = "Autoboard"

[setup]
script = "just setup"

[[actions]]
name = "Dev"
icon = "run"
command = "just dev"

[[actions]]
name = "Verify"
icon = "test"
command = "just verify"
`,
  );
});
```

- [ ] **Step 2: Run the contract test and verify it fails**

Run:

```sh
node --test --test-name-pattern \
  "Codex worktrees use the canonical local environment" \
  test/developer-workflow.test.mjs
```

Expected: FAIL with `ENOENT` for
`.codex/environments/environment.toml`.

- [ ] **Step 3: Add the minimal environment configuration**

Create `.codex/environments/environment.toml`:

```toml
version = 1
name = "Autoboard"

[setup]
script = "just setup"

[[actions]]
name = "Dev"
icon = "run"
command = "just dev"

[[actions]]
name = "Verify"
icon = "test"
command = "just verify"
```

- [ ] **Step 4: Run the contract test and verify it passes**

Run:

```sh
node --test --test-name-pattern \
  "Codex worktrees use the canonical local environment" \
  test/developer-workflow.test.mjs
```

Expected: one matching test passes and all non-matching tests are skipped.

- [ ] **Step 5: Validate TOML syntax and schema values**

Run:

```sh
python3 -c 'import pathlib, tomllib; p = pathlib.Path(".codex/environments/environment.toml"); d = tomllib.loads(p.read_text()); assert d == {"version": 1, "name": "Autoboard", "setup": {"script": "just setup"}, "actions": [{"name": "Dev", "icon": "run", "command": "just dev"}, {"name": "Verify", "icon": "test", "command": "just verify"}]}'
```

Expected: exit code 0 with no output.

- [ ] **Step 6: Bootstrap the fresh worktree**

Run:

```sh
just setup
```

Expected: pnpm dependencies, Git hooks, Go modules, the pinned Go linter, and
Playwright Chromium are available with exit code 0.

- [ ] **Step 7: Format and run the focused workflow tests**

Run:

```sh
corepack pnpm format:prettier
node --test test/developer-workflow.test.mjs
corepack pnpm format:check
```

Expected: formatting is unchanged after the write, seven workflow tests pass,
and the format check exits 0.

- [ ] **Step 8: Run the complete handoff gate**

Temporarily stop the installed Autoboard service so its port does not conflict
with lifecycle tests, use an exit trap to restore it, and run:

```sh
set -e
just stop-service
trap 'just start-service' EXIT
just verify
```

Expected: `just verify` exits 0, including coverage, race tests, production
build, and the Playwright black-box scenario; the exit trap restarts Autoboard.

- [ ] **Step 9: Confirm the service and diff**

Run:

```sh
just service-status
git diff --check
git status --short
```

Expected: Autoboard reports `health: ok`, the diff check exits 0, and only the
environment file, workflow test, and this implementation plan are in scope.

- [ ] **Step 10: Commit the implementation**

Run the commit with the installed service temporarily stopped so the Husky
pre-commit gate can bind its lifecycle-test port:

```sh
set -e
git add .codex/environments/environment.toml \
  test/developer-workflow.test.mjs
just stop-service
trap 'just start-service' EXIT
git commit -m "chore: configure Codex worktree environment"
```

Expected: the commit succeeds without bypassing hooks and Autoboard restarts.
