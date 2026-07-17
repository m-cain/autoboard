import { Schema } from "effect"
import { pipe } from "effect/Function"

type Fields = Parameters<typeof Schema.Struct>[0]

/**
 * Effect's Struct parser intentionally drops unrecognized keys. Transport
 * boundaries must instead reject them, so validate the raw object before the
 * Struct decoder sees it. The accompanying `*JsonSchema` exports keep the
 * structurally equivalent form available to the JSON Schema generator.
 */
const strictStruct = <F extends Fields>(fields: F) =>
  Schema.compose(
    Schema.filter((value) =>
      typeof value === "object" &&
      value !== null &&
      !Array.isArray(value) &&
      Object.keys(value).every((key) => Object.hasOwn(fields, key)),
    )(Schema.Unknown),
    Schema.Struct(fields),
  )

export const UUID = pipe(Schema.UUID, Schema.brand("UUID"))
export const Revision = pipe(Schema.Number, Schema.int(), Schema.positive())
export const Timestamp = pipe(Schema.String, Schema.pattern(/^\d{4}-\d{2}-\d{2}T.*Z$/))
export const Sha256 = pipe(Schema.String, Schema.pattern(/^[a-f0-9]{64}$/))

export const ProjectState = Schema.Literal("active", "archived")
export const TicketStatus = Schema.Literal("triage", "backlog", "ready", "in_progress", "done", "canceled")
export const Priority = Schema.Literal("none", "low", "medium", "high", "urgent")
export const Assignee = Schema.Literal("unassigned", "me", "codex")
export const Actor = Schema.Literal("me", "codex", "system")

const ProjectFields = {
  id: UUID,
  key: Schema.NonEmptyString,
  name: Schema.NonEmptyString,
  description: Schema.String,
  state: ProjectState,
  revision: Revision,
  inserted_at: Timestamp,
  updated_at: Timestamp,
} as const

export const ProjectJsonSchema = Schema.Struct(ProjectFields)
export const Project = strictStruct(ProjectFields)

const LabelFields = {
  id: UUID,
  name: Schema.NonEmptyString,
  project_id: UUID,
} as const

export const LabelJsonSchema = Schema.Struct(LabelFields)
export const Label = strictStruct(LabelFields)

const TicketSummaryFields = {
  id: UUID,
  identifier: Schema.NonEmptyString,
  project_id: UUID,
  title: Schema.NonEmptyString,
  description: Schema.String,
  status: TicketStatus,
  priority: Priority,
  assignee: Assignee,
  revision: Revision,
  parent_ticket_id: Schema.NullOr(UUID),
  labels: Schema.Array(Label),
  blocked: Schema.Boolean,
  comment_count: pipe(Schema.Number, Schema.int(), Schema.nonNegative()),
  attachment_count: pipe(Schema.Number, Schema.int(), Schema.nonNegative()),
  inserted_at: Timestamp,
  updated_at: Timestamp,
} as const

export const TicketSummaryJsonSchema = Schema.Struct({
  ...TicketSummaryFields,
  labels: Schema.Array(LabelJsonSchema),
})
export const TicketSummary = strictStruct(TicketSummaryFields)

const AttachmentFields = {
  id: UUID,
  ticket_id: UUID,
  project_id: UUID,
  original_filename: Schema.NonEmptyString,
  media_type: Schema.NonEmptyString,
  byte_size: pipe(Schema.Number, Schema.int(), Schema.nonNegative()),
  sha256: Sha256,
  actor: Actor,
  inserted_at: Timestamp,
} as const

export const AttachmentJsonSchema = Schema.Struct(AttachmentFields)
export const Attachment = strictStruct(AttachmentFields)

const AttachmentRpcFields = {
  ...AttachmentFields,
  managed_path: Schema.NonEmptyString,
} as const

export const AttachmentRpcJsonSchema = Schema.Struct(AttachmentRpcFields)
export const AttachmentRpc = strictStruct(AttachmentRpcFields)

const CommentFields = {
  id: UUID,
  ticket_id: UUID,
  project_id: UUID,
  body: Schema.String,
  actor: Actor,
  inserted_at: Timestamp,
} as const

export const CommentJsonSchema = Schema.Struct(CommentFields)
export const Comment = strictStruct(CommentFields)

const ActivityEventFields = {
  id: pipe(Schema.Number, Schema.int(), Schema.positive()),
  event_type: Schema.NonEmptyString,
  actor: Actor,
  project_id: UUID,
  ticket_id: Schema.NullOr(UUID),
  payload: Schema.Record({ key: Schema.String, value: Schema.Unknown }),
  inserted_at: Timestamp,
} as const

export const ActivityEventJsonSchema = Schema.Struct(ActivityEventFields)
export const ActivityEvent = strictStruct(ActivityEventFields)

const TicketDetailFields = {
  ...TicketSummaryFields,
  project: Project,
  parent: Schema.NullOr(TicketSummary),
  subtasks: Schema.Array(TicketSummary),
  blockers: Schema.Array(TicketSummary),
  blocked_tickets: Schema.Array(TicketSummary),
  comments: Schema.Array(Comment),
  attachments: Schema.Array(Attachment),
  activity: Schema.Array(ActivityEvent),
} as const

export const TicketDetailJsonSchema = Schema.Struct({
  ...TicketSummaryFields,
  labels: Schema.Array(LabelJsonSchema),
  project: ProjectJsonSchema,
  parent: Schema.NullOr(TicketSummaryJsonSchema),
  subtasks: Schema.Array(TicketSummaryJsonSchema),
  blockers: Schema.Array(TicketSummaryJsonSchema),
  blocked_tickets: Schema.Array(TicketSummaryJsonSchema),
  comments: Schema.Array(CommentJsonSchema),
  attachments: Schema.Array(AttachmentJsonSchema),
  activity: Schema.Array(ActivityEventJsonSchema),
})
export const TicketDetail = strictStruct(TicketDetailFields)

const ProjectBoardFields = {
  project: Project,
  columns: strictStruct({
    backlog: Schema.Array(TicketSummary),
    ready: Schema.Array(TicketSummary),
    in_progress: Schema.Array(TicketSummary),
    done: Schema.Array(TicketSummary),
  }),
} as const

export const ProjectBoardJsonSchema = Schema.Struct({
  project: ProjectJsonSchema,
  columns: Schema.Struct({
    backlog: Schema.Array(TicketSummaryJsonSchema),
    ready: Schema.Array(TicketSummaryJsonSchema),
    in_progress: Schema.Array(TicketSummaryJsonSchema),
    done: Schema.Array(TicketSummaryJsonSchema),
  }),
})
export const ProjectBoard = strictStruct(ProjectBoardFields)

export type Project = Schema.Schema.Encoded<typeof Project>
export type TicketSummary = Schema.Schema.Encoded<typeof TicketSummary>
export type TicketDetail = Schema.Schema.Encoded<typeof TicketDetail>
export type ProjectBoard = Schema.Schema.Encoded<typeof ProjectBoard>
export type ActivityEvent = Schema.Schema.Encoded<typeof ActivityEvent>
export type Attachment = Schema.Schema.Encoded<typeof Attachment>
export type AttachmentRpc = Schema.Schema.Encoded<typeof AttachmentRpc>
