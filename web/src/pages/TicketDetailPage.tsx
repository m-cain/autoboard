import { Link, useLocation } from "react-router"
import type { TicketDetail, TicketSummary } from "@autoboard/contracts"
import { ActivityTimeline } from "../components/ActivityTimeline.js"
import { Markdown } from "../components/Markdown.js"

const ticketPath = (identifier: string) => `/tickets/${encodeURIComponent(identifier)}`

type DrawerState = { readonly backgroundLocation: unknown; readonly drawerDepth?: number; readonly originIdentifier?: string }
const drawerState = (state: unknown): DrawerState | undefined => {
  if (typeof state !== "object" || state === null || !("backgroundLocation" in state)) return undefined
  const background = state.backgroundLocation
  if (typeof background !== "object" || background === null || !("pathname" in background) || typeof background.pathname !== "string" || !/^\/projects\/[^/]+$/.test(background.pathname)) return undefined
  return state as DrawerState
}

const TicketLinks = ({ tickets, empty, state }: { readonly tickets: readonly TicketSummary[]; readonly empty: string; readonly state?: DrawerState }) => (
  tickets.length === 0 ? <p className="empty-state">{empty}</p> : <ul className="detail-ticket-links">
    {tickets.map((ticket) => <li key={ticket.id}><Link to={ticketPath(ticket.identifier)} state={state ? { ...state, drawerDepth: (state.drawerDepth ?? 1) + 1 } : undefined}>{ticket.identifier} · {ticket.title}</Link></li>)}
  </ul>
)

export const TicketDetailPage = ({ ticket }: { readonly ticket: TicketDetail }) => {
  const state = drawerState(useLocation().state)
  return <article className="page ticket-detail">
    <header className="page-heading">
      <p className="eyebrow">{ticket.identifier}</p>
      <h1>{ticket.title}</h1>
      <dl className="ticket-facts">
        <div><dt>Assignee</dt><dd>{ticket.assignee}</dd></div>
        <div><dt>Status</dt><dd>{ticket.status.replace("_", " ")}</dd></div>
        <div><dt>Priority</dt><dd>{ticket.priority}</dd></div>
        <div><dt>Blocking</dt><dd>{ticket.blocked ? "Blocked" : "Not blocked"}</dd></div>
      </dl>
      {ticket.labels.length > 0 ? <ul className="label-list" aria-label="Labels">{ticket.labels.map((label: { readonly id: string; readonly name: string }) => <li key={label.id}>{label.name}</li>)}</ul> : null}
    </header>

    <section className="detail-section" aria-labelledby="description-heading"><h2 id="description-heading">Description</h2><Markdown>{ticket.description}</Markdown></section>
    {ticket.parent ? <section className="detail-section" aria-labelledby="parent-heading"><h2 id="parent-heading">Parent</h2><TicketLinks tickets={[ticket.parent]} empty="No parent" state={state} /></section> : null}
    <section className="detail-section" aria-labelledby="blockers-heading"><h2 id="blockers-heading">Blockers</h2><TicketLinks tickets={ticket.blockers} empty="No unresolved blockers" state={state} /></section>
    <section className="detail-section" aria-labelledby="blocked-heading"><h2 id="blocked-heading">Blocks</h2><TicketLinks tickets={ticket.blocked_tickets} empty="Does not block other tickets" state={state} /></section>
    <section className="detail-section" aria-labelledby="subtasks-heading"><h2 id="subtasks-heading">Subtasks</h2><TicketLinks tickets={ticket.subtasks} empty="No subtasks" state={state} /></section>
    <section className="detail-section" aria-labelledby="comments-heading"><h2 id="comments-heading">Comments</h2>{ticket.comments.length === 0 ? <p className="empty-state">No comments yet</p> : <ol className="comment-list">{ticket.comments.map((comment: { readonly id: string; readonly actor: string; readonly inserted_at: string; readonly body: string }) => <li key={comment.id}><p className="comment-meta">{comment.actor} · <time dateTime={comment.inserted_at}>{new Date(comment.inserted_at).toLocaleString()}</time></p><Markdown>{comment.body}</Markdown></li>)}</ol>}</section>
    <section className="detail-section" aria-labelledby="attachments-heading"><h2 id="attachments-heading">Attachments</h2>{ticket.attachments.length === 0 ? <p className="empty-state">No attachments</p> : <ul className="attachment-list">{ticket.attachments.map((attachment: { readonly id: string; readonly original_filename: string; readonly media_type: string; readonly byte_size: number }) => <li key={attachment.id}><a href={`/api/v1/attachments/${encodeURIComponent(attachment.id)}`}>{attachment.original_filename}</a><span>{attachment.media_type} · {attachment.byte_size.toLocaleString()} bytes</span></li>)}</ul>}</section>
    <ActivityTimeline activity={ticket.activity} />
  </article>
}
