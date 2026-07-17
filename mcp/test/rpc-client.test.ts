import { mkdtemp, rm } from "node:fs/promises"
import { createServer, type Server, type Socket } from "node:net"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { afterEach, describe, expect, test } from "vitest"
import { Schema } from "effect"
import { IndeterminateWriteError, RpcError, RpcProtocolError } from "../src/rpc-error.js"
import { RpcClient } from "../src/rpc-client.js"

type Request = { id: number; method: string; params: Record<string, unknown> }
type TestServer = { path: string; server: Server; close: () => Promise<void> }

const servers: TestServer[] = []

afterEach(async () => {
  await Promise.all(servers.splice(0).map(({ close }) => close()))
})

const frame = (value: unknown) => {
  const payload = Buffer.from(JSON.stringify(value))
  const header = Buffer.allocUnsafe(4)
  header.writeUInt32BE(payload.length)
  return Buffer.concat([header, payload])
}

const startServer = async (handler: (request: Request, socket: Socket) => void): Promise<TestServer> => {
  const directory = await mkdtemp(join(tmpdir(), "autoboard-mcp-"))
  const path = join(directory, "rpc.sock")
  const server = createServer((socket) => {
    let buffer = Buffer.alloc(0)
    socket.on("data", (chunk: Buffer) => {
      buffer = Buffer.concat([buffer, chunk])
      while (buffer.length >= 4) {
        const size = buffer.readUInt32BE(0)
        if (buffer.length < size + 4) return
        const request = JSON.parse(buffer.subarray(4, size + 4).toString("utf8")) as Request
        buffer = buffer.subarray(size + 4)
        handler(request, socket)
      }
    })
  })
  await new Promise<void>((resolve, reject) => server.listen(path, () => resolve()).once("error", reject))
  const testServer = {
    path,
    server,
    close: async () => {
      await new Promise<void>((resolve) => server.close(() => resolve()))
      await rm(directory, { recursive: true, force: true })
    },
  }
  servers.push(testServer)
  return testServer
}

const initialized = (request: Request, socket: Socket) => {
  if (request.method === "session.initialize") {
    socket.write(frame({ jsonrpc: "2.0", id: request.id, result: { protocol_version: 1, actor: "codex" } }))
    return true
  }
  return false
}

describe("RpcClient", () => {
  test("initializes and decodes split headers, split payloads, and coalesced frames", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      const first = frame({ jsonrpc: "2.0", id: request.id, result: { value: "first" } })
      const second = frame({ jsonrpc: "2.0", id: request.id + 100, result: { value: "ignored" } })
      socket.write(first.subarray(0, 2))
      socket.write(Buffer.concat([first.subarray(2, 9), first.subarray(9), second]))
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.get", {}, Schema.Struct({ value: Schema.String }), "read")).resolves.toEqual({ value: "first" })
    await client.close()
  })

  test("matches concurrent responses by their JSON-RPC id even when reordered", async () => {
    const requests: Array<{ request: Request; socket: Socket }> = []
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      requests.push({ request, socket })
      if (requests.length === 2) {
        for (const current of requests.reverse()) {
          current.socket.write(frame({ jsonrpc: "2.0", id: current.request.id, result: { value: current.request.method } }))
        }
      }
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(Promise.all([
      client.call("one", {}, Schema.Struct({ value: Schema.String }), "read"),
      client.call("two", {}, Schema.Struct({ value: Schema.String }), "read"),
    ])).resolves.toEqual([{ value: "one" }, { value: "two" }])
    await client.close()
  })

  test("surfaces server errors as typed RPC errors", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      socket.write(frame({ jsonrpc: "2.0", id: request.id, error: { code: -32010, message: "stale", data: { kind: "revision_conflict" } } }))
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.update", {}, Schema.Unknown, "write")).rejects.toBeInstanceOf(RpcError)
    await client.close()
  })

  test("rejects a result that does not decode through the caller's Effect schema", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      socket.write(frame({ jsonrpc: "2.0", id: request.id, result: { value: 42 } }))
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.get", {}, Schema.Struct({ value: Schema.String }), "read")).rejects.toBeInstanceOf(RpcProtocolError)
    await client.close()
  })

  test("reconnects once and replays pending reads with a new request id", async () => {
    let connections = 0
    const seen: Request[] = []
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      seen.push(request)
      if (connections++ === 0) socket.destroy()
      else socket.write(frame({ jsonrpc: "2.0", id: request.id, result: { value: "replayed" } }))
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.get", {}, Schema.Struct({ value: Schema.String }), "read")).resolves.toEqual({ value: "replayed" })
    expect(seen).toHaveLength(2)
    expect(seen[1]?.id).not.toBe(seen[0]?.id)
    await client.close()
  })

  test("never replays a write after disconnect and marks it indeterminate", async () => {
    let writes = 0
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      writes += 1
      socket.destroy()
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.update", {}, Schema.Unknown, "write")).rejects.toBeInstanceOf(IndeterminateWriteError)
    expect(writes).toBe(1)
    await client.close()
  })

  test("rejects pending and future calls after the replay connection also disconnects", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      socket.destroy()
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.get", {}, Schema.Unknown, "read")).rejects.toThrow("retry")
    await expect(client.call("tickets.get", {}, Schema.Unknown, "read")).rejects.toThrow("retry")
    await client.close()
  })

  test("rejects malformed or oversized response frames without allocating their payload", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return
      const header = Buffer.allocUnsafe(4)
      header.writeUInt32BE(4 * 1024 * 1024 + 1)
      socket.write(header)
    })
    const client = await RpcClient.connect({ socketPath: fixture.path, token: "token" })

    await expect(client.call("tickets.get", {}, Schema.Unknown, "read")).rejects.toBeInstanceOf(RpcProtocolError)
    await client.close()
  })
})
