import type { ActivityEvent, Project } from "@autoboard/contracts"

type CurrentTicket = { readonly id: string; readonly project_id: string; readonly drawer: boolean; readonly relatedTicketIds?: readonly string[] }

const payloadId = (payload: Record<string, unknown>, key: string): string | undefined => typeof payload[key] === "string" ? payload[key] : undefined

export const isActivityRelevant = (event: ActivityEvent, pathname: string, projects: readonly Project[], currentTicket?: CurrentTicket): boolean => {
  if (pathname === "/" || pathname === "/projects") return event.ticket_id === null
  if (pathname === "/triage") return projects.some((project) => project.id === event.project_id)
  if (/^\/tickets\/[^/]+$/.test(pathname)) {
    if (!currentTicket) return false
    return currentTicket.drawer
      ? event.project_id === currentTicket.project_id
      : event.project_id === currentTicket.project_id && (
        event.ticket_id === null ||
        event.ticket_id === currentTicket.id ||
        currentTicket.relatedTicketIds?.includes(event.ticket_id ?? "") === true ||
        (event.event_type === "ticket.created" && payloadId(event.payload, "parent_ticket_id") === currentTicket.id) ||
        (event.event_type.startsWith("dependency.") && payloadId(event.payload, "blocker_ticket_id") === currentTicket.id)
      )
  }
  const projectMatch = /^\/projects\/([^/]+)(?:\/canceled)?$/.exec(pathname)
  if (!projectMatch) return false
  const key = decodeURIComponent(projectMatch[1]!)
  return projects.some((project) => project.key === key && project.id === event.project_id)
}

export const createRevalidationCoalescer = <Timer,>(revalidate: () => void, schedule: (work: () => void) => Timer, cancel: (timer: Timer) => void) => {
  let timer: Timer | undefined
  let disposed = false
  return {
    request: () => {
      if (disposed || timer !== undefined) return
      timer = schedule(() => { timer = undefined; if (!disposed) revalidate() })
    },
    dispose: () => {
      if (disposed) return
      disposed = true
      if (timer !== undefined) cancel(timer)
      timer = undefined
    },
  }
}
