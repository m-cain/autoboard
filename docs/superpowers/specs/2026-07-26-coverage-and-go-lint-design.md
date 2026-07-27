# Coverage and Go Lint Quality Gates

## Decision

Autoboard will enforce coverage and Go linting as required local quality gates.
The policy applies equally to humans and LLM agents.

The selected approach combines repository-wide thresholds with a minimum for
each Go package. A global-only threshold would allow well-tested packages to
hide untested commands, while per-file TypeScript thresholds would over-reward
small implementation details and make harmless refactors unnecessarily
expensive. Diff-only coverage is not authoritative in detached worktrees and
would not repair the current baseline.

## Coverage policy

Go coverage must meet both of these requirements:

- At least 80% statement coverage across all first-party Go packages.
- At least 70% statement coverage in every first-party Go package.

TypeScript coverage is enforced independently in `@autoboard/contracts` and
`@autoboard/web`. Each workspace must meet:

- At least 80% line coverage.
- At least 80% statement coverage.
- At least 80% function coverage.
- At least 75% branch coverage.

All first-party Go, TypeScript, and TSX source is included. Generated contracts
are excluded because their generator and consumer behavior are tested instead.
Tests must exercise observable behavior; importing modules solely to increase
coverage is not acceptable. Thresholds and exclusions are policy, not tuning
knobs: agents must not lower or broaden them without explicit user approval.

Coverage output is written only to ignored temporary or coverage directories.
The canonical commands are `just coverage-go`, `just coverage-typescript`, and
`just coverage`.

## Go lint policy

The repository pins golangci-lint v2.12.2 and uses a version 2 configuration.
The enabled profile covers compiler/vet analysis, static analysis, error
handling, resource handling, SQL and HTTP correctness, security, context use,
modernization, Unicode safety, logging, test hygiene, and suppression hygiene.
It deliberately does not enable subjective layout, line-length, naming-length,
or blanket complexity linters.

Exceptions must be narrow, documented in `.golangci.yml`, and validated as
used. Inline `//nolint` directives require a specific linter and explanation.
Generated code is recognized only through the standard strict generated-file
marker.

The repository-owned installer places the exact binary under `.tools/bin`.
`just dependencies` installs it, `just lint-go` verifies the configuration and
runs it, and no unpinned globally installed binary is used.

## Workflow enforcement

Husky invokes `just pre-commit`. That gate runs formatting checks, generated
contract freshness, TypeScript checks and browser lint, golangci-lint, unit
tests, and both coverage policies. Race tests, production builds, and black-box
E2E remain in `just verify`.

`AGENTS.md` states the exact coverage goals, commands, and prohibition against
weakening gates. The README documents the same commands for developers.

## Verification

The implementation is complete only when all of the following pass from the
current worktree:

1. `just coverage-go`
2. `just coverage-typescript`
3. `just lint-go`
4. `.husky/pre-commit`
5. `just verify`

The final report must include measured Go overall and per-package coverage,
both TypeScript workspace summaries, the golangci-lint version, and confirmation
that the hook uses the canonical gate.
