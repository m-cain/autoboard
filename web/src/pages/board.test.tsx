// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type {
  Project,
  ProjectBoard,
  TicketSummary,
} from "@autoboard/contracts";
import { ProjectBoardPage } from "./ProjectBoardPage.js";
import { ProjectsPage } from "./ProjectsPage.js";
import { TriagePage } from "./TriagePage.js";
import { CanceledPage } from "./CanceledPage.js";

const timestamp = "2026-07-16T12:34:56Z";
const project = (key: string, state: Project["state"] = "active"): Project => ({
  id: `11111111-1111-4111-8111-${key.padStart(12, "0").slice(-12)}`,
  key,
  name: `${key} project`,
  description: "",
  state,
  revision: 1,
  inserted_at: timestamp,
  updated_at: timestamp,
});

const ticket = (
  identifier: string,
  status: TicketSummary["status"],
  overrides: Partial<TicketSummary> = {},
): TicketSummary => ({
  id: `22222222-2222-4222-8222-${identifier
    .replace(/[^0-9]/g, "")
    .padStart(12, "0")
    .slice(-12)}`,
  identifier,
  project_id: "11111111-1111-4111-8111-000000000001",
  title: `Ticket ${identifier}`,
  description: "",
  status,
  priority: "none",
  assignee: "unassigned",
  revision: 1,
  parent_ticket_id: null,
  labels: [],
  blocked: false,
  comment_count: 0,
  attachment_count: 0,
  inserted_at: timestamp,
  updated_at: timestamp,
  ...overrides,
});

const renderPage = (element: React.ReactNode) =>
  render(<MemoryRouter>{element}</MemoryRouter>);

afterEach(cleanup);

describe("project pages", () => {
  it("groups active and archived projects with project links", () => {
    renderPage(
      <ProjectsPage
        projects={{
          active: [project("AUTO")],
          archived: [project("OLD", "archived")],
        }}
      />,
    );

    expect(
      screen.getByRole("heading", { name: "Projects" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /AUTO project/ })).toHaveAttribute(
      "href",
      "/projects/AUTO",
    );
    expect(
      screen.getByRole("heading", { name: "Archived projects" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /OLD project/ })).toHaveAttribute(
      "href",
      "/projects/OLD",
    );
  });

  it("uses the fixed kanban order and exposes ticket signals", () => {
    const board: ProjectBoard = {
      project: project("AUTO"),
      columns: {
        backlog: [ticket("AUTO-1", "backlog")],
        ready: [
          ticket("AUTO-2", "ready", {
            assignee: "codex",
            priority: "high",
            blocked: true,
            comment_count: 2,
            attachment_count: 1,
          }),
        ],
        in_progress: [ticket("AUTO-3", "in_progress")],
        done: [ticket("AUTO-4", "done")],
      },
    };
    renderPage(<ProjectBoardPage board={board} />);

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Backlog", "Ready", "In progress", "Done"]);
    expect(screen.getByRole("link", { name: /Ticket AUTO-2/ })).toHaveAttribute(
      "href",
      "/tickets/AUTO-2",
    );
    expect(screen.getByText("codex")).toBeInTheDocument();
    expect(screen.getByText("high")).toBeInTheDocument();
    expect(
      screen.getByText("Blocked by unresolved dependencies"),
    ).toBeInTheDocument();
    expect(screen.getByText("2 comments")).toBeInTheDocument();
    expect(screen.getByText("1 attachment")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Canceled tickets" }),
    ).toHaveAttribute("href", "/projects/AUTO/canceled");
  });

  it("renders a clear empty board state", () => {
    const board: ProjectBoard = {
      project: project("AUTO"),
      columns: { backlog: [], ready: [], in_progress: [], done: [] },
    };
    renderPage(<ProjectBoardPage board={board} />);
    expect(screen.getAllByText("No tickets")).toHaveLength(4);
  });

  it("renders triage and canceled ticket lists grouped by project", () => {
    const triageTicket = ticket("AUTO-5", "triage", {
      project_id: project("AUTO").id,
      assignee: "me",
      priority: "urgent",
    });
    const canceledTicket = ticket("AUTO-6", "canceled");
    const { rerender } = renderPage(
      <TriagePage tickets={[triageTicket]} projects={[project("AUTO")]} />,
    );
    expect(screen.getByRole("heading", { name: "Triage" })).toBeInTheDocument();
    expect(screen.getByText("AUTO project")).toBeInTheDocument();
    expect(screen.getByText("urgent")).toBeInTheDocument();

    rerender(
      <MemoryRouter>
        <CanceledPage project={project("AUTO")} tickets={[canceledTicket]} />
      </MemoryRouter>,
    );
    expect(
      screen.getByRole("heading", { name: "Canceled tickets" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Ticket AUTO-6/ })).toHaveAttribute(
      "href",
      "/tickets/AUTO-6",
    );
  });
});
