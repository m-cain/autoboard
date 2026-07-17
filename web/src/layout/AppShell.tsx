import { useEffect, useRef } from "react"
import { Link, Outlet, useLocation, useMatches, useRevalidator, useRouteLoaderData } from "react-router"
import type { Project } from "@autoboard/contracts"
import { activityStream } from "../events/activityStream.js"
import { startActivityRevalidation } from "../events/liveRevalidation.js"
import { isActivityRelevant } from "../events/revalidation.js"
import type { TicketRouteData } from "../router.js"

type ShellProject = Pick<Project, "key" | "name">
type Props = { readonly triageCount: number; readonly projects: readonly ShellProject[]; readonly children?: React.ReactNode }

export const AppShell = ({ triageCount, projects, children }: Props) => (
  <div className="app-shell">
    <header className="app-header">
      <Link className="wordmark" to="/projects">Autoboard</Link>
      <Link className="triage-link" to="/triage">Triage ({triageCount})</Link>
    </header>
    <div className="app-body">
      <nav className="project-nav" aria-label="Projects">
        <p className="project-nav__heading">Projects</p>
        <ul>
          {projects.map((project) => <li key={project.key}><Link to={`/projects/${encodeURIComponent(project.key)}`}>{project.name}</Link></li>)}
        </ul>
      </nav>
      <main>{children ?? <Outlet />}</main>
    </div>
  </div>
)

type RootData = { readonly projects: { readonly active: Project[]; readonly archived: Project[] }; readonly triage: { readonly tickets: readonly { readonly id: string }[] } }

export const RouterAppShell = () => {
  const data = useRouteLoaderData("root") as RootData
  const location = useLocation()
  const { revalidate } = useRevalidator()
  const matches = useMatches()
  const ticketData = matches.find((match) => match.id === "ticket")?.loaderData as TicketRouteData | undefined
  const projects = [...data.projects.active, ...data.projects.archived]
  const rootTriageTicketIds = data.triage.tickets.map((triageTicket) => triageTicket.id)
  const current = useRef({ pathname: location.pathname, projects, ticket: ticketData?.ticket, drawer: false, rootTriageTicketIds })
  const ticket = ticketData?.ticket
  const relatedTicketIds = ticket ? [ticket.parent, ...ticket.subtasks, ...ticket.blockers, ...ticket.blocked_tickets].filter((related): related is { readonly id: string } => related !== null).map((related) => related.id) : undefined
  const background = typeof location.state === "object" && location.state !== null && "backgroundLocation" in location.state ? location.state.backgroundLocation : undefined
  const drawer = Boolean(ticket && typeof background === "object" && background !== null && "pathname" in background && background.pathname === `/projects/${encodeURIComponent(ticket.project.key)}`)
  current.current = { pathname: location.pathname, projects, ticket: ticket ? { ...ticket, relatedTicketIds } : undefined, drawer, rootTriageTicketIds }

  useEffect(() => {
    if (typeof EventSource === "undefined") return
    return startActivityRevalidation({
      stream: activityStream(),
      relevant: (event) => isActivityRelevant(event, current.current.pathname, current.current.projects, current.current.ticket ? { ...current.current.ticket, drawer: current.current.drawer } : undefined, current.current.rootTriageTicketIds),
      revalidate,
      schedule: (work) => window.setTimeout(work, 0),
      cancel: (timer) => window.clearTimeout(timer),
    })
  }, [revalidate])

  return <AppShell triageCount={data.triage.tickets.length} projects={data.projects.active} />
}
