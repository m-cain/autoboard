# Autoboard

Autoboard is a local, single-user project board. Its browser application is deliberately read-only: create projects, tickets, dependencies, comments, and attachments through its MCP server, then use the browser to browse the canonical state.

## Local operation

Requirements: Docker with Compose, Elixir/Erlang, Node.js with Corepack, and Just 1.36 or newer. PostgreSQL is supplied through Compose.

Prepare a fresh checkout, create a Codex credential, and start the hot-reload development stack:

```bash
just setup
just token codex
just dev
```

Open `http://localhost:5173/projects`. Vite serves the browser application with hot reload and proxies `/api` and its server-sent event stream to the Elixir server. Press Ctrl-C once to stop both development processes.

`just setup` installs the pnpm and Mix dependencies, installs the Git hooks and Playwright Chromium runtime, starts PostgreSQL, migrates it, and initializes the private data directories. It is safe to run again. Token creation stays separate because `just token me` and `just token codex` each print one new plaintext token exactly once. The database persists only a SHA-256 digest, so save the printed token in a local secret store before closing the terminal.

To build and run the integrated production release instead:

```bash
just serve
```

Open `http://127.0.0.1:4040/projects` after the release becomes healthy. The HTTP API exposes only `GET` endpoints and the UI intentionally contains no mutation controls. The MCP adapter is built separately at `mcp/dist/main.js`; it is not bundled into the Elixir release.

### Local configuration

Just automatically loads an optional, ignored root `.env` file. Variables already exported by the shell take precedence. The application and Compose defaults are:

| Variable                | Default                                              | Purpose                                           |
| ----------------------- | ---------------------------------------------------- | ------------------------------------------------- |
| `DATABASE_URL`          | `ecto://autoboard:autoboard@localhost/autoboard_dev` | PostgreSQL connection URL                         |
| `AUTOBOARD_DATA_DIR`    | `server/var`                                         | Private attachment and socket directory           |
| `AUTOBOARD_SOCKET`      | `<data-dir>/autoboard.sock`                          | Unix socket used by MCP                           |
| `AUTOBOARD_HTTP_PORT`   | `4040`                                               | Elixir HTTP port and Vite proxy target            |
| `AUTOBOARD_DB_NAME`     | `autoboard`                                          | Initial Compose database                          |
| `AUTOBOARD_DB_USER`     | `autoboard`                                          | Compose database user                             |
| `AUTOBOARD_DB_PASSWORD` | `autoboard`                                          | Compose database password                         |
| `AUTOBOARD_DB_PORT`     | `5432`                                               | Loopback PostgreSQL port                          |
| `COMPOSE_PROJECT_NAME`  | `autoboard`                                          | Stable Compose project shared by linked worktrees |

Override `COMPOSE_PROJECT_NAME`, `AUTOBOARD_DB_PORT`, and the corresponding database URLs together when a worktree needs an isolated PostgreSQL instance.

For Codex setup, see [docs/codex-mcp-config.md](docs/codex-mcp-config.md).

## Development and verification

Run `just` without arguments to see the grouped command list. The most common recipes are:

| Task                     | Command                                                                            |
| ------------------------ | ---------------------------------------------------------------------------------- |
| Start the complete stack | `just dev`                                                                         |
| Run one development app  | `just dev-server` / `just dev-web`                                                 |
| Build everything         | `just build`                                                                       |
| Build one component      | `just build-contracts`, `just build-mcp`, `just build-web`, or `just build-server` |
| Run unit tests           | `just test`                                                                        |
| Run E2E                  | `just test-e2e`                                                                    |
| Format source            | `just format`                                                                      |
| Run static checks        | `just check`                                                                       |
| Verify a handoff         | `just verify`                                                                      |

Database lifecycle recipes are `just db-up`, `just db-down`, `just db-status`, and `just db-logs`. `just db-down` preserves the named volume. `just db-reset` requires confirmation, refuses non-loopback database URLs, and then drops and recreates only the configured development database.

`just format` rewrites Just, Prettier-owned, and Mix-owned files; `just format-check` is check-only verification. Git hooks install through `just setup` or `pnpm install`. The pre-commit hook remains check-only: when it reports formatting problems, format and restage the affected files before committing.

### Underlying commands

The Just recipes compose these existing commands. They remain useful when debugging an individual layer:

```bash
corepack pnpm format
corepack pnpm format:check
docker compose up -d postgres
(cd server && MIX_ENV=test mix ecto.reset && mix format --check-formatted && mix test)
corepack pnpm check && corepack pnpm test && corepack pnpm build
corepack pnpm --filter @autoboard/e2e test
git diff --check
```

The E2E test starts its own temporary server, Unix socket, data directory, `autoboard_e2e` database, MCP child process, and Playwright browser. It leaves no token or runtime files in the repository. `just setup` installs its browser runtime; the direct equivalent is `corepack pnpm --filter @autoboard/e2e exec playwright install chromium`.
