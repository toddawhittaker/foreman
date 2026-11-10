import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live, version20, type FakeGame, type FakePortal, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.1, 9.2, 9.7, 9.8, 9.12: search, the plan, and the install on a running
// world, through the fake portal. The portal's generated mods are made for
// Factorio 2.0, so these worlds are 2.0 worlds.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };

const modsTable = (page: Page) => page.getByRole("table", { name: /Mods of Base/ });

test("search, install and confirm: progress, then Done on the Server page, the game restarted", async ({ page, foreman, fakeGame, fakePortal }) => {
  const g = fakeGame!;
  const portal = fakePortal!;
  await portal.add("fp-signs", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, g, { ...world, start: true });
  const before = await creates(g);
  // A player online makes the stop count down, which holds the install while the page shows its steps;
  // with nobody online the fake game restarts so fast that the steps can be gone before the page loads.
  await g.call("/join", { name: "foreman-base", player: "alice" });
  await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);

  await page.getByRole("link", { name: "Mods", exact: true }).click();
  await expect(page).toHaveURL("/worlds/base/mods");
  await expect(page.locator("#mod-status")).not.toContainText("Checking the mod portal", live);
  await checkPage(page);

  await page.getByLabel("Mod name").fill("fp-signs");
  await page.getByRole("button", { name: "Search" }).click();
  await expect(page.getByRole("table", { name: "1 mod for Factorio 2.0 matches." })).toBeVisible();
  await expect(page.getByRole("checkbox", { name: "fp-signs" })).toBeVisible();
  await checkPage(page);

  await page.getByRole("link", { name: "Add to plan fp-signs", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install fp-signs in Base?" })).toBeVisible();
  await expect(page.locator("#mod-plan")).toContainText("fp-signs 1.0.0");
  // A running game: the plan says it will warn, stop and start it again, and asks for the countdown.
  const steps = page.locator("ol.plan-steps");
  await expect(steps.getByRole("listitem")).toHaveText([
    "Download 1 mod from the mod portal and check its checksum.",
    "Warn the players, then save and stop Base.",
    'Take a snapshot labeled "Before mod change".',
    "Change the mods, then start Base again.",
  ]);
  await expect(page.locator("#countdown")).toBeVisible();
  // Issue 375: an install destroys nothing, so it is the primary action, not red, while the game runs too.
  await expect(page.getByRole("button", { name: "Install 1 mod" })).toHaveClass(/button--primary/);
  await expect(page.locator("section.confirm")).toHaveClass(/confirm--plain/);
  // Issue 368: the undo is the one-click Undo the result offers, not a trip to another tab.
  await expect(page.locator("#mod-plan")).toContainText("a one-click Undo appears with the result");
  await expect(page.locator("#mod-plan")).not.toContainText("Snapshots tab");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/i-plan-install-running-${test.info().project.name}.png`, fullPage: true });

  // Long enough for the Server page to load under a busy machine; the steps show only while the install runs.
  await page.getByLabel("Countdown in seconds").fill("10");
  await page.getByRole("button", { name: "Install 1 mod" }).click();
  await expect(page).toHaveURL("/");
  // One look at the list while it runs: the download done, the warning counting down.
  const running = page.locator("ol.steps").filter({ hasText: /Done Download the mods/ }).filter({ hasText: /Warn players: \d+ seconds left/ });
  await expect(running).toBeVisible(live);
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  await expect(stateBadge(page)).toHaveText("Running", live);
  // A fresh container for the restart.
  expect(await creates(g)).toBe(before + 1);
  await checkPage(page);

  await page.goto("/worlds/base/mods");
  const row = modsTable(page).getByRole("row", { name: /fp-signs/ });
  await expect(row).toContainText("1.0.0");
  await expect(row).not.toContainText("Disabled");
  // Foreman reached the portal through the fake's proxy, and nothing else was asked for.
  expect(await portal.count("mods.factorio.com", "/api/mods")).toBeGreaterThan(0);
  expect(await portal.count("dl-mod.factorio.com", "/cdn/")).toBe(1);
  expect((await portal.state()).refused).toEqual([]);
});

test("a chain three levels deep: the plan lists what is needed and why, and all three end installed", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-c", "1.0.0");
  await portal.add("fp-b", "1.0.0", ["base >= 2.0", "fp-c >= 1.0.0"]);
  await portal.add("fp-a", "1.0.0", ["base >= 2.0", "fp-b >= 1.0.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, world);

  await page.goto("/worlds/base/mods/search?q=fp-a");
  await page.getByRole("link", { name: "Add to plan fp-a", exact: true }).click();
  const plan = page.locator("#mod-plan");
  await expect(plan).toContainText("fp-a 1.0.0");
  await expect(plan).toContainText("fp-b 1.0.0 (fp-a needs it)");
  await expect(plan).toContainText("fp-c 1.0.0 (fp-b needs it)");
  await checkPage(page);

  await page.getByRole("button", { name: "Install 3 mods" }).click();
  await expect(doneBox(page)).toContainText(/Done: Installed fp-\w, fp-\w and fp-\w in Base\. Foreman took the snapshot "Before mod change" first\./, live);
  await page.goto("/worlds/base/mods");
  for (const name of ["fp-a", "fp-b", "fp-c"]) await expect(modsTable(page).getByRole("row", { name: new RegExp(name) })).not.toContainText("Disabled");
  for (const name of ["fp-a", "fp-b", "fp-c"]) expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", `${name}_1.0.0.zip`))).toBe(true);
  expect((await portal.state()).refused).toEqual([]);
});

// SPEC 9.2: a "+" dependency starts ticked and can be unticked; one the portal lacks does not block the plan.
test("a recommended dependency starts ticked, and one the portal lacks is left out", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-lane", "1.3.1");
  await fakePortal!.add("fp-k2", "2.1.4", ["base >= 2.0", "+ fp-lane >= 1.3.0", "+ fp-gone"]);
  await setUpModsWorld(page, foreman, fakeGame!, world);
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-k2");
  const box = page.getByLabel("fp-lane, recommended by fp-k2");
  await expect(box).toBeChecked();
  await expect(page.locator("#plan-unavailable")).toContainText("fp-gone, recommended by fp-k2, is left out");
  await expect(page.getByRole("button", { name: "Install 2 mods" })).toBeVisible();
  await checkPage(page);

  await box.uncheck();
  await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
  await expect(box).not.toBeChecked();
  await expect(box).toBeFocused();
  await box.check();
  await page.getByRole("button", { name: "Install 2 mods" }).click();
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "fp-k2_2.1.4.zip"))).toBe(true);
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "fp-lane_1.3.1.zip"))).toBe(true);
});

test.describe("an optional dependency", () => {
  async function addMods(portal: FakePortal) {
    await portal.add("fp-opt", "1.0.0");
    await portal.add("fp-main", "1.0.0", ["base >= 2.0", "? fp-opt"]);
  }

  test("ticked: the plan is drawn again at once, with script", async ({ page, foreman, fakeGame, fakePortal }) => {
    await addMods(fakePortal!);
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main");
    const box = page.getByLabel("fp-opt, offered by fp-main");
    await expect(box).not.toBeChecked();
    await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
    await checkPage(page);

    await box.check();
    await expect(page.getByRole("button", { name: "Install 2 mods" })).toBeVisible();
    await expect(page.locator("#mod-plan")).toContainText("fp-opt 1.0.0 (offered by fp-main)");
    // The redraw keeps the ticked box, and focus stays on it.
    await expect(box).toBeChecked();
    await expect(box).toBeFocused();
    // Script redraws the plan without a page load, so there is no Recalculate button.
    await expect(page.getByRole("button", { name: "Recalculate the plan" })).toBeHidden();
    await checkPage(page);
  });

  // SPEC 9.2: the admin includes optional dependencies one by one. A ticked offer stays in the redrawn plan,
  // ticked, so the post carries it and the server's plan is the one shown.
  test("a ticked optional mod is installed when the plan is confirmed", async ({ page, foreman, fakeGame, fakePortal }) => {
    await addMods(fakePortal!);
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main");
    await page.getByLabel("fp-opt, offered by fp-main").check();
    await expect(page.getByLabel("fp-opt, offered by fp-main")).toBeChecked();
    await page.getByRole("button", { name: "Install 2 mods" }).click();
    await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    for (const name of ["fp-main", "fp-opt"]) expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", `${name}_1.0.0.zip`))).toBe(true);
  });

  test.describe("without script", () => {
    // The accessibility scan needs script, so these pages are scanned in the tests with script.
    test.use({ javaScriptEnabled: false });

    async function recalculate(page: Page, foreman: Foreman, g: FakeGame, portal: FakePortal) {
      await addMods(portal);
      await setUpModsWorld(page, foreman, g, world);
      await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main");
      await page.getByLabel("fp-opt, offered by fp-main").check();
      await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
      await page.getByRole("button", { name: "Recalculate the plan" }).click();
      await expect(page.getByRole("button", { name: "Install 2 mods" })).toBeVisible();
      await expect(page.locator("#mod-plan")).toContainText("fp-opt 1.0.0 (offered by fp-main)");
    }

    test("Recalculate draws the plan again", async ({ page, foreman, fakeGame, fakePortal }) => {
      await recalculate(page, foreman, fakeGame!, fakePortal!);
      await expect(page).toHaveURL(/optional=fp-opt/);
    });

    // The same, reached with Recalculate.
    test("a ticked optional mod is installed when the plan is confirmed", async ({ page, foreman, fakeGame, fakePortal }) => {
      await recalculate(page, foreman, fakeGame!, fakePortal!);
      await expect(page.getByLabel("fp-opt, offered by fp-main")).toBeChecked();
      await page.getByRole("button", { name: "Install 2 mods" }).click();
      await expect(page).toHaveURL("/", live);
      // Without script the page does not update itself; load it until the change has finished.
      await expect(async () => {
        await page.reload();
        await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, { timeout: 500 });
      }).toPass(live);
      for (const name of ["fp-main", "fp-opt"]) expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", `${name}_1.0.0.zip`))).toBe(true);
    });
  });
});

test("an incompatibility is a conflict whose fix removes the other mod in the same plan", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-clash", "1.0.0", ["base >= 2.0", "! fp-other"]);
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-other": "2.0" } });

  await page.goto("/worlds/base/mods/search?q=fp-clash");
  await page.getByRole("link", { name: "Add to plan fp-clash", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install fp-clash in Base?" })).toBeVisible();
  const conflicts = page.locator("#plan-conflicts");
  await expect(conflicts).toContainText("This plan cannot be applied because of a conflict:");
  await expect(conflicts).toContainText("fp-clash cannot run together with fp-other.");
  await expect(page.getByRole("button", { name: /^Install/ })).toHaveCount(0);
  await expect(page.locator("#act-apply")).toHaveCount(0);
  // Issue 344: a plan that cannot be applied does not say what applying does, and offers its fix and a way back.
  await expect(page.locator("#plan-steps")).toHaveCount(0);
  await expect(conflicts.getByRole("link", { name: "Back to search" })).toHaveAttribute("href", "/worlds/base/mods/search?q=fp-clash");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/i-plan-conflict-${test.info().project.name}.png`, fullPage: true });
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "fp-clash_1.0.0.zip"))).toBe(false);
  expect(readFileSync(join(foreman.dataDir, "worlds", "base", "mods", "mod-list.json"), "utf8")).not.toContain("fp-clash");

  // The fix redraws the plan in place with fp-other removed too, says so, and puts focus on the changes.
  await conflicts.getByRole("link", { name: "Remove fp-other as part of this plan" }).click();
  await expect(page.locator("#announcer")).toHaveText("Plan updated. It changes 2 mods.");
  await expect(page.getByRole("heading", { level: 1, name: "Change the mods of Base?" })).toBeVisible();
  await expect(page.locator("#plan-changes")).toBeFocused();
  await expect(page.locator("#plan-conflicts")).toHaveCount(0);
  await expect(page.locator(".plan-groups")).toContainText("Remove");
  await expect(page.locator(".plan-groups")).toContainText("fp-other 1.0.0");
  // Removing a mod destroys something, so this one is red.
  await expect(page.getByRole("button", { name: "Change 2 mods" })).toHaveClass(/button--danger/);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/i-plan-conflict-fixed-${test.info().project.name}.png`, fullPage: true });

  // One confirm: one installed, the other gone.
  await page.getByRole("button", { name: "Change 2 mods" }).click();
  await expect(doneBox(page)).toContainText(/Changed 2 mods in Base: .*\. Foreman took the snapshot "Before mod change" first\./, live);
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "fp-clash_1.0.0.zip"))).toBe(true);
  expect(readdirSync(join(foreman.dataDir, "worlds", "base", "mods")).filter((f) => f.startsWith("fp-other"))).toEqual([]);
});

// Issue 344: removing a mod another enabled mod needs offers "Disable it too", which redraws the plan with both.
test("removing a needed mod offers to disable the one that needs it, in one change", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-lib": "2.0", "fp-needer": { factorio: "2.0", deps: ["fp-lib"] } } });
  await page.goto("/worlds/base/mods");
  await modsTable(page).getByRole("row", { name: /fp-lib/ }).getByRole("link", { name: "Remove" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Remove fp-lib from Base?" })).toBeVisible();
  const conflicts = page.locator("#plan-conflicts");
  await expect(conflicts).toContainText("fp-needer needs fp-lib.");
  await expect(page.locator("#plan-steps")).toHaveCount(0);
  await checkPage(page);

  await conflicts.getByRole("link", { name: "Disable fp-needer too" }).click();
  await expect(page.locator("#plan-changes")).toBeFocused();
  await expect(page.locator(".plan-groups")).toContainText("Disable");
  await expect(page.locator(".plan-groups")).toContainText("fp-needer 1.0.0");
  await expect(page.locator("#plan-steps")).toBeVisible();
  await checkPage(page);
  await page.getByRole("button", { name: "Change 2 mods" }).click();
  await expect(doneBox(page)).toContainText(/Changed 2 mods in Base: 1 disabled, 1 removed\. Foreman took the snapshot "Before mod change" first\./, live);
  const list = JSON.parse(readFileSync(join(foreman.dataDir, "worlds", "base", "mods", "mod-list.json"), "utf8")).mods as { name: string; enabled: boolean }[];
  expect(readdirSync(join(foreman.dataDir, "worlds", "base", "mods")).filter((f) => f.startsWith("fp-lib"))).toEqual([]);
  expect(list.find((m) => m.name === "fp-needer")?.enabled).toBe(false);
});
