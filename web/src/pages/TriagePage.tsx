import type { Project, TicketSummary } from "@autoboard/contracts";
import { TicketCard } from "../components/TicketCard.js";

export const TriagePage = ({
  tickets,
  projects,
}: {
  readonly tickets: readonly TicketSummary[];
  readonly projects: readonly Project[];
}) => {
  const names = new Map(projects.map((project) => [project.id, project.name]));
  const groups = tickets.reduce<Map<string, TicketSummary[]>>(
    (result, ticket) => {
      const group = result.get(ticket.project_id) ?? [];
      group.push(ticket);
      result.set(ticket.project_id, group);
      return result;
    },
    new Map(),
  );
  return (
    <section className="page list-page">
      <div className="page-heading">
        <p className="eyebrow">Across active projects</p>
        <h1>Triage</h1>
      </div>
      {groups.size === 0 ? (
        <p className="empty-state">No tickets in triage</p>
      ) : (
        [...groups].map(([projectId, groupedTickets]) => (
          <section className="ticket-group" key={projectId}>
            <h2>{names.get(projectId) ?? "Project"}</h2>
            <div className="ticket-list">
              {groupedTickets.map((ticket) => (
                <TicketCard key={ticket.id} ticket={ticket} />
              ))}
            </div>
          </section>
        ))
      )}
    </section>
  );
};
