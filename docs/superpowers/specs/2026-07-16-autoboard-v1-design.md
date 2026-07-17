# Autoboard v1 Design

**Status:** Approved in conversation on 2026-07-16

## Summary

Autoboard is a local, single-user project-planning system controlled by an LLM through MCP. Its browser UI is a read-only project lens: it visualizes projects and tickets but cannot create, edit, move, assign, comment on, or otherwise mutate them.

Codex launches a TypeScript MCP adapter over stdio. The adapter connects to the Elixir application through an owner-only Unix domain socket and a small versioned JSON-RPC protocol. Elixir owns all domain behavior, persists canonical state in PostgreSQL through Ecto, serves the read-only React application through Plug and Bandit, and broadcasts committed activity through Server-Sent Events.

The first release is local-only, gives Codex global direct-write access, and relies on Codex's MCP approval policy and server instructions for confirmation before broad or destructive work. Application-level approval queues and project-scoped credentials are deferred.

## Product Principles

- The database is canonical; neither the React client nor the MCP adapter contains domain rules.
- The UI is for navigation and inspection only. All mutations enter through MCP and the private RPC boundary.
- Codex-assigned work is explicit. A ticket assigned to `me` is reserved for the human and excluded from Codex work discovery.
- Mutations apply immediately once authorized. Autoboard does not model proposals or approvals.
- Current state and immutable activity are stored together transactionally.
- The v1 surface stays local and single-user; security is sufficient for a loopback-only personal tool, not a public service.

## Scope

### Included

- Projects, project-local ticket identifiers, fixed ticket statuses, priority, labels, and assignment.
- Comments, one-level subtasks, same-project dependencies, and local file attachments.
- A read-only project index, project-first Kanban, global triage view, ticket detail, and activity display.
- A TypeScript MCP adapter using the official TypeScript MCP SDK.
- A private Unix-socket command/query API between TypeScript and Elixir.
- A read-only HTTP API and SSE event stream for the React application.
- Global bearer credentials with actor attribution.
- Durable activity history, optimistic concurrency, and local bootstrap tasks.

### Deferred

- Agent execution or orchestration beyond making tickets discoverable to Codex.
- Application-level proposals, approvals, or confirmation workflows.
- Project-scoped credentials, named agents, additional humans, roles, or account management.
- Estimates, cycles, milestones, time tracking, notifications, and automations.
- Cross-project dependencies, deeper subtask trees, hard deletion, and editable comments.
- Remote hosting, OAuth, cloud storage, antivirus scanning, encryption at rest, or public-network hardening.
- ChatGPT-hosted plugin packaging and custom MCP UI.

## System Architecture

```text
Codex
  | MCP over stdio
  v
TypeScript MCP adapter
  | framed JSON-RPC over an owner-only Unix domain socket
  v
Elixir OTP application
  |-- domain commands and queries
  |-- Ecto Repo --> PostgreSQL
  |-- attachment storage --> local filesystem
  |-- Plug/Bandit --> read-only HTTP API, SSE, and React assets
  `-- committed-event broadcaster

Browser --> loopback HTTP --> Elixir
```

The repository is organized as a small monorepo:

- `server/`: Mix project containing the OTP application, Ecto domain, RPC listener, Plug router, SSE broadcaster, release tasks, and tests.
- `web/`: Vite/React read-only application.
- `mcp/`: Node-based stdio MCP executable.
- `packages/contracts/`: Effect schemas shared by `web` and `mcp`, plus generated JSON Schema used by Elixir contract tests.

The MCP adapter and HTTP router are adapters over the same Elixir domain services. They do not call each other. The HTTP API exposes queries only; the Unix-socket RPC surface exposes both queries and commands.

## Domain Model

### Projects

A project has:

- A stable UUID.
- An immutable, unique uppercase key used in ticket identifiers.
- A mutable name and Markdown description.
- State `active` or `archived`.
- An integer revision incremented by every project mutation.
- Inserted and updated timestamps.

Archiving hides a project from the active project list but preserves all related records and keeps it browsable in the archived section. Restoring returns it to the active list.

### Tickets

A ticket has:

- A stable UUID and a project-local, monotonically allocated sequence number. The visible identifier is `<PROJECT_KEY>-<SEQUENCE>`; gaps are allowed.
- Title and Markdown description.
- Status: `triage`, `backlog`, `ready`, `in_progress`, `done`, or `canceled`.
- Priority: `none`, `low`, `medium`, `high`, or `urgent`.
- Assignee: `unassigned`, `me`, or `codex`.
- An integer revision incremented by every mutation to the ticket or its direct relationships.
- Optional parent ticket ID for one-level subtasks.
- Inserted and updated timestamps.

New tickets default to `triage`, `none`, and `unassigned`. Statuses display in workflow order, but the command API may move a ticket between any statuses. A ticket cannot become `done` while it has an unresolved blocker or a non-terminal subtask. A parent cannot become `canceled` while it has a non-terminal subtask. Terminal subtasks are `done` or `canceled`.

### Labels

Labels are project-local and have a stable UUID plus a unique case-insensitive display name within the project. Tickets and labels have a many-to-many relationship. `update_ticket` supplies the complete desired set of label names; absent labels are created automatically.

### Subtasks

A ticket may have one parent in the same project. A ticket that is already a subtask cannot have children. Parent changes that would create deeper nesting are rejected.

### Dependencies and blocking

A dependency is a directed same-project edge from a blocking ticket to a blocked ticket. Self-dependencies, duplicates, and cycles are rejected.

`blocked` is computed, not stored. A ticket is blocked when at least one blocker is not `done` or `canceled`. Dependency removal and blocker transitions increment the affected blocked ticket's revision and create activity events.

### Comments

Comments are append-only Markdown records attached to a ticket. The author is derived from the authenticated credential and is `me`, `codex`, or `system`; callers cannot choose another author. Comments are not edited or deleted in v1.

### Attachments

Attachments belong to tickets and contain an opaque UUID, original filename, media type, byte size, SHA-256 checksum, managed storage path, actor, and timestamp. The application copies an absolute source path into managed storage atomically. The default maximum is 50 MB and is configurable.

Text attachments can be returned inline to MCP within its response-size limit. Other attachments return metadata and the managed local path so local Codex tools can inspect them. The HTTP API streams attachment downloads by opaque ID.

### Activity

Every successful mutation appends an immutable activity record containing:

- A monotonic database ID used for SSE replay.
- Event type, actor, project ID, optional ticket ID, and timestamp.
- A compact JSON payload sufficient to render the change, including changed field names and relevant old/new values.

Current-state tables remain canonical. Activity is an audit and synchronization log, not an event-sourced reconstruction mechanism.

## Assignment and Actionable Work

Only `ready` tickets assigned to `codex` are actionable. `list_actionable_tickets` also excludes tickets that are blocked or that have a non-terminal subtask. It never returns tickets assigned to `me` or `unassigned`.

Other read tools may display any authorized ticket, including human-assigned work. MCP server instructions describe `me` as reserved and tell Codex not to begin such work unless the human explicitly reassigns it.

## Authentication and Future Authorization

`mix autoboard.token.create --actor codex` and `--actor me` create random credentials and print each plaintext token once. PostgreSQL stores only a SHA-256 token digest, actor, creation timestamp, and revocation timestamp.

V1 credentials grant global access. The RPC handshake converts a credential into an authorization context, and every domain query and command requires that context even though the only supported context is global.

Future project-scoped credentials will add project grants during credential issuance and make the authorization context filter every project-rooted query. Domain and MCP interfaces will not change. Cross-project relationships remain excluded so a scoped project is a closed authorization boundary.

The HTTP read surface is unauthenticated but binds to loopback only. The Unix socket is created in the application data directory with owner-only permissions. This is appropriate for v1's local single-user threat model.

## Private RPC Protocol

The Elixir application listens on a configurable Unix socket. `AUTOBOARD_SOCKET` gives the MCP adapter its path, and `AUTOBOARD_TOKEN` provides the credential.

Messages are JSON-RPC 2.0 documents framed by a four-byte unsigned big-endian payload length followed by UTF-8 JSON. Frames larger than 4 MB are rejected. Request IDs permit concurrent calls over one connection.

The first request must be `session.initialize` with:

```json
{
  "protocol_version": 1,
  "token": "plaintext startup token",
  "client": { "name": "autoboard-mcp", "version": "..." }
}
```

The response returns the negotiated protocol version, server version, actor, and authorization kind. No other method is accepted before initialization. Unsupported versions and invalid credentials close the connection after returning an error.

RPC method names mirror MCP capabilities under `projects.*`, `tickets.*`, `comments.*`, `attachments.*`, and `dependencies.*`. Elixir validates all inputs again; Effect validation in TypeScript is not trusted as authorization or domain enforcement.

Errors use normal JSON-RPC error envelopes with a stable `data.kind`:

- `unauthorized`
- `not_found`
- `validation_failed`
- `revision_conflict`
- `invalid_transition`
- `blocked_by_dependency`
- `dependency_cycle`
- `attachment_failed`
- `internal_error`

Validation errors include field paths. Revision conflicts include the latest entity. Internal errors include a correlation ID but no stack trace. The adapter reconnects once after a broken socket and retries read-only calls only; write calls return an indeterminate-result error rather than risk duplication.

## MCP Surface

The MCP server exposes tools only in v1; it does not expose resources, reusable prompts, sampling, elicitation, or custom UI.

### Read tools

- `list_projects`
- `get_project_board`
- `search_tickets`
- `get_ticket`
- `list_actionable_tickets`
- `read_attachment`

### Write tools

- `create_project`
- `update_project`
- `archive_project`
- `restore_project`
- `create_ticket`
- `update_ticket`
- `transition_ticket`
- `add_comment`
- `add_attachment_from_path`
- `add_dependency`
- `remove_dependency`

`create_ticket` accepts an optional parent identifier. `update_project` mutates name or description. `update_ticket` mutates title, description, priority, labels, or assignee. Project updates, archival, and restoration require the project's `expected_revision`; ticket updates, transitions, and relationship changes require the ticket's `expected_revision`. A stale revision returns `revision_conflict` and the current entity.

Tool results include concise human-readable content and structured data with stable IDs, visible identifiers, revisions, and timestamps. Tool annotations accurately mark reads, writes, and destructive operations. `archive_project` and `remove_dependency` are destructive; other mutation tools, including `restore_project`, are writes but not destructive.

The server initialization instructions put the critical rules in their opening text:

- Tickets assigned to `me` are reserved for the human.
- Codex should execute only work returned by `list_actionable_tickets` unless explicitly instructed otherwise.
- Codex should read the latest entity before revision-checked mutation.
- Broad reorganizations, project archival, and dependency removal should be confirmed with the human.

Autoboard applies every authorized MCP write immediately. Users who want confirmation configure Codex's MCP approval behavior to prompt for writes.

## Read-only HTTP and React UI

Bandit serves compiled React assets and these loopback-only endpoints:

- `GET /api/v1/projects`
- `GET /api/v1/projects/:key/board`
- `GET /api/v1/projects/:key/canceled`
- `GET /api/v1/tickets/:identifier`
- `GET /api/v1/attachments/:id`
- `GET /api/v1/events`
- `GET /health`

There are no HTTP mutation routes in v1.

The React application has these routes:

- `/projects`: active projects followed by an archived section.
- `/projects/:key`: project-first Kanban showing backlog, ready, in-progress, and done columns, with project summary links to triage and canceled tickets.
- `/projects/:key/canceled`: canceled ticket history for the project.
- `/tickets/:identifier`: full ticket detail; when opened from a board, the same route renders as a drawer over the board.
- `/triage`: triage tickets across active projects.

Ticket detail renders Markdown description, assignment, priority, labels, computed blocking, subtasks, dependency links, comments, attachment links, and activity. Navigation, deep links, attachment downloads, scrolling, and expandable read-only sections are allowed. Forms, editable fields, drag-and-drop, create buttons, action menus, and mutation shortcuts are prohibited.

Effect provides the HTTP client, transport schemas, runtime decoding, typed errors, retry policy, and SSE stream handling. React Router provides routes and loader-based data fetching. No second client cache is introduced. SSE events carry activity ID and affected entity IDs; route loaders revalidate only relevant board or ticket data.

`packages/contracts` is the TypeScript source of truth for transport shapes, not domain behavior. It exports Effect schemas to both TypeScript applications and emits JSON Schema. ExUnit contract tests validate Elixir responses and RPC fixtures against those generated schemas so drift fails CI.

## Transactions, Events, and Recovery

Every command runs in one Ecto transaction that updates current state, increments revisions, and appends activity. Project-local sequence allocation is performed transactionally and remains safe under concurrent creation.

After commit, the event broadcaster notifies connected SSE processes. `GET /api/v1/events` accepts `Last-Event-ID`, replays later activity rows, and then streams new IDs. The client treats an event as an invalidation signal and refetches canonical state rather than applying event payloads locally.

Attachment upload first copies to a temporary file and computes metadata. The final managed-file move and database insert are coordinated so failed database operations remove the temporary file. Startup cleanup removes stale temporary files and reports managed files with no database row.

On startup, the server removes a stale socket owned by the current user before binding. A missing PostgreSQL connection makes `/health` unhealthy and prevents command processing. The React application shows a retryable unavailable state. The MCP adapter reports a typed connection error if the Elixir application is not running.

## Bootstrap and Local Operation

- `mix autoboard.setup` creates or migrates PostgreSQL and creates the managed data and attachment directories.
- `mix autoboard.token.create --actor codex` prints the global Codex token once.
- The Elixir release starts Bandit on loopback and binds the Unix socket.
- Codex config launches `node mcp/dist/main.js` over stdio with `AUTOBOARD_SOCKET` and `AUTOBOARD_TOKEN` supplied from the environment.
- The production build compiles `web`, copies its assets into the Elixir release, and builds `mcp` as a separate Node executable bundle.

The system does not require the MCP adapter to be running for the UI to work. It does not require the browser to be open for MCP mutations to work.

## Testing Strategy

### Elixir domain and persistence

- Project key immutability and concurrent project-local numbering.
- Ticket defaults, status changes, terminal-state guards, and revision increments.
- One-level subtask enforcement and parent terminal guards.
- Dependency self-edge, duplicate, cycle, blocking, and resolution behavior.
- Label uniqueness, comment actor attribution, attachment metadata, and no hard-delete paths.
- Actionable work includes only ready, unblocked, leaf Codex tickets and excludes `me`.
- Global authorization context is required by every public domain query and command.
- State changes and activity append atomically; rolled-back work is neither broadcast nor visible.
- SSE replay, reconnect, and relevant-entity invalidation metadata.
- Attachment copy, size rejection, rollback cleanup, and startup orphan reporting.

### RPC and contract tests

- Fragmented and coalesced frames, concurrent IDs, oversized frames, malformed JSON, and disconnects.
- Mandatory initialization, bad credentials, version mismatch, and actor propagation.
- Every stable error kind and its required structured fields.
- Shared fixtures decode through Effect Schema and validate against Elixir responses.
- Writes are never automatically retried after an ambiguous connection loss.

### MCP adapter

- Tool names, schemas, annotations, server instructions, and structured result shapes.
- Correct mapping between every tool and RPC method.
- `me` exclusion and actionable-work rules remain visible in tool descriptions and initialization instructions.
- Revision conflict and validation errors give Codex actionable recovery data.
- Socket reconnect works for reads and fails safely for writes.

### React

- Project index, archived projects, project board, global triage, canceled history, and ticket deep links.
- Markdown, comments, subtasks, dependencies, attachments, activity, and computed blocking.
- Loading, empty, not-found, unavailable, and malformed-response states.
- SSE invalidates only affected data and catches up after reconnect.
- Automated assertions verify the absence of forms, editable elements, drag handlers, create controls, and mutation requests.

### End-to-end acceptance

Run PostgreSQL, Elixir, and the stdio MCP adapter. Through an MCP client:

1. Create a project and confirm it appears in `/projects`.
2. Create related tickets, a subtask, a dependency, a comment, and an attachment.
3. Confirm the project board and ticket detail render the resulting state without mutation controls.
4. Assign a ready leaf ticket to `codex` and verify `list_actionable_tickets` returns it.
5. Assign another ticket to `me` and verify it never appears as actionable.
6. Transition the Codex ticket to in-progress and done, then verify browser state updates through SSE.
7. Attempt a stale revision write and a dependency cycle and verify structured recovery errors.

The release is acceptable when this loop works without browser mutation controls and survives restarting the browser, MCP adapter, and Elixir application without losing canonical state or activity history.

## References

- [Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp)
- [Model Context Protocol TypeScript SDK](https://ts.sdk.modelcontextprotocol.io/server)
- [Effect OpenAPI JSON Schema API](https://effect-ts.github.io/effect/platform/OpenApiJsonSchema.ts.html)
