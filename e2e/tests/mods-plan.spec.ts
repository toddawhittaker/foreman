import { test, expect, checkPage, setUpModsWorld, doneBox, live, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.1, 9.2, 9.7, 9.11, #131: building one plan from a search, the portal
// links, the built-in line, the settings help and the Server page notice,
// through the fake portal. The portal's generated mods are made for Factorio
// 2.0, so these worlds are 2.0 worlds.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const modsDir = (dataDir: string) => join(dataDir, "worlds", "base", "mods");
const snapshots = (dataDir: string) => readdirSync(join(dataDir, "snapshots", "base")).filter((n) => /^\d{8}T\d{6}Z/.test(n)).length;
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });

test("two mods from one search: added in place, then one install with one snapshot", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-lamp", "1.0.0", ["base >= 2.0"]);
  await portal.add("fp-lamp-colours", "1.2.0", ["base >= 2.0", "fp-lamp-lib >= 1.0.0"]);
  await portal.add("fp-lamp-lib", "1.0.0");
  await setUpModsWorld(page, foreman, fakeGame!, world);

  await page.goto("/worlds/base/mods/search?q=fp-lamp");
  await checkPage(page);
  await shot(page, "mods-search");
  // A search result goes to the plan; the portal page opens in a new tab and names its mod.
  const portalLink = page.getByRole("link", { name: "Mod portal for fp-lamp, opens in a new tab", exact: true });
  await expect(portalLink).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-lamp");
  await expect(portalLink).toHaveAttribute("target", "_blank");
  await expect(portalLink).toHaveAttribute("rel", "noopener noreferrer");
  await page.getByRole("link", { name: "Add to plan fp-lamp", exact: true }).click();

  await expect(page.getByRole("heading", { level: 1, name: "Install fp-lamp in Base?" })).toBeVisible();
  const asked = page.getByRole("list", { name: "Mods you asked for:" });
  await expect(asked).toContainText("fp-lamp 1.0.0");
  // A stopped world's install is the primary action, not a danger (DESIGN 7.1).
  await expect(page.getByRole("button", { name: "Install 1 mod" })).toHaveClass(/button--primary/);
  // The same search waits under the plan, after the confirm button.
  const add = page.getByRole("region", { name: "Add another mod" });
  await expect(add.getByLabel("Mod name")).toHaveValue("fp-lamp");
  await expect(add.getByText("In plan")).toBeVisible();
  const apply = (await page.getByRole("button", { name: "Install 1 mod" }).boundingBox())!;
  expect(apply.y).toBeLessThan((await add.boundingBox())!.y);
  await checkPage(page);

  // Adding redraws the plan in place, says so, and leaves focus where it was.
  await add.getByRole("link", { name: "Add to plan fp-lamp-colours", exact: true }).click();
  await expect(page.locator("#announcer")).toHaveText("Added fp-lamp-colours. The plan has 3 mods.");
  await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeVisible();
  await expect(page.locator("#mod-fp-lamp-colours-add")).toBeFocused();
  await expect(page.locator("#mod-fp-lamp-colours-add")).toHaveText("In plan");
  await expect(page).toHaveURL(/name=fp-lamp&name=fp-lamp-colours/);
  await expect(page.locator("#mod-plan")).toContainText("fp-lamp-lib 1.0.0 (fp-lamp-colours needs it)");
  await expect(asked.getByRole("link", { name: "Remove from plan fp-lamp-colours" })).toBeVisible();
  await checkPage(page);
  await shot(page, "mods-plan-two");

  // Taking one out redraws again and puts focus on the list.
  await asked.getByRole("link", { name: "Remove from plan fp-lamp-colours" }).click();
  await expect(page.locator("#announcer")).toHaveText("Removed fp-lamp-colours. The plan has 1 mod.");
  await expect(page.locator("#asked-heading")).toBeFocused();
  await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
  await add.getByRole("link", { name: "Add to plan fp-lamp-colours", exact: true }).click();

  // One confirm, one snapshot, every mod installed.
  await page.getByRole("button", { name: "Install 3 mods" }).click();
  await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
  for (const f of ["fp-lamp_1.0.0.zip", "fp-lamp-colours_1.2.0.zip", "fp-lamp-lib_1.0.0.zip"]) expect(existsSync(join(modsDir(foreman.dataDir), f))).toBe(true);
  expect(snapshots(foreman.dataDir)).toBe(1);
});

test.describe("without script", () => {
  // The accessibility scan needs script, so these pages are scanned in the tests with script.
  test.use({ javaScriptEnabled: false });

  test("each add and remove link is the plan with or without its mod", async ({ page, foreman, fakeGame, fakePortal }) => {
    await fakePortal!.add("fp-a1", "1.0.0");
    await fakePortal!.add("fp-a2", "1.0.0");
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-a1&q=fp-a");
    await page.getByRole("link", { name: "Add to plan fp-a2", exact: true }).click();
    await expect(page.getByRole("button", { name: "Install 2 mods" })).toBeVisible();
    await page.getByRole("link", { name: "Remove from plan fp-a2" }).click();
    await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
    // The add search is a plain form that keeps the plan.
    await page.getByRole("region", { name: "Add another mod" }).getByLabel("Mod name").fill("fp-a2");
    await page.getByRole("region", { name: "Add another mod" }).getByRole("button", { name: "Search" }).click();
    await expect(page).toHaveURL(/name=fp-a1.*q=fp-a2#add-heading/);
    await expect(page.getByRole("link", { name: "Add to plan fp-a2", exact: true })).toBeVisible();
  });
});

test("the Mods tab: built-in mods in one line, portal links, and where mod-settings.dat lives", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-here", "1.0.0");
  await setUpModsWorld(page, foreman, fakeGame!, {
    version: version20,
    fixture: "2.0.77-space-age.zip",
    spaceAge: true,
    mods: { "fp-here": "2.0", "fp-gone-from-portal": "2.0" },
  });
  await page.goto("/worlds/base/mods");
  await expect(page.locator("#mod-status")).not.toContainText("Checking the mod portal", live);
  const line = page.locator("#mods-built-in");
  await expect(line).toHaveText("Built in, version 2.0.77: base, elevated-rails, quality, space-age. They follow Space Age. Change Space Age");
  await expect(line.getByRole("link", { name: "Change Space Age" })).toHaveAttribute("href", "/worlds/base/settings");
  const table = page.getByRole("table", { name: /Mods of Base/ });
  await expect(table.getByRole("row", { name: /space-age/ })).toHaveCount(0);
  await expect(table.getByRole("link", { name: "Mod portal for fp-here, opens in a new tab" })).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-here");
  // A mod the portal does not have gets no portal link once the portal has answered.
  await expect(table.getByRole("link", { name: /Mod portal for fp-gone-from-portal/ })).toHaveCount(0);
  await expect(page.locator("#settings-file-help")).toContainText("Windows %APPDATA%\\Factorio\\mods, macOS ~/Library/Application Support/factorio/mods, Linux ~/.factorio/mods.");
  await expect(page.locator("#settings-file-help")).toContainText("Startup settings apply when the world next starts.");
  // Mod names never break with a hyphen, in a table cell or in a button (DESIGN 8.3).
  expect(await page.locator("td .ident").first().evaluate((el) => getComputedStyle(el).hyphens)).toBe("manual");
  expect(await page.locator("#mod-status .button .ident").first().evaluate((el) => getComputedStyle(el).hyphens)).toBe("manual");
  // The notices and the table share one width, so the tab is one column.
  const notice = (await page.locator("#mod-status .message").first().boundingBox())!;
  const box = (await table.boundingBox())!;
  expect(Math.abs(notice.width - box.width)).toBeLessThan(2);
  await checkPage(page);
  await shot(page, "mods-tab-portal");
});

test("the Server page notice names the mod and its fix", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("visible-planets", "1.7.2", ["base >= 2.0", "space-age >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, {
    version: version20,
    fixture: "2.0.77-space-age.zip",
    spaceAge: true,
    mods: { "visible-planets": { factorio: "2.0", version: "1.5.5", deps: ["base >= 2.0", "space-age >= 2.0"] } },
  });
  const notice = page.locator("#mods-notice");
  await expect(notice).toContainText("visible-planets 1.5.5 is gone from the mod portal, so players cannot join.", live);
  const fix = notice.getByRole("link", { name: "Update visible-planets" });
  await expect(fix).toHaveAttribute("href", "/worlds/base/mods/plan?kind=update&name=visible-planets");
  // The name joins the button's line, space and all, rather than becoming a flex item of its own.
  expect(await fix.evaluate((a) => getComputedStyle(a.querySelector(".ident")!).display)).toBe("contents");
  await checkPage(page);
  await shot(page, "mods-server-notice");
});
