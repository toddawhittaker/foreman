import { test, expect, createAdmin, adminPassword } from "./foreman.js";
import type { Page } from "@playwright/test";
import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";

// The ERROR log check in the foreman fixture (docs/WORKFLOW.md).
// The projects differ only in theme and width, which the log does not depend on,
// so one project is enough.
test.beforeEach(({}, testInfo) => {
  test.skip(testInfo.project.name !== "desktop-dark", "the log check does not depend on theme or width");
});

const declared = "stored password hash is unusable";

// Signing in against an unusable stored hash is an expected condition that
// Foreman logs as one ERROR line.
async function causeError(dataDir: string, page: Page) {
  await createAdmin(page);
  const db = new DatabaseSync(join(dataDir, "foreman.db"));
  try {
    db.prepare("UPDATE users SET password_hash = 'junk'").run();
  } finally {
    db.close();
  }
  await page.context().clearCookies();
  await page.goto("/signin");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(adminPassword);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.locator("#password-error, #username-error")).toBeVisible();
}

test("an ERROR line the test did not declare fails the test", async ({ foreman, page }) => {
  test.fail();
  await causeError(foreman.dataDir, page);
});

test("a declared string matches the ERROR line", async ({ foreman, page }) => {
  foreman.expectError(declared);
  await causeError(foreman.dataDir, page);
});

test("a declared regexp matches the ERROR line", async ({ foreman, page }) => {
  foreman.expectError(/"level":"ERROR".*hash is unusable/);
  await causeError(foreman.dataDir, page);
});

test("a declaration that matches no line fails the test", async ({ foreman, page }) => {
  test.fail();
  foreman.expectError("this line is never written");
  await page.goto("/setup");
});
