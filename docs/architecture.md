# Architecture

Autoboard has one non-client process:

```text
Codex -- Streamable HTTP MCP --\
                                Go daemon -- SQLite WAL
Browser -- GET API and SSE ----/     |
                                      `-- managed attachments
```

The daemon binds only to `127.0.0.1`. A standard-library `http.ServeMux`
dispatches `/mcp`, `/api/v1/*`, `/health`, and the embedded React SPA. There is
no secondary web framework, private RPC protocol, adapter process, external
database, or container runtime.

## Boundaries

- `internal/app` owns domain rules, optimistic revisions, dependency safety,
  attachment management, activity, read models, and the validated two-axis
  `Attribution` value object.
- `internal/store` opens SQLite with WAL, foreign keys, normal synchronous
  writes, a busy timeout, and embedded Goose migrations.
- `internal/mcpapi` exposes exactly 17 official-SDK tools with strict input and
  output schemas, annotations, structured output, and repairable tool errors.
- `internal/httpapi` exposes GET/HEAD reads, attachment downloads, health, and
  replayable SSE. No HTTP mutation route exists.
- `internal/contracts` derives browser response schemas from the Go DTOs. The
  TypeScript package infers types from those schemas and validates responses
  without runtime code generation.
- `internal/webui` serves immutable hashed assets, SPA fallbacks, and a strict
  content security policy.
- `internal/launchagent` atomically installs the binary and manages one
  per-user macOS LaunchAgent.
- `internal/installation` owns the recorded-checkout update path, binary
  fingerprints, exact Codex MCP registration, and ownership-safe installation
  of the personal Autoboard skill. The lifecycle transaction preflights both
  Codex artifacts before changing the LaunchAgent, then restores their prior
  state if a later installation step fails. Its fail-closed skill transaction
  uses Darwin `renameatx_np(RENAME_SWAP)` or Linux
  `renameat2(RENAME_EXCHANGE)` for atomic directory exchange.

The daemon rejects non-loopback peers, unexpected Host values, and unexpected
origins across every route. Development mode adds only the two Vite loopback
origins. Request bodies are bounded, ordinary requests have deadlines, panics
are recovered with correlation IDs, and request logs are structured.

SQLite is canonical. Writes are serialized in-process and committed activity
rows are the durable SSE replay log. Browser events are invalidation signals;
the client refetches canonical read models rather than applying event payloads
locally.

## Write attribution

`Attribution` is a pair of `performed_by` and `initiated_by` principals. The
only valid pairs are `me/me`, `codex/me`, `codex/codex`, and `system/system`.
Projects and tickets persist immutable creation attribution as
`created_attribution`; comments, attachments, and activity events persist
operation attribution as `attribution`. A single mutation's attribution is
also used by every derived activity event it causes.

All eleven MCP write tools require `initiated_by: "me" | "codex"`, with no
default, and the MCP adapter constructs `performed_by: "codex"`. `me` means
the human explicitly selected that exact Autoboard mutation; a broader outcome
request, or an operation selected by Codex or its subagent, uses `codex`.
Subagents retain `me` only when the handoff explicitly carries the exact human
request. The `me/me` pair is reserved for a future manual client, while
`system/system` is reserved for independent daemon work.

This attribution data flows through MCP responses, GET APIs, SSE activity, and
generated browser contracts. It does not change the HTTP boundary: the browser
remains a GET/HEAD and SSE consumer, and no HTTP mutation route exists.
