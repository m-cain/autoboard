import type { Project, TicketSummary } from "@autoboard/contracts";
import { TicketCard } from "../components/TicketCard.js";

export const CanceledPage = ({
  project,
  tickets,
}: {
  readonly project: Project;
  readonly tickets: readonly TicketSummary[];
}) => (
  <section className="page list-page">
    <div className="page-heading">
      <p className="eyebrow">{project.key}</p>
      <h1>Canceled tickets</h1>
    </div>
    {tickets.length === 0 ? (
      <p className="empty-state">No canceled tickets</p>
    ) : (
      <div className="ticket-list">
        {tickets.map((ticket) => (
          <TicketCard key={ticket.id} ticket={ticket} />
        ))}
      </div>
    )}
  </section>
);
