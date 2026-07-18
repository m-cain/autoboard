# Autoboard v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local, single-user project planner whose canonical Elixir/PostgreSQL state is mutated through a TypeScript MCP adapter and visualized through a strictly read-only React UI.

**Architecture:** An Elixir OTP application owns domain rules, Ecto persistence, attachment storage, a private framed JSON-RPC Unix socket, and a read-only Plug/Bandit HTTP surface. Codex launches a Node MCP server over stdio; the adapter authenticates to Elixir over the Unix socket. React consumes typed read endpoints and SSE invalidation events using Effect.

**Tech Stack:** Elixir 1.19 / OTP 28, Ecto SQL 3.14, Postgrex 0.22, PostgreSQL 17, Plug 1.20, Bandit 1.12, Node 24, pnpm 11, TypeScript 7, Effect 3, React 19, React Router 8, Vite 8, MCP TypeScript SDK 1, Zod 4, Vitest 4, Playwright 1.61.

## Global Constraints

- Do not add Phoenix or an HTTP mutation route.
- The browser is read-only: no forms, editable fields, drag-and-drop, create buttons, action menus, or mutation shortcuts.
- V1 is local-only, single-user, loopback-only, and supports global credentials only.
- The only assignees are `unassigned`, `me`, and `codex`; `me` is never actionable for Codex.
- Ticket statuses are `triage`, `backlog`, `ready`, `in_progress`, `done`, and `canceled`.
- Priorities are `none`, `low`, `medium`, `high`, and `urgent`.
- Subtasks are one level deep; dependencies are acyclic and same-project.
- Comments and activity are append-only; projects archive/restore and tickets cancel instead of hard deletion.
- Archived projects are browseable but reject every mutation except restoration.
- Attachments are local files, copied into managed storage, with a default 50 MB limit.
- Every mutation updates current state and appends activity in one Ecto transaction.
- Project and ticket mutations use integer revisions and reject stale `expected_revision` values.
- MCP writes apply directly; Codex approval behavior remains a host configuration concern.
- Keep `.superpowers/` ignored and do not commit visual-companion artifacts.

## Repository Map

- `server/lib/autoboard/`: focused domain contexts, Ecto schemas, authorization, event broadcasting, attachment storage, and RPC implementation.
- `server/lib/autoboard_web/`: read-only Plug router, serializers, SSE, and static-SPA delivery.
- `server/priv/repo/migrations/`: PostgreSQL schema.
- `server/test/`: ExUnit domain, persistence, RPC, HTTP, and contract tests.
- `packages/contracts/src/`: Effect schemas, encoded transport types, JSON Schema generator, and fixtures shared by TypeScript packages.
- `mcp/src/`: Unix-socket RPC client, MCP tool registration, tool metadata, result/error mapping, and stdio entrypoint.
- `web/src/`: Effect API service, router loaders, read-only board/detail components, SSE revalidation, and styles.
- `test/e2e/`: black-box MCP-to-browser acceptance test.

---

### Task 1: Scaffold the Monorepo and Runtime Baseline

**Files:**

- Create: `server/` with `mix new server --sup --module Autoboard --app autoboard`
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `compose.yaml`
- Modify: `.gitignore`
- Create: `mcp/package.json`, `mcp/tsconfig.json`, `mcp/src/main.ts`
- Create: `packages/contracts/package.json`, `packages/contracts/tsconfig.json`, `packages/contracts/src/index.ts`
- Create: `web/` with the Vite React TypeScript template
- Modify: `server/mix.exs`, `server/config/config.exs`, `server/config/runtime.exs`, `server/config/test.exs`
- Test: `server/test/autoboard/application_test.exs`

**Interfaces:**

- Produces: a supervised `Autoboard.Repo`, root pnpm scripts, a PostgreSQL dev service, and buildable `contracts`, `mcp`, and `web` packages.
- Produces configuration keys: `:database_url`, `:http_ip`, `:http_port`, `:socket_path`, `:data_dir`, and `:max_attachment_bytes`.

- [ ] **Step 1: Generate the language workspaces**

Run:

```bash
mix new server --sup --module Autoboard --app autoboard
corepack pnpm create vite web --template react-ts
mkdir -p mcp/src packages/contracts/src test/e2e
```

Expected: Mix and Vite create clean projects; the three TypeScript workspace directories exist.

- [ ] **Step 2: Define root workspace metadata**

Create `pnpm-workspace.yaml`:

```yaml
packages:
  - web
  - mcp
  - packages/*
  - test/e2e
```

Create root `package.json`:

```json
{
  "name": "autoboard",
  "private": true,
  "packageManager": "pnpm@11.13.1",
  "scripts": {
    "check": "pnpm -r check",
    "test": "pnpm -r test",
    "build": "pnpm -r build"
  }
}
```

Create `compose.yaml` with PostgreSQL 17, database/user/password `autoboard`, port `5432`, and a named volume `postgres-data`. Add a healthcheck using `pg_isready -U autoboard`.

Extend root `.gitignore` with:

```gitignore
.env
node_modules/
dist/
coverage/
server/var/
server/priv/static/
*.sock
```

- [ ] **Step 3: Add Elixir dependencies and configuration**

Set `server/mix.exs` dependencies to:

```elixir
defp deps do
  [
    {:ecto_sql, "~> 3.14"},
    {:postgrex, "~> 0.22.3"},
    {:plug, "~> 1.20"},
    {:bandit, "~> 1.12"},
    {:jason, "~> 1.4"},
    {:xema, "~> 0.17.9", only: :test}
  ]
end
```

Configure `Autoboard.Repo` with `DATABASE_URL`, defaulting in development to `ecto://autoboard:autoboard@localhost/autoboard_dev`. Configure test database `autoboard_test#{System.get_env("MIX_TEST_PARTITION")}` and SQL sandbox pool. Default the socket to `Path.join(data_dir, "autoboard.sock")`, data to `Path.expand("../var", __DIR__)`, HTTP to `127.0.0.1:4040`, and attachment size to `52_428_800` bytes.

- [ ] **Step 4: Write the failing application supervision test**

Create `server/test/autoboard/application_test.exs`:

```elixir
defmodule Autoboard.ApplicationTest do
  use ExUnit.Case, async: false

  test "starts the repository under the application supervisor" do
    assert Process.whereis(Autoboard.Repo)
    assert %{active: active} = Supervisor.count_children(Autoboard.Supervisor)
    assert active >= 1
  end
end
```

Run `docker compose up -d postgres && cd server && mix deps.get && MIX_ENV=test mix ecto.create && mix test test/autoboard/application_test.exs`.

Expected: FAIL because `Autoboard.Repo` and the named supervisor are not configured.

- [ ] **Step 5: Add Repo supervision and make all workspaces compile**

Create `Autoboard.Repo`, register it in `ecto_repos`, name the root supervisor `Autoboard.Supervisor`, and add the Repo child. Add TypeScript package metadata using ESM, composite TypeScript configs, and these dependency floors: `effect ^3.22`, `react ^19.2`, `react-router ^8.2`, `vite ^8.1`, `@modelcontextprotocol/sdk ^1.29`, `zod ^4.4`, `vitest ^4.1`, `@testing-library/react ^16.3`, `@testing-library/jest-dom ^6.9`, and `typescript ^7.0`.

Use package names `@autoboard/contracts`, `@autoboard/mcp`, and `@autoboard/web`. Every package defines `check` as `tsc --noEmit`, `test` as `vitest run`, and `build` as its non-watch production build. `mcp` and `web` depend on `@autoboard/contracts: workspace:*`; `mcp` also depends on Effect, the MCP SDK, and Zod; `web` also depends on Effect, React, React Router, `react-markdown`, and `rehype-sanitize`.

Run:

```bash
cd server && mix test test/autoboard/application_test.exs
cd .. && corepack pnpm install && corepack pnpm check
```

Expected: both commands PASS.

- [ ] **Step 6: Commit the baseline**

```bash
git add server web mcp packages package.json pnpm-workspace.yaml pnpm-lock.yaml compose.yaml
git commit -m "build: scaffold autoboard workspaces"
```

---

### Task 2: Add Authorization, Projects, Tokens, and Activity

**Files:**

- Create: `server/priv/repo/migrations/20260716000100_create_projects_tokens_and_activity.exs`
- Create: `server/lib/autoboard/auth/context.ex`
- Create: `server/lib/autoboard/auth/token.ex`
- Create: `server/lib/autoboard/domain/error.ex`
- Create: `server/lib/autoboard/projects/project.ex`
- Create: `server/lib/autoboard/projects.ex`
- Create: `server/lib/autoboard/activity/event.ex`
- Create: `server/lib/autoboard/activity.ex`
- Create: `server/test/support/data_case.ex`
- Modify: `server/test/test_helper.exs`
- Create: `server/test/autoboard/projects_test.exs`
- Create: `server/test/autoboard/auth/token_test.exs`

**Interfaces:**

- Produces: `Autoboard.Auth.Context.global/1`, accepting `:me | :codex`.
- Produces: `Autoboard.Auth.Token.issue/1` and `authenticate/1`.
- Produces: `Autoboard.Projects.create/2`, `update/4`, `archive/3`, `restore/3`, `list/1`, and `fetch/2`.
- Produces: `%Autoboard.Domain.Error{kind, message, fields, current}` used by every later adapter.
- Produces: `Autoboard.Activity.append/5` for use inside an existing transaction.

- [ ] **Step 1: Write project and token behavior tests**

Cover these exact cases in ExUnit:

```elixir
test "project keys are normalized once and remain immutable", %{ctx: ctx} do
  assert {:ok, project} = Projects.create(ctx, %{key: "auto", name: "Autoboard", description: ""})
  assert project.key == "AUTO"
  assert project.revision == 1
  assert {:error, %Error{kind: :validation_failed}} =
           Projects.update(ctx, project.id, project.revision, %{key: "NEW"})
end

test "stale project writes return the current project", %{ctx: ctx} do
  project = project_fixture(ctx)
  assert {:ok, updated} = Projects.update(ctx, project.id, 1, %{name: "Renamed"})
  assert {:error, %Error{kind: :revision_conflict, current: ^updated}} =
           Projects.archive(ctx, project.id, 1)
end

test "tokens authenticate without storing plaintext" do
  assert {:ok, plaintext, token} = Token.issue(:codex)
  refute token.digest == plaintext
  assert {:ok, %Context{actor: :codex, scope: :global}} = Token.authenticate(plaintext)
end
```

Run `cd server && mix test test/autoboard/projects_test.exs test/autoboard/auth/token_test.exs`.

Expected: FAIL because schemas and contexts do not exist.

- [ ] **Step 2: Create the initial migration and schemas**

Create tables:

- `projects`: binary UUID primary key, `key` citext unique, `name`, `description`, `state`, `revision` default 1, `next_ticket_number` default 1, timestamps.
- `access_tokens`: binary UUID primary key, unique 32-byte `digest`, `actor`, `revoked_at`, timestamps.
- `activity_events`: bigserial primary key, `event_type`, `actor`, project UUID, nullable ticket UUID without an FK until Task 3, `payload` jsonb, `inserted_at`.

Enable the `citext` extension. Add database checks for project state and actor values.

In `test_helper.exs`, start ExUnit and set `Ecto.Adapters.SQL.Sandbox.mode(Autoboard.Repo, :manual)`. `DataCase` checks out the sandbox for each test and switches to shared mode only for non-async tests.

Validate project keys against `^[A-Z][A-Z0-9]{1,7}$`, project names at 1-200 trimmed characters, and descriptions as strings. All project-rooted mutations call a shared active-project guard; only `Projects.restore/3` bypasses it.

- [ ] **Step 3: Implement shared context and error types**

Use these public shapes:

```elixir
defmodule Autoboard.Auth.Context do
  @enforce_keys [:actor, :scope]
  defstruct [:actor, :scope]
  @type actor :: :me | :codex | :system
  @type t :: %__MODULE__{actor: actor(), scope: :global}

  def global(actor) when actor in [:me, :codex], do: %__MODULE__{actor: actor, scope: :global}
end

defmodule Autoboard.Domain.Error do
  @enforce_keys [:kind, :message]
  defstruct [:kind, :message, fields: %{}, current: nil]
end
```

Generate tokens as `"ab_" <> Base.url_encode64(:crypto.strong_rand_bytes(32), padding: false)` and store `:crypto.hash(:sha256, plaintext)`. Compare digests with `Plug.Crypto.secure_compare/2` after lookup.

- [ ] **Step 4: Implement project transactions and activity**

Each mutation must:

1. Fetch the project under `FOR UPDATE`.
2. Compare `expected_revision` for existing projects.
3. update `revision` by one.
4. insert an activity row through `Activity.append/5`.
5. return the entity plus an activity-event list from `Repo.transaction/1`, then broadcast only after commit in Task 5.

Use event types `project.created`, `project.updated`, `project.archived`, and `project.restored`. Ensure list returns active projects before archived projects and sorts each group by case-insensitive name.

- [ ] **Step 5: Run migrations and tests**

Run:

```bash
cd server
MIX_ENV=test mix ecto.reset
mix test test/autoboard/projects_test.exs test/autoboard/auth/token_test.exs
```

Expected: PASS with no SQL sandbox ownership errors.

- [ ] **Step 6: Commit project foundations**

```bash
git add server/priv/repo/migrations server/lib/autoboard server/test
git commit -m "feat: add projects tokens and activity"
```

---

### Task 3: Implement Tickets, Labels, and One-Level Subtasks

**Files:**

- Create: `server/priv/repo/migrations/20260716000200_create_tickets_and_labels.exs`
- Create: `server/lib/autoboard/tickets/ticket.ex`
- Create: `server/lib/autoboard/tickets/label.ex`
- Create: `server/lib/autoboard/tickets.ex`
- Create: `server/test/autoboard/tickets_test.exs`

**Interfaces:**

- Produces: `Tickets.create/2`, `update/4`, `transition/4`, `fetch/2`, and `search/2`.
- `create/2` accepts `project_id`, `title`, optional `description`, `status`, `priority`, `assignee`, `labels`, and `parent_ticket_id`.
- `update/4` accepts only `title`, `description`, `priority`, `assignee`, and complete `labels`.
- `transition/4` accepts a target status and requires `expected_revision`.

- [ ] **Step 1: Write failing ticket tests**

Add tests for defaults, concurrent numbering, label normalization, stale revisions, and subtask depth. Include:

```elixir
test "allocates project-local identifiers and defaults", %{ctx: ctx, project: project} do
  assert {:ok, first} = Tickets.create(ctx, %{project_id: project.id, title: "First"})
  assert {:ok, second} = Tickets.create(ctx, %{project_id: project.id, title: "Second"})
  assert first.identifier == "AUTO-1"
  assert second.identifier == "AUTO-2"
  assert {first.status, first.priority, first.assignee, first.revision} ==
           {:triage, :none, :unassigned, 1}
end

test "rejects a grandchild", %{ctx: ctx, project: project} do
  parent = ticket_fixture(ctx, project)
  child = ticket_fixture(ctx, project, %{parent_ticket_id: parent.id})
  assert {:error, %Error{kind: :validation_failed}} =
           Tickets.create(ctx, %{project_id: project.id, parent_ticket_id: child.id, title: "Too deep"})
end
```

Run `cd server && mix test test/autoboard/tickets_test.exs`.

Expected: FAIL because ticket tables and context are absent.

- [ ] **Step 2: Create ticket and label tables**

Create `tickets` with UUID primary key, project FK, integer `number`, title, description, status, priority, assignee, revision default 1, nullable parent FK, and timestamps. Add unique `(project_id, number)`, project/status and project/assignee indexes, enum check constraints, and a check preventing a ticket from parenting itself.

Create `labels` with UUID primary key, project FK, citext name, timestamps, and unique `(project_id, name)`. Create `ticket_labels` with a composite unique key.

Validate ticket titles at 1-500 trimmed characters, labels at 1-50 trimmed characters, at most 20 labels per ticket, and descriptions as strings. Normalize labels by trimming and collapsing internal whitespace before case-insensitive lookup.

Add the activity ticket FK after `tickets` exists using `on_delete: :nothing`.

- [ ] **Step 3: Implement transactional numbering and ticket creation**

Inside `Repo.transaction/1`, lock the project row using `lock: "FOR UPDATE"`, take `next_ticket_number`, increment it, validate an optional parent belongs to the same project and has no parent, insert labels by normalized trimmed name with conflict-ignore followed by lookup, insert the ticket and join rows, and append `ticket.created`.

Expose visible identifiers in a virtual field populated as `project.key <> "-" <> Integer.to_string(number)` by query preload/presentation rather than storing duplicated text.

- [ ] **Step 4: Implement update and transition guards**

Lock the ticket, compare revision, replace the complete label set, and increment revision exactly once. Permit transitions between any status values, except:

- `done` fails with `blocked_by_dependency` when blockers remain; Task 4 supplies the query.
- `done` and `canceled` fail with `invalid_transition` when a parent has non-terminal subtasks.

Append `ticket.updated` or `ticket.transitioned` with changed fields and old/new status.

- [ ] **Step 5: Run ticket tests and the full suite**

Run `cd server && mix test`.

Expected: all project, token, and ticket tests PASS.

- [ ] **Step 6: Commit ticket core**

```bash
git add server/priv/repo/migrations server/lib/autoboard/tickets* server/test/autoboard/tickets_test.exs
git commit -m "feat: add tickets labels and subtasks"
```

---

### Task 4: Add Dependencies and Actionable Work Discovery

**Files:**

- Create: `server/priv/repo/migrations/20260716000300_create_ticket_dependencies.exs`
- Create: `server/lib/autoboard/tickets/dependency.ex`
- Create: `server/lib/autoboard/tickets/graph.ex`
- Modify: `server/lib/autoboard/tickets.ex`
- Create: `server/test/autoboard/dependencies_test.exs`
- Create: `server/test/autoboard/actionable_tickets_test.exs`

**Interfaces:**

- Produces: `Tickets.add_dependency/5`, `remove_dependency/5`, `blocked?/2`, and `list_actionable/2`.
- Dependency functions accept blocked ticket ID, blocker ticket ID, and the blocked ticket's expected revision.
- `list_actionable/2` accepts `%{project_id: optional_uuid, limit: 1..100}`.

- [ ] **Step 1: Write graph and actionable-work tests**

Test self-edge, cross-project edge, duplicate edge, direct cycle, multi-hop cycle, resolved blockers, and leaf-only work. Include:

```elixir
test "Codex work excludes human, unassigned, blocked, and parent tickets", %{ctx: ctx, project: project} do
  codex = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex})
  _human = ticket_fixture(ctx, project, %{status: :ready, assignee: :me})
  _unassigned = ticket_fixture(ctx, project, %{status: :ready, assignee: :unassigned})
  parent = ticket_fixture(ctx, project, %{status: :ready, assignee: :codex})
  _child = ticket_fixture(ctx, project, %{parent_ticket_id: parent.id})

  assert Enum.map(Tickets.list_actionable(ctx, %{limit: 100}), & &1.id) == [codex.id]
end
```

Run both new test files and expect missing-function failures.

- [ ] **Step 2: Create the dependency table and pure graph helper**

Create `ticket_dependencies` with blocker and blocked ticket UUID FKs, timestamps, a unique pair, and a check rejecting equal IDs.

Implement `Graph.reachable?(edges, from, target)` as iterative depth-first traversal using `MapSet`; unit test it without Ecto.

- [ ] **Step 3: Implement race-safe relationship mutation**

Within a transaction, lock the project row to serialize dependency edits for that project, lock the blocked ticket, compare revision, load all project edges, and reject an addition when `Graph.reachable?(edges, blocked_id, blocker_id)` is true. Verify both tickets belong to the project. Increment the blocked ticket revision and append `dependency.added` or `dependency.removed`.

- [ ] **Step 4: Implement blocking and actionable queries**

`blocked?/2` returns true when a joined blocker status is not `done` or `canceled`.

`list_actionable/2` returns only tickets matching all of:

- `status == :ready`
- `assignee == :codex`
- no unresolved blocker
- no non-terminal subtask
- optional authorized project filter

Sort by priority rank urgent-to-none, then oldest `inserted_at`, then UUID for deterministic ties.

- [ ] **Step 5: Integrate transition guards and run tests**

Wire `blocked?/2` into `Tickets.transition/4`. Run `cd server && mix test`.

When a blocker enters or leaves a terminal status, lock every directly blocked ticket, increment each revision once, and append `dependency.blocking_changed` activity alongside `ticket.transitioned` in the same transaction. Return all committed events for post-commit broadcast.

Expected: dependency, ticket-transition, and actionable tests PASS.

- [ ] **Step 6: Commit relationship behavior**

```bash
git add server/priv/repo/migrations server/lib/autoboard/tickets server/lib/autoboard/tickets.ex server/test
git commit -m "feat: add ticket dependencies and work discovery"
```

---

### Task 5: Add Comments, Attachments, and Post-Commit Event Broadcasting

**Files:**

- Create: `server/priv/repo/migrations/20260716000400_create_comments_and_attachments.exs`
- Create: `server/lib/autoboard/comments/comment.ex`
- Create: `server/lib/autoboard/comments.ex`
- Create: `server/lib/autoboard/attachments/attachment.ex`
- Create: `server/lib/autoboard/attachments/storage.ex`
- Create: `server/lib/autoboard/attachments.ex`
- Create: `server/lib/autoboard/activity/broadcaster.ex`
- Modify: `server/lib/autoboard/activity.ex`
- Modify: `server/lib/autoboard/application.ex`
- Create: `server/test/autoboard/comments_test.exs`
- Create: `server/test/autoboard/attachments_test.exs`
- Create: `server/test/autoboard/activity_test.exs`

**Interfaces:**

- Produces: `Comments.add/3`.
- Produces: `Attachments.add_from_path/3`, `fetch/2`, `read/2`, and `cleanup/0`.
- Produces: `Activity.subscribe/0`, `unsubscribe/0`, `replay_after/2`, and `broadcast/1`.

- [ ] **Step 1: Write failure-first tests**

Cover actor derivation, blank comment rejection, absolute-path requirement, 50 MB rejection through a lowered test config, SHA-256 metadata, copy cleanup on transaction failure, stale temp cleanup, and broadcast-after-commit only.

Use a test temp directory from `System.tmp_dir!()` plus a UUID and remove only that explicit directory in `on_exit`.

- [ ] **Step 2: Create schemas and append-only contexts**

Create `comments` and `attachments` tables with the fields from the design, ticket/project FKs, actor checks, and no update/delete context functions. `Comments.add/3` derives actor from context, inserts the comment, increments ticket revision, and appends `comment.added` in one transaction.

- [ ] **Step 3: Implement managed attachment storage**

`Storage.stage/1` must:

1. reject non-absolute paths;
2. stat the source and enforce configured maximum bytes;
3. stream-copy to `<data_dir>/attachments/tmp/<uuid>`;
4. compute lowercase SHA-256 while reading;
5. return staged path, original filename, MIME type, size, and checksum.

`Attachments.add_from_path/3` moves the staged file to `<data_dir>/attachments/<attachment_uuid>` before inserting. On transaction error it removes that exact final file. Startup cleanup removes temp files older than one hour and logs final files that lack attachment rows without deleting them.

- [ ] **Step 4: Implement broadcast-after-commit**

Start a duplicate-key `Registry` named `Autoboard.Activity.Registry`. `Activity.subscribe/0` registers the process; `broadcast/1` uses `Registry.dispatch/3` to send `{:activity, event}`.

Create a single helper `Activity.commit/1` that runs a function returning `{result, events}` inside `Repo.transaction/1`, broadcasts every event only on `{:ok, {result, events}}`, and returns `{:ok, result}`. Refactor earlier mutation contexts to use it.

- [ ] **Step 5: Run tests and inspect temp storage**

Run `cd server && mix test`.

Expected: all tests PASS, and the test data directory contains no staged files after the suite.

- [ ] **Step 6: Commit collaboration records and storage**

```bash
git add server/priv/repo/migrations server/lib/autoboard server/test
git commit -m "feat: add comments attachments and event broadcasting"
```

---

### Task 6: Build Canonical Read Models and Transport Presenters

**Files:**

- Create: `server/lib/autoboard/read_model.ex`
- Create: `server/lib/autoboard/presenter.ex`
- Create: `server/test/autoboard/read_model_test.exs`
- Create: `server/test/autoboard/presenter_test.exs`
- Create: `server/test/fixtures/contracts/*.json`

**Interfaces:**

- Produces: `ReadModel.list_projects/1`, `triage_tickets/1`, `project_board/2`, `canceled_tickets/2`, `ticket_detail/2`, `search_tickets/2`, and `actionable_tickets/2`.
- Produces presenter maps with snake_case JSON keys shared by RPC and HTTP.

- [ ] **Step 1: Write complete read-model fixture tests**

Construct one project containing every status, a parent/subtask, blocker relationship, labels, comment, attachment, and activity. Assert:

- board groups only backlog, ready, in-progress, and done;
- triage and canceled are separate queries;
- detail preloads all relationships without N+1 queries;
- `blocked` is encoded as a boolean;
- identifier, revision, actor, timestamps, and attachment metadata are present;
- managed storage paths never appear in HTTP-shaped maps.

- [ ] **Step 2: Implement preload-focused read functions**

Keep all joins and authorization filtering in `ReadModel`. Use a bounded activity query defaulting to 100 newest rows, a ticket search limit of 100, and case-insensitive title/description substring search. Return domain structs plus explicit relationship aggregates; do not serialize Ecto structs directly.

- [ ] **Step 3: Implement deterministic presenters**

Create pure presenter functions:

```elixir
project(project)
ticket_summary(ticket)
ticket_detail(detail)
board(project, grouped_tickets)
activity(event)
attachment(attachment, include_managed_path? \\ false)
error(%Autoboard.Domain.Error{})
```

Encode enums as snake_case strings and datetimes as ISO 8601 UTC. Only the authenticated RPC `read_attachment` response may set `include_managed_path?` true.

- [ ] **Step 4: Freeze representative JSON fixtures**

Write actual fixture output from presenter tests to files under `server/test/fixtures/contracts/`. Assert those files decode with Jason and exactly equal the presenter maps. These fixtures become Task 8's cross-language contract inputs.

- [ ] **Step 5: Run the suite and commit read contracts**

Run `cd server && mix test`, expect PASS, then:

```bash
git add server/lib/autoboard/read_model.ex server/lib/autoboard/presenter.ex server/test
git commit -m "feat: add canonical autoboard read models"
```

---

### Task 7: Implement the Versioned Unix-Socket RPC Server

**Files:**

- Create: `server/lib/autoboard/rpc/listener.ex`
- Create: `server/lib/autoboard/rpc/acceptor.ex`
- Create: `server/lib/autoboard/rpc/session.ex`
- Create: `server/lib/autoboard/rpc/router.ex`
- Create: `server/lib/autoboard/rpc/error.ex`
- Modify: `server/lib/autoboard/application.ex`
- Create: `server/test/support/rpc_client.ex`
- Create: `server/test/autoboard/rpc/session_test.exs`
- Create: `server/test/autoboard/rpc/router_test.exs`

**Interfaces:**

- Produces the JSON-RPC 2.0 methods listed in the design, framed with Erlang `packet: 4`.
- `session.initialize` returns `protocol_version`, `server_version`, `actor`, and `authorization.kind`.
- Router returns `{:ok, map}` or `{:error, %Domain.Error{}}`; only Session knows JSON-RPC envelopes.

- [ ] **Step 1: Write socket protocol tests**

Start the listener on a per-test explicit Unix path. Test initialization-required, valid token, invalid token, protocol mismatch, fragmented/coalesced messages through `packet: 4`, concurrent request IDs, malformed JSON, unknown method, and a payload over 4 MB.

Assert this initialization response shape:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocol_version": 1,
    "server_version": "0.1.0",
    "actor": "codex",
    "authorization": { "kind": "global" }
  }
}
```

- [ ] **Step 2: Implement listener and session supervision**

Use `:gen_tcp.listen(0, [:binary, packet: 4, active: false, reuseaddr: true, packet_size: 4_194_304, ifaddr: {:local, String.to_charlist(path)}])`. Remove only a pre-existing socket owned by the current user, chmod the created socket to `0o600`, and supervise accepted sessions under a `Task.Supervisor`.

The session reads one packet at a time, decodes Jason, requires `session.initialize` first, holds `%Auth.Context{}` after authentication, and closes after returning unauthorized or version-mismatch errors.

- [ ] **Step 3: Implement router method mapping**

Map these exact RPC names:

```text
projects.list projects.get projects.create projects.update projects.archive projects.restore
tickets.board tickets.search tickets.get tickets.actionable tickets.create tickets.update tickets.transition
comments.add attachments.add_from_path attachments.read
dependencies.add dependencies.remove
```

Validate required keys before invoking contexts. All identifiers may be UUIDs or visible ticket identifiers where a ticket is expected. Never accept actor or authorization scope from method parameters.

- [ ] **Step 4: Encode stable error envelopes**

Use JSON-RPC codes `-32600`, `-32601`, and `-32602` for protocol errors. Use application code `-32010` and `data.kind` for domain errors. Include `fields` for validation, `current` for revision conflict, and `correlation_id` only for internal errors.

- [ ] **Step 5: Run RPC tests and full Elixir suite**

Run `cd server && mix test`.

Expected: all RPC cases and prior domain cases PASS; no socket file remains after test shutdown.

- [ ] **Step 6: Commit the private API**

```bash
git add server/lib/autoboard/rpc server/lib/autoboard/application.ex server/test
git commit -m "feat: add unix socket rpc server"
```

---

### Task 8: Implement Shared Effect Contracts and the Node RPC Client

**Files:**

- Create: `packages/contracts/src/domain.ts`
- Create: `packages/contracts/src/rpc.ts`
- Create: `packages/contracts/src/http.ts`
- Create: `packages/contracts/src/generate-json-schema.ts`
- Create: `packages/contracts/test/contracts.test.ts`
- Create: `packages/contracts/generated/*.schema.json`
- Create: `mcp/src/rpc-client.ts`
- Create: `mcp/src/rpc-error.ts`
- Create: `mcp/test/rpc-client.test.ts`
- Create: `server/test/autoboard/contract_schema_test.exs`

**Interfaces:**

- Produces Effect schemas and encoded types: `Project`, `TicketSummary`, `TicketDetail`, `ProjectBoard`, `ActivityEvent`, `Attachment`, `RpcSuccess`, and `RpcFailure`.
- Produces `RpcClient.connect({socketPath, token})`, `call(method, params, schema, mode)`, and `close()`.
- `mode` is `"read" | "write"`; only reads reconnect and retry once.

- [ ] **Step 1: Define failing fixture and schema tests**

Load every JSON fixture from `server/test/fixtures/contracts` and decode it with the matching Effect schema using `Schema.decodeUnknownSync`. Add negative cases for unknown enums, missing revision, and attachment responses that leak `managed_path` in HTTP shapes.

Run `corepack pnpm --filter @autoboard/contracts test`.

Expected: FAIL because schemas are absent.

- [ ] **Step 2: Implement Effect schemas and encoded types**

Define enums with `Schema.Literal`, UUIDs as branded non-empty strings, revisions as positive integers, datetimes as ISO strings, and every object with exact known fields. Export encoded TypeScript types using `Schema.Schema.Encoded<typeof SchemaName>` so transport code never assumes decoded Date objects.

Generate OpenAPI-compatible JSON Schema through `OpenApiJsonSchema.make` into `packages/contracts/generated`. Add an ExUnit test using Xema to validate the same fixtures against generated schemas.

- [ ] **Step 3: Write failing Node framing tests**

Use `node:net` to create a temporary Unix server. Test split headers, split payloads, multiple frames per data event, out-of-order responses, server errors, initialization, read reconnect, and write indeterminate failure.

- [ ] **Step 4: Implement the RPC client**

Frame requests with `Buffer.allocUnsafe(4).writeUInt32BE(payload.length)`. Maintain a receive buffer and a `Map<number, {resolve, reject}>`. After connection, call `session.initialize` before releasing the client. Decode every result with the passed Effect schema.

On disconnect:

- reject pending writes with `IndeterminateWriteError`;
- reconnect once and replay pending reads with new JSON-RPC IDs;
- reject all calls after the second failure.

- [ ] **Step 5: Run cross-language contract checks**

Run:

```bash
corepack pnpm --filter @autoboard/contracts test
corepack pnpm --filter @autoboard/mcp test -- rpc-client
cd server && mix test test/autoboard/contract_schema_test.exs
```

Expected: all three PASS.

- [ ] **Step 6: Commit contracts and transport**

```bash
git add packages/contracts mcp server/test/autoboard/contract_schema_test.exs
git commit -m "feat: add typed rpc contracts and client"
```

---

### Task 9: Expose the Complete MCP Tool Surface

**Files:**

- Create: `mcp/src/server.ts`
- Create: `mcp/src/tool-result.ts`
- Create: `mcp/src/tools/read.ts`
- Create: `mcp/src/tools/write.ts`
- Modify: `mcp/src/main.ts`
- Create: `mcp/test/tools.test.ts`
- Create: `mcp/test/instructions.test.ts`

**Interfaces:**

- Produces stdio MCP tools: `list_projects`, `get_project_board`, `search_tickets`, `get_ticket`, `list_actionable_tickets`, `read_attachment`, `create_project`, `update_project`, `archive_project`, `restore_project`, `create_ticket`, `update_ticket`, `transition_ticket`, `add_comment`, `add_attachment_from_path`, `add_dependency`, and `remove_dependency`.

- [ ] **Step 1: Write tool registry tests**

Instantiate the server with a fake `RpcClient` and assert the exact 17 names, descriptions, Zod input schemas, output schemas, and annotations. Assert every read has `readOnlyHint: true`, every write has `readOnlyHint: false`, `archive_project` and `remove_dependency` have `destructiveHint: true`, and every tool has `openWorldHint: false`.

- [ ] **Step 2: Implement server instructions and result mapping**

Use this opening text verbatim so the first 512 characters are self-contained:

```text
Autoboard is a direct-write project board. Tickets assigned to `me` are reserved for the human. Execute only tickets returned by list_actionable_tickets unless the human explicitly instructs otherwise. Read the latest entity before revision-checked writes. Confirm broad reorganizations, project archival, and dependency removal with the human.
```

Map successful RPC responses to `{content: [{type: "text", text}], structuredContent}`. Map domain failures to `isError: true` with the error kind, message, field paths, current entity, and a specific repair hint.

- [ ] **Step 3: Register read tools**

Implement the six read tools with bounded limits and strict Zod schemas. `list_actionable_tickets` defaults to 25 and caps at 100. `read_attachment` returns inline UTF-8 content only when Elixir marks it text and within the response limit; otherwise return the managed path and metadata.

- [ ] **Step 4: Register write tools**

Implement the eleven write tools. Require `expected_revision` for project update/archive/restore, ticket update/transition, and dependency add/remove. Create, comment append, and attachment append do not require an expected revision; they return the newly incremented revision. Do not expose actor, authorization scope, hard delete, arbitrary RPC method, or raw SQL inputs.

- [ ] **Step 5: Connect stdio entrypoint and verify with MCP Inspector**

`main.ts` must require `AUTOBOARD_SOCKET` and `AUTOBOARD_TOKEN`, connect the RPC client, create `StdioServerTransport`, connect `McpServer`, log only to stderr, and close the RPC socket on SIGINT/SIGTERM.

Run:

```bash
corepack pnpm --filter @autoboard/mcp test
corepack pnpm --filter @autoboard/mcp build
npx @modelcontextprotocol/inspector node mcp/dist/main.js
```

Expected: tests PASS, build succeeds, Inspector lists exactly 17 tools, and stdout contains MCP frames only.

- [ ] **Step 6: Commit MCP capability**

```bash
git add mcp
git commit -m "feat: expose autoboard mcp tools"
```

---

### Task 10: Add the Read-only HTTP API and Replayable SSE

**Files:**

- Create: `server/lib/autoboard_web/router.ex`
- Create: `server/lib/autoboard_web/json.ex`
- Create: `server/lib/autoboard_web/events_stream.ex`
- Create: `server/lib/autoboard_web/spa.ex`
- Modify: `server/lib/autoboard/application.ex`
- Create: `server/test/autoboard_web/router_test.exs`
- Create: `server/test/autoboard_web/events_stream_test.exs`

**Interfaces:**

- Produces only the eight GET endpoints and static SPA fallback from the design, including `GET /api/v1/triage`.
- SSE emits events with `id`, `event: activity`, and JSON `data` containing `event_type`, `project_id`, optional `ticket_id`, and `inserted_at`.

- [ ] **Step 1: Write read route and mutation-absence tests**

Using `Plug.Test`, assert every GET response against fixture maps. Assert `POST`, `PUT`, `PATCH`, and `DELETE` to every `/api/v1` prefix return 404 and never change database row counts.

Assert `/health` returns 200 with `{"status":"ok"}` when `SELECT 1` succeeds and 503 otherwise.

- [ ] **Step 2: Implement the Plug router**

Use `Plug.Router`, `Plug.Parsers` for JSON error bodies only, and `Plug.Static` for built assets. Call `ReadModel`, then `Presenter`, then `Jason.encode!`; never call Ecto from route clauses. Validate project keys and identifiers before queries.

- [ ] **Step 3: Write replay-race tests**

Test `Last-Event-ID` replay, no duplicates across replay/live boundary, heartbeat comments every 15 seconds, disconnect cleanup, and event ordering.

- [ ] **Step 4: Implement race-safe SSE**

For each connection:

1. subscribe to `Activity`;
2. read the current maximum activity ID as a high-water mark;
3. replay `(last_id, high_water]`;
4. stream subscribed events with IDs greater than `high_water`;
5. send `: heartbeat\n\n` every 15 seconds;
6. unsubscribe when `Plug.Conn.chunk/2` returns an error.

Use `send_chunked(conn, 200)` and `text/event-stream`; do not buffer the whole response.

- [ ] **Step 5: Start Bandit and run HTTP tests**

Add `{Bandit, plug: AutoboardWeb.Router, scheme: :http, ip: configured_loopback, port: configured_port}` to the supervisor.

Run `cd server && mix test test/autoboard_web` and then the full `mix test`.

Expected: PASS and no HTTP write route exists.

- [ ] **Step 6: Commit the read surface**

```bash
git add server/lib/autoboard_web server/lib/autoboard/application.ex server/test/autoboard_web
git commit -m "feat: add read only http api and events"
```

---

### Task 11: Build the Project Index, Triage, and Kanban Views

**Files:**

- Create: `web/src/runtime.ts`
- Create: `web/src/api/client.ts`
- Create: `web/src/router.tsx`
- Create: `web/src/layout/AppShell.tsx`
- Create: `web/src/pages/ProjectsPage.tsx`
- Create: `web/src/pages/ProjectBoardPage.tsx`
- Create: `web/src/pages/TriagePage.tsx`
- Create: `web/src/pages/CanceledPage.tsx`
- Create: `web/src/components/TicketCard.tsx`
- Create: `web/src/styles.css`
- Modify: `web/src/main.tsx`
- Create: `web/src/pages/board.test.tsx`
- Create: `web/src/read-only.test.tsx`

**Interfaces:**

- Consumes `Project`, `ProjectBoard`, and ticket summary Effect schemas.
- Produces routes `/projects`, `/projects/:key`, `/projects/:key/canceled`, and `/triage`.

- [ ] **Step 1: Write browser component tests first**

Using Vitest, jsdom, and Testing Library, assert active/archived grouping, fixed Kanban column order, assignee/priority/blocked labels, empty states, navigation links, and canceled/triage pages.

Add a global read-only assertion:

```ts
expect(
  container.querySelectorAll(
    "form,input,textarea,select,[contenteditable=true]",
  ),
).toHaveLength(0);
expect(
  screen.queryByText(/create|edit|delete|move ticket/i),
).not.toBeInTheDocument();
```

Run `corepack pnpm --filter @autoboard/web test` and expect failures.

- [ ] **Step 2: Implement the Effect API service**

Create an `ApiClient` Effect service whose methods call `fetch`, reject non-2xx status with tagged `HttpError`, decode unknown JSON with shared Effect schemas, retry network/503 failures twice with 250 ms then 1 second delays, and never issue a non-GET request.

- [ ] **Step 3: Implement loader-based routes and shell**

Use React Router loaders that call `Effect.runPromise` on `ApiClient`. The shell contains only the Autoboard wordmark, Triage link/count, project links, and main outlet. Preserve the approved project-first layout and use semantic links rather than buttons for navigation.

- [ ] **Step 4: Implement board and history pages**

Render backlog, ready, in-progress, and done columns in fixed order. Each ticket card links to `/tickets/:identifier` and displays title, identifier, assignee, priority, comment/attachment counts, and computed blocker copy. Triage and canceled pages are read-only lists grouped by project.

- [ ] **Step 5: Run UI tests and build**

Run:

```bash
corepack pnpm --filter @autoboard/web test
corepack pnpm --filter @autoboard/web build
```

Expected: PASS; production assets build without TypeScript errors.

- [ ] **Step 6: Commit read-only board UI**

```bash
git add web
git commit -m "feat: add read only project boards"
```

---

### Task 12: Add Ticket Detail, Markdown, Attachments, and Live Revalidation

**Files:**

- Create: `web/src/pages/TicketDetailPage.tsx`
- Create: `web/src/components/TicketDrawer.tsx`
- Create: `web/src/components/Markdown.tsx`
- Create: `web/src/components/ActivityTimeline.tsx`
- Create: `web/src/events/activityStream.ts`
- Modify: `web/src/router.tsx`
- Modify: `web/src/layout/AppShell.tsx`
- Create: `web/src/pages/ticket-detail.test.tsx`
- Create: `web/src/events/activityStream.test.ts`

**Interfaces:**

- Consumes `TicketDetail` and `ActivityEvent` schemas.
- Produces `/tickets/:identifier` as a deep-link page and as a drawer when navigation carries a background project location.

- [ ] **Step 1: Write ticket detail and SSE tests**

Assert rendering of sanitized Markdown, assignee, labels, blocker links, subtasks, comments, attachment download links, and activity. Assert raw HTML scripts and event-handler attributes do not render.

For SSE, use a fake `EventSource` and assert relevant project/ticket events trigger one router revalidation while unrelated events do nothing. Assert reconnect starts after the last seen ID.

- [ ] **Step 2: Implement safe Markdown and detail components**

Use `react-markdown ^10.1` with `rehype-sanitize ^6`. Do not enable raw HTML. Use semantic lists and links. Attachments link only to `/api/v1/attachments/:id` and never reveal managed paths.

- [ ] **Step 3: Implement drawer/deep-link routing**

When a ticket link is opened from a board, pass the board location as `state.backgroundLocation`; render the board route beneath `TicketDrawer`. Direct navigation renders `TicketDetailPage` as a full page. Closing a drawer navigates back; it does not mutate server state.

- [ ] **Step 4: Wrap EventSource in Effect Stream**

Create `activityStream(lastEventId)` with `Stream.async`, decode each `activity` event with `ActivityEvent`, retain the greatest ID, and use exponential reconnect delays capped at 10 seconds. In the shell, compare event entity IDs with the current route and call React Router `revalidate()` only when relevant.

- [ ] **Step 5: Run UI verification**

Run `corepack pnpm --filter @autoboard/web test && corepack pnpm --filter @autoboard/web build`.

Expected: PASS, including read-only and sanitization assertions.

- [ ] **Step 6: Commit complete browser experience**

```bash
git add web
git commit -m "feat: add ticket detail and live updates"
```

---

### Task 13: Add Bootstrap Tasks, Release Assembly, and End-to-End Acceptance

**Files:**

- Create: `server/lib/mix/tasks/autoboard.setup.ex`
- Create: `server/lib/mix/tasks/autoboard.token.create.ex`
- Create: `server/lib/autoboard/release.ex`
- Modify: `server/mix.exs`, `server/config/runtime.exs`
- Modify: `package.json`
- Create: `test/e2e/package.json`
- Create: `test/e2e/run.mjs`
- Create: `test/e2e/vitest.config.ts`
- Create: `test/e2e/autoboard.test.ts`
- Create: `README.md`
- Create: `docs/codex-mcp-config.md`

**Interfaces:**

- Produces `mix autoboard.setup` and `mix autoboard.token.create --actor me|codex`.
- Produces a release containing static web assets plus the separate `mcp/dist/main.js` bundle.
- Produces a black-box acceptance test for the complete MCP-to-browser loop.

- [ ] **Step 1: Write bootstrap task tests**

Test actor validation, token printed exactly once, digest persisted, data directories created with owner permissions, migrations invoked through `Autoboard.Release.migrate/0`, and repeated setup remaining safe.

- [ ] **Step 2: Implement release and Mix tasks**

`Autoboard.Release.migrate/0` loads the application and calls `Ecto.Migrator.with_repo/3`. `autoboard.setup` migrates then creates `<data_dir>/attachments/tmp` with mode `0o700`. `autoboard.token.create` parses only `--actor`, issues the token, and prints plaintext to stdout with all diagnostics on stderr.

- [ ] **Step 3: Assemble production builds**

Add root scripts:

```json
{
  "build:contracts": "pnpm --filter @autoboard/contracts build",
  "build:mcp": "pnpm --filter @autoboard/mcp build",
  "build:web": "pnpm --filter @autoboard/web build",
  "build:server": "cp -R web/dist/. server/priv/static/ && cd server && MIX_ENV=prod mix release",
  "build": "pnpm build:contracts && pnpm build:mcp && pnpm build:web && pnpm build:server"
}
```

Also change root `test` to `pnpm -r --filter '!@autoboard/e2e' test` and add `test:e2e` as `pnpm --filter @autoboard/e2e test`, keeping unit and black-box runs separate.

Configure `Plug.Static` to serve `server/priv/static` and SPA fallback to `index.html`. Keep the MCP build outside the Elixir release so Codex can launch it directly.

- [ ] **Step 4: Write the black-box acceptance test**

Use the official MCP client with `StdioClientTransport` to launch `node mcp/dist/main.js`. The test must:

1. create project `AUTO`;
2. create parent, subtask, blocker, Codex, and human tickets;
3. add a dependency, comment, and temporary text attachment;
4. assert only the ready unblocked Codex leaf is actionable;
5. perform a stale revision write and dependency cycle and assert error kinds;
6. fetch HTTP project/ticket data and assert matching state;
7. open the React app with Playwright and assert project board/detail content;
8. transition a ticket through MCP and assert the browser updates without reload;
9. assert no mutation controls or non-GET `/api/v1` requests occurred.

Name the package `@autoboard/e2e` and make its `test` script run `node run.mjs`. The runner creates a unique temporary data directory and socket path, reserves an unused loopback HTTP port, sets `DATABASE_URL` to the dedicated `autoboard_e2e` database, runs `mix ecto.reset`, captures the one-line token from `mix autoboard.token.create --actor codex`, starts `mix run --no-halt` with the temporary paths and port, waits for `/health`, runs Vitest, and terminates only the child processes it started in a `finally` block. The same block removes only the temporary directory it created. Pass the captured socket and token to the MCP child through environment variables; never write the token to the repository.

- [ ] **Step 5: Document local operation and Codex configuration**

Document exact commands:

```bash
docker compose up -d postgres
cd server && mix autoboard.setup
cd server && mix autoboard.token.create --actor codex
corepack pnpm build
cd server && _build/prod/rel/autoboard/bin/autoboard start
```

Document Codex stdio configuration with `command = "node"`, absolute `mcp/dist/main.js`, `AUTOBOARD_SOCKET`, `AUTOBOARD_TOKEN`, and `default_tools_approval_mode = "writes"`. State that v1 tokens are global and the HTTP UI is loopback/read-only.

- [ ] **Step 6: Run the complete verification matrix**

Run:

```bash
docker compose up -d postgres
cd server && MIX_ENV=test mix ecto.reset && mix format --check-formatted && mix test
cd .. && corepack pnpm check && corepack pnpm test && corepack pnpm build
corepack pnpm --filter @autoboard/e2e test
git diff --check
```

Expected: every command exits 0; the acceptance test passes; `git diff --check` is silent.

- [ ] **Step 7: Commit the completed v1 integration**

```bash
git add server package.json web mcp packages test README.md docs/codex-mcp-config.md
git commit -m "feat: complete autoboard v1"
```

## Final Review Checklist

- [ ] Confirm all 17 MCP tools are present and no generic command escape hatch exists.
- [ ] Confirm every context entrypoint requires `%Auth.Context{}`.
- [ ] Confirm project and ticket scope can be filtered centrally in a future authorization context.
- [ ] Confirm no HTTP mutation route and no browser mutation affordance exists.
- [ ] Confirm comments/activity are append-only and core records have no hard-delete API.
- [ ] Confirm write reconnect behavior never retries an indeterminate mutation.
- [ ] Confirm `.superpowers/`, tokens, socket files, attachments, databases, and release secrets are ignored.
- [ ] Confirm the design acceptance scenario passes from MCP through the live browser.
