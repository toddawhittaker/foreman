import { test, expect, checkPage, createAdmin, seedWorld } from "./foreman.js";

// The Mods tab and its pages (SPEC 9, 17). These run with no mod portal at
// all: the e2e environment points HTTPS_PROXY at a closed port, so every
// portal answer is "unreachable". The flows that need the portal's fake are
// in the browser tests built on it.

test.describe("mods tab", () => {
  test.use({ fakeGameOn: true });

  // The projects run this in both themes, at desktop and phone width.
  test("reads as a document and passes the checks", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman, { mods: { "a-mod-with-a-rather-long-name-for-a-phone": "2.1", other: "2.1" } });
    await page.goto("/worlds/base/mods");
    await expect(page.getByRole("heading", { level: 1, name: "Base" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Mods", exact: true })).toHaveAttribute("aria-current", "page");
    const table = page.getByRole("table", { name: /Mods of Base/ });
    await expect(table.getByRole("row", { name: /a-mod-with-a-rather-long-name/ })).toBeVisible();
    // Built-in mods are one line above the table, not rows, with the way to change them (SPEC 9.7).
    await expect(table.getByRole("row", { name: /\bbase\b/ })).toHaveCount(0);
    await expect(page.locator("#mods-built-in")).toContainText("Built in, version 2.1.24: base");
    // Issue 372: it says which are off, and why, with the way to turn them on.
    await expect(page.locator("#mods-built-in")).toContainText("Space Age is off, so elevated-rails, quality, space-age are off.");
    await expect(page.getByRole("link", { name: "Turn on Space Age", exact: true })).toHaveAttribute("href", "/worlds/base/settings");
    // Each row's buttons name their mod for a screen reader.
    await expect(table.getByRole("link", { name: "Disable other" })).toBeVisible();
    await expect(table.getByRole("link", { name: "Remove other" })).toBeVisible();
    // The portal cannot be reached here, and the tab says so in words, in a live region.
    await expect(page.locator("#mod-status")).toHaveAttribute("role", "status");
    await expect(page.locator("#mod-status")).toContainText("could not reach the mod portal");
    // Issue 373: the save's mods are read, so the tab says they differ before any click.
    await expect(page.locator("#sync-state")).toContainText("The mods differ from the save base.zip:");
    await expect(page.getByRole("link", { name: "Match mods to the save" })).toBeVisible();
    await checkPage(page);
    await page.screenshot({ path: `../screenshots/mods-tab-${test.info().project.name}.png`, fullPage: true });
  });

  test("is one click from the Server page", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman, { mods: { other: "2.1" } });
    await page.goto("/");
    await page.locator("#act-mods").click();
    await expect(page).toHaveURL("/worlds/base/mods");
  });

  test("a plan the portal cannot work out says so and changes nothing", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman, { mods: { other: "2.1" } });
    await page.goto("/worlds/base/mods/plan?kind=install&name=jetpack");
    await expect(page.getByRole("heading", { level: 1, name: "Install jetpack in Base?" })).toBeVisible();
    await expect(page.locator("#plan-error")).toContainText("could not reach the mod portal");
    await expect(page.getByRole("link", { name: "Try again" })).toBeVisible();
    await expect(page.getByRole("button", { name: /Install/ })).toHaveCount(0);
    await checkPage(page);
  });

  test("search says the portal cannot be reached", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/worlds/base/mods");
    await page.getByLabel("Mod name").fill("jetpack");
    await page.getByRole("button", { name: "Search" }).click();
    await expect(page).toHaveURL(/\/worlds\/base\/mods\/search\?q=jetpack/);
    await expect(page.getByText("could not reach the mod portal, so it could not search")).toBeVisible();
    await checkPage(page);
  });
});
