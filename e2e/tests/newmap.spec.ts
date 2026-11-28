import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { openNewMap, newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 17.9, 17.10, 17.19: step 3 of the guided Generate sets the map
// settings of a world whose map is not made yet, from the planets its mods add.
test.use({ fakeGameOn: true });

const worldJSON = (dataDir: string, id: string) => JSON.parse(readFileSync(join(dataDir, "worlds", id, "world.json"), "utf8"));
const exchange = (name: string) => readFileSync(join(import.meta.dirname, "..", "..", "internal", "mapexchange", "unittestdata", name), "utf8").trim();

async function setUp(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  await page.goto("/");
}

// Adds the fake planet mod to a world and enables it, as a Mods install would.
function addFakePlanet(dataDir: string, id: string, on = true) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: [{ name: "base", enabled: true }] }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat(on ? [{ name: "fake-planet", enabled: true }] : []);
  writeFileSync(listFile, JSON.stringify(list));
}

test("step 3 shows the step bar, saves a preset and lands on a next step", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const id = await openNewMap(page, { name: "Step three" });
  const steps = page.getByRole("navigation", { name: "Steps to make the world" });
  // Four steps; only built-in mods, so step 3 has nothing to set and is plain text (SPEC 5.6).
  await expect(steps.getByRole("listitem")).toHaveCount(4);
  await expect(steps.getByRole("link")).toHaveCount(3);
  await expect(steps.getByRole("listitem").nth(2)).toHaveText("3. Mod settings (none)");
  await expect(steps.getByRole("link", { name: "4. Map settings" })).toHaveAttribute("aria-current", "step");
  await expect(steps.getByRole("link", { name: "1. World (done)" })).toBeVisible();
  // The footer: Back to step 2, since step 3 has nothing, then the save as the primary button (SPEC 5.6).
  const bar = page.locator("#generate-form > .form-foot");
  await expect(bar.locator("> *").first()).toHaveText("Back to the mods");
  await expect(bar.locator("> *").first()).toHaveAttribute("href", `/worlds/${id}/mods`);
  await expect(bar.locator("> *").last()).toHaveAttribute("id", "newmap-save");
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/step4-${test.info().project.name}.png`, fullPage: true });

  await page.locator("#generate-preset").selectOption("rich-resources");
  await page.locator("#generate-seed").fill("4242");
  // Finish saves and lands on the world's page, with no step between (SPEC 5.6).
  await page.getByRole("button", { name: "Finish" }).click();
  await expect(page).toHaveURL(`/worlds/${id}?setup=done`);
  await expect(page.locator("#world-ready")).toContainText("Done: This world is set up.");
  await checkPage(page);
  const meta = worldJSON(foreman.dataDir, id);
  expect(meta.new_map).toEqual({ preset: "rich-resources", seed: 4242 });
  await page.goto(newMapPath(id));
  await expect(page.locator("#generate-preset")).toHaveValue("rich-resources");
});

test("a world whose map is made says why and offers no form", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto(newMapPath("base"));
  await expect(page.locator("#newmap-why")).toContainText("map is already made");
  await expect(page.locator("#generate-form")).toHaveCount(0);
  await checkPage(page);
});

test("a planet a mod adds is laid out, and one no longer in the mods is listed", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  await page.goto(newMapPath(id));
  await expect(page.locator('#generate-form details[data-planet="fake-planet"]')).toHaveCount(1, { timeout: 60_000 });
  await expect(page.locator("#newmap-builtin")).toHaveCount(0);

  // Saved settings naming fake-ore, then the mod removed (SPEC 5.6).
  writeFileSync(join(foreman.dataDir, "worlds", id, "config", "map-gen-settings.json"), JSON.stringify({ autoplace_controls: { "fake-ore": { frequency: 1, size: 1, richness: 1 } } }));
  addFakePlanet(foreman.dataDir, id, false);
  await page.goto(newMapPath(id));
  await expect(page.locator("#newmap-gone")).toContainText("fake-ore", { timeout: 60_000 });
  await checkPage(page);
  await expect(page.locator('#generate-form details[data-planet="fake-planet"]')).toHaveCount(0);
});

test("a map exchange string fills the form; one of another line is refused", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await openNewMap(page);
  await page.getByText("Fill from a map exchange string").click();
  await page.locator("#exchange-string").fill(exchange("2.0.77.txt"));
  await page.getByRole("button", { name: "Fill the settings" }).click();
  await expect(page.locator("#exchange-string-error")).toContainText("Change the world's game version on its page");
  // The message carries the link that does what it says (SPEC 17.9).
  await expect(page.locator("#exchange-to-world")).toHaveAttribute("href", /^\/worlds\/[^/]+$/);
  await checkPage(page);

  // The box stays open after the refusal.
  await expect(page.locator("#exchange-fill")).toHaveAttribute("open", "");
  await page.locator("#exchange-string").fill(exchange("2.1.24-custom.txt"));
  await page.getByRole("button", { name: "Fill the settings" }).click();
  await expect(page.locator("#exchange-string-error")).toHaveCount(0);
  await expect(page.locator("#preset-modified")).toHaveText("[modified]");
  // The filled settings are not saved yet, so leaving asks (SPEC 5.6).
  await page.getByRole("link", { name: "Worlds", exact: true }).first().click();
  await expect(page.locator("#leave-dialog")).toBeVisible();
});

test("leaving step 3 with a changed setting asks first", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await openNewMap(page);
  await page.locator("#generate-seed").fill("99");
  await page.locator("#flow-back").click();
  await expect(page.locator("#leave-dialog")).toBeVisible();
  await checkPage(page);
  await page.getByRole("button", { name: "Stay on this page" }).click();
  await expect(page.locator("#leave-dialog")).toBeHidden();
});

test("the wait is said once, then the form's arrival, and focus moves to the heading", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const id = await openNewMap(page, { name: "Waiting" });
  // Hold the form back, so the loading box can be looked at.
  let release = () => {};
  const held = new Promise<void>((done) => { release = done; });
  await page.route(/new-map\?wait=1/, async (route) => { await held; await route.continue(); });
  await page.goto(newMapPath(id));
  const status = page.locator("#newmap-load-status");
  await expect(status).toHaveAttribute("role", "status");
  await expect(status).toContainText("Reading the planets");
  await checkPage(page);
  await page.locator("#newmap-load a").focus();
  release();
  await expect(page.locator("#generate-form")).toBeVisible();
  await expect(page.locator("#announcer")).toHaveText("Map settings ready.");
  await expect(page.locator("main .pagehead > h1")).toBeFocused();
});

test("when the world's planets cannot be read, the page says so and the save is refused", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const id = await openNewMap(page, { name: "Unread" });
  addFakePlanet(foreman.dataDir, id);
  // Without the game image the planet run cannot start.
  await fakeGame!.call("/image", { ref: image21, on: false });
  await page.goto(newMapPath(id));
  await expect(page.locator("#newmap-builtin")).toContainText("could not be read", { timeout: 60_000 });
  await expect(page.locator("#newmap-builtin-mods")).toHaveAttribute("href", `/worlds/${id}/mods`);
  await checkPage(page);
  await page.getByRole("button", { name: "Finish" }).click();
  await expect(page.locator("#newmap-save-error")).toContainText("Nothing was saved");
  await expect(page.locator("#newmap-retry")).toHaveAttribute("href", newMapPath(id));
  await expect(page.locator("#newmap-to-mods")).toHaveAttribute("href", `/worlds/${id}/mods`);
  await checkPage(page);
  expect(worldJSON(foreman.dataDir, id).new_map.preset).toBe("");
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("the page reloads itself with the form and saves", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
    await page.locator("#generate-name").fill("Plain");
    await page.locator("#generate-button").click();
    const id = new URL(page.url()).pathname.split("/")[2];
    await page.goto(newMapPath(id));
    await expect(page).toHaveURL(/new-map\?wait=1/);
    await page.locator("#generate-seed").fill("12");
    await page.getByRole("button", { name: "Finish" }).click();
    await expect(page.locator("#world-ready")).toBeVisible();
    expect(worldJSON(foreman.dataDir, id).new_map.seed).toBe(12);
  });
});
