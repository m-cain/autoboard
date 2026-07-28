# Autoboard

Autoboard is a local, single-user project board. Its browser application is
deliberately read-only: create projects, tickets, dependencies, comments, and
attachments through MCP, then use the browser to inspect canonical state.

One Go daemon owns SQLite persistence, the 17 MCP tools, the read-only HTTP API,
the replayable activity stream, and the embedded React application. There is no
separate database, RPC layer, or MCP adapter.

## Local development

Requirements: Go 1.25, Node.js with Corepack, and Just 1.36 or newer.

```bash
just setup
just dev
```

Open `http://localhost:5173/projects`. Vite provides hot reload and proxies API
and SSE requests to the Go daemon on `127.0.0.1:4040`. Press Ctrl-C once to stop
both processes. Repository-run commands keep development state under the
ignored `autoboard-data/` directory.

Build and run the integrated binary with:

```bash
just serve
```

Then open `http://127.0.0.1:4040/projects`. The same binary exposes the MCP
endpoint at `http://127.0.0.1:4040/mcp`.

## macOS service

Install or update the production binary and its user LaunchAgent:

```bash
just install
```

This builds the application, installs and verifies the service, records the
source checkout for future updates, and installs both the exact Streamable HTTP
MCP registration and the personal Autoboard skill. The service starts
immediately and on login. Its database,
attachments, binary, installation record, and logs live under
`~/Library/Application Support/Autoboard`; the LaunchAgent property list lives
under `~/Library/LaunchAgents`.

Useful lifecycle commands are `just update-service`, `just service-status`,
`just restart-service`, `just stop-service`, `just start-service`, and
`just uninstall-service`. Updates rebuild from the checkout recorded at install
time. Uninstalling removes the managed binary, LaunchAgent, matching Codex
registration, and owned personal skill, but deliberately preserves the database
and attachments. `just update-service` refreshes both Codex integration
artifacts from the recorded checkout.

The Codex skill refresh and rollback primitive atomically exchanges skill
directories on macOS and Linux. The install, update, and service lifecycle
commands documented here remain macOS-only.

Start a new Codex task after installing or updating, then use the canonical
prompt `$autoboard create ticket in …`. Ordinary Autoboard phrasing also
activates the skill implicitly. If a new task does not discover the updated
skill, restart Codex and try again.

See [Codex MCP configuration](docs/codex-mcp-config.md) for client setup.

## Configuration

The daemon accepts these environment variables:

| Variable                         | Default                            | Purpose                         |
| -------------------------------- | ---------------------------------- | ------------------------------- |
| `AUTOBOARD_HTTP_PORT`            | `4040`                             | Loopback HTTP and MCP port      |
| `AUTOBOARD_DATA_DIR`             | User config directory `/Autoboard` | Database, attachments, and logs |
| `AUTOBOARD_DATABASE_PATH`        | `<data-dir>/autoboard.db`          | SQLite database path            |
| `AUTOBOARD_MAX_ATTACHMENT_BYTES` | `52428800`                         | Maximum copied attachment size  |
| `AUTOBOARD_DEVELOPMENT`          | unset                              | Permit the Vite proxy when `1`  |

The daemon always binds to `127.0.0.1`. Its MCP endpoint is intentionally a
full-write local interface, protected by peer, Host, and Origin checks rather
than application credentials. The SQLite database path must remain inside the
private data directory.

### Write attribution

Every MCP mutation requires `initiated_by`, with no default. The daemon always
records `performed_by: "codex"`; MCP callers may supply only
`initiated_by: "me"` for an exact Autoboard operation explicitly requested by
the human, or `initiated_by: "codex"` when Codex selected the operation while
pursuing a broader goal. An unambiguous follow-up to an exact request also uses
`me`; a broad outcome request does not. A subagent preserves `me` only when its
handoff carries that exact requested operation.

Attribution has two axes and accepts exactly these pairs: `me/me` (a future
manual human write), `codex/me` (an exact operation delegated to Codex),
`codex/codex` (an agent-selected operation), and `system/system` (independent
daemon work). Projects and tickets return immutable `created_attribution`;
comments, attachments, and activity events return `attribution`.

The browser presents this history but remains read-only. It has no mutation
routes or write controls: use MCP to create or change board state, then use the
browser to inspect the canonical GET and SSE read surfaces.

## Development and verification

Run `just` to see the grouped command list. The common recipes are:

| Task                        | Command                                             |
| --------------------------- | --------------------------------------------------- |
| Start daemon and Vite       | `just dev`                                          |
| Run one development process | `just dev-daemon` / `just dev-web`                  |
| Build the integrated binary | `just build`                                        |
| Build one layer             | `just build-contracts`, `build-web`, `build-daemon` |
| Run unit tests              | `just test`                                         |
| Run black-box acceptance    | `just test-e2e`                                     |
| Measure all coverage        | `just coverage`                                     |
| Measure Go coverage         | `just coverage-go`                                  |
| Measure TypeScript coverage | `just coverage-typescript`                          |
| Run pinned Go lint          | `just lint-go`                                      |
| Run the Git commit gate     | `just pre-commit`                                   |
| Format source               | `just format`                                       |
| Run static checks           | `just check`                                        |
| Verify a handoff            | `just verify`                                       |

Coverage is a required quality gate. Go must remain at least 80% covered
overall and at least 70% in every first-party package. Both
`@autoboard/contracts` and `@autoboard/web` must independently remain at least
80% covered for lines, statements, and functions and at least 75% for
branches. All first-party production source is measured; generated contracts
are the only exclusion. Tests must exercise observable behavior rather than
importing code solely to improve the number. Do not lower thresholds or broaden
exclusions without explicit approval.

`just lint-go` installs and runs the repository-pinned golangci-lint v2.12.2
binary from `.tools/bin`; no global linter is required. Husky invokes
`just pre-commit`, which checks formatting and generated contracts, runs static
analysis and unit tests, and enforces both coverage policies.

`just verify` includes the commit gate, Go race tests, production builds, and
the isolated MCP-to-browser Playwright scenario. The E2E runner creates a
unique temporary SQLite database and data directory, restarts the daemon to
verify persistence and SSE replay, and removes only the temporary resources it
owns.
