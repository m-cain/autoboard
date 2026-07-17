/* oxlint-disable react/only-export-components -- React Router route configuration and its loader-aware views share this module. */
import { Effect } from "effect"
import { createBrowserRouter, Link, useLoaderData, useRouteError, useRouteLoaderData, type RouteObject } from "react-router"
import { ApiClient, HttpError, RequestAbortedError, createApiClient, type ApiClientService } from "./api/client.js"
import { RouterAppShell } from "./layout/AppShell.js"
import { createApiRunner } from "./runtime.js"
import { ProjectsPage } from "./pages/ProjectsPage.js"
import { ProjectBoardPage } from "./pages/ProjectBoardPage.js"
import { TriagePage } from "./pages/TriagePage.js"
import { CanceledPage } from "./pages/CanceledPage.js"
import type { Project, ProjectBoard, TicketSummary } from "@autoboard/contracts"

type TicketListData = { readonly tickets: readonly TicketSummary[] }
type RootData = { readonly projects: { readonly active: readonly Project[]; readonly archived: readonly Project[] }; readonly triage: TicketListData }

const projectKey = (params: Record<string, string | undefined>): string => {
  if (!params.key) throw new HttpError({ status: 404, message: "Project not found" })
  return params.key
}

const ErrorPage = () => {
  const error = useRouteError()
  if (error instanceof RequestAbortedError) return <main className="route-loading" role="status">Loading Autoboard…</main>
  const unavailable = error instanceof HttpError && error.status === 503
  const missing = error instanceof HttpError && error.status === 404
  return <main className="route-error"><p className="eyebrow">{missing ? "Not found" : "Unavailable"}</p><h1>{missing ? "This view does not exist" : "Autoboard is unavailable"}</h1><p>{unavailable ? "The local server is starting or unavailable. Try again shortly." : "The current board could not be loaded."}</p><Link to="/projects">View projects</Link></main>
}

const ProjectsRoute = () => <ProjectsPage projects={(useRouteLoaderData("root") as RootData).projects} />
const TriageRoute = () => {
  const root = useRouteLoaderData("root") as RootData
  return <TriagePage tickets={root.triage.tickets} projects={root.projects.active} />
}
const BoardRoute = () => <ProjectBoardPage board={useLoaderData()} />
const CanceledRoute = () => {
  const { project, tickets } = useLoaderData() as { readonly project: Project; readonly tickets: readonly TicketSummary[] }
  return <CanceledPage project={project} tickets={tickets} />
}

const NotFoundPage = () => <section className="route-error"><p className="eyebrow">Not found</p><h1>This view does not exist</h1><p>Choose a project to continue.</p><Link to="/projects">View projects</Link></section>
const InitialLoading = () => <div className="route-loading" role="status" aria-live="polite">Loading Autoboard…</div>

export const createAppRoutes = (client: ApiClientService): RouteObject[] => {
  const run = createApiRunner(client)
  const fromApi = <A, E>(get: (service: ApiClientService) => Effect.Effect<A, E>) => run(Effect.flatMap(ApiClient, get))
  const rootLoader = async ({ request }: { readonly request: Request }): Promise<RootData> => {
    const [projects, triage] = await Promise.all([fromApi((service) => service.listProjects(request.signal)), fromApi((service) => service.listTriage(request.signal))])
    return { projects, triage }
  }

  return [
  {
    id: "root", path: "/", loader: rootLoader, Component: RouterAppShell, ErrorBoundary: ErrorPage, HydrateFallback: InitialLoading,
    children: [
      { index: true, Component: ProjectsRoute },
      { path: "projects", Component: ProjectsRoute },
      {
        path: "projects/:key", loader: ({ params, request }) => fromApi((service) => service.getProjectBoard(projectKey(params), request.signal)), Component: BoardRoute,
      },
      {
        path: "projects/:key/canceled", loader: async ({ params, request }) => {
          const key = projectKey(params)
          const [board, canceled] = await Promise.all([fromApi<ProjectBoard, unknown>((service) => service.getProjectBoard(key, request.signal)), fromApi<TicketListData, unknown>((service) => service.getCanceledTickets(key, request.signal))])
          return { project: board.project, tickets: canceled.tickets }
        }, Component: CanceledRoute,
      },
      { path: "triage", Component: TriageRoute },
      { path: "*", Component: NotFoundPage },
    ],
  },
  ]
}

export const router = createBrowserRouter(createAppRoutes(createApiClient()), { basename: import.meta.env.BASE_URL })
