import { test, expect, checkPage, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 9.1, 9.2, 9.14, 17.10: the search results are a list shown ten at a
// time with page controls above and below; "+" adds a mod to the world's
// plan beside the results, which shows what each pick needs. The projects
// run it in both themes, at desktop and at 320 px.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });
const names = Array.from({ length: 25 }, (_, i) => `fp-pg-${String(i).padStart(2, "0")}`);
const phone = (page: Page) => (page.viewportSize()?.width ?? 1280) < 1024;
const panel = (page: Page) => page.locator("#plan-panel");
const hit = (page: Page, name: string) => page.locator(`#mod-${name}-hit`);

test.beforeEach(async ({ page, foreman, fakeGame, fakePortal }) => {
  // The mod list is cached once the world is set up, so every mod is added first.
  const portal = fakePortal!;
  for (const n of [...names, "fp-rs-rail-signal", "fp-rs-rail-only"]) await portal.add(n, "1.0.0");
  // Two mods that share a dependency, one of which needs an installed mod updated.
  await portal.add("fp-core", "1.0.0");
  await portal.add("fp-lib", "1.0.0");
  await portal.add("fp-lib", "1.1.0");
  await portal.add("fp-rails", "1.0.0", ["base >= 2.0", "fp-core", "fp-lib >= 1.1.0"]);
  await portal.add("fp-trains", "1.0.0", ["fp-core >= 1.0.0", "fp-gone"]);
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-lib": "2.0" } });
});

test("25 results: a list a page at a time, with Previous, a page box and Next above and below", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^Base/);
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg"' })).toBeVisible();
  const list = page.getByRole("list", { name: "25 mods for Factorio 2.0 match. Showing 1 to 10." });
  await expect(list.getByRole("listitem")).toHaveCount(10);
  await expect(page.getByRole("table")).toHaveCount(0);
  // Each row: the title links to the portal in a new tab, then the summary and the facts.
  const first = hit(page, "fp-pg-00");
  const title = first.getByRole("link", { name: "fp-pg-00 on the mod portal, opens in a new tab" });
  await expect(title).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-pg-00");
  await expect(title).toHaveAttribute("target", "_blank");
  await expect(first).toContainText("added by a test");
  await expect(first.locator("dt")).toHaveText(["Latest", "Downloads", "Updated"]);
  await expect(first.locator("time")).toHaveAttribute("datetime", /^\d{4}-\d\d-\d\dT/);
  const add = first.getByRole("button", { name: "Add fp-pg-00 to the plan" });
  const size = (await add.boundingBox())!;
  // 44 px on a touch screen, 32 with a mouse (WCAG 2.5.8 asks 24).
  expect(size.width).toBeGreaterThanOrEqual(32);
  const top = page.getByRole("navigation", { name: "Result pages, above the list" });
  const bottom = page.getByRole("navigation", { name: "Result pages, below the list" });
  await expect(top.getByRole("textbox", { name: "Page" })).toHaveValue("1");
  await expect(top).toContainText("of 3");
  await expect(bottom.getByRole("link", { name: "Next page" })).toBeVisible();
  await expect(top.getByRole("link", { name: "Previous page" })).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 2, name: "Plan" })).toBeVisible();
  await expect(panel(page)).toContainText("No mods in the plan yet. Choose + beside a mod to add it, then search again for the next.");
  // On a wide screen the plan starts level with the search, in the rail beside it.
  const searchBox = (await page.locator("section:has(#search-heading)").boundingBox())!;
  const planBox = (await panel(page).boundingBox())!;
  if (phone(page)) expect(planBox.y).toBeGreaterThan(searchBox.y + searchBox.height);
  else {
    expect(Math.abs(planBox.y - searchBox.y)).toBeLessThan(2);
    // It stays in view while the list scrolls past.
    await page.getByRole("navigation", { name: "Result pages, below the list" }).scrollIntoViewIfNeeded();
    const held = (await panel(page).boundingBox())!;
    expect(held.y).toBeGreaterThanOrEqual(0);
    expect(held.y).toBeLessThan(40);
  }
  await checkPage(page);
  await shot(page, "mods-search-25");

  // Next replaces only the list, keeps the address in step and puts focus on the results.
  await bottom.getByRole("link", { name: "Next page" }).click();
  await expect(page.getByRole("list", { name: /Showing 11 to 20\./ })).toBeVisible();
  await expect(page.locator("#results-heading")).toBeFocused();
  await expect(page).toHaveURL(/page=2/);
  // The page box jumps straight to a page.
  await top.getByRole("textbox", { name: "Page" }).fill("3");
  await top.getByRole("button", { name: "Go to page" }).click();
  await expect(page.getByRole("list", { name: /Showing 21 to 25\./ })).toBeVisible();
  await expect(page.locator("#results-heading")).toBeFocused();
  await expect(top.getByRole("link", { name: "Next page" })).toHaveCount(0);
  await expect(top.getByRole("link", { name: "Previous page" })).toBeVisible();
  // A number past the end goes to the last page.
  await bottom.getByRole("textbox", { name: "Page" }).fill("99");
  await bottom.getByRole("textbox", { name: "Page" }).press("Enter");
  await expect(page.getByRole("list", { name: /Showing 21 to 25\./ })).toBeVisible();
  await checkPage(page);
});

// SPEC 9.1, 9.2 (rulings 2, 3, 6, 9): "+" says it is working, then the row
// says "In plan" with "−" and the panel shows the pick with what it needs.
test("+ adds a mod in place: Adding…, In plan with −, and the panel's tree", async ({ page }) => {
  await page.goto("/worlds/base/mods");
  const box = page.getByLabel("Mod name");
  await box.fill("fp-rails");
  await expect(hit(page, "fp-rails")).toBeVisible();
  // Hold the add, so its working state shows.
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  await page.route("**/mods/plan/mods", async (route) => {
    await held;
    await route.continue();
  });
  await hit(page, "fp-rails").getByRole("button", { name: "Add fp-rails to the plan" }).click();
  await expect(hit(page, "fp-rails").getByText("Adding…")).toBeVisible();
  release();
  await expect(page.locator("#announcer")).toHaveText("fp-rails added to the plan. The plan has 1 mod.");
  const remove = hit(page, "fp-rails").getByRole("button", { name: "Remove fp-rails from the plan" });
  await expect(remove).toBeFocused();
  await expect(hit(page, "fp-rails")).toContainText("In plan");
  await expect(hit(page, "fp-rails").getByText("Adding…")).toBeHidden();
  // The address stays the tab's.
  expect(new URL(page.url()).pathname).toBe("/worlds/base/mods");

  if (phone(page)) await panel(page).locator("#plan-list > summary").click();
  const plan = panel(page);
  await expect(plan.locator(".plan-tree__pick")).toHaveCount(1);
  const deps = plan.locator(".plan-tree__dep");
  await expect(deps).toHaveCount(2);
  await expect(deps.nth(0)).toContainText("fp-core");
  await expect(deps.nth(0)).toContainText("Needed by fp-rails");
  await expect(deps.nth(1)).toContainText("fp-lib");
  await expect(deps.nth(1)).toContainText("Will update to 1.1.0");
  // base is built in, so it is never listed.
  await expect(plan).not.toContainText("base");
  await expect(plan.getByRole("link", { name: "Review and install 3 mods" })).toHaveAttribute("href", "/worlds/base/mods/plan?kind=install");

  // A dependency in the results says what needs it and has no button.
  await box.fill("fp-core");
  await expect(hit(page, "fp-core")).toContainText("Needed by fp-rails");
  await expect(hit(page, "fp-core").getByRole("button")).toHaveCount(0);
  await checkPage(page);

  // "−" takes it out again; the dependency goes with it.
  await box.fill("fp-rails");
  await hit(page, "fp-rails").getByRole("button", { name: "Remove fp-rails from the plan" }).click();
  await expect(page.locator("#announcer")).toHaveText("fp-rails removed from the plan. The plan has 0 mods.");
  await expect(hit(page, "fp-rails").getByRole("button", { name: "Add fp-rails to the plan" })).toBeVisible();
  await expect(panel(page)).toContainText("No mods in the plan yet.");
});

// Rulings 2-4, SPEC 9.2: two picks that share a dependency show it once,
// under the first, naming both; an
// installed mod that must change shows its update; a dependency that cannot
// be had shows a warning; Review counts each mod once.
test("two picks share a dependency: it shows once naming both, counted once, with an update and a warning", async ({ page }) => {
  await page.goto("/worlds/base/mods");
  const box = page.getByLabel("Mod name");
  for (const n of ["fp-rails", "fp-trains"]) {
    await box.fill(n);
    await hit(page, n).getByRole("button", { name: `Add ${n} to the plan` }).click();
    await expect(hit(page, n)).toContainText("In plan");
  }
  const plan = panel(page);
  if (phone(page)) {
    // The bar holds to the bottom of the screen with the count, the names and Review.
    const bar = (await plan.boundingBox())!;
    expect(Math.round(bar.y + bar.height)).toBeLessThanOrEqual(page.viewportSize()!.height + 1);
    await expect(plan.locator(".plan-panel__sum")).toHaveText("2 mods in the plan");
    await expect(plan.locator(".plan-panel__names")).toHaveText("fp-rails, fp-trains");
    await expect(plan.getByRole("link", { name: "Review and install 4 mods" })).toBeVisible();
    await checkPage(page);
    await shot(page, "mods-plan-bar");
    await plan.locator("#plan-list > summary").click();
  }
  const picks = plan.locator(".plan-tree__pick");
  await expect(picks).toHaveCount(2);
  await expect(picks.nth(0)).toContainText("fp-core");
  await expect(picks.nth(0)).toContainText("Will update to 1.1.0");
  await expect(picks.nth(0).locator(".plan-tree__why").first()).toHaveText("Needed by fp-rails and fp-trains");
  await expect(picks.nth(1)).not.toContainText("fp-core");
  await expect(picks.nth(1).locator(".plan-tree__why").first()).toHaveText("Needed by fp-trains");
  await expect(picks.nth(1).locator(".plan-tree__warn")).toHaveText("Warning: Not on the mod portal");
  // Each name links to its portal page in a new tab, apart from the cross; a
  // mod the portal lacks is not linked (ruling on #623).
  const rails = picks.nth(0).getByRole("link", { name: "fp-rails on the mod portal, opens in a new tab" });
  await expect(rails).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-rails");
  await expect(rails).toHaveAttribute("target", "_blank");
  await expect(picks.nth(0).getByRole("link", { name: "fp-core on the mod portal, opens in a new tab" }).first()).toBeVisible();
  await expect(picks.nth(1).getByRole("link", { name: /^fp-gone/ })).toHaveCount(0);
  await expect(picks.nth(0).getByRole("button", { name: "Remove fp-rails from the plan" })).toBeVisible();
  // fp-rails, fp-trains, fp-core and fp-lib; fp-gone cannot be had.
  await expect(plan.getByRole("link", { name: "Review and install 4 mods" })).toBeVisible();
  await expect(plan).toContainText(/Started /);
  await checkPage(page);
  await shot(page, "mods-plan-tree");

  // The plan is kept on the server: a reload still has it.
  await page.reload();
  await expect(panel(page).locator(".plan-panel__sum")).toHaveText("2 mods in the plan");

  // Review opens today's plan page with the picks.
  await panel(page).getByRole("link", { name: "Review and install 4 mods" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeVisible();
  // The link names no mods: the review reads the saved picks (SPEC 17).
  await expect(page).toHaveURL(/\/mods\/plan\?kind=install$/);
  await expect(page.locator("#mod-plan")).toContainText("fp-trains");
  // The review page names both too, on the shared dependency's one row.
  await expect(page.locator("#mod-plan")).toContainText("fp-rails and fp-trains need it");
});

// Ruling 5: the cross takes a pick out with the dependencies nothing else
// needs; Clear plan empties it.
test("the cross removes a pick and its own dependencies, and Clear plan empties the plan", async ({ page }) => {
  await page.goto("/worlds/base/mods");
  const box = page.getByLabel("Mod name");
  for (const n of ["fp-rails", "fp-pg-01"]) {
    await box.fill(n);
    await hit(page, n).getByRole("button", { name: `Add ${n} to the plan` }).click();
    await expect(hit(page, n)).toContainText("In plan");
  }
  const plan = panel(page);
  if (phone(page)) await plan.locator("#plan-list > summary").click();
  await plan.getByRole("button", { name: "Remove fp-rails from the plan" }).click();
  await expect(page.locator("#announcer")).toHaveText("fp-rails removed from the plan. The plan has 1 mod.");
  await expect(plan.getByRole("button", { name: "Remove fp-pg-01 from the plan" })).toBeFocused();
  await expect(plan).not.toContainText("fp-core");
  await expect(plan).not.toContainText("fp-rails");
  await plan.getByRole("button", { name: "Clear plan" }).click();
  await expect(page.locator("#announcer")).toHaveText("The plan is empty.");
  await expect(page.locator("#plan-heading")).toBeFocused();
  await expect(plan).toContainText("No mods in the plan yet.");
  await expect(hit(page, "fp-pg-01").getByRole("button", { name: "Add fp-pg-01 to the plan" })).toBeVisible();
});

// While a change to the plan runs, Review is held back and says why, so it
// never opens the plan without the last change; "Adding…" is read out.
test("Review waits for a change that is still being worked out", async ({ page }) => {
  await page.goto("/worlds/base/mods");
  const box = page.getByLabel("Mod name");
  await box.fill("fp-pg-01");
  await hit(page, "fp-pg-01").getByRole("button", { name: "Add fp-pg-01 to the plan" }).click();
  await expect(hit(page, "fp-pg-01")).toContainText("In plan");
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  await page.route("**/mods/plan/mods", async (route) => {
    await held;
    await route.continue();
  });
  await box.fill("fp-pg-02");
  await hit(page, "fp-pg-02").getByRole("button", { name: "Add fp-pg-02 to the plan" }).click();
  await expect(page.locator("#announcer")).toHaveText("Adding…");
  const review = panel(page).getByRole("link", { name: "Review and install 1 mod" });
  await expect(review).toHaveAttribute("aria-disabled", "true");
  await expect(review).toHaveAccessibleDescription("Review is ready once the plan is updated.");
  await expect(panel(page).getByText("Review is ready once the plan is updated.")).toBeVisible();
  await review.click({ force: true });
  expect(new URL(page.url()).pathname).toBe("/worlds/base/mods");
  release();
  const ready = panel(page).getByRole("link", { name: "Review and install 2 mods" });
  await expect(ready).not.toHaveAttribute("aria-disabled", "true");
  await ready.click();
  await expect(page).toHaveURL(/\/mods\/plan\?kind=install$/);
  await expect(page.locator("#mod-plan")).toContainText("fp-pg-02");
});

// WCAG 2.4.11: at phone width, a row's button reached by keyboard is never
// hidden behind the plan's bar.
test("keyboard focus is never hidden behind the plan's bar", async ({ page }) => {
  test.skip(!phone(page), "the bar is a phone layout");
  await page.goto("/worlds/base/mods");
  const box = page.getByLabel("Mod name");
  await box.fill("fp-pg-00");
  await hit(page, "fp-pg-00").getByRole("button", { name: "Add fp-pg-00 to the plan" }).click();
  await expect(hit(page, "fp-pg-00")).toContainText("In plan");
  await box.fill("fp-pg");
  await expect(page.getByRole("list", { name: /Showing 1 to 10\./ })).toBeVisible();
  await box.focus();
  let checked = 0;
  for (let i = 0; i < 60 && checked < 8; i++) {
    await page.keyboard.press("Tab");
    const focused = page.locator(":focus");
    if (!(await focused.evaluate((el) => !!el.closest(".hits")))) continue;
    const bar = (await panel(page).boundingBox())!;
    const at = (await focused.boundingBox())!;
    expect(at.y + at.height, await focused.evaluate((el) => el.outerHTML.slice(0, 80))).toBeLessThanOrEqual(bar.y + 1);
    checked++;
  }
  expect(checked).toBeGreaterThan(4);
});

// SPEC 9.2: with the portal out of reach the panel still lists the picks by
// name, says their dependencies cannot be checked now, and offers to check again.
test("with the portal down the panel lists the picks by name and says why", async ({ page, fakePortal }) => {
  // A pick the portal client has not seen yet, so working out the plan needs the portal.
  const r = await page.request.post("/worlds/base/mods/plan/mods", { form: { name: "fp-unseen", op: "add" }, maxRedirects: 0 });
  expect(r.status()).toBe(303);
  await fakePortal!.failApi(500);
  await page.goto("/worlds/base/mods");
  const plan = panel(page);
  if (phone(page)) await plan.locator("#plan-list > summary").click();
  await expect(plan).toContainText("fp-unseen");
  await expect(plan.locator(".message--warning")).toContainText("Foreman could not reach the mod portal, so it cannot check what these mods need now.");
  await expect(plan.getByRole("link", { name: "Check again" })).toHaveAttribute("href", "/worlds/base/mods");
  await expect(plan.getByRole("link", { name: "Review the plan" })).toHaveAttribute("href", "/worlds/base/mods/plan?kind=install");
  await checkPage(page);
});

// SPEC 9.1: without script every control is a plain link or form, and each
// change comes back to the same search, page and row.
test("without script +, −, the page controls and Clear plan work and keep the place", async ({ page, foreman, browser }) => {
  const plain = await browser.newContext({ baseURL: foreman.url, javaScriptEnabled: false, storageState: await page.context().storageState() });
  const p = await plain.newPage();
  await p.goto("/worlds/base/mods");
  await expect(p.getByRole("heading", { level: 2, name: "Mods matching" })).toBeVisible();
  await expect(p.locator("#results")).toContainText("Type at least 2 characters of a mod's name in Find a mod.");
  await p.getByLabel("Mod name").fill("fp-pg");
  await p.getByRole("button", { name: "Search", exact: true }).click();
  await expect(p).toHaveURL(/\/mods\/search\?q=fp-pg$/);
  await p.getByRole("navigation", { name: "Result pages, below the list" }).getByRole("link", { name: "Next page" }).click();
  await expect(p.getByRole("list", { name: /Showing 11 to 20\./ })).toBeVisible();
  await p.getByRole("button", { name: "Add fp-pg-12 to the plan" }).click();
  await expect(p).toHaveURL(/\/mods\/search\?page=2&q=fp-pg#mod-fp-pg-12-hit$/);
  await expect(hit(p, "fp-pg-12")).toContainText("In plan");
  await expect(p.locator("#plan-panel .plan-panel__sum")).toHaveText("1 mod in the plan");
  // The page box is a plain form too.
  const top = p.getByRole("navigation", { name: "Result pages, above the list" });
  await top.getByRole("textbox", { name: "Page" }).fill("3");
  await top.getByRole("button", { name: "Go to page" }).click();
  await expect(p.getByRole("list", { name: /Showing 21 to 25\./ })).toBeVisible();
  await p.goto("/worlds/base/mods/search?q=fp-pg&page=2");
  await hit(p, "fp-pg-12").getByRole("button", { name: "Remove fp-pg-12 from the plan" }).click();
  await expect(p).toHaveURL(/page=2&q=fp-pg#mod-fp-pg-12-hit$/);
  await expect(hit(p, "fp-pg-12").getByRole("button", { name: "Add fp-pg-12 to the plan" })).toBeVisible();
  await p.getByRole("button", { name: "Add fp-pg-13 to the plan" }).click();
  // Without script the panel's list is folded on every width; it opens by its own line.
  await p.locator("#plan-list > summary").click();
  await p.getByRole("button", { name: "Clear plan" }).click();
  await expect(p).toHaveURL(/#plan-panel$/);
  await expect(p.locator("#plan-panel")).toContainText("No mods in the plan yet.");
  await plain.close();
});

// SPEC 9.1: a search of two words finds a mod with both, in either order.
test("a search of two words finds a mod with both, in either order", async ({ page }) => {
  for (const q of ["signal rail", "rail signal"]) {
    await page.goto(`/worlds/base/mods/search?q=${encodeURIComponent(q)}`);
    await expect(hit(page, "fp-rs-rail-signal")).toBeVisible();
    await expect(hit(page, "fp-rs-rail-only")).toHaveCount(0);
  }
});

// SPEC 9.1, 9.11: a pasted mod portal link, with a sub-page and a query, finds that one mod.
test("a pasted portal link with a sub-page and query finds that exact mod", async ({ page }) => {
  await page.goto("/worlds/base/mods/search");
  await page.getByLabel("Mod name").fill("www.mods.factorio.com/mod/fp-pg-03/changelog?from=search");
  await page.getByLabel("Mod name").press("Enter");
  const list = page.getByRole("list", { name: "1 mod for Factorio 2.0 matches." });
  await expect(list.getByRole("listitem")).toHaveCount(1);
  await expect(list.getByRole("button", { name: "Add fp-pg-03 to the plan" })).toBeVisible();
  await checkPage(page);
});

test("a pasted portal link to an unknown mod, to a mod without a release, and to another site", async ({ page, fakePortal }) => {
  await fakePortal!.add("fp-link-new", "1.0.0", ["base >= 2.1"], "2.1");
  const search = async (q: string) => {
    await page.goto("/worlds/base/mods/search");
    await page.getByLabel("Mod name").fill(q);
    await page.getByLabel("Mod name").press("Enter");
  };
  await search("https://mods.factorio.com/mod/fp-pg-1");
  await expect(page.getByText("No mod named fp-pg-1 is on the mod portal.")).toBeVisible();
  await expect(page.locator(".hits")).toHaveCount(0);
  await checkPage(page);
  await search("mods.factorio.com/mod/fp-link-new");
  await expect(page.getByText("fp-link-new has no release for Factorio 2.0.")).toBeVisible();
  await search("https://example.com/mod/fp-pg-03");
  await expect(page.getByText("No mod for Factorio 2.0 matches.")).toBeVisible();
});

// SPEC 9.1: with script the results follow the typing after a pause; only the
// results change, focus stays in the box, the address and tab title follow the
// text without new history entries, and a screen reader hears the count.
test("on the search page results follow the typing, keep focus, and the address follows", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg-01");
  const box = page.getByLabel("Mod name");
  const history = await page.evaluate(() => window.history.length);
  await box.fill("fp-pg-0");
  await expect(page.getByRole("list", { name: "10 mods for Factorio 2.0 match." })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: 'Mods matching "fp-pg-0"' })).toBeVisible();
  await expect(box).toBeFocused();
  await expect(page.locator("#announcer")).toHaveText("10 mods for Factorio 2.0 match.");
  expect(new URL(page.url()).searchParams.get("q")).toBe("fp-pg-0");
  await expect(page).toHaveTitle(/Mods matching "fp-pg-0"/);
  expect(await page.evaluate(() => window.history.length)).toBe(history);
  await page.reload();
  await expect(box).toHaveValue("fp-pg-0");
  await checkPage(page);
});

// SPEC 9.1: under 2 characters the panel stays and says what to type.
test("under 2 characters the panel says what to type and the portal is not asked", async ({ page }) => {
  await page.goto("/worlds/base/mods/search?q=fp-pg-01");
  const box = page.getByLabel("Mod name");
  await box.fill("f");
  await expect(page.getByRole("heading", { level: 2, name: "Mods matching" })).toBeVisible();
  await expect(page.locator("#results .empty")).toHaveText("Type at least 2 characters of a mod's name in Find a mod. Matching mods show here.");
  await expect(page.locator("#announcer")).toHaveText("Type at least 2 characters of a mod's name in Find a mod. Matching mods show here.");
  await expect(page.locator(".hits")).toHaveCount(0);
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
  await expect(page.getByRole("list", { name: "1 mod for Factorio 2.0 matches." })).toBeVisible();
  await expect(error).toHaveCount(0);
  await expect(box).not.toHaveAttribute("aria-invalid", "true");
  await expect(box).toHaveAttribute("aria-describedby", "mod-search-hint mod-paste-hint");
});

// A reply that lands after the box changed is stale and never shows.
test("a slow reply does not replace the results for newer text", async ({ page }) => {
  await page.goto("/worlds/base/mods");
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  let asked!: () => void;
  const sent = new Promise<void>((r) => (asked = r));
  let first = true;
  await page.route("**/mods/search?**", async (route) => {
    if (!first) return route.continue();
    first = false;
    asked();
    await held;
    await route.continue();
  });
  const box = page.getByLabel("Mod name");
  await box.fill("fp-pg-03");
  await sent;
  await box.fill("f");
  release();
  await page.waitForTimeout(800);
  await expect(page.locator(".hits")).toHaveCount(0);
  await expect(page.locator("#results .empty")).toBeVisible();
});

// The portal's own browse page for the world's game line, in a new tab, on both search forms.
for (const at of ["/worlds/base/mods", "/worlds/base/mods/search?q=fp-pg"]) {
  test(`the Browse the mod portal link on ${at}`, async ({ page }) => {
    await page.goto(at);
    const link = page.getByRole("link", { name: "Browse the mod portal (opens in a new tab)" });
    await expect(link).toHaveAttribute("href", "https://mods.factorio.com/browse/downloaded?factorio_version=2.0");
    await expect(link).toHaveAttribute("target", "_blank");
    await expect(page.getByText("You can also paste a mod's link from the portal.")).toBeVisible();
  });
}

// SPEC 9.1: on the Mods tab the "Mods matching" panel is always there; typing
// and paging replace only the list, and the address stays the tab's.
test.describe("typing on the Mods tab", () => {
  test("the panel is there before any typing, and results follow the typing in it", async ({ page }) => {
    await page.goto("/worlds/base/mods");
    const url = page.url();
    const history = await page.evaluate(() => window.history.length);
    await expect(page.getByRole("heading", { level: 2, name: "Mods matching" })).toBeVisible();
    await expect(page.locator("#results .empty")).toHaveText("Type at least 2 characters of a mod's name in Find a mod. Matching mods show here.");
    const box = page.getByLabel("Mod name");
    await box.click();
    await page.keyboard.type("fp-pg-0", { delay: 20 });
    await expect(page.getByRole("list", { name: "10 mods for Factorio 2.0 match." })).toBeVisible();
    await expect(page.locator("#announcer")).toHaveText("10 mods for Factorio 2.0 match.");
    await expect(box).toBeFocused();
    expect(await box.evaluate((b: HTMLInputElement) => [b.selectionStart, b.selectionEnd])).toEqual([7, 7]);
    await page.keyboard.type("2");
    await expect(page.getByRole("list", { name: "1 mod for Factorio 2.0 matches." })).toBeVisible();
    expect(page.url()).toBe(url);
    expect(await page.evaluate(() => window.history.length)).toBe(history);
    // Search, results, the plan, then the installed list.
    const order = await page.evaluate(() => {
      const at = (s: string) => document.querySelector(s)!;
      const after = (a: Element, b: Element) => !!(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
      return after(at("#search-heading"), at("#results-heading")) && after(at("#results-heading"), at("#plan-heading")) && after(at("#plan-heading"), at("#installed-heading"));
    });
    expect(order).toBe(true);
    await checkPage(page);
    await shot(page, "mods-tab-typed");
  });

  test("Enter searches in place, and the page controls keep the tab's address", async ({ page }) => {
    await page.goto("/worlds/base/mods");
    const box = page.getByLabel("Mod name");
    await box.fill("fp-pg");
    await box.press("Enter");
    await expect(page.getByRole("list", { name: /Showing 1 to 10\./ })).toBeVisible();
    await page.getByRole("navigation", { name: "Result pages, above the list" }).getByRole("link", { name: "Next page" }).click();
    await expect(page.getByRole("list", { name: /Showing 11 to 20\./ })).toBeVisible();
    await expect(page.locator("#results-heading")).toBeFocused();
    expect(new URL(page.url()).pathname).toBe("/worlds/base/mods");
    expect(new URL(page.url()).search).toBe("");
  });

  test("a refused search says why in place", async ({ page }) => {
    await page.goto("/worlds/base/mods");
    const box = page.getByLabel("Mod name");
    await box.fill("a".repeat(150));
    await expect(page.locator("#mod-search-error")).toHaveText("Error: Enter at most 100 characters.");
    await expect(box).toHaveAttribute("aria-invalid", "true");
    await expect(box).toBeFocused();
    expect(page.url()).toMatch(/\/worlds\/base\/mods$/);
  });

  test("a long pasted link with a tracking query finds its mod", async ({ page }) => {
    const link = `https://mods.factorio.com/mod/fp-pg-04?utm_source=${"x".repeat(150)}&utm_medium=share`;
    await page.goto("/worlds/base/mods");
    await page.getByLabel("Mod name").fill(link);
    await expect(page.getByRole("list", { name: "1 mod for Factorio 2.0 matches." }).getByRole("button", { name: "Add fp-pg-04 to the plan" })).toBeVisible();
  });
});
