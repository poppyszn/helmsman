import { defineConfig } from "@playwright/test";

// Two kinds of run:
//   dev     The whole product against the dev stack (fake GitHub, a real agent, demo apps).
//           `make e2e` runs it in Docker. Sign-in goes through the fake GitHub picker.
//   server  A read-mostly check of a real deployment (TLS, headers, sign-in redirect, and, when
//           break-glass credentials are supplied, the admin pages). `make e2e-server`.
const hostRules = process.env.E2E_HOST_RULES; // lets a container reach localhost:5173 and :8090

export default defineConfig({
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    launchOptions: hostRules ? { args: [`--host-resolver-rules=${hostRules}`] } : {},
  },
  projects: [
    {
      name: "setup",
      testDir: "./tests/dev",
      testMatch: /global\.setup\.ts/,
      use: { baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173" },
    },
    {
      name: "dev",
      testDir: "./tests/dev",
      testIgnore: /global\.setup\.ts/,
      dependencies: ["setup"],
      use: { baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173" },
    },
    {
      name: "server",
      testDir: "./tests/server",
      use: {
        baseURL: process.env.E2E_SERVER_URL ?? "https://helmsman.vasconsolutionsapp.com",
        // Only for a private CA (a pilot or a local rehearsal). Leave unset against a real certificate.
        ignoreHTTPSErrors: process.env.E2E_INSECURE_TLS === "true",
      },
    },
  ],
});
