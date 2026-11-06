import { defineConfig, devices } from "@playwright/test";

// The binary is built by `make e2e` before this runs; each test starts its own
// copy through the fixture in tests/foreman.ts, and nothing builds or downloads.
export default defineConfig({
  testDir: "tests",
  forbidOnly: true,
  retries: 0,
  reporter: "list",
  // SPEC 22.2: the browser's language picks Foreman's, so every project asks
  // for English and no test depends on the machine it runs on.
  use: { locale: "en-US" },
  // DESIGN 10: every screen is checked in both themes, at desktop and at
  // 320 CSS pixels (SPEC 17.2).
  projects: [
    { name: "desktop-dark", use: { ...devices["Desktop Chrome"], colorScheme: "dark" } },
    { name: "desktop-light", use: { ...devices["Desktop Chrome"], colorScheme: "light" } },
    { name: "phone-dark", use: { ...devices["Desktop Chrome"], viewport: { width: 320, height: 640 }, colorScheme: "dark" } },
    { name: "phone-light", use: { ...devices["Desktop Chrome"], viewport: { width: 320, height: 640 }, colorScheme: "light" } },
  ],
});
