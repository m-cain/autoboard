import { Effect, Fiber, Stream } from "effect";
import { afterEach, describe, expect, it, vi } from "vitest";
import { activityStream, type EventSourceLike } from "./activityStream.js";

const event = {
  id: 12,
  event_type: "ticket.updated",
  actor: "codex" as const,
  project_id: "11111111-1111-4111-8111-111111111111",
  ticket_id: "22222222-2222-4222-8222-222222222222",
  payload: { changed: ["status"] },
  inserted_at: "2026-07-17T12:34:56Z",
};

class FakeEventSource implements EventSourceLike {
  readonly listeners = new Map<
    string,
    Set<(event: MessageEvent<string>) => void>
  >();
  onerror: (() => void) | null = null;
  closed = false;
  closeCount = 0;

  addEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ) {
    const listeners = this.listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(
    type: string,
    listener: (event: MessageEvent<string>) => void,
  ) {
    this.listeners.get(type)?.delete(listener);
  }

  close() {
    this.closed = true;
    this.closeCount += 1;
  }
  emit(type: string, payload: string, lastEventId = "") {
    this.listeners
      .get(type)
      ?.forEach((listener) =>
        listener({ data: payload, lastEventId } as MessageEvent<string>),
      );
  }
  fail() {
    this.onerror?.();
  }
  listener(type: string) {
    return [...(this.listeners.get(type) ?? [])][0];
  }
}

const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 0));
const fibers: Fiber.Fiber<unknown, unknown>[] = [];
afterEach(async () => {
  await Promise.all(
    fibers.splice(0).map((fiber) => Effect.runPromise(Fiber.interrupt(fiber))),
  );
});

describe("activityStream", () => {
  it("emits only strictly increasing consistent ActivityEvents and reconnects from the greatest cursor", async () => {
    const sources: FakeEventSource[] = [];
    const urls: string[] = [];
    const reconnects: Array<{
      readonly work: () => void;
      readonly delay: number;
    }> = [];
    const seen: number[] = [];
    const fiber = Effect.runFork(
      Stream.runForEach(
        activityStream({
          createEventSource: (url) => {
            urls.push(url);
            const source = new FakeEventSource();
            sources.push(source);
            return source;
          },
          schedule: (reconnect, delay) => {
            reconnects.push({ work: reconnect, delay });
            return reconnects.length;
          },
          cancel: () => undefined,
        }),
        (activity) => Effect.sync(() => seen.push(activity.id)),
      ),
    );
    fibers.push(fiber);
    await settle();

    sources[0]!.emit("activity", "not JSON");
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 8 }), "8");
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 8 }), "8");
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 7 }), "7");
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 11 }), "10");
    sources[0]!.emit("activity", JSON.stringify(event), "12");
    await vi.waitFor(() => expect(seen).toEqual([8, 12]));

    sources[0]!.fail();
    expect(reconnects.map(({ delay }) => delay)).toEqual([250]);
    reconnects[0]!.work();
    expect(urls[1]).toContain("last_event_id=12");
    expect(sources[0]!.closed).toBe(true);
    sources[1]!.fail();
    reconnects[1]!.work();
    sources[2]!.fail();
    reconnects[2]!.work();
    sources[3]!.fail();
    reconnects[3]!.work();
    sources[4]!.fail();
    reconnects[4]!.work();
    sources[5]!.fail();
    reconnects[5]!.work();
    sources[6]!.fail();
    reconnects[6]!.work();
    sources[7]!.fail();
    expect(reconnects.map(({ delay }) => delay)).toEqual([
      250, 500, 1000, 2000, 4000, 8000, 10_000, 10_000,
    ]);
  });

  it("detaches retired sources so queued callbacks cannot advance the replay cursor", async () => {
    const sources: FakeEventSource[] = [];
    const urls: string[] = [];
    const reconnects: Array<{
      readonly work: () => void;
      readonly delay: number;
    }> = [];
    const seen: number[] = [];
    const fiber = Effect.runFork(
      Stream.runForEach(
        activityStream({
          createEventSource: (url) => {
            urls.push(url);
            const source = new FakeEventSource();
            sources.push(source);
            return source;
          },
          schedule: (work, delay) => {
            reconnects.push({ work, delay });
            return reconnects.length;
          },
          cancel: () => undefined,
        }),
        (activity) => Effect.sync(() => seen.push(activity.id)),
      ),
    );
    fibers.push(fiber);
    await settle();

    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 10 }), "10");
    await vi.waitFor(() => expect(seen).toEqual([10]));
    const retiredActivity = sources[0]!.listener("activity")!;
    const retiredError = sources[0]!.onerror!;
    sources[0]!.fail();
    expect(sources[0]!.closed).toBe(true);
    expect(sources[0]!.closeCount).toBe(1);
    expect(sources[0]!.listeners.get("activity")?.size).toBe(0);
    expect(sources[0]!.onerror).toBeNull();
    reconnects[0]!.work();
    expect(sources).toHaveLength(2);

    retiredActivity({
      data: JSON.stringify({ ...event, id: 99 }),
      lastEventId: "99",
    } as MessageEvent<string>);
    retiredError();
    expect(seen).toEqual([10]);
    expect(reconnects).toHaveLength(1);
    expect(sources[0]!.closeCount).toBe(1);

    sources[1]!.emit("activity", JSON.stringify({ ...event, id: 11 }), "11");
    await vi.waitFor(() => expect(seen).toEqual([10, 11]));
    sources[1]!.fail();
    reconnects[1]!.work();
    expect(urls[2]).toContain("last_event_id=11");

    const activeActivity = sources[2]!.listener("activity")!;
    const activeError = sources[2]!.onerror!;
    await Effect.runPromise(Fiber.interrupt(fiber));
    expect(sources[2]!.listeners.get("activity")?.size).toBe(0);
    expect(sources[2]!.onerror).toBeNull();
    expect(sources[2]!.closeCount).toBe(1);
    activeActivity({
      data: JSON.stringify({ ...event, id: 42 }),
      lastEventId: "42",
    } as MessageEvent<string>);
    activeError();
    expect(seen).toEqual([10, 11]);
    expect(reconnects).toHaveLength(2);
  });
});
