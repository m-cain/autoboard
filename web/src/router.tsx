/* oxlint-disable react/only-export-components -- React Router route configuration and its loader-aware views share this module. */
import { Effect } from "effect"
import { createBrowserRouter, Link, useLoaderData, useLocation, useRouteError, useRouteLoaderData, type RouteObject } from "react-router"
import { ApiClient, HttpError, RequestAbortedError, createApiClient, type ApiClientService } from "./api/client.js"
import { RouterAppShell } from "./layout/AppShell.js"
import { createApiRunner } from "./runtime.js"
import { ProjectsPage } from "./pages/ProjectsPage.js"
import { ProjectBoardPage } from "./pages/ProjectBoardPage.js"
import { TriagePage } from "./pages/TriagePage.js"
import { CanceledPage } from "./pages/CanceledPage.js"
import { TicketDetailPage } from "./pages/TicketDetailPage.js"
import { TicketDrawer } from "./components/TicketDrawer.js"
import type { Project, ProjectBoard, TicketDetail, TicketSummary } from "@autoboard/contracts"

type TicketListData = { readonly tickets: readonly TicketSummary[] }
type RootData = { readonly projects: { readonly active: readonly Project[]; readonly archived: readonly Project[] }; readonly triage: TicketListData }
export type TicketRouteData = { readonly ticket: TicketDetail; readonly board: ProjectBoard }

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
const ticketIdentifier = (params: Record<string, string | undefined>): string => {
  if (!params.identifier) throw new HttpError({ status: 404, message: "Ticket not found" })
  return params.identifier
}
const backgroundPath = (state: unknown): string | undefined => {
  if (typeof state !== "object" || state === null || !("backgroundLocation" in state)) return undefined
  const background = state.backgroundLocation
  if (typeof background !== "object" || background === null || !("pathname" in background) || typeof background.pathname !== "string") return undefined
  const search = "search" in background && typeof background.search === "string" ? background.search : ""
  const hash = "hash" in background && typeof background.hash === "string" ? background.hash : ""
  return `${background.pathname}${search}${hash}`
}
const TicketRoute = () => {
  const data = useLoaderData() as TicketRouteData
  const state = useLocation().state
  const closeTo = backgroundPath(state)
  if (!closeTo) return <TicketDetailPage ticket={data.ticket} />
  const originIdentifier = typeof state === "object" && state !== null && "originIdentifier" in state && typeof state.originIdentifier === "string" ? state.originIdentifier : undefined
  return <div className="ticket-drawer-layer"><div aria-hidden="true"><ProjectBoardPage board={data.board} /></div><TicketDrawer closeTo={closeTo} originIdentifier={originIdentifier}><TicketDetailPage ticket={data.ticket} /></TicketDrawer></div>
}
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
      {
        id: "ticket", path: "tickets/:identifier", loader: async ({ params, request }): Promise<TicketRouteData> => {
          const ticket = await fromApi<TicketDetail, unknown>((service) => service.getTicket(ticketIdentifier(params), request.signal))
          const board = await fromApi<ProjectBoard, unknown>((service) => service.getProjectBoard(ticket.project.key, request.signal))
          return { ticket, board }
        }, Component: TicketRoute,
      },
      { path: "triage", Component: TriageRoute },
      { path: "*", Component: NotFoundPage },
    ],
  },
  ]
}

export const router = createBrowserRouter(createAppRoutes(createApiClient()), { basename: import.meta.env.BASE_URL })
