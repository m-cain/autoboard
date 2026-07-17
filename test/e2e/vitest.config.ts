import { defineConfig } from "vitest/config"

export default defineConfig({
  test: {
    include: ["autoboard.test.ts"],
    testTimeout: 45_000,
    hookTimeout: 45_000,
    fileParallelism: false,
  },
})
