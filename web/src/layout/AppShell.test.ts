import { describe, expect, it } from "vitest"
import type { ActivityEvent } from "@autoboard/contracts"
import { createRevalidationCoalescer, isActivityRelevant } from "../events/revalidation.js"

const projectId = "11111111-1111-4111-8111-111111111111"
const ticketId = "22222222-2222-4222-8222-222222222222"
const event: ActivityEvent = { id: 4, event_type: "ticket.updated", actor: "codex", project_id: projectId, ticket_id: ticketId, payload: {}, inserted_at: "2026-07-17T12:34:56Z" }
const projects = [{ id: projectId, key: "AUTO", name: "Autoboard", description: "", state: "active" as const, revision: 1, inserted_at: "2026-07-17T12:34:56Z", updated_at: "2026-07-17T12:34:56Z" }]

describe("live revalidation", () => {
  it("only invalidates the current project or ticket", () => {
    expect(isActivityRelevant(event, "/projects/AUTO", projects)).toBe(true)
    expect(isActivityRelevant(event, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: "33333333-3333-4333-8333-333333333333" }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(false)
    expect(isActivityRelevant({ ...event, ticket_id: null }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: "33333333-3333-4333-8333-333333333333" }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: true })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: null }, "/projects", projects)).toBe(true)
    expect(isActivityRelevant(event, "/projects/OTHER", projects)).toBe(false)
    expect(isActivityRelevant({ ...event, project_id: "33333333-3333-4333-8333-333333333333" }, "/triage", projects)).toBe(false)
  })

  it("coalesces a burst into one revalidation and cancels pending work", () => {
    let scheduled: (() => void) | undefined
    let revalidated = 0
    const coalesce = createRevalidationCoalescer(() => { revalidated += 1 }, (work) => { scheduled = work; return 1 }, () => { scheduled = undefined })
    coalesce.request()
    coalesce.request()
    expect(revalidated).toBe(0)
    scheduled?.()
    expect(revalidated).toBe(1)
    coalesce.request()
    coalesce.dispose()
    expect(scheduled).toBeUndefined()
  })
})
