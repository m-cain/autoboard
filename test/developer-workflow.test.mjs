import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { test } from "node:test";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");

const runJust = (args, env = {}) => {
  const result = spawnSync("just", args, {
    cwd: root,
    env: { ...process.env, ...env },
    encoding: "utf8",
  });
  return {
    ...result,
    output: `${result.stdout}${result.stderr}`,
  };
};

const expectInOrder = (output, snippets) => {
  let cursor = -1;
  for (const snippet of snippets) {
    const next = output.indexOf(snippet, cursor + 1);
    assert.notEqual(
      next,
      -1,
      `missing ${JSON.stringify(snippet)} in:\n${output}`,
    );
    assert.ok(
      next > cursor,
      `${JSON.stringify(snippet)} appeared out of order`,
    );
    cursor = next;
  }
};

test("lists the complete grouped developer command surface", () => {
  const result = runJust(["--list"]);

  assert.equal(result.status, 0, result.output);
  for (const group of [
    "Bootstrap",
    "Development",
    "Build",
    "Quality",
    "Database",
    "Operations",
  ]) {
    assert.match(result.output, new RegExp(`\\[${group}\\]`));
  }

  for (const recipe of [
    "setup",
    "install",
    "playwright-install",
    "dev",
    "dev-server",
    "dev-web",
    "serve",
    "build",
    "build-contracts",
    "build-mcp",
    "build-web",
    "build-server",
    "check",
    "test",
    "test-contracts",
    "test-mcp",
    "test-web",
    "test-server",
    "test-e2e",
    "format",
    "format-check",
    "verify",
    "db-up",
    "db-down",
    "db-status",
    "db-logs",
    "db-reset",
    'token actor="codex"',
  ]) {
    assert.match(
      result.output,
      new RegExp(`(?:^|\\s)${recipe.replaceAll("-", "\\-")}`),
    );
  }
});

test("dry runs preserve bootstrap and build dependency order", () => {
  const setup = runJust(["--dry-run", "setup"]);
  assert.equal(setup.status, 0, setup.output);
  expectInOrder(setup.output, [
    "pnpm install",
    "mix deps.get",
    "docker compose up -d --wait postgres",
    "mix ecto.create",
    "mix autoboard.setup",
    "playwright install chromium",
  ]);

  const server = runJust(["--dry-run", "build-server"]);
  assert.equal(server.status, 0, server.output);
  expectInOrder(server.output, [
    "build:contracts",
    "build:web",
    "build:server",
  ]);

  const e2e = runJust(["--dry-run", "test-e2e"]);
  assert.equal(e2e.status, 0, e2e.output);
  expectInOrder(e2e.output, ["pnpm build", "pnpm test:e2e"]);
});

test("database reset refuses non-loopback database URLs before Compose starts", () => {
  const result = runJust(["--yes", "db-reset"], {
    DATABASE_URL: "ecto://autoboard:autoboard@example.com/autoboard_dev",
  });

  assert.notEqual(result.status, 0, result.output);
  assert.match(result.output, /refusing to reset a non-loopback database/i);
  assert.doesNotMatch(result.output, /docker compose up/);
  assert.doesNotMatch(result.output, /mix ecto\.reset/);
});
