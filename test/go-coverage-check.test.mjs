import assert from "node:assert/strict";
import { test } from "node:test";

import {
  calculateProfileCoverage,
  effectiveGoCommand,
  effectiveGoEnvironment,
  evaluateCoverage,
  parsePackageCoverage,
} from "../scripts/check-go-coverage.mjs";

test("uses the effective module toolchain instead of the PATH wrapper", () => {
  assert.equal(
    effectiveGoCommand("/toolchains/go1.25.7\n", "darwin"),
    "/toolchains/go1.25.7/bin/go",
  );
  assert.equal(
    effectiveGoCommand("C:\\toolchains\\go1.25.7\r\n", "win32"),
    "C:\\toolchains\\go1.25.7\\bin\\go.exe",
  );
});

test("calculates per-package and total statements from a combined profile", () => {
  const result = calculateProfileCoverage(
    `mode: atomic
example/a/a.go:1.1,2.1 2 1
example/a/a.go:4.1,7.1 3 0
example/b/b.go:1.1,5.1 4 2
`,
    ["example/a", "example/b"],
  );

  assert.deepEqual(
    [...result.packageCoverage],
    [
      ["example/a", 40],
      ["example/b", 100],
    ],
  );
  assert.ok(Math.abs(result.overallCoverage - 66.666_666_666_666_66) < 1e-9);
});

test("does not round a near-miss up to the threshold", () => {
  const coverage = calculateProfileCoverage(
    `mode: atomic
example/a/a.go:1.1,2.1 1599 1
example/a/a.go:4.1,7.1 401 0
`,
    ["example/a"],
  );
  const result = evaluateCoverage({
    expectedPackages: ["example/a"],
    packageCoverage: coverage.packageCoverage,
    overallCoverage: coverage.overallCoverage,
    packageThreshold: 80,
    overallThreshold: 80,
  });

  assert.equal(coverage.overallCoverage, 79.95);
  assert.equal(result.failures.length, 2);
});

test("keeps Go coverage subprocesses on the effective module toolchain", () => {
  assert.deepEqual(
    effectiveGoEnvironment(
      { PATH: "/usr/bin", GOTOOLCHAIN: "auto", OTHER: "value" },
      "/toolchains/go1.25.7/bin/go",
      "darwin",
    ),
    {
      PATH: "/toolchains/go1.25.7/bin:/usr/bin",
      GOTOOLCHAIN: "local",
      OTHER: "value",
    },
  );
});

test("parses package coverage from successful Go test output", () => {
  const coverage = parsePackageCoverage(`
ok  github.com/m-cain/autoboard/internal/app 0.4s coverage: 81.2% of statements
ok  github.com/m-cain/autoboard/internal/store 0.2s coverage: 70.0% of statements
    github.com/m-cain/autoboard/cmd/tool coverage: 0.0% of statements
`);

  assert.deepEqual(
    [...coverage],
    [
      ["github.com/m-cain/autoboard/internal/app", 81.2],
      ["github.com/m-cain/autoboard/internal/store", 70],
      ["github.com/m-cain/autoboard/cmd/tool", 0],
    ],
  );
});

test("reports missing and below-threshold packages plus the overall deficit", () => {
  const result = evaluateCoverage({
    expectedPackages: ["example/a", "example/b", "example/c"],
    packageCoverage: new Map([
      ["example/a", 70],
      ["example/b", 69.9],
    ]),
    overallCoverage: 79.9,
    packageThreshold: 70,
    overallThreshold: 80,
  });

  assert.deepEqual(result.failures, [
    "example/b: 69.9% is below the 70.0% package threshold",
    "example/c: coverage result is missing",
    "total: 79.9% is below the 80.0% overall threshold",
  ]);
});

test("accepts exact package and overall thresholds", () => {
  const result = evaluateCoverage({
    expectedPackages: ["example/a"],
    packageCoverage: new Map([["example/a", 70]]),
    overallCoverage: 80,
    packageThreshold: 70,
    overallThreshold: 80,
  });

  assert.deepEqual(result.failures, []);
});
