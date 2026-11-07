import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import type { Page } from "@playwright/test";

// DESIGN 8.7, SPEC 5.6 and 17: with Space Age each tab shows one row per
// planet. One planet is open at a time, the same in every tab, and the map
// preview follows it. Cliffs can be switched off, and a resource the game
// needs warns while off.
test.use({ fakeGameOn: true });

async function setUp(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

const tab = (page: Page, name: string) => page.locator("label.tab", { hasText: new RegExp(`^\\s*${name}\\s*$`) });
const row = (page: Page, panel: string, planet: string) => page.locator(`#mg-${panel}-${planet}`);
const isOpen = (page: Page, panel: string, planet: string) => row(page, panel, planet).evaluate((d) => (d as HTMLDetailsElement).open);
const openRows = (page: Page, panel: string) =>
  page.locator(`#panel-${panel} details[data-planet]`).evaluateAll((ds) => ds.filter((d) => (d as HTMLDetailsElement).open).map((d) => (d as HTMLElement).dataset.planet));
const pictures = (page: Page) => page.getByRole("region", { name: "Map preview" }).getByRole("img");

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
});

// One click opens a row and it stays open, both before and after the first
// preview arrives (the first click once focused the row, which opened it,
// and the click's own toggle shut it again).
test("one click opens a planet row and it stays open", async ({ page }) => {
  await page.goto("/generate");
  await row(page, "resources", "gleba").locator("summary").click();
  await page.waitForTimeout(2000);
  expect(await isOpen(page, "resources", "gleba")).toBe(true);

  await expect(pictures(page)).toHaveCount(1);
  await row(page, "resources", "fulgora").locator("summary").click();
  await page.waitForTimeout(2000);
  expect(await isOpen(page, "resources", "fulgora")).toBe(true);
});

test("opening a row closes the other and keeps focus on it", async ({ page }) => {
  await page.goto("/generate");
  expect(await openRows(page, "resources")).toEqual(["nauvis"]);
  const summary = row(page, "resources", "vulcanus").locator("summary");
  await summary.click();
  await expect.poll(() => openRows(page, "resources")).toEqual(["vulcanus"]);
  await expect(summary).toBeFocused();
  await expect(page.locator("#open-planet")).toHaveValue("vulcanus");
  await checkPage(page);
});

test("the open planet carries across tabs and over an error round trip", async ({ page }) => {
  await page.goto("/generate");
  await row(page, "resources", "fulgora").locator("summary").click();
  await tab(page, "Terrain").click();
  await expect.poll(() => openRows(page, "terrain")).toEqual(["fulgora"]);
  // The name is left empty, so the form comes back with its error.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toContainText("name");
  expect(await openRows(page, "resources")).toEqual(["fulgora"]);
  expect(await openRows(page, "terrain")).toEqual(["fulgora"]);
});

test("the open row stays open when a preset redraws the settings", async ({ page }) => {
  await page.goto("/generate");
  await row(page, "resources", "fulgora").locator("summary").click();
  await expect(page.locator("#open-planet")).toHaveValue("fulgora");
  await page.locator("#generate-preset").selectOption({ label: "Rail world" });
  await expect(page.locator("#mg-nauvis-iron-ore-size")).toHaveValue("300");
  await page.waitForTimeout(500);
  expect(await openRows(page, "resources")).toEqual(["fulgora"]);
  expect(await openRows(page, "terrain")).toEqual(["fulgora"]);
});

test("opening a row switches the preview to that planet, and says so once", async ({ page }) => {
  await page.goto("/generate");
  await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Nauvis/);
  // Every text the live region is given, in order.
  await page.evaluate(() => {
    const w = window as unknown as { said: string[] };
    w.said = [];
    const a = document.getElementById("announcer")!;
    new MutationObserver(() => { if (a.textContent) w.said.push(a.textContent); }).observe(a, { childList: true, characterData: true, subtree: true });
  });
  const said = () => page.evaluate(() => (window as unknown as { said: string[] }).said.filter((t) => t.startsWith("Preview:")));
  await row(page, "resources", "gleba").locator("summary").click();
  await expect(page.locator("#preview-planet")).toHaveValue("gleba");
  await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Gleba/);
  await expect.poll(said).toEqual(["Preview: Gleba"]);
  // A tab switch shows the same planet and says nothing new.
  await tab(page, "Terrain").click();
  await expect.poll(() => openRows(page, "terrain")).toEqual(["gleba"]);
  await page.waitForTimeout(300);
  expect(await said()).toEqual(["Preview: Gleba"]);
});

test("choosing a planet in the preview opens its row without moving focus", async ({ page }) => {
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  const pick = page.locator("#preview-planet");
  await pick.selectOption("vulcanus");
  await expect.poll(() => openRows(page, "resources")).toEqual(["vulcanus"]);
  // Opening the row moves no focus into the settings.
  expect(await page.evaluate(() => !!document.activeElement?.closest("#map-settings"))).toBe(false);
  await expect(page.locator("#open-planet")).toHaveValue("vulcanus");
  // Another script may ask for a planet too.
  await page.evaluate(() => document.dispatchEvent(new CustomEvent("preview:planet", { detail: { planet: "fulgora" } })));
  await expect.poll(() => openRows(page, "resources")).toEqual(["fulgora"]);
});

test("cliffs can be switched off, which disables their settings and marks the preset modified", async ({ page }) => {
  await page.goto("/generate");
  await tab(page, "Terrain").click();
  const box = page.locator("#mg-nauvis-nauvis_cliff-on");
  await expect(box).toBeChecked();
  await expect(page.locator("#mg-nauvis-nauvis_cliff-size option[value='0']")).toHaveCount(0);
  await box.uncheck();
  await expect(page.locator("#mg-nauvis-nauvis_cliff-frequency")).toBeDisabled();
  await expect(page.locator("#preset-modified")).toHaveText("[modified]");
  await box.check();
  await expect(page.locator("#mg-nauvis-nauvis_cliff-frequency")).toBeEnabled();
  await expect(page.locator("#preset-modified")).toHaveText("");
  await expect(page.locator("#mg-gleba-gleba_cliff-on")).toHaveCount(1);
  await expect(page.locator("#mg-fulgora-fulgora_cliff-on")).toHaveCount(1);
  // Vulcanus has no cliffs box, as in the game.
  await expect(page.locator("#mg-vulcanus-vulcanus_cliff-on")).toHaveCount(0);
  await checkPage(page);
});

test("a needed resource warns while off, under its box and in the summary", async ({ page }) => {
  await page.goto("/generate");
  const summary = page.locator("#need-summary");
  await expect(summary).toBeHidden();
  const coal = page.locator("#mg-nauvis-coal-on");
  const warning = "Without coal on Nauvis the game cannot be finished unless a mod provides it.";
  await coal.uncheck();
  await expect(page.locator("#mg-nauvis-coal-on-need")).toHaveText(warning);
  await expect(summary).toBeVisible();
  await expect(summary.getByText(warning)).toBeVisible();
  await expect(page.locator("#announcer")).toHaveText(warning);
  await checkPage(page);
  await coal.check();
  await expect(page.locator("#mg-nauvis-coal-on-need")).toHaveText("");
  await expect(summary).toBeHidden();
  await expect(page.locator("#announcer")).toHaveText("Warning cleared: Coal is on.");
});

test("the server shows the same warning after a round trip", async ({ page }) => {
  await page.goto("/generate");
  await page.locator("#mg-nauvis-stone-on").uncheck();
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toBeVisible();
  await expect(page.locator("#mg-nauvis-stone-on-need")).toHaveText(/^Without stone on Nauvis/);
  await expect(page.locator("#need-summary")).toBeVisible();
});

test("the rows work by keyboard alone at 320 px", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto("/generate");
  const summary = row(page, "resources", "gleba").locator("summary");
  await tabTo(page, summary);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(1000);
  expect(await openRows(page, "resources")).toEqual(["gleba"]);
  await expect(summary).toBeFocused();
  await checkPage(page);
});
