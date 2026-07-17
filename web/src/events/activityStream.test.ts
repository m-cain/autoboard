import { Effect, Fiber, Stream } from "effect"
import { afterEach, describe, expect, it, vi } from "vitest"
import { activityStream, type EventSourceLike } from "./activityStream.js"

const event = {
  id: 12,
  event_type: "ticket.updated",
  actor: "codex" as const,
  project_id: "11111111-1111-4111-8111-111111111111",
  ticket_id: "22222222-2222-4222-8222-222222222222",
  payload: { changed: ["status"] },
  inserted_at: "2026-07-17T12:34:56Z",
}

class FakeEventSource implements EventSourceLike {
  readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  onerror: (() => void) | null = null
  closed = false

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    const listeners = this.listeners.get(type) ?? new Set()
    listeners.add(listener)
    this.listeners.set(type, listeners)
  }

  removeEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    this.listeners.get(type)?.delete(listener)
  }

  close() { this.closed = true }
  emit(type: string, payload: string, lastEventId = "") { this.listeners.get(type)?.forEach((listener) => listener({ data: payload, lastEventId } as MessageEvent<string>)) }
  fail() { this.onerror?.() }
}

const settle = () => new Promise<void>((resolve) => setTimeout(resolve, 0))
const fibers: Fiber.Fiber<unknown, unknown>[] = []
afterEach(async () => { await Promise.all(fibers.splice(0).map((fiber) => Effect.runPromise(Fiber.interrupt(fiber)))) })

describe("activityStream", () => {
  it("emits only strictly increasing consistent ActivityEvents and reconnects from the greatest cursor", async () => {
    const sources: FakeEventSource[] = []
    const urls: string[] = []
    const reconnects: Array<{ readonly work: () => void; readonly delay: number }> = []
    const seen: number[] = []
    const fiber = Effect.runFork(Stream.runForEach(
      activityStream({
        createEventSource: (url) => { urls.push(url); const source = new FakeEventSource(); sources.push(source); return source },
        schedule: (reconnect, delay) => { reconnects.push({ work: reconnect, delay }); return reconnects.length },
        cancel: () => undefined,
      }),
      (activity) => Effect.sync(() => seen.push(activity.id)),
    ))
    fibers.push(fiber)
    await settle()

    sources[0]!.emit("activity", "not JSON")
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 8 }), "8")
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 8 }), "8")
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 7 }), "7")
    sources[0]!.emit("activity", JSON.stringify({ ...event, id: 11 }), "10")
    sources[0]!.emit("activity", JSON.stringify(event), "12")
    await vi.waitFor(() => expect(seen).toEqual([8, 12]))

    sources[0]!.fail()
    expect(reconnects.map(({ delay }) => delay)).toEqual([250])
    reconnects[0]!.work()
    expect(urls[1]).toContain("last_event_id=12")
    expect(sources[0]!.closed).toBe(true)
    sources[1]!.fail()
    reconnects[1]!.work()
    sources[2]!.fail()
    reconnects[2]!.work()
    sources[3]!.fail()
    reconnects[3]!.work()
    sources[4]!.fail()
    reconnects[4]!.work()
    sources[5]!.fail()
    reconnects[5]!.work()
    sources[6]!.fail()
    reconnects[6]!.work()
    sources[7]!.fail()
    expect(reconnects.map(({ delay }) => delay)).toEqual([250, 500, 1000, 2000, 4000, 8000, 10_000, 10_000])
  })
})
