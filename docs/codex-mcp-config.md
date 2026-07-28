# Codex MCP configuration

Install the local daemon first:

```bash
just install
```

The installer registers its Streamable HTTP endpoint and narrowly adds these
settings to the generated Autoboard table:

```toml
[mcp_servers.autoboard]
url = "http://127.0.0.1:4040/mcp"
default_tools_approval_mode = "writes"
required = false
```

Open a new Codex task after installation. `just service-status` reports launchd
state, fingerprints, paths, health, schema/MCP readiness, and the exact Codex
registration. `just update-service` rebuilds from the recorded checkout,
atomically replaces the service, and verifies it.

Autoboard exposes 17 bounded tools and no generic command or SQL escape hatch.
The six read tools are unchanged. Every one of the eleven write tools requires
`initiated_by: "me" | "codex"`; it has no default, and `performed_by` is not a
caller input because MCP always records it as `codex`.

Use `initiated_by: "me"` only for an exact Autoboard mutation the human
explicitly requested, including an unambiguous follow-up. Do not use it for a
broad outcome request. If Codex selects the mutation while pursuing a goal, use
`initiated_by: "codex"`. A subagent retains `me` only when its handoff
explicitly carries that exact human-requested mutation; otherwise it uses
`codex`.

The resulting pairs are `codex/me` for an explicitly delegated write and
`codex/codex` for an agent-selected write. `me/me` is reserved for a future
manual client and `system/system` for independent daemon work. The browser
only displays this attribution through its GET and SSE read surfaces; it does
not provide a write path.

The server instructions also reserve tickets assigned to `me` for the human,
direct Codex to `list_actionable_tickets`, require fresh reads before
revision-checked writes, and call for confirmation before broad
reorganizations, project archival, or dependency removal.

The endpoint is local-only but has full write access to the board. It binds to
`127.0.0.1` and applies MCP host and cross-origin protection; no token is stored
in Codex configuration.
