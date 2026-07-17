import { useCallback, useEffect, useRef } from "react"
import { Link, useNavigate } from "react-router"

const focusable = "a[href], [tabindex]:not([tabindex='-1'])"

export const TicketDrawer = ({ closeTo, children }: { readonly closeTo: string; readonly children: React.ReactNode }) => {
  const navigate = useNavigate()
  const dialog = useRef<HTMLElement>(null)
  const close = useCallback(() => navigate(closeTo), [closeTo, navigate])

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
    return () => { document.body.style.overflow = bodyOverflow; document.removeEventListener("keydown", onKeyDown) }
  }, [close])

  return <div className="ticket-drawer-backdrop" onMouseDown={(event) => { if (event.currentTarget === event.target) close() }}>
    <aside ref={dialog} className="ticket-drawer" role="dialog" aria-modal="true" aria-label="Ticket detail">
      <Link className="ticket-drawer__close" to={closeTo}>Back to board</Link>
      {children}
    </aside>
  </div>
}
