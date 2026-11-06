import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
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
  await expect(page.getByLabel("Game version")).toContainText("2.1.24, same as the active world");
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
  await expect(page.locator("#exchange-string")).toBeVisible();
  await expect(page.locator("#generate-preset")).toBeHidden();

  // Empty, then not a string, then one from another game line: each says what to do.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Paste a map exchange string");
  await expect(page).toHaveTitle(/^Error: /);
  await expect(mode(page, "Paste a map exchange string")).toBeChecked();
  await checkPage(page);

  await page.locator("#exchange-string").fill(">>>this is not one<<<");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Foreman could not read this as a map exchange string");
  await page.locator("#error-summary a").click();
  await expect(page.locator("#exchange-string")).toBeFocused();
  await expect(page.locator("#exchange-string")).toHaveValue(">>>this is not one<<<");

  await page.locator("#exchange-string").fill(exchange("2.0.77.txt"));
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("made by Factorio 2.0.77, but this world uses 2.1.24");
  await checkPage(page);

  await page.locator("#exchange-string").fill(exchange("2.1.24.txt"));
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
  expect(typeof JSON.parse(readFileSync(configFile(foreman.dataDir, "custom-one", "map-gen-settings.json"), "utf8"))).toBe("object");
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
  await expect(summary).toContainText("Enter the seed as a whole number");
  await expect(page.locator("#generate-seed")).toHaveValue("not a number");
  await page.locator("#generate-seed").fill("9");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(summary).toContainText("Another world already has this name");
  await summary.getByRole("link").click();
  await expect(page.getByLabel("World name")).toBeFocused();
  await expect(page.getByLabel("World name")).toHaveValue("base");
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
  await expect(page.locator('input[name="map_key"]')).toHaveValue("1.1|0");
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

// DESIGN 8.8: the preview is optional and adds no screen to Generate.
test.describe("map preview", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
  });

  test("shows one picture per planet, keeps the seed it used, and Generate makes that map", async ({ page, foreman }) => {
    await page.getByLabel("World name").fill("Previewed");
    await page.getByRole("button", { name: "Preview map" }).click();
    const section = page.getByRole("region", { name: "Map preview" });
    await expect(section.getByRole("img")).toHaveCount(5);
    await expect(section.getByRole("img", { name: /^Map preview of Nauvis, seed \d+$/ })).toBeVisible();
    await expect(page.locator("#announcer")).toContainText("Map preview ready.");
    // The blank seed now holds the one the pictures used.
    const seed = await page.locator("#generate-seed").inputValue();
    expect(seed).toMatch(/^\d+$/);
    await expect(section).toContainText(`seed ${seed}`);
    const sizes = await section.getByRole("img").evaluateAll((imgs) => (imgs as HTMLImageElement[]).map((i) => i.naturalWidth));
    expect(sizes.every((w) => w > 0)).toBe(true);
    await checkPage(page);

    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/previewed?generated=1");
    expect(worldJSON(foreman.dataDir, "previewed").new_map.seed).toBe(Number(seed));
  });

  test("the custom form takes the seed too, and Nauvis only without Space Age", async ({ page, foreman }) => {
    await mode(page, "Choose each setting").check();
    await page.getByLabel("World name").fill("Custom preview");
    await page.getByRole("button", { name: "Preview map" }).click();
    const section = page.getByRole("region", { name: "Map preview" });
    await expect(section.getByRole("img")).toHaveCount(5);
    const seed = await page.locator("#mg-seed").inputValue();
    expect(seed).toMatch(/^\d+$/);
    await page.getByLabel("Space Age is on").uncheck();
    // Turning Space Age off redraws the planet groups (Nauvis and the raw box are left); a new preview has one picture.
    await expect(page.locator("#custom-fields details")).toHaveCount(2);
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page.getByRole("region", { name: "Map preview" }).getByRole("img")).toHaveCount(1);
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/custom-preview?generated=1");
  });

  test("a game that fails says so, and Generate still works", async ({ page, foreman, fakeGame }) => {
    await fakeGame!.call("/preview-mode", { mode: "fail" });
    await page.getByLabel("World name").fill("No picture");
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page.locator("#preview .message--error")).toContainText("The game could not make the map preview. You can still generate the world.");
    await expect(page.getByRole("button", { name: "Generate world" })).toBeEnabled();
    await checkPage(page);
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/no-picture?generated=1");
  });

  test("a bad form is shown again whole, with its errors", async ({ page }) => {
    await mode(page, "Paste a map exchange string").check();
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page.locator("#error-summary")).toContainText("Paste a map exchange string");
    await expect(page.locator("#error-summary")).toBeFocused();
  });
});

// DESIGN 8.8, SPEC 17.3: the person who pressed Preview map keeps their place,
// hears what happened, and is never told about errors they have fixed.
test.describe("the preview keeps focus and says what happened", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
    await page.getByLabel("World name").fill("Focus");
  });

  test("focus stays on Preview map while it runs and after the pictures arrive", async ({ page }) => {
    const button = page.getByRole("button", { name: "Preview map" });
    // The answer is held back for a moment, so the busy state can be seen.
    await page.route("**/generate/preview", async (route) => {
      await new Promise((r) => setTimeout(r, 1000));
      await route.continue();
    });
    await button.focus();
    await page.keyboard.press("Enter");
    // Busy is aria-disabled, never disabled, so the button keeps focus.
    await expect(button).toHaveAttribute("aria-disabled", "true");
    await expect(button).not.toHaveAttribute("disabled", "");
    await expect(button).toBeFocused();
    await expect(page.locator("#preview-wait")).toBeVisible();

    await expect(page.getByRole("region", { name: "Map preview" }).getByRole("img")).toHaveCount(5);
    await expect(button).toBeFocused();
    await expect(button).not.toHaveAttribute("aria-disabled", "true");
    await expect(page.locator("#announcer")).toContainText("Map preview ready.");
  });

  test("a busy Generate does nothing, even from the Enter key in a text field", async ({ page }) => {
    await page.route("**/generate/preview", async (route) => {
      await new Promise((r) => setTimeout(r, 1500));
      await route.continue();
    });
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page.getByRole("button", { name: "Preview map" })).toHaveAttribute("aria-disabled", "true");
    await page.getByLabel("World name").press("Enter");
    await page.getByRole("button", { name: "Generate world" }).click({ force: true });
    await expect(page.getByRole("region", { name: "Map preview" }).getByRole("img")).toHaveCount(5);
    // Neither attempt made the world: the page is still the form.
    await expect(page).toHaveURL("/generate");
  });

  test("an old error summary is dropped, and the preview is still announced", async ({ page }) => {
    await page.getByLabel("World name").fill("");
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page.locator("#error-summary")).toBeFocused();
    await page.getByLabel("World name").fill("Fixed");
    const button = page.getByRole("button", { name: "Preview map" });
    await button.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("region", { name: "Map preview" }).getByRole("img")).toHaveCount(5);
    await expect(page.locator("#error-summary")).toHaveCount(0);
    await expect(page.locator("#announcer")).toContainText("Map preview ready.");
    await expect(button).toBeFocused();
  });

  test("an expired session sends the person to sign-in and announces no success", async ({ page }) => {
    await page.getByLabel("World name").fill("Expired");
    await page.context().clearCookies();
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(page).toHaveURL(/\/signin/);
    await expect(page.locator("#announcer")).not.toContainText("Map preview ready.");
  });

  test("a failed request writes a sentence in the preview and the announcer", async ({ page }) => {
    const button = page.getByRole("button", { name: "Preview map" });
    await page.route("**/generate/preview", (route) => route.fulfill({ status: 500, body: "boom" }));
    await button.focus();
    await page.keyboard.press("Enter");
    const message = page.locator("#preview .message--error");
    await expect(message).toContainText("The map preview request failed.");
    await expect(page.locator("#announcer")).toContainText("The map preview request failed.");
    await expect(button).toBeFocused();
    await expect(button).not.toHaveAttribute("aria-disabled", "true");
    await checkPage(page);

    await page.unroute("**/generate/preview");
    await page.route("**/generate/preview", (route) => route.abort("connectionreset"));
    await page.getByRole("button", { name: "Preview map" }).press("Enter");
    await expect(page.locator("#announcer")).toContainText("The map preview request failed.");
    await expect(page.locator("#preview .message--error")).toHaveCount(1);
    // The world can still be made.
    await page.unroute("**/generate/preview");
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/focus?generated=1");
  });
});

// SPEC 17.3: Generate can be done with the keyboard alone.
test("a preset world with a seed is made with the keyboard alone, previewing first", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await tabTo(page, page.getByLabel("World name"));
  await page.keyboard.type("Keys only");
  await tabTo(page, page.locator("#generate-preset"));
  await page.keyboard.type("Rich");
  await expect(page.locator("#generate-preset")).toHaveValue("rich-resources");
  await tabTo(page, page.locator("#generate-seed"));
  await page.keyboard.type("777");
  await tabTo(page, page.getByRole("button", { name: "Preview map" }));
  await page.keyboard.press("Enter");
  await expect(page.getByRole("region", { name: "Map preview" }).getByRole("img")).toHaveCount(5);
  await expect(page.getByRole("button", { name: "Preview map" })).toBeFocused();
  await expect(page.locator("#generate-seed")).toHaveValue("777");
  // The primary button comes first in the action row.
  await page.keyboard.press("Shift+Tab");
  await expect(page.getByRole("button", { name: "Generate world" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/worlds/keys-only?generated=1");
  expect(worldJSON(foreman.dataDir, "keys-only")).toMatchObject({ new_map: { preset: "rich-resources", seed: 777 } });
});

// DESIGN 8.7: the custom form opens filled with the preset that was chosen, in
// the game's own words for terrain controls.
test("the custom form takes the chosen preset's values and words terrain as the game does", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Rail");
  await page.locator("#generate-preset").selectOption({ label: "Rail world" });
  await expect(page.locator("#mg-preset-note")).toBeAttached();
  // The select was not redrawn, so it keeps focus and its choice.
  await expect(page.locator("#generate-preset")).toHaveValue("rail-world");
  await mode(page, "Choose each setting").check();
  await expect(page.locator("#mg-preset-note")).toContainText("Filled in from the preset “Rail world”.");
  await expect(page.locator("#mg-nauvis-iron-ore-size")).toHaveValue("300");
  await expect(page.locator("#mg-nauvis-iron-ore-frequency")).toHaveValue("33");
  await expect(page.locator("#mg-nauvis-water-size")).toHaveValue("150");
  await expect(page.locator("#mg-nauvis-water-frequency")).toHaveValue("200");
  const trees = page.getByRole("group", { name: "Trees", exact: true });
  await expect(trees.getByLabel("Scale")).toBeVisible();
  await expect(trees.getByLabel("Coverage")).toBeVisible();
  await expect(trees.getByLabel("Frequency")).toHaveCount(0);
  const iron = page.getByRole("group", { name: "Iron ore", exact: true });
  await expect(iron.getByLabel("Frequency")).toBeVisible();
  await expect(iron.getByLabel("Size")).toBeVisible();
  await checkPage(page);

  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/rail?generated=1");
  const doc = JSON.parse(readFileSync(configFile(foreman.dataDir, "rail", "map-gen-settings.json"), "utf8"));
  expect(doc.autoplace_controls["iron-ore"]).toMatchObject({ size: 3 });
  expect(doc.autoplace_controls["iron-ore"].frequency).toBeCloseTo(1 / 3, 6);
  expect(doc.autoplace_controls.water).toMatchObject({ frequency: 0.5, size: 1.5 });

  // Choosing "Default" again puts the plain values back.
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption({ label: "Rich resources" });
  await expect(page.locator("#mg-preset-note")).toContainText("Rich resources");
  await page.locator("#generate-preset").selectOption({ label: "Default" });
  await expect(page.locator("#mg-preset-note")).toContainText("Default");
  await mode(page, "Choose each setting").check();
  await expect(page.locator("#mg-nauvis-iron-ore-richness")).toHaveValue("100");
});

// SPEC 17.3: a preset change only refills the hidden custom form, so nothing
// is read out over the select that is reading its new option.
test("choosing a preset announces nothing", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption({ label: "Rail world" });
  await expect(page.locator("#mg-preset-note")).toBeAttached();
  // The announcer writes 50 ms after a swap; give it time to be wrong.
  await page.waitForTimeout(300);
  await expect(page.locator("#announcer")).toHaveText("");
});

// The version choice is the first thing on the form; its text must show the
// version at 320 px, in every language (SPEC 22.7).
test("the version choice is not cut off at 320 px", async ({ page, foreman, fakeGame }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  const select = page.locator("#generate-version-select");
  await expect(select).toContainText("2.1.24, same as the active world");
  // The text up to the version must fit inside the select's own box.
  const fits = await select.evaluate((el) => {
    const s = el as HTMLSelectElement;
    const cs = getComputedStyle(s);
    const c = document.createElement("canvas").getContext("2d")!;
    c.font = `${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
    const room = s.getBoundingClientRect().width - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight) - 24;
    return { room, version: c.measureText("2.1.24").width, whole: c.measureText(s.selectedOptions[0].text).width, ellipsis: cs.textOverflow };
  });
  expect(fits.version, "the version is the first thing shown").toBeLessThan(fits.room);
  expect(fits.ellipsis).toBe("ellipsis");
});
