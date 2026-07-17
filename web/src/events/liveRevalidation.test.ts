import { describe, expect, it, vi } from "vitest"
import type { ActivityEvent } from "@autoboard/contracts"
import { activityStream, type EventSourceLike } from "./activityStream.js"
import { startActivityRevalidation } from "./liveRevalidation.js"

const projectId = "11111111-1111-4111-8111-111111111111"
const event = (id: number, idProject = projectId): ActivityEvent => ({ id, event_type: "ticket.updated", actor: "codex", project_id: idProject, ticket_id: null, payload: {}, inserted_at: "2026-07-17T12:34:56Z" })

class FakeEventSource implements EventSourceLike {
  readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>()
  onerror: (() => void) | null = null
  closed = false
  addEventListener(type: string, listener: (message: MessageEvent<string>) => void) { const listeners = this.listeners.get(type) ?? new Set(); listeners.add(listener); this.listeners.set(type, listeners) }
  removeEventListener(type: string, listener: (message: MessageEvent<string>) => void) { this.listeners.get(type)?.delete(listener) }
  close() { this.closed = true }
  emit(activity: ActivityEvent) { this.listeners.get("activity")?.forEach((listener) => listener({ data: JSON.stringify(activity), lastEventId: String(activity.id) } as MessageEvent<string>)) }
}

describe("startActivityRevalidation", () => {
  it("revalidates one time for a relevant burst, ignores unrelated events, and closes its EventSource", async () => {
    const sources: FakeEventSource[] = []
    const timers: Array<() => void> = []
    const revalidate = vi.fn()
    const stop = startActivityRevalidation({
      stream: activityStream({ createEventSource: () => { const source = new FakeEventSource(); sources.push(source); return source } }),
      relevant: (activity) => activity.project_id === projectId,
      revalidate,
      schedule: (work) => { timers.push(work); return timers.length },
      cancel: () => undefined,
    })
    await vi.waitFor(() => expect(sources).toHaveLength(1))
    sources[0]!.emit(event(1, "33333333-3333-4333-8333-333333333333"))
    sources[0]!.emit(event(2))
    sources[0]!.emit(event(3))
    await vi.waitFor(() => expect(timers).toHaveLength(1))
    timers[0]!()
    expect(revalidate).toHaveBeenCalledTimes(1)
    stop()
    await vi.waitFor(() => expect(sources[0]!.closed).toBe(true))
  })
})
