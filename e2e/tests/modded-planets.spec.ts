import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { openNewMap, newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 14.6, 17.14, 22: step 3 of a world with the fake planet mod.
// Names Foreman knows keep its labels; the mod's own names are marked
// translate="no"; a shared control says what else it changes; a planet's own
// default is shown and kept; the preview says what it cannot show.
test.use({ fakeGameOn: true });

// Adds the fake planet mod to a world and enables it, as a Mods install would.
function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: [{ name: "base", enabled: true }] }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

async function openModded(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }): Promise<string> {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  await page.goto(newMapPath(id));
  await expect(page.locator('#generate-form details[data-planet="fake-planet"]').first()).toBeAttached({ timeout: 60_000 });
  return id;
}

test("a modded planet's names, shared control and own default", async ({ page, foreman, fakeGame }) => {
  const id = await openModded(page, foreman, fakeGame!);

  // The planet's row and the mod's control are the mod's words, not translated.
  const planet = page.locator('#mg-resources-fake-planet');
  await expect(planet.locator('summary [translate="no"]')).toHaveText("Fake planet");
  await expect(page.locator('#mg-fake-planet-fake-ore legend [translate="no"]')).toContainText("Fake ore");
  // Stone is Foreman's own word on both planets, and says it is one value.
  await expect(page.locator("#mg-fake-planet-stone legend")).toContainText("Stone");
  await expect(page.locator('#mg-fake-planet-stone legend [translate="no"]')).toHaveCount(0);
  await expect(page.locator("#mg-fake-planet-stone-also")).toHaveText("Also changes Nauvis");
  await expect(page.locator("#mg-nauvis-stone-also")).toHaveText("Also changes Fake planet");
  await expect(page.locator('#mg-nauvis-stone-also [translate="no"]')).toHaveText("Fake planet");
  await expect(page.locator("#mg-fake-planet-stone")).toHaveAttribute("aria-describedby", "mg-fake-planet-stone-also");

  // The planet's own richness of 150 is shown as its value.
  const richness = page.locator("#mg-fake-planet-fake-ore-richness");
  await expect(richness).toHaveValue("x150");
  await expect(richness.locator("option:checked")).toHaveText("15000% (this planet's default)");
  await checkPage(page);

  // With another planet's setting moved the settings are saved as a file: the
  // untouched richness is written back as 150, and stone once.
  await page.locator("#mg-nauvis-iron-ore-size").evaluate((el: HTMLSelectElement) => {
    el.value = "200";
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await page.getByRole("button", { name: "Save map settings" }).click();
  await expect(page.locator("#newmap-done")).toBeVisible();
  const file = readFileSync(join(foreman.dataDir, "worlds", id, "config", "map-gen-settings.json"), "utf8");
  expect(JSON.parse(file).autoplace_controls["fake-ore"].richness).toBe(150);
  expect(file.match(/"stone"/g)).toHaveLength(1);
});

test("the preview says what it cannot show, and a one-color picture says why", async ({ page, foreman, fakeGame }) => {
  await openModded(page, foreman, fakeGame!);
  await page.locator("#preview-button").click();
  const line = page.locator("#preview-limit-line");
  await expect(line).toContainText("This fast preview cannot show terrain a mod makes by script.", { timeout: 60_000 });
  await line.getByRole("button", { name: /what the fast preview cannot show/ }).click();
  await expect(page.locator("#preview-limit-tip")).toBeVisible();
  // The fake game's pictures are one color.
  await expect(page.locator(".preview-one-color").first()).toContainText("everything near the start outside the map");
  await checkPage(page);
});

test("a step 3 preview with a problem stays on step 3", async ({ page, foreman, fakeGame }) => {
  const id = await openModded(page, foreman, fakeGame!);
  await page.locator("#generate-seed").fill("not a seed");
  await page.locator("#preview-button").click();
  await expect(page.locator('#preview[data-preview="invalid"]')).toBeVisible({ timeout: 60_000 });
  await expect(page).toHaveURL(newMapPath(id));
  await expect(page.getByRole("navigation", { name: "Steps to make the world" })).toBeVisible();
  await expect(page.locator("#generate-name")).toHaveCount(0);
  await checkPage(page);
});

test("the preview says when the world's planet list could not be read", async ({ page, foreman, fakeGame }) => {
  const id = await openModded(page, foreman, fakeGame!);
  // The game's runs fail, the planet run among them; a new mod makes it run again.
  await fakeGame!.call("/preview-mode", { mode: "fail" });
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(foreman.dataDir, "worlds", id, "mods", "other-mod_1.0.0.zip"));
  await page.goto(newMapPath(id));
  await expect(page.locator("#newmap-builtin")).toBeVisible({ timeout: 60_000 });
  await page.locator("#preview-button").click();
  await expect(page.locator("#preview-fallback")).toContainText("could not read this world's planet list", { timeout: 60_000 });
  await checkPage(page);
});

test("a world mod named like Foreman's helper mod is said in the preview", async ({ page, foreman, fakeGame }) => {
  const id = await openModded(page, foreman, fakeGame!);
  // A mod with the name of the mod Foreman adds to read the planets.
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(foreman.dataDir, "worlds", id, "mods", "foreman-modsettings_1.0.0.zip"));
  await page.goto(newMapPath(id));
  await expect(page.locator("#newmap-builtin")).toBeVisible({ timeout: 60_000 });
  await page.locator("#preview-button").click();
  await expect(page.locator("#preview-fallback")).toContainText("same name as a mod Foreman adds", { timeout: 60_000 });
  await checkPage(page);
});
