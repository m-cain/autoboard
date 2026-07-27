import { randomBytes } from "node:crypto";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { createServer as createTcpServer } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const root = resolve(import.meta.dirname, "../..");
const executable = join(root, "dist/autoboard");
const failureLog = join(root, "output/playwright/autoboard-e2e-daemon.log");
const owned = new Set();
let tempDir;
let control;
let controlUrl;
let controlToken;
let daemon;
let port;
let daemonOutput = "";
let cleaning = false;

const delay = (milliseconds) =>
  new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));

const capture = (chunk) => {
  daemonOutput += chunk.toString();
  if (daemonOutput.length > 200_000) {
    daemonOutput = daemonOutput.slice(-200_000);
  }
};

const signalGroup = (child, signal) => {
  if (!child?.pid) return;
  try {
    process.kill(-child.pid, signal);
  } catch {
    try {
      child.kill(signal);
    } catch {
      // The process already exited.
    }
  }
};

const waitForExit = (child, milliseconds) =>
  new Promise((resolveExit) => {
    if (!child || child.exitCode !== null || child.signalCode !== null) {
      resolveExit(true);
      return;
    }
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

const reservePort = async () => {
  const listener = createTcpServer();
  await new Promise((resolveListen, rejectListen) => {
    listener.once("error", rejectListen);
    listener.listen({ host: "127.0.0.1", port: 0 }, resolveListen);
  });
  const address = listener.address();
  await new Promise((resolveClose, rejectClose) => {
    listener.close((error) => (error ? rejectClose(error) : resolveClose()));
  });
  if (!address || typeof address === "string") {
    throw new Error("could not reserve a loopback port");
  }
  return address.port;
};

const environment = () => ({
  ...process.env,
  AUTOBOARD_DATA_DIR: join(tempDir, "data"),
  AUTOBOARD_DATABASE_PATH: join(tempDir, "data", "autoboard.db"),
  AUTOBOARD_HTTP_PORT: String(port),
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
      // The daemon has not bound the port yet.
    }
    await delay(100);
  }
  throw new Error(`Autoboard did not become healthy at ${url}`);
};

const startDaemon = async ({ retryPort = true } = {}) => {
  const attempts = retryPort ? 3 : 1;
  let lastError;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (retryPort || port === undefined) port = await reservePort();
    const child = spawnOwned(executable, ["serve"], {
      cwd: root,
      env: environment(),
      stdio: ["ignore", "pipe", "pipe"],
    });
    child.stdout.on("data", capture);
    child.stderr.on("data", capture);
    daemon = child;
    try {
      await waitForHealth();
      return;
    } catch (error) {
      lastError = error;
      await stop(child);
      daemon = undefined;
    }
  }
  throw lastError;
};

const restartDaemon = async () => {
  await stop(daemon);
  daemon = undefined;
  await startDaemon({ retryPort: false });
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
      await restartDaemon();
      response.writeHead(204).end();
    } catch (error) {
      response.writeHead(500).end(String(error));
    }
  });
  await new Promise((resolveListen, rejectListen) => {
    control.once("error", rejectListen);
    control.listen({ host: "127.0.0.1", port: 0 }, resolveListen);
  });
  const address = control.address();
  if (!address || typeof address === "string") {
    throw new Error("could not bind the e2e control server");
  }
  controlUrl = `http://127.0.0.1:${address.port}`;
};

const closeControl = async () => {
  if (!control) return;
  await new Promise((resolveClose) => control.close(resolveClose));
  control = undefined;
};

const writeFailureLog = async () => {
  if (!daemonOutput) return;
  await mkdir(join(root, "output/playwright"), { recursive: true });
  await writeFile(failureLog, daemonOutput, { mode: 0o600 });
};

const cleanup = async () => {
  if (cleaning) return;
  cleaning = true;
  await closeControl();
  await Promise.all([...owned].map(stop));
  if (tempDir) await rm(tempDir, { recursive: true, force: true });
};

const onSignal = (code) => {
  void cleanup().finally(() => process.exit(code));
};

process.once("SIGINT", () => onSignal(130));
process.once("SIGTERM", () => onSignal(143));

try {
  if (!existsSync(executable)) {
    throw new Error(
      "Missing production binary. Run `corepack pnpm build` before the e2e suite.",
    );
  }
  tempDir = await mkdtemp(join(tmpdir(), "autoboard-e2e-"));
  const attachmentPath = join(tempDir, "note.txt");
  await writeFile(attachmentPath, "temporary acceptance attachment\n", {
    mode: 0o600,
  });
  await startControl();
  await startDaemon();
  const child = spawnOwned(
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
      cwd: root,
      env: {
        ...environment(),
        AUTOBOARD_E2E_URL: `http://127.0.0.1:${port}`,
        AUTOBOARD_E2E_ATTACHMENT: attachmentPath,
        AUTOBOARD_E2E_CONTROL_URL: controlUrl,
        AUTOBOARD_E2E_CONTROL_TOKEN: controlToken,
      },
      stdio: "inherit",
    },
  );
  const completed = await waitForExit(child, 90_000);
  if (!completed) {
    await stop(child);
    throw new Error("end-to-end suite timed out after 90 seconds");
  }
  if (child.exitCode !== 0) {
    throw new Error(
      `end-to-end suite exited with ${child.exitCode ?? child.signalCode}`,
    );
  }
} catch (error) {
  await writeFailureLog();
  console.error(error instanceof Error ? error.message : String(error));
  if (daemonOutput) {
    console.error(`Autoboard daemon log saved to ${failureLog}`);
  }
  process.exitCode = 1;
} finally {
  await cleanup();
}
