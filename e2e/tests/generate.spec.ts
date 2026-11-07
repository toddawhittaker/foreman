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

const mode = (page: Page, name: string) => page.getByRole("radio", { name, exact: true });
// A tab is a radio drawn as a tab; it is picked through its label.
const tab = (page: Page, name: string) => page.locator("label.tab", { hasText: new RegExp(`^\s*${name}\s*$`) });

// The typed version sits in a disclosure, opened first.
async function typedVersion(page: Page) {
  const d = page.locator("details.disclosure");
  if (!(await d.evaluate((e) => (e as HTMLDetailsElement).open))) await d.locator("summary").click();
  return page.getByLabel("Or type a version");
}

// Sets a setting of steps through its select, which is what the form sends
// whether the page shows the select or the slider.
async function setStep(page: Page, id: string, code: string) {
  await page.locator(`#${id}`).evaluate((s, v) => {
    (s as HTMLSelectElement).value = v;
    s.dispatchEvent(new Event("change", { bubbles: true }));
  }, code);
}

test("a preset world takes three clicks from the Server page and says its map is made at the first start", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.getByRole("link", { name: "Worlds", exact: true }).click();
  await page.getByRole("link", { name: "Generate a world" }).click();
  await expect(page).toHaveURL("/generate");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Generate a world");
  // The version is the active world's, the preset is the default, the seed is blank, or the one a preview drawn on load used.
  await expect(page.getByLabel("Game version")).toContainText("2.1.24, same as the active world");
  await expect(mode(page, "Map settings")).toBeChecked();
  await expect(page.locator("#generate-preset")).toHaveValue("default");
  await expect(page.locator("#generate-seed")).toHaveValue(/^\d*$/);
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
  await mode(page, "Map exchange string").check();
  await expect(page.locator("#exchange-string")).toBeVisible();
  await expect(page.locator("#generate-preset")).toBeHidden();

  // Empty, then not a string, then one from another game line: each says what to do.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Paste a map exchange string");
  await expect(page).toHaveTitle(/^Error: /);
  await expect(mode(page, "Map exchange string")).toBeChecked();
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

// SPEC 5.6: any change writes both files in full and passes no preset.
test("a changed setting writes both files and no preset", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Custom one");
  await tab(page, "Advanced").click();
  await page.getByLabel("Width").fill("512");
  await expect(page.locator("#preset-modified")).toHaveText("[modified]");
  await checkPage(page);
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/custom-one?generated=1");
  expect(JSON.parse(readFileSync(configFile(foreman.dataDir, "custom-one", "map-gen-settings.json"), "utf8")).width).toBe(512);
  const ms = JSON.parse(readFileSync(configFile(foreman.dataDir, "custom-one", "map-settings.json"), "utf8"));
  expect(ms.enemy_expansion.evolution_group_size_factor).toBe(8);
  expect(worldJSON(foreman.dataDir, "custom-one").new_map.preset ?? "").toBe("");
});

test("errors name the field, keep what was typed and make nothing", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("base"); // the name of the active world
  await (await typedVersion(page)).fill("2.1.24");
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

  await (await typedVersion(page)).fill("1.1.110");
  await (await typedVersion(page)).press("Tab");
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

  test("changed settings take the seed too, and Nauvis only without Space Age", async ({ page }) => {
    await setStep(page, "mg-nauvis-iron-ore-size", "200");
    await page.getByLabel("World name").fill("Custom preview");
    await page.getByRole("button", { name: "Preview map" }).click();
    const section = page.getByRole("region", { name: "Map preview" });
    await expect(section.getByRole("img")).toHaveCount(5);
    expect(await page.locator("#generate-seed").inputValue()).toMatch(/^\d+$/);
    await page.getByLabel("Space Age is on").uncheck();
    // Turning Space Age off redraws the settings for Nauvis alone; a new preview has one picture.
    await expect(page.locator("#panel-resources details")).toHaveCount(0);
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
    await mode(page, "Map exchange string").check();
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

// DESIGN 8.7: choosing a preset fills the tabs with its values, in the game's
// own words for terrain controls; left unchanged it is passed as the preset.
test("a preset fills the tabs and words terrain as the game does", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Rail");
  await page.locator("#generate-preset").selectOption({ label: "Rail world" });
  await expect(page.locator("#mg-nauvis-iron-ore-size")).toHaveValue("300");
  // The select itself is not redrawn, so it keeps its choice.
  await expect(page.locator("#generate-preset")).toHaveValue("rail-world");
  await expect(page.locator("#mg-nauvis-iron-ore-frequency")).toHaveValue("33");
  await expect(page.locator("#mg-nauvis-water-size")).toHaveValue("150");
  await expect(page.locator("#mg-nauvis-water-frequency")).toHaveValue("200");
  await expect(page.locator("#preset-modified")).toHaveText("");
  await tab(page, "Terrain").click();
  const trees = page.getByRole("group", { name: "Trees", exact: true });
  await expect(trees.getByText("Scale")).toBeVisible();
  await expect(trees.getByText("Coverage")).toBeVisible();
  await expect(trees.getByText("Frequency")).toHaveCount(0);
  await tab(page, "Resources").click();
  const iron = page.getByRole("group", { name: "Iron ore", exact: true }).first();
  await expect(iron.getByText("Frequency")).toBeVisible();
  await checkPage(page);

  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/rail?generated=1");
  expect(worldJSON(foreman.dataDir, "rail").new_map.preset).toBe("rail-world");
  expect(existsSync(configFile(foreman.dataDir, "rail", "map-gen-settings.json"))).toBe(false);
  expect(existsSync(configFile(foreman.dataDir, "rail", "map-settings.json"))).toBe(false);
});

// SPEC 17.3: a preset change refills the settings in place, so nothing is
// read out over the select that is reading its new option.
test("choosing a preset announces nothing", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption({ label: "Rail world" });
  await expect(page.locator("#mg-nauvis-iron-ore-size")).toHaveValue("300");
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

// DESIGN 8.7 (ruling 2): the settings are four tabs, drawn as tabs from radios,
// so the group is one tab stop and the arrow keys move between them.
test.describe("the map settings tabs", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
  });

  test("show one panel at a time, and the arrow keys move between them", async ({ page }) => {
    const panels = ["resources", "terrain", "enemy", "advanced"];
    const shown = async () => {
      const out: string[] = [];
      for (const p of panels) if (await page.locator(`#panel-${p}`).isVisible()) out.push(p);
      return out;
    };
    expect(await shown()).toEqual(["resources"]);
    await tab(page, "Enemy").click();
    expect(await shown()).toEqual(["enemy"]);
    await expect(page.getByRole("checkbox", { name: "Peaceful mode" })).toBeVisible();
    await page.keyboard.press("ArrowRight");
    await expect(page.locator("#tab-advanced")).toBeFocused();
    expect(await shown()).toEqual(["advanced"]);
    await expect(page.getByLabel("Price multiplier")).toBeVisible();
    // The tab radios are one stop: Tab leaves them for the panel's first field.
    await page.keyboard.press("Tab");
    await expect(page.locator("#panel-advanced :focus")).toHaveCount(1);
    await checkPage(page);
  });

  test("an error in a hidden tab is reached from the summary", async ({ page }) => {
    await page.getByLabel("World name").fill("Bad");
    await tab(page, "Enemy").click();
    await page.getByLabel("Time factor").fill("fast");
    await tab(page, "Resources").click();
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page.locator("#error-summary")).toContainText("Enter a number of 0 or more for Time factor.");
    // The page comes back on the tab that holds the error.
    await expect(page.locator("#panel-enemy")).toBeVisible();
    await tab(page, "Resources").click();
    await page.locator("#error-summary a").click();
    await expect(page.getByLabel("Time factor")).toBeFocused();
    await expect(page.getByLabel("Time factor")).toHaveValue("fast");
  });
});

// DESIGN 8.7: sliders over the game's steps on a wide panel, selects of the
// same steps on a narrow one; each shows its value in words.
test("steps are sliders on a wide panel and selects on a phone", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  const select = page.locator("#mg-nauvis-iron-ore-size");
  const slider = page.locator("#mg-nauvis-iron-ore-size-range");
  const value = page.locator("#mg-nauvis-iron-ore-size-value");
  if ((page.viewportSize()?.width ?? 1280) > 600) {
    await expect(slider).toBeVisible();
    await expect(select).toBeHidden();
    await expect(slider).toHaveAccessibleName("Size");
    await expect(value).toHaveText("100% (default)");
    await slider.focus();
    await page.keyboard.press("ArrowRight");
    await expect(select).toHaveValue("141");
    await expect(value).toHaveText("141%");
    await expect(slider).toHaveAttribute("aria-valuetext", "141%");
    // The value's box keeps its place: the slider does not move as the words change.
    const before = (await slider.boundingBox())!;
    await page.keyboard.press("End");
    await expect(select).toHaveValue("600");
    expect((await slider.boundingBox())!.width).toBe(before.width);
  } else {
    await expect(select).toBeVisible();
    await expect(slider).toBeHidden();
    await select.selectOption("141");
  }
  await expect(page.locator("#preset-modified")).toHaveText("[modified]");
  await checkPage(page);
});

// SPEC 5.6: the Terrain tab's map type and scales, and a resource's on/off
// box, as the game's map generator has them. Off writes size 0, the game's
// own off value; map type and scales go into property_expression_names.
test("a resource switched off, a map type, a scale and a bias reach the file", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Dry island");
  const iron = page.getByRole("checkbox", { name: "Iron ore", exact: true });
  await expect(iron).toBeChecked();
  await iron.uncheck();
  await expect(page.locator("#preset-modified")).toHaveText("[modified]");
  await tab(page, "Terrain").click();
  const mapType = page.getByLabel("Map type");
  await expect(mapType).toBeVisible();
  await expect(page.locator("#mg-map-type-range")).toHaveCount(0);
  await mapType.selectOption({ label: "Island elevation" });
  await setStep(page, "mg-moisture-scale", "200");
  await expect(page.locator("#mg-moisture-group legend")).toHaveText("Moisture");
  // Bias is a slider on a wide panel and a select on a phone, as every step is.
  const bias = page.locator("#mg-aux-bias");
  const biasSlider = page.locator("#mg-aux-bias-range");
  if ((page.viewportSize()?.width ?? 1280) > 600) {
    await expect(biasSlider).toBeVisible();
    await expect(biasSlider).toHaveAccessibleName("Bias");
    await expect(page.locator("#mg-aux-bias-value")).toHaveText("0.00 (default)");
    await biasSlider.focus();
    await page.keyboard.press("Home");
    await expect(bias).toHaveValue("-50");
  } else {
    await expect(bias).toBeVisible();
    await expect(biasSlider).toBeHidden();
    await bias.selectOption("-50");
  }
  await checkPage(page);
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/dry-island?generated=1");
  const mg = JSON.parse(readFileSync(configFile(foreman.dataDir, "dry-island", "map-gen-settings.json"), "utf8"));
  expect(mg.autoplace_controls["iron-ore"].size).toBe(0);
  expect(mg.autoplace_controls["copper-ore"].size).toBe(1);
  expect(mg.property_expression_names.elevation).toBe("elevation_island");
  expect(mg.property_expression_names["control:moisture:frequency"]).toBe("0.5");
  expect(mg.property_expression_names["control:aux:frequency"]).toBe("1");
  expect(mg.property_expression_names["control:aux:bias"]).toBe("-0.5");
  // A bias of 0 is left out, as the game's own generator writes it.
  expect(mg.property_expression_names["control:moisture:bias"]).toBeUndefined();
  expect(worldJSON(foreman.dataDir, "dry-island").new_map.preset ?? "").toBe("");
});

test("the island preset fills the map type, and default leaves it out of the file", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption({ label: "Island" });
  await tab(page, "Terrain").click();
  await expect(page.getByLabel("Map type")).toHaveValue("elevation_island");
  await page.locator("#generate-preset").selectOption({ label: "Default" });
  await expect(page.getByLabel("Map type")).toHaveValue("elevation");
  await page.getByLabel("World name").fill("Plain");
  await setStep(page, "mg-aux-scale", "50");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/plain?generated=1");
  const mg = JSON.parse(readFileSync(configFile(foreman.dataDir, "plain", "map-gen-settings.json"), "utf8"));
  expect(mg.property_expression_names.elevation).toBeUndefined();
  expect(mg.property_expression_names["control:aux:frequency"]).toBe("2");
});

// DESIGN 8.6 (ruling 2): "[modified]" shows once anything changes, with a reset.
test("[modified] shows once a setting changes, and Reset puts the preset back", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption({ label: "Death world" });
  await expect(page.locator("#mg-nauvis-enemy-base-size")).toHaveValue("200");
  const mark = page.locator("#preset-modified");
  const reset = page.getByRole("button", { name: "Reset to preset default" });
  await expect(mark).toHaveText("");
  await expect(reset).toBeHidden();
  await page.locator("#generate-seed").fill("5");
  await expect(mark).toHaveText("");
  await tab(page, "Enemy").click();
  await page.getByLabel("Maximum expansion distance").fill("3");
  await expect(mark).toHaveText("[modified]");
  await expect(reset).toBeVisible();
  await page.getByLabel("Maximum expansion distance").fill("5");
  await expect(mark).toHaveText("");
  await page.getByLabel("Maximum expansion distance").fill("2");
  await reset.click();
  await expect(page.getByLabel("Maximum expansion distance")).toHaveValue("5");
  await expect(mark).toHaveText("");
  await expect(page.locator("#generate-seed")).toHaveValue("5");
  await checkPage(page);
});

// SPEC 5.6 (#235): Random seed fills a new seed from 0 to 4294967295.
test("Random seed fills a seed the game takes", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  const seed = page.locator("#generate-seed");
  const seen = new Set<string>();
  for (let i = 0; i < 3; i++) {
    await page.getByRole("button", { name: "Random seed" }).click();
    const v = await seed.inputValue();
    expect(v).toMatch(/^\d+$/);
    expect(Number(v)).toBeLessThanOrEqual(4294967295);
    seen.add(v);
  }
  expect(seen.size).toBeGreaterThan(1);
  await page.getByLabel("World name").fill("Lucky");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/lucky?generated=1");
  expect(String(worldJSON(foreman.dataDir, "lucky").new_map.seed)).toBe(await Promise.resolve([...seen].at(-1)));
});

// SPEC 5.6 (ruling 3, #239): a warning beside a setting as soon as it turns
// achievements off, and one summary by the Generate button.
test("settings that turn achievements off are warned of at once", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  const summary = page.locator("#ach-summary");
  await expect(summary).toBeHidden();
  await tab(page, "Enemy").click();
  await page.getByRole("checkbox", { name: "Peaceful mode" }).check();
  await expect(page.locator("#mg-peaceful-ach")).toHaveText("Turns off some achievements.");
  await expect(page.getByRole("checkbox", { name: "Peaceful mode" })).toHaveAccessibleDescription(/Turns off some achievements\./);
  await expect(summary).toBeVisible();
  await expect(summary.getByRole("listitem")).toHaveText(["Cannot be unlocked in peaceful mode."]);
  await expect(page.locator("#announcer")).toHaveText("Peaceful mode turns off some achievements.");
  await page.getByLabel("Time factor").fill("20");
  await expect(page.locator("#ms-enemy-evolution-time-factor-ach")).toHaveText("Turns off some achievements.");
  await expect(summary.getByRole("listitem")).toHaveText(["Cannot be unlocked in peaceful mode.", "Cannot be unlocked with easier enemy evolution settings."]);
  await checkPage(page);
  // The server says the same of the page it sends back.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toBeVisible();
  await expect(page.locator("#mg-peaceful-ach")).toHaveText("Turns off some achievements.");
  await expect(summary.getByRole("listitem")).toHaveText(["Cannot be unlocked in peaceful mode.", "Cannot be unlocked with easier enemy evolution settings."]);
  await page.getByRole("checkbox", { name: "Peaceful mode" }).uncheck();
  await page.getByLabel("Time factor").fill("40");
  await expect(summary).toBeHidden();
});

// At 320 px and 200% text, the settings still fit and keep their selects.
test("at 320 px and 200% text the settings fit", async ({ page, foreman, fakeGame }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await page.evaluate(() => {
    document.documentElement.style.fontSize = "200%";
  });
  for (const name of ["Resources", "Terrain", "Enemy", "Advanced"]) {
    await tab(page, name).click();
    const size = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
    expect(size.scroll, `${name} at 200%`).toBeLessThanOrEqual(size.client);
  }
  await checkPage(page);
});

// Without script the tabs stack and the server does every redraw; a new
// preset is filled before anything is made (ruling 2, filled_from).
test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("the tabs stack, and a new preset is filled before the world is made", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
    for (const p of ["resources", "terrain", "enemy", "advanced"]) await expect(page.locator(`#panel-${p}`)).toBeVisible();
    await expect(page.locator(".tabset")).toBeHidden();
    await expect(page.getByRole("button", { name: "Random seed" })).toBeHidden();
    await expect(page.locator("#mg-nauvis-iron-ore-size")).toBeVisible();

    await page.getByLabel("World name").fill("Hard");
    await page.locator("#generate-preset").selectOption({ label: "Death world" });
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page.locator("#map-refilled")).toContainText("The settings below were filled from the preset “Death world”.");
    await expect(page.locator("#mg-nauvis-enemy-base-size")).toHaveValue("200");
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/hard?generated=1");
    expect(worldJSON(foreman.dataDir, "hard").new_map.preset).toBe("death-world");

    // Reset works by a round trip.
    await page.goto("/generate");
    await page.getByLabel("Width").fill("64");
    await page.getByRole("button", { name: "Generate world" }).click(); // no name: shown again, changed
    await expect(page.locator("#preset-modified")).toHaveText("[modified]");
    await page.getByRole("button", { name: "Reset to preset default" }).click();
    await expect(page.getByLabel("Width")).toHaveValue("0");
  });
});
