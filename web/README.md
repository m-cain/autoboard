# Autoboard browser

This React/Vite application is a read-only view over the local Go daemon.
Mutations belong exclusively to the daemon's MCP tools.

From the repository root, use `just dev` for the daemon plus hot reload,
`just dev-web` for Vite alone, and `just test-web` for browser unit tests.
Production assets are embedded into the Go binary by `just build`.
