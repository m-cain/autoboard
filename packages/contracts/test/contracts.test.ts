import { readFile } from "node:fs/promises"
import { join } from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, test } from "vitest"
import { Schema } from "effect"
import {
  Attachment,
  AttachmentRpc,
  ProjectBoard,
  RpcFailure,
  TicketDetail,
} from "../src/index.js"

const fixtures = join(
  fileURLToPath(new URL("../../../server/test/fixtures/contracts", import.meta.url)),
)

const fixture = async (name: string): Promise<unknown> =>
  JSON.parse(await readFile(join(fixtures, name), "utf8"))

describe("Autoboard transport contracts", () => {
  test("decodes every frozen read fixture with its matching exact schema", async () => {
    expect(Schema.decodeUnknownSync(ProjectBoard)(await fixture("project_board.json"))).toBeTruthy()
    expect(Schema.decodeUnknownSync(TicketDetail)(await fixture("ticket_detail.json"))).toBeTruthy()
    expect(Schema.decodeUnknownSync(TicketDetail)(await fixture("parent_ticket_detail.json"))).toBeTruthy()
    expect(Schema.decodeUnknownSync(AttachmentRpc)(await fixture("attachment_rpc.json"))).toBeTruthy()
    expect(Schema.decodeUnknownSync(RpcFailure)(await fixture("error_current.json"))).toBeTruthy()
  })

  test("rejects unknown enum values and a missing optimistic revision", async () => {
    const board = (await fixture("project_board.json")) as {
      project: { state: string; revision?: number }
    }

    expect(() => Schema.decodeUnknownSync(ProjectBoard)({ ...board, project: { ...board.project, state: "paused" } })).toThrow()
    const detail = (await fixture("ticket_detail.json")) as { revision?: number }
    const { revision: _revision, ...withoutRevision } = detail
    expect(() => Schema.decodeUnknownSync(TicketDetail)(withoutRevision)).toThrow()
  })

  test("keeps HTTP attachments exact and storage-path free", async () => {
    const rpcAttachment = await fixture("attachment_rpc.json") as Record<string, unknown>
    const { managed_path: _managedPath, ...httpAttachment } = rpcAttachment

    expect(Schema.decodeUnknownSync(Attachment)(httpAttachment)).toEqual(httpAttachment)
    expect(() => Schema.decodeUnknownSync(Attachment)(rpcAttachment)).toThrow()
  })
})
