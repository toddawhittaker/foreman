import { test, expect, checkPage, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 9.1, 9.2, 17.10: a search with many results is a table
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
  // The world is the heading; the matches are a section that says how many there are in all.
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
  await expect(page.getByRole("group", { name: "Also ticked" }).getByRole("checkbox", { name: "fp-pg-01" })).toBeChecked();
  await page.getByRole("checkbox", { name: "fp-pg-12" }).focus();
  await page.keyboard.press("Space");
  await checkPage(page);
  await page.getByRole("button", { name: "Add ticked mods to plan" }).click();

  // One plan with both, and the same page of the same search below it.
  await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeVisible();
  const asked = page.locator("#mod-plan .plan-groups");
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

// SPEC 9.1: ticks are kept across new searches, so mods found by several searches go into one plan.
test("three searches, one tick each, one plan with all three", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg-01");
  await page.getByRole("checkbox", { name: "fp-pg-01" }).check();
  for (const n of ["fp-pg-02", "fp-pg-03", "fp-pg-04"]) {
    await page.getByLabel("Mod name").fill(n);
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await page.getByRole("table").getByRole("checkbox", { name: n }).check();
  }
  // The mods ticked in earlier searches are listed with their own boxes, so one can be unticked.
  const ticked = page.getByRole("group", { name: "Also ticked" });
  await expect(ticked.getByRole("checkbox")).toHaveCount(3);
  await expect(ticked.getByRole("checkbox", { name: "fp-pg-01" })).toBeChecked();
  await checkPage(page);
  await shot(page, "mods-search-ticked");
  await ticked.getByRole("checkbox", { name: "fp-pg-02" }).uncheck();
  await page.getByRole("button", { name: "Add ticked mods to plan" }).click();

  await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeVisible();
  const asked = page.locator("#mod-plan .plan-groups");
  for (const n of ["fp-pg-01", "fp-pg-03", "fp-pg-04"]) await expect(asked).toContainText(`${n} 1.0.0`);
  await expect(asked).not.toContainText("fp-pg-02");
  await expect(page.getByRole("button", { name: "Install 3 mods" })).toBeVisible();
});

// SPEC 9.1: Enter in the search box runs a new search, not Add ticked mods, and keeps the ticks.
test("Enter in the search box searches and keeps the ticks", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg-01");
  await page.getByRole("checkbox", { name: "fp-pg-01" }).check();
  await page.getByLabel("Mod name").fill("fp-pg-02");
  await page.getByLabel("Mod name").press("Enter");
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg-02"' })).toBeVisible();
  await expect(page.getByRole("group", { name: "Also ticked" }).getByRole("checkbox", { name: "fp-pg-01" })).toBeChecked();
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

test("a search under Add another mod that finds nothing says what to try", async ({ page }) => {
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-pg-00");
  const add = page.getByRole("region", { name: "Add another mod" });
  await add.getByLabel("Mod name").fill("no such mod anywhere");
  await add.getByRole("button", { name: "Search" }).click();
  await expect(add).toContainText("No mod for Factorio 2.0 matches. Try a shorter part of the name.");
  await checkPage(page);
});

// SPEC 9.1, 9.11: a pasted mod portal link, with a sub-page and a query, finds
// that one mod by its exact name, ready to add.
test("a pasted portal link with a sub-page and query finds that exact mod", async ({ page }) => {
  await page.goto("/worlds/base/mods/search");
  await page.getByLabel("Mod name").fill("www.mods.factorio.com/mod/fp-pg-03/changelog?from=search");
  await page.getByLabel("Mod name").press("Enter");
  const table = page.getByRole("table", { name: "1 mod for Factorio 2.0 matches." });
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(table.getByRole("checkbox", { name: "fp-pg-03" })).toBeVisible();
  await checkPage(page);
  await page.getByRole("link", { name: "Add to plan fp-pg-03", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install fp-pg-03 in Base?" })).toBeVisible();
});

// SPEC 9.1: a link to a name no mod has says so, rather than listing mods that merely contain it.
test("a pasted portal link to an unknown mod says there is no such mod", async ({ page }) => {
  await page.goto("/worlds/base/mods/search");
  await page.getByLabel("Mod name").fill("https://mods.factorio.com/mod/fp-pg-1");
  await page.getByLabel("Mod name").press("Enter");
  await expect(page.getByText("No mod named fp-pg-1 is on the mod portal.")).toBeVisible();
  await expect(page.getByRole("table")).toHaveCount(0);
  await checkPage(page);
});

// SPEC 9.1: a mod the portal has, but not for the world's game line, is told apart from one it lacks.
test("a pasted portal link to a mod with no release for the game line says so", async ({ page, fakePortal }) => {
  await fakePortal!.add("fp-link-new", "1.0.0", ["base >= 2.1"], "2.1");
  await page.goto("/worlds/base/mods/search");
  await page.getByLabel("Mod name").fill("mods.factorio.com/mod/fp-link-new");
  await page.getByLabel("Mod name").press("Enter");
  await expect(page.getByText("fp-link-new has no release for Factorio 2.0.")).toBeVisible();
  await expect(page.getByRole("table")).toHaveCount(0);
  await checkPage(page);
});

test("a link to another site is ordinary search text", async ({ page }) => {
  await page.goto("/worlds/base/mods/search");
  await page.getByLabel("Mod name").fill("https://example.com/mod/fp-pg-03");
  await page.getByLabel("Mod name").press("Enter");
  await expect(page.getByText("No mod for Factorio 2.0 matches.")).toBeVisible();
  await expect(page.getByRole("table")).toHaveCount(0);
});

// SPEC 9.1: with script the results follow the typing after a pause; only the
// results change, focus stays in the box, the address stays, and a screen
// reader hears the count.
test("results appear while typing, without Enter, and keep focus, address and ticks", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg-01");
  const box = page.getByLabel("Mod name");
  await page.getByRole("checkbox", { name: "fp-pg-01" }).check();
  await box.fill("fp-pg-0");
  const table = page.getByRole("table", { name: "10 mods for Factorio 2.0 match." });
  await expect(table).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg-0"' })).toBeVisible();
  await expect(box).toBeFocused();
  await expect(page).toHaveURL(/\?q=fp-pg-01$/);
  await expect(table.getByRole("checkbox", { name: "fp-pg-01" })).toBeChecked();
  await expect(page.locator("#announcer")).toHaveText("10 mods for Factorio 2.0 match.");
  // A ticked box survives a second typed search that no longer lists it.
  await box.fill("fp-pg-02");
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg-02"' })).toBeVisible();
  await expect(page.getByRole("group", { name: "Also ticked" }).getByRole("checkbox", { name: "fp-pg-01" })).toBeChecked();
  await expect(page.locator("#announcer")).toHaveText("1 mod for Factorio 2.0 matches.");
  await checkPage(page);
});

test("under 2 characters typing asks the server nothing", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg-01");
  let asked = 0;
  page.on("request", (r) => {
    if (r.headers()["hx-request"]) asked++;
  });
  await page.getByLabel("Mod name").fill("f");
  await page.waitForTimeout(600);
  expect(asked).toBe(0);
  await expect(page.getByRole("heading", { level: 2, name: /Mods matching/ })).toHaveCount(0);
});

// SPEC 9.1, 9.11: a pasted link goes through the same typed search.
test("a pasted portal link shows its one mod while typing", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  await page.getByLabel("Mod name").fill("https://mods.factorio.com/mod/fp-pg-03");
  const table = page.getByRole("table", { name: "1 mod for Factorio 2.0 matches." });
  await expect(table.getByRole("row")).toHaveCount(2);
  await expect(table.getByRole("checkbox", { name: "fp-pg-03" })).toBeVisible();
});

test("without script the search form and Search button still work", async ({ page, foreman, browser }) => {
  const plain = await browser.newContext({ baseURL: foreman.url, javaScriptEnabled: false, storageState: await page.context().storageState() });
  const p = await plain.newPage();
  await p.goto("/worlds/base/mods/search?q=fp-pg-01");
  await p.getByLabel("Mod name").fill("fp-pg-02");
  await p.getByRole("button", { name: "Search", exact: true }).click();
  await expect(p).toHaveURL(/q=fp-pg-02/);
  await expect(p.getByRole("table", { name: "1 mod for Factorio 2.0 matches." })).toBeVisible();
  await plain.close();
});

// The portal's own browse page for the world's game line, in a new tab, on both search forms.
for (const at of ["/worlds/base/mods", "/worlds/base/mods/search?q=fp-pg"]) {
  test(`the Browse the mod portal link on ${at}`, async ({ page }) => {
    await page.goto(at);
    const link = page.getByRole("link", { name: "Browse the mod portal (opens in a new tab)" });
    await expect(link).toHaveAttribute("href", "https://mods.factorio.com/browse/downloaded?factorio_version=2.0");
    await expect(link).toHaveAttribute("target", "_blank");
    await expect(page.getByText("You can also paste a mod's link from the portal.")).toBeVisible();
    await shot(page, `mods-browse-${at.includes("search") ? "search" : "tab"}`);
  });
}

// SPEC 9.1: a long pasted link with a tracking query is not cut by the box,
// on the Mods tab or while typing on the search page.
test("a long pasted link with a tracking query finds its mod", async ({ page }) => {
  const link = `https://mods.factorio.com/mod/fp-pg-04?utm_source=${"x".repeat(150)}&utm_medium=share`;
  expect(link.length).toBeGreaterThan(100);
  await page.goto("/worlds/base/mods");
  await page.getByLabel("Mod name").fill(link);
  await page.getByLabel("Mod name").press("Enter");
  await expect(page.getByRole("table", { name: "1 mod for Factorio 2.0 matches." }).getByRole("checkbox", { name: "fp-pg-04" })).toBeVisible();
  await page.getByLabel("Mod name").fill(link.replace("fp-pg-04", "fp-pg-05"));
  await expect(page.getByRole("table", { name: "1 mod for Factorio 2.0 matches." }).getByRole("checkbox", { name: "fp-pg-05" })).toBeVisible();
});

// SPEC 9.1: a typed search the server refuses shows and announces why, ties
// the error to the box, and a good search clears it.
test("a refused typed search says why, and a good one clears it", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  const box = page.getByLabel("Mod name");
  await box.fill("a".repeat(150));
  const error = page.locator("#mod-search-error");
  await expect(error).toHaveText("Error: Enter at most 100 characters.");
  await expect(box).toHaveAttribute("aria-invalid", "true");
  await expect(box).toHaveAttribute("aria-describedby", /mod-search-error/);
  await expect(page.locator("#announcer")).toHaveText("Error: Enter at most 100 characters.");
  await expect(box).toBeFocused();
  await box.fill("fp-pg-02");
  await expect(page.getByRole("table", { name: "1 mod for Factorio 2.0 matches." })).toBeVisible();
  await expect(error).toHaveCount(0);
  await expect(box).not.toHaveAttribute("aria-invalid", "true");
  await expect(box).toHaveAttribute("aria-describedby", "mod-search-hint mod-paste-hint");
});

test("going back under 2 characters hides the old results", async ({ page }) => {
  await page.goto("/worlds/base/mods/search");
  const box = page.getByLabel("Mod name");
  await box.fill("fp-pg-03");
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg-03"' })).toBeVisible();
  await box.fill("f");
  await expect(page.getByRole("heading", { level: 2, name: /Mods matching/ })).toHaveCount(0);
  await page.waitForTimeout(600);
  await expect(page.getByRole("table")).toHaveCount(0);
});

test("a typed search from page 2 keeps focus in the box", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page.getByRole("navigation", { name: "Result pages" })).toContainText("Page 2 of 3");
  const box = page.getByLabel("Mod name");
  await box.fill("fp-pg-1");
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg-1"' })).toBeVisible();
  await expect(box).toBeFocused();
});

// SPEC 9.1: a reply that lands after the box went under 2 characters is dropped.
test("a slow reply does not bring back results after the text is cut", async ({ page }) => {
  await page.goto("/worlds/base/mods/search");
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  let sent!: () => void;
  const out = new Promise<void>((r) => (sent = r));
  await page.route(/\/mods\/search\?/, async (route) => {
    if (!route.request().headers()["hx-request"]) return route.continue();
    sent();
    await held;
    await route.continue();
  });
  const box = page.getByLabel("Mod name");
  await box.fill("fp-pg-03");
  await out;
  await box.fill("f");
  release();
  await page.waitForTimeout(800);
  await expect(page.getByRole("heading", { level: 2, name: /Mods matching/ })).toHaveCount(0);
  await expect(page.getByRole("table")).toHaveCount(0);
});

test("cutting a refused search under 2 characters clears the error and says why", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  const box = page.getByLabel("Mod name");
  await box.fill("a".repeat(150));
  await expect(box).toHaveAttribute("aria-invalid", "true");
  await box.fill("a");
  await expect(box).not.toHaveAttribute("aria-invalid", "true");
  await expect(box).toHaveAttribute("aria-describedby", "mod-search-hint mod-paste-hint");
  await expect(page.locator("#mod-search-error")).toBeHidden();
  await expect(page.locator("#announcer")).toHaveText("Type at least 2 characters to search.");
});
