import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import { newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 17.9, 17.10: the guided Generate. Step 1 is what the world is,
// step 2 its mods, step 3 its map settings; a world left part way is
// Unfinished, with a Continue link to where work remains.
test.use({ fakeGameOn: true });

const worldJSON = (dataDir: string, id: string) => JSON.parse(readFileSync(join(dataDir, "worlds", id, "world.json"), "utf8"));

async function setUp(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  await page.goto("/");
}

test("the whole flow: step 1, skip the mods, a preset on step 3, saved in five clicks", async ({ page, foreman, fakeGame }, info) => {
  await setUp(page, foreman, fakeGame!);
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.getByRole("link", { name: "Worlds", exact: true }).click();
  await page.getByRole("link", { name: "Generate a world" }).click();
  await page.getByLabel("World name").fill("Rich start");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/d1-step1-${info.project.name}.png`, fullPage: true });
  await page.getByRole("button", { name: "Generate world" }).click();

  // Step 2, the world's mods, can be skipped.
  await expect(page).toHaveURL("/worlds/rich-start/mods?generated=1");
  // The Mods tab says the world is made and takes focus, as other Done boxes do (SPEC 17.9).
  const made = page.locator("#mods-generated");
  await expect(made).toHaveText("Done: Rich start is made. Next, pick its mods or skip to map settings.");
  await expect(made).toBeFocused();
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/d1-step2-made-${info.project.name}.png`, fullPage: true });
  await page.getByRole("link", { name: "Skip to map settings" }).click();

  // Step 3: a preset and a seed, then save.
  await expect(page).toHaveURL(new RegExp(`${newMapPath("rich-start")}`));
  await expect(page.locator("#generate-form")).toBeVisible({ timeout: 60_000 });
  await page.locator("#generate-preset").selectOption({ label: "Rich resources" });
  await expect(page.locator("#generate-preset")).toHaveValue("rich-resources");
  await page.locator("#generate-seed").fill("12345");
  await page.getByRole("button", { name: "Save map settings" }).click();
  await expect(page.locator("#newmap-done")).toBeVisible();
  await expectWithin(clicks, 5, "generate a world with a preset, skipping the mods");
  expect(worldJSON(foreman.dataDir, "rich-start")).toMatchObject({ game_version: "2.1.24", space_age: true, new_map: { preset: "rich-resources", seed: 12345 } });
  await checkPage(page);
});

test("an Unfinished world says so on the list and its page, and Continue opens step 3", async ({ page, foreman, fakeGame }, info) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Half made");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/half-made/mods?generated=1");

  await page.goto("/worlds");
  const row = page.locator("ul.worlds > li", { has: page.locator("#world-half-made") });
  await expect(row.locator(".badge")).toHaveText("Unfinished");
  // The active world has a save, so only the new one is Unfinished.
  await expect(page.locator("ul.worlds .badge", { hasText: "Unfinished" })).toHaveCount(1);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/d1-worlds-unfinished-${info.project.name}.png`, fullPage: true });

  await page.getByRole("link", { name: "Half made", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Unfinished");
  await expect(page.locator("#world-unfinished")).toContainText("Setting up this world is not finished.");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/d1-world-unfinished-${info.project.name}.png`, fullPage: true });
  await page.getByRole("link", { name: "Continue setting up" }).click();
  await expect(page).toHaveURL(new RegExp(`${newMapPath("half-made")}`));

  // From the list too, its name read with it.
  await page.goto("/worlds");
  await page.getByRole("link", { name: "Continue setting up Half made" }).click();
  await expect(page).toHaveURL(new RegExp(`${newMapPath("half-made")}`));
});

// SPEC 5.6: without script, Preview map on step 3 answers with step 3, the
// pictures under its form.
test.describe("step 3 without script", () => {
  test.use({ javaScriptEnabled: false });
  test("Preview map shows the pictures on step 3", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
    await page.getByLabel("World name").fill("No script");
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/no-script/mods?generated=1");
    await page.goto(newMapPath("no-script"));
    await expect(page.locator("#generate-form")).toBeVisible({ timeout: 60_000 });
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Map settings");
    await expect(page.locator("#newmap-save")).toBeVisible();
    await expect(page.locator(".gen-preview .pictures img").first()).toBeVisible();
  });
});
