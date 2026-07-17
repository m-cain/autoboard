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

type RootData = { readonly projects: { readonly active: Project[]; readonly archived: Project[] }; readonly triage: { readonly tickets: unknown[] } }

export const RouterAppShell = () => {
  const data = useRouteLoaderData("root") as RootData
  const location = useLocation()
  const { revalidate } = useRevalidator()
  const matches = useMatches()
  const ticketData = matches.find((match) => match.id === "ticket")?.loaderData as TicketRouteData | undefined
  const projects = [...data.projects.active, ...data.projects.archived]
  const current = useRef({ projects, ticket: ticketData?.ticket })
  current.current = { projects, ticket: ticketData?.ticket }

  useEffect(() => {
    if (typeof EventSource === "undefined") return
    return startActivityRevalidation({
      stream: activityStream(),
      relevant: (event) => isActivityRelevant(event, location.pathname, current.current.projects, current.current.ticket),
      revalidate,
      schedule: (work) => window.setTimeout(work, 0),
      cancel: (timer) => window.clearTimeout(timer),
    })
  }, [location.pathname, revalidate])

  return <AppShell triageCount={data.triage.tickets.length} projects={data.projects.active} />
}
