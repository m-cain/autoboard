# Codex MCP configuration

Build Autoboard and issue a Codex credential first:

```bash
docker compose up -d postgres
(cd server && mix autoboard.setup)
(cd server && mix autoboard.token.create --actor codex)
corepack pnpm build
```

Store the printed token outside this repository. Then configure a local stdio MCP server with an absolute adapter path and the private Unix socket/token values. In a Codex configuration file, the shape is:

```toml
[mcp_servers.autoboard]
command = "node"
args = ["/absolute/path/to/autoboard/mcp/dist/main.js"]
env = { AUTOBOARD_SOCKET = "/absolute/path/to/autoboard/server/var/autoboard.sock", AUTOBOARD_TOKEN = "ab_replace_with_the_saved_token" }
default_tools_approval_mode = "writes"
```

Start the local server separately:

```bash
(cd server && _build/prod/rel/autoboard/bin/autoboard start)
```

The v1 credential model is global: a token can access the entire board. A future project-scoped credential will keep the same MCP surface while the server filters project-rooted queries through its authorization context. The browser HTTP interface is loopback-only and read-only; all creates and edits go through MCP.

The adapter’s own server instructions treat tickets assigned to `me` as reserved for the human. Codex should work from `list_actionable_tickets`, read the latest entity before revision-checked writes, and seek confirmation for broad reorganizations, project archival, or dependency removal. `default_tools_approval_mode = "writes"` makes that confirmation posture visible in Codex while still allowing direct writes after approval.
