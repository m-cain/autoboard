import { describe, expect, it } from "vitest";
import {
  decodeBrowserActivityEvent,
  decodeBrowserProjectBoard,
  decodeBrowserProjectList,
  decodeBrowserTicketDetail,
  decodeBrowserTicketList,
} from "../src/browser.js";

const project = {
  id: "11111111-1111-4111-8111-111111111111",
  key: "AUTO",
  name: "Autoboard",
  description: "",
  state: "active",
  revision: 1,
  created_attribution: { performed_by: "codex", initiated_by: "me" },
  inserted_at: "2026-07-25T12:00:00Z",
  updated_at: "2026-07-25T12:00:00Z",
};

const ticket = {
  id: "22222222-2222-4222-8222-222222222222",
  identifier: "AUTO-1",
  project_id: project.id,
  title: "Measure coverage",
  description: "",
  status: "ready",
  priority: "high",
  assignee: "codex",
  revision: 1,
  created_attribution: { performed_by: "codex", initiated_by: "me" },
  parent_ticket_id: null,
  labels: [
    {
      id: "33333333-3333-4333-8333-333333333333",
      project_id: project.id,
      name: "quality",
    },
  ],
  blocked: false,
  comment_count: 1,
  attachment_count: 1,
  inserted_at: "2026-07-25T12:00:00Z",
  updated_at: "2026-07-25T12:00:00.123Z",
};

const projectList = {
  active: [project],
  archived: [],
};

describe("Go-generated browser contracts", () => {
  it("accepts valid responses and rejects missing or extra fields", () => {
    expect(decodeBrowserProjectList(projectList)).toEqual(projectList);
    expect(() =>
      decodeBrowserProjectList({ ...projectList, leaked: true }),
    ).toThrow();
    expect(() =>
      decodeBrowserProjectList({
        active: [{ ...projectList.active[0], revision: undefined }],
        archived: [],
      }),
    ).toThrow();
  });

  it("validates the complete SSE activity payload", () => {
    const event = {
      id: 1,
      event_type: "project.created",
      attribution: { performed_by: "codex", initiated_by: "me" },
      attribution: { performed_by: "codex", initiated_by: "me" },
      project_id: "11111111-1111-4111-8111-111111111111",
      ticket_id: null,
      payload: {},
      inserted_at: "2026-07-25T12:00:00Z",
    };
    expect(decodeBrowserActivityEvent(event)).toEqual(event);
    expect(() =>
      decodeBrowserActivityEvent({ ...event, actor: "somebody" }),
    ).toThrow();
  });

  it("validates board and ticket list field constraints", () => {
    const board = {
      project,
      columns: {
        backlog: [],
        ready: [ticket],
        in_progress: [],
        done: [],
      },
    };
    expect(decodeBrowserProjectBoard(board)).toEqual(board);
    expect(decodeBrowserTicketList({ tickets: [ticket] })).toEqual({
      tickets: [ticket],
    });

    const invalidTickets = [
      { ...ticket, blocked: "no" },
      { ...ticket, attachment_count: -1 },
      { ...ticket, revision: 1.5 },
      { ...ticket, id: "not-a-uuid" },
      { ...ticket, updated_at: "not-a-date" },
      { ...ticket, title: "" },
      { ...ticket, title: "x".repeat(501) },
      { ...ticket, identifier: 1 },
      { ...ticket, priority: "eventually" },
      { ...ticket, parent_ticket_id: 42 },
    ];
    for (const invalidTicket of invalidTickets) {
      expect(() =>
        decodeBrowserTicketList({ tickets: [invalidTicket] }),
      ).toThrow();
    }
    expect(() =>
      decodeBrowserProjectBoard({
        ...board,
        columns: { ...board.columns, backlog: {} },
      }),
    ).toThrow(/must be an array/);
    expect(() =>
      decodeBrowserProjectList({
        active: [{ ...project, key: "auto" }],
        archived: [],
      }),
    ).toThrow(/invalid format/);
  });

  it("validates hydrated ticket relationships and attachment metadata", () => {
    const detail = {
      ...ticket,
      project,
      parent: null,
      subtasks: [],
      blockers: [],
      blocked_tickets: [],
      comments: [
        {
          id: "44444444-4444-4444-8444-444444444444",
          ticket_id: ticket.id,
          project_id: project.id,
          body: "Covered",
          attribution: { performed_by: "codex", initiated_by: "me" },
          attribution: { performed_by: "codex", initiated_by: "me" },
          inserted_at: "2026-07-25T12:00:00Z",
        },
      ],
      attachments: [
        {
          id: "55555555-5555-4555-8555-555555555555",
          ticket_id: ticket.id,
          project_id: project.id,
          original_filename: "coverage.txt",
          media_type: "text/plain",
          byte_size: 7,
          sha256: "a".repeat(64),
          attribution: { performed_by: "system", initiated_by: "system" },
          attribution: { performed_by: "system", initiated_by: "system" },
          inserted_at: "2026-07-25T12:00:00Z",
        },
      ],
      activity: [
        {
          id: 1,
          event_type: "ticket.created",
          attribution: { performed_by: "codex", initiated_by: "me" },
          attribution: { performed_by: "codex", initiated_by: "me" },
          project_id: project.id,
          ticket_id: ticket.id,
          payload: {},
          inserted_at: "2026-07-25T12:00:00Z",
        },
      ],
    };
    expect(decodeBrowserTicketDetail(detail)).toEqual(detail);
    expect(() =>
      decodeBrowserTicketDetail({
        ...detail,
        attachments: [{ ...detail.attachments[0], sha256: "invalid" }],
      }),
    ).toThrow(/invalid format/);
    expect(() =>
      decodeBrowserTicketDetail({
        ...detail,
        comments: [{ ...detail.comments[0], body: "x".repeat(100_001) }],
      }),
    ).toThrow(/too long/);
    expect(() =>
      decodeBrowserTicketDetail({
        ...detail,
        activity: [{ ...detail.activity[0], payload: [] }],
      }),
    ).toThrow(/must be an object/);
  });

  it("rejects impossible calendar components", () => {
    for (const inserted_at of [
      "2026-13-01T12:00:00Z",
      "2026-02-30T12:00:00Z",
      "2026-01-01T25:00:00Z",
    ]) {
      expect(() =>
        decodeBrowserProjectList({
          active: [{ ...project, inserted_at }],
          archived: [],
        }),
      ).toThrow(/RFC 3339/);
    }
  });
});
