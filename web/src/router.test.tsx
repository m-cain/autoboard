// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest"
import { cleanup, render, screen } from "@testing-library/react"
import { Effect } from "effect"
import { createMemoryRouter, RouterProvider } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { ApiClientService } from "./api/client.js"
import { createAppRoutes } from "./router.js"
import type { Project, ProjectBoard } from "@autoboard/contracts"

const project = { id: "11111111-1111-4111-8111-111111111111", key: "AUTO", name: "Autoboard", description: "", state: "active", revision: 1, inserted_at: "2026-07-16T12:34:56Z", updated_at: "2026-07-16T12:34:56Z" } as Project
const board = { project, columns: { backlog: [], ready: [], in_progress: [], done: [] } } as ProjectBoard
const client: ApiClientService = {
  listProjects: () => Effect.succeed({ active: [project], archived: [] }),
  listTriage: () => Effect.succeed({ tickets: [] }),
  getProjectBoard: () => Effect.succeed(board),
  getCanceledTickets: () => Effect.succeed({ tickets: [] }),
  getTicket: () => Effect.die("not used"),
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
})
