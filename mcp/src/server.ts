import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js"
import { readTools, runTool, type RpcCaller, type ToolSpec } from "./tools/read.js"
import { writeTools } from "./tools/write.js"
import type { ToolResult } from "./tool-result.js"

export const MCP_INSTRUCTIONS = "Autoboard is a direct-write project board. Tickets assigned to `me` are reserved for the human. Execute only tickets returned by list_actionable_tickets unless the human explicitly instructs otherwise. Read the latest entity before revision-checked writes. Confirm broad reorganizations, project archival, and dependency removal with the human."

export { type RpcCaller }

export const toolRegistry: ToolSpec[] = [...readTools, ...writeTools]

export const createMcpServer = (client: RpcCaller): {
  server: McpServer
  handlers: Map<string, (params: Record<string, unknown>) => Promise<ToolResult>>
} => {
  const server = new McpServer({ name: "autoboard", version: "0.1.0" }, { instructions: MCP_INSTRUCTIONS })
  const handlers = new Map<string, (params: Record<string, unknown>) => Promise<ToolResult>>()

  for (const spec of toolRegistry) {
    const handler = (params: Record<string, unknown>) => runTool(client, spec, params)
    handlers.set(spec.name, handler)
    server.registerTool(spec.name, {
      description: spec.description,
      inputSchema: spec.inputSchema,
      outputSchema: spec.outputSchema,
      annotations: spec.annotations,
    }, handler)
  }

  return { server, handlers }
}
