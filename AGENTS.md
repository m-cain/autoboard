# Agent guidance

- After changing Prettier-owned JS, TS, TSX, MJS, JSON, CSS, Markdown, YAML, HTML, or SVG source, configuration, or test files, run `corepack pnpm format:prettier`.
- After changing `server/**/*.{ex,exs}`, including migrations, run `corepack pnpm format:mix`.
- Before commits and handoff, run `corepack pnpm format:check` and relevant tests.
- Never use `--no-verify` or `HUSKY=0` unless the user explicitly directs it.
- Preserve unrelated existing working-tree changes.
