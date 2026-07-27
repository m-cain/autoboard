# Coverage and Go Lint Quality Gates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enforce the approved Go and TypeScript coverage policy and a pinned,
comprehensive golangci-lint profile in local workflows, Husky, and agent
instructions.

**Architecture:** Repository-owned scripts install the exact linter and verify
Go coverage without relying on global tools. Vitest V8 coverage is configured
per TypeScript workspace. Just recipes are the canonical interface consumed by
Husky and the full verification suite.

**Tech Stack:** Go 1.25.7 coverage, golangci-lint v2.12.2 configuration v2,
Vitest 4 V8 coverage, pnpm 11, Just, Husky 9, Node.js test runner.

## Global Constraints

- Go coverage is at least 80% overall and 70% in every first-party package.
- Each TypeScript workspace is at least 80% lines, statements, and functions,
  and at least 75% branches.
- Generated contracts are the only coverage exclusion.
- Tests exercise observable behavior; coverage-only imports are forbidden.
- Thresholds and exclusions cannot be weakened without explicit user approval.
- golangci-lint is pinned to v2.12.2 and runs from `.tools/bin`.
- Existing unrelated worktree changes must be preserved.

---

### Task 1: Make the quality-gate workflow executable

**Files:**

- Create: `.golangci-version`
- Create: `.golangci.yml`
- Create: `scripts/install-golangci-lint.sh`
- Create: `scripts/check-go-coverage.mjs`
- Modify: `test/developer-workflow.test.mjs`
- Modify: `package.json`
- Modify: `justfile`
- Modify: `.husky/pre-commit`
- Modify: `.gitignore`

**Interfaces:**

- Produces: `just lint-go`, `just coverage-go`,
  `just coverage-typescript`, `just coverage`, and `just pre-commit`.
- Produces: a Go coverage checker enforcing 80% overall and 70% per package.

- [ ] **Step 1: Add failing workflow tests**

Extend `test/developer-workflow.test.mjs` to require all five recipes, assert
that `verify` invokes lint and coverage, and assert that `.husky/pre-commit`
executes `just pre-commit`.

- [ ] **Step 2: Verify the workflow tests fail**

Run `node --test test/developer-workflow.test.mjs`.
Expected: failure because the recipes and hook command do not exist.

- [ ] **Step 3: Add the pinned tool and canonical recipes**

Pin `v2.12.2`, install it into `.tools/bin`, add the version 2 linter
configuration, and implement the five Just recipes. The Go coverage checker
must run a fresh combined `-coverpkg=./...` profile, calculate statement
coverage for every package and the module, fail for missing package data, and
print a deterministic summary.

- [ ] **Step 4: Verify workflow tests pass**

Run `node --test test/developer-workflow.test.mjs`.
Expected: all workflow tests pass.

### Task 2: Configure TypeScript coverage

**Files:**

- Create: `packages/contracts/vitest.config.ts`
- Modify: `packages/contracts/package.json`
- Modify: `web/vite.config.ts`
- Modify: `web/package.json`
- Modify: `pnpm-lock.yaml`
- Test: `packages/contracts/test/browser-generated.test.ts`
- Test: `web/src/**/*.test.ts`
- Test: `web/src/**/*.test.tsx`

**Interfaces:**

- Produces: `pnpm coverage` in both TypeScript workspaces.
- Consumes: the exact 80/80/80/75 thresholds from the design.

- [ ] **Step 1: Install the matching V8 provider and add threshold configs**

Add `@vitest/coverage-v8` at the resolved Vitest 4.1.10 version. Include every
`src/**/*.ts` and `src/**/*.tsx` file and exclude only generated contracts.

- [ ] **Step 2: Run both coverage commands and record the failing metrics**

Run `corepack pnpm --filter @autoboard/contracts coverage` and
`corepack pnpm --filter @autoboard/web coverage`.
Expected: a threshold failure for every metric below policy.

- [ ] **Step 3: Add behavior-focused tests for uncovered paths**

Add tests that invoke public exports, browser validation behavior, routes,
pages, components, API failures, and runtime initialization. Do not add
coverage-only imports or exclude ordinary source.

- [ ] **Step 4: Verify TypeScript coverage passes**

Run `just coverage-typescript`.
Expected: both workspace summaries meet 80% lines/statements/functions and 75%
branches.

### Task 3: Raise Go coverage to policy

**Files:**

- Test: `cmd/autoboard/**/*_test.go`
- Test: `cmd/autoboard-dev/**/*_test.go`
- Test: `cmd/autoboard-embed/**/*_test.go`
- Test: `cmd/autoboard-schema/**/*_test.go`
- Test: `internal/**/*_test.go`
- Modify: focused Go implementation files only when needed to expose
  dependency-injected behavioral seams.

**Interfaces:**

- Consumes: `scripts/check-go-coverage.mjs`.
- Produces: at least 80% total statement coverage and 70% for every package.

- [ ] **Step 1: Run the Go coverage gate and record every deficit**

Run `just coverage-go`.
Expected: failure showing the overall baseline and every package below 70%.

- [ ] **Step 2: Add one failing behavioral test at a time**

Prioritize lifecycle verification, command argument handling, contract
write/check behavior, developer supervisor shutdown, attachment/media errors,
HTTP error paths, and launchd command failures. For any required production
seam, first write the test that cannot pass without it.

- [ ] **Step 3: Verify each new test before continuing**

Run the narrow `go test` package command after every test, then rerun
`just coverage-go` after each package crosses 70%.

- [ ] **Step 4: Verify the complete Go policy**

Run `just coverage-go` and `go test -race ./...`.
Expected: every package is at least 70%, total is at least 80%, and race tests
pass.

### Task 4: Remediate the full Go lint profile

**Files:**

- Modify: `.golangci.yml`
- Modify: Go source and tests reported by the approved linter set.

**Interfaces:**

- Consumes: `.tools/bin/golangci-lint` v2.12.2.
- Produces: a clean `just lint-go` run without unexplained suppressions.

- [ ] **Step 1: Validate configuration and capture lint failures**

Run `.tools/bin/golangci-lint config verify` and `just lint-go`.
Expected: configuration succeeds and lint reports existing source issues.

- [ ] **Step 2: Fix findings by category**

Resolve correctness and security findings first, then resource/error handling,
static analysis, modernization, and style. Add a regression test before every
behavioral production change. Use a narrow documented exclusion only when the
rule does not apply to this codebase.

- [ ] **Step 3: Verify lint and tests**

Run `just lint-go`, `go test ./...`, and `go test -race ./...`.
Expected: all commands pass with no linter warnings.

### Task 5: Establish the agent contract and finish integration

**Files:**

- Modify: `AGENTS.md`
- Modify: `README.md`
- Modify: `.prettierignore` only if generated coverage output requires it.

**Interfaces:**

- Documents: exact thresholds, canonical commands, test-quality rule, and
  approval requirement for policy changes.
- Consumes: all quality recipes from Tasks 1 through 4.

- [ ] **Step 1: Document the policy**

Add the approved goals and commands to `AGENTS.md` and the developer workflow
section of `README.md`.

- [ ] **Step 2: Format all owned files**

Run `corepack pnpm format:prettier`, `just --unstable --fmt`, and
`corepack pnpm format:go`.

- [ ] **Step 3: Run the hook exactly as Git will**

Run `.husky/pre-commit`.
Expected: formatting, lint, unit tests, and coverage all pass.

- [ ] **Step 4: Run the complete handoff suite**

Run `just verify`.
Expected: formatting, contracts, type checks, browser lint, golangci-lint,
unit tests, coverage, race tests, build, E2E, and diff checks all pass.

- [ ] **Step 5: Audit the requirements**

Read the design and this plan line by line. Record the exact Go package
coverage, both TypeScript workspace summaries, golangci-lint version, hook
command, and verification exit statuses before declaring completion.
