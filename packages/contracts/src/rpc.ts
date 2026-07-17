import { Schema } from "effect"
import { exactStruct, Project, ProjectJsonSchema, TicketDetail, TicketDetailJsonSchema, TicketSummary, TicketSummaryJsonSchema } from "./domain.js"

const Fields = Schema.Record({ key: Schema.String, value: Schema.Array(Schema.String) })
const Current = Schema.Union(Project, TicketSummary, TicketDetail)
const CurrentJson = Schema.Union(ProjectJsonSchema, TicketSummaryJsonSchema, TicketDetailJsonSchema)
const simpleKinds = ["unauthorized", "not_found", "invalid_transition", "blocked_by_dependency", "dependency_cycle", "attachment_failed"] as const
const simple = Schema.Union(...simpleKinds.map((kind) => exactStruct({ kind: Schema.Literal(kind), message: Schema.String, fields: Fields })))
const simpleJson = Schema.Union(...simpleKinds.map((kind) => Schema.Struct({ kind: Schema.Literal(kind), message: Schema.String, fields: Fields })))
const validation = exactStruct({ kind: Schema.Literal("validation_failed"), message: Schema.String, fields: Fields })
const validationJson = Schema.Struct({ kind: Schema.Literal("validation_failed"), message: Schema.String, fields: Fields })
const revision = exactStruct({ kind: Schema.Literal("revision_conflict"), message: Schema.String, fields: Fields, current: Current })
const revisionJson = Schema.Struct({ kind: Schema.Literal("revision_conflict"), message: Schema.String, fields: Fields, current: CurrentJson })

/** Presenter.error/1 domain payload; internal RPC failures use the separate branch below. */
export const RpcFailure = Schema.Union(simple, validation, revision)
export const RpcFailureJsonSchema = Schema.Union(simpleJson, validationJson, revisionJson)

const invalidRequest = exactStruct({ kind: Schema.Literal("invalid_request") })
const methodNotFound = exactStruct({ kind: Schema.Literal("method_not_found") })
const internal = exactStruct({ kind: Schema.Literal("internal_error"), correlation_id: Schema.String })
const invalidRequestJson = Schema.Struct({ kind: Schema.Literal("invalid_request") })
const methodNotFoundJson = Schema.Struct({ kind: Schema.Literal("method_not_found") })
const internalJson = Schema.Struct({ kind: Schema.Literal("internal_error"), correlation_id: Schema.String })

const envelope = <D extends Schema.Schema.Any>(code: number, data: D) => exactStruct({
  jsonrpc: Schema.Literal("2.0"), id: Schema.Union(Schema.String, Schema.Number, Schema.Null),
  error: exactStruct({ code: Schema.Literal(code), message: Schema.String, data }),
})
const envelopeJson = <D extends Schema.Schema.Any>(code: number, data: D) => Schema.Struct({
  jsonrpc: Schema.Literal("2.0"), id: Schema.Union(Schema.String, Schema.Number, Schema.Null),
  error: Schema.Struct({ code: Schema.Literal(code), message: Schema.String, data }),
})

export const RpcEnvelopeFailure = Schema.Union(
  envelope(-32600, invalidRequest), envelope(-32601, methodNotFound), envelope(-32602, validation),
  envelope(-32010, RpcFailure), envelope(-32010, internal),
)
export const RpcEnvelopeFailureJsonSchema = Schema.Union(
  envelopeJson(-32600, invalidRequestJson), envelopeJson(-32601, methodNotFoundJson), envelopeJson(-32602, validationJson),
  envelopeJson(-32010, RpcFailureJsonSchema), envelopeJson(-32010, internalJson),
)

export const RpcSuccess = exactStruct({ jsonrpc: Schema.Literal("2.0"), id: Schema.Union(Schema.String, Schema.Number), result: Schema.Unknown })
export const RpcSuccessJsonSchema = Schema.Struct({ jsonrpc: Schema.Literal("2.0"), id: Schema.Union(Schema.String, Schema.Number), result: Schema.Unknown })
export const SessionInitialize = exactStruct({ protocol_version: Schema.Literal(1), server_version: Schema.NonEmptyString, actor: Schema.Literal("me", "codex"), authorization: exactStruct({ kind: Schema.Literal("global") }) })

export type RpcSuccess = Schema.Schema.Encoded<typeof RpcSuccess>
export type RpcFailure = Schema.Schema.Encoded<typeof RpcFailure>
export type SessionInitialize = Schema.Schema.Type<typeof SessionInitialize>
