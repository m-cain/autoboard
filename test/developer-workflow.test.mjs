import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
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
    "Operations",
  ]) {
    assert.match(result.output, new RegExp(`\\[${group}\\]`));
  }

  for (const recipe of [
    "setup",
    "dependencies",
    "install",
    "playwright-install",
    "dev",
    "dev-daemon",
    "dev-web",
    "serve",
    "build",
    "build-contracts",
    "build-web",
    "build-daemon",
    "check",
    "test",
    "test-contracts",
    "test-web",
    "test-go",
    "test-e2e",
    "coverage",
    "coverage-go",
    "coverage-typescript",
    "lint-go",
    "pre-commit",
    "format",
    "format-check",
    "verify",
    "install-service",
    "update-service",
    "uninstall-service",
    "start-service",
    "stop-service",
    "restart-service",
    "service-status",
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
    "go mod download",
    "playwright install chromium",
  ]);

  const install = runJust(["--dry-run", "install"]);
  assert.equal(install.status, 0, install.output);
  expectInOrder(install.output, [
    "pnpm install",
    "go mod download",
    "pnpm build",
    "./dist/autoboard install",
  ]);

  const daemon = runJust(["--dry-run", "build-daemon"]);
  assert.equal(daemon.status, 0, daemon.output);
  expectInOrder(daemon.output, [
    "build:contracts",
    "build:web",
    "build:daemon",
  ]);

  const e2e = runJust(["--dry-run", "test-e2e"]);
  assert.equal(e2e.status, 0, e2e.output);
  expectInOrder(e2e.output, ["pnpm build", "pnpm test:e2e"]);
});

test("TypeScript consumers build fresh contracts first", () => {
  const check = runJust(["--dry-run", "check"]);
  assert.equal(check.status, 0, check.output);
  expectInOrder(check.output, [
    "pnpm check:contracts",
    "pnpm build:contracts",
    "pnpm check",
  ]);

  const web = runJust(["--dry-run", "test-web"]);
  assert.equal(web.status, 0, web.output);
  expectInOrder(web.output, [
    "pnpm build:contracts",
    "--filter @autoboard/web test",
  ]);

  const coverage = runJust(["--dry-run", "coverage-typescript"]);
  assert.equal(coverage.status, 0, coverage.output);
  expectInOrder(coverage.output, [
    "pnpm build:contracts",
    "--filter @autoboard/contracts coverage",
    "--filter @autoboard/web coverage",
  ]);
});

test("workflow contains no legacy database, Elixir, or adapter lifecycle", async () => {
  const source = await readFile(resolve(root, "justfile"), "utf8");
  for (const legacy of [
    "docker compose",
    "mix ",
    "ecto",
    "build-mcp",
    "test-mcp",
    "db-up",
    "probe-entrypoint",
  ]) {
    assert.doesNotMatch(source, new RegExp(legacy, "i"));
  }
});

test("verification and Husky enforce the canonical quality gates", async () => {
  const verify = runJust(["--dry-run", "verify"]);
  assert.equal(verify.status, 0, verify.output);
  for (const command of [
    "just lint-go",
    "scripts/check-go-coverage.mjs",
    "--filter @autoboard/contracts coverage",
    "--filter @autoboard/web coverage",
  ]) {
    assert.match(verify.output, new RegExp(command.replaceAll(".", "\\.")));
  }

  const lint = runJust(["--dry-run", "lint-go"]);
  assert.equal(lint.status, 0, lint.output);
  assert.match(lint.output, /\.tools\/bin\/golangci-lint config verify/);
  assert.match(lint.output, /\.tools\/bin\/golangci-lint run \.\/\.\.\./);

  const hook = await readFile(resolve(root, ".husky/pre-commit"), "utf8");
  assert.equal(hook, "#!/usr/bin/env sh\njust pre-commit\n");

  const version = await readFile(resolve(root, ".golangci-version"), "utf8");
  assert.equal(version, "v2.12.2\n");

  const goCoverage = await readFile(
    resolve(root, "scripts/check-go-coverage.mjs"),
    "utf8",
  );
  assert.match(goCoverage, /"-count=1"/);
});

test("agent and developer guidance preserve the coverage policy", async () => {
  const agentGuidance = await readFile(resolve(root, "AGENTS.md"), "utf8");
  for (const requirement of [
    "80% overall",
    "70% in every first-party package",
    "80% for lines, statements, and functions",
    "75% for branches",
    "Generated contracts are the only exclusion",
    "Do not lower a coverage threshold",
    "just coverage-go",
    "just coverage-typescript",
    "just lint-go",
    "just pre-commit",
    "just verify",
  ]) {
    assert.match(agentGuidance, new RegExp(requirement));
  }

  const readme = await readFile(resolve(root, "README.md"), "utf8");
  for (const command of [
    "just coverage",
    "just coverage-go",
    "just coverage-typescript",
    "just lint-go",
    "just pre-commit",
    "just verify",
  ]) {
    assert.match(readme, new RegExp(command));
  }
});
