import { IndeterminateWriteError, RpcConnectionError, RpcError, RpcProtocolError } from "./rpc-error.js"

export type ToolResult = {
  content: Array<{ type: "text"; text: string }>
  structuredContent?: Record<string, unknown>
  isError?: true
}

const repairHint = (kind: string): string => {
  switch (kind) {
    case "revision_conflict": return "Read the latest entity, use its current revision, then retry the intended change."
    case "validation_failed": return "Correct the listed fields and retry with valid values."
    case "not_found": return "Read the project or ticket first and use its current ID or visible identifier."
    case "invalid_transition": return "Read the ticket's current state and choose a valid next status."
    case "blocked_by_dependency": return "Resolve or cancel the blocking tickets before changing this ticket."
    case "dependency_cycle": return "Choose a dependency direction that does not create a cycle."
    case "attachment_failed": return "Check that the source path is absolute, readable, and within the attachment size limit."
    case "unauthorized": return "Use an Autoboard credential with the required access and reconnect the MCP server."
    default: return "Read the current state before deciding how to proceed."
  }
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value)

const errorText = (kind: string, message: string, fields: unknown, current: unknown): string => {
  const lines = [`Autoboard ${kind}: ${message}`, `Repair: ${repairHint(kind)}`]
  if (isRecord(fields) && Object.keys(fields).length > 0) {
    lines.push(`Fields: ${JSON.stringify(fields)}`)
  }
  if (current !== undefined) lines.push(`Current entity: ${JSON.stringify(current)}`)
  return lines.join("\n")
}

export const success = (value: Record<string, unknown>, text?: string): ToolResult => ({
  content: [{ type: "text", text: text ?? JSON.stringify(value, null, 2) }],
  structuredContent: value,
})

export const attachmentSuccess = (value: Record<string, unknown>): ToolResult => {
  const filename = typeof value.original_filename === "string" ? value.original_filename : "attachment"
  const content = value.content
  const text = typeof content === "string"
    ? `Attachment ${filename} (inline UTF-8 content):\n\n${content}`
    : `Attachment ${filename} is not returned inline. Inspect the managed local path: ${String(value.managed_path ?? "unavailable")}.`
  return success(value, text)
}

export const toolError = (error: unknown): ToolResult => {
  if (error instanceof IndeterminateWriteError) {
    return {
      isError: true,
      content: [{ type: "text", text: `${error.message}\nRepair: Do not retry this write automatically. Read the latest entity to determine whether it was applied.` }],
    }
  }

  if (error instanceof RpcError && isRecord(error.data)) {
    const kind = typeof error.data.kind === "string" ? error.data.kind : "rpc_error"
    const message = typeof error.data.message === "string" ? error.data.message : error.message
    return { isError: true, content: [{ type: "text", text: errorText(kind, message, error.data.fields, error.data.current) }] }
  }

  if (error instanceof RpcProtocolError) {
    return { isError: true, content: [{ type: "text", text: `Autoboard RPC protocol error: ${error.message}\nRepair: Retry a read after reconnecting; do not blindly repeat a write.` }] }
  }

  if (error instanceof RpcConnectionError) {
    return { isError: true, content: [{ type: "text", text: `Autoboard connection error: ${error.message}\nRepair: Check that the local server is running and retry a read before any write.` }] }
  }

  return { isError: true, content: [{ type: "text", text: "Autoboard tool failed unexpectedly. Repair: read the current state and retry only if the outcome is known." }] }
}
