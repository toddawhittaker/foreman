import { test, expect, checkPage, createAdmin, seedWorld, pickLanguage, version20 } from "./foreman.js";
import type { Page, Locator } from "@playwright/test";
import { mkdirSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { inflateSync } from "node:zlib";

// SPEC 5.6: the map exchange string is offered on a world's page and in each
// favorite, for a world made by Generate, an imported one, and a favorite.
const mapGen = JSON.stringify({ seed: 4242, width: 0, height: 0 });
const mapSettings = JSON.stringify({ pollution: { enabled: false } });
const favID = "0123456789abcdef";
const copyName = "Copy map exchange string";

// What the real game gives for this save (savefile/testdata/mxstrings).
const savedString = (name: string) => readFileSync(join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata", "mxstrings", name + ".txt"), "utf8").trim();

// The game and Foreman compress differently, so strings are compared by what they hold.
const payload = (s: string) => inflateSync(Buffer.from(s.slice(3, -3), "base64")).toString("hex");
const expectSameMap = async (f: Locator, name: string) => expect(payload(await f.inputValue())).toBe(payload(savedString(name)));

function writeMapFiles(dir: string, gen = mapGen) {
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "map-gen-settings.json"), gen);
  writeFileSync(join(dir, "map-settings.json"), mapSettings);
}

function seedFavorite(dataDir: string, id: string, name: string, withMap: boolean) {
  const dir = join(dataDir, "favorites", id);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "favorite.json"), JSON.stringify({
    name, created_at: "2026-11-15T00:00:00Z", source_world: "Base", game_version: version20, space_age: false,
    has_map_settings: withMap, format_version: 1, left_out: [], mods: [],
  }));
  if (withMap) writeMapFiles(dir);
}

const field = (page: Page, id = "exchange") => page.locator(`#${id}-value`);

async function copyAndRead(page: Page, scope: Locator, field: Locator) {
  await scope.getByRole("button", { name: copyName }).click();
  await expect(page.locator("#announcer")).toHaveText("Map exchange string copied");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(await field.inputValue());
}

test("a generated world offers its string and copies it", { tag: "@looks" }, async ({ page, foreman, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await createAdmin(page);
  seedWorld(foreman, { version: version20, fixture: "2.0.77-no-space-age.zip" });
  writeMapFiles(join(foreman.dataDir, "worlds", "base", "config"));
  await page.goto("/worlds/base");
  const f = field(page);
  await expect(f).toHaveJSProperty("readOnly", true);
  const value = await f.inputValue();
  expect(value).toMatch(/^>>>[A-Za-z0-9+/=]+<<<$/);
  // The map files decide, not the save: this is not the save's own string.
  expect(payload(value)).not.toBe(payload(savedString("2.0.77-no-space-age")));
  await expect(page.getByText("The string carries the map settings only, not mods.")).toBeVisible();
  await copyAndRead(page, page.locator("section", { has: page.getByRole("heading", { name: "Map exchange string" }) }), f);
  expect(page.url()).not.toContain(">>>");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/x2-world-${test.info().project.name}.png`, fullPage: true });
});

test("an imported world gives the game's own string from its save", async ({ page, foreman, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await createAdmin(page);
  seedWorld(foreman, { version: version20, fixture: "2.0.77-no-space-age.zip" });
  await page.goto("/worlds/base");
  await expectSameMap(field(page), "2.0.77-no-space-age");
  await copyAndRead(page, page.locator("section", { has: page.getByRole("heading", { name: "Map exchange string" }) }), field(page));
  await checkPage(page);
});

test("a random seed uses the save, and with no save there is no button and the page says why", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, { version: version20, fixture: "2.0.77-no-space-age.zip" });
  const base = join(foreman.dataDir, "worlds", "base");
  writeMapFiles(join(base, "config"), JSON.stringify({ seed: null, width: 0 }));
  await page.goto("/worlds/base");
  await expectSameMap(field(page), "2.0.77-no-space-age");

  // A save that cannot be read leaves the control off, with the reason beside it.
  writeFileSync(join(base, "saves", "base.zip"), "not a zip");
  await page.goto("/worlds/base");
  await expect(page.getByRole("button", { name: copyName })).toHaveCount(0);
  await expect(page.locator("#exchange-reason")).toContainText("could not read the world's newest save");
  await checkPage(page);

  // No save at all: the seed is left to chance, so nothing can name the map.
  rmSync(join(base, "saves", "base.zip"));
  await page.goto("/worlds/base");
  await expect(page.getByRole("button", { name: copyName })).toHaveCount(0);
  await expect(page.locator("#exchange-reason")).toContainText("seed is left to chance");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/x2-world-off-${test.info().project.name}.png`, fullPage: true });
});

test("a favorite offers its string, and one without map settings says why", { tag: "@looks" }, async ({ page, foreman, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await createAdmin(page);
  seedFavorite(foreman.dataDir, favID, "With map", true);
  seedFavorite(foreman.dataDir, "fedcba9876543210", "No map", false);
  await page.goto("/favorites");
  const withMap = page.locator("li.favorite", { has: page.locator(".favorite__name", { hasText: "With map" }) });
  const noMap = page.locator("li.favorite", { has: page.locator(".favorite__name", { hasText: "No map" }) });

  // The string sits in the favorite's Details, folded away until wanted.
  await expect(field(page, `exchange-${favID}`)).toBeHidden();
  await withMap.locator("summary").click();
  await expect(field(page, `exchange-${favID}`)).toHaveValue(/^>>>[A-Za-z0-9+/=]+<<<$/);
  await copyAndRead(page, withMap, field(page, `exchange-${favID}`));
  // The button's name is the same on every row; its description names the favorite.
  await expect(withMap.getByRole("button", { name: copyName })).toHaveAccessibleDescription(/With map/);

  await noMap.locator("summary").click();
  await expect(noMap.getByRole("button", { name: copyName })).toHaveCount(0);
  await expect(noMap).toContainText("keeps no map settings");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/x2-favorites-${test.info().project.name}.png`, fullPage: true });
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("the string is a read-only field to select, on the world and the favorite", { tag: "@looks" }, async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman, { version: version20, fixture: "2.0.77-no-space-age.zip" });
    seedFavorite(foreman.dataDir, favID, "With map", true);
    await page.goto("/worlds/base");
    await expectSameMap(field(page), "2.0.77-no-space-age");
    await expect(field(page)).toHaveAttribute("readonly", "");
    // The copy button needs script, so it stays hidden; the field is the way.
    await expect(page.getByRole("button", { name: copyName })).toBeHidden();
    await field(page).focus();
    await expect(field(page)).toBeFocused();

    await page.goto("/favorites");
    await page.locator(".favorite summary").click();
    await expect(field(page, `exchange-${favID}`)).toHaveValue(/^>>>/);
    await expect(field(page, `exchange-${favID}`)).toBeVisible();
  });
});

// SPEC 17.2, 22.7: German and Russian run long; the string breaks in its field and the buttons keep their words.
const exchangeWords = { de: ["Code zum Kartenaustausch", "Code zum Kartenaustausch kopieren"], ru: ["Строка обмена карты", "Скопировать строку обмена карты"] } as const;
for (const lang of ["de", "ru"] as const) {
  test(`at 320 px and 200% text in ${lang}, the world and favorites pages fit`, { tag: "@looks" }, async ({ page, foreman }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await createAdmin(page);
    seedWorld(foreman, { version: version20, fixture: "2.0.77-no-space-age.zip" });
    seedFavorite(foreman.dataDir, favID, "With map", true);
    await pickLanguage(page, lang);
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    await page.goto("/worlds/base");
    await expect(page.getByRole("heading", { name: exchangeWords[lang][0] })).toBeVisible();
    await expect(page.getByRole("button", { name: exchangeWords[lang][1] })).toBeVisible();
    await checkPage(page);
    await page.goto("/favorites");
    await page.locator(".favorite summary").click();
    await expect(page.getByRole("heading", { name: new RegExp(exchangeWords[lang][0]) })).toBeVisible();
    await expect(field(page, `exchange-${favID}`)).toBeVisible();
    await checkPage(page);
    await page.screenshot({ path: `../screenshots/x2-${lang}-200-${test.info().project.name}.png`, fullPage: true });
  });
}
