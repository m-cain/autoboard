import { describe, expect, test } from "vitest";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import {
  IndeterminateWriteError,
  RpcError,
  RpcProtocolError,
} from "../src/rpc-error.js";
import {
  createMcpServer,
  MCP_INSTRUCTIONS,
  toolRegistry,
  type RpcCaller,
} from "../src/server.js";

const names = [
  "list_projects",
  "get_project_board",
  "search_tickets",
  "get_ticket",
  "list_actionable_tickets",
  "read_attachment",
  "create_project",
  "update_project",
  "archive_project",
  "restore_project",
  "create_ticket",
  "update_ticket",
  "transition_ticket",
  "add_comment",
  "add_attachment_from_path",
  "add_dependency",
  "remove_dependency",
];
const descriptions = [
  "List active and archived Autoboard projects.",
  "Read a project's Kanban board and project metadata.",
  "Search tickets globally or within one project.",
  "Read complete ticket detail, including relationships, comments, attachments, and activity.",
  "List ready, unblocked tickets assigned to codex; tickets assigned to `me` and tickets with non-terminal subtasks are excluded.",
  "Read a managed attachment's inline UTF-8 text when available, otherwise return its local managed path and metadata.",
  "Create an active project with an immutable key.",
  "Update a project's name or Markdown description using its current revision.",
  "Archive a project using its current revision. Confirm with the human before archival.",
  "Restore an archived project using its current revision.",
  "Create a ticket, optionally as a one-level subtask, in an active project.",
  "Update ticket fields and labels using its current revision.",
  "Move a ticket to a status using its current revision.",
  "Append a Markdown comment to a ticket.",
  "Copy an absolute local file into managed attachment storage for a ticket.",
  "Add a same-project blocker dependency using the blocked ticket's current revision.",
  "Remove a blocker dependency using the blocked ticket's current revision. Confirm with the human first.",
];
const id = "00000000-0000-4000-8000-000000000001";
const otherId = "00000000-0000-4000-8000-000000000002";
const attachmentId = "00000000-0000-4000-8000-000000000003";
const timestamp = "2026-07-17T12:00:00Z";
const project = {
  id,
  key: "AUTO",
  name: "Autoboard",
  description: "",
  state: "active",
  revision: 2,
  inserted_at: timestamp,
  updated_at: timestamp,
};
const summary = {
  id,
  identifier: "AUTO-1",
  project_id: id,
  title: "Build it",
  description: "",
  status: "ready",
  priority: "medium",
  assignee: "codex",
  revision: 2,
  parent_ticket_id: null,
  labels: [{ id: otherId, name: "feature", project_id: id }],
  blocked: false,
  comment_count: 1,
  attachment_count: 1,
  inserted_at: timestamp,
  updated_at: timestamp,
};
const attachment = {
  id: attachmentId,
  ticket_id: id,
  project_id: id,
  original_filename: "note.txt",
  media_type: "text/plain",
  byte_size: 5,
  sha256: "a".repeat(64),
  actor: "codex",
  inserted_at: timestamp,
};
const ticketDetail = {
  ...summary,
  project,
  parent: null,
  subtasks: [],
  blockers: [],
  blocked_tickets: [],
  comments: [
    {
      id,
      ticket_id: id,
      project_id: id,
      body: "note",
      actor: "codex",
      inserted_at: timestamp,
    },
  ],
  attachments: [attachment],
  activity: [
    {
      id: 1,
      event_type: "ticket.created",
      actor: "codex",
      project_id: id,
      ticket_id: id,
      payload: {},
      inserted_at: timestamp,
    },
  ],
};
const attachmentInline = { ...attachment, content: "hello" };
const attachmentWrite = { ...attachment, ticket_revision: 3 };
const comment = {
  id,
  ticket_id: id,
  project_id: id,
  body: "note",
  actor: "codex",
  inserted_at: timestamp,
  ticket_revision: 3,
};

const inputs = new Map<string, Record<string, unknown>>([
  ["list_projects", {}],
  ["get_project_board", { project_id: "AUTO" }],
  ["search_tickets", { query: "build", limit: 10 }],
  ["get_ticket", { ticket_id: "AUTO-1" }],
  ["list_actionable_tickets", {}],
  ["read_attachment", { attachment_id: attachmentId }],
  ["create_project", { key: "AUTO", name: "Autoboard" }],
  [
    "update_project",
    { project_id: "AUTO", expected_revision: 1, name: "Renamed" },
  ],
  ["archive_project", { project_id: "AUTO", expected_revision: 1 }],
  ["restore_project", { project_id: "AUTO", expected_revision: 1 }],
  ["create_ticket", { project_id: "AUTO", title: "Build it" }],
  [
    "update_ticket",
    { ticket_id: "AUTO-1", expected_revision: 1, title: "Build it better" },
  ],
  [
    "transition_ticket",
    { ticket_id: "AUTO-1", expected_revision: 1, status: "ready" },
  ],
  ["add_comment", { ticket_id: "AUTO-1", body: "note" }],
  ["add_attachment_from_path", { ticket_id: "AUTO-1", path: "/tmp/file.txt" }],
  [
    "add_dependency",
    {
      blocked_ticket_id: "AUTO-1",
      blocker_ticket_id: "AUTO-2",
      expected_revision: 1,
    },
  ],
  [
    "remove_dependency",
    {
      blocked_ticket_id: "AUTO-1",
      blocker_ticket_id: "AUTO-2",
      expected_revision: 1,
    },
  ],
]);
const rpcMethods = [
  "projects.list",
  "tickets.board",
  "tickets.search",
  "tickets.get",
  "tickets.actionable",
  "attachments.read",
  "projects.create",
  "projects.update",
  "projects.archive",
  "projects.restore",
  "tickets.create",
  "tickets.update",
  "tickets.transition",
  "comments.add",
  "attachments.add_from_path",
  "dependencies.add",
  "dependencies.remove",
];

const responseFor = (method: string): Record<string, unknown> => {
  switch (method) {
    case "projects.list":
      return { active: [project], archived: [] };
    case "tickets.board":
      return {
        project,
        columns: { backlog: [], ready: [summary], in_progress: [], done: [] },
      };
    case "tickets.search":
    case "tickets.actionable":
      return { tickets: [summary] };
    case "tickets.get":
      return ticketDetail;
    case "attachments.read":
      return attachmentInline;
    case "comments.add":
      return comment;
    case "attachments.add_from_path":
      return attachmentWrite;
    case "projects.create":
    case "projects.update":
    case "projects.archive":
    case "projects.restore":
      return project;
    default:
      return summary;
  }
};

class FakeClient implements RpcCaller {
  calls: Array<{
    method: string;
    params: Record<string, unknown>;
    mode: "read" | "write";
  }> = [];
  failure: Error | undefined;

  async call(
    method: string,
    params: Record<string, unknown>,
    _schema: unknown,
    mode: "read" | "write",
  ): Promise<unknown> {
    this.calls.push({ method, params, mode });
    if (this.failure) throw this.failure;
    return responseFor(method);
  }
}

const connected = async (rpc: FakeClient) => {
  const { server } = createMcpServer(rpc);
  const [clientTransport, serverTransport] =
    InMemoryTransport.createLinkedPair();
  await server.connect(serverTransport);
  const client = new Client({ name: "autoboard-test", version: "1.0.0" });
  await client.connect(clientTransport);
  return { client, server };
};

describe("Autoboard MCP tool registry", () => {
  test("publishes exact descriptions, strict JSON schemas, bounded inputs, and annotations for all 17 tools", async () => {
    expect(toolRegistry.map((tool) => tool.name)).toEqual(names);
    expect(toolRegistry.map((tool) => tool.description)).toEqual(descriptions);

    const { client } = await connected(new FakeClient());
    const listed = await client.listTools();
    expect(listed.tools.map((tool) => tool.name)).toEqual(names);
    expect(listed.tools.map((tool) => tool.description)).toEqual(descriptions);
    expect(client.getInstructions()).toBe(MCP_INSTRUCTIONS);
    for (const [index, tool] of toolRegistry.entries()) {
      const wire = listed.tools[index]!;
      expect(wire.inputSchema.additionalProperties).toBe(false);
      expect(wire.outputSchema?.additionalProperties).toBe(false);
      if (tool.name === "list_projects")
        expect(Object.keys(wire.inputSchema.properties ?? {})).toHaveLength(0);
      else
        expect(
          Object.keys(wire.inputSchema.properties ?? {}).length,
        ).toBeGreaterThan(0);
      expect(tool.inputSchema.safeParse(inputs.get(tool.name)).success).toBe(
        true,
      );
      expect(
        tool.outputSchema.safeParse(responseFor(tool.rpcMethod)).success,
      ).toBe(true);
      for (const forbidden of ["actor", "scope", "method", "sql", "delete"]) {
        expect(
          tool.inputSchema.safeParse({
            ...inputs.get(tool.name),
            [forbidden]: "forbidden",
          }).success,
        ).toBe(false);
      }
      expect(wire.annotations?.readOnlyHint).toBe(index < 6);
      expect(wire.annotations?.openWorldHint).toBe(false);
      expect(wire.annotations?.destructiveHint).toBe(
        ["archive_project", "remove_dependency"].includes(tool.name),
      );
    }
    const actionable = toolRegistry.find(
      (tool) => tool.name === "list_actionable_tickets",
    )!;
    expect(actionable.inputSchema.parse({}).limit).toBe(25);
    expect(actionable.inputSchema.safeParse({ limit: 0 }).success).toBe(false);
    expect(actionable.inputSchema.safeParse({ limit: 101 }).success).toBe(
      false,
    );
    expect(
      toolRegistry
        .find((tool) => tool.name === "search_tickets")!
        .inputSchema.parse({}).limit,
    ).toBe(25);
    for (const name of [
      "update_project",
      "archive_project",
      "restore_project",
      "update_ticket",
      "transition_ticket",
      "add_dependency",
      "remove_dependency",
    ]) {
      const input = inputs.get(name)!;
      const { expected_revision: _expectedRevision, ...withoutRevision } =
        input;
      expect(
        toolRegistry
          .find((tool) => tool.name === name)!
          .inputSchema.safeParse(withoutRevision).success,
      ).toBe(false);
    }
    await client.close();
  });

  test("runs every tool through the official MCP SDK with contract-valid structured output", async () => {
    const rpc = new FakeClient();
    const { client } = await connected(rpc);
    for (const name of names) {
      const result = await client.callTool({
        name,
        arguments: inputs.get(name),
      });
      expect(result.isError).not.toBe(true);
      expect(result.content).toEqual(
        expect.arrayContaining([expect.objectContaining({ type: "text" })]),
      );
      expect(result.structuredContent).toEqual(
        responseFor(rpcMethods[names.indexOf(name)]!),
      );
    }
    expect(rpc.calls.map((call) => call.method)).toEqual(rpcMethods);
    expect(rpc.calls.map((call) => call.mode)).toEqual(
      names.map((_, index) => (index < 6 ? "read" : "write")),
    );
    await client.close();
  });

  test("returns repairable domain, protocol, and indeterminate write errors over MCP", async () => {
    const rpc = new FakeClient();
    const { client } = await connected(rpc);
    rpc.failure = new RpcError(-32010, "stale", {
      kind: "revision_conflict",
      message: "stale",
      fields: { expected_revision: ["is stale"] },
      current: project,
    });
    await expect(
      client.callTool({
        name: "update_ticket",
        arguments: inputs.get("update_ticket"),
      }),
    ).resolves.toMatchObject({
      isError: true,
      content: [{ text: expect.stringContaining("expected_revision") }],
    });
    rpc.failure = new RpcProtocolError("bad response");
    await expect(
      client.callTool({
        name: "get_ticket",
        arguments: inputs.get("get_ticket"),
      }),
    ).resolves.toMatchObject({
      isError: true,
      content: [{ text: expect.stringContaining("protocol") }],
    });
    rpc.failure = new IndeterminateWriteError("tickets.update");
    await expect(
      client.callTool({
        name: "update_ticket",
        arguments: inputs.get("update_ticket"),
      }),
    ).resolves.toMatchObject({
      isError: true,
      content: [{ text: expect.stringContaining("Do not retry") }],
    });
    await client.close();
  });
});
