# Codex MCP configuration

Install the local daemon first:

```bash
just install
```

The installer registers its Streamable HTTP endpoint, narrowly adds these
settings to the generated Autoboard table, and copies the personal Autoboard
skill to `~/.agents/skills/autoboard`:

```toml
[mcp_servers.autoboard]
url = "http://127.0.0.1:4040/mcp"
default_tools_approval_mode = "writes"
required = false
```

Open a new Codex task after installation or `just update-service`, then use
`$autoboard create ticket in …`. Ordinary Autoboard phrasing also invokes the
skill implicitly. If task discovery does not refresh, restart Codex as a
fallback. `just service-status` reports launchd state, fingerprints, paths,
health, schema/MCP readiness, the exact Codex registration, and whether the
personal skill is current. `just update-service` rebuilds from the recorded
checkout, atomically replaces the service, refreshes both Codex integration
artifacts, and verifies it.

Autoboard exposes 17 bounded tools and no generic command or SQL escape hatch.
The server instructions reserve tickets assigned to `me` for the human, direct
Codex to `list_actionable_tickets`, require fresh reads before
revision-checked writes, and call for confirmation before broad
reorganizations, project archival, or dependency removal.

The endpoint is local-only but has full write access to the board. It binds to
`127.0.0.1` and applies MCP host and cross-origin protection; no token is stored
in Codex configuration.
