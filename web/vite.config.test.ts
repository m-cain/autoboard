import { afterEach, expect, test, vi } from "vitest";
import { build, resolveConfig } from "vite";

const originalPort = process.env.AUTOBOARD_HTTP_PORT;
const originalNodeEnv = process.env.NODE_ENV;

afterEach(() => {
  if (originalPort === undefined) delete process.env.AUTOBOARD_HTTP_PORT;
  else process.env.AUTOBOARD_HTTP_PORT = originalPort;
  if (originalNodeEnv === undefined) delete process.env.NODE_ENV;
  else process.env.NODE_ENV = originalNodeEnv;
  vi.resetModules();
});

test("proxies development API and SSE requests to the configured server port", async () => {
  process.env.AUTOBOARD_HTTP_PORT = "4545";
  vi.resetModules();

  const { default: config } = await import("./vite.config.js");

  expect(config.server?.proxy?.["/api"]).toMatchObject({
    target: "http://127.0.0.1:4545",
    changeOrigin: false,
  });
});

test("enforces the approved browser coverage policy", async () => {
  const { default: config } = await import("./vite.config.js");

  expect(config.test?.coverage).toMatchObject({
    provider: "v8",
    include: ["src/**/*.{ts,tsx}"],
    thresholds: {
      lines: 80,
      statements: 80,
      functions: 80,
      branches: 75,
    },
  });
});

test("keeps production JavaScript chunks within Vite's warning threshold", async () => {
  process.env.NODE_ENV = "production";
  const root = import.meta.dirname;
  const config = await resolveConfig(
    { root, logLevel: "silent" },
    "build",
    "production",
  );
  expect(config.build.chunkSizeWarningLimit).toBe(500);
  const warningThresholdBytes = config.build.chunkSizeWarningLimit * 1_000;

  const result = await build({
    root,
    logLevel: "silent",
    build: { write: false },
  });
  if (!Array.isArray(result) && "close" in result) {
    await result.close();
    throw new Error("production build unexpectedly entered watch mode");
  }

  const outputs = Array.isArray(result) ? result : [result];
  const oversizedChunks = outputs
    .flatMap((output) => output.output)
    .filter((artifact) => artifact.type === "chunk")
    .map((chunk) => ({
      fileName: chunk.fileName,
      bytes: Buffer.byteLength(chunk.code),
    }))
    .filter((chunk) => chunk.bytes > warningThresholdBytes);

  expect(oversizedChunks).toEqual([]);
});
