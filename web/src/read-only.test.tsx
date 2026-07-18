// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import { AppShell } from "./layout/AppShell.js";
import { TicketCard } from "./components/TicketCard.js";
import type { TicketSummary } from "@autoboard/contracts";

const ticket: TicketSummary = {
  id: "22222222-2222-4222-8222-222222222222",
  identifier: "AUTO-1",
  project_id: "11111111-1111-4111-8111-111111111111",
  title: "Read the current state",
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
};

describe("read-only UI", () => {
  afterEach(cleanup);
  it("contains navigation links and no mutation controls", () => {
    const { container } = render(
      <MemoryRouter>
        <AppShell
          triageCount={1}
          projects={[{ key: "AUTO", name: "Autoboard" }]}
        >
          <TicketCard ticket={ticket} />
        </AppShell>
      </MemoryRouter>,
    );

    expect(
      screen.getAllByRole("link", { name: "Autoboard" })[0],
    ).toHaveAttribute("href", "/projects");
    expect(screen.getByRole("link", { name: /Triage \(1\)/ })).toHaveAttribute(
      "href",
      "/triage",
    );
    expect(screen.getAllByRole("link", { name: "Autoboard" })).toHaveLength(2);
    expect(
      container.querySelectorAll(
        "form,input,textarea,select,[contenteditable=true]",
      ),
    ).toHaveLength(0);
    expect(
      screen.queryByText(/create|edit|delete|move ticket/i),
    ).not.toBeInTheDocument();
    expect(container.querySelectorAll("button,[draggable=true]")).toHaveLength(
      0,
    );
  });
});
