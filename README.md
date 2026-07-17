# Autoboard

Autoboard is a local, single-user project board. Its browser application is deliberately read-only: create projects, tickets, dependencies, comments, and attachments through its MCP server, then use the browser to browse the canonical state.

## Local operation

Requirements: Docker, Elixir/Erlang, Node.js with Corepack, and PostgreSQL available through the supplied Compose service.

```bash
docker compose up -d postgres
(cd server && mix autoboard.setup)
(cd server && mix autoboard.token.create --actor codex)
corepack pnpm build
(cd server && _build/prod/rel/autoboard/bin/autoboard start)
```

`mix autoboard.setup` is idempotent. It migrates PostgreSQL and creates the managed data, attachment, and temporary-attachment directories with owner-only (`0700`) permissions. `mix autoboard.token.create --actor me` and `--actor codex` accept no other actors; each prints one new plaintext token exactly once. The database persists only a SHA-256 digest, so save the printed token in a local secret store before closing the terminal.

The release reads these optional environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | `ecto://autoboard:autoboard@localhost/autoboard_dev` | PostgreSQL connection URL |
| `AUTOBOARD_DATA_DIR` | `server/var` | Private attachment and socket directory |
| `AUTOBOARD_SOCKET` | `<data-dir>/autoboard.sock` | Unix socket used by MCP |
| `AUTOBOARD_HTTP_PORT` | `4040` | Loopback-only HTTP port |

Open `http://127.0.0.1:4040/projects` after the release becomes healthy. The HTTP API exposes only `GET` endpoints and the UI intentionally contains no mutation controls. The MCP adapter is built separately at `mcp/dist/main.js`; it is not bundled into the Elixir release.

For Codex setup, see [docs/codex-mcp-config.md](docs/codex-mcp-config.md).

## Development and verification

```bash
docker compose up -d postgres
(cd server && MIX_ENV=test mix ecto.reset && mix format --check-formatted && mix test)
corepack pnpm check && corepack pnpm test && corepack pnpm build
corepack pnpm --filter @autoboard/e2e test
git diff --check
```

The last test starts its own temporary server, Unix socket, data directory, `autoboard_e2e` database, MCP child process, and Playwright browser. It leaves no token or runtime files in the repository. Install the browser runtime once with `corepack pnpm --filter @autoboard/e2e exec playwright install chromium` when it is not already available.
