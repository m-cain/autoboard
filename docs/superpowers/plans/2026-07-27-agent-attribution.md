# Agent Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox syntax for tracking.

**Goal:** Distinguish future manual, explicitly delegated Codex, and
agent-initiated Autoboard writes across persistence, MCP, generated contracts,
and the read-only browser.

**Architecture:** Replace the overloaded actor string with a validated
two-principal `Attribution`. MCP callers choose only the initiator while the
daemon fixes the performer to Codex; the application persists attribution on
entities and activity, and generated read contracts carry it to the browser.

**Tech Stack:** Go 1.25, SQLite/Goose, official Go MCP SDK, JSON Schema,
TypeScript, React, Vitest, Playwright, Just.

## Global Constraints

- Valid attribution pairs are exactly `me/me`, `codex/me`, `codex/codex`, and
  `system/system`.
- Projects and tickets expose immutable `created_attribution`; comments,
  attachments, and activity expose `attribution`.
- All eleven MCP write tools require `initiated_by: "me" | "codex"` with no
  default; MCP always records `performed_by: "codex"`.
- `initiated_by: "me"` means the human explicitly requested that exact
  Autoboard mutation. Agent-selected operations use `codex`; subagents preserve
  `me` only when their handoff carries the exact human request.
- The six read tools, ticket assignees, actionable-ticket rules, and read-only
  browser boundary remain unchanged.
- The unused installed database has been reset. Modify the initial migration
  directly; do not add compatibility migrations, legacy values, or backfills.
- Independent system activity uses `system/system`; derived events inherit the
  triggering operation's attribution.
- Follow strict red-green-refactor TDD. Tests must assert observable behavior,
  not source text or mocks.
- After Go changes run `gofmt`, relevant Go tests, and `just lint-go`. After
  Prettier-owned changes run `corepack pnpm format:prettier`.
- Preserve at least 80% overall and 70% per-package Go coverage, and the
  repository's independent TypeScript coverage thresholds.
- Before commits run `just pre-commit`; before handoff run
  `corepack pnpm format:check`, relevant tests, and `just verify`.

---

### Task 1: Validated attribution value object

**Files:**

- Create: `internal/app/attribution.go`
- Create: `internal/app/attribution_test.go`

**Interfaces:**

- Produce:

  ```go
  type Principal string

  const (
      PrincipalMe     Principal = "me"
      PrincipalCodex  Principal = "codex"
      PrincipalSystem Principal = "system"
  )

  type Attribution struct {
      PerformedBy Principal `json:"performed_by"`
      InitiatedBy Principal `json:"initiated_by"`
  }

  func (attribution Attribution) Validate() error
  ```

- `Validate` accepts exactly the four pairs in Global Constraints and returns a
  validation-domain error for empty, unknown, or disallowed pairs.

- [x] **Step 1: Write failing value-object tests**

  Add a table-driven test proving the four valid pairs succeed and every other
  combination of `me`, `codex`, `system`, empty, and an unknown principal
  fails. Assert the returned error is an application validation error.

- [x] **Step 2: Run focused tests and verify RED**

  Run:

  ```bash
  go test ./internal/app
  ```

  Expected: compilation failures caused by the missing attribution API.

- [x] **Step 3: Implement the minimal value object**

  Add the principals, the attribution struct with schema enums, and validation.
  Do not change mutation signatures, persistence, MCP, or browser contracts in
  this task.

- [x] **Step 4: Format and verify GREEN**

  Run:

  ```bash
  gofmt -w internal/app/attribution.go internal/app/attribution_test.go
  go test ./internal/app
  just lint-go
  just pre-commit
  ```

  Expected: all commands pass with coverage thresholds unchanged.

- [x] **Step 5: Commit**

  ```bash
  git add internal/app/attribution.go internal/app/attribution_test.go
  git commit -m "feat: add attribution value object"
  ```

### Task 2: Persisted attribution and required MCP initiator

**Files:**

- Modify: `internal/app/app.go`
- Modify: `internal/app/projects.go`
- Modify: `internal/app/tickets.go`
- Modify: `internal/app/comments.go`
- Modify: `internal/app/attachments.go`
- Modify: `internal/app/read_model.go`
- Modify: `internal/store/migrations/00001_initial.sql`
- Modify: `internal/mcpapi/types.go`
- Modify: `internal/mcpapi/server.go`
- Modify: `packages/contracts/generated-go/*.schema.json`
- Modify: `packages/contracts/src/generated/browser-schemas.ts`
- Modify: `packages/contracts/test/browser-generated.test.ts`
- Modify: existing browser test fixtures under `web/src/`
- Modify: `test/e2e/autoboard.test.ts`
- Modify: relevant E2E helpers if required
- Test: `internal/app/*_test.go`
- Test: `internal/store/store_test.go`
- Test: `internal/mcpapi/server_test.go`
- Test: `internal/mcpapi/server_internal_test.go`

**Interfaces:**

- Consume the Task 1 attribution value object.
- Add `CreatedAttribution Attribution` with JSON name
  `created_attribution` to `Project` and `Ticket`.
- Replace `Actor` on `Comment`, `Attachment`, and `ActivityEvent` with
  `Attribution Attribution` using JSON name `attribution`.
- Every public service mutation takes `attribution Attribution` immediately
  after `context.Context`; reads remain unchanged.
- `insertActivity` takes attribution explicitly, and every derived activity
  call receives the triggering attribution.
- Projects and tickets store `created_performed_by` and
  `created_initiated_by`. Comments, attachments, and activity store
  `performed_by` and `initiated_by`.
- Add required `initiated_by` to every write input shape. Shared input structs
  such as project revision and dependency inputs need the field only once.
- Each write handler constructs:

  ```go
  app.Attribution{
      PerformedBy: app.PrincipalCodex,
      InitiatedBy: app.Principal(input.InitiatedBy),
  }
  ```

- Write schemas describe the exact-user-operation classification boundary and
  allow only `me` or `codex`. Read schemas do not gain this field.
- Generated browser contracts require the new output fields. Update existing
  browser fixtures so the full repository remains green, but do not add
  attribution presentation in this task.

- [x] **Step 1: Write failing domain, migration, MCP, and contract tests**

  Prove project/ticket creation attribution survives reads, comments and
  attachments retain attribution, every mutation event retains attribution,
  and dependency side-effect activity inherits it. Assert the initial schema
  constraints through real inserts, not source inspection. Test that all eleven
  write tools require `initiated_by`, reject missing, unknown, `system`, and
  extra values without mutation, while all six read tools remain unchanged.
  Exercise delegated and autonomous writes and assert persisted values. Add
  decoder fixtures that reject missing or invalid output attribution. Extend
  E2E inputs and assertions through MCP, GET/SSE, restart, and re-read. Assert
  server instructions explain the exact-operation and subagent rules.

- [x] **Step 2: Run focused tests and verify RED**

  Run:

  ```bash
  go test ./internal/app ./internal/store ./internal/mcpapi
  corepack pnpm --filter @autoboard/contracts test
  just test-e2e
  ```

  Expected: compilation or behavior failures because persistence, MCP, and
  generated contracts lack attribution.

- [x] **Step 3: Implement the vertical attribution boundary**

  Validate attribution before starting a transaction. Thread it through every
  mutation and activity insert, update schema and row scans, add the required
  MCP fields and descriptions, and pass Codex-performed attribution to every
  service mutation. Do not expose `performed_by` as caller input. Regenerate
  browser contracts, update existing complete fixtures without rendering the
  new data yet, and satisfy the persistence-focused E2E assertions.

- [x] **Step 4: Format and verify GREEN**

  Run:

  ```bash
  gofmt -w internal/app/*.go internal/store/*.go internal/mcpapi/*.go
  go generate ./internal/contracts
  corepack pnpm format:prettier
  go test ./internal/app ./internal/store ./internal/mcpapi
  corepack pnpm --filter @autoboard/contracts test
  corepack pnpm --filter @autoboard/web test
  just test-e2e
  just lint-go
  just pre-commit
  ```

  Expected: all domain, MCP, generated-contract, browser fixture, and
  repository commit gates pass.

- [x] **Step 5: Commit**

  ```bash
  git add internal packages/contracts web test/e2e
  git commit -m "feat: persist attributed MCP writes"
  ```

### Task 3: Attribution UI

**Files:**

- Create: `web/src/components/AttributionLabel.tsx`
- Create: `web/src/components/AttributionLabel.test.tsx`
- Modify: project, board, ticket, activity, and attachment presentation under
  `web/src/`
- Modify: `web/src/styles.css`
- Modify: `test/e2e/autoboard.test.ts`
- Test: affected `web/src/**/*.test.tsx`

**Interfaces:**

- Consume Task 2's generated `created_attribution` on projects/tickets and
  `attribution` on comments, attachments, and activity.
- Produce one shared component that renders:
  - `me/me` as `me`
  - `codex/me` as `me via Codex`
  - `codex/codex` as `Codex`
  - `system/system` as `system`
- Render project creation attribution in project rows and board headers; ticket
  creation attribution on cards and ticket details; operation attribution in
  comments, attachment metadata, and activity.

- [x] **Step 1: Write failing UI tests**

  Add component and page tests for all four labels and every required surface,
  using real components rather than attribution mocks. Extend the browser E2E
  assertions to require both `me via Codex` and `Codex`.

- [x] **Step 2: Run focused tests and verify RED**

  Run:

  ```bash
  corepack pnpm --filter @autoboard/web test
  just test-e2e
  ```

  Expected: failures because the UI does not present the available fields.

- [x] **Step 3: Implement the shared presentation**

  Add the shared attribution presentation and compact metadata styling to every
  required read-only surface. Do not introduce mutation controls.

- [x] **Step 4: Format and verify GREEN**

  Run:

  ```bash
  corepack pnpm format:prettier
  corepack pnpm --filter @autoboard/web test
  just test-e2e
  corepack pnpm format:check
  just pre-commit
  ```

  Expected: generated-contract freshness, TypeScript coverage, and UI tests
  pass.

- [x] **Step 5: Commit**

  ```bash
  git add web test/e2e
  git commit -m "feat: show write attribution in the browser"
  ```

### Task 4: Documentation and final repository verification

**Files:**

- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/codex-mcp-config.md`
- Review/update: `docs/superpowers/specs/2026-07-27-agent-attribution-design.md`
- Review/update: `docs/superpowers/plans/2026-07-27-agent-attribution.md`

**Interfaces:**

- Tasks 2 and 3 already provide the black-box MCP, GET/SSE, restart, and browser
  acceptance flow.
- Documentation explains the two-axis model, required MCP input, exact
  classification rule, and future manual pair without suggesting the browser
  can write.

- [x] **Step 1: Update user and architecture documentation**

  Update README, architecture, and Codex configuration guidance with the
  approved semantics and unchanged read-only boundary. Review the design and
  plan documents against the implemented public names and correct any drift.

- [x] **Step 2: Run documentation formatting**

  Run:

  ```bash
  corepack pnpm format:prettier
  corepack pnpm format:check
  ```

  Expected: all Prettier-owned documentation and source remain canonical.

- [x] **Step 3: Run targeted cross-surface tests**

  Run:

  ```bash
  go test ./internal/app ./internal/store ./internal/mcpapi
  corepack pnpm --filter @autoboard/contracts test
  corepack pnpm --filter @autoboard/web test
  just test-e2e
  ```

  Expected: domain, persistence, MCP, contracts, browser, restart, and
  black-box attribution behavior pass.

- [x] **Step 4: Format and run full verification**

  Run:

  ```bash
  gofmt -w internal/app/*.go internal/store/*.go internal/mcpapi/*.go
  corepack pnpm format:prettier
  corepack pnpm format:check
  just verify
  ```

  Expected: formatting, contract freshness, lint, all coverage policies, race
  tests, production builds, and black-box E2E pass.

- [x] **Step 5: Commit**

  ```bash
  git add README.md docs test packages/contracts web internal
  git commit -m "test: verify agent attribution end to end"
  ```

## Final live acceptance

After task reviews and the whole-branch review are clean:

1. Run `just verify` again on the final reviewed tree.
2. Run `just install` to install and start the verified binary.
3. Run `just service-status` and read `http://127.0.0.1:4040/health`.
4. Confirm schema version `1`, activity high-water `0`, no projects, the
   LaunchAgent loaded, and the exact MCP registration healthy.
5. Do not create production smoke records merely to test attribution; the
   isolated E2E flow is the write-path acceptance evidence.
