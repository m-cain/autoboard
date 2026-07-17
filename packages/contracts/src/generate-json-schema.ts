import { mkdir, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { fileURLToPath } from "node:url"
import { Schema } from "effect"
import * as OpenApiJsonSchema from "effect/JSONSchema"
import {
  ActivityEventJsonSchema,
  AttachmentJsonSchema,
  AttachmentRpcJsonSchema,
  ProjectBoardJsonSchema,
  ProjectJsonSchema,
  TicketDetailJsonSchema,
  TicketSummaryJsonSchema,
} from "./domain.js"
import { RpcFailure, RpcSuccess } from "./rpc.js"

const destination = join(fileURLToPath(new URL("../generated", import.meta.url)))

const schemas = {
  "activity-event.schema.json": ActivityEventJsonSchema,
  "attachment-rpc.schema.json": AttachmentRpcJsonSchema,
  "attachment.schema.json": AttachmentJsonSchema,
  "project-board.schema.json": ProjectBoardJsonSchema,
  "project.schema.json": ProjectJsonSchema,
  "rpc-failure.schema.json": RpcFailure,
  "rpc-success.schema.json": RpcSuccess,
  "ticket-detail.schema.json": TicketDetailJsonSchema,
  "ticket-summary.schema.json": TicketSummaryJsonSchema,
}

const xemaCompatible = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(xemaCompatible)
  if (value === null || typeof value !== "object") {
    return typeof value === "string" ? value.replaceAll("#/$defs/", "#/definitions/") : value
  }

  const object = value as Record<string, unknown>
  if (object.$id === "/schemas/unknown" || object.$id === "/schemas/any") return true

  return Object.fromEntries(
    Object.entries(object).map(([key, nested]) => [
      key === "$defs" ? "definitions" : key,
      xemaCompatible(nested),
    ]),
  )
}

await mkdir(destination, { recursive: true })

await Promise.all(
  Object.entries(schemas as Record<string, Schema.Schema.Any>).map(async ([filename, schema]) => {
    // Effect 3 exposes the OpenAPI generator through JSONSchema.make. Emit
    // draft-07 here: it is accepted by OpenAPI tooling and the pinned Xema
    // validator, unlike the newer OpenAPI 3.1 meta-schema URI.
    const document = xemaCompatible(OpenApiJsonSchema.make(schema, { target: "jsonSchema7" }))
    await writeFile(join(destination, filename), `${JSON.stringify(document, null, 2)}\n`)
  }),
)
