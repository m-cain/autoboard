import { createConnection, type Socket } from "node:net"
import { Schema } from "effect"
import { RpcEnvelopeFailure, SessionInitialize } from "@autoboard/contracts"
import { IndeterminateWriteError, RpcConnectionError, RpcError, RpcProtocolError } from "./rpc-error.js"

const MAX_FRAME_BYTES = 4 * 1024 * 1024

type Mode = "read" | "write"
type JsonRpcId = number

type Pending = {
  id: JsonRpcId
  method: string
  params: Record<string, unknown>
  schema: Schema.Schema.Any
  mode: Mode
  resolve: (value: unknown) => void
  reject: (error: Error) => void
}

type InitializePending = {
  resolve: (session: Schema.Schema.Type<typeof SessionInitialize>) => void
  reject: (error: Error) => void
}

export type RpcClientOptions = {
  socketPath: string
  token: string
  client?: { name: string; version: string }
}

/**
 * A deliberately small JSON-RPC 2.0 client for the private Unix socket.
 * Request IDs are local to a live connection and are regenerated when a read
 * is replayed, preventing a late packet from a dead connection being matched.
 */
export class RpcClient {
  private socket: Socket | undefined
  private opening: Socket | undefined
  private receiveBuffer = Buffer.alloc(0)
  private nextId = 1
  private readonly pending = new Map<JsonRpcId, Pending>()
  private readonly initializing = new Map<JsonRpcId, InitializePending>()
  private recovery: Promise<void> | undefined
  private disconnects = 0
  private isInitializing = false
  private closed = false
  private failed: Error | undefined

  private constructor(private readonly options: Required<RpcClientOptions>) {}

  static async connect(options: RpcClientOptions): Promise<RpcClient> {
    const client = new RpcClient({
      ...options,
      client: options.client ?? { name: "autoboard-mcp", version: "0.1.0" },
    })
    await client.establish()
    return client
  }

  async call<S extends Schema.Schema.Any>(
    method: string,
    params: Record<string, unknown>,
    schema: S,
    mode: Mode,
  ): Promise<Schema.Schema.Type<S>> {
    if (this.closed) throw new RpcConnectionError("RPC client is closed")
    if (this.failed) throw this.failed
    if (this.recovery) await this.recovery
    if (!this.socket) throw this.failed ?? new RpcConnectionError("RPC client is disconnected")

    return new Promise<Schema.Schema.Type<S>>((resolve, reject) => {
      const id = this.allocateId()
      const pending: Pending = {
        id,
        method,
        params,
        schema,
        mode,
        resolve: (value) => resolve(value as Schema.Schema.Type<S>),
        reject,
      }
      this.pending.set(id, pending)
      try {
        this.send(pending)
      } catch (error) {
        this.pending.delete(id)
        reject(error instanceof Error ? error : new RpcConnectionError("Unable to send RPC request"))
      }
    })
  }

  async close(): Promise<void> {
    if (this.closed) return
    this.closed = true
    const error = new RpcConnectionError("RPC client is closed")
    this.failed = error
    this.rejectInitializing(error)
    this.rejectAll(error)
    this.opening?.destroy()
    this.opening = undefined
    this.socket?.destroy()
    this.socket = undefined
  }

  private async establish(): Promise<void> {
    const socket = await this.openSocket()
    this.socket = socket
    this.receiveBuffer = Buffer.alloc(0)
    this.isInitializing = true

    try {
      await this.initialize()
    } catch (error) {
      socket.destroy()
      this.socket = undefined
      throw error
    } finally {
      this.isInitializing = false
    }
  }

  private openSocket(): Promise<Socket> {
    return new Promise((resolve, reject) => {
      const socket = createConnection(this.options.socketPath)
      this.opening = socket
      let settled = false
      const finish = (error: Error) => {
        if (settled) return
        settled = true
        cleanup()
        if (this.opening === socket) this.opening = undefined
        reject(error)
      }
      const onError = (error: Error) => {
        socket.destroy()
        finish(new RpcConnectionError(`Unable to connect to RPC socket: ${error.message}`))
      }
      const onClose = () => {
        finish(new RpcConnectionError("RPC socket closed before connecting"))
      }
      const onConnect = () => {
        if (settled) return
        settled = true
        cleanup()
        if (this.opening === socket) this.opening = undefined
        if (this.closed) {
          socket.destroy()
          reject(new RpcConnectionError("RPC client is closed"))
          return
        }
        socket.on("data", (chunk: Buffer) => this.receive(socket, chunk))
        socket.on("close", () => this.disconnected(socket))
        socket.on("error", () => undefined)
        resolve(socket)
      }
      const cleanup = () => {
        socket.off("error", onError)
        socket.off("connect", onConnect)
        socket.off("close", onClose)
      }
      socket.once("error", onError)
      socket.once("connect", onConnect)
      socket.once("close", onClose)
    })
  }

  private initialize(): Promise<Schema.Schema.Type<typeof SessionInitialize>> {
    const id = this.allocateId()
    return new Promise<Schema.Schema.Type<typeof SessionInitialize>>((resolve, reject) => {
      this.initializing.set(id, { resolve, reject })
      try {
        this.write({
          jsonrpc: "2.0",
          id,
          method: "session.initialize",
          params: {
            protocol_version: 1,
            token: this.options.token,
            client: this.options.client,
          },
        })
      } catch (error) {
        this.initializing.delete(id)
        reject(error instanceof Error ? error : new RpcConnectionError("Unable to initialize RPC session"))
      }
    })
  }

  private send(pending: Pending): void {
    this.write({
      jsonrpc: "2.0",
      id: pending.id,
      method: pending.method,
      params: pending.params,
    })
  }

  private write(message: unknown): void {
    const payload = Buffer.from(JSON.stringify(message), "utf8")
    if (payload.length > MAX_FRAME_BYTES) {
      throw new RpcProtocolError(`RPC request exceeds ${MAX_FRAME_BYTES} bytes`)
    }
    const header = Buffer.allocUnsafe(4)
    header.writeUInt32BE(payload.length)
    if (!this.socket?.writable) {
      throw new RpcConnectionError("RPC socket is not writable")
    }
    this.socket.write(Buffer.concat([header, payload]))
  }

  private receive(socket: Socket, chunk: Buffer): void {
    if (socket !== this.socket || this.closed) return
    this.receiveBuffer = Buffer.concat([this.receiveBuffer, chunk])
    while (this.receiveBuffer.length >= 4) {
      const size = this.receiveBuffer.readUInt32BE(0)
      if (size > MAX_FRAME_BYTES) {
        this.failProtocol(socket, `RPC response exceeds ${MAX_FRAME_BYTES} bytes`)
        return
      }
      if (this.receiveBuffer.length < size + 4) return
      const payload = this.receiveBuffer.subarray(4, size + 4)
      this.receiveBuffer = this.receiveBuffer.subarray(size + 4)

      try {
        this.response(JSON.parse(payload.toString("utf8")) as Record<string, unknown>)
      } catch (error) {
        this.failProtocol(socket, error instanceof Error ? error.message : "Malformed RPC response")
        return
      }
    }
  }

  private response(message: Record<string, unknown>): void {
    const id = message.id
    if (typeof id !== "number") return

    const initializing = this.initializing.get(id)
    if (initializing) {
      this.initializing.delete(id)
      if ("error" in message) {
        try {
          const error = this.decodeErrorEnvelope(message, id)
          initializing.reject(new RpcError(error.code, error.message, error.data))
        } catch (error) {
          initializing.reject(error instanceof Error ? error : new RpcProtocolError("Invalid JSON-RPC error response"))
        }
      }
      else if (message.jsonrpc === "2.0" && "result" in message) {
        try {
          initializing.resolve(
            Schema.decodeUnknownSync(
              SessionInitialize as unknown as Schema.Schema<Schema.Schema.Type<typeof SessionInitialize>, unknown, never>,
            )(message.result),
          )
        } catch (error) {
          initializing.reject(new RpcProtocolError(`Invalid session.initialize response: ${String(error)}`))
        }
      } else initializing.reject(new RpcProtocolError("Invalid session.initialize response"))
      return
    }

    const pending = this.pending.get(id)
    if (!pending) return
    this.pending.delete(id)

    if ("error" in message) {
      try {
        const error = this.decodeErrorEnvelope(message, id)
        pending.reject(new RpcError(error.code, error.message, error.data))
      } catch (error) {
        pending.reject(error instanceof Error ? error : new RpcProtocolError("Invalid JSON-RPC error response"))
      }
      return
    }
    if (message.jsonrpc !== "2.0" || !("result" in message)) {
      pending.reject(new RpcProtocolError("Invalid JSON-RPC response"))
      return
    }
    try {
      pending.resolve(
        Schema.decodeUnknownSync(pending.schema as Schema.Schema<unknown, unknown, never>)(message.result),
      )
    } catch (error) {
      pending.reject(new RpcProtocolError(`RPC result failed contract decoding: ${String(error)}`))
    }
  }

  private decodeErrorEnvelope(message: Record<string, unknown>, expectedId: number): {
    code: number
    message: string
    data: unknown
  } {
    try {
      const envelope = Schema.decodeUnknownSync(
        RpcEnvelopeFailure as unknown as Schema.Schema<{
          id: string | number | null
          error: { code: number; message: string; data: unknown }
        }, unknown, never>,
      )(message)
      if (envelope.id !== expectedId) throw new RpcProtocolError("JSON-RPC error response id does not match request")
      return envelope.error
    } catch (error) {
      if (error instanceof RpcProtocolError) throw error
      throw new RpcProtocolError(`Invalid JSON-RPC error envelope: ${String(error)}`)
    }
  }

  private failProtocol(socket: Socket, message: string): void {
    this.failed = new RpcProtocolError(message)
    socket.destroy()
  }

  private disconnected(socket: Socket): void {
    if (socket !== this.socket || this.closed) return
    this.socket = undefined
    const initializationError = new RpcConnectionError("RPC connection closed during initialization")
    this.rejectInitializing(initializationError)

    if (this.isInitializing) return
    this.disconnects += 1

    for (const [id, pending] of this.pending) {
      if (pending.mode === "write") {
        this.pending.delete(id)
        pending.reject(new IndeterminateWriteError(pending.method))
      }
    }

    if (this.disconnects >= 2 || this.failed) {
      const error = this.failed ?? new RpcConnectionError("RPC connection failed after its retry")
      this.failed = error
      this.rejectAll(error)
      return
    }

    this.recovery = this.reconnect().finally(() => {
      this.recovery = undefined
    })
  }

  private async reconnect(): Promise<void> {
    try {
      await this.establish()
      if (this.closed) return
      for (const pending of [...this.pending.values()]) {
        this.pending.delete(pending.id)
        pending.id = this.allocateId()
        this.pending.set(pending.id, pending)
        this.send(pending)
      }
    } catch (error) {
      this.disconnects = 2
      const failure = error instanceof Error ? error : new RpcConnectionError("RPC reconnection failed")
      if (!this.closed) {
        this.failed = failure
        this.rejectAll(failure)
      }
    }
  }

  private rejectAll(error: Error): void {
    for (const pending of this.pending.values()) pending.reject(error)
    this.pending.clear()
  }

  private rejectInitializing(error: Error): void {
    for (const { reject } of this.initializing.values()) reject(error)
    this.initializing.clear()
  }

  private allocateId(): number {
    const id = this.nextId
    this.nextId += 1
    if (this.nextId > Number.MAX_SAFE_INTEGER) this.nextId = 1
    return id
  }
}
