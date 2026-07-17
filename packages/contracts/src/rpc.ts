import { Schema } from "effect"
import { ProjectJsonSchema, TicketDetailJsonSchema, TicketSummaryJsonSchema } from "./domain.js"

export const DomainErrorKind = Schema.Literal(
  "unauthorized",
  "not_found",
  "validation_failed",
  "revision_conflict",
  "invalid_transition",
  "blocked_by_dependency",
  "dependency_cycle",
  "attachment_failed",
  "internal_error",
)

const ErrorData = Schema.Struct({
  kind: DomainErrorKind,
  fields: Schema.optional(Schema.Record({ key: Schema.String, value: Schema.Array(Schema.String) })),
  current: Schema.optional(Schema.Union(ProjectJsonSchema, TicketSummaryJsonSchema, TicketDetailJsonSchema)),
  correlation_id: Schema.optional(Schema.String),
})

export const RpcSuccess = Schema.Struct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Union(Schema.String, Schema.Number),
  result: Schema.Unknown,
})

export const RpcFailure = Schema.Struct({
  kind: DomainErrorKind,
  message: Schema.String,
  fields: Schema.optional(Schema.Record({ key: Schema.String, value: Schema.Array(Schema.String) })),
  current: Schema.optional(Schema.Union(ProjectJsonSchema, TicketSummaryJsonSchema, TicketDetailJsonSchema)),
  correlation_id: Schema.optional(Schema.String),
})

export const RpcEnvelopeFailure = Schema.Struct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Union(Schema.String, Schema.Number, Schema.Null),
  error: Schema.Struct({
    code: Schema.Number,
    message: Schema.String,
    data: ErrorData,
  }),
})

export type RpcSuccess = Schema.Schema.Encoded<typeof RpcSuccess>
export type RpcFailure = Schema.Schema.Encoded<typeof RpcFailure>
