import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js"
import { RpcClient } from "./rpc-client.js"
import { createCloseOnce, createSignalShutdown } from "./lifecycle.js"
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

  const close = createCloseOnce(server, client)
  const shutdown = createSignalShutdown(close, (code) => process.exit(code), (message) => console.error(message))
  process.once("SIGINT", () => void shutdown())
  process.once("SIGTERM", () => void shutdown())
}
