import { Schema } from "effect"
import { Project, ProjectBoard, TicketDetail, TicketSummary } from "@autoboard/contracts"
import { z } from "zod"
import { attachmentSuccess, success, toolError, type ToolResult } from "../tool-result.js"

export type RpcMode = "read" | "write"
export type RpcCaller = {
  call(method: string, params: Record<string, unknown>, schema: Schema.Schema.Any, mode: RpcMode): Promise<unknown>
}

export type ToolSpec = {
  name: string
  description: string
  inputSchema: z.ZodObject<z.ZodRawShape>
  outputSchema: z.ZodObject<z.ZodRawShape>
  annotations: { readOnlyHint: boolean; destructiveHint: boolean; openWorldHint: false }
  rpcMethod: string
  mode: RpcMode
  resultSchema: Schema.Schema.Any
  resultText?: (value: Record<string, unknown>) => ToolResult
}

const ref = z.string().trim().min(1).max(128)
const limit = z.number().int().min(1).max(100)
const nonEmpty = z.string().trim().min(1)
const revision = z.number().int().positive()
const readAnnotations = { readOnlyHint: true, destructiveHint: false, openWorldHint: false } as const

const projectOutput = z.object({
  id: z.string(), key: z.string(), name: z.string(), description: z.string(), state: z.enum(["active", "archived"]),
  revision, inserted_at: z.string(), updated_at: z.string(),
}).strict()
const labelOutput = z.object({ id: z.string(), name: z.string(), project_id: z.string() }).strict()
const ticketSummaryOutput = z.object({
  id: z.string(), identifier: z.string(), project_id: z.string(), title: z.string(), description: z.string(),
  status: z.enum(["triage", "backlog", "ready", "in_progress", "done", "canceled"]),
  priority: z.enum(["none", "low", "medium", "high", "urgent"]), assignee: z.enum(["unassigned", "me", "codex"]),
  revision, parent_ticket_id: z.string().nullable(), labels: z.array(labelOutput), blocked: z.boolean(),
  comment_count: z.number().int().nonnegative(), attachment_count: z.number().int().nonnegative(), inserted_at: z.string(), updated_at: z.string(),
}).strict()
const boardOutput = z.object({
  project: projectOutput,
  columns: z.object({ backlog: z.array(ticketSummaryOutput), ready: z.array(ticketSummaryOutput), in_progress: z.array(ticketSummaryOutput), done: z.array(ticketSummaryOutput) }).strict(),
}).strict()
const attachmentOutput = z.object({
  id: z.string(), ticket_id: z.string(), project_id: z.string(), original_filename: z.string(), media_type: z.string(),
  byte_size: z.number().int().nonnegative(), sha256: z.string(), actor: z.enum(["me", "codex", "system"]), inserted_at: z.string(),
  managed_path: z.string().optional(), content: z.string().optional(),
}).strict().refine((value) => (value.content === undefined) !== (value.managed_path === undefined), "attachment has either inline content or managed path")
const attachmentDetailOutput = z.object({
  id: z.string(), ticket_id: z.string(), project_id: z.string(), original_filename: z.string(), media_type: z.string(),
  byte_size: z.number().int().nonnegative(), sha256: z.string(), actor: z.enum(["me", "codex", "system"]), inserted_at: z.string(),
}).strict()
const commentOutput = z.object({
  id: z.string(), ticket_id: z.string(), project_id: z.string(), body: z.string(), actor: z.enum(["me", "codex", "system"]), inserted_at: z.string(),
}).strict()
const activityOutput = z.object({
  id: z.number().int().positive(), event_type: z.string(), actor: z.enum(["me", "codex", "system"]), project_id: z.string(), ticket_id: z.string().nullable(), payload: z.record(z.string(), z.unknown()), inserted_at: z.string(),
}).strict()
const ticketDetailOutput = ticketSummaryOutput.extend({
  project: projectOutput, parent: ticketSummaryOutput.nullable(), subtasks: z.array(ticketSummaryOutput), blockers: z.array(ticketSummaryOutput), blocked_tickets: z.array(ticketSummaryOutput),
  comments: z.array(commentOutput), attachments: z.array(attachmentDetailOutput), activity: z.array(activityOutput),
}).strict()

const listProjects = Schema.Struct({ active: Schema.Array(Project), archived: Schema.Array(Project) })
const ticketList = Schema.Struct({ tickets: Schema.Array(TicketSummary) })
const attachmentRpcResult = Schema.Struct({
  id: Schema.String, ticket_id: Schema.String, project_id: Schema.String, original_filename: Schema.String,
  media_type: Schema.String, byte_size: Schema.Number, sha256: Schema.String,
  actor: Schema.Literal("me", "codex", "system"), inserted_at: Schema.String, managed_path: Schema.String,
})
const attachmentRead = Schema.Union(
  Schema.Struct({
    id: Schema.String, ticket_id: Schema.String, project_id: Schema.String, original_filename: Schema.String,
    media_type: Schema.String, byte_size: Schema.Number, sha256: Schema.String,
    actor: Schema.Literal("me", "codex", "system"), inserted_at: Schema.String, content: Schema.String,
  }),
  attachmentRpcResult,
)

export const readTools: ToolSpec[] = [
  {
    name: "list_projects", description: "List active and archived Autoboard projects.", inputSchema: z.object({}).strict(),
    outputSchema: z.object({ active: z.array(projectOutput), archived: z.array(projectOutput) }).strict(), annotations: readAnnotations,
    rpcMethod: "projects.list", mode: "read", resultSchema: listProjects,
  },
  {
    name: "get_project_board", description: "Read a project's Kanban board and project metadata.", inputSchema: z.object({ project_id: ref }).strict(),
    outputSchema: boardOutput, annotations: readAnnotations, rpcMethod: "tickets.board", mode: "read", resultSchema: ProjectBoard,
  },
  {
    name: "search_tickets", description: "Search tickets globally or within one project.", inputSchema: z.object({ query: z.string().max(500).default(""), project_id: ref.optional(), limit: limit.default(25) }).strict(),
    outputSchema: z.object({ tickets: z.array(ticketSummaryOutput) }).strict(), annotations: readAnnotations, rpcMethod: "tickets.search", mode: "read", resultSchema: ticketList,
  },
  {
    name: "get_ticket", description: "Read complete ticket detail, including relationships, comments, attachments, and activity.", inputSchema: z.object({ ticket_id: ref }).strict(),
    outputSchema: ticketDetailOutput, annotations: readAnnotations, rpcMethod: "tickets.get", mode: "read", resultSchema: TicketDetail,
  },
  {
    name: "list_actionable_tickets", description: "List ready, unblocked tickets assigned to codex; tickets assigned to `me` and tickets with non-terminal subtasks are excluded.", inputSchema: z.object({ project_id: ref.optional(), limit: limit.default(25) }).strict(),
    outputSchema: z.object({ tickets: z.array(ticketSummaryOutput) }).strict(), annotations: readAnnotations, rpcMethod: "tickets.actionable", mode: "read", resultSchema: ticketList,
  },
  {
    name: "read_attachment", description: "Read a managed attachment's inline UTF-8 text when available, otherwise return its local managed path and metadata.", inputSchema: z.object({ attachment_id: z.string().uuid() }).strict(),
    outputSchema: attachmentOutput, annotations: readAnnotations, rpcMethod: "attachments.read", mode: "read", resultSchema: attachmentRead, resultText: attachmentSuccess,
  },
]

export const boundedText = nonEmpty
export const boundedRevision = revision
export const boundedReference = ref
export const ticketOutputSchema = ticketSummaryOutput
export const projectOutputSchema = projectOutput
export const attachmentOutputSchema = attachmentOutput

export const runTool = async (client: RpcCaller, spec: ToolSpec, params: Record<string, unknown>): Promise<ToolResult> => {
  try {
    const value = await client.call(spec.rpcMethod, params, spec.resultSchema, spec.mode)
    const result = value as Record<string, unknown>
    return spec.resultText?.(result) ?? success(result)
  } catch (error) {
    return toolError(error)
  }
}
