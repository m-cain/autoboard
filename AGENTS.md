# Agent guidance

- Treat functionality changes as cross-surface changes: in the same change, update every affected implementation, MCP tool definition, schema, description, generated or shared contract, Codex plugin, skill, or agent integration, installer or lifecycle path, test, and user, architecture, or operations document. Do not defer known synchronization work to follow-up changes; untouched surfaces must be genuinely unaffected.
- After changing Prettier-owned JS, TS, TSX, MJS, JSON, CSS, Markdown, YAML, HTML, or SVG source, configuration, or test files, run `corepack pnpm format:prettier`.
- After changing Go source or tests, run `gofmt`, relevant Go tests, and `just lint-go`.
- Go coverage must remain at least 80% overall and at least 70% in every first-party package.
- TypeScript coverage is enforced independently in `@autoboard/contracts` and `@autoboard/web`. Each workspace must remain at least 80% for lines, statements, and functions and at least 75% for branches.
- Include all first-party Go, TypeScript, and TSX production source in coverage. Generated contracts are the only exclusion.
- Tests must exercise observable behavior. Do not add coverage-only imports, assertions, or production branches.
- Do not lower a coverage threshold, broaden a coverage exclusion, disable a linter, or add a suppression without explicit user approval. Any approved lint exception must be narrow and documented; inline `//nolint` directives require a specific linter and explanation.
- Use `just coverage-go`, `just coverage-typescript`, and `just coverage` to measure the policy. Use the repository-pinned `.tools/bin/golangci-lint` through `just lint-go`; do not substitute a global binary.
- Before commits, run `just pre-commit`. Before handoff, run `corepack pnpm format:check`, relevant tests, and `just verify`.
- Never use `--no-verify` or `HUSKY=0` unless the user explicitly directs it.
- Preserve unrelated existing working-tree changes.
