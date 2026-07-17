import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js"
import { RpcClient } from "./rpc-client.js"
import { createMcpServer } from "./server.js"

const socketPath = process.env.AUTOBOARD_SOCKET
const token = process.env.AUTOBOARD_TOKEN

if (!socketPath || !token) {
  console.error("AUTOBOARD_SOCKET and AUTOBOARD_TOKEN are required")
  process.exitCode = 1
} else {
  const client = await RpcClient.connect({ socketPath, token })
  const { server } = createMcpServer(client)
  const transport = new StdioServerTransport()
  await server.connect(transport)
  console.error("Autoboard MCP server connected")

  let closing = false
  const close = async () => {
    if (closing) return
    closing = true
    try {
      await server.close()
      await client.close()
    } catch (error) {
      console.error(`Autoboard MCP shutdown failed: ${error instanceof Error ? error.message : String(error)}`)
    }
  }
  const shutdown = async () => {
    await close()
    process.exit(0)
  }
  process.once("SIGINT", () => void shutdown())
  process.once("SIGTERM", () => void shutdown())
}
