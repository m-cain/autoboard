// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import type { TicketDetail, TicketSummary } from "@autoboard/contracts";
import { TicketDetailPage } from "./TicketDetailPage.js";

const timestamp = "2026-07-17T12:34:56Z";
const projectId = "11111111-1111-4111-8111-111111111111";
const ticketId = "22222222-2222-4222-8222-222222222222";

const summary = (
  identifier: string,
  overrides: Partial<TicketSummary> = {},
): TicketSummary => ({
  id:
    identifier === "AUTO-1" ? ticketId : "33333333-3333-4333-8333-333333333333",
  identifier,
  project_id: projectId,
  title: `${identifier} title`,
  description: "",
  status: "ready",
  priority: "high",
  assignee: "codex",
  revision: 2,
  parent_ticket_id: null,
  labels: [],
  blocked: false,
  comment_count: 0,
  attachment_count: 0,
  inserted_at: timestamp,
  updated_at: timestamp,
  ...overrides,
});

const detail: TicketDetail = {
  ...summary("AUTO-1", {
    title: "Render the real ticket",
    description:
      "## Safe Markdown\n\nA [useful link](https://example.test) and <script>window.pwned = true</script><img src=x onerror=alert(1) />",
    labels: [
      {
        id: "44444444-4444-4444-8444-444444444444",
        name: "frontend",
        project_id: projectId,
      },
    ],
    blocked: true,
    comment_count: 1,
    attachment_count: 1,
  }),
  project: {
    id: projectId,
    key: "AUTO",
    name: "Autoboard",
    description: "",
    state: "active",
    revision: 1,
    inserted_at: timestamp,
    updated_at: timestamp,
  },
  parent: null,
  subtasks: [summary("AUTO-2")],
  blockers: [summary("AUTO-3")],
  blocked_tickets: [summary("AUTO-4")],
  comments: [
    {
      id: "55555555-5555-4555-8555-555555555555",
      ticket_id: ticketId,
      project_id: projectId,
      body: "A **useful** comment",
      actor: "me",
      inserted_at: timestamp,
    },
  ],
  attachments: [
    {
      id: "66666666-6666-4666-8666-666666666666",
      ticket_id: ticketId,
      project_id: projectId,
      original_filename: "notes.txt",
      media_type: "text/plain",
      byte_size: 12,
      sha256: "a".repeat(64),
      actor: "codex",
      inserted_at: timestamp,
    },
  ],
  activity: [
    {
      id: 17,
      event_type: "ticket.updated",
      actor: "codex",
      project_id: projectId,
      ticket_id: ticketId,
      payload: { changed: ["status"] },
      inserted_at: timestamp,
    },
  ],
};

afterEach(cleanup);

describe("TicketDetailPage", () => {
  it("renders the complete read-only ticket detail with sanitized Markdown", () => {
    const { container } = render(
      <MemoryRouter>
        <TicketDetailPage ticket={detail} />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "Render the real ticket" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("codex")).not.toHaveLength(0);
    expect(screen.getByText("frontend")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Safe Markdown" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "useful link" })).toHaveAttribute(
      "href",
      "https://example.test",
    );
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    expect(container.innerHTML).not.toContain("onerror");
    expect(screen.getByRole("link", { name: /AUTO-3 title/ })).toHaveAttribute(
      "href",
      "/tickets/AUTO-3",
    );
    expect(screen.getByRole("link", { name: /AUTO-4 title/ })).toHaveAttribute(
      "href",
      "/tickets/AUTO-4",
    );
    expect(screen.getByRole("link", { name: /AUTO-2 title/ })).toHaveAttribute(
      "href",
      "/tickets/AUTO-2",
    );
    expect(screen.getByText("useful")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "notes.txt" })).toHaveAttribute(
      "href",
      "/api/v1/attachments/66666666-6666-4666-8666-666666666666",
    );
    expect(container.textContent).not.toContain("managed_path");
    expect(screen.getByText("ticket.updated")).toBeInTheDocument();
    expect(
      container.querySelectorAll(
        "form,input,textarea,select,button,[contenteditable=true]",
      ),
    ).toHaveLength(0);
  });
});
