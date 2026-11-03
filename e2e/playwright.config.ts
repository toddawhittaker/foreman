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
    // A fresh data directory per run. Foreman shares a user ID with the "game"
    // (SPEC 3.6), and adopted mode skips the Docker lookup this container cannot do.
    command:
      "FOREMAN_DATA_DIR=$(mktemp -d) FOREMAN_GAME_UID=$(id -u) FOREMAN_GAME_GID=$(id -g) exec ../bin/foreman serve",
    env: { FOREMAN_LISTEN: `127.0.0.1:${port}`, FOREMAN_MODE: "adopted" },
    url: `http://127.0.0.1:${port}/healthz`,
    reuseExistingServer: false,
  },
});
