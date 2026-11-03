import { defineConfig, devices } from "@playwright/test";

const port = 18080;

// The binary is built by `make e2e` before this runs; tests never build or download anything.
export default defineConfig({
  testDir: "tests",
  forbidOnly: true,
  retries: 0,
  reporter: "list",
  use: { baseURL: `http://127.0.0.1:${port}` },
  projects: [
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
    // SPEC 17.2: every page works at 320 CSS pixels wide.
    { name: "phone", use: { ...devices["Desktop Chrome"], viewport: { width: 320, height: 640 } } },
  ],
  webServer: {
    command: "../bin/foreman serve",
    env: { FOREMAN_LISTEN: `127.0.0.1:${port}` },
    url: `http://127.0.0.1:${port}/healthz`,
    reuseExistingServer: false,
  },
});
