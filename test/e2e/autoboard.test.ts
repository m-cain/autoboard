import { afterAll, beforeAll, describe, expect, test } from "vitest"
import { Client } from "@modelcontextprotocol/sdk/client/index.js"
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js"
import { chromium, type Browser, type Page } from "playwright"
import { existsSync } from "node:fs"
import { join, resolve } from "node:path"

type Project = { id: string; key: string; revision: number; name: string }
type Ticket = { id: string; identifier: string; revision: number; title: string; status: string; assignee: string }
type Board = { project: Project; columns: Record<string, Ticket[]> }

const required = (key: string): string => {
  const value = process.env[key]
  if (!value) throw new Error(`${key} is required for the end-to-end suite`)
  return value
}

const root = resolve(import.meta.dirname, "../..")
const socketPath = required("AUTOBOARD_E2E_SOCKET")
const token = required("AUTOBOARD_E2E_TOKEN")
const baseUrl = required("AUTOBOARD_E2E_URL")
const attachmentPath = required("AUTOBOARD_E2E_ATTACHMENT")
const mcpEntrypoint = join(root, "mcp/dist/main.js")

let client: Client
let transport: StdioClientTransport
let browser: Browser
let page: Page

const tool = async <T>(name: string, args: Record<string, unknown>): Promise<T> => {
  const result = await client.callTool({ name, arguments: args })
  expect(result.isError).not.toBe(true)
  return result.structuredContent as T
}

const failedTool = async (name: string, args: Record<string, unknown>, kind: string) => {
  const result = await client.callTool({ name, arguments: args })
  expect(result.isError).toBe(true)
  const text = result.content.filter((item) => item.type === "text").map((item) => item.text).join("\n")
  expect(text).toContain(kind)
}

beforeAll(async () => {
  expect(existsSync(mcpEntrypoint), "build the MCP adapter before running the e2e suite").toBe(true)
  transport = new StdioClientTransport({
    command: process.execPath,
    args: [mcpEntrypoint],
    cwd: root,
    env: { ...process.env, AUTOBOARD_SOCKET: socketPath, AUTOBOARD_TOKEN: token },
    stderr: "pipe",
  })
  client = new Client({ name: "autoboard-e2e", version: "1.0.0" })
  await client.connect(transport)
  browser = await chromium.launch({ headless: true })
  page = await browser.newPage()
})

afterAll(async () => {
  await browser?.close()
  await client?.close()
  await transport?.close()
})

describe("Autoboard MCP to browser acceptance", () => {
  test("drives the complete read-only live-board loop", async () => {
    const project = await tool<Project>("create_project", { key: "AUTO", name: "Autoboard acceptance", description: "Created over MCP" })
    const parent = await tool<Ticket>("create_ticket", { project_id: project.id, title: "Parent work", status: "ready", assignee: "codex" })
    const subtask = await tool<Ticket>("create_ticket", { project_id: project.id, parent_ticket_id: parent.id, title: "Subtask", status: "backlog", assignee: "codex" })
    const blocker = await tool<Ticket>("create_ticket", { project_id: project.id, title: "Blocker", status: "backlog", assignee: "codex" })
    const blocked = await tool<Ticket>("create_ticket", { project_id: project.id, title: "Blocked work", status: "ready", assignee: "codex" })
    const codex = await tool<Ticket>("create_ticket", { project_id: project.id, title: "Codex leaf", status: "ready", assignee: "codex" })
    const human = await tool<Ticket>("create_ticket", { project_id: project.id, title: "Human work", status: "ready", assignee: "me" })

    await tool<Ticket>("add_dependency", { blocked_ticket_id: blocked.id, blocker_ticket_id: blocker.id, expected_revision: blocked.revision })
    await tool("add_comment", { ticket_id: subtask.id, body: "A durable **MCP** comment." })
    await tool("add_attachment_from_path", { ticket_id: subtask.id, path: attachmentPath })

    const actionable = await tool<{ tickets: Ticket[] }>("list_actionable_tickets", { project_id: project.id })
    expect(actionable.tickets.map((ticket) => ticket.identifier)).toEqual([codex.identifier])
    expect(actionable.tickets.map((ticket) => ticket.identifier)).not.toContain(human.identifier)
    expect(actionable.tickets.map((ticket) => ticket.identifier)).not.toContain(parent.identifier)
    expect(actionable.tickets.map((ticket) => ticket.identifier)).not.toContain(blocked.identifier)

    await tool<Ticket>("update_ticket", { ticket_id: codex.id, expected_revision: codex.revision, priority: "high" })
    await failedTool("update_ticket", { ticket_id: codex.id, expected_revision: codex.revision, title: "stale" }, "revision_conflict")
    const freshBlocked = await tool<Ticket>("get_ticket", { ticket_id: blocked.id })
    await failedTool("add_dependency", { blocked_ticket_id: blocker.id, blocker_ticket_id: freshBlocked.id, expected_revision: blocker.revision }, "dependency_cycle")

    const httpBoard = await fetch(`${baseUrl}/api/v1/projects/AUTO/board`).then(async (response) => {
      expect(response.status).toBe(200)
      return response.json() as Promise<Board>
    })
    expect(httpBoard.project.id).toBe(project.id)
    expect(httpBoard.columns.ready.map((ticket) => ticket.identifier)).toContain(codex.identifier)
    const httpTicket = await fetch(`${baseUrl}/api/v1/tickets/${subtask.identifier}`).then(async (response) => {
      expect(response.status).toBe(200)
      return response.json() as Promise<{ title: string; comments: Array<{ body: string }>; attachments: Array<{ original_filename: string }> }>
    })
    expect(httpTicket).toMatchObject({ title: "Subtask", comments: [{ body: "A durable **MCP** comment." }], attachments: [{ original_filename: "note.txt" }] })

    const apiMethods: string[] = []
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.startsWith("/api/v1")) apiMethods.push(request.method())
    })
    await page.goto(`${baseUrl}/projects`, { waitUntil: "networkidle" })
    await expect(page.getByRole("link", { name: "Autoboard acceptance" })).toBeVisible()
    await page.goto(`${baseUrl}/projects/AUTO`, { waitUntil: "networkidle" })
    await expect(page.getByRole("heading", { name: "Autoboard acceptance" })).toBeVisible()
    await expect(page.getByText("Codex leaf", { exact: true })).toBeVisible()
    await page.getByText("Subtask", { exact: true }).click()
    await expect(page.getByText("A durable MCP comment.", { exact: true })).toBeVisible()
    await expect(page.getByRole("link", { name: "note.txt" })).toBeVisible()
    await expect(page.locator("form, input, textarea, select, button, [contenteditable=true], [draggable=true]")).toHaveCount(0)

    await page.goto(`${baseUrl}/projects/AUTO`, { waitUntil: "networkidle" })
    const beforeTransitionUrl = page.url()
    const freshCodex = await tool<Ticket>("get_ticket", { ticket_id: codex.id })
    const inProgress = await tool<Ticket>("transition_ticket", { ticket_id: freshCodex.id, expected_revision: freshCodex.revision, status: "in_progress" })
    await expect(page.getByRole("region", { name: "In progress tickets" }).getByText("Codex leaf", { exact: true })).toBeVisible()
    expect(page.url()).toBe(beforeTransitionUrl)
    const done = await tool<Ticket>("transition_ticket", { ticket_id: inProgress.id, expected_revision: inProgress.revision, status: "done" })
    await expect(page.getByRole("region", { name: "Done tickets" }).getByText("Codex leaf", { exact: true })).toBeVisible()
    expect(done.status).toBe("done")
    expect(apiMethods).toHaveLength(expect.any(Number))
    expect(apiMethods.every((method) => method === "GET")).toBe(true)
  })
})
