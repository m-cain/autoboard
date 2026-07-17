import { describe, expect, it } from "vitest"
import type { ActivityEvent } from "@autoboard/contracts"
import { createRevalidationCoalescer, isActivityRelevant } from "../events/revalidation.js"

const projectId = "11111111-1111-4111-8111-111111111111"
const ticketId = "22222222-2222-4222-8222-222222222222"
const event: ActivityEvent = { id: 4, event_type: "ticket.updated", actor: "codex", project_id: projectId, ticket_id: ticketId, payload: {}, inserted_at: "2026-07-17T12:34:56Z" }
const projects = [{ id: projectId, key: "AUTO", name: "Autoboard", description: "", state: "active" as const, revision: 1, inserted_at: "2026-07-17T12:34:56Z", updated_at: "2026-07-17T12:34:56Z" }]

describe("live revalidation", () => {
  it("keeps the always-visible root data current for global project and ticket mutations", () => {
    const otherProject = "33333333-3333-4333-8333-333333333333"
    const crossProject = { ...event, project_id: otherProject }

    for (const eventType of ["project.created", "project.updated", "project.archived", "project.restored", "ticket.created", "ticket.updated", "ticket.transitioned"]) {
      expect(isActivityRelevant({ ...crossProject, event_type: eventType }, "/projects/AUTO", projects)).toBe(true)
      expect(isActivityRelevant({ ...crossProject, event_type: eventType }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(true)
    }

    expect(isActivityRelevant({ ...crossProject, event_type: "comment.added" }, "/projects/AUTO", projects)).toBe(false)
    expect(isActivityRelevant({ ...crossProject, event_type: "attachment.added" }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(false)
  })

  it("invalidates root-affecting ticket mutations globally and detail-only events locally", () => {
    expect(isActivityRelevant(event, "/projects/AUTO", projects)).toBe(true)
    expect(isActivityRelevant(event, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: "33333333-3333-4333-8333-333333333333" }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: null }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: false })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: "33333333-3333-4333-8333-333333333333" }, "/tickets/AUTO-1", projects, { id: ticketId, project_id: projectId, drawer: true })).toBe(true)
    expect(isActivityRelevant({ ...event, ticket_id: null }, "/projects", projects)).toBe(true)
    expect(isActivityRelevant(event, "/projects/OTHER", projects)).toBe(true)
    expect(isActivityRelevant({ ...event, project_id: "33333333-3333-4333-8333-333333333333" }, "/triage", projects)).toBe(true)
  })

  it("invalidates a direct detail page for its visible relationships and newly created relationships only", () => {
    const current = {
      id: ticketId,
      project_id: projectId,
      drawer: false,
      relatedTicketIds: ["33333333-3333-4333-8333-333333333333"],
    }
    const related = "33333333-3333-4333-8333-333333333333"
    const unrelated = "44444444-4444-4444-8444-444444444444"

    expect(isActivityRelevant({ ...event, ticket_id: related }, "/tickets/AUTO-1", projects, current)).toBe(true)
    expect(isActivityRelevant({ ...event, event_type: "comment.added", ticket_id: unrelated }, "/tickets/AUTO-1", projects, current)).toBe(false)
    expect(isActivityRelevant({ ...event, event_type: "ticket.created", ticket_id: unrelated, payload: { parent_ticket_id: ticketId } }, "/tickets/AUTO-1", projects, current)).toBe(true)
    expect(isActivityRelevant({ ...event, event_type: "ticket.created", ticket_id: unrelated, payload: { parent_ticket_id: unrelated } }, "/tickets/AUTO-1", projects, current)).toBe(true)
    expect(isActivityRelevant({ ...event, event_type: "dependency.created", ticket_id: unrelated, payload: { blocker_ticket_id: ticketId } }, "/tickets/AUTO-1", projects, current)).toBe(true)
    expect(isActivityRelevant({ ...event, event_type: "dependency.deleted", ticket_id: unrelated, payload: { blocker_ticket_id: unrelated } }, "/tickets/AUTO-1", projects, current)).toBe(false)
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
    coalesce.request()
    expect(scheduled).toBeUndefined()
    expect(revalidated).toBe(1)
  })
})
