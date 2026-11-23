import { defineConfig, devices } from "@playwright/test";

// The binary is built by `make e2e` before this runs; each test starts its own
// copy through the fixture in tests/foreman.ts, and nothing builds or downloads.
const looks = /@looks/;

export default defineConfig({
  testDir: "tests",
  forbidOnly: true,
  // Each test starts its own Foreman (tests/foreman.ts), so tests need no order,
  // and CI's slices split by test rather than by whole file.
  fullyParallel: true,
  retries: 0,
  // Eight workers locally, so two runs at once fit a 32-core machine; each
  // worker starts its own Foreman and browser. CI runs three on its 4-core
  // runners: the tests mostly wait, but four starved the axe scans of CPU. The Makefile passes CI into the container.
  workers: process.env.CI ? 3 : 8,
  reporter: "list",
  // SPEC 22.2: the browser's language picks Foreman's, so every project asks
  // for English and no test depends on the machine it runs on.
  use: {
    locale: "en-US",
    // A failing test keeps its trace in e2e/test-results (CI uploads that folder when a shard fails);
    // a passing one drops it. A trace holds the page's DOM, every request and reply with headers and
    // bodies, and the values the test typed into forms, so it is for failure diagnosis only and its
    // artifact is kept briefly. Sources are left out so a trace carries no test code.
    trace: { mode: "retain-on-failure", screenshots: true, snapshots: true, sources: false },
  },
  // DESIGN 10 and SPEC 17.2: how a page looks and reads is checked in both
  // themes at desktop and 320 px; behavior is checked once, in desktop-dark.
  // A test tagged @looks runs in all four projects, any other only in the first.
  projects: [
    { name: "desktop-dark", use: { ...devices["Desktop Chrome"], colorScheme: "dark" } },
    { name: "desktop-light", grep: looks, use: { ...devices["Desktop Chrome"], colorScheme: "light" } },
    { name: "phone-dark", grep: looks, use: { ...devices["Desktop Chrome"], viewport: { width: 320, height: 640 }, colorScheme: "dark" } },
    { name: "phone-light", grep: looks, use: { ...devices["Desktop Chrome"], viewport: { width: 320, height: 640 }, colorScheme: "light" } },
  ],
});
