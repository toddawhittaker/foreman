import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live, version20, type FakeGame, type FakePortal, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.3 (updates) and 9.8 (mods players cannot get, issue 131), through the
// fake portal. The fake's generated mods are made for Factorio 2.0.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const modsFile = (dataDir: string, file: string) => join(dataDir, "worlds", "base", "mods", file);

test.describe("updates", () => {
  async function seed(page: Page, foreman: Foreman, g: FakeGame, portal: FakePortal) {
    for (const [name, versions] of Object.entries({ "fp-up1": ["1.0.0", "1.1.0"], "fp-up2": ["1.0.0", "1.2.0"], "fp-same": ["1.0.0"] })) {
      for (const v of versions) await portal.add(name, v, ["base >= 2.0"]);
    }
    await setUpModsWorld(page, foreman, g, { ...world, mods: { "fp-up1": "2.0", "fp-up2": "2.0", "fp-same": "2.0" } });
  }

  test("update all: the status says how many, one plan changes them, and the old files go", async ({ page, foreman, fakeGame, fakePortal }) => {
    await seed(page, foreman, fakeGame!, fakePortal!);
    await page.goto("/worlds/base/mods");
    const status = page.locator("#mod-status");
    await expect(status).toContainText("2 mods have an update for this game version.", live);
    await expect(page.getByRole("link", { name: "Update to 1.1.0" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Update to 1.2.0" })).toBeVisible();
    await checkPage(page);

    await page.getByRole("link", { name: "Update all" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Update the mods of Base?" })).toBeVisible();
    const plan = page.locator("#mod-plan");
    await expect(plan).toContainText("fp-up1 1.0.0 to 1.1.0");
    await expect(plan).toContainText("fp-up2 1.0.0 to 1.2.0");
    await expect(plan).not.toContainText("fp-same 1.0.0 to");
    await checkPage(page);

    await page.getByRole("button", { name: "Update 2 mods" }).click();
    // The world is stopped, so it stays stopped.
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    await expect(stateBadge(page)).toHaveText("Stopped");
    expect(existsSync(modsFile(foreman.dataDir, "fp-up1_1.1.0.zip"))).toBe(true);
    expect(existsSync(modsFile(foreman.dataDir, "fp-up2_1.2.0.zip"))).toBe(true);
    expect(existsSync(modsFile(foreman.dataDir, "fp-up1_1.0.0.zip"))).toBe(false);
    expect(existsSync(modsFile(foreman.dataDir, "fp-same_1.0.0.zip"))).toBe(true);

    await page.goto("/worlds/base/mods");
    await expect(page.locator("#mod-status")).toContainText("All 3 mods from the mod portal are up to date.", live);
    expect((await fakePortal!.state()).refused).toEqual([]);
  });

  test("one row's Update appears when the portal status loads, not before", async ({ page, foreman, fakeGame, fakePortal }) => {
    await seed(page, foreman, fakeGame!, fakePortal!);
    // Hold the status reply, so the page is seen as it first loads.
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/worlds/base/mods/status", async (route) => {
      await held;
      await route.continue();
    });
    await page.goto("/worlds/base/mods");
    const row = page.getByRole("table", { name: /Mods of Base/ }).getByRole("row", { name: /fp-up1/ });
    await expect(row).toBeVisible();
    await expect(page.locator("#mod-status")).toContainText("Checking the mod portal for updates");
    await expect(row.getByRole("link", { name: "Update to 1.1.0" })).toHaveCount(0);
    // The other buttons are there at once.
    await expect(row.getByRole("link", { name: "Disable" })).toBeVisible();

    release();
    await expect(row.getByRole("link", { name: "Update to 1.1.0" })).toBeVisible(live);
    await expect(row.getByRole("link", { name: "Update to 1.1.0" })).toHaveAttribute("href", /kind=update&name=fp-up1/);
    // The mod with no update has no Update button.
    await expect(page.getByRole("table", { name: /Mods of Base/ }).getByRole("row", { name: /fp-same/ }).getByRole("link", { name: /^Update/ })).toHaveCount(0);
    await checkPage(page);

    await row.getByRole("link", { name: "Update to 1.1.0" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Update fp-up1 in Base?" })).toBeVisible();
    await page.getByRole("button", { name: "Update 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    expect(existsSync(modsFile(foreman.dataDir, "fp-up1_1.1.0.zip"))).toBe(true);
    expect(existsSync(modsFile(foreman.dataDir, "fp-up2_1.0.0.zip"))).toBe(true);
  });
});

// Issue 131: the portal has no 1.5.5 of visible-planets, so players cannot download it.
test.describe("a mod players cannot get", () => {
  const spaceAge = { version: version20, fixture: "2.0.77-space-age.zip", spaceAge: true };
  const mods = {
    "visible-planets": { factorio: "2.0", version: "1.5.5", deps: ["base >= 2.0", "space-age >= 2.0"] },
    "chest-auto-sort": { factorio: "2.0", version: "1.3.0", deps: ["base"] },
  };

  test("the Mods tab flags it with its fix, the Server page says so, and Fix ends with the newer version", async ({ page, foreman, fakeGame, fakePortal }) => {
    const g = fakeGame!;
    const portal = fakePortal!;
    // Downloadable newer releases; with these added, the portal's /full lists no 1.5.5.
    await portal.add("visible-planets", "1.7.2", ["base >= 2.0", "space-age >= 2.0"]);
    // The game runs with the mod: it started while the portal could not be asked (SPEC 7.4).
    await portal.setDown(true);
    await setUpModsWorld(page, foreman, g, { ...spaceAge, mods, start: true });
    await portal.setDown(false);
    const before = await creates(g);

    // The Server page loads the notice after it opens.
    await page.goto("/");
    const notice = page.locator("#mods-notice");
    await expect(notice).toContainText("visible-planets 1.5.5 is gone from the mod portal, so players cannot join.", live);

    await page.goto("/worlds/base/mods");
    // Issue 342: one summary line, red only because players cannot join; each mod's news is in its row.
    const error = page.locator("#mod-status .message--error");
    await expect(error).toContainText("1 mod stops players joining. Its row says how to fix it.", live);
    await expect(error).toContainText("1 mod is deprecated on the mod portal.");
    const table = page.getByRole("table", { name: /Mods of Base/ });
    const gone = table.getByRole("row", { name: /visible-planets/ });
    await expect(gone).toContainText("Error: Players cannot join: the mod portal no longer has version 1.5.5");
    await expect(gone.getByRole("link", { name: "Update to 1.7.2 visible-planets" })).toBeVisible();
    // chest-auto-sort is marked deprecated on the portal: a warning in its row, with no error.
    const deprecated = table.getByRole("row", { name: /chest-auto-sort/ });
    await expect(deprecated).toContainText("Warning: Deprecated on the mod portal");
    await expect(deprecated).not.toContainText("Error:");
    await checkPage(page);

    await page.goto("/");
    await expect(notice).toContainText("cannot join", live);
    await notice.getByRole("link", { name: "Update visible-planets" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Update visible-planets in Base?" })).toBeVisible();
    await expect(page.locator("#mod-plan")).toContainText("visible-planets 1.5.5 to 1.7.2");
    await checkPage(page);
    await page.getByRole("button", { name: "Update 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    await expect(stateBadge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    expect(existsSync(modsFile(foreman.dataDir, "visible-planets_1.7.2.zip"))).toBe(true);
    expect(existsSync(modsFile(foreman.dataDir, "visible-planets_1.5.5.zip"))).toBe(false);

    // The error is gone from the tab; the deprecated warning stays.
    await page.goto("/worlds/base/mods");
    await expect(page.locator("#mod-status")).toContainText("1 mod is deprecated on the mod portal.", live);
    await expect(page.locator("#mod-status .message--error")).toHaveCount(0);
    await expect(page.getByRole("table", { name: /Mods of Base/ }).getByRole("row", { name: /visible-planets/ })).toContainText("1.7.2");
    expect((await portal.state()).refused).toEqual([]);
  });

  // SPEC 17.2: targets are at least 24 px, so the fix is a button under the sentence; the notice is a live region beside the facts.
  test("the Server page notice passes the page checks", async ({ page, foreman, fakeGame, fakePortal }) => {
    await fakePortal!.add("visible-planets", "1.7.2", ["base >= 2.0", "space-age >= 2.0"]);
    await setUpModsWorld(page, foreman, fakeGame!, { ...spaceAge, mods });
    const notice = page.locator("#mods-notice");
    await expect(notice).toContainText("cannot join", live);
    await expect(notice).toHaveAttribute("role", "status");
    // It sits with the status facts, before the players (DESIGN 7.3).
    const players = page.getByRole("heading", { level: 2, name: "Players online" });
    expect((await notice.boundingBox())!.y).toBeLessThan((await players.boundingBox())!.y);
    // The fix link has a button face, so it is a full target at any width and in any language (DESIGN 6, rule 3).
    const fix = (await notice.getByRole("link", { name: "Update visible-planets" }).boundingBox())!;
    expect(Math.min(fix.width, fix.height)).toBeGreaterThanOrEqual(44);
    await checkPage(page);
    await page.screenshot({ path: `../screenshots/mods-notice-${test.info().project.name}.png`, fullPage: true });
  });

  test("a world with nothing to fix shows no notice", async ({ page, foreman, fakeGame, fakePortal }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { ...spaceAge, mods: { "chest-auto-sort": mods["chest-auto-sort"] } });
    // The notice loads after the page; wait for the request to finish, then look.
    const answered = page.waitForResponse("**/worlds/base/mods/notice");
    await page.goto("/");
    await answered;
    await expect(page.locator("#mods-notice")).toBeEmpty();
    expect(await fakePortal!.count("mods.factorio.com", "/api/mods/chest-auto-sort/full")).toBeGreaterThan(0);
  });
});
