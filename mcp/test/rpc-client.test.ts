import { mkdtemp, rm } from "node:fs/promises";
import { createServer, type Server, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, test } from "vitest";
import { Schema } from "effect";
import {
  IndeterminateWriteError,
  RpcError,
  RpcProtocolError,
} from "../src/rpc-error.js";
import { RpcClient } from "../src/rpc-client.js";

type Request = { id: number; method: string; params: Record<string, unknown> };
type TestServer = { path: string; server: Server; close: () => Promise<void> };

const servers: TestServer[] = [];

afterEach(async () => {
  await Promise.all(servers.splice(0).map(({ close }) => close()));
});

const frame = (value: unknown) => {
  const payload = Buffer.from(JSON.stringify(value));
  const header = Buffer.allocUnsafe(4);
  header.writeUInt32BE(payload.length);
  return Buffer.concat([header, payload]);
};

const startServer = async (
  handler: (request: Request, socket: Socket) => void,
): Promise<TestServer> => {
  const directory = await mkdtemp(join(tmpdir(), "autoboard-mcp-"));
  const path = join(directory, "rpc.sock");
  const server = createServer((socket) => {
    let buffer = Buffer.alloc(0);
    socket.on("data", (chunk: Buffer) => {
      buffer = Buffer.concat([buffer, chunk]);
      while (buffer.length >= 4) {
        const size = buffer.readUInt32BE(0);
        if (buffer.length < size + 4) return;
        const request = JSON.parse(
          buffer.subarray(4, size + 4).toString("utf8"),
        ) as Request;
        buffer = buffer.subarray(size + 4);
        handler(request, socket);
      }
    });
  });
  await new Promise<void>((resolve, reject) =>
    server.listen(path, () => resolve()).once("error", reject),
  );
  const testServer = {
    path,
    server,
    close: async () => {
      await new Promise<void>((resolve) => server.close(() => resolve()));
      await rm(directory, { recursive: true, force: true });
    },
  };
  servers.push(testServer);
  return testServer;
};

const initialized = (request: Request, socket: Socket) => {
  if (request.method === "session.initialize") {
    socket.write(
      frame({
        jsonrpc: "2.0",
        id: request.id,
        result: {
          protocol_version: 1,
          server_version: "0.1.0",
          actor: "codex",
          authorization: { kind: "global" },
        },
      }),
    );
    return true;
  }
  return false;
};

describe("RpcClient", () => {
  test("initializes and decodes split headers, split payloads, and coalesced frames", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      const first = frame({
        jsonrpc: "2.0",
        id: request.id,
        result: { value: "first" },
      });
      const second = frame({
        jsonrpc: "2.0",
        id: request.id + 100,
        result: { value: "ignored" },
      });
      socket.write(first.subarray(0, 2));
      socket.write(
        Buffer.concat([first.subarray(2, 9), first.subarray(9), second]),
      );
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call(
        "tickets.get",
        {},
        Schema.Struct({ value: Schema.String }),
        "read",
      ),
    ).resolves.toEqual({ value: "first" });
    await client.close();
  });

  test("matches concurrent responses by their JSON-RPC id even when reordered", async () => {
    const requests: Array<{ request: Request; socket: Socket }> = [];
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      requests.push({ request, socket });
      if (requests.length === 2) {
        for (const current of requests.reverse()) {
          current.socket.write(
            frame({
              jsonrpc: "2.0",
              id: current.request.id,
              result: { value: current.request.method },
            }),
          );
        }
      }
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      Promise.all([
        client.call("one", {}, Schema.Struct({ value: Schema.String }), "read"),
        client.call("two", {}, Schema.Struct({ value: Schema.String }), "read"),
      ]),
    ).resolves.toEqual([{ value: "one" }, { value: "two" }]);
    await client.close();
  });

  test("accepts coalesced valid frames whose aggregate exceeds the single-frame limit", async () => {
    const requests: Array<{ request: Request; socket: Socket }> = [];
    const value = "x".repeat(2_100_000);
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      requests.push({ request, socket });
      if (requests.length === 2) {
        socket.write(
          Buffer.concat(
            requests
              .reverse()
              .map(({ request }) =>
                frame({ jsonrpc: "2.0", id: request.id, result: { value } }),
              ),
          ),
        );
      }
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      Promise.all([
        client.call("one", {}, Schema.Struct({ value: Schema.String }), "read"),
        client.call("two", {}, Schema.Struct({ value: Schema.String }), "read"),
      ]),
    ).resolves.toEqual([{ value }, { value }]);
    await client.close();
  });

  test("surfaces server errors as typed RPC errors", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      socket.write(
        frame({
          jsonrpc: "2.0",
          id: request.id,
          error: {
            code: -32010,
            message: "no access",
            data: { kind: "unauthorized", message: "no access", fields: {} },
          },
        }),
      );
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call("tickets.update", {}, Schema.Unknown, "write"),
    ).rejects.toBeInstanceOf(RpcError);
    await client.close();
  });

  test("rejects malformed error envelopes as protocol errors and preserves valid envelope errors", async () => {
    const malformed = [
      {
        jsonrpc: "1.0",
        error: {
          code: -32010,
          message: "no",
          data: { kind: "unauthorized", message: "no", fields: {} },
        },
      },
      {
        jsonrpc: "2.0",
        error: {
          code: "-32010",
          message: "no",
          data: { kind: "unauthorized", message: "no", fields: {} },
        },
      },
      {
        jsonrpc: "2.0",
        error: {
          code: -32010,
          message: 9,
          data: { kind: "unauthorized", message: "no", fields: {} },
        },
      },
      {
        jsonrpc: "2.0",
        error: {
          code: -32010,
          message: "no",
          data: { kind: "unauthorized", message: "no" },
        },
      },
      {
        jsonrpc: "2.0",
        error: {
          code: -32010,
          message: "no",
          data: {
            kind: "unauthorized",
            message: "no",
            fields: {},
            extra: true,
          },
        },
      },
    ];
    let index = 0;
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      const response = malformed[index++]!;
      socket.write(frame({ ...response, id: request.id }));
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    for (const _ of malformed) {
      await expect(
        client.call("tickets.get", {}, Schema.Unknown, "read"),
      ).rejects.toBeInstanceOf(RpcProtocolError);
    }
    await client.close();
  });

  test("rejects malformed or incompatible session initialization responses", async () => {
    const fixture = await startServer((request, socket) => {
      if (request.method === "session.initialize") {
        socket.write(
          frame({
            jsonrpc: "2.0",
            id: request.id,
            result: { protocol_version: 2 },
          }),
        );
      }
    });

    await expect(
      RpcClient.connect({ socketPath: fixture.path, token: "token" }),
    ).rejects.toBeInstanceOf(RpcProtocolError);
  });

  test("rejects a result that does not decode through the caller's Effect schema", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      socket.write(
        frame({ jsonrpc: "2.0", id: request.id, result: { value: 42 } }),
      );
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call(
        "tickets.get",
        {},
        Schema.Struct({ value: Schema.String }),
        "read",
      ),
    ).rejects.toBeInstanceOf(RpcProtocolError);
    await client.close();
  });

  test("reconnects once and replays pending reads with a new request id", async () => {
    let connections = 0;
    const seen: Request[] = [];
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      seen.push(request);
      if (connections++ === 0) socket.destroy();
      else
        socket.write(
          frame({
            jsonrpc: "2.0",
            id: request.id,
            result: { value: "replayed" },
          }),
        );
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call(
        "tickets.get",
        {},
        Schema.Struct({ value: Schema.String }),
        "read",
      ),
    ).resolves.toEqual({ value: "replayed" });
    expect(seen).toHaveLength(2);
    expect(seen[1]?.id).not.toBe(seen[0]?.id);
    await client.close();
  });

  test("never replays a write after disconnect and marks it indeterminate", async () => {
    let writes = 0;
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      writes += 1;
      socket.destroy();
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call("tickets.update", {}, Schema.Unknown, "write"),
    ).rejects.toBeInstanceOf(IndeterminateWriteError);
    expect(writes).toBe(1);
    await client.close();
  });

  test("rejects pending and future calls after the replay connection also disconnects", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      socket.destroy();
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call("tickets.get", {}, Schema.Unknown, "read"),
    ).rejects.toThrow("retry");
    await expect(
      client.call("tickets.get", {}, Schema.Unknown, "read"),
    ).rejects.toThrow("retry");
    await client.close();
  });

  test("settles recovery without unhandled rejection when the reconnect initialization fails", async () => {
    let initializations = 0;
    const fixture = await startServer((request, socket) => {
      if (request.method === "session.initialize") {
        initializations += 1;
        if (initializations === 1) initialized(request, socket);
        else socket.destroy();
        return;
      }
      socket.destroy();
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });
    const unhandled = new Promise<unknown>((resolve) =>
      process.once("unhandledRejection", resolve),
    );

    await expect(
      client.call("tickets.get", {}, Schema.Unknown, "read"),
    ).rejects.toBeInstanceOf(Error);
    await expect(
      Promise.race([
        unhandled,
        new Promise((resolve) => setTimeout(resolve, 25)),
      ]),
    ).resolves.toBeUndefined();
    await expect(
      client.call("tickets.get", {}, Schema.Unknown, "read"),
    ).rejects.toBeInstanceOf(Error);
    await client.close();
  });

  test("close cancels an in-flight reconnect initialization and settles waiting calls", async () => {
    let initializations = 0;
    const fixture = await startServer((request, socket) => {
      if (request.method === "session.initialize") {
        initializations += 1;
        if (initializations === 1) initialized(request, socket);
        return;
      }
      socket.destroy();
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });
    const read = client.call("tickets.get", {}, Schema.Unknown, "read");

    await new Promise((resolve) => setTimeout(resolve, 5));
    await client.close();
    await expect(read).rejects.toThrow("closed");
  });

  test("close settles a reconnect that is still opening its Unix socket", async () => {
    let initializations = 0;
    const fixture = await startServer((request, socket) => {
      if (request.method === "session.initialize") {
        initializations += 1;
        if (initializations === 1) initialized(request, socket);
        return;
      }
      socket.destroy();
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });
    const read = client.call("tickets.get", {}, Schema.Unknown, "read");
    const outcome = read.catch((error: unknown) => error);
    await new Promise((resolve) => setTimeout(resolve, 1));
    const stopped = new Promise<void>((resolve) =>
      fixture.server.close(() => resolve()),
    );
    await client.close();
    await stopped;
    await expect(outcome).resolves.toMatchObject({
      message: expect.stringContaining("closed"),
    });
  });

  test("returns the decoded Type of transforming Effect schemas", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      socket.write(frame({ jsonrpc: "2.0", id: request.id, result: "42" }));
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call("tickets.get", {}, Schema.NumberFromString, "read"),
    ).resolves.toBe(42);
    await client.close();
  });

  test("rejects malformed or oversized response frames without allocating their payload", async () => {
    const fixture = await startServer((request, socket) => {
      if (initialized(request, socket)) return;
      const header = Buffer.allocUnsafe(4);
      header.writeUInt32BE(4 * 1024 * 1024 + 1);
      socket.write(header);
    });
    const client = await RpcClient.connect({
      socketPath: fixture.path,
      token: "token",
    });

    await expect(
      client.call("tickets.get", {}, Schema.Unknown, "read"),
    ).rejects.toBeInstanceOf(RpcProtocolError);
    await client.close();
  });
});
