import assert from "node:assert/strict"
import { existsSync } from "node:fs"
import { spawn } from "node:child_process"
import { test } from "node:test"
import { join } from "node:path"

const runner = join(import.meta.dirname, "run.mjs")
const start = (args, env = {}) => {
  const child = spawn(process.execPath, [runner, ...args], { env: { ...process.env, ...env }, stdio: ["ignore", "pipe", "pipe"] })
  let output = ""
  child.stdout.on("data", (chunk) => { output += chunk })
  child.stderr.on("data", (chunk) => { output += chunk })
  return { child, output: () => output }
}
const line = async (process) => {
  const deadline = Date.now() + 5_000
  while (Date.now() < deadline) {
    const first = process.output().split("\n").find(Boolean)
    if (first) return first
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 25))
  }
  throw new Error(`runner produced no output: ${process.output()}`)
}
const exited = (child) => new Promise((resolveExit) => child.once("close", (code) => resolveExit(code)))

test("a concurrent runner refuses before ecto.reset while a trusted lock owner is alive", async () => {
  const first = start(["--lock-probe"], { AUTOBOARD_E2E_LOCK_HOLD_MS: "5000" })
  assert.equal(await line(first), "lock-acquired")
  const second = start(["--lock-probe"])
  assert.equal(await exited(second.child), 1)
  assert.match(second.output(), /refusing before ecto\.reset/)
  first.child.kill("SIGTERM")
  assert.equal(await exited(first.child), 143)
})

test("SIGTERM reaps owned descendants and removes only the runner temporary directory", async () => {
  const probe = start(["--signal-probe"])
  const record = JSON.parse(await line(probe))
  probe.child.kill("SIGTERM")
  assert.equal(await exited(probe.child), 143)
  await new Promise((resolveDelay) => setTimeout(resolveDelay, 100))
  assert.equal(existsSync(record.tempDir), false)
  assert.throws(() => process.kill(record.childPid, 0), /ESRCH/)
})
