import { expect, test } from "vitest"
import { MCP_INSTRUCTIONS } from "../src/server.js"

test("opening instructions begin with the required self-contained safety guidance", () => {
  expect(MCP_INSTRUCTIONS.slice(0, 512)).toBe(
    "Autoboard is a direct-write project board. Tickets assigned to `me` are reserved for the human. Execute only tickets returned by list_actionable_tickets unless the human explicitly instructs otherwise. Read the latest entity before revision-checked writes. Confirm broad reorganizations, project archival, and dependency removal with the human.",
  )
})
