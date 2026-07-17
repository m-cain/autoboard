// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest"
import { cleanup, render, screen } from "@testing-library/react"
import { Effect } from "effect"
import { createMemoryRouter, RouterProvider } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { ApiClientService } from "./api/client.js"
import { createAppRoutes } from "./router.js"
import type { Project, ProjectBoard, TicketDetail } from "@autoboard/contracts"

const project = { id: "11111111-1111-4111-8111-111111111111", key: "AUTO", name: "Autoboard", description: "", state: "active", revision: 1, inserted_at: "2026-07-16T12:34:56Z", updated_at: "2026-07-16T12:34:56Z" } as Project
const board = { project, columns: { backlog: [], ready: [], in_progress: [], done: [] } } as ProjectBoard
const detail = {
  id: "22222222-2222-4222-8222-222222222222", identifier: "AUTO-1", project_id: project.id, title: "Inspect ticket", description: "", status: "ready", priority: "medium", assignee: "codex", revision: 1,
  parent_ticket_id: null, labels: [], blocked: false, comment_count: 0, attachment_count: 0, inserted_at: "2026-07-16T12:34:56Z", updated_at: "2026-07-16T12:34:56Z",
  project, parent: null, subtasks: [], blockers: [], blocked_tickets: [], comments: [], attachments: [], activity: [],
} as TicketDetail
const client: ApiClientService = {
  listProjects: () => Effect.succeed({ active: [project], archived: [] }),
  listTriage: () => Effect.succeed({ tickets: [] }),
  getProjectBoard: () => Effect.succeed(board),
  getCanceledTickets: () => Effect.succeed({ tickets: [] }),
  getTicket: () => Effect.succeed(detail),
}

const renderRoute = (entry: string, api: ApiClientService = client) => {
  const router = createMemoryRouter(createAppRoutes(api), { initialEntries: [entry] })
  return { router, ...render(<RouterProvider router={router} />) }
}

afterEach(cleanup)

describe("data routes", () => {
  it.each(["/", "/projects"])("renders the project index from root loader data at %s", async (entry) => {
    renderRoute(entry)
    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: /Autoboard/ }).length).toBeGreaterThan(0)
  })

  it("renders a dedicated not-found child inside the shell", async () => {
    const { container } = renderRoute("/not-a-route")
    expect(await screen.findByRole("heading", { name: "This view does not exist" })).toBeInTheDocument()
    expect(screen.queryByText("Autoboard is unavailable")).not.toBeInTheDocument()
    expect(container.querySelectorAll("main")).toHaveLength(1)
  })

  it("cancels a superseded board navigation without rendering an unavailable state", async () => {
    let rejectBoard: ((error: unknown) => void) | undefined
    const board = vi.fn((_key: string, signal?: AbortSignal) => Effect.tryPromise({
      try: () => new Promise((_, reject) => {
        rejectBoard = reject
        signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true })
      }),
      catch: (error) => error,
    }))
    const api = { ...client, getProjectBoard: board } as ApiClientService
    const { router } = renderRoute("/projects/AUTO", api)
    await vi.waitFor(() => expect(board).toHaveBeenCalledTimes(1))
    await router.navigate("/projects")
    rejectBoard?.(new DOMException("Aborted", "AbortError"))
    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument()
    expect(screen.queryByText("Autoboard is unavailable")).not.toBeInTheDocument()
  })

  it("has no router console warnings after root data resolves", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => undefined)
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined)
    renderRoute("/")
    await screen.findByRole("heading", { name: "Projects" })
    expect(error).not.toHaveBeenCalled()
    expect(warn).not.toHaveBeenCalled()
    error.mockRestore()
    warn.mockRestore()
  })

  it("renders a ticket as a direct deep-link page", async () => {
    renderRoute("/tickets/AUTO-1")
    expect(await screen.findByRole("heading", { name: "Inspect ticket" })).toBeInTheDocument()
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })

  it("keeps the project board accessible beneath a ticket drawer opened from it", async () => {
    const router = createMemoryRouter(createAppRoutes(client), {
      initialEntries: [{ pathname: "/tickets/AUTO-1", state: { backgroundLocation: { pathname: "/projects/AUTO", search: "", hash: "" } } }],
    })
    render(<RouterProvider router={router} />)
    expect(await screen.findByRole("dialog", { name: "Ticket detail" })).toBeInTheDocument()
    expect(screen.getByText("Autoboard", { selector: "h1" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back to board" })).toHaveAttribute("href", "/projects/AUTO")
  })
})
