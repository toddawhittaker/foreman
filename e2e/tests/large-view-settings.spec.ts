import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Page, Request } from "@playwright/test";

// SPEC 5.6, 17; DESIGN 8.8: on a screen wide enough for both, the
// large view holds the viewed planet's Resources and Terrain settings to the
// left of the map. They are the form's own fields, moved there while the view
// is open, so a change redraws the map after the page's own pause and is
// still there once the view closes. Focus decides who owns the arrow keys.
// Narrower, the view stays map only.
test.use({ fakeGameOn: true });

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const dialog = (page: Page) => page.locator("#large-view");
const frame = (page: Page) => page.locator("#large-frame");
const panel = (page: Page) => page.locator("#large-settings");
const status = (page: Page) => page.locator("#large-status");
const field = (r: Request, name: string) => new URLSearchParams(r.postData() ?? "").get(name) ?? undefined;
const isAuto = (r: Request) => r.method() === "POST" && r.url().endsWith("/generate/preview") && field(r, "auto") === "1";
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png` });
// The view needs 77rem; the desktop projects are 1280 wide, the phone ones 320.
const isWide = (page: Page) => (page.viewportSize()?.width ?? 0) >= 1240;

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

async function openLarge(page: Page) {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await expect(dialog(page)).toBeVisible();
  await expect.poll(() => page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
}

// The id of a planet's first setting of steps in a tab, read from its row on the page.
const firstSelect = (page: Page, tab: string, planet: string) =>
  page.locator(`#mg-${tab}-${planet} .step select`).first().getAttribute("id");

test.describe("on a wide screen", () => {
  test.beforeEach(({ page }) => test.skip(!isWide(page), "the settings show beside the map only where both fit"));

  test("the planet's Resources and Terrain settings sit left of the map, and are the form's own fields", async ({ page }) => {
    await page.goto("/generate");
    const iron = (await firstSelect(page, "resources", "nauvis"))!;
    await openLarge(page);
    await expect(panel(page).locator(`#${iron}`)).toHaveCount(1);
    await expect(panel(page)).toBeVisible();
    await expect(panel(page).getByRole("heading", { level: 3, name: "Resources" })).toBeVisible();
    await expect(panel(page).getByRole("heading", { level: 3, name: "Terrain" })).toBeVisible();
    // One copy: the field is in the view, inside the form, and no longer in its row.
    await expect(page.locator(`[id="${iron}"]`)).toHaveCount(1);
    expect(await page.locator(`#${iron}`).evaluate((s) => Boolean(s.closest("#generate-form #large-view")))).toBe(true);
    await expect(page.locator(`#mg-resources-nauvis #${iron}`)).toHaveCount(0);
    // Left of the map, and the map no narrower than the settings.
    const p = (await panel(page).boundingBox())!;
    const f = (await frame(page).boundingBox())!;
    expect(p.x + p.width).toBeLessThanOrEqual(f.x);
    expect(f.width).toBeGreaterThanOrEqual(p.width);
    // Wide enough for sliders, as the page has them.
    await expect(page.locator(`#${iron}-range`)).toBeVisible();
    await expect(page.locator(`#${iron}`)).toBeHidden();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
    await shot(page, "large-view-settings");
    await checkPage(page);
    // Closing puts the field back in its row.
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await expect(page.locator(`#mg-resources-nauvis #${iron}`)).toHaveCount(1);
    await expect(page.locator(`[id="${iron}"]`)).toHaveCount(1);
  });

  test("a change there redraws the map after the pause, says so, and stays once the view closes", async ({ page }) => {
    await openLarge(page);
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    const select = page.locator("#large-resources .step select").first();
    const id = (await select.getAttribute("id"))!;
    const name = (await select.getAttribute("name"))!;
    const before = await select.inputValue();
    const oldSrc = await page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).src);
    const range = page.locator(`#${id}-range`);
    // The redraw is held until its state has been seen.
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/preview", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await range.focus();
    // The arrows belong to the slider: it moves, and the view stays on Nauvis.
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await expect(range).toBeFocused();
    const after = await select.inputValue();
    expect(after).not.toBe(before);
    // Zoom keys belong to the field too.
    await page.keyboard.press("+");
    await expect(status(page)).not.toHaveText("Zoom 115%");
    // The slow redraw shows as on the page: the picture fades and the line says so; then the map is ready.
    await expect(frame(page)).toHaveAttribute("aria-busy", "true");
    await expect(status(page)).toHaveText("Drawing the map preview for the current settings…");
    release();
    await expect(status(page)).toHaveText("Map preview ready.");
    await expect(frame(page)).toHaveAttribute("aria-busy", "false");
    // One request for the two steps, after the pause, with the new value.
    await page.waitForTimeout(1200);
    expect(sent.length).toBe(1);
    expect(field(sent[0], name)).toBe(after);
    expect(field(sent[0], "planet")).toBe("nauvis");
    // The view shows the new picture.
    const shown = await page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).src);
    expect(shown).not.toBe(oldSrc);
    expect(shown).toBe(await section(page).locator("a.picture img").evaluate((i) => (i as HTMLImageElement).src));
    await expect(range).toBeFocused();

    // Escape closes the view from the slider; the change is the page's.
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await expect(section(page).getByRole("link", { name: /Map preview of Nauvis/ })).toBeFocused();
    await expect(page.locator(`#mg-resources-nauvis #${id}`)).toHaveValue(after);
    await expect(page.locator("#preset-modified")).toHaveText("[modified]");
    // Reset and Undo reach it as any other setting.
    await page.getByRole("button", { name: "Reset to defaults" }).click();
    await expect(page.locator(`#${id}`)).toHaveValue(before);
    await page.getByRole("button", { name: "Undo reset" }).click();
    await expect(page.locator(`#${id}`)).toHaveValue(after);
  });

  test("a change in the view counts as unsaved, so leaving asks first", async ({ page }) => {
    const asked: string[] = [];
    page.on("dialog", async (d) => {
      asked.push(d.type());
      await d.dismiss();
    });
    await openLarge(page);
    await page.locator("#large-terrain .step__range").first().focus();
    await page.keyboard.press("ArrowLeft");
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await page.getByRole("link", { name: "Cancel" }).click();
    await expect.poll(() => asked).toEqual(["beforeunload"]);
    await expect(page).toHaveURL("/generate");
  });

  test("another planet viewed brings its own settings", async ({ page }) => {
    await page.goto("/generate");
    const nauvis = (await firstSelect(page, "resources", "nauvis"))!;
    const vulcanus = (await firstSelect(page, "resources", "vulcanus"))!;
    const gleba = (await firstSelect(page, "terrain", "gleba"))!;
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(panel(page).locator(`#${nauvis}`)).toHaveCount(1);
    // The arrows on the map still step between planets.
    await expect(frame(page)).toBeFocused();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await expect(panel(page).locator(`#${vulcanus}`)).toHaveCount(1);
    await expect(panel(page).locator(`#${nauvis}`)).toHaveCount(0);
    await expect(page.locator(`#mg-resources-nauvis #${nauvis}`)).toHaveCount(1);
    await dialog(page).getByRole("button", { name: "Next planet: Gleba" }).click();
    await expect(page.getByRole("dialog", { name: "Gleba" })).toBeVisible();
    await expect(panel(page).locator(`#${gleba}`)).toHaveCount(1);
    await expect(page.locator("#open-planet")).toHaveValue("gleba");
    await dialog(page).getByRole("button", { name: "Close" }).click();
    await expect(page.locator(`#mg-terrain-gleba #${gleba}`)).toHaveCount(1);
    await expect(page.locator(`#mg-resources-vulcanus #${vulcanus}`)).toHaveCount(1);
  });

  // SPEC 17.10: looking at a planet in the large view, change its setting and see the new map.
  test("changing the setting of the planet in the large view takes one click", async ({ page }) => {
    await openLarge(page);
    const clicks = await countClicks(page);
    await clicks.reset();
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    const range = page.locator("#large-resources .step__range").first();
    const box = (await range.boundingBox())!;
    await range.click({ position: { x: box.width * 0.9, y: box.height / 2 } });
    await expect(status(page)).toHaveText("Map preview ready.");
    expect(sent.length).toBe(1);
    await expectWithin(clicks, 1, "Change the setting of the planet in the large view and see it");
  });
});

test("where the settings and the map do not both fit, the view is map only", async ({ page }) => {
  if (isWide(page)) await page.setViewportSize({ width: 1100, height: 760 });
  await openLarge(page);
  await expect(panel(page)).toBeHidden();
  const d = (await dialog(page).boundingBox())!;
  const f = (await frame(page).boundingBox())!;
  // The frame takes the dialog's width less its padding.
  expect(f.width).toBeGreaterThan(d.width - 40);
  await shot(page, "large-view-narrow");
  await checkPage(page);
  // The fields are back in their rows once it closes.
  await page.keyboard.press("Escape");
  await expect(page.locator("#mg-resources-nauvis .step select").first()).toBeAttached();
  await expect(page.locator("#large-settings select")).toHaveCount(0);
});
