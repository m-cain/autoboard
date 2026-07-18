// @vitest-environment jsdom
import { Effect } from "effect";
import { describe, expect, it } from "vitest";
import { HttpError, type ApiClientService } from "./api/client.js";
import { createApiRunner } from "./runtime.js";

const service = {} as ApiClientService;

describe("API runtime", () => {
  it("preserves tagged client failures for router error boundaries", async () => {
    const run = createApiRunner(service);
    await expect(
      run(Effect.fail(new HttpError({ status: 404, message: "not found" }))),
    ).rejects.toMatchObject({ _tag: "HttpError", status: 404 });
  });
});
