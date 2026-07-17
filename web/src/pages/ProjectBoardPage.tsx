import { Link } from "react-router"
import type { ProjectBoard, TicketSummary } from "@autoboard/contracts"
import { TicketCard } from "../components/TicketCard.js"

type BoardColumnKey = "backlog" | "ready" | "in_progress" | "done"
const columns: ReadonlyArray<{ readonly key: BoardColumnKey; readonly title: string }> = [
  { key: "backlog", title: "Backlog" }, { key: "ready", title: "Ready" }, { key: "in_progress", title: "In progress" }, { key: "done", title: "Done" },
]

const BoardColumn = ({ title, tickets }: { readonly title: string; readonly tickets: readonly TicketSummary[] }) => (
  <section className="board-column" aria-label={`${title} tickets`}>
    <h2>{title}</h2>
    <p className="board-column__count">{tickets.length}</p>
    <div className="board-column__tickets">{tickets.length === 0 ? <p className="empty-state">No tickets</p> : tickets.map((ticket) => <TicketCard key={ticket.id} ticket={ticket} />)}</div>
  </section>
)

export const ProjectBoardPage = ({ board }: { readonly board: ProjectBoard }) => (
  <section className="page board-page">
    <div className="page-heading page-heading--split"><div><p className="eyebrow">{board.project.key}</p><h1>{board.project.name}</h1></div><Link to={`/projects/${encodeURIComponent(board.project.key)}/canceled`}>Canceled tickets</Link></div>
    <div className="kanban-board">{columns.map(({ key, title }) => <BoardColumn key={key} title={title} tickets={board.columns[key]} />)}</div>
  </section>
)
