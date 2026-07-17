import { useCallback, useEffect, useRef } from "react"
import { Link, useNavigate } from "react-router"

const focusable = "a[href], [tabindex]:not([tabindex='-1'])"

export const TicketDrawer = ({ closeTo, originIdentifier, children }: { readonly closeTo: string; readonly originIdentifier?: string; readonly children: React.ReactNode }) => {
  const navigate = useNavigate()
  const dialog = useRef<HTMLElement>(null)
  const closing = useRef(false)
  const close = useCallback(() => { closing.current = true; navigate(-1) }, [navigate])

  useEffect(() => {
    const bodyOverflow = document.body.style.overflow
    document.body.style.overflow = "hidden"
    const initialFocus = dialog.current?.querySelector<HTMLElement>(focusable)
    initialFocus?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); close(); return }
      if (event.key !== "Tab") return
      const nodes = [...(dialog.current?.querySelectorAll<HTMLElement>(focusable) ?? [])]
      if (nodes.length === 0) return
      const first = nodes[0]!
      const last = nodes[nodes.length - 1]!
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener("keydown", onKeyDown)
    const onPopState = () => { closing.current = true }
    window.addEventListener("popstate", onPopState)
    return () => {
      document.body.style.overflow = bodyOverflow
      document.removeEventListener("keydown", onKeyDown)
      window.removeEventListener("popstate", onPopState)
      if (closing.current && originIdentifier) window.setTimeout(() => document.querySelector<HTMLElement>(`[data-ticket-identifier="${originIdentifier}"]`)?.focus(), 0)
    }
  }, [close, originIdentifier])

  return <div className="ticket-drawer-backdrop" onMouseDown={(event) => { if (event.currentTarget === event.target) close() }}>
    <aside ref={dialog} className="ticket-drawer" role="dialog" aria-modal="true" aria-label="Ticket detail">
      <Link className="ticket-drawer__close" to={closeTo} onClick={(event) => { event.preventDefault(); close() }}>Back to board</Link>
      {children}
    </aside>
  </div>
}
