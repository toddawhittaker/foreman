import { test, expect, checkPage, createAdmin, adminPassword } from "./foreman.js";
import type { Page } from "@playwright/test";

async function signIn(page: Page) {
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill(adminPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
}

// SPEC 14.9: signing in returns to the page that was asked for.
test("sign-in returns to the requested page", async ({ page }) => {
  await createAdmin(page);
  await page.getByRole("button", { name: "Sign out" }).click();

  await page.goto("/worlds?from=test");
  await expect(page).toHaveURL("/signin?next=%2Fworlds%3Ffrom%3Dtest");
  await checkPage(page);

  // A wrong password keeps the page for the next try.
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill("not the right password");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.locator("#error-summary")).toBeFocused();

  await signIn(page);
  await expect(page).toHaveURL("/worlds?from=test");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Worlds");
});

// SPEC 14.9: a return path that is not a local path is ignored.
test("sign-in ignores a return path that leaves the site", async ({ page, foreman }) => {
  await createAdmin(page);
  for (const next of ["//evil.example/", "https://evil.example/", "/\\evil.example", "/signin"]) {
    await page.getByRole("button", { name: "Sign out" }).click();
    await page.goto("/signin?next=" + encodeURIComponent(next));
    await expect(page.locator('input[name="next"]')).toHaveCount(0);
    await signIn(page);
    await expect(page).toHaveURL("/");
    expect(new URL(page.url()).origin).toBe(foreman.url);
  }
});

// SPEC 12.3: setup reports every problem at once.
test("setup reports every field problem together", async ({ page }) => {
  await page.goto("/setup");
  await page.getByLabel("Password", { exact: true }).fill("short");
  await page.getByLabel("Confirm password").fill("other");
  await page.getByRole("button", { name: "Create admin account" }).click();
  const summary = page.locator("#error-summary");
  await expect(summary).toBeFocused();
  await expect(summary.getByRole("heading")).toHaveText(/Fix 3 problems to continue/);
  await expect(summary).toContainText("Enter a username");
  await expect(summary).toContainText("Use at least 12 characters");
  await expect(summary).toContainText("Enter the same password in both fields");
  await checkPage(page);
});
