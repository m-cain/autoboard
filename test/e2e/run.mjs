import { createServer } from "node:http";
import { createServer as createTcpServer } from "node:net";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { randomBytes } from "node:crypto";
import { spawn } from "node:child_process";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const root = resolve(import.meta.dirname, "../..");
const serverDir = join(root, "server");
const lockPath = join(tmpdir(), "autoboard-e2e.lock");
const failureLog = join(root, "output/playwright/autoboard-e2e-server.log");
const owned = new Set();
const lockNonce = randomBytes(16).toString("hex");
let ownsLock = false;
let tempDir;
let control;
let controlUrl;
let controlToken;
let server;
let port;
let serverOutput = "";
let cleaning = false;

const delay = (milliseconds) =>
  new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));
const isRunning = (pid) => {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};
const capture = (chunk) => {
  serverOutput += chunk.toString();
  if (serverOutput.length > 200_000)
    serverOutput = serverOutput.slice(-200_000);
};
const signalGroup = (child, signal) => {
  if (!child?.pid) return;
  try {
    process.kill(-child.pid, signal);
  } catch {
    try {
      child.kill(signal);
    } catch {
      /* already exited */
    }
  }
};
const waitForExit = (child, milliseconds) =>
  new Promise((resolveExit) => {
    if (!child || child.exitCode !== null || child.signalCode !== null)
      return resolveExit(true);
    const timeout = setTimeout(() => resolveExit(false), milliseconds);
    child.once("close", () => {
      clearTimeout(timeout);
      resolveExit(true);
    });
  });
const stop = async (child) => {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  signalGroup(child, "SIGTERM");
  if (await waitForExit(child, 5_000)) return;
  signalGroup(child, "SIGKILL");
  await waitForExit(child, 5_000);
};
const spawnOwned = (command, args, options) => {
  const child = spawn(command, args, { ...options, detached: true });
  owned.add(child);
  child.once("close", () => owned.delete(child));
  return child;
};

const run = async (
  command,
  args,
  { cwd = root, env, timeoutMs = 60_000 } = {},
) => {
  const child = spawnOwned(command, args, {
    cwd,
    env,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stdout = "";
  let stderr = "";
  child.stdout.on("data", (chunk) => {
    stdout += chunk;
  });
  child.stderr.on("data", (chunk) => {
    stderr += chunk;
  });
  const result = await Promise.race([
    new Promise((resolveResult, rejectResult) => {
      child.once("error", rejectResult);
      child.once("close", (code, signal) => resolveResult({ code, signal }));
    }),
    delay(timeoutMs).then(() => ({ timeout: true })),
  ]);
  if ("timeout" in result) {
    await stop(child);
    throw new Error(
      `${command} ${args.join(" ")} timed out after ${timeoutMs}ms`,
    );
  }
  if (result.code !== 0)
    throw new Error(
      `${command} ${args.join(" ")} exited with ${result.code ?? result.signal}${stderr ? `: ${scrub(stderr)}` : ""}`,
    );
  return { stdout, stderr };
};

const scrub = (value) =>
  value.replace(/ab_[A-Za-z0-9_-]+/g, "[REDACTED_TOKEN]");
const writeFailureLog = async () => {
  if (!serverOutput) return;
  await mkdir(join(root, "output/playwright"), { recursive: true });
  await writeFile(failureLog, scrub(serverOutput), { mode: 0o600 });
};

const acquireLock = async () => {
  try {
    await mkdir(lockPath, { mode: 0o700 });
  } catch (error) {
    if (error?.code !== "EEXIST") throw error;
    let record;
    try {
      record = JSON.parse(await readFile(join(lockPath, "owner.json"), "utf8"));
    } catch {
      throw new Error(
        `Autoboard e2e lock is present but untrusted: ${lockPath}. Remove it manually after verifying no runner is active.`,
      );
    }
    if (typeof record?.pid !== "number")
      throw new Error(
        `Autoboard e2e lock is malformed: ${lockPath}. Remove it manually after verifying no runner is active.`,
      );
    if (isRunning(record.pid))
      throw new Error(
        `Another Autoboard e2e runner owns ${lockPath}; refusing before ecto.reset.`,
      );
    throw new Error(
      `Autoboard e2e lock is stale: ${lockPath}. Remove it manually after verifying no runner is active.`,
    );
  }
  await writeFile(
    join(lockPath, "owner.json"),
    JSON.stringify({ pid: process.pid, nonce: lockNonce }),
    { mode: 0o600 },
  );
  ownsLock = true;
};
const releaseLock = async () => {
  if (!ownsLock) return;
  try {
    const record = JSON.parse(
      await readFile(join(lockPath, "owner.json"), "utf8"),
    );
    if (record?.pid === process.pid && record?.nonce === lockNonce)
      await rm(lockPath, { recursive: true, force: true });
  } finally {
    ownsLock = false;
  }
};

async function reservePort() {
  const listener = createTcpServer();
  await new Promise((resolveListen, rejectListen) => {
    listener.once("error", rejectListen);
    listener.listen({ host: "127.0.0.1", port: 0 }, resolveListen);
  });
  const address = listener.address();
  await new Promise((resolveClose, rejectClose) =>
    listener.close((error) => (error ? rejectClose(error) : resolveClose())),
  );
  if (!address || typeof address === "string")
    throw new Error("could not reserve a loopback port");
  return address.port;
}

const environment = () => ({
  ...process.env,
  DATABASE_URL: "ecto://autoboard:autoboard@localhost/autoboard_e2e",
  AUTOBOARD_DATA_DIR: join(tempDir, "data"),
  AUTOBOARD_SOCKET: join(tempDir, "autoboard.sock"),
  AUTOBOARD_HTTP_PORT: String(port),
  MIX_ENV: "dev",
});
const waitForHealth = async (milliseconds = 30_000) => {
  const url = `http://127.0.0.1:${port}/health`;
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      const body = await response.json();
      if (response.status === 200 && body.status === "ok") return;
    } catch {
      /* process has not bound the port yet */
    }
    await delay(150);
  }
  throw new Error(`Autoboard did not become healthy at ${url}`);
};
const startServer = async ({ retryPort = true } = {}) => {
  const attempts = retryPort ? 3 : 1;
  let lastError;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (retryPort || port === undefined) port = await reservePort();
    const child = spawnOwned("mix", ["run", "--no-halt"], {
      cwd: serverDir,
      env: environment(),
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.stdout.on("data", capture);
    child.stderr.on("data", capture);
    server = child;
    try {
      await waitForHealth();
      return;
    } catch (error) {
      lastError = error;
      await stop(child);
      server = undefined;
    }
  }
  throw lastError;
};
const restartServer = async () => {
  await stop(server);
  server = undefined;
  await startServer({ retryPort: false });
};
const startControl = async () => {
  controlToken = randomBytes(24).toString("hex");
  control = createServer(async (request, response) => {
    if (
      request.method !== "POST" ||
      request.url !== "/restart" ||
      request.headers["x-autoboard-e2e-control"] !== controlToken
    ) {
      response.writeHead(404).end();
      return;
    }
    try {
      await restartServer();
      response.writeHead(204).end();
    } catch (error) {
      response.writeHead(500).end(scrub(String(error)));
    }
  });
  await new Promise((resolveListen, rejectListen) => {
    control.once("error", rejectListen);
    control.listen({ host: "127.0.0.1", port: 0 }, resolveListen);
  });
  const address = control.address();
  if (!address || typeof address === "string")
    throw new Error("could not bind the e2e control server");
  controlUrl = `http://127.0.0.1:${address.port}`;
};
const closeControl = async () => {
  if (!control) return;
  await new Promise((resolveClose) => control.close(resolveClose));
  control = undefined;
};
const requireBuildArtifacts = () => {
  const missing = [
    join(root, "mcp/dist/main.js"),
    join(serverDir, "priv/static/index.html"),
  ].filter((path) => !existsSync(path));
  if (missing.length > 0)
    throw new Error(
      `Missing production build artifacts. Run \`corepack pnpm build\` first: ${missing.join(", ")}`,
    );
};
const cleanup = async () => {
  if (cleaning) return;
  cleaning = true;
  await closeControl();
  await Promise.all([...owned].map(stop));
  if (tempDir) await rm(tempDir, { recursive: true, force: true });
  await releaseLock();
};
const onSignal = (code) => {
  void cleanup().finally(() => process.exit(code));
};
process.once("SIGINT", () => onSignal(130));
process.once("SIGTERM", () => onSignal(143));

try {
  await acquireLock();
  tempDir = await mkdtemp(join(tmpdir(), "autoboard-e2e-"));
  if (process.argv.includes("--lock-probe")) {
    process.stdout.write("lock-acquired\n");
    await delay(Number(process.env.AUTOBOARD_E2E_LOCK_HOLD_MS ?? 0));
  } else if (process.argv.includes("--signal-probe")) {
    const slow = spawnOwned(
      process.execPath,
      ["-e", "setInterval(() => {}, 1000)"],
      { stdio: "ignore" },
    );
    process.stdout.write(
      JSON.stringify({ tempDir, childPid: slow.pid }) + "\n",
    );
    await new Promise(() => {});
  } else {
    requireBuildArtifacts();
    port = await reservePort();
    await run("mix", ["ecto.reset"], { cwd: serverDir, env: environment() });
    await run("mix", ["autoboard.setup"], {
      cwd: serverDir,
      env: environment(),
    });
    const { stdout } = await run(
      "mix",
      ["autoboard.token.create", "--actor", "codex"],
      { cwd: serverDir, env: environment() },
    );
    const tokenLines = stdout.trim().split("\n");
    if (tokenLines.length !== 1 || !/^ab_[A-Za-z0-9_-]+$/.test(tokenLines[0]))
      throw new Error("token task did not emit exactly one plaintext token");
    const token = tokenLines[0];
    const attachmentPath = join(tempDir, "note.txt");
    await writeFile(attachmentPath, "temporary acceptance attachment\n", {
      mode: 0o600,
    });
    await startControl();
    await startServer();
    await run(
      "corepack",
      [
        "pnpm",
        "--filter",
        "@autoboard/e2e",
        "exec",
        "vitest",
        "run",
        "--config",
        "vitest.config.ts",
      ],
      {
        env: {
          ...environment(),
          AUTOBOARD_E2E_SOCKET: join(tempDir, "autoboard.sock"),
          AUTOBOARD_E2E_TOKEN: token,
          AUTOBOARD_E2E_URL: `http://127.0.0.1:${port}`,
          AUTOBOARD_E2E_ATTACHMENT: attachmentPath,
          AUTOBOARD_E2E_CONTROL_URL: controlUrl,
          AUTOBOARD_E2E_CONTROL_TOKEN: controlToken,
        },
        timeoutMs: 90_000,
      },
    );
  }
} catch (error) {
  await writeFailureLog();
  console.error(scrub(error instanceof Error ? error.message : String(error)));
  if (serverOutput)
    console.error(`Autoboard server log saved to ${failureLog}`);
  process.exitCode = 1;
} finally {
  await cleanup();
}
