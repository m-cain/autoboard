// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, useLocation } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"
import { TicketDrawer } from "./TicketDrawer.js"

const CurrentPath = () => <p data-testid="path">{useLocation().pathname}</p>

afterEach(cleanup)

describe("TicketDrawer", () => {
  it("traps initial focus, closes on Escape by navigation, and restores page scrolling", async () => {
    document.body.style.overflow = "auto"
    const { unmount } = render(<MemoryRouter initialEntries={["/projects/AUTO", "/tickets/AUTO-1"]} initialIndex={1}><TicketDrawer closeTo="/projects/AUTO"><a href="#detail">Detail link</a></TicketDrawer><CurrentPath /></MemoryRouter>)

    const close = screen.getByRole("link", { name: "Back to board" })
    expect(close).toHaveFocus()
    expect(document.body.style.overflow).toBe("hidden")
    fireEvent.keyDown(document, { key: "Escape" })
    await vi.waitFor(() => expect(screen.getByTestId("path")).toHaveTextContent("/projects/AUTO"))
    unmount()
    expect(document.body.style.overflow).toBe("auto")
  })
})
