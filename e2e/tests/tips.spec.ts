import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import type { Page } from "@playwright/test";

// DESIGN 7.7, 8.7; SPEC 5.6 and 17: every setting of the Enemy and Advanced
// tabs has an (i) toggletip. It opens on a click or tap, on keyboard focus
// and on hover, closes on Escape or a tap elsewhere, stays on screen, and its
// text describes the field. Without script the text shows under the field.
test.use({ fakeGameOn: true });

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

const tab = (page: Page, name: string) => page.locator("label.tab", { hasText: new RegExp(`^\\s*${name}\\s*$`) });
const timeTip = (page: Page) => page.getByRole("button", { name: "About Time factor" });
const timeText = (page: Page) => page.locator("#ms-enemy-evolution-time-factor-tip");

async function onEnemyTab(page: Page) {
  await page.goto("/generate");
  await tab(page, "Enemy").click();
  await expect(timeTip(page)).toBeVisible();
}

// The tip's box lies inside the window, with room to spare.
async function expectOnScreen(page: Page, text: ReturnType<Page["locator"]>) {
  const box = (await text.boundingBox())!;
  const width = await page.evaluate(() => document.documentElement.clientWidth);
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(width);
}

test("every Enemy and Advanced setting has a tip, and the evolution factors have no power of ten", async ({ page }) => {
  await onEnemyTab(page);
  const enemy = page.locator("#panel-enemy");
  // The enemy bases' two settings, starting area, No enemies, Peaceful, and the map-settings fields.
  for (const name of ["Frequency", "Size", "Starting area size", "No enemies", "Peaceful mode", "Enemy expansion", "Evolution",
    "Minimum expansion distance", "Maximum expansion distance", "Evolution group size factor", "Minimum cooldown",
    "Maximum cooldown", "Time factor", "Destroy factor", "Pollution factor"]) {
    await expect(enemy.getByRole("button", { name: `About ${name}`, exact: true }).first()).toBeVisible();
  }
  await expect(page.locator("#generate-form")).not.toContainText(/10⁻|× 10\b/);
  await page.locator("#mg-start-label").scrollIntoViewIfNeeded();
  await page.screenshot({ path: `../screenshots/tips-enemy-${test.info().project.name}.png` });
  await tab(page, "Advanced").click();
  const advanced = page.locator("#panel-advanced");
  for (const name of ["Price multiplier", "Pollution", "Diffusion ratio", "Absorption modifier", "Attack cost modifier",
    "Minimum to damage trees", "Absorbed per damaged tree", "Spawning rate", "Spoiling rate"]) {
    await expect(advanced.getByRole("button", { name: `About ${name}`, exact: true })).toBeVisible();
  }
  // The field is described by its tip, and the button controls it.
  await expect(page.locator("#ms-enemy-evolution-time-factor")).toHaveAccessibleDescription(/Controls how fast evolution increases over time\. .*40 means 0\.000004/);
  await expect(page.locator("#mg-nauvis-enemy-base-frequency-range")).toHaveAccessibleDescription(/The number of enemy bases\./);
  await expect(page.locator('button.tip[aria-controls="ms-enemy-evolution-time-factor-tip"]')).toHaveCount(1);
});

test("a click opens a tip and a second click or Escape closes it", async ({ page }) => {
  await onEnemyTab(page);
  await expect(timeText(page)).toBeHidden();
  await timeTip(page).click();
  await expect(timeText(page)).toBeVisible();
  await expect(timeTip(page)).toHaveAttribute("aria-expanded", "true");
  await expectOnScreen(page, timeText(page));
  // The pointer leaves; a tip opened by a click stays.
  await page.mouse.move(0, 0);
  await page.waitForTimeout(300);
  await expect(timeText(page)).toBeVisible();
  await timeTip(page).click();
  await expect(timeText(page)).toBeHidden();
  await expect(timeTip(page)).toHaveAttribute("aria-expanded", "false");

  await timeTip(page).click();
  await page.keyboard.press("Escape");
  await expect(timeText(page)).toBeHidden();
  await checkPage(page);
});

test("hover shows a tip, and leaving the button hides it", async ({ page }) => {
  await onEnemyTab(page);
  await timeTip(page).hover();
  await expect(timeText(page)).toBeVisible();
  await page.mouse.move(0, 0);
  await expect(timeText(page)).toBeHidden();
});

test("keyboard focus shows a tip, Escape hides it, and leaving it hides it", async ({ page }) => {
  await onEnemyTab(page);
  await page.locator("#ms-enemy-evolution-enabled").focus();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "About Evolution", exact: true })).toBeFocused();
  await expect(page.locator("#ms-enemy-evolution-enabled-tip")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator("#ms-enemy-evolution-enabled-tip")).toBeHidden();
  await expect(page.getByRole("button", { name: "About Evolution", exact: true })).toBeFocused();
  await tabTo(page, timeTip(page));
  await expect(timeText(page)).toBeVisible();
  await expect(page.locator("#ms-enemy-evolution-enabled-tip")).toBeHidden();
  await page.keyboard.press("Tab");
  await expect(timeText(page)).toBeHidden();
});

// A tip opened by focus is shown, so Enter on it closes it, and Enter again opens it.
test("Enter on a tip that focus opened closes it, and Enter again opens it", async ({ page }) => {
  await onEnemyTab(page);
  await tabTo(page, timeTip(page));
  await expect(timeText(page)).toBeVisible();
  await expect(timeTip(page)).toHaveAttribute("aria-expanded", "true");
  await page.keyboard.press("Enter");
  await expect(timeText(page)).toBeHidden();
  await expect(timeTip(page)).toHaveAttribute("aria-expanded", "false");
  await page.keyboard.press("Enter");
  await expect(timeText(page)).toBeVisible();
  await expect(timeTip(page)).toHaveAttribute("aria-expanded", "true");
});

test("a tip in the Advanced tab opens and passes axe", async ({ page }) => {
  await onEnemyTab(page);
  await tab(page, "Advanced").click();
  const price = page.getByRole("button", { name: "About Price multiplier" });
  await price.click();
  const text = page.locator("#ms-difficulty-settings-technology-price-multiplier-tip");
  await expect(text).toBeVisible();
  await expectOnScreen(page, text);
  await page.screenshot({ path: `../screenshots/tips-advanced-${test.info().project.name}.png` });
  await checkPage(page);
});

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true });

  test("a tap opens a tip, it stays on screen, and a tap elsewhere closes it", async ({ page }) => {
    await onEnemyTab(page);
    // The last tip on a row sits nearest the window's right edge.
    const tips = page.locator("#panel-enemy button.tip");
    const last = tips.last();
    await last.scrollIntoViewIfNeeded();
    await last.tap();
    const text = page.locator(`#${await last.getAttribute("aria-controls")}`);
    await expect(text).toBeVisible();
    await expectOnScreen(page, text);
    // A tip button is a 44 px target on touch (SPEC 17.2).
    const box = (await last.boundingBox())!;
    expect(Math.min(box.width, box.height)).toBeGreaterThanOrEqual(44);
    await page.screenshot({ path: `../screenshots/tips-tap-${test.info().project.name}.png` });
    await page.locator("#panel-enemy .ms-group > legend").first().tap();
    await expect(text).toBeHidden();
  });

  // At 320 px an open tip lies over its own field; a tap on its text must not
  // leave it there.
  test("a tap on the tip's text closes it", async ({ page }) => {
    await onEnemyTab(page);
    await timeTip(page).tap();
    await expect(timeText(page)).toBeVisible();
    await timeText(page).tap();
    await expect(timeText(page)).toBeHidden();
    await expect(timeTip(page)).toHaveAttribute("aria-expanded", "false");
  });

  // Chrome moves focus on a real tap, which closes the tip by itself; a bare
  // touch press, with no focus change, must close it too, and a mouse press on
  // the text (to select it) must not.
  test("a touch press on the text closes it, a mouse press does not", async ({ page }) => {
    await onEnemyTab(page);
    const press = (pointerType: string) => timeText(page).evaluate((el, type) => {
      el.dispatchEvent(new PointerEvent("pointerdown", { pointerType: type, bubbles: true }));
    }, pointerType);
    await timeTip(page).tap();
    await expect(timeText(page)).toBeVisible();
    await press("mouse");
    await expect(timeText(page)).toBeVisible();
    await press("touch");
    await expect(timeText(page)).toBeHidden();
  });
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("each tip's text shows under its field and the button is hidden", async ({ page }) => {
    await page.goto("/generate");
    await expect(timeText(page)).toBeVisible();
    await expect(timeText(page)).toContainText("Controls how fast evolution increases over time.");
    await expect(page.locator("#mg-start-tip")).toBeVisible();
    await expect(page.locator("button.tip").first()).toBeHidden();
    await expect(page.locator("#ms-enemy-evolution-time-factor")).toHaveAccessibleDescription(/Controls how fast evolution/);
    await page.locator("#ms-enemy-evolution-time-factor").scrollIntoViewIfNeeded();
    await page.screenshot({ path: `../screenshots/tips-noscript-${test.info().project.name}.png` });
  });
});
