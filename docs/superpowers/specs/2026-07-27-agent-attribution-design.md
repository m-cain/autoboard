# Agent Attribution Design

**Status:** Approved in conversation on 2026-07-27

## Summary

Autoboard will distinguish who performed a write from whose intent initiated
it. This separates a future manual human action, an exact Autoboard operation
delegated to Codex, and an operation Codex or one of its subagents chose while
pursuing a broader goal.

The model applies to every write. Project and ticket creation attribution is
first-class on the entities, while every mutation also records attribution in
the immutable activity log. Comments and attachments retain their own
attribution.

## Domain model

`Principal` has the values `me`, `codex`, and `system`.

`Attribution` contains:

- `performed_by`: the principal that executed the write.
- `initiated_by`: the principal whose intent selected the exact Autoboard
  operation.

The valid pairs are:

| Performed by | Initiated by | Meaning                               |
| ------------ | ------------ | ------------------------------------- |
| `me`         | `me`         | Future manual human action            |
| `codex`      | `me`         | Exact operation delegated to Codex    |
| `codex`      | `codex`      | Agent-selected operation              |
| `system`     | `system`     | Independent daemon-generated activity |

An operation's attribution is propagated to every activity event caused by
that operation, including derived dependency activity. `system/system` is
reserved for independent daemon work.

Projects and tickets expose `created_attribution`. Comments, attachments, and
activity events expose `attribution`.

## MCP behavior

All eleven write tools require `initiated_by` with the value `me` or `codex`.
There is no default. The MCP server fixes `performed_by` to `codex`; callers
cannot impersonate a future manual action or the system.

Use `initiated_by: "me"` only when the human explicitly requested that exact
Autoboard mutation, including an unambiguous follow-up to such a request. A
broader outcome request does not qualify. When Codex decides that creating or
changing Autoboard state would help accomplish a broader goal, it uses
`initiated_by: "codex"`.

A subagent preserves `me` only when its handoff explicitly carries the exact
human-requested Autoboard operation. If an agent chose the operation or chose
to ask a subagent to perform it, the value is `codex`.

The six read tools and ticket assignee semantics are unchanged.

## Persistence and read surfaces

The installed board is unused and was explicitly approved for reset. The
installed LaunchAgent is stopped before implementation, and only the SQLite
database, WAL, and shared-memory files are removed. The empty attachment store
is preserved. The daemon remains stopped until the verified replacement is
installed.

Because no data is retained, the initial migration is revised directly. There
is no compatibility migration, legacy attribution value, fallback, or
backfill.

Generated Go-to-TypeScript contracts require the new attribution fields across
MCP output, GET APIs, SSE activity, TypeScript types, and runtime decoding.

The read-only UI uses one shared presentation rule:

- `me/me` → `me`
- `codex/me` → `me via Codex`
- `codex/codex` → `Codex`
- `system/system` → `system`

Attribution appears on project rows and headers, ticket cards and details,
comments, attachments, and activity.

## Boundaries

This change does not add named agent records, delegation chains, writable
browser routes, HTTP mutations, new assignees, or application approval state.
The two-axis attribution model is sufficient for the current single-user
system and leaves room for a future manual client without pretending MCP writes
were manual.

## Acceptance

- Every domain mutation rejects invalid attribution before changing state.
- Projects and tickets persist immutable creation attribution.
- Comments, attachments, and every activity event persist operation
  attribution.
- Every MCP write requires a valid initiator and always records Codex as the
  performer.
- Delegated and autonomous writes remain distinguishable after daemon restart.
- Generated contracts and browser decoding reject missing or invalid
  attribution.
- Every relevant read-only UI surface renders the canonical label.
- Repository formatting, coverage, lint, pre-commit, race, build, and E2E
  gates pass.
- The reinstalled production service starts with schema version 1, no projects,
  and no activity.
