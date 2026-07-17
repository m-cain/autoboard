import { Link, Outlet, useRouteLoaderData } from "react-router"
import type { Project } from "@autoboard/contracts"

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
  return <AppShell triageCount={data.triage.tickets.length} projects={data.projects.active} />
}
