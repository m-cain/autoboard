import type { ActivityEvent, Project } from "@autoboard/contracts"

export const isActivityRelevant = (event: ActivityEvent, pathname: string, projects: readonly Project[], currentTicket?: { readonly id: string; readonly project_id: string }): boolean => {
  if (pathname === "/triage") return projects.some((project) => project.id === event.project_id)
  if (/^\/tickets\/[^/]+$/.test(pathname)) return currentTicket !== undefined && (event.ticket_id === currentTicket.id || event.project_id === currentTicket.project_id)
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
