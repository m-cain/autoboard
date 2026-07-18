import { spawn } from "node:child_process";

const child = spawn(process.execPath, ["dist/main.js"], {
  env: {
    ...process.env,
    AUTOBOARD_SOCKET: undefined,
    AUTOBOARD_TOKEN: undefined,
  },
  stdio: ["ignore", "pipe", "pipe"],
});
let stdout = "";
let stderr = "";
child.stdout.setEncoding("utf8").on("data", (chunk) => {
  stdout += chunk;
});
child.stderr.setEncoding("utf8").on("data", (chunk) => {
  stderr += chunk;
});
const code = await new Promise((resolve, reject) =>
  child.once("error", reject).once("exit", resolve),
);

if (
  code !== 1 ||
  stdout !== "" ||
  !stderr.includes("AUTOBOARD_SOCKET and AUTOBOARD_TOKEN are required")
) {
  throw new Error(
    `entrypoint probe failed: code=${String(code)} stdout=${JSON.stringify(stdout)} stderr=${JSON.stringify(stderr)}`,
  );
}

console.error("Autoboard MCP entrypoint probe passed");
