import { Link, useLocation } from "react-router"
import type { TicketSummary } from "@autoboard/contracts"

export const TicketCard = ({ ticket }: { readonly ticket: TicketSummary }) => {
  const location = useLocation()
  return <article className="ticket-card">
    <Link className="ticket-card__title" data-ticket-identifier={ticket.identifier} to={`/tickets/${encodeURIComponent(ticket.identifier)}`} state={{ backgroundLocation: location, drawerDepth: 1, originIdentifier: ticket.identifier }}>
      {ticket.title}
    </Link>
    <p className="ticket-card__identifier">{ticket.identifier}</p>
    <div className="ticket-card__meta" aria-label="Ticket metadata">
      <span>{ticket.assignee}</span>
      <span>{ticket.priority}</span>
    </div>
    {ticket.blocked ? <p className="ticket-card__blocked">Blocked by unresolved dependencies</p> : null}
    <div className="ticket-card__counts" aria-label="Ticket activity counts">
      <span>{ticket.comment_count} {ticket.comment_count === 1 ? "comment" : "comments"}</span>
      <span>{ticket.attachment_count} {ticket.attachment_count === 1 ? "attachment" : "attachments"}</span>
    </div>
  </article>
}
