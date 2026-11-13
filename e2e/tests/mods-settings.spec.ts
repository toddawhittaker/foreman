import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live, makeZip, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";

// SPEC 9.6, 9.13: the mod settings editor. The fake game's definitions run
// reports the fmtest mod's settings; the world holds the real fmtest mod, so
// its names and descriptions come from its own locale file.
test.use({ fakeGameOn: true, fakePortalOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { fmtest: "2.0" } };
const modsDir = (dataDir: string) => join(dataDir, "worlds", "base", "mods");
const fmtestDir = join(import.meta.dirname, "..", "..", "internal", "game", "testdata", "modsettings", "fmtest_1.0.0");

// The fmtest mod as a zip, from the folder the real-game tests use.
function fmtestZip(): Buffer {
  const entries: Record<string, string> = {};
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else entries[`fmtest_1.0.0/${relative(fmtestDir, p)}`] = readFileSync(p, "utf8");
    }
  };
  walk(fmtestDir);
  return makeZip(entries);
}

async function setUp(page: Page, foreman: { dataDir: string }, fakeGame: Parameters<typeof setUpModsWorld>[2], start = false) {
  await setUpModsWorld(page, foreman as Parameters<typeof setUpModsWorld>[1], fakeGame, { ...world, start });
  writeFileSync(join(modsDir(foreman.dataDir), "fmtest_1.0.0.zip"), fmtestZip());
}

const form = (page: Page) => page.locator("#mod-settings-form form");

async function openEditor(page: Page) {
  await page.goto("/worlds/base/mods");
  await page.getByRole("link", { name: "Edit mod settings" }).first().click();
  await expect(page.getByLabel("Startup integer")).toBeVisible(live);
}

test.describe("edit mod settings", () => {
  test("a stopped world: change a startup value, saved with a snapshot and no start", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await openEditor(page);
    // The description is the hint below its control, and the field says so.
    await expect(page.getByLabel("Startup integer")).toHaveAccessibleDescription("An integer from 1 to 10.");
    // Map settings of a world with a save are values for a new map, changed in the game (SPEC 9.6).
    await expect(page.locator("#mod-settings-map-heading + p")).toContainText("Settings, Mod settings, Map");
    await expect(page.locator("#mod-settings-per-user")).toBeVisible();
    await expect(form(page)).not.toContainText("fm-sta-hidden");
    await expect(page.locator("#countdown")).toHaveCount(0);
    await checkPage(page);

    await page.getByLabel("Startup integer").fill("3");
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(doneBox(page)).toContainText(/Foreman took the snapshot "Before mod change" first\./, live);
    await expect(stateBadge(page)).toHaveText("Stopped");
    expect(existsSync(join(modsDir(foreman.dataDir), "mod-settings.dat"))).toBe(true);

    // The editor shows the saved value.
    await openEditor(page);
    await expect(page.getByLabel("Startup integer")).toHaveValue("3");
  });

  test("a running world: the countdown is offered, and the game restarts", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g, true);
    await openEditor(page);
    await expect(page.locator("#countdown")).toBeVisible();
    await expect(form(page)).toContainText("Warn the players, then save and stop the server.");
    const before = await creates(g);
    await page.getByRole("checkbox", { name: "fm-sta-bool" }).click();
    await page.getByRole("button", { name: "Save and restart" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base were changed.", live);
    await expect(stateBadge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    await expect(page.getByRole("link", { name: "Undo this mod change" })).toBeVisible();
  });

  test("a value out of range is refused next to its field, and nothing is written", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await openEditor(page);
    await page.getByLabel("Startup integer").fill("42");
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(page.locator("#error-summary")).toBeFocused();
    await expect(page.locator("#error-summary")).toContainText("Enter a value from 1 to 10 for Startup integer.");
    const field = page.getByLabel("Startup integer");
    await expect(field).toHaveAttribute("aria-invalid", "true");
    await expect(field).toHaveValue("42");
    await checkPage(page);
    expect(existsSync(join(modsDir(foreman.dataDir), "mod-settings.dat"))).toBe(false);

    // The summary's link reaches the field.
    await page.locator("#error-summary a").first().click();
    await expect(field).toBeFocused();
  });

  test("a failed definitions run says why and offers the file upload", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await g.call("/preview-mode", { mode: "oom" });
    try {
      await page.goto("/worlds/base/mods/settings");
      await expect(page.locator("#mod-settings-error")).toContainText("ran out of memory", live);
      await checkPage(page);
      await page.getByRole("link", { name: "Upload mod-settings.dat on the Mods tab" }).click();
      await expect(page).toHaveURL(/\/worlds\/base\/mods#mod-settings-replace$/);
    } finally {
      await g.call("/preview-mode", { mode: "ok" });
    }
  });

  test("at phone width the form is one column with no sideways scroll", async ({ page, foreman, fakeGame }) => {
    await page.setViewportSize({ width: 320, height: 800 });
    await setUp(page, foreman, fakeGame!);
    await page.goto("/worlds/base/mods/settings");
    await expect(page.getByLabel("Startup integer")).toBeVisible(live);
    await checkPage(page);
  });
});

// A decimal typed with a comma, as a German browser user writes it, is
// read as the number meant, not with the comma dropped.
test.describe("a comma-decimal browser", () => {
  test.use({ locale: "de-DE" });

  test("a decimal typed with a comma is saved", async ({ page, foreman, fakeGame }) => {
    // The pages stay in English; only the browser's number format is German.
    await page.context().addCookies([{ name: "foreman_lang", value: "en", url: foreman.url }]);
    await setUp(page, foreman, fakeGame!);
    await page.goto("/worlds/base/mods/settings");
    const field = page.getByRole("textbox", { name: "fm-sta-double" });
    await expect(field).toBeVisible(live);
    await field.fill("2,5");
    await page.locator("#mod-settings-form button[type=submit]").click();
    await expect(doneBox(page)).toBeVisible(live);
    await page.goto("/worlds/base/mods/settings");
    await expect(page.getByRole("textbox", { name: "fm-sta-double" })).toHaveValue("2.5", live);
  });
});
