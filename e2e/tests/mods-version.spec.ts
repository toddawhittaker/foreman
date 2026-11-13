import { test, expect, checkPage, setUpModsWorld, doneBox, live, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.2 and 9.3: choosing a mod's version on the plan page, and
// Change version in the installed table, which also goes back after a bad
// update. The fake portal's mods are made for Factorio 2.0.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const modsFile = (dataDir: string, file: string) => join(dataDir, "worlds", "base", "mods", file);
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });

test("the plan lists the line's releases, newest that fits first, and an older one redraws the plan", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-ver-lib", "1.0.0");
  await portal.add("fp-ver-lib", "2.0.0");
  await portal.add("fp-ver", "1.0.0", ["base >= 2.0", "fp-ver-lib >= 1.0.0"]);
  await portal.add("fp-ver", "2.0.0", ["base >= 2.0", "fp-ver-lib >= 2.0.0"]);
  await portal.add("fp-ver", "3.0.0", ["base >= 2.1"], "2.1");
  await setUpModsWorld(page, foreman, fakeGame!, world);

  await page.goto("/worlds/base/mods/search?q=fp-ver");
  await page.getByRole("link", { name: "Add to plan fp-ver", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install fp-ver in Base?" })).toBeVisible();
  const version = page.getByLabel("Version of fp-ver");
  await expect(version).toHaveValue("");
  await expect(version.locator("option:checked")).toHaveText("Newest that fits (2.0.0)");
  // Only the world's line: no 3.0.0, which is for 2.1.
  await expect(version.locator("option")).toHaveText(["Newest that fits (2.0.0)", "2.0.0", "1.0.0"]);
  await expect(page.locator("#mod-plan .plan-groups")).toContainText("fp-ver 2.0.0");
  await checkPage(page);
  await shot(page, "mods-version-plan");

  // Choosing 1.0.0 from the keyboard redraws the plan in place; focus stays on the list.
  await version.focus();
  await version.selectOption("1.0.0");
  await expect(page.locator("#announcer")).toHaveText("Plan updated: 2 mods change.");
  await expect(page.locator("#mod-plan .plan-groups")).toContainText("fp-ver 1.0.0");
  await expect(page.getByLabel("Version of fp-ver")).toBeFocused();
  await checkPage(page);

  await page.getByRole("button", { name: "Install 2 mods" }).click();
  await expect(doneBox(page)).toContainText("Base", live);
  expect(existsSync(modsFile(foreman.dataDir, "fp-ver_1.0.0.zip"))).toBe(true);
  expect(existsSync(modsFile(foreman.dataDir, "fp-ver_2.0.0.zip"))).toBe(false);
});

// SPEC 9.2: the newest choice wins. An older redraw that is still on its way
// must not bring back the older choice.
test("a late redraw does not undo a newer version choice", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-race", "1.0.0", ["base >= 2.0"]);
  await portal.add("fp-race", "2.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, world);
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-race");
  const version = page.getByLabel("Version of fp-race");
  await expect(version).toHaveValue("");

  // The first redraw is held until the second choice is made.
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  let first!: () => void;
  const firstSeen = new Promise<void>((r) => (first = r));
  let n = 0;
  await page.route("**/mods/plan?*", async (route) => {
    if (!route.request().headers()["hx-request"] || ++n > 1) return route.continue();
    first();
    await held;
    await route.continue().catch(() => {});
  });
  await version.selectOption("1.0.0");
  await firstSeen;
  await version.selectOption("2.0.0");
  release();

  await expect(page.locator("#mod-plan .plan-groups")).toContainText("fp-race 2.0.0");
  await expect(page.locator("#announcer")).toHaveText("Plan updated: 1 mod changes.");
  // Wait out any late swap, then check the choice stayed.
  await page.waitForTimeout(1000);
  await expect(page.getByLabel("Version of fp-race")).toHaveValue("2.0.0");
  await expect(page.locator("#mod-plan .plan-groups")).toContainText("fp-race 2.0.0");
  await checkPage(page);
});

// Clicks from the Mods tab: Change version, the version, Change 1 mod (SPEC 17.10).
test("Change version goes back after a bad update, with a snapshot first", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-back", "1.0.0", ["base >= 2.0"]);
  await portal.add("fp-back", "1.1.0", ["base >= 2.0"]);
  // A long name with an update, so the row with every action is seen at each width.
  const long = "fp-a-rather-long-mod-name-for-the-row";
  await portal.add(long, "1.0.0", ["base >= 2.0"]);
  await portal.add(long, "1.10.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, {
    ...world,
    mods: { "fp-back": { factorio: "2.0", version: "1.1.0", deps: ["base >= 2.0"] }, [long]: { factorio: "2.0", version: "1.0.0", deps: ["base >= 2.0"] } },
  });

  await page.goto("/worlds/base/mods");
  const table = page.getByRole("table", { name: /Mods of Base/ });
  const row = table.getByRole("row", { name: /fp-back/ });
  await expect(table.getByRole("row", { name: new RegExp(long) }).getByRole("link", { name: "Update to 1.10.0" })).toBeVisible(live);
  const longRow = table.getByRole("row", { name: new RegExp(long) });
  await longRow.locator("details.row-menu > summary").click();
  await expect(longRow.getByRole("link", { name: "Change version " + long, exact: true })).toBeVisible();
  // Update stays on the row; Change version leads the row's menu, before Disable and Remove.
  await expect(longRow.locator(".mod-actions a")).toHaveText([`Update to 1.10.0 ${long}`, `Change version ${long}`, `Disable ${long}`, `Remove ${long}`]);
  await checkPage(page);
  await shot(page, "mods-version-tab");
  await row.locator("details.row-menu > summary").click();
  await row.getByRole("link", { name: "Change version fp-back", exact: true }).click();

  await expect(page.getByRole("heading", { level: 1, name: "Change the version of fp-back in Base?" })).toBeVisible();
  const version = page.getByLabel("Version of fp-back");
  await expect(version.locator("option:checked")).toHaveText("1.1.0 (installed)");
  await expect(page.locator("#plan-same")).toHaveText("Nothing changes at this version. Choose another version to see what would change.");
  await expect(page.locator("#act-apply")).toHaveCount(0);
  await checkPage(page);
  await shot(page, "mods-version-held");

  await version.selectOption("1.0.0");
  await expect(page.locator("#mod-plan .plan-groups")).toContainText("fp-back 1.1.0 to 1.0.0");
  await expect(page.locator("#plan-steps")).toBeVisible();
  await expect(page.locator(".plan-steps")).toContainText('Take a snapshot labeled "Before mod change".');
  await checkPage(page);
  await shot(page, "mods-version-back");

  await page.getByRole("button", { name: "Change 1 mod" }).click();
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  expect(existsSync(modsFile(foreman.dataDir, "fp-back_1.0.0.zip"))).toBe(true);
  expect(existsSync(modsFile(foreman.dataDir, "fp-back_1.1.0.zip"))).toBe(false);
});

test("a version the portal has not got is a conflict with no apply button", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-odd", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, world);
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-odd&version-fp-odd=9.9.9");
  await expect(page.locator("#plan-conflicts")).toContainText("The mod portal has no release 9.9.9 of fp-odd for this game version. Choose another version.");
  await expect(page.locator("#act-apply")).toHaveCount(0);
  // The list says what the plan holds, and offers the way out.
  await expect(page.getByLabel("Version of fp-odd").locator("option:checked")).toHaveText("9.9.9");
  await page.getByLabel("Version of fp-odd").selectOption("");
  await expect(page.locator("#act-apply")).toHaveText("Install 1 mod");
  await checkPage(page);
});

// SPEC 9.3: the tab draws no Change version until the portal status says a
// different release exists, so nothing appears and then slides away under a
// thumb. A mod with one release on the line, or one the portal lacks, never
// gets it; a mod with another release gets it when the status arrives.
test("Change version appears with the status, only for a mod with another release", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-two", "1.0.0", ["base >= 2.0"]);
  await fakePortal!.add("fp-two", "1.1.0", ["base >= 2.0"]);
  await fakePortal!.add("fp-solo", "1.0.0", ["base >= 2.0"]);
  await fakePortal!.add("fp-solo", "0.9.0", ["base >= 2.1"], "2.1");
  await setUpModsWorld(page, foreman, fakeGame!, {
    ...world,
    mods: {
      "fp-two": { factorio: "2.0", version: "1.1.0", deps: ["base >= 2.0"] },
      "fp-solo": { factorio: "2.0", version: "1.0.0", deps: ["base >= 2.0"] },
      "zz-private": "2.0",
    },
  });
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  await page.route("**/mods/status", async (route) => {
    await held;
    await route.continue();
  });
  await page.goto("/worlds/base/mods");
  const change = (n: string) => page.getByRole("link", { name: "Change version " + n, exact: true });
  await expect(page.getByRole("row", { name: /fp-two/ })).toBeVisible();
  await expect(page.getByRole("link", { name: /^Change version/ })).toHaveCount(0);
  release();
  await expect(change("fp-two")).toHaveCount(1, live);
  await page.getByRole("row", { name: /fp-two/ }).locator("details.row-menu > summary").click();
  await expect(change("fp-two")).toBeVisible();
  await expect(change("fp-solo")).toHaveCount(0);
  await expect(change("zz-private")).toHaveCount(0);
  await checkPage(page);
});

// SPEC 17: at 320 px and 200% text a row with every action keeps each one
// inside its panel, and the page does not scroll sideways.
test("a row with every action fits at large text", async ({ page, foreman, fakeGame, fakePortal }) => {
  const long = "fp-a-rather-long-mod-name-for-the-row";
  await fakePortal!.add(long, "1.0.0", ["base >= 2.0"]);
  await fakePortal!.add(long, "1.10.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { [long]: { factorio: "2.0", version: "1.0.0", deps: ["base >= 2.0"] } } });
  // A phone's width in every project, so the desktop runs cannot pass on room alone.
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto("/worlds/base/mods");
  await page.addStyleTag({ content: "html { font-size: 200% }" });
  const row = page.getByRole("row", { name: new RegExp(long) });
  await expect(row.getByRole("link", { name: "Update to 1.10.0 " + long })).toBeVisible(live);
  const panel = (await page.locator("section.panel").filter({ has: row }).boundingBox())!;
  await row.locator("details.row-menu > summary").click();
  for (const name of ["Update to 1.10.0 ", "Change version ", "Disable ", "Remove "]) {
    const b = (await row.getByRole("link", { name: name + long, exact: true }).boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(panel.x - 0.5);
    expect(b.x + b.width).toBeLessThanOrEqual(panel.x + panel.width + 0.5);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/mods-version-large-text-${test.info().project.name}.png`, fullPage: true });
});
