import { test, expect, planRow, dropFile, checkPage, createAdmin, pickLanguage, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { mkdirSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.12: a favorite is made from a world's page and starts a new world on
// Generate, whose mods then go through the review page (SPEC 9.13).
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

// A favorite's row: its Details foldout, named by the favorite, and Generate beside it.
const favRow = (page: Page, name: string) => page.locator("li.favorite", { has: page.locator(".favorite__name", { hasText: new RegExp(`^${name}$`) }) });

test("make a favorite, start Generate from it, land on the review page, then delete it", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-signs", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { "fp-signs": "2.0" } });
  const base = join(foreman.dataDir, "worlds", "base");
  writeFileSync(join(base, "config", "server-settings.json"), JSON.stringify({ name: "Club", game_password: "hunter2-game", max_players: 7 }));
  mkdirSync(join(base, "mods", "mylocal"), { recursive: true });
  writeFileSync(join(base, "mods", "mylocal", "info.json"), JSON.stringify({ name: "mylocal", version: "0.1.0", factorio_version: "2.0" }));

  // Make it from the world's page.
  await page.goto("/worlds/base");
  const name = page.getByLabel("Favorite name");
  await expect(name).toHaveValue("Base");
  await expect(page.locator("#favorite-hint")).toContainText("default map");
  await name.fill("Club start");
  await page.getByRole("button", { name: "Make favorite" }).click();
  const made = page.locator("#favorite-made");
  await expect(made).toContainText('Made the favorite "Club start".');
  await expect(made).toContainText("1 mod is left out");
  await expect(made.getByRole("listitem")).toHaveText(["mylocal"]);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/f2-world-favorite-${test.info().project.name}.png`, fullPage: true });

  // A second favorite with the same name is refused on the field.
  await page.getByLabel("Favorite name").fill("Club start");
  await page.getByRole("button", { name: "Make favorite" }).click();
  await expect(page.locator("#favorite-name-error")).toContainText("Another favorite already has this name.");

  // See it listed, reached from Generate.
  await page.goto("/generate");
  await page.getByRole("link", { name: "See all favorites" }).click();
  await expect(page).toHaveURL("/favorites");
  const row = favRow(page, "Club start");
  await expect(row.locator("details")).not.toHaveAttribute("open");
  await expect(row.getByRole("link", { name: "Generate a world from Club start" })).toBeVisible();
  await expect(row).toContainText("Base");
  await expect(row).toContainText("2.0.77");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/f2-favorites-${test.info().project.name}.png`, fullPage: true });

  // Start Generate from it.
  await row.getByRole("link", { name: "Generate a world from Club start" }).click();
  await expect(page.locator("#favorite-note")).toContainText('Starting from the favorite "Club start".');
  await expect(page.locator("#favorite-note")).toContainText("you review 1 mod to install");
  await expect(page.getByLabel("Favorite", { exact: true })).toHaveValue(/^[0-9a-f]{16}$/);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/f2-generate-favorite-${test.info().project.name}.png`, fullPage: true });

  await page.getByLabel("World name").fill("From club");
  await page.getByRole("button", { name: "Next: Mods" }).click();
  await expect(page).toHaveURL(/\/worlds\/[^/]+\/mods\/plan\?/);
  const url = new URL(page.url());
  // The address names the favorite alone; the review reads its mods and pinned versions (SPEC 17).
  expect(url.searchParams.get("favorite")).toMatch(/^[0-9a-f]{16}$/);
  expect(url.searchParams.getAll("name")).toEqual([]);
  await expect(planRow(page, "fp-signs")).toContainText("1.0.0");
  // A toast says the world was made; it leaves focus alone.
  const generated = page.locator("#toasts [data-toast]");
  await expect(generated).toContainText("Generated From club from the favorite.");
  await expect(generated).not.toBeFocused();
  await checkPage(page);
  const id = url.pathname.split("/")[2];
  const server = readFileSync(join(foreman.dataDir, "worlds", id, "config", "server-settings.json"), "utf8");
  expect(server).toContain('"max_players"');
  expect(server).not.toContain("hunter2-game");

  // Delete it, with a confirmation.
  await page.goto("/favorites");
  await favRow(page, "Club start").locator("summary").click();
  await page.getByRole("link", { name: "Delete favorite" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText('Delete the favorite "Club start"?');
  await checkPage(page);
  await page.getByRole("button", { name: "Delete favorite" }).click();
  await expect(page.locator("#done-message")).toContainText('Deleted the favorite "Club start".');
  await expect(page.locator("#favorites-empty")).toBeVisible();
  await checkPage(page);
});

// SPEC 5.12, DESIGN 7.11: a favorite exports as a file and imports again
// through the drop zone, with or without script.
const favID = "0123456789abcdef";

function seedFavorite(dataDir: string) {
  const dir = join(dataDir, "favorites", favID);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "favorite.json"), JSON.stringify({
    name: "Shared pack", created_at: "2026-11-15T00:00:00Z", source_world: "Base", game_version: version20,
    space_age: false, has_map_settings: false, format_version: 1, left_out: [],
    mods: [{ name: "fp-signs", version: "1.0.0", title: "Signs of the Factory" }],
  }));
  writeFileSync(join(dir, "mod-settings.dat"), "modsettings-bytes");
}

async function exportAndDelete(page: Page, dataDir: string, name = "Shared pack"): Promise<Buffer> {
  await page.goto("/favorites");
  const row = favRow(page, name);
  await row.locator("summary").click();
  await expect(row.locator(".favorite-export-warning")).toContainText("a mod may keep a token");
  await expect(row.locator(".favorite-export-warning")).toHaveCount(1);
  const title = row.getByRole("link", { name: /Signs of the Factory/ });
  await expect(title).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-signs");
  await expect(title).toHaveAttribute("target", "_blank");
  const downloading = page.waitForEvent("download");
  await row.getByRole("link", { name: "Export" }).click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe(`${name}-favorite.zip`);
  const file = readFileSync(await download.path());
  rmSync(join(dataDir, "favorites", favID), { recursive: true });
  return file;
}

test("export a favorite, see the warning, then import it by dropping the file", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedFavorite(foreman.dataDir);
  await page.goto("/favorites");
  await checkPage(page);
  const file = await exportAndDelete(page, foreman.dataDir);

  await page.goto("/favorites");
  await expect(page.locator("#favorites-empty")).toBeVisible();
  await dropFile(page, "favorite-file", [{ name: "Shared_pack-favorite.zip", body: file, type: "application/zip" }]);
  await expect(page.locator(".dropzone__chosen")).toContainText("Shared_pack-favorite.zip");
  await page.getByRole("button", { name: "Import favorite" }).click();
  await expect(page.locator("#done-message")).toContainText('Imported the favorite "Shared pack".');
  const imported = favRow(page, "Shared pack");
  await expect(imported).toBeVisible();
  // An imported favorite names no source world and is dated on import.
  await expect(imported.getByText("Made from")).toHaveCount(0);
  await expect(imported.locator("time")).not.toHaveAttribute("datetime", /^2026-11-15T00:00:00/);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/p1-favorites-imported-${test.info().project.name}.png`, fullPage: true });

  // A damaged file is refused on the field, and nothing is added.
  await page.locator("#favorite-file").setInputFiles({ name: "bad.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
  await page.getByRole("button", { name: "Import favorite" }).click();
  await expect(page.locator("#favorite-file-error")).toContainText("Nothing was imported.");
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.locator("#error-summary").getByRole("link")).toHaveAttribute("href", "#favorite-file");
  await expect(favRow(page, "Shared pack")).toHaveCount(1);
  await checkPage(page);
});

// SPEC 5.12, DESIGN 7.11 (#658): a closed row shows the name, a line of facts
// and Generate; Details holds the rest in order, and Rename changes the name
// in the list and in an export.
async function openDetails(page: Page, name: string) {
  const row = favRow(page, name);
  await row.locator("summary").click();
  return row;
}

async function expectDetails(page: Page, name: string) {
  const row = favRow(page, name);
  await expect(row.locator("summary")).toContainText(`${version20} · No Space Age · 1 mod`);
  await expect(row.getByRole("link", { name: `Generate a world from ${name}` })).toBeVisible();
  await expect(row.getByText("Made from")).toBeHidden();
  await openDetails(page, name);
  await expect(row.getByText("Made from")).toBeVisible();
  await expect(row.getByText("Made on")).toBeVisible();
  await expect(row.getByRole("link", { name: /Signs of the Factory/ })).toBeVisible();
  await expect(row.getByRole("heading", { name: /Map exchange string/ })).toBeVisible();
  await expect(row).toContainText("keeps no map settings");
  await expect(row.getByRole("textbox", { name: /New name/ })).toHaveValue(name);
  await expect(row.getByRole("button", { name: "Rename" })).toBeVisible();
  await expect(row.getByRole("link", { name: "Export" })).toBeVisible();
  await expect(row.getByRole("link", { name: "Delete favorite" })).toBeVisible();
  // The order is facts, mods, exchange string, Rename, the export warning, then Export and Delete (DESIGN 7.11).
  const order = await row.locator(".favorite__body > *").evaluateAll((els) => els.map((e) => e.className || e.tagName));
  expect(order).toEqual(["facts facts--inline", "favorite__part", "favorite__part", "field favorite__rename", "message message--warning favorite-export-warning", "actions"]);
  return row;
}

async function rename(page: Page, from: string, to: string) {
  const row = favRow(page, from);
  if ((await row.locator("details").getAttribute("open")) === null) await row.locator("summary").click();
  await row.getByRole("textbox", { name: /New name/ }).fill(to);
  await row.getByRole("button", { name: "Rename" }).click();
}

test("a favorite is a closed row; Details holds the rest; Rename changes the list and the export", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedFavorite(foreman.dataDir);
  const other = join(foreman.dataDir, "favorites", "fedcba9876543210");
  mkdirSync(other, { recursive: true });
  writeFileSync(join(other, "favorite.json"), JSON.stringify({
    name: "Other pack", created_at: "2026-11-15T00:00:00Z", source_world: "Base", game_version: version20,
    space_age: true, has_map_settings: false, format_version: 1, left_out: [], mods: [],
  }));
  await page.goto("/favorites");
  await expectDetails(page, "Shared pack");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/p1-favorite-details-${test.info().project.name}.png`, fullPage: true });

  // Another favorite's name is refused on the field, with Details open.
  await rename(page, "Shared pack", "other PACK");
  const err = page.locator(`#favorite-${favID}-rename-error`);
  await expect(err).toContainText("Another favorite already has this name.");
  await expect(favRow(page, "Shared pack").locator("details")).toHaveAttribute("open", "");
  await expect(page.getByRole("textbox", { name: /New name Shared pack/ })).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);
  await rename(page, "Shared pack", "   ");
  await expect(err).toContainText("Enter a name for the favorite.");

  await rename(page, "Shared pack", "Rail world");
  await expect(page).toHaveURL(`/favorites?renamed=${favID}`);
  await expect(page.locator("#done-message")).toContainText('Renamed the favorite to "Rail world".');
  await expect(favRow(page, "Rail world")).toHaveCount(1);
  await expect(favRow(page, "Shared pack")).toHaveCount(0);
  await expect(JSON.parse(readFileSync(join(foreman.dataDir, "favorites", favID, "favorite.json"), "utf8")).name).toBe("Rail world");

  // The export carries the new name, in its file name and its favorite.json.
  const file = await exportAndDelete(page, foreman.dataDir, "Rail world");
  // Importing it back names it from its favorite.json.
  await page.goto("/favorites");
  await page.locator("#favorite-file").setInputFiles({ name: "pack.zip", mimeType: "application/zip", buffer: file });
  await page.getByRole("button", { name: "Import favorite" }).click();
  await expect(page.locator("#done-message")).toContainText('Imported the favorite "Rail world".');
});

for (const lang of ["de", "ru"] as const) {
  test(`at 320 px and 200% text in ${lang}, a favorite's row and Details fit`, { tag: "@looks" }, async ({ page, foreman }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await createAdmin(page);
    seedFavorite(foreman.dataDir);
    await pickLanguage(page, lang);
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    await page.goto("/favorites");
    await checkPage(page);
    await favRow(page, "Shared pack").locator("summary").click();
    await checkPage(page);
    await page.screenshot({ path: `../screenshots/p1-favorites-${lang}-200-${test.info().project.name}.png`, fullPage: true });
  });
}

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, isMobile: true });

  test("a favorite's row, Generate and Details controls are 44 px", { tag: "@looks" }, async ({ page, foreman }) => {
    await createAdmin(page);
    seedFavorite(foreman.dataDir);
    await page.goto("/favorites");
    expect(await page.evaluate(() => matchMedia("(pointer: coarse)").matches)).toBe(true);
    const row = await openDetails(page, "Shared pack");
    for (const target of [row.locator("summary"), row.getByRole("link", { name: /Generate a world/ }), row.getByRole("button", { name: "Rename" }),
      row.getByRole("textbox", { name: /New name/ }), row.getByRole("link", { name: "Export" }), row.getByRole("link", { name: "Delete favorite" })]) {
      const box = (await target.boundingBox())!;
      expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(44);
    }
  });
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("Details opens and Rename works", async ({ page, foreman }) => {
    await createAdmin(page);
    seedFavorite(foreman.dataDir);
    await page.goto("/favorites");
    await expectDetails(page, "Shared pack");
    await rename(page, "Shared pack", "SHARED PACK");
    await expect(page.locator("#done-message")).toContainText('Renamed the favorite to "SHARED PACK".');
  });

  test("a favorite file imports through the plain file field", async ({ page, foreman }) => {
    await createAdmin(page);
    seedFavorite(foreman.dataDir);
    const file = await exportAndDelete(page, foreman.dataDir);
    await page.goto("/favorites");
    await page.getByLabel("Favorite file").setInputFiles({ name: "pack.zip", mimeType: "application/zip", buffer: file });
    await page.getByRole("button", { name: "Import favorite" }).click();
    await expect(page.locator("#done-message")).toContainText('Imported the favorite "Shared pack".');
  });
});
