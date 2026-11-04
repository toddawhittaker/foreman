import { defineConfig, devices } from "@playwright/test";

// The phone scenario against the real game (make e2e-game). It is not part of
// `make e2e`: it needs the host's Docker socket and the pinned game image.
export default defineConfig({
  testDir: ".",
  forbidOnly: true,
  retries: 0,
  reporter: "list",
  // The game takes a while to load and to save.
  timeout: 8 * 60_000,
  // One theme is enough here; the fake-game run covers both.
  projects: [{ name: "phone", use: { ...devices["Desktop Chrome"], viewport: { width: 320, height: 640 }, colorScheme: "dark" } }],
});
