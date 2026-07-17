import { Schema } from "effect"
import { Project, TicketSummary } from "@autoboard/contracts"
import { z } from "zod"
import { boundedReference, boundedRevision, boundedText, projectOutputSchema, ticketOutputSchema, type ToolSpec } from "./read.js"

const write = (destructiveHint = false) => ({ readOnlyHint: false, destructiveHint, openWorldHint: false } as const)
const optionalDescription = z.string().max(100_000)
const labels = z.array(z.string().trim().min(1).max(100)).max(100)
const assignee = z.enum(["unassigned", "me", "codex"])
const priority = z.enum(["none", "low", "medium", "high", "urgent"])
const status = z.enum(["triage", "backlog", "ready", "in_progress", "done", "canceled"])
const commentOutput = z.object({
  id: z.string(), ticket_id: z.string(), project_id: z.string(), body: z.string(), actor: z.enum(["me", "codex", "system"]), inserted_at: z.string(), ticket_revision: boundedRevision,
}).strict()
const attachmentWriteOutput = z.object({
  id: z.string(), ticket_id: z.string(), project_id: z.string(), original_filename: z.string(), media_type: z.string(),
  byte_size: z.number().int().nonnegative(), sha256: z.string(), actor: z.enum(["me", "codex", "system"]), inserted_at: z.string(), ticket_revision: boundedRevision,
}).strict()
const commentResult = Schema.Struct({ id: Schema.String, ticket_id: Schema.String, project_id: Schema.String, body: Schema.String, actor: Schema.Literal("me", "codex", "system"), inserted_at: Schema.String, ticket_revision: Schema.Number })
const attachmentResult = Schema.Struct({
  id: Schema.String, ticket_id: Schema.String, project_id: Schema.String, original_filename: Schema.String,
  media_type: Schema.String, byte_size: Schema.Number, sha256: Schema.String,
  actor: Schema.Literal("me", "codex", "system"), inserted_at: Schema.String, ticket_revision: Schema.Number,
})

export const writeTools: ToolSpec[] = [
  {
    name: "create_project", description: "Create an active project with an immutable key.",
    inputSchema: z.object({ key: z.string().trim().regex(/^[A-Za-z][A-Za-z0-9]{1,7}$/), name: boundedText.max(200), description: optionalDescription.default("") }).strict(),
    outputSchema: projectOutputSchema, annotations: write(), rpcMethod: "projects.create", mode: "write", resultSchema: Project,
  },
  {
    name: "update_project", description: "Update a project's name or Markdown description using its current revision.",
    inputSchema: z.object({ project_id: boundedReference, expected_revision: boundedRevision, name: boundedText.max(200).optional(), description: optionalDescription.optional() }).strict().refine((value) => value.name !== undefined || value.description !== undefined, "name or description is required"),
    outputSchema: projectOutputSchema, annotations: write(), rpcMethod: "projects.update", mode: "write", resultSchema: Project,
  },
  {
    name: "archive_project", description: "Archive a project using its current revision. Confirm with the human before archival.",
    inputSchema: z.object({ project_id: boundedReference, expected_revision: boundedRevision }).strict(), outputSchema: projectOutputSchema,
    annotations: write(true), rpcMethod: "projects.archive", mode: "write", resultSchema: Project,
  },
  {
    name: "restore_project", description: "Restore an archived project using its current revision.",
    inputSchema: z.object({ project_id: boundedReference, expected_revision: boundedRevision }).strict(), outputSchema: projectOutputSchema,
    annotations: write(), rpcMethod: "projects.restore", mode: "write", resultSchema: Project,
  },
  {
    name: "create_ticket", description: "Create a ticket, optionally as a one-level subtask, in an active project.",
    inputSchema: z.object({ project_id: boundedReference, title: boundedText.max(500), description: optionalDescription.default(""), status: status.optional(), priority: priority.optional(), assignee: assignee.optional(), parent_ticket_id: boundedReference.nullable().optional(), labels: labels.optional() }).strict(),
    outputSchema: ticketOutputSchema, annotations: write(), rpcMethod: "tickets.create", mode: "write", resultSchema: TicketSummary,
  },
  {
    name: "update_ticket", description: "Update ticket fields and labels using its current revision.",
    inputSchema: z.object({ ticket_id: boundedReference, expected_revision: boundedRevision, title: boundedText.max(500).optional(), description: optionalDescription.optional(), priority: priority.optional(), assignee: assignee.optional(), labels: labels.optional() }).strict().refine((value) => value.title !== undefined || value.description !== undefined || value.priority !== undefined || value.assignee !== undefined || value.labels !== undefined, "at least one ticket field is required"),
    outputSchema: ticketOutputSchema, annotations: write(), rpcMethod: "tickets.update", mode: "write", resultSchema: TicketSummary,
  },
  {
    name: "transition_ticket", description: "Move a ticket to a status using its current revision.",
    inputSchema: z.object({ ticket_id: boundedReference, expected_revision: boundedRevision, status }).strict(), outputSchema: ticketOutputSchema,
    annotations: write(), rpcMethod: "tickets.transition", mode: "write", resultSchema: TicketSummary,
  },
  {
    name: "add_comment", description: "Append a Markdown comment to a ticket.",
    inputSchema: z.object({ ticket_id: boundedReference, body: boundedText.max(100_000) }).strict(), outputSchema: commentOutput,
    annotations: write(), rpcMethod: "comments.add", mode: "write", resultSchema: commentResult,
  },
  {
    name: "add_attachment_from_path", description: "Copy an absolute local file into managed attachment storage for a ticket.",
    inputSchema: z.object({ ticket_id: boundedReference, path: z.string().startsWith("/").max(4_096) }).strict(), outputSchema: attachmentWriteOutput,
    annotations: write(), rpcMethod: "attachments.add_from_path", mode: "write", resultSchema: attachmentResult,
  },
  {
    name: "add_dependency", description: "Add a same-project blocker dependency using the blocked ticket's current revision.",
    inputSchema: z.object({ blocked_ticket_id: boundedReference, blocker_ticket_id: boundedReference, expected_revision: boundedRevision }).strict(), outputSchema: ticketOutputSchema,
    annotations: write(), rpcMethod: "dependencies.add", mode: "write", resultSchema: TicketSummary,
  },
  {
    name: "remove_dependency", description: "Remove a blocker dependency using the blocked ticket's current revision. Confirm with the human first.",
    inputSchema: z.object({ blocked_ticket_id: boundedReference, blocker_ticket_id: boundedReference, expected_revision: boundedRevision }).strict(), outputSchema: ticketOutputSchema,
    annotations: write(true), rpcMethod: "dependencies.remove", mode: "write", resultSchema: TicketSummary,
  },
]
