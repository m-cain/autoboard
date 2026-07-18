import { describe, expect, test, vi } from "vitest";
import { createCloseOnce, createSignalShutdown } from "../src/lifecycle.js";

describe("MCP lifecycle", () => {
  test("always closes the RPC client when MCP close fails", async () => {
    const closeServer = vi
      .fn()
      .mockRejectedValue(new Error("server close failed"));
    const closeClient = vi.fn().mockResolvedValue(undefined);
    const close = createCloseOnce(
      { close: closeServer },
      { close: closeClient },
    );

    await expect(close()).rejects.toThrow("server close failed");
    expect(closeClient).toHaveBeenCalledOnce();
  });

  test("shares one cleanup promise across concurrent signals and exits only after cleanup", async () => {
    let resolveClose!: () => void;
    const close = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveClose = resolve;
        }),
    );
    const exit = vi.fn();
    const shutdown = createSignalShutdown(close, exit, vi.fn());

    shutdown();
    shutdown();
    expect(close).toHaveBeenCalledOnce();
    expect(exit).not.toHaveBeenCalled();
    resolveClose();
    await Promise.resolve();
    await Promise.resolve();
    expect(exit).toHaveBeenCalledTimes(1);
    expect(exit).toHaveBeenCalledWith(0);
  });

  test("waits for failed cleanup before emitting one failing exit", async () => {
    const close = vi.fn().mockRejectedValue(new Error("close failed"));
    const exit = vi.fn();
    const log = vi.fn();
    const shutdown = createSignalShutdown(close, exit, log);

    shutdown();
    shutdown();
    await Promise.resolve();
    await Promise.resolve();
    expect(close).toHaveBeenCalledOnce();
    expect(log).toHaveBeenCalledWith(expect.stringContaining("close failed"));
    expect(exit).toHaveBeenCalledTimes(1);
    expect(exit).toHaveBeenCalledWith(1);
  });
});
