set dotenv-load := true

export AUTOBOARD_HTTP_PORT := env_var_or_default("AUTOBOARD_HTTP_PORT", "4040")
export COMPOSE_PROJECT_NAME := env_var_or_default("COMPOSE_PROJECT_NAME", "autoboard")

[private]
default:
    @just --list

[private]
_database-setup: db-up
    cd server && mix ecto.create
    cd server && mix autoboard.setup

# Install JavaScript, Elixir, and Git hook dependencies.
[group('Bootstrap')]
install:
    pnpm install
    cd server && mix deps.get
    pnpm prepare

# Install the Chromium runtime used by the end-to-end suite.
[group('Bootstrap')]
playwright-install:
    pnpm --filter @autoboard/e2e exec playwright install chromium

# Fully prepare a fresh checkout for local development.
[group('Bootstrap')]
setup: install _database-setup playwright-install

# Start the Elixir server and Vite together, stopping both on exit.
[group('Development')]
dev: _database-setup build-contracts
    #!/usr/bin/env node
    const { spawn } = require("node:child_process");
    const { join } = require("node:path");

    const detached = process.platform !== "win32";
    const children = [
      spawn("mix", ["run", "--no-halt"], {
        cwd: join(process.cwd(), "server"),
        detached,
        env: process.env,
        stdio: "inherit",
      }),
      spawn("pnpm", ["--filter", "@autoboard/web", "dev"], {
        cwd: process.cwd(),
        detached,
        env: process.env,
        stdio: "inherit",
      }),
    ];
    let stopping = false;

    const waitForExit = (child) =>
      child.exitCode !== null || child.signalCode !== null
        ? Promise.resolve()
        : new Promise((resolve) => child.once("exit", resolve));
    const signal = (child, name) => {
      if (!child.pid || child.exitCode !== null || child.signalCode !== null) return;
      try {
        if (detached) process.kill(-child.pid, name);
        else child.kill(name);
      } catch {
        // The process may have exited between the state check and the signal.
      }
    };
    const stop = async (code, message) => {
      if (stopping) return;
      stopping = true;
      if (message) console.error(message);
      for (const child of children) signal(child, "SIGTERM");
      const result = await Promise.race([
        Promise.all(children.map(waitForExit)),
        new Promise((resolve) => setTimeout(() => resolve("timeout"), 5_000)),
      ]);
      if (result === "timeout") {
        for (const child of children) signal(child, "SIGKILL");
        await Promise.all(children.map(waitForExit));
      }
      process.exit(code);
    };

    process.once("SIGINT", () => void stop(130));
    process.once("SIGTERM", () => void stop(143));
    process.once("SIGHUP", () => void stop(143));
    for (const child of children) {
      child.once("error", (error) =>
        void stop(1, `Failed to start a development process: ${error.message}`),
      );
      child.once("exit", (code, signalName) => {
        if (!stopping) {
          void stop(
            code === 0 ? 1 : (code ?? 1),
            `A development process exited unexpectedly (${signalName ?? code}); stopping its peer.`,
          );
        }
      });
    }

# Run only the Elixir development server.
[group('Development')]
dev-server: _database-setup
    cd server && mix run --no-halt

# Run only the Vite development server.
[group('Development')]
dev-web: build-contracts
    pnpm --filter @autoboard/web dev

# Build and run the integrated production release.
[group('Development')]
serve: _database-setup build
    cd server && _build/prod/rel/autoboard/bin/autoboard start

# Build every application and the integrated release.
[group('Build')]
build:
    pnpm build

# Build the shared TypeScript contracts.
[group('Build')]
build-contracts:
    pnpm build:contracts

# Build the MCP adapter after its contracts.
[group('Build')]
build-mcp: build-contracts
    pnpm build:mcp

# Build the browser application after its contracts.
[group('Build')]
build-web: build-contracts
    pnpm build:web

# Build the Elixir release with current browser assets.
[group('Build')]
build-server: build-web
    pnpm build:server

# Run formatting, type, workflow, and web lint checks.
[group('Quality')]
check: build-contracts
    just --unstable --fmt --check
    pnpm check
    pnpm --filter @autoboard/web lint
    node --test test/developer-workflow.test.mjs

# Run all unit and developer-workflow tests.
[group('Quality')]
test: test-contracts test-mcp test-web test-server test-workflow

# Run shared contract tests.
[group('Quality')]
test-contracts:
    pnpm --filter @autoboard/contracts test

# Run MCP adapter tests.
[group('Quality')]
test-mcp:
    pnpm --filter @autoboard/mcp test

# Run browser application tests.
[group('Quality')]
test-web:
    pnpm --filter @autoboard/web test

# Reset the isolated test database and run Elixir tests.
[group('Quality')]
test-server: db-up
    cd server && MIX_ENV=test mix ecto.reset && mix test

# Validate the Just developer command surface.
[group('Quality')]
test-workflow:
    node --test test/developer-workflow.test.mjs

# Build and run the black-box end-to-end suite.
[group('Quality')]
test-e2e: db-up build
    pnpm test:e2e

# Format Just, Prettier-owned, and Mix-owned files.
[group('Quality')]
format:
    just --unstable --fmt
    pnpm format

# Check Just, Prettier, and Mix formatting without rewriting files.
[group('Quality')]
format-check:
    just --unstable --fmt --check
    pnpm format:check

# Run the complete handoff verification suite.
[group('Quality')]
verify: format-check check test test-e2e
    git diff --check

# Start PostgreSQL and wait for its health check.
[group('Database')]
db-up:
    docker compose up -d --wait postgres

# Stop local Compose services without deleting their volumes.
[group('Database')]
db-down:
    docker compose down

# Show local Compose service status.
[group('Database')]
db-status:
    docker compose ps

# Follow PostgreSQL logs.
[group('Database')]
db-logs:
    docker compose logs --follow postgres

# Drop, recreate, migrate, and initialize only a loopback development database.
[confirm('Drop and recreate the configured local development database?')]
[group('Database')]
db-reset:
    #!/usr/bin/env sh
    set -eu

    database_url="${DATABASE_URL:-ecto://autoboard:autoboard@localhost/autoboard_dev}"
    DATABASE_URL="$database_url" node <<'NODE'
    let url;
    try {
      url = new URL(process.env.DATABASE_URL.replace(/^ecto:/, "postgres:"));
    } catch {
      console.error("Refusing to reset an invalid DATABASE_URL.");
      process.exit(1);
    }
    const loopback = new Set(["localhost", "127.0.0.1", "::1", "[::1]"]);
    if (!loopback.has(url.hostname)) {
      console.error(
        `Refusing to reset a non-loopback database host: ${url.hostname}`,
      );
      process.exit(1);
    }
    NODE

    just db-up
    cd server
    DATABASE_URL="$database_url" MIX_ENV=dev mix ecto.reset
    DATABASE_URL="$database_url" MIX_ENV=dev mix autoboard.setup

# Create a one-time local access token for `codex` or `me`.
[group('Operations')]
token actor="codex":
    cd server && mix autoboard.token.create --actor "{{ actor }}"
