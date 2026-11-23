import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live, makeZip, version20, openModTools } from "./foreman.js";
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
function fmtestZip(extra: Record<string, string> = {}): Buffer {
  const entries: Record<string, string> = { ...extra };
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

async function setUp(page: Page, foreman: { dataDir: string }, fakeGame: Parameters<typeof setUpModsWorld>[2], start = false, extra: Record<string, string> = {}) {
  await setUpModsWorld(page, foreman as Parameters<typeof setUpModsWorld>[1], fakeGame, { ...world, start });
  writeFileSync(join(modsDir(foreman.dataDir), "fmtest_1.0.0.zip"), fmtestZip(extra));
}

const form = (page: Page) => page.locator("#mod-settings-form form");

async function openEditor(page: Page) {
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await page.getByRole("link", { name: "Edit mod settings" }).click();
  await expect(page.getByLabel("Startup integer")).toBeVisible(live);
}

test.describe("edit mod settings", () => {
  // SPEC 14.6: a mod's rich text keeps a safe subset (bold, named colours,
  // object names, line breaks), written the way Muluna writes it.
  test("a mod's rich text shows bold, colour, object names and line breaks, never its markup", async ({ page, foreman, fakeGame }) => {
    const rich = [
      "[mod-setting-name]",
      "fm-sta-bool=[color=red][font=heading-2]Hard[/font][/color] Restrict Crusher recipe to Muluna",
      "fm-sta-double=[color=red\" onmouseover=\"alert(1)]Odd[/color] <b>number</b>",
      "[mod-setting-description]",
      "fm-sta-bool=Logistic system requires [item=space-science-pack]",
      "fm-sta-double=First line\\nSecond line",
      "",
    ].join("\n");
    await setUp(page, foreman, fakeGame!, false, { "fmtest_1.0.0/locale/en/rich.cfg": rich });
    await openEditor(page);
    const box = page.getByLabel("Hard Restrict Crusher recipe to Muluna");
    await expect(box).toBeVisible();
    const hard = page.locator("#mod-settings-form label.check strong", { hasText: "Hard" });
    await expect(hard).toHaveCount(1);
    await expect(hard.locator("xpath=..")).toHaveClass("rt-danger");
    await expect(box).toHaveAccessibleDescription(/Logistic system requires Space science pack/);
    await expect(form(page)).not.toContainText("[color");
    await expect(form(page)).not.toContainText("[item");
    // A hostile colour and HTML are text, never markup.
    await expect(page.getByText("Odd <b>number</b>")).toBeVisible();
    await expect(page.locator("#mod-settings-form [onmouseover], #mod-settings-form b")).toHaveCount(0);
    const hint = page.locator(`#${await page.getByLabel("Odd <b>number</b>").getAttribute("id")}-hint`);
    await expect(hint.locator("br")).toHaveCount(1);
    await expect(hint).toHaveText(/First line\s*Second line/);
    await checkPage(page);
  });

  test("a stopped world: change a startup value, saved with a snapshot and no start", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await openEditor(page);
    // The description is the hint below its control, and the field says so.
    await expect(page.getByLabel("Startup integer")).toHaveAccessibleDescription("An integer from 1 to 10.");
    // A setting's own localised name, with a parameter, as the game shows it (#598).
    await expect(page.locator("#mod-settings-form dt", { hasText: "Disable modification of chests" })).toBeVisible();
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

  // #636, SPEC 17.16: a setting that differs from the mod's default says what the default is.
  test("a setting that differs from the mod's default shows the default, and one at its default does not", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await openEditor(page);
    const field = page.getByLabel("Startup integer");
    const own = "An integer from 1 to 10.";
    await field.fill("3");
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(doneBox(page)).toBeVisible(live);
    await openEditor(page);
    await expect(field).toHaveValue("3");
    const note = page.locator(`#${await field.getAttribute("id")}-default`);
    await expect(note).toHaveText(/^Mod default: \d+$/);
    // The note is part of the field's description, after the mod's own text.
    await expect(field).toHaveAccessibleDescription(`${own} ${await note.innerText()}`);
    await checkPage(page);

    // Back to the default: the note goes.
    const def = /(\d+)$/.exec(await note.innerText())![1];
    await field.fill(def);
    await page.getByRole("button", { name: "Save settings" }).click();
    await expect(doneBox(page)).toBeVisible(live);
    await openEditor(page);
    await expect(field).toHaveValue(def);
    await expect(page.locator(`#${await field.getAttribute("id")}-default`)).toHaveCount(0);
    await expect(field).toHaveAccessibleDescription(own);
  });

  // Ruling on #623 (SPEC 9.15): each mod's group is headed by its title from
  // info.json, linked to the portal in a new tab, and the name stays out of sight.
  test("each mod's settings are headed by its title, linked to the mod portal", async ({ page, foreman, fakeGame, fakePortal }) => {
    await fakePortal!.add("fmtest", "1.0.0", ["base >= 2.0"]);
    await setUp(page, foreman, fakeGame!);
    await openEditor(page);
    // The group is named by the title alone, so no setting is announced with the link's words.
    const group = form(page).locator("fieldset.mod-settings-group").first();
    await expect(group).toHaveAccessibleName("FM test");
    await expect(group.locator("> legend")).not.toContainText("portal");
    const link = group.getByRole("link", { name: /Mod portal/ });
    await expect(link).toHaveAccessibleName("Mod portal for FM test, opens in a new tab");
    await expect(link).toHaveAttribute("href", "https://mods.factorio.com/mod/fmtest");
    await expect(link).toHaveAttribute("target", "_blank");
    await expect(form(page).locator("legend", { hasText: /^fmtest$/ })).toHaveCount(0);
    await checkPage(page);
  });

  // A mod the portal is known to lack is named by its title but not linked.
  test("a mod the portal lacks is headed by its title, without a link", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    // The Mods tab asks the portal, which has no fmtest; the editor then knows it.
    await page.goto("/worlds/base/mods");
    await expect(page.locator(".mod-flags").first()).toBeVisible(live);
    await openEditor(page);
    const group = form(page).locator("fieldset.mod-settings-group").first();
    await expect(group).toHaveAccessibleName("FM test");
    await expect(group.getByRole("link")).toHaveCount(0);
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

  test("in German the summary marks the English setting name as English", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.context().addCookies([{ name: "foreman_lang", value: "de", url: foreman.url }]);
    await page.goto("/worlds/base/mods/settings");
    const field = page.getByLabel("Startup integer");
    await expect(field).toBeVisible(live);
    await field.fill("42");
    await page.locator("#mod-settings-form button[type=submit]").click();
    await expect(page.locator("#error-summary")).toBeFocused();
    await expect(page.locator("#error-summary li [lang=en]")).toHaveText("Startup integer");
    await expect(page.locator("#" + (await field.getAttribute("id")) + "-error [lang=en]")).toHaveText("Startup integer");
  });

  test("a failed definitions run says why and offers the file upload", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await g.call("/preview-mode", { mode: "oom" });
    try {
      await page.goto("/worlds/base/mods/settings");
      await expect(page.locator("#mod-settings-error")).toContainText("ran out of memory", live);
      await checkPage(page);
      await expect(page.locator("#act-settings-upload")).toHaveClass("link-tab");
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
