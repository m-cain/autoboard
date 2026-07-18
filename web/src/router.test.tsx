// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Effect } from "effect";
import { createMemoryRouter, RouterProvider } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ApiClientService } from "./api/client.js";
import { createAppRoutes } from "./router.js";
import { boardSnapshot } from "./boardState.js";
import type {
  Project,
  ProjectBoard,
  TicketDetail,
  TicketSummary,
} from "@autoboard/contracts";

const project = {
  id: "11111111-1111-4111-8111-111111111111",
  key: "AUTO",
  name: "Autoboard",
  description: "",
  state: "active",
  revision: 1,
  inserted_at: "2026-07-16T12:34:56Z",
  updated_at: "2026-07-16T12:34:56Z",
} as Project;
const ticketSummary = {
  id: "22222222-2222-4222-8222-222222222222",
  identifier: "AUTO-1",
  project_id: project.id,
  title: "Inspect ticket",
  description: "",
  status: "ready",
  priority: "medium",
  assignee: "codex",
  revision: 1,
  parent_ticket_id: null,
  labels: [],
  blocked: false,
  comment_count: 0,
  attachment_count: 0,
  inserted_at: "2026-07-16T12:34:56Z",
  updated_at: "2026-07-16T12:34:56Z",
} as TicketSummary;
const nestedSummary = {
  ...ticketSummary,
  id: "33333333-3333-4333-8333-333333333333",
  identifier: "AUTO-2",
  title: "Nested ticket",
};
const board = {
  project,
  columns: { backlog: [], ready: [ticketSummary], in_progress: [], done: [] },
} as ProjectBoard;
const detail = {
  id: "22222222-2222-4222-8222-222222222222",
  identifier: "AUTO-1",
  project_id: project.id,
  title: "Inspect ticket",
  description: "",
  status: "ready",
  priority: "medium",
  assignee: "codex",
  revision: 1,
  parent_ticket_id: null,
  labels: [],
  blocked: false,
  comment_count: 0,
  attachment_count: 0,
  inserted_at: "2026-07-16T12:34:56Z",
  updated_at: "2026-07-16T12:34:56Z",
  project,
  parent: null,
  subtasks: [],
  blockers: [nestedSummary],
  blocked_tickets: [],
  comments: [],
  attachments: [],
  activity: [],
} as TicketDetail;
const client: ApiClientService = {
  listProjects: () => Effect.succeed({ active: [project], archived: [] }),
  listTriage: () => Effect.succeed({ tickets: [] }),
  getProjectBoard: () => Effect.succeed(board),
  getCanceledTickets: () => Effect.succeed({ tickets: [] }),
  getTicket: (identifier) =>
    Effect.succeed(
      identifier === "AUTO-2"
        ? {
            ...detail,
            id: "33333333-3333-4333-8333-333333333333",
            identifier: "AUTO-2",
            title: "Nested ticket",
            blockers: [],
          }
        : detail,
    ),
};

const renderRoute = (entry: string, api: ApiClientService = client) => {
  const router = createMemoryRouter(createAppRoutes(api), {
    initialEntries: [entry],
  });
  return { router, ...render(<RouterProvider router={router} />) };
};

afterEach(cleanup);

describe("data routes", () => {
  it("keeps one shell EventSource and its cursor alive across route transitions", async () => {
    class ShellEventSource {
      static instances: ShellEventSource[] = [];
      onerror: (() => void) | null = null;
      constructor(_url: string) {
        ShellEventSource.instances.push(this);
      }
      addEventListener(
        _type: string,
        _listener: (event: MessageEvent<string>) => void,
      ) {}
      removeEventListener(
        _type: string,
        _listener: (event: MessageEvent<string>) => void,
      ) {}
      close() {}
    }
    vi.stubGlobal("EventSource", ShellEventSource);
    const { router } = renderRoute("/projects/AUTO");
    await screen.findByRole("heading", { name: "Autoboard" });
    await vi.waitFor(() => expect(ShellEventSource.instances).toHaveLength(1));
    await router.navigate("/triage");
    await screen.findByRole("heading", { name: "Triage" });
    await router.navigate("/tickets/AUTO-1");
    await screen.findByRole("heading", { name: "Inspect ticket" });
    await vi.waitFor(() => expect(ShellEventSource.instances).toHaveLength(1));
    vi.unstubAllGlobals();
  });

  it.each(["/", "/projects"])(
    "renders the project index from root loader data at %s",
    async (entry) => {
      renderRoute(entry);
      expect(
        await screen.findByRole("heading", { name: "Projects" }),
      ).toBeInTheDocument();
      expect(
        screen.getAllByRole("link", { name: /Autoboard/ }).length,
      ).toBeGreaterThan(0);
    },
  );

  it("renders a dedicated not-found child inside the shell", async () => {
    const { container } = renderRoute("/not-a-route");
    expect(
      await screen.findByRole("heading", { name: "This view does not exist" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Autoboard is unavailable"),
    ).not.toBeInTheDocument();
    expect(container.querySelectorAll("main")).toHaveLength(1);
  });

  it("cancels a superseded board navigation without rendering an unavailable state", async () => {
    let rejectBoard: ((error: unknown) => void) | undefined;
    const board = vi.fn((_key: string, signal?: AbortSignal) =>
      Effect.tryPromise({
        try: () =>
          new Promise((_, reject) => {
            rejectBoard = reject;
            signal?.addEventListener(
              "abort",
              () => reject(new DOMException("Aborted", "AbortError")),
              { once: true },
            );
          }),
        catch: (error) => error,
      }),
    );
    const api = { ...client, getProjectBoard: board } as ApiClientService;
    const { router } = renderRoute("/projects/AUTO", api);
    await vi.waitFor(() => expect(board).toHaveBeenCalledTimes(1));
    await router.navigate("/projects");
    rejectBoard?.(new DOMException("Aborted", "AbortError"));
    expect(
      await screen.findByRole("heading", { name: "Projects" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Autoboard is unavailable"),
    ).not.toBeInTheDocument();
  });

  it("has no router console warnings after root data resolves", async () => {
    const error = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    renderRoute("/");
    await screen.findByRole("heading", { name: "Projects" });
    expect(error).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
    error.mockRestore();
    warn.mockRestore();
  });

  it("renders a ticket as a direct deep-link page", async () => {
    renderRoute("/tickets/AUTO-1");
    expect(
      await screen.findByRole("heading", { name: "Inspect ticket" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps the project board accessible beneath a ticket drawer opened from it", async () => {
    const router = createMemoryRouter(createAppRoutes(client), {
      initialEntries: [
        "/projects/AUTO",
        {
          pathname: "/tickets/AUTO-1",
          state: {
            backgroundLocation: {
              pathname: "/projects/AUTO",
              search: "",
              hash: "",
            },
            drawerDepth: 1,
            originIdentifier: "AUTO-1",
          },
        },
      ],
      initialIndex: 1,
    });
    render(<RouterProvider router={router} />);
    expect(
      await screen.findByRole("dialog", { name: "Ticket detail" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Autoboard", { selector: "h1" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Back to board" })).toHaveAttribute(
      "href",
      "/projects/AUTO",
    );
    fireEvent.click(screen.getByRole("link", { name: "Back to board" }));
    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe("/projects/AUTO"),
    );
    await vi.waitFor(() =>
      expect(document.activeElement).toHaveAttribute(
        "data-ticket-identifier",
        "AUTO-1",
      ),
    );
    await router.navigate(-1);
    expect(router.state.location.pathname).toBe("/projects/AUTO");
  });

  it("keeps nested ticket links in the drawer history and closes back through each prior view", async () => {
    const router = createMemoryRouter(createAppRoutes(client), {
      initialEntries: [
        "/projects/AUTO",
        {
          pathname: "/tickets/AUTO-1",
          state: {
            backgroundLocation: {
              pathname: "/projects/AUTO",
              search: "",
              hash: "",
            },
            drawerDepth: 1,
            originIdentifier: "AUTO-1",
          },
        },
      ],
      initialIndex: 1,
    });
    render(<RouterProvider router={router} />);
    const nested = await screen.findByRole("link", { name: /Nested ticket/ });
    fireEvent.click(nested);
    expect(
      await screen.findByRole("heading", { name: "Nested ticket" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("dialog", { name: "Ticket detail" }),
    ).toBeInTheDocument();
    await router.navigate(-1);
    expect(
      await screen.findByRole("heading", { name: "Inspect ticket" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("dialog", { name: "Ticket detail" }),
    ).toBeInTheDocument();
    expect(document.activeElement?.closest("[role=dialog]")).toBe(
      screen.getByRole("dialog", { name: "Ticket detail" }),
    );
    fireEvent.click(screen.getByRole("link", { name: "Back to board" }));
    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe("/projects/AUTO"),
    );
  });

  it("opens a drawer only from a project board, not triage or canceled views", async () => {
    const listClient = {
      ...client,
      listTriage: () => Effect.succeed({ tickets: [ticketSummary] }),
      getCanceledTickets: () => Effect.succeed({ tickets: [ticketSummary] }),
    } as ApiClientService;
    const { router } = renderRoute("/triage", listClient);
    await screen.findByRole("heading", { name: "Triage" });
    fireEvent.click(screen.getByRole("link", { name: "Inspect ticket" }));
    await screen.findByRole("heading", { name: "Inspect ticket" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await router.navigate("/projects/AUTO/canceled");
    fireEvent.click(
      await screen.findByRole("link", { name: "Inspect ticket" }),
    );
    await screen.findByRole("heading", { name: "Inspect ticket" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await router.navigate("/tickets/AUTO-1", {
      state: {
        backgroundLocation: { pathname: "/projects/AUTO" },
        drawerDepth: 1,
        originIdentifier: "AUTO-1",
      },
    });
    expect(
      await screen.findByRole("dialog", { name: "Ticket detail" }),
    ).toBeInTheDocument();
  });

  it("closes a depth-two drawer directly to its originating card", async () => {
    const scrollTo = vi
      .spyOn(window, "scrollTo")
      .mockImplementation(() => undefined);
    const { router } = renderRoute("/projects/AUTO");
    fireEvent.click(
      await screen.findByRole("link", { name: "Inspect ticket" }),
    );
    fireEvent.click(await screen.findByRole("link", { name: /Nested ticket/ }));
    expect(
      await screen.findByRole("heading", { name: "Nested ticket" }),
    ).toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe("/projects/AUTO"),
    );
    await vi.waitFor(() =>
      expect(document.activeElement).toHaveAttribute(
        "data-ticket-identifier",
        "AUTO-1",
      ),
    );
    scrollTo.mockRestore();
  });

  it("preserves board window and Kanban scroll through a nested drawer and final close", async () => {
    const scrollTo = vi
      .spyOn(window, "scrollTo")
      .mockImplementation(() => undefined);
    Object.defineProperty(window, "scrollX", { configurable: true, value: 17 });
    Object.defineProperty(window, "scrollY", { configurable: true, value: 81 });
    const { router } = renderRoute("/projects/AUTO");
    const title = await screen.findByRole("link", { name: "Inspect ticket" });
    const initialScroller = document.querySelector<HTMLElement>(
      "[data-kanban-scroll]",
    );
    expect(initialScroller).not.toBeNull();
    initialScroller!.scrollLeft = 43;
    fireEvent.click(title);
    await screen.findByRole("dialog", { name: "Ticket detail" });
    expect(
      boardSnapshot(
        (router.state.location.state as { boardSnapshotKey: string })
          .boardSnapshotKey,
      ),
    ).toEqual({ scrollX: 17, scrollY: 81, kanbanScrollLeft: 43 });
    expect(
      document.querySelector<HTMLElement>("[data-kanban-scroll]")?.scrollLeft,
    ).toBe(43);
    fireEvent.click(screen.getByRole("link", { name: /Nested ticket/ }));
    await screen.findByRole("heading", { name: "Nested ticket" });
    await router.navigate(-1);
    await screen.findByRole("heading", { name: "Inspect ticket" });
    fireEvent.keyDown(document, { key: "Escape" });
    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe("/projects/AUTO"),
    );
    expect(
      document.querySelector<HTMLElement>("[data-kanban-scroll]")?.scrollLeft,
    ).toBe(43);
    expect(scrollTo).toHaveBeenLastCalledWith(17, 81);
    scrollTo.mockRestore();
  });
});
