import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const PACKAGE_THRESHOLD = 70;
export const OVERALL_THRESHOLD = 80;

export const effectiveGoCommand = (goRootOutput, platform) => {
  const goRoot = goRootOutput.trim();
  const separator = platform === "win32" ? "\\" : "/";
  const executable = platform === "win32" ? "go.exe" : "go";
  return `${goRoot}${separator}bin${separator}${executable}`;
};

export const effectiveGoEnvironment = (environment, goCommand, platform) => {
  const pathSeparator = platform === "win32" ? ";" : ":";
  const slashIndex = Math.max(
    goCommand.lastIndexOf("/"),
    goCommand.lastIndexOf("\\"),
  );
  const binaryDirectory = goCommand.slice(0, slashIndex);
  const currentPath = environment.PATH ?? "";
  return {
    ...environment,
    PATH: currentPath
      ? `${binaryDirectory}${pathSeparator}${currentPath}`
      : binaryDirectory,
    GOTOOLCHAIN: "local",
  };
};

export const parsePackageCoverage = (output) => {
  const coverage = new Map();
  const pattern =
    /^(?:(?:ok|\?)\s+|[ \t]+)(\S+).*coverage:\s+([0-9]+(?:\.[0-9]+)?)%\s+of statements$/gm;
  for (const match of output.matchAll(pattern)) {
    coverage.set(match[1], Number(match[2]));
  }
  return coverage;
};

const percentage = (covered, total) =>
  total === 0 ? 0 : (covered / total) * 100;

export const calculateProfileCoverage = (profile, expectedPackages) => {
  const packageNames = [...expectedPackages].sort();
  const packagesBySpecificity = [...packageNames].sort(
    (left, right) => right.length - left.length,
  );
  const blocks = new Map();

  for (const line of profile.split(/\r?\n/)) {
    if (!line || line.startsWith("mode:")) {
      continue;
    }
    const match = line.match(/^(.+):\d+\.\d+,\d+\.\d+\s+(\d+)\s+(\d+)$/);
    if (!match) {
      throw new Error(`Invalid Go coverage profile line: ${line}`);
    }
    const [, file, statementCountText, executionCountText] = match;
    const packageName = packagesBySpecificity.find((candidate) =>
      file.startsWith(`${candidate}/`),
    );
    if (!packageName) {
      continue;
    }
    const statementCount = Number(statementCountText);
    const covered = Number(executionCountText) > 0;
    const blockKey = `${packageName}\0${file}\0${line.slice(file.length + 1, line.lastIndexOf(" "))}`;
    const existing = blocks.get(blockKey);
    blocks.set(blockKey, {
      packageName,
      statementCount,
      covered: covered || (existing?.covered ?? false),
    });
  }

  const totals = new Map(
    packageNames.map((packageName) => [packageName, { covered: 0, total: 0 }]),
  );
  for (const block of blocks.values()) {
    const total = totals.get(block.packageName);
    total.total += block.statementCount;
    if (block.covered) {
      total.covered += block.statementCount;
    }
  }

  let overallCovered = 0;
  let overallTotal = 0;
  const packageCoverage = new Map();
  for (const [packageName, total] of totals) {
    packageCoverage.set(packageName, percentage(total.covered, total.total));
    overallCovered += total.covered;
    overallTotal += total.total;
  }
  return {
    packageCoverage,
    overallCoverage: percentage(overallCovered, overallTotal),
  };
};

export const evaluateCoverage = ({
  expectedPackages,
  packageCoverage,
  overallCoverage,
  packageThreshold,
  overallThreshold,
}) => {
  const failures = [];
  for (const packageName of [...expectedPackages].sort()) {
    const percentage = packageCoverage.get(packageName);
    if (percentage === undefined) {
      failures.push(`${packageName}: coverage result is missing`);
    } else if (percentage < packageThreshold) {
      failures.push(
        `${packageName}: ${percentage.toFixed(1)}% is below the ${packageThreshold.toFixed(1)}% package threshold`,
      );
    }
  }
  if (overallCoverage < overallThreshold) {
    failures.push(
      `total: ${overallCoverage.toFixed(1)}% is below the ${overallThreshold.toFixed(1)}% overall threshold`,
    );
  }
  return { failures };
};

const run = (command, commandArguments, environment = process.env) => {
  const result = spawnSync(command, commandArguments, {
    cwd: resolve(import.meta.dirname, ".."),
    encoding: "utf8",
    env: environment,
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.error) {
    throw result.error;
  }
  return result;
};

const main = async () => {
  const temporaryDirectory = await mkdtemp(
    join(tmpdir(), "autoboard-go-coverage."),
  );
  const profile = join(temporaryDirectory, "coverage.out");
  try {
    const environmentResult = run("go", ["env", "GOROOT"]);
    if (environmentResult.status !== 0) {
      process.stderr.write(
        `${environmentResult.stdout}${environmentResult.stderr}`,
      );
      return environmentResult.status ?? 1;
    }
    const goCommand = effectiveGoCommand(
      environmentResult.stdout,
      process.platform,
    );
    const goEnvironment = effectiveGoEnvironment(
      process.env,
      goCommand,
      process.platform,
    );
    const listResult = run(
      goCommand,
      ["list", "-f", "{{.ImportPath}}", "./..."],
      goEnvironment,
    );
    if (listResult.status !== 0) {
      process.stderr.write(`${listResult.stdout}${listResult.stderr}`);
      return 1;
    }
    const expectedPackages = listResult.stdout
      .trim()
      .split("\n")
      .filter(Boolean);
    const testResult = run(
      goCommand,
      [
        "test",
        "-count=1",
        "-covermode=atomic",
        "-coverpkg=./...",
        `-coverprofile=${profile}`,
        "./...",
      ],
      goEnvironment,
    );
    process.stdout.write(testResult.stdout);
    process.stderr.write(testResult.stderr);
    if (testResult.status !== 0) {
      return testResult.status ?? 1;
    }

    const profileCoverage = calculateProfileCoverage(
      await readFile(profile, "utf8"),
      expectedPackages,
    );
    const { packageCoverage, overallCoverage } = profileCoverage;
    const evaluation = evaluateCoverage({
      expectedPackages,
      packageCoverage,
      overallCoverage,
      packageThreshold: PACKAGE_THRESHOLD,
      overallThreshold: OVERALL_THRESHOLD,
    });

    process.stdout.write("\nGo coverage policy:\n");
    for (const packageName of [...expectedPackages].sort()) {
      const percentage = packageCoverage.get(packageName);
      process.stdout.write(
        `  ${packageName}: ${percentage === undefined ? "missing" : `${percentage.toFixed(1)}%`}\n`,
      );
    }
    process.stdout.write(`  total: ${overallCoverage.toFixed(1)}%\n`);
    if (evaluation.failures.length > 0) {
      process.stderr.write(
        `\nGo coverage gate failed:\n${evaluation.failures
          .map((failure) => `  - ${failure}`)
          .join("\n")}\n`,
      );
      return 1;
    }
    return 0;
  } finally {
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
};

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  process.exitCode = await main();
}
