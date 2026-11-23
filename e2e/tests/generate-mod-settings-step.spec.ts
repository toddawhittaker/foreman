import { test, expect, checkPage, createAdmin, seedWorld, image21, live } from "./foreman.js";
import { openNewMap, newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 9.6: step 3 of the guided Generate is the startup settings of
// the world's mods. Saving goes on to step 4, whose planets follow the new
// values; the fake game adds the planet fake-moon when fm-sta-int is above 7.
test.use({ fakeGameOn: true });

const stepPath = (id: string) => `${newMapPath(id)}/mod-settings`;
const steps = (page: Page) => page.getByRole("navigation", { name: "Steps to make the world" });

// Adds the fake planet mod to a world and enables it, as a Mods install would.
function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: [{ name: "base", enabled: true }] }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

async function modded(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }): Promise<string> {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  return id;
}

test("step 3 shows the startup settings, and saving one adds the planet it turns on to step 4", async ({ page, foreman, fakeGame }) => {
  const id = await modded(page, foreman, fakeGame!);

  // Continue opens step 3: a mod has startup settings and none are saved.
  await page.goto(`/worlds/${id}`);
  await page.getByRole("link", { name: "Continue setting up" }).click();
  await expect(page).toHaveURL(stepPath(id));
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Mod settings");
  await expect(steps(page).getByRole("link", { name: "3. Mod settings" })).toHaveAttribute("aria-current", "step");
  await expect(steps(page).getByRole("link")).toHaveCount(4);
  const field = page.getByLabel("fm-sta-int");
  await expect(field).toBeVisible(live);
  // Startup settings only: the map settings of the mods wait for the world page.
  await expect(page.locator("#mod-settings-map-heading")).toHaveCount(0);
  await checkPage(page);

  // Before the save step 4 has no fake-moon.
  await page.goto(newMapPath(id));
  await expect(page.locator('#generate-form details[data-planet="fake-planet"]').first()).toBeAttached({ timeout: 60_000 });
  await expect(page.locator('#generate-form details[data-planet="fake-moon"]')).toHaveCount(0);

  await page.goto(stepPath(id));
  await field.fill("9");
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL(newMapPath(id), live);
  await expect(page.locator('#generate-form details[data-planet="fake-moon"]').first()).toBeAttached({ timeout: 60_000 });
  await checkPage(page);

  // With the settings saved, Continue goes on to step 4.
  await page.goto(`/worlds/${id}`);
  await page.getByRole("link", { name: "Continue setting up" }).click();
  await expect(page).toHaveURL(new RegExp(newMapPath(id)));
});

test("Skip to map settings goes on without saving", async ({ page, foreman, fakeGame }) => {
  const id = await modded(page, foreman, fakeGame!);
  await page.goto(stepPath(id));
  await expect(page.getByLabel("fm-sta-int")).toBeVisible(live);
  const skip = page.getByRole("link", { name: "Skip to map settings" });
  await expect(skip).toHaveAttribute("href", newMapPath(id));
  await skip.click();
  await expect(page).toHaveURL(newMapPath(id));
  expect(existsSync(join(foreman.dataDir, "worlds", id, "mods", "mod-settings.dat"))).toBe(false);
});

test("a world with only built-in mods skips step 3 and marks it (none)", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Plain" });
  await page.goto(stepPath(id));
  await expect(page).toHaveURL(newMapPath(id));
  const bar = steps(page);
  await expect(bar.getByRole("listitem").nth(2)).toHaveText("3. Mod settings (none)");
  await expect(bar.getByRole("link", { name: /Mod settings/ })).toHaveCount(0);
  await expect(bar.getByRole("link")).toHaveCount(3);
  await checkPage(page);
});

test.describe("step 3 without script", () => {
  test.use({ javaScriptEnabled: false });
  test("the page waits for the settings and shows the form", async ({ page, foreman, fakeGame }) => {
    const id = await modded(page, foreman, fakeGame!);
    await page.goto(stepPath(id));
    await expect(page.getByLabel("fm-sta-int")).toBeVisible(live);
  });
});
