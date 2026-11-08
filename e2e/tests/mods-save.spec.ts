import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live } from "./foreman.js";
import { copyFileSync, mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.4, 9.8, 9.10, 17.5, 17.9: a world whose save uses a mod. Turning the
// mod off drops what it built, so the plan asks first; a start the check
// refuses, and an import, lead to the plan that matches the mods to the save.
// The save is the real 2.1.24 save that uses foreman-test 0.1.0 with Space Age on.
test.use({ fakeGameOn: true, fakePortalOn: true });

const save = "2.1.24-space-age-testmod.zip";
const withMod = { fixture: save, spaceAge: true, mods: { "foreman-test": { factorio: "2.1", version: "0.1.0" } } };

// Whether the world's mod-list.json has the mod on.
const modEnabled = (dataDir: string, name: string): boolean | undefined =>
  (JSON.parse(readFileSync(join(dataDir, "worlds", "base", "mods", "mod-list.json"), "utf8")).mods as { name: string; enabled: boolean }[]).find((m) => m.name === name)?.enabled;

test.describe("turning off a mod the loading save uses", () => {
  test("a warning and a required box; without the box nothing happens; with it the world ends running without the mod", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUpModsWorld(page, foreman, g, { ...withMod, start: true });
    const before = await creates(g);
    await page.goto("/worlds/base/mods");
    await page.getByRole("table", { name: /Mods of Base/ }).getByRole("row", { name: /foreman-test/ }).getByRole("link", { name: "Disable" }).click();

    await expect(page.getByRole("heading", { level: 1, name: "Disable foreman-test in Base?" })).toBeVisible();
    const warning = page.locator(".message--warning").filter({ hasText: "The save that loads uses 1 mod this change turns off: foreman-test." });
    await expect(warning).toContainText("the game deletes everything it added to the world");
    const box = page.locator("#confirm-drops");
    await expect(box).not.toBeChecked();
    await expect(page.locator("#countdown")).toBeVisible();
    await checkPage(page);

    // Without the box: a field error, the focus on the summary whose link goes to the box, and nothing applied.
    await page.getByRole("button", { name: "Disable 1 mod" }).click();
    const summary = page.locator("#error-summary");
    await expect(summary).toBeFocused();
    await expect(summary).toContainText("Tick the box to confirm that the game will delete what these mods added");
    await expect(box).toHaveAttribute("aria-invalid", "true");
    await expect(page.locator("#confirm-drops-error")).toContainText("Tick the box");
    await checkPage(page);
    expect(await creates(g)).toBe(before);
    expect(modEnabled(foreman.dataDir, "foreman-test")).toBe(true);
    await summary.getByRole("link").click();
    await expect(box).toBeFocused();

    // With it: the game restarts without the mod and ends running.
    await box.check();
    await page.getByRole("button", { name: "Disable 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    await expect(stateBadge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    expect(modEnabled(foreman.dataDir, "foreman-test")).toBe(false);
    await expect(page.getByRole("link", { name: "Undo this mod change" })).toBeVisible();
  });

  test("a mod the save does not use turns off with no warning", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { ...withMod, mods: { ...withMod.mods, "fp-extra": "2.1" } });
    await page.goto("/worlds/base/mods/plan?kind=disable&name=fp-extra");
    await expect(page.getByRole("heading", { level: 1, name: "Disable fp-extra in Base?" })).toBeVisible();
    await expect(page.locator("#confirm-drops")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Disable 1 mod" })).toBeVisible();
  });
});

test("a start the check refuses leads to the plan that matches the mods to the save", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
  await page.getByRole("button", { name: "Start server" }).click();
  const link = page.getByRole("link", { name: "Match the mods to the save" });
  await expect(link).toBeVisible(live);
  await expect(page.locator("summary", { hasText: "Or start anyway" })).toBeVisible();
  await checkPage(page);

  await link.click();
  await expect(page).toHaveURL("/worlds/base/mods/plan?kind=sync");
  await expect(page.getByRole("heading", { level: 1, name: "Match the mods of Base to the save base.zip?" })).toBeVisible();
  await expect(page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" })).toBeVisible();
});

test("an import of a save that uses a mod links to the same plan", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!);
  const src = join(foreman.dataDir, "import", "modded-saves");
  mkdirSync(src, { recursive: true });
  copyFileSync(join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata", save), join(src, "a.zip"));

  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await form.getByLabel("World name").fill("Modded");
  await form.getByLabel("Or type a version").fill("2.1.24");
  await form.getByLabel("Download the game image", { exact: false }).uncheck();
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL(/\/worlds\/modded\?imported=1/);
  const link = page.getByRole("link", { name: "Match this world's mods to its save" });
  await expect(link).toBeVisible();
  await checkPage(page);

  await link.click();
  await expect(page).toHaveURL("/worlds/modded/mods/plan?kind=sync");
  await expect(page.getByRole("heading", { level: 1, name: /^Match the mods of Modded to the save/ })).toBeVisible();
  await checkPage(page);
});
