import { test, expect, checkPage, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 9.1, 9.2, 17.10, #355, #345: a search with many results is a table
// shown ten at a time, with boxes to add several mods to one plan, on the
// Mods tab's search and under the plan's "Add another mod".
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });
const names = Array.from({ length: 25 }, (_, i) => `fp-pg-${String(i).padStart(2, "0")}`);

test.beforeEach(async ({ page, foreman, fakeGame, fakePortal }) => {
  for (const n of names) await fakePortal!.add(n, "1.0.0");
  await setUpModsWorld(page, foreman, fakeGame!, world);
});

test("25 results: a table a page at a time, ticks kept across pages, one plan for the ticked mods", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  // The world is the heading; the matches are a section that says how many there are in all (#345).
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^Base/);
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg"' })).toBeVisible();
  const table = page.getByRole("table", { name: "25 mods for Factorio 2.0 match. Showing 1 to 10." });
  await expect(table.getByRole("row")).toHaveCount(11);
  await expect(table.getByRole("columnheader")).toHaveText(["Mod", "Latest version", "Downloads"]);
  await expect(table.getByText("Game version")).toHaveCount(0);
  await expect(page.getByRole("navigation", { name: "Result pages" })).toContainText("Page 1 of 3");
  // The summary is there on demand.
  await expect(table.getByText("added by a test").first()).toBeHidden();
  await table.locator("summary").first().click();
  await expect(table.getByText("added by a test").first()).toBeVisible();
  await expect(table.getByRole("link", { name: "Mod portal for fp-pg-00, opens in a new tab" })).toBeVisible();
  await checkPage(page);
  await shot(page, "mods-search-25");

  // Tick one, page on with the keyboard, and the tick comes along.
  await table.getByRole("checkbox", { name: "fp-pg-01" }).check();
  await page.getByRole("button", { name: "Next page" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#results-heading")).toBeFocused();
  await expect(page.getByRole("table", { name: /Showing 11 to 20\./ })).toBeVisible();
  await expect(page.locator("#picks-elsewhere")).toHaveText("Also ticked on other pages: fp-pg-01.");
  await page.getByRole("checkbox", { name: "fp-pg-12" }).focus();
  await page.keyboard.press("Space");
  await checkPage(page);
  await page.getByRole("button", { name: "Add ticked mods to plan" }).click();

  // One plan with both, and the same page of the same search below it.
  await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeVisible();
  const asked = page.getByRole("list", { name: "Mods you asked for:" });
  await expect(asked).toContainText("fp-pg-01 1.0.0");
  await expect(asked).toContainText("fp-pg-12 1.0.0");
  await expect(page.getByRole("button", { name: "Install 2 mods" })).toBeVisible();
  const add = page.getByRole("region", { name: "Add another mod" });
  await expect(add.getByRole("table", { name: /Showing 11 to 20\./ })).toBeVisible();
  await expect(add.locator("#mod-fp-pg-12-add")).toHaveText("In plan fp-pg-12");
  // Each Add adds at once here, so there is no box to tick.
  await expect(add.getByRole("checkbox")).toHaveCount(0);
  await checkPage(page);
  await shot(page, "mods-plan-add-25");

  // A page link keeps the plan and puts focus on the results; Add redraws in place.
  await add.getByRole("link", { name: "Next page" }).click();
  await expect(page.locator("#add-heading")).toBeFocused();
  await expect(add.getByRole("table", { name: /Showing 21 to 25\./ })).toBeVisible();
  await add.getByRole("link", { name: "Add to plan fp-pg-22", exact: true }).click();
  await expect(page.locator("#announcer")).toHaveText("Added fp-pg-22. The plan has 3 mods.");
  await expect(page.locator("#mod-fp-pg-22-add")).toBeFocused();
  await expect(page).toHaveURL(/page=3/);
  // The plan's button is one link away.
  await add.getByRole("link", { name: "Back to the plan" }).click();
  await expect(page).toHaveURL(/#confirm-heading$/);
  await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeInViewport();
});

test("Add ticked mods with nothing ticked says so on the search page", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  await page.getByRole("button", { name: "Add ticked mods to plan" }).click();
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.locator("#pick-error")).toHaveText("Error: Nothing is ticked. Tick the mods to add, or use a mod's own Add to plan button.");
  // The summary points at the first box.
  await expect(page.locator("#error-summary").getByRole("link")).toHaveAttribute("href", "#mod-fp-pg-00-pick");
  await expect(page.getByRole("checkbox", { name: "fp-pg-00" })).toHaveAttribute("aria-describedby", "pick-error");
  await checkPage(page);
});

test("a search under Add another mod that finds nothing says what to try (#355)", async ({ page }) => {
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-pg-00");
  const add = page.getByRole("region", { name: "Add another mod" });
  await add.getByLabel("Mod name").fill("no such mod anywhere");
  await add.getByRole("button", { name: "Search" }).click();
  await expect(add).toContainText("No mod for Factorio 2.0 matches. Try a shorter part of the name.");
  await checkPage(page);
});
