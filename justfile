set dotenv-load := true

export AUTOBOARD_HTTP_PORT := env_var_or_default("AUTOBOARD_HTTP_PORT", "4040")
export AUTOBOARD_DATA_DIR := env_var_or_default("AUTOBOARD_DATA_DIR", "autoboard-data")

[private]
default:
    @just --list

# Install development dependencies and Git hooks.
[group('Bootstrap')]
dependencies:
    corepack pnpm install
    corepack pnpm prepare
    go mod download
    ./scripts/install-golangci-lint.sh

# Install the repository-pinned golangci-lint binary.
[group('Bootstrap')]
install-golangci-lint:
    ./scripts/install-golangci-lint.sh

# Install the Chromium runtime used by the end-to-end suite.
[group('Bootstrap')]
playwright-install:
    corepack pnpm --filter @autoboard/e2e exec playwright install chromium

# Fully prepare a fresh checkout for local development.
[group('Bootstrap')]
setup: dependencies playwright-install

# Start the Go daemon and Vite together, stopping both on exit.
[group('Development')]
dev: build-contracts
    go run ./cmd/autoboard-dev

# Run only the Go daemon.
[group('Development')]
dev-daemon:
    go run ./cmd/autoboard serve

# Run only the Vite development server.
[group('Development')]
dev-web: build-contracts
    corepack pnpm --filter @autoboard/web dev

# Build and run the integrated production binary.
[group('Development')]
serve: build
    ./dist/autoboard serve

# Build the contracts, browser, and integrated Go binary.
[group('Build')]
build:
    corepack pnpm build

# Generate and compile the browser contracts from Go response DTOs.
[group('Build')]
build-contracts:
    corepack pnpm build:contracts

# Build the browser application after its contracts.
[group('Build')]
build-web: build-contracts
    corepack pnpm build:web

# Embed the current browser build and compile the Go daemon.
[group('Build')]
build-daemon: build-web
    corepack pnpm build:daemon

# Run formatting, contract freshness, type, vet, workflow, and browser lint checks.
[group('Quality')]
check:
    just --unstable --fmt --check
    corepack pnpm check:contracts
    corepack pnpm build:contracts
    corepack pnpm check
    corepack pnpm --filter @autoboard/web lint
    go vet ./...
    just lint-go
    node --test test/developer-workflow.test.mjs

# Run all unit and developer-workflow tests.
[group('Quality')]
test: test-contracts test-web test-go test-workflow

# Run generated browser contract tests.
[group('Quality')]
test-contracts:
    corepack pnpm --filter @autoboard/contracts test

# Run browser application tests.
[group('Quality')]
test-web: build-contracts
    corepack pnpm --filter @autoboard/web test

# Run Go unit tests.
[group('Quality')]
test-go:
    go test ./...

# Run Go tests with data race checking.
[group('Quality')]
test-go-race:
    go test -race ./...

# Validate the Just developer command surface.
[group('Quality')]
test-workflow:
    node --test test/developer-workflow.test.mjs test/go-coverage-check.test.mjs

# Enforce Go statement coverage globally and per package.
[group('Quality')]
coverage-go:
    node scripts/check-go-coverage.mjs

# Enforce line, statement, function, and branch coverage in TypeScript workspaces.
[group('Quality')]
coverage-typescript: build-contracts
    corepack pnpm --filter @autoboard/contracts coverage
    corepack pnpm --filter @autoboard/web coverage

# Enforce all Go and TypeScript coverage thresholds.
[group('Quality')]
coverage: coverage-go coverage-typescript

# Validate and run the repository-pinned comprehensive Go linter.
[group('Quality')]
lint-go: install-golangci-lint
    .tools/bin/golangci-lint config verify
    .tools/bin/golangci-lint run ./...

# Run the complete local commit gate used by Husky.
[group('Quality')]
pre-commit: format-check check test coverage

# Build and run the isolated MCP-to-browser black-box suite.
[group('Quality')]
test-e2e: build
    corepack pnpm test:e2e

# Format Just, Prettier-owned files, and Go source.
[group('Quality')]
format:
    just --unstable --fmt
    corepack pnpm format

# Check Just, Prettier, and Go formatting without rewriting files.
[group('Quality')]
format-check:
    just --unstable --fmt --check
    corepack pnpm format:check

# Run the complete handoff verification suite.
[group('Quality')]
verify: pre-commit test-go-race test-e2e
    git diff --check

# Build, install, verify, and register the macOS service with Codex.
[group('Operations')]
install: dependencies build
    ./dist/autoboard install

# Backward-compatible alias for the complete installation.
[group('Operations')]
install-service: install

# Rebuild from the recorded checkout and atomically update the service.
[group('Operations')]
update-service:
    "$HOME/Library/Application Support/Autoboard/bin/autoboard" update

# Unload the LaunchAgent and remove its runtime files while preserving data.
[group('Operations')]
uninstall-service:
    go run ./cmd/autoboard uninstall

# Load the installed macOS LaunchAgent.
[group('Operations')]
start-service:
    go run ./cmd/autoboard start

# Unload the installed macOS LaunchAgent.
[group('Operations')]
stop-service:
    go run ./cmd/autoboard stop

# Reload the installed macOS LaunchAgent.
[group('Operations')]
restart-service:
    go run ./cmd/autoboard restart

# Print launchd status for the installed daemon.
[group('Operations')]
service-status:
    go run ./cmd/autoboard status
