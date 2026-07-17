import { Effect } from "effect"
import { describe, expect, it, vi } from "vitest"
// @vitest-environment jsdom
import { ApiClient, createApiClient } from "./client.js"
import { createApiRunner } from "../runtime.js"

const projectResponse = {
  active: [{ id: "11111111-1111-4111-8111-111111111111", key: "AUTO", name: "Autoboard", description: "", state: "active", revision: 1, inserted_at: "2026-07-16T12:34:56Z", updated_at: "2026-07-16T12:34:56Z" }],
  archived: [],
}

describe("ApiClient", () => {
  it("uses GET and decodes the exact projects response", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(projectResponse), { status: 200 }))
    const client = createApiClient({ fetch, sleep: async () => undefined })
    await expect(Effect.runPromise(client.listProjects())).resolves.toEqual(projectResponse)
    expect(fetch).toHaveBeenCalledWith("/api/v1/projects", { method: "GET", headers: { accept: "application/json" } })
  })

  it("retries only a network failure and a 503 with fixed delays", async () => {
    const fetch = vi.fn()
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValueOnce(new Response("unavailable", { status: 503 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(projectResponse), { status: 200 }))
    const sleep = vi.fn().mockResolvedValue(undefined)
    const client = createApiClient({ fetch, sleep })
    await expect(Effect.runPromise(client.listProjects())).resolves.toEqual(projectResponse)
    expect(fetch).toHaveBeenCalledTimes(3)
    expect(sleep).toHaveBeenNthCalledWith(1, 250, undefined)
    expect(sleep).toHaveBeenNthCalledWith(2, 1000, undefined)
  })

  it("does not retry non-503 HTTP or malformed responses", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response("not found", { status: 404 }))
    const client = createApiClient({ fetch, sleep: async () => undefined })
    await expect(Effect.runPromise(client.listProjects())).rejects.toThrow("HTTP 404")
    expect(fetch).toHaveBeenCalledTimes(1)

    fetch.mockResolvedValue(new Response(JSON.stringify({ active: [] }), { status: 200 }))
    await expect(Effect.runPromise(client.listProjects())).rejects.toThrow("Response did not match")
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it("uses only GET requests for every browser read endpoint", async () => {
    const ticket = {
      id: "22222222-2222-4222-8222-222222222222", identifier: "AUTO-1", project_id: "11111111-1111-4111-8111-111111111111", title: "Ticket", description: "", status: "ready", priority: "none", assignee: "codex", revision: 1,
      parent_ticket_id: null, labels: [], blocked: false, comment_count: 0, attachment_count: 0, inserted_at: "2026-07-16T12:34:56Z", updated_at: "2026-07-16T12:34:56Z",
    }
    const board = { project: projectResponse.active[0], columns: { backlog: [], ready: [ticket], in_progress: [], done: [] } }
    const fetch = vi.fn((path: RequestInfo | URL, _init?: RequestInit) => {
      const requestPath = String(path)
      const payload = requestPath.includes("board") ? board
        : requestPath.includes("canceled") || requestPath.endsWith("triage") ? { tickets: [ticket] }
        : requestPath.includes("tickets/AUTO-1") ? { ...ticket, project: projectResponse.active[0], parent: null, subtasks: [], blockers: [], blocked_tickets: [], comments: [], attachments: [], activity: [] }
        : projectResponse
      return Promise.resolve(new Response(JSON.stringify(payload), { status: 200 }))
    })
    const client = createApiClient({ fetch, sleep: async () => undefined })

    await Promise.all([Effect.runPromise(client.listProjects()), Effect.runPromise(client.listTriage()), Effect.runPromise(client.getProjectBoard("AUTO")), Effect.runPromise(client.getCanceledTickets("AUTO")), Effect.runPromise(client.getTicket("AUTO-1"))])

    expect(fetch.mock.calls.map((args) => args[1]?.method)).toEqual(["GET", "GET", "GET", "GET", "GET"])
  })

  it("propagates a request signal and stops promptly when fetch is aborted", async () => {
    const controller = new AbortController()
    const fetch = vi.fn((_path: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true })))
    const client = createApiClient({ fetch, sleep: async () => undefined })
    const run = createApiRunner(client)
    const request = run(Effect.flatMap(ApiClient, (api) => api.listProjects(controller.signal)))
    controller.abort()
    await expect(request).rejects.toMatchObject({ _tag: "RequestAbortedError" })
    expect(fetch.mock.calls[0]?.[1]?.signal).toBe(controller.signal)
  })

  it("aborts retry sleeps and makes no further retry attempt", async () => {
    const controller = new AbortController()
    const fetch = vi.fn().mockResolvedValue(new Response("unavailable", { status: 503 }))
    const sleep = vi.fn((_milliseconds: number, signal?: AbortSignal) => new Promise<void>((_resolve, reject) => signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true })))
    const client = createApiClient({ fetch, sleep })
    const request = Effect.runPromise(client.listProjects(controller.signal))
    await vi.waitFor(() => expect(sleep).toHaveBeenCalledWith(250, controller.signal))
    controller.abort()
    await expect(request).rejects.toThrow("Request was aborted")
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})
