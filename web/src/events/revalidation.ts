import type { ActivityEvent, Project } from "@autoboard/contracts"

type CurrentTicket = { readonly id: string; readonly project_id: string; readonly drawer: boolean }

export const isActivityRelevant = (event: ActivityEvent, pathname: string, projects: readonly Project[], currentTicket?: CurrentTicket): boolean => {
  if (pathname === "/" || pathname === "/projects") return event.ticket_id === null
  if (pathname === "/triage") return projects.some((project) => project.id === event.project_id)
  if (/^\/tickets\/[^/]+$/.test(pathname)) {
    if (!currentTicket) return false
    return currentTicket.drawer
      ? event.project_id === currentTicket.project_id
      : event.ticket_id === currentTicket.id || (event.ticket_id === null && event.project_id === currentTicket.project_id)
  }
  const projectMatch = /^\/projects\/([^/]+)(?:\/canceled)?$/.exec(pathname)
  if (!projectMatch) return false
  const key = decodeURIComponent(projectMatch[1]!)
  return projects.some((project) => project.key === key && project.id === event.project_id)
}

export const createRevalidationCoalescer = <Timer,>(revalidate: () => void, schedule: (work: () => void) => Timer, cancel: (timer: Timer) => void) => {
  let timer: Timer | undefined
  return {
    request: () => {
      if (timer !== undefined) return
      timer = schedule(() => { timer = undefined; revalidate() })
    },
    dispose: () => {
      if (timer !== undefined) cancel(timer)
      timer = undefined
    },
  }
}
