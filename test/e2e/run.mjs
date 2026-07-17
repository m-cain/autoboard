import { createServer } from "node:net"
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises"
import { existsSync } from "node:fs"
import { spawn } from "node:child_process"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"

const root = resolve(import.meta.dirname, "../..")
const serverDir = join(root, "server")
const tempDir = await mkdtemp(join(tmpdir(), "autoboard-e2e-"))
const dataDir = join(tempDir, "data")
const socketPath = join(tempDir, "autoboard.sock")
const attachmentPath = join(tempDir, "note.txt")
const failureLog = join(root, "output/playwright/autoboard-e2e-server.log")
const port = await reservePort()
const databaseUrl = "ecto://autoboard:autoboard@localhost/autoboard_e2e"
const environment = {
  ...process.env,
  DATABASE_URL: databaseUrl,
  AUTOBOARD_DATA_DIR: dataDir,
  AUTOBOARD_SOCKET: socketPath,
  AUTOBOARD_HTTP_PORT: String(port),
  MIX_ENV: "dev",
}
let server
let serverOutput = ""
let cleaning = false

const capture = (chunk) => {
  serverOutput += chunk.toString()
  if (serverOutput.length > 200_000) serverOutput = serverOutput.slice(-200_000)
}

const terminateGroup = (child, signal) => {
  if (!child?.pid) return
  try {
    process.kill(-child.pid, signal)
  } catch {
    child.kill(signal)
  }
}

const run = (command, args, { cwd = root, env = environment, captureOutput = true, timeoutMs = 60_000 } = {}) => new Promise((resolveRun, rejectRun) => {
  const child = spawn(command, args, { cwd, env, stdio: ["ignore", "pipe", "pipe"], detached: true })
  let stdout = ""
  let stderr = ""
  const timeout = setTimeout(() => {
    terminateGroup(child, "SIGTERM")
    rejectRun(new Error(`${command} ${args.join(" ")} timed out after ${timeoutMs}ms`))
  }, timeoutMs)
  child.stdout.on("data", (chunk) => { stdout += chunk })
  child.stderr.on("data", (chunk) => { stderr += chunk })
  child.once("error", (error) => { clearTimeout(timeout); rejectRun(error) })
  child.once("close", (code, signal) => {
    clearTimeout(timeout)
    if (code === 0) resolveRun({ stdout, stderr })
    else rejectRun(new Error(`${command} ${args.join(" ")} exited with ${code ?? signal}${captureOutput && stderr ? `: ${stderr}` : ""}`))
  })
})

const stop = async (child) => {
  if (!child?.pid) return
  if (child.exitCode !== null || child.signalCode !== null) return
  try {
    terminateGroup(child, "SIGTERM")
  } catch {
    child.kill("SIGTERM")
  }
  await new Promise((resolveStop) => {
    const timeout = setTimeout(() => {
      terminateGroup(child, "SIGKILL")
      resolveStop()
    }, 5_000)
    child.once("close", () => { clearTimeout(timeout); resolveStop() })
  })
}

const waitForHealth = async () => {
  const url = `http://127.0.0.1:${port}/health`
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url)
      if (response.status === 200) return
    } catch { /* server has not bound yet */ }
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 150))
  }
  throw new Error(`Autoboard did not become healthy at ${url}`)
}

async function reservePort() {
  const listener = createServer()
  await new Promise((resolveListen, rejectListen) => {
    listener.once("error", rejectListen)
    listener.listen({ host: "127.0.0.1", port: 0 }, resolveListen)
  })
  const address = listener.address()
  await new Promise((resolveClose, rejectClose) => listener.close((error) => error ? rejectClose(error) : resolveClose()))
  if (!address || typeof address === "string") throw new Error("could not reserve a loopback port")
  return address.port
}

const requireBuildArtifacts = () => {
  const missing = [join(root, "mcp/dist/main.js"), join(serverDir, "priv/static/index.html")].filter((path) => !existsSync(path))
  if (missing.length > 0) throw new Error(`Missing production build artifacts. Run \`corepack pnpm build\` first: ${missing.join(", ")}`)
}

const preserveFailureLog = async () => {
  if (!serverOutput) return
  await mkdir(join(root, "output/playwright"), { recursive: true })
  await writeFile(failureLog, serverOutput, { mode: 0o600 })
}

const cleanup = async () => {
  if (cleaning) return
  cleaning = true
  await stop(server)
  await rm(tempDir, { recursive: true, force: true })
}

const onSignal = (signal) => {
  void cleanup().finally(() => process.exit(signal === "SIGINT" ? 130 : 143))
}

process.once("SIGINT", () => onSignal("SIGINT"))
process.once("SIGTERM", () => onSignal("SIGTERM"))

try {
  requireBuildArtifacts()
  await run("mix", ["ecto.reset"], { cwd: serverDir })
  await run("mix", ["autoboard.setup"], { cwd: serverDir })
  const { stdout } = await run("mix", ["autoboard.token.create", "--actor", "codex"], { cwd: serverDir })
  const tokenLines = stdout.trim().split("\n")
  if (tokenLines.length !== 1 || !/^ab_[A-Za-z0-9_-]+$/.test(tokenLines[0])) throw new Error("token task did not emit exactly one plaintext token")
  const token = tokenLines[0]
  await writeFile(attachmentPath, "temporary acceptance attachment\n", { mode: 0o600 })

  server = spawn("mix", ["run", "--no-halt"], { cwd: serverDir, env: environment, stdio: ["ignore", "pipe", "pipe"], detached: true })
  server.stdout.on("data", capture)
  server.stderr.on("data", capture)
  server.once("error", (error) => { serverOutput += `\nserver spawn error: ${error.message}` })
  await waitForHealth()

  await run("corepack", ["pnpm", "--filter", "@autoboard/e2e", "exec", "vitest", "run", "--config", "vitest.config.ts"], {
    env: {
      ...environment,
      AUTOBOARD_E2E_SOCKET: socketPath,
      AUTOBOARD_E2E_TOKEN: token,
      AUTOBOARD_E2E_URL: `http://127.0.0.1:${port}`,
      AUTOBOARD_E2E_ATTACHMENT: attachmentPath,
    },
  })
} catch (error) {
  await preserveFailureLog()
  console.error(error instanceof Error ? error.message : String(error))
  if (serverOutput) console.error(`Autoboard server log saved to ${failureLog}`)
  process.exitCode = 1
} finally {
  await cleanup()
}
