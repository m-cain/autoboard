# Autoboard Codex Skill Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `$autoboard create ticket in …` a first-class Codex invocation
while preserving ordinary implicit Autoboard activation.

**Architecture:** Add one thin, repo-owned Autoboard skill and extend the
existing macOS installer to copy it into the personal Codex skill directory.
The existing Streamable HTTP MCP server remains the sole live-data and mutation
surface; installation, status, update, and uninstall manage the MCP
registration and skill as one owned integration.

**Tech Stack:** Go 1.25, Codex skills (`SKILL.md` and `agents/openai.yaml`),
Streamable HTTP MCP, macOS LaunchAgent lifecycle, Markdown/YAML documentation.

## Global Constraints

- The browser remains strictly read-only; all mutations use the existing 17
  semantic Autoboard MCP tools.
- Keep the skill thin. Do not duplicate tool schemas, revision rules, or
  workflow policy already supplied by the MCP server.
- Support explicit `$autoboard` invocation and implicit activation.
- Install a copied user-wide skill at `$HOME/.agents/skills/autoboard`; do not
  use a checkout symlink, custom prompt, per-action skills, or plugin.
- The canonical MCP dependency is named `autoboard`, uses Streamable HTTP, and
  has URL `http://127.0.0.1:4040/mcp`.
- Preserve unrelated Codex configuration and skill content. Refuse conflicts,
  use exact ownership markers, and remove only owned artifacts.
- `CODEX_HOME` continues to control Codex configuration only; the personal
  skill path is always rooted at the user's home directory.
- Do not add or change HTTP routes, database schema, MCP tools, MCP schemas,
  approval mode, or browser behavior.
- After changing Prettier-owned files, run
  `corepack pnpm format:prettier`. After changing Go, run `gofmt`, relevant Go
  tests, and `just lint-go`.
- Go coverage remains at least 80% overall and 70% in every first-party
  package. TypeScript coverage remains at least 80% for lines, statements, and
  functions and 75% for branches in each measured workspace.

---

### Task 1: Add the repo-owned skill and safe skill manager

**Files:**

- Create: `.agents/skills/autoboard/SKILL.md`
- Create: `.agents/skills/autoboard/agents/openai.yaml`
- Create: `internal/installation/skill.go`
- Create: `internal/installation/skill_test.go`

**Interfaces:**

- `SkillManager` consumes a source directory and installed destination
  directory.
- `SkillManager.Status()` returns one of `missing`, `current`, `outdated`, or
  `conflicting`.
- `SkillManager.Validate()` rejects a missing/invalid source and any
  conflicting destination without mutating either path.
- `SkillManager.Ensure()` atomically creates or refreshes a marker-owned copied
  skill and returns whether the destination was newly created.
- `SkillManager.Remove()` removes only a marker-owned skill; absence is
  idempotent and conflicts are refused.

- [ ] **Step 1: Write failing tests for source validation and skill states**

Cover valid source metadata, missing required files, wrong skill name, wrong MCP
dependency, missing destination, exact current content, marker-owned outdated
content, and conflicting files/directories/symlinks.

- [ ] **Step 2: Run the focused tests and confirm they fail**

Run: `go test ./internal/installation -run 'Skill'`

Expected: FAIL because `SkillManager` and the skill source do not exist.

- [ ] **Step 3: Add the thin Autoboard skill**

`SKILL.md` must use:

```markdown
---
name: autoboard
description: Use Autoboard to inspect and manage the local project board.
---

<!-- autoboard.codex-integration.v1 -->
```

Its body must direct Codex to read relevant projects/tickets before targeted
updates, keep browser use read-only, use Autoboard MCP for changes, and follow
the MCP server's safety, confirmation, and revision instructions.

`agents/openai.yaml` must declare:

```yaml
interface:
  display_name: "Autoboard"
  short_description: "Inspect and manage the local Autoboard project board"

policy:
  allow_implicit_invocation: true

dependencies:
  tools:
    - type: "mcp"
      value: "autoboard"
      description: "Autoboard local project-board tools"
      transport: "streamable_http"
      url: "http://127.0.0.1:4040/mcp"
```

- [ ] **Step 4: Implement ownership-safe copied skill management**

Use an exact standalone `<!-- autoboard.codex-integration.v1 -->` line for
ownership. Compare the two canonical files byte-for-byte to distinguish
`current` from `outdated`. Create parent directories with owner-only
permissions, stage a complete temporary sibling directory, write files with
`0644`, and rename it into place. Refuse destination symlinks and any
destination without the exact marker. Preserve or restore the previous owned
directory if replacement fails.

- [ ] **Step 5: Add removal and idempotency tests**

Cover initial install, repeated ensure, owned refresh, atomic replacement
failure, absent removal, owned removal, and conflicting removal.

- [ ] **Step 6: Format and run focused verification**

Run:

```bash
gofmt -w internal/installation/skill.go internal/installation/skill_test.go
corepack pnpm format:prettier
go test ./internal/installation
just lint-go
```

- [ ] **Step 7: Commit**

```bash
git add .agents/skills/autoboard internal/installation/skill.go internal/installation/skill_test.go
git commit -m "feat: add Autoboard Codex skill"
```

### Task 2: Integrate the skill with lifecycle, status, and documentation

**Files:**

- Modify: `internal/installation/codex.go`
- Modify: `internal/installation/codex_test.go`
- Modify: `cmd/autoboard/main.go`
- Modify: `cmd/autoboard/main_test.go`
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/codex-mcp-config.md`

**Interfaces:**

- `CodexManager` continues to own exact-table MCP configuration and gains the
  installed skill path.
- Install/update obtain the source skill from
  `<recorded-checkout>/.agents/skills/autoboard`.
- Status retains `codex: <url>` and adds
  `codex_skill: current (<absolute-path>)`.
- Missing, outdated, or conflicting skill state is reported explicitly and
  makes status fail.
- No new CLI or Just commands are added.

- [ ] **Step 1: Write failing lifecycle and status tests**

Extend install → status → update → uninstall coverage to require the copied
skill, exact MCP dependency metadata, `codex_skill: current`, update of stale
owned content, and owned removal. Add failure cases proving preflight refuses a
conflicting skill before changing the LaunchAgent or Codex config.

- [ ] **Step 2: Run focused tests and confirm they fail**

Run:

```bash
go test ./cmd/autoboard ./internal/installation
```

Expected: FAIL because lifecycle code does not yet manage the skill.

- [ ] **Step 3: Integrate skill preflight, ensure, status, and removal**

Construct the installed skill destination from the user's home, independent of
`CODEX_HOME`. Preflight MCP and skill conflicts before installing the runtime.
After endpoint verification, ensure both integration artifacts and then write
the installation record. On any later failure, restore the prior MCP and skill
states. Uninstall must attempt LaunchAgent cleanup, MCP removal, and skill
removal independently and join errors.

`start`, `stop`, and `restart` must not add, remove, or rewrite the skill.

- [ ] **Step 4: Update operational and architecture documentation**

Document that `just install` and `just update-service` install/update both the
MCP registration and personal skill. Show the canonical prompt:

```text
$autoboard create ticket in …
```

State that ordinary Autoboard phrasing still activates the skill implicitly,
that a new Codex task is required after installation/update, and that restarting
Codex is the fallback if discovery does not refresh. Update the architecture
boundary for `internal/installation`.

- [ ] **Step 5: Format and run focused verification**

Run:

```bash
gofmt -w internal/installation/codex.go internal/installation/codex_test.go cmd/autoboard/main.go cmd/autoboard/main_test.go
corepack pnpm format:prettier
go test ./cmd/autoboard ./internal/installation
just lint-go
just coverage-go
```

- [ ] **Step 6: Run repository gates**

Run:

```bash
just pre-commit
corepack pnpm format:check
just verify
```

- [ ] **Step 7: Commit**

```bash
git add internal/installation/codex.go internal/installation/codex_test.go cmd/autoboard/main.go cmd/autoboard/main_test.go README.md docs/architecture.md docs/codex-mcp-config.md
git commit -m "feat: install Autoboard skill with service"
```

### Task 3: Review and live activation readiness

**Files:**

- Review all files changed by Tasks 1–2.
- Do not mutate the real board during acceptance.

**Interfaces:**

- The stable checkout is `/Users/matt/Documents/autoboard`.
- The managed worktree must not replace the recorded stable checkout as the
  installed update source.

- [ ] **Step 1: Confirm repository cleanliness and full verification evidence**

Run:

```bash
git status --short
git diff --check
just verify
```

- [ ] **Step 2: Confirm activation procedure**

After the implementation is integrated into the stable checkout, the activation
command is `just update-service` from `/Users/matt/Documents/autoboard`. It must
start the currently stopped LaunchAgent and synchronize the skill without
recording this temporary worktree.

- [ ] **Step 3: Define live acceptance**

`just service-status` must report a loaded service, healthy endpoint, 17 MCP
tools, the canonical MCP URL, and `codex_skill: current`. In a new Codex task,
`$autoboard` must be discoverable and complete a read-only board query. Ticket
creation remains covered against the isolated E2E database so no real-board
test artifact is created.
