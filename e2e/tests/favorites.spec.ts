import { test, expect, checkPage, createAdmin, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { mkdirSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.12: a favorite is made from a world's page and starts a new world on
// Generate, whose mods then go through the review page (SPEC 9.13).
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

test("make a favorite, start Generate from it, land on the review page, then delete it", async ({ page, foreman, fakeGame, fakePortal }) => {
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
  const row = page.getByRole("listitem").filter({ has: page.getByRole("heading", { name: "Club start" }) });
  await expect(row).toContainText("Base");
  await expect(row).toContainText("2.0.77");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/f2-favorites-${test.info().project.name}.png`, fullPage: true });

  // Start Generate from it.
  await row.getByRole("link", { name: "Generate from it" }).click();
  await expect(page.locator("#favorite-note")).toContainText('Starting from the favorite "Club start".');
  await expect(page.locator("#favorite-note")).toContainText("you review 1 mod to install");
  await expect(page.getByLabel("Favorite", { exact: true })).toHaveValue(/^[0-9a-f]{16}$/);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/f2-generate-favorite-${test.info().project.name}.png`, fullPage: true });

  await page.getByLabel("World name").fill("From club");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL(/\/worlds\/[^/]+\/mods\/plan\?/);
  const url = new URL(page.url());
  expect(url.searchParams.getAll("name")).toEqual(["fp-signs"]);
  expect(url.searchParams.get("version-fp-signs")).toBe("1.0.0");
  await expect(page.locator("#mod-plan")).toContainText("fp-signs 1.0.0");
  // The page says the world was made, above the changes, and takes focus.
  const generated = page.locator("#plan-generated");
  await expect(generated).toContainText("Generated From club from the favorite.");
  await expect(generated).toBeFocused();
  await checkPage(page);
  const id = url.pathname.split("/")[2];
  const server = readFileSync(join(foreman.dataDir, "worlds", id, "config", "server-settings.json"), "utf8");
  expect(server).toContain('"max_players"');
  expect(server).not.toContain("hunter2-game");

  // Delete it, with a confirmation.
  await page.goto("/favorites");
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

async function exportAndDelete(page: Page, dataDir: string): Promise<Buffer> {
  await page.goto("/favorites");
  const row = page.getByRole("listitem").filter({ has: page.getByRole("heading", { name: "Shared pack" }) });
  await expect(page.locator("#favorite-export-warning")).toContainText("a mod may keep a token");
  await row.getByText("Show the mods").click();
  const title = row.getByRole("link", { name: /Signs of the Factory/ });
  await expect(title).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-signs");
  await expect(title).toHaveAttribute("target", "_blank");
  const downloading = page.waitForEvent("download");
  await row.getByRole("link", { name: "Export" }).click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe("Shared pack-favorite.zip");
  const file = readFileSync(await download.path());
  rmSync(join(dataDir, "favorites", favID), { recursive: true });
  return file;
}

test("export a favorite, see the warning, then import it by dropping the file", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFavorite(foreman.dataDir);
  await page.goto("/favorites");
  await checkPage(page);
  const file = await exportAndDelete(page, foreman.dataDir);

  await page.goto("/favorites");
  await expect(page.locator("#favorites-empty")).toBeVisible();
  const dt = await page.evaluateHandle((b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const dt = new DataTransfer();
    dt.items.add(new File([bytes], "Shared_pack-favorite.zip", { type: "application/zip" }));
    return dt;
  }, file.toString("base64"));
  const area = page.locator("#favorite-file").locator("..");
  await page.dispatchEvent("body", "dragenter", { dataTransfer: dt });
  await area.dispatchEvent("dragover", { dataTransfer: dt });
  await area.dispatchEvent("drop", { dataTransfer: dt });
  await expect(page.locator(".dropzone__chosen")).toContainText("Shared_pack-favorite.zip");
  await page.getByRole("button", { name: "Import favorite" }).click();
  await expect(page.locator("#done-message")).toContainText('Imported the favorite "Shared pack".');
  await expect(page.getByRole("heading", { name: "Shared pack" })).toBeVisible();
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/p1-favorites-imported-${test.info().project.name}.png`, fullPage: true });

  // A damaged file is refused on the field, and nothing is added.
  await page.locator("#favorite-file").setInputFiles({ name: "bad.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
  await page.getByRole("button", { name: "Import favorite" }).click();
  await expect(page.locator("#favorite-file-error")).toContainText("Nothing was imported.");
  await expect(page.getByRole("heading", { level: 2, name: "Shared pack" })).toHaveCount(1);
  await checkPage(page);
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

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
