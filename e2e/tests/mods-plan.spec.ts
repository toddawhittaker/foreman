import { test, expect, planRow, checkPage, setUpModsWorld, doneBox, live, version20, openModTools } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.1, 9.2, 9.7, 9.11: building one plan from a search, the portal
// links, the built-in line, the settings help and the Server page notice,
// through the fake portal. The portal's generated mods are made for Factorio
// 2.0, so these worlds are 2.0 worlds.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const modsDir = (dataDir: string) => join(dataDir, "worlds", "base", "mods");
const snapshots = (dataDir: string) => readdirSync(join(dataDir, "snapshots", "base")).filter((n) => /^\d{8}T\d{6}Z/.test(n)).length;
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });

const hit = (page: Page, name: string) => page.locator(`#mod-${name}-hit`);
const panel = (page: Page) => page.locator("#plan-panel");

// Picks fp-lamp and fp-lamp-colours (which needs fp-lamp-lib) on the Mods tab.
async function pickTwo(page: Page) {
  await page.goto("/worlds/base/mods");
  await page.getByLabel("Mod name").fill("fp-lamp");
  for (const n of ["fp-lamp", "fp-lamp-colours"]) {
    await hit(page, n).getByRole("button", { name: `Add ${n} to the plan`, exact: true }).click();
    await expect(hit(page, n)).toContainText("In plan");
  }
}

test.describe("the review of picks from the Mods tab", () => {
  test.beforeEach(async ({ page, foreman, fakeGame, fakePortal }) => {
    const portal = fakePortal!;
    await portal.add("fp-lamp", "1.0.0", ["base >= 2.0"]);
    await portal.add("fp-lamp-colours", "1.2.0", ["base >= 2.0", "fp-lamp-lib >= 1.0.0"]);
    await portal.add("fp-lamp-lib", "1.0.0");
    await setUpModsWorld(page, foreman, fakeGame!, world);
  });

  test("the search page's title links to the mod's portal page", async ({ page }) => {
    await page.goto("/worlds/base/mods/search?q=fp-lamp");
    await checkPage(page);
    await shot(page, "mods-search");
    // A result's title is its portal page, in a new tab, and names its mod.
    const portalLink = page.getByRole("link", { name: "fp-lamp on the mod portal, opens in a new tab", exact: true });
    await expect(portalLink).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-lamp");
    await expect(portalLink).toHaveAttribute("target", "_blank");
    await expect(portalLink).toHaveAttribute("rel", "noopener noreferrer");
  });

  // SPEC 9.2, 9.14: Review opens the plan of the picks; adding more happens on
  // the Mods tab, so the page has no search; Cancel goes back to the tab where
  // the picks are still in the panel.
  test("review of the picks has no add search, and Cancel returns to the Mods tab with the picks kept", { tag: "@looks" }, async ({ page }) => {
    await pickTwo(page);
    await panel(page).getByRole("link", { name: "Review and install 3 mods" }).click();

    await expect(page.getByRole("heading", { level: 1, name: "Install mods in Base?" })).toBeVisible();
    await expect(planRow(page, "fp-lamp-lib")).toContainText("1.0.0 (fp-lamp-colours needs it)");
    await expect(page.getByRole("region", { name: "Add another mod" })).toHaveCount(0);
    // A stopped world's install is the primary action, not a danger (DESIGN 7.1).
    await expect(page.getByRole("button", { name: "Install 3 mods" })).toHaveClass(/button--primary/);
    await checkPage(page);
    await shot(page, "mods-plan-two");

    await page.getByRole("link", { name: "Cancel, change nothing" }).click();
    await expect(page).toHaveURL(/\/worlds\/base\/mods$/);
    await expect(panel(page).locator(".plan-panel__sum")).toHaveText("2 mods in the plan");
    await expect(panel(page).getByRole("link", { name: "Review and install 3 mods" })).toBeVisible();
  });

  test("the heading of a plan for one named mod is one run of text", { tag: "@looks" }, async ({ page }) => {
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-lamp");
    await expect(page.getByRole("heading", { level: 1, name: "Install fp-lamp in Base?" })).toBeVisible();
    // Issue 364: the name has one space each side and breaks like a word.
    const h1 = page.locator("#confirm-heading");
    expect(await h1.evaluate((el) => getComputedStyle(el).display)).toBe("block");
    expect(await h1.evaluate((el) => (el as HTMLElement).innerText)).toBe("Install fp-lamp in Base?");
  });

  // SPEC 9.2, 17.9: taking a mod out of the review redraws it in place, says so,
  // and puts focus on the list it left.
  test("taking a mod out of the review redraws the plan and says so", { tag: "@looks" }, async ({ page }) => {
    await pickTwo(page);
    await panel(page).getByRole("link", { name: "Review and install 3 mods" }).click();
    const asked = page.locator("#mod-plan .plan-groups");
    await asked.getByRole("link", { name: "Remove fp-lamp-colours from the plan" }).click();
    await expect(page.locator("#announcer")).toHaveText("Removed fp-lamp-colours. The plan has 1 mod.");
    await expect(page.locator("#plan-changes")).toBeFocused();
    await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
    await checkPage(page);
  });

  // Issue 620: each change names its mod by title, as the portal link; the
  // version list and the remove cross sit in the same row.
  test("each change's row holds its title link, version list and remove cross", { tag: "@looks" }, async ({ page }) => {
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-lamp&name=fp-lamp-colours");
    const row = page.locator("#mod-plan .plan-row", { has: page.getByRole("link", { name: "fp-lamp-colours on the mod portal, opens in a new tab" }) });
    await expect(row.getByRole("link", { name: "fp-lamp-colours on the mod portal, opens in a new tab" })).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-lamp-colours");
    await expect(row.getByLabel("Version of fp-lamp-colours")).toBeVisible();
    const cross = row.getByRole("link", { name: "Remove fp-lamp-colours from the plan" });
    const box = await cross.boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
    await expect(page.locator(".portal-link")).toHaveCount(0);
    await checkPage(page);
  });

  // SPEC 9.13, 9.14: one confirm installs every pick and what they need with one
  // snapshot, and the Mods tab's plan is empty afterwards.
  test("two picks: one install with one snapshot, then the panel is empty", { tag: "@looks" }, async ({ page, foreman }) => {
    await pickTwo(page);
    await panel(page).getByRole("link", { name: "Review and install 3 mods" }).click();
    await page.getByRole("button", { name: "Install 3 mods" }).click();
    await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    for (const f of ["fp-lamp_1.0.0.zip", "fp-lamp-colours_1.2.0.zip", "fp-lamp-lib_1.0.0.zip"]) expect(existsSync(join(modsDir(foreman.dataDir), f))).toBe(true);
    expect(snapshots(foreman.dataDir)).toBe(1);

    await page.goto("/worlds/base/mods");
    await expect(panel(page)).toContainText("No mods in the plan yet.");
    await page.getByLabel("Mod name").fill("fp-lamp");
    await expect(hit(page, "fp-lamp")).toContainText("Installed");
  });

  test.describe("without script", () => {
    // The accessibility scan needs script, so these pages are scanned in the tests with script.
    test.use({ javaScriptEnabled: false });

    test("each remove link is the plan without its mod", { tag: "@looks" }, async ({ page }) => {
      await page.goto("/worlds/base/mods/plan?kind=install&name=fp-lamp&name=fp-lamp-colours");
      await expect(page.getByRole("button", { name: "Install 3 mods" })).toBeVisible();
      await page.getByRole("link", { name: "Remove fp-lamp-colours from the plan" }).click();
      await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
      await page.getByRole("link", { name: "Cancel, change nothing" }).click();
      await expect(page).toHaveURL(/\/worlds\/base\/mods$/);
    });
  });
});

// A conflict takes focus on load; its way back is the Mods tab, not a search.
test("a conflict is focused and its way back is the Mods tab", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-conf", "1.0.0", ["base >= 2.0", "! fp-other"]);
  await fakePortal!.add("fp-main", "1.0.0", ["base >= 2.0", "? fp-conf"]);
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { "fp-other": "2.0" } });
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main&optional=fp-conf");
  await expect(page.locator("#plan-conflicts")).toBeFocused();
  await expect(page.locator("#plan-conflicts").getByRole("link", { name: "Back to the mods" })).toHaveAttribute("href", "/worlds/base/mods");
  await checkPage(page);
});

test("the Mods tab: built-in mods in one line, portal links, and where mod-settings.dat lives", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
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
  await expect(line).toHaveText("Built in, version 2.0.77: base, elevated-rails, quality, space-age. Space Age is on, so all of them are on. Change Space Age");
  await expect(line.getByRole("link", { name: "Change Space Age" })).toHaveAttribute("href", "/worlds/base/settings");
  const table = page.getByRole("table", { name: /Mods of Base/ });
  await expect(table.getByRole("row", { name: /space-age/ })).toHaveCount(0);
  // The mod's name is its portal link.
  await expect(table.getByRole("link", { name: "fp-here on the mod portal, opens in a new tab" })).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-here");
  // A mod the portal does not have gets no portal link once the portal has answered; its name stays.
  await expect(table.getByRole("link", { name: /fp-gone-from-portal on the mod portal/ })).toHaveCount(0);
  await expect(table.getByRole("row", { name: /fp-gone-from-portal/ })).toContainText("Note: Not on the mod portal");
  // The settings help is in the folded replace form.
  await openModTools(page);
  await page.locator("#mod-settings-replace > summary").click();
  await expect(page.locator("#settings-file-help")).toContainText("Windows %APPDATA%\\Factorio\\mods, macOS ~/Library/Application Support/factorio/mods, Linux ~/.factorio/mods.");
  await expect(page.locator("#settings-file-help")).toContainText("Startup settings apply when the world next starts.");
  // Mod names never break with a hyphen in a table cell (DESIGN 8.3).
  expect(await page.locator("td .ident").first().evaluate((el) => getComputedStyle(el).hyphens)).toBe("manual");
  // The summary line and the table share one width, so the tab is one column.
  const summary = (await page.locator("#mod-status .mod-status__line").boundingBox())!;
  const box = (await table.boundingBox())!;
  expect(Math.abs(summary.width - box.width)).toBeLessThan(2);
  await checkPage(page);
  await shot(page, "mods-tab-portal");
});

test("the Server page notice names the mod and its fix", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
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

// WCAG 4.1.2, 1.4.3: the fold of mods the plan needs and leaves as they are is scanned open.
test("the plan's fold of mods that stay as they are opens and passes the checks", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-user", "1.0.0", ["base >= 2.0", "fp-base-lib >= 1.0.0"]);
  await fakePortal!.add("fp-base-lib", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-base-lib": { factorio: "2.0", version: "1.0.0", deps: ["base >= 2.0"] } } });
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-user");
  const kept = page.locator("#mod-plan details.disclosure");
  await expect(kept.locator("summary")).toHaveText("1 other mod stays as it is");
  await expect(kept.getByText("fp-base-lib")).toBeHidden();
  await kept.locator("summary").click();
  await expect(kept.getByText("fp-base-lib")).toBeVisible();
  await checkPage(page);
});
