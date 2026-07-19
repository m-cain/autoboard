import { afterEach, expect, test, vi } from "vitest";

const originalPort = process.env.AUTOBOARD_HTTP_PORT;

afterEach(() => {
  if (originalPort === undefined) delete process.env.AUTOBOARD_HTTP_PORT;
  else process.env.AUTOBOARD_HTTP_PORT = originalPort;
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
