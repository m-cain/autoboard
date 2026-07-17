import { Schema } from "effect"
import {
  exactStruct,
  Project,
  ProjectJsonSchema,
  TicketDetail,
  TicketDetailJsonSchema,
  TicketSummary,
  TicketSummaryJsonSchema,
} from "./domain.js"

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

const ErrorFields = Schema.Record({ key: Schema.String, value: Schema.Array(Schema.String) })
const ErrorCurrent = Schema.Union(Project, TicketSummary, TicketDetail)
const ErrorCurrentJsonSchema = Schema.Union(ProjectJsonSchema, TicketSummaryJsonSchema, TicketDetailJsonSchema)

const SimpleFailure = Schema.Union(
  exactStruct({ kind: Schema.Literal("unauthorized"), message: Schema.String, fields: ErrorFields }),
  exactStruct({ kind: Schema.Literal("not_found"), message: Schema.String, fields: ErrorFields }),
  exactStruct({ kind: Schema.Literal("invalid_transition"), message: Schema.String, fields: ErrorFields }),
  exactStruct({ kind: Schema.Literal("blocked_by_dependency"), message: Schema.String, fields: ErrorFields }),
  exactStruct({ kind: Schema.Literal("dependency_cycle"), message: Schema.String, fields: ErrorFields }),
  exactStruct({ kind: Schema.Literal("attachment_failed"), message: Schema.String, fields: ErrorFields }),
)
const SimpleFailureJsonSchema = Schema.Union(
  Schema.Struct({ kind: Schema.Literal("unauthorized"), message: Schema.String, fields: ErrorFields }),
  Schema.Struct({ kind: Schema.Literal("not_found"), message: Schema.String, fields: ErrorFields }),
  Schema.Struct({ kind: Schema.Literal("invalid_transition"), message: Schema.String, fields: ErrorFields }),
  Schema.Struct({ kind: Schema.Literal("blocked_by_dependency"), message: Schema.String, fields: ErrorFields }),
  Schema.Struct({ kind: Schema.Literal("dependency_cycle"), message: Schema.String, fields: ErrorFields }),
  Schema.Struct({ kind: Schema.Literal("attachment_failed"), message: Schema.String, fields: ErrorFields }),
)

export const RpcFailure = Schema.Union(
  SimpleFailure,
  exactStruct({ kind: Schema.Literal("validation_failed"), message: Schema.String, fields: ErrorFields }),
  exactStruct({
    kind: Schema.Literal("revision_conflict"),
    message: Schema.String,
    current: ErrorCurrent,
    fields: ErrorFields,
  }),
  exactStruct({ kind: Schema.Literal("internal_error"), message: Schema.String, fields: ErrorFields }),
)

export const RpcFailureJsonSchema = Schema.Union(
  SimpleFailureJsonSchema,
  Schema.Struct({ kind: Schema.Literal("validation_failed"), message: Schema.String, fields: ErrorFields }),
  Schema.Struct({
    kind: Schema.Literal("revision_conflict"),
    message: Schema.String,
    current: ErrorCurrentJsonSchema,
    fields: ErrorFields,
  }),
  Schema.Struct({ kind: Schema.Literal("internal_error"), message: Schema.String, fields: ErrorFields }),
)

const ProtocolErrorData = Schema.Union(
  exactStruct({ kind: Schema.Literal("invalid_request") }),
  exactStruct({ kind: Schema.Literal("method_not_found") }),
  exactStruct({ kind: Schema.Literal("internal_error"), correlation_id: Schema.String }),
)
const ProtocolErrorDataJsonSchema = Schema.Union(
  Schema.Struct({ kind: Schema.Literal("invalid_request") }),
  Schema.Struct({ kind: Schema.Literal("method_not_found") }),
  Schema.Struct({ kind: Schema.Literal("internal_error"), correlation_id: Schema.String }),
)

export const RpcSuccess = exactStruct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Union(Schema.String, Schema.Number),
  result: Schema.Unknown,
})
export const RpcSuccessJsonSchema = Schema.Struct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Union(Schema.String, Schema.Number),
  result: Schema.Unknown,
})

export const SessionInitialize = exactStruct({
  protocol_version: Schema.Literal(1),
  server_version: Schema.NonEmptyString,
  actor: Schema.Literal("me", "codex"),
  authorization: exactStruct({ kind: Schema.Literal("global") }),
})

export const RpcEnvelopeFailure = exactStruct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Union(Schema.String, Schema.Number, Schema.Null),
  error: exactStruct({
    code: Schema.Number,
    message: Schema.String,
    data: Schema.Union(RpcFailure, ProtocolErrorData),
  }),
})
export const RpcEnvelopeFailureJsonSchema = Schema.Struct({
  jsonrpc: Schema.Literal("2.0"),
  id: Schema.Union(Schema.String, Schema.Number, Schema.Null),
  error: Schema.Struct({
    code: Schema.Number,
    message: Schema.String,
    data: Schema.Union(RpcFailureJsonSchema, ProtocolErrorDataJsonSchema),
  }),
})

export type RpcSuccess = Schema.Schema.Encoded<typeof RpcSuccess>
export type RpcFailure = Schema.Schema.Encoded<typeof RpcFailure>
export type SessionInitialize = Schema.Schema.Type<typeof SessionInitialize>
