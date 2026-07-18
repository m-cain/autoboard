import { useCallback, useEffect, useRef } from "react";
import { Link, useNavigate } from "react-router";
import { prepareBoardSnapshotRestore } from "../boardState.js";

const focusable = "a[href], [tabindex]:not([tabindex='-1'])";

export const TicketDrawer = ({
  closeTo,
  originIdentifier,
  drawerDepth,
  focusKey,
  boardSnapshotKey,
  children,
}: {
  readonly closeTo: string;
  readonly originIdentifier?: string;
  readonly drawerDepth: number;
  readonly focusKey: string;
  readonly boardSnapshotKey?: string;
  readonly children: React.ReactNode;
}) => {
  const navigate = useNavigate();
  const dialog = useRef<HTMLElement>(null);
  const closeAllIntent = useRef(false);
  const browserBackIntent = useRef(false);
  const depth = useRef(drawerDepth);
  const origin = useRef(originIdentifier);
  depth.current = drawerDepth;
  origin.current = originIdentifier;
  const snapshotKey = useRef(boardSnapshotKey);
  snapshotKey.current = boardSnapshotKey;
  const close = useCallback(() => {
    closeAllIntent.current = true;
    prepareBoardSnapshotRestore(snapshotKey.current);
    navigate(-depth.current);
  }, [navigate]);

  useEffect(() => {
    const bodyOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const initialFocus = dialog.current?.querySelector<HTMLElement>(focusable);
    initialFocus?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        close();
        return;
      }
      if (event.key !== "Tab") return;
      const nodes = [
        ...(dialog.current?.querySelectorAll<HTMLElement>(focusable) ?? []),
      ];
      if (nodes.length === 0) return;
      const first = nodes[0]!;
      const last = nodes[nodes.length - 1]!;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    const onPopState = () => {
      browserBackIntent.current = true;
      if (depth.current === 1) prepareBoardSnapshotRestore(snapshotKey.current);
    };
    window.addEventListener("popstate", onPopState);
    return () => {
      document.body.style.overflow = bodyOverflow;
      document.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("popstate", onPopState);
      if (
        (closeAllIntent.current || browserBackIntent.current) &&
        origin.current
      )
        window.setTimeout(
          () =>
            document
              .querySelector<HTMLElement>(
                `[data-ticket-identifier="${origin.current}"]`,
              )
              ?.focus(),
          0,
        );
    };
  }, [close]);

  useEffect(() => {
    if (browserBackIntent.current) browserBackIntent.current = false;
    if (dialog.current && !dialog.current.contains(document.activeElement))
      dialog.current.querySelector<HTMLElement>(focusable)?.focus();
  }, [focusKey]);

  return (
    <div
      className="ticket-drawer-backdrop"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) close();
      }}
    >
      <aside
        ref={dialog}
        className="ticket-drawer"
        role="dialog"
        aria-modal="true"
        aria-label="Ticket detail"
      >
        <Link
          className="ticket-drawer__close"
          to={closeTo}
          onClick={(event) => {
            event.preventDefault();
            close();
          }}
        >
          Back to board
        </Link>
        {children}
      </aside>
    </div>
  );
};
