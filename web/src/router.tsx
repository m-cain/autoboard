/* oxlint-disable react/only-export-components -- React Router route configuration and its loader-aware views share this module. */
import { Effect } from "effect"
import { createBrowserRouter, Link, useLoaderData, useRouteError, useRouteLoaderData } from "react-router"
import { ApiClient, HttpError, type ApiClientService } from "./api/client.js"
import { RouterAppShell } from "./layout/AppShell.js"
import { runApi } from "./runtime.js"
import { ProjectsPage } from "./pages/ProjectsPage.js"
import { ProjectBoardPage } from "./pages/ProjectBoardPage.js"
import { TriagePage } from "./pages/TriagePage.js"
import { CanceledPage } from "./pages/CanceledPage.js"
import type { Project, ProjectBoard, TicketSummary } from "@autoboard/contracts"

const fromApi = <A, E>(get: (client: ApiClientService) => Effect.Effect<A, E>) => runApi(Effect.flatMap(ApiClient, get))

type TicketListData = { readonly tickets: readonly TicketSummary[] }
type RootData = { readonly projects: { readonly active: readonly Project[]; readonly archived: readonly Project[] }; readonly triage: TicketListData }

const rootLoader = async (): Promise<RootData> => {
  const [projects, triage] = await Promise.all([
    fromApi((client) => client.listProjects()),
    fromApi((client) => client.listTriage()),
  ])
  return { projects, triage }
}

const projectKey = (params: Record<string, string | undefined>): string => {
  if (!params.key) throw new HttpError({ status: 404, message: "Project not found" })
  return params.key
}

const ErrorPage = () => {
  const error = useRouteError()
  const unavailable = error instanceof HttpError && error.status === 503
  const missing = error instanceof HttpError && error.status === 404
  return <main className="route-error"><p className="eyebrow">{missing ? "Not found" : "Unavailable"}</p><h1>{missing ? "This view does not exist" : "Autoboard is unavailable"}</h1><p>{unavailable ? "The local server is starting or unavailable. Try again shortly." : "The current board could not be loaded."}</p><Link to="/projects">View projects</Link></main>
}

const ProjectsRoute = () => <ProjectsPage projects={(useLoaderData() as RootData).projects} />
const TriageRoute = () => {
  const root = useRouteLoaderData("root") as RootData
  return <TriagePage tickets={root.triage.tickets} projects={root.projects.active} />
}
const BoardRoute = () => <ProjectBoardPage board={useLoaderData()} />
const CanceledRoute = () => {
  const { project, tickets } = useLoaderData() as { readonly project: Project; readonly tickets: readonly TicketSummary[] }
  return <CanceledPage project={project} tickets={tickets} />
}

export const router = createBrowserRouter([
  {
    id: "root", path: "/", loader: rootLoader, Component: RouterAppShell, ErrorBoundary: ErrorPage,
    children: [
      { index: true, loader: () => null, Component: () => <ProjectsRoute /> },
      { path: "projects", Component: ProjectsRoute },
      {
        path: "projects/:key", loader: ({ params }) => fromApi((client) => client.getProjectBoard(projectKey(params))), Component: BoardRoute,
      },
      {
        path: "projects/:key/canceled", loader: async ({ params }) => {
          const key = projectKey(params)
          const [board, canceled] = await Promise.all([fromApi<ProjectBoard, unknown>((client) => client.getProjectBoard(key)), fromApi<TicketListData, unknown>((client) => client.getCanceledTickets(key))])
          return { project: board.project, tickets: canceled.tickets }
        }, Component: CanceledRoute,
      },
      { path: "triage", Component: TriageRoute },
      { path: "*", Component: () => <ErrorPage /> },
    ],
  },
], { basename: import.meta.env.BASE_URL })
