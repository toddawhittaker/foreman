import { test, expect, checkPage, setUpModsWorld, doneBox, live, makeZip, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.8 and 9.10: make a world's mods the ones its save was made with, and
// what the plan asks when the portal lacks a mod's version.
//
// The save is the real 2.1.24 save that uses foreman-test 0.1.0, with Space Age
// on. The fake portal's mods are made for Factorio 2.0, so a 2.1 world can offer
// no "nearest release"; the test of that choice uses a 2.0 world, which makes
// the save's built-in recycler unresolved too. The plan reads the save's mod
// list only and the game is stopped, so no start check compares game versions.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const save = "2.1.24-space-age-testmod.zip";
const world21 = { fixture: save, spaceAge: true };
const world20 = { version: version20, fixture: save, spaceAge: true };
const syncPlan = "/worlds/base/mods/plan?kind=sync";
const modsFolder = (dataDir: string) => join(dataDir, "worlds", "base", "mods");
const modsTable = (page: Page) => page.getByRole("table", { name: /Mods of Base/ });

// A zip of foreman-test, as the admin would upload it.
const testModZip = (version = "0.1.0") =>
  makeZip({ [`foreman-test_${version}/info.json`]: JSON.stringify({ name: "foreman-test", version, factorio_version: "2.1", dependencies: ["base"] }) });

test.describe("a save's mod the portal does not have", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, world21);
  });

  test("the plan offers upload first, with its zip field, and continue without", async ({ page }) => {
    await page.goto("/worlds/base/mods");
    await page.getByRole("link", { name: "Match mods to the save" }).click();
    await expect(page).toHaveURL(syncPlan);
    await expect(page.getByRole("heading", { level: 1, name: "Match the mods of Base to the save base.zip?" })).toBeVisible();
    const choices = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
    // The portal has nothing near this version for Factorio 2.1, so there is no "nearest" option.
    await expect(choices.getByRole("radio")).toHaveCount(2);
    await expect(choices.getByLabel("Upload the zip of foreman-test 0.1.0")).toBeChecked();
    await expect(choices.getByLabel("Continue without foreman-test")).not.toBeChecked();
    // Upload is the default and no zip is uploaded yet, so there is nothing to confirm.
    await expect(page.locator("#act-apply")).toHaveCount(0);
    await expect(page.getByLabel("Zip of foreman-test 0.1.0", { exact: true })).toBeVisible();
    await checkPage(page);
  });

  test("continue without: the box is required, and a post without it is a field error", async ({ page, foreman }) => {
    await page.goto(syncPlan);
    await page.getByLabel("Continue without foreman-test").check();
    const apply = page.locator("#act-apply");
    await expect(apply).toBeVisible();
    await expect(page.getByLabel("Upload the zip of foreman-test 0.1.0")).not.toBeChecked();
    await checkPage(page);

    await apply.click();
    const summary = page.locator("#error-summary");
    await expect(summary).toBeFocused();
    await expect(summary).toContainText("Tick the box to confirm that the game will delete what foreman-test added");
    const box = page.locator("#confirm-0");
    await expect(box).toHaveAttribute("aria-invalid", "true");
    await expect(box).not.toBeChecked();
    await expect(page.locator("#confirm-0-error")).toContainText("Tick the box");
    // The choice is kept across the refusal.
    await expect(page.getByLabel("Continue without foreman-test")).toBeChecked();
    await checkPage(page);

    // The summary's link takes focus to the box.
    await summary.getByRole("link").click();
    await expect(box).toBeFocused();
    await box.check();
    await page.locator("#act-apply").click();
    await expect(page.locator("#error-summary")).toHaveCount(0);
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    // The save's mod stays out of the world.
    expect(existsSync(join(modsFolder(foreman.dataDir), "foreman-test_0.1.0.zip"))).toBe(false);
    await page.goto("/worlds/base/mods");
    await expect(modsTable(page).getByRole("row", { name: /foreman-test/ })).toHaveCount(0);
  });

  test("an uploaded zip answers the choice in the plan, and is installed", async ({ page, foreman }) => {
    await page.goto(syncPlan);
    const form = page.locator("form.unresolved-upload");
    await form.getByLabel("Zip of foreman-test 0.1.0").setInputFiles({ name: "foreman-test_0.1.0.zip", mimeType: "application/zip", buffer: testModZip() });
    await form.getByRole("button", { name: "Upload zip" }).click();

    await expect(page.getByLabel("Use the uploaded zip of foreman-test 0.1.0")).toBeChecked();
    await expect(page.locator("#mod-plan")).toContainText("foreman-test 0.1.0, from the uploaded zip");
    await checkPage(page);
    await page.locator("#act-apply").click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    expect(readFileSync(join(modsFolder(foreman.dataDir), "foreman-test_0.1.0.zip")).equals(testModZip())).toBe(true);
    await page.goto("/worlds/base/mods");
    await expect(modsTable(page).getByRole("row", { name: /foreman-test/ })).toContainText("Enabled");
  });

  test("a zip of another mod is refused where it was sent, and the choice is kept", async ({ page }) => {
    await page.goto(syncPlan);
    const other = makeZip({ "other_1.0.0/info.json": JSON.stringify({ name: "other", version: "1.0.0", factorio_version: "2.1" }) });
    const form = page.locator("form.unresolved-upload");
    await form.getByLabel("Zip of foreman-test 0.1.0").setInputFiles({ name: "other_1.0.0.zip", mimeType: "application/zip", buffer: other });
    await form.getByRole("button", { name: "Upload zip" }).click();
    await expect(page.locator("#upload-0-error")).toContainText("This zip holds other, not foreman-test.");
    await expect(page.locator("#upload-0")).toHaveAttribute("aria-invalid", "true");
    await expect(page.locator("#act-apply")).toHaveCount(0);
    await checkPage(page);
  });

  // Known fault, fixed after this task: a refused zip for an unresolved mod
  // puts the error beside the field only, with no error summary (DESIGN 7.2).
  test.fail("a refused zip gets an error summary with focus on it", async ({ page }) => {
    await page.goto(syncPlan);
    const form = page.locator("form.unresolved-upload");
    await form.getByLabel("Zip of foreman-test 0.1.0").setInputFiles({ name: "junk.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
    await form.getByRole("button", { name: "Upload zip" }).click();
    await expect(page.locator("#error-summary")).toBeFocused({ timeout: 3_000 });
  });
});

test("the nearest release is one of three choices, and it is downloaded and installed", async ({ page, foreman, fakeGame, fakePortal }) => {
  // The portal has no foreman-test 0.1.0; 0.9.0 is the lowest release above it for Factorio 2.0.
  await fakePortal!.add("foreman-test", "0.9.0", ["base >= 2.0"]);
  await fakePortal!.add("foreman-test", "1.1.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, world20);
  await page.goto(syncPlan);
  const choices = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
  await expect(choices.getByRole("radio")).toHaveCount(3);
  await expect(choices.getByLabel("Upload the zip of foreman-test 0.1.0")).toBeChecked();
  await expect(choices.getByLabel("Continue without foreman-test")).not.toBeChecked();
  await choices.getByLabel("Use foreman-test 0.9.0, the nearest release for Factorio 2.0").check();
  await expect(page.locator("#mod-plan")).toContainText("foreman-test 0.9.0");
  // The save's built-in recycler is a 2.1 mod, which this 2.0 world lacks; it is dropped knowingly.
  await page.getByLabel("Continue without recycler").check();
  await page.locator("#confirm-1").check();
  await checkPage(page);

  await page.locator("#act-apply").click();
  await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
  expect(existsSync(join(modsFolder(foreman.dataDir), "foreman-test_0.9.0.zip"))).toBe(true);
  expect((await fakePortal!.state()).refused).toEqual([]);
});

// Known fault, fixed after this task: ticking an optional mod that conflicts
// makes the server answer 409, and htmx does not swap a 4xx reply, so the page
// keeps the old plan and the conflict is never shown.
test.fail("ticking an optional mod that conflicts shows the conflict", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-conf", "1.0.0", ["base >= 2.0", "! fp-other"]);
  await portal.add("fp-main", "1.0.0", ["base >= 2.0", "? fp-conf"]);
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { "fp-other": "2.0" } });
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main");
  await page.getByLabel("fp-conf, offered by fp-main").check();
  await expect(page.locator("#plan-conflicts")).toContainText("fp-conf cannot run together with fp-other.", { timeout: 3_000 });
});
