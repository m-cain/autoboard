import { describe, expect, test } from "vitest"
import { z } from "zod"
import { Client } from "@modelcontextprotocol/sdk/client/index.js"
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js"
import { IndeterminateWriteError, RpcError, RpcProtocolError } from "../src/rpc-error.js"
import { createMcpServer, MCP_INSTRUCTIONS, toolRegistry, type RpcCaller } from "../src/server.js"

const names = [
  "list_projects", "get_project_board", "search_tickets", "get_ticket", "list_actionable_tickets", "read_attachment",
  "create_project", "update_project", "archive_project", "restore_project", "create_ticket", "update_ticket",
  "transition_ticket", "add_comment", "add_attachment_from_path", "add_dependency", "remove_dependency",
]

class FakeClient implements RpcCaller {
  calls: Array<{ method: string; params: Record<string, unknown>; mode: "read" | "write" }> = []
  response: unknown = { ok: true }

  async call(method: string, params: Record<string, unknown>, _schema: unknown, mode: "read" | "write"): Promise<unknown> {
    this.calls.push({ method, params, mode })
    if (this.response instanceof Error) throw this.response
    return this.response
  }
}

describe("Autoboard MCP tool registry", () => {
  test("exposes exactly the planned tool surface with strict bounded inputs and accurate annotations", () => {
    expect(toolRegistry.map((tool) => tool.name)).toEqual(names)
    expect(new Set(toolRegistry.map((tool) => tool.description)).size).toBe(17)

    for (const tool of toolRegistry) {
      expect(tool.inputSchema).toBeInstanceOf(z.ZodType)
      expect(tool.outputSchema).toBeInstanceOf(z.ZodType)
      expect(tool.annotations.openWorldHint).toBe(false)
      expect(tool.annotations.readOnlyHint).toBe(names.indexOf(tool.name) < 6)
    }

    expect(toolRegistry.find((tool) => tool.name === "archive_project")?.annotations.destructiveHint).toBe(true)
    expect(toolRegistry.find((tool) => tool.name === "remove_dependency")?.annotations.destructiveHint).toBe(true)
    expect(toolRegistry.filter((tool) => !["archive_project", "remove_dependency"].includes(tool.name))
      .every((tool) => tool.annotations.destructiveHint === false)).toBe(true)

    const actionable = toolRegistry.find((tool) => tool.name === "list_actionable_tickets")!
    expect(actionable.inputSchema.safeParse({}).success).toBe(true)
    expect(actionable.inputSchema.parse({}).limit).toBe(25)
    expect(actionable.inputSchema.safeParse({ limit: 101 }).success).toBe(false)
    expect(actionable.inputSchema.safeParse({ actor: "me" }).success).toBe(false)

    for (const name of ["update_project", "archive_project", "restore_project", "update_ticket", "transition_ticket", "add_dependency", "remove_dependency"]) {
      const tool = toolRegistry.find((entry) => entry.name === name)!
      expect(tool.inputSchema.safeParse({}).success).toBe(false)
    }
  })

  test("is interoperable over MCP and publishes the required instructions and schemas", async () => {
    const rpc = new FakeClient()
    rpc.response = { active: [], archived: [] }
    const { server } = createMcpServer(rpc)
    const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair()
    await server.connect(serverTransport)
    const client = new Client({ name: "autoboard-test", version: "1.0.0" })
    await client.connect(clientTransport)

    const listed = await client.listTools()
    expect(listed.tools.map((tool) => tool.name)).toEqual(names)
    expect(client.getInstructions()).toBe(MCP_INSTRUCTIONS)
    for (const tool of listed.tools) {
      expect(tool.inputSchema.additionalProperties).toBe(false)
      expect(tool.outputSchema).toBeDefined()
      expect(tool.annotations?.openWorldHint).toBe(false)
    }
    await expect(client.callTool({ name: "list_projects", arguments: {} })).resolves.toMatchObject({
      content: [{ type: "text" }], structuredContent: { active: [], archived: [] },
    })
    await client.close()
  })

  test("maps every registered tool to its private RPC method and returns structured content", async () => {
    const client = new FakeClient()
    const { handlers } = createMcpServer(client)
    const input = new Map<string, Record<string, unknown>>([
      ["list_projects", {}], ["get_project_board", { project_id: "AUTO" }], ["search_tickets", { query: "find" }],
      ["get_ticket", { ticket_id: "AUTO-1" }], ["list_actionable_tickets", {}], ["read_attachment", { attachment_id: "00000000-0000-4000-8000-000000000000" }],
      ["create_project", { key: "AUTO", name: "Autoboard" }], ["update_project", { project_id: "AUTO", expected_revision: 1, name: "Renamed" }],
      ["archive_project", { project_id: "AUTO", expected_revision: 1 }], ["restore_project", { project_id: "AUTO", expected_revision: 1 }],
      ["create_ticket", { project_id: "AUTO", title: "Build it" }], ["update_ticket", { ticket_id: "AUTO-1", expected_revision: 1, title: "Build it better" }],
      ["transition_ticket", { ticket_id: "AUTO-1", expected_revision: 1, status: "ready" }], ["add_comment", { ticket_id: "AUTO-1", body: "note" }],
      ["add_attachment_from_path", { ticket_id: "AUTO-1", path: "/tmp/file.txt" }], ["add_dependency", { blocked_ticket_id: "AUTO-1", blocker_ticket_id: "AUTO-2", expected_revision: 1 }],
      ["remove_dependency", { blocked_ticket_id: "AUTO-1", blocker_ticket_id: "AUTO-2", expected_revision: 1 }],
    ])
    const rpcMethods = [
      "projects.list", "tickets.board", "tickets.search", "tickets.get", "tickets.actionable", "attachments.read",
      "projects.create", "projects.update", "projects.archive", "projects.restore", "tickets.create", "tickets.update",
      "tickets.transition", "comments.add", "attachments.add_from_path", "dependencies.add", "dependencies.remove",
    ]

    for (const [index, tool] of toolRegistry.entries()) {
      const result = await handlers.get(tool.name)!(input.get(tool.name)!)
      expect(result).toMatchObject({ content: [{ type: "text" }], structuredContent: { ok: true } })
      expect(client.calls[index]).toMatchObject({ method: rpcMethods[index], mode: index < 6 ? "read" : "write" })
    }
  })

  test("turns domain, protocol, and indeterminate write failures into safe repairable tool errors", async () => {
    const client = new FakeClient()
    const { handlers } = createMcpServer(client)

    client.response = new RpcError(-32010, "stale", {
      kind: "revision_conflict", message: "stale", fields: { expected_revision: ["is stale"] }, current: { id: "now" },
    })
    await expect(handlers.get("update_ticket")!({ ticket_id: "AUTO-1", expected_revision: 1, title: "new" }))
      .resolves.toMatchObject({ isError: true, content: [{ text: expect.stringContaining("expected_revision") }] })

    client.response = new RpcProtocolError("bad response")
    await expect(handlers.get("get_ticket")!({ ticket_id: "AUTO-1" }))
      .resolves.toMatchObject({ isError: true, content: [{ text: expect.stringContaining("protocol") }] })

    client.response = new IndeterminateWriteError("tickets.update")
    await expect(handlers.get("update_ticket")!({ ticket_id: "AUTO-1", expected_revision: 1, title: "new" }))
      .resolves.toMatchObject({ isError: true, content: [{ text: expect.stringContaining("Do not retry") }] })
  })
})
