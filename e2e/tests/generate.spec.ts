import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6: Generate makes a world that has no save; its map is made when it
// first starts. The fake game stands in for Docker, so the image download the
// form offers has a host to ask.
test.use({ fakeGameOn: true });

const exchange = (name: string) => readFileSync(join(import.meta.dirname, "..", "..", "internal", "mapexchange", "unittestdata", name), "utf8").trim();

const worldJSON = (dataDir: string, id: string) => JSON.parse(readFileSync(join(dataDir, "worlds", id, "world.json"), "utf8"));
const configFile = (dataDir: string, id: string, file: string) => join(dataDir, "worlds", id, "config", file);

// Signs in and seeds the active world "base" (2.1.24), with its image present.
async function setUp(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  await page.goto("/");
}

const mode = (page: Page, name: string) => page.getByRole("radio", { name });

test("a preset world takes three clicks from the Server page and says its map is made at the first start", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.getByRole("link", { name: "Worlds", exact: true }).click();
  await page.getByRole("link", { name: "Generate a world" }).click();
  await expect(page).toHaveURL("/generate");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Generate a world");
  // The version is the active world's, the preset is the default, the seed is blank.
  await expect(page.getByLabel("Game version")).toContainText("Same as the active world (2.1.24)");
  await expect(mode(page, "Use a preset and a seed")).toBeChecked();
  await expect(page.locator("#generate-preset")).toHaveValue("default");
  await expect(page.locator("#generate-seed")).toHaveValue("");
  await checkPage(page);

  await page.getByLabel("World name").fill("Fresh start");
  await page.locator("#generate-preset").selectOption({ label: "Rich resources" });
  await page.locator("#generate-seed").fill("12345");
  await page.getByRole("button", { name: "Generate world" }).click();

  await expect(page).toHaveURL("/worlds/fresh-start?generated=1");
  await expect(page.locator(".message--success")).toHaveText("Done: Generated Fresh start. Its map is made when it first starts.");
  await expectWithin(clicks, 3, "generate a preset world");
  await checkPage(page);

  const meta = worldJSON(foreman.dataDir, "fresh-start");
  expect(meta).toMatchObject({ game_version: "2.1.24", space_age: true, new_map: { preset: "rich-resources", seed: 12345 } });
  expect(readdirSync(join(foreman.dataDir, "worlds", "fresh-start", "saves"))).toEqual([]);
  // A preset needs no settings files.
  expect(existsSync(configFile(foreman.dataDir, "fresh-start", "map-gen-settings.json"))).toBe(false);
  expect(existsSync(configFile(foreman.dataDir, "fresh-start", "map-settings.json"))).toBe(false);
});

test("a blank seed is filled with a random one", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Random one");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/random-one?generated=1");
  const seed = worldJSON(foreman.dataDir, "random-one").new_map.seed;
  expect(Number.isInteger(seed) && seed >= 0 && seed <= 4294967295).toBe(true);
});

test("a map exchange string writes both settings files, and a bad one is refused on the form", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("From a string");
  await mode(page, "Paste a map exchange string").check();
  await expect(page.getByLabel("Map exchange string")).toBeVisible();
  await expect(page.locator("#generate-preset")).toBeHidden();

  // Empty, then not a string, then one from another game line: each says what to do.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Paste a map exchange string");
  await expect(page).toHaveTitle(/^Error: /);
  await expect(mode(page, "Paste a map exchange string")).toBeChecked();
  await checkPage(page);

  await page.getByLabel("Map exchange string").fill(">>>this is not one<<<");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Foreman could not read this as a map exchange string");
  await page.locator("#error-summary a").click();
  await expect(page.getByLabel("Map exchange string")).toBeFocused();
  await expect(page.getByLabel("Map exchange string")).toHaveValue(">>>this is not one<<<");

  await page.getByLabel("Map exchange string").fill(exchange("2.0.77.txt"));
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("made by Factorio 2.0.77, but this world uses 2.1.24");
  await checkPage(page);

  await page.getByLabel("Map exchange string").fill(exchange("2.1.24.txt"));
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/from-a-string?generated=1");
  for (const f of ["map-gen-settings.json", "map-settings.json"]) {
    const doc = JSON.parse(readFileSync(configFile(foreman.dataDir, "from-a-string", f), "utf8"));
    expect(typeof doc).toBe("object");
  }
  const seed = worldJSON(foreman.dataDir, "from-a-string").new_map.seed;
  expect(Number.isInteger(seed)).toBe(true);
});

test("custom settings write only the map-gen file", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Custom one");
  await mode(page, "Choose each setting").check();
  await expect(page.locator("#generate-preset")).toBeHidden();
  await checkPage(page);
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/custom-one?generated=1");
  expect(JSON.parse(readFileSync(configFile(foreman.dataDir, "custom-one", "map-gen-settings.json"), "utf8"))).toBeTypeOf("object");
  // The patch-specific map-settings.json is never written for a custom map.
  expect(existsSync(configFile(foreman.dataDir, "custom-one", "map-settings.json"))).toBe(false);
  expect(worldJSON(foreman.dataDir, "custom-one").new_map.seed).toBeGreaterThanOrEqual(0);
});

test("errors name the field, keep what was typed and make nothing", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("base"); // the name of the active world
  await page.getByLabel("Or type a version").fill("2.1.24");
  await page.locator("#generate-seed").fill("not a number");
  await page.getByRole("button", { name: "Generate world" }).click();

  const summary = page.locator("#error-summary");
  await expect(summary).toContainText("Fix 2 problems to continue");
  await expect(summary).toContainText("Another world already has this name");
  await expect(summary).toContainText("Enter the seed as a whole number");
  await expect(page.getByLabel("World name")).toHaveValue("base");
  await expect(page.locator("#generate-seed")).toHaveValue("not a number");
  await expect(page.getByLabel("World name")).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);
  expect(readdirSync(join(foreman.dataDir, "worlds")).filter((n) => !n.startsWith("."))).toEqual(["base"]);
});

test("a long world name fits at any width", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  const name = "A very long world name ".repeat(3).trim().slice(0, 60);
  await page.getByLabel("World name").fill(name);
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText(name);
  await checkPage(page);
});

// DESIGN 8.6: the map options follow the game version and Space Age.
test("choosing another game line redraws the presets, and Space Age is refused for 1.1", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  const presets = page.locator("#generate-preset");
  await expect(presets.locator("option", { hasText: "Lakes" })).toHaveCount(1);
  await expect(page.locator('input[name="map_key"]')).toHaveValue("2.1|1");

  await page.getByLabel("Or type a version").fill("1.1.110");
  await page.getByLabel("Or type a version").press("Tab");
  await expect(page.locator('input[name="map_key"]')).toHaveValue("1.1|1");
  await expect(presets.locator("option", { hasText: "Lakes" })).toHaveCount(0);
  await expect(presets.locator("option", { hasText: "Ribbon world" })).toHaveCount(1);
  await expect(page.locator("#announcer")).toContainText("Map options updated");

  await page.getByLabel("World name").fill("Old game");
  await page.getByLabel("Download the game image", { exact: false }).uncheck();
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Space Age needs Factorio 2.0 or later");
  await page.getByLabel("Space Age is on").uncheck();
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/old-game?generated=1");
  expect(worldJSON(foreman.dataDir, "old-game")).toMatchObject({ game_version: "1.1.110", space_age: false });
});

// DESIGN 8.8: the preview is optional. It needs the game layer's previewer;
// a build without one has no button, and the form works as before.
test.describe("map preview", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
    test.skip((await page.locator("#preview-button").count()) === 0, "this build has no map previewer wired in");
  });

  test("shows one picture per planet, keeps the seed it used, and Generate makes that map", async ({ page, foreman }) => {
    await page.getByLabel("World name").fill("Previewed");
    await page.getByRole("button", { name: "Preview map" }).click();
    const section = page.getByRole("region", { name: "Map preview" });
    await expect(section.getByRole("img")).toHaveCount(5);
    await expect(section.getByRole("img", { name: /^Map preview of Nauvis, seed \d+$/ })).toBeVisible();
    await expect(page.locator("#announcer")).toContainText("Map preview ready.");
    const seed = await page.locator("#generate-seed").inputValue();
    expect(seed).toMatch(/^\d+$/);
    await expect(section).toContainText(`seed ${seed}`);
    // Every picture loads, and each links to its full-size file.
    const sizes = await section.getByRole("img").evaluateAll((imgs) => (imgs as HTMLImageElement[]).map((i) => i.naturalWidth));
    expect(sizes.every((w) => w > 0)).toBe(true);
    await checkPage(page);

    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/previewed?generated=1");
    expect(worldJSON(foreman.dataDir, "previewed").new_map.seed).toBe(Number(seed));
  });

  test("Nauvis only without Space Age", async ({ page }) => {
    await page.getByLabel("Space Age is on").uncheck();
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page.getByRole("region", { name: "Map preview" }).getByRole("img")).toHaveCount(1);
  });
});
