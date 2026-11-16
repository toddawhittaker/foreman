import { test, expect, checkPage, createAdmin, seedWorld, image21, pickLanguage } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";

// DESIGN 7.8 (#657): every foldout has the orange triangle at its start, a
// line only between stacked foldouts, and 44 px rows on touch.
test.use({ fakeGameOn: true });

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

const lone = (page: Page) => page.locator(".version-fields > details.disclosure");
const planet = (page: Page, name: string) => page.locator(`#mg-resources-${name}`);
const topLine = (d: Locator) => d.evaluate((e) => getComputedStyle(e).borderTopWidth);
const primary = (page: Page) =>
  page.evaluate(() => {
    const probe = document.createElement("span");
    probe.style.color = "var(--color-primary)";
    document.body.append(probe);
    const c = getComputedStyle(probe).color;
    probe.remove();
    return c;
  });
const marker = (d: Locator) =>
  d.locator("> summary").evaluate((s) => ({ color: getComputedStyle(s, "::marker").color, type: getComputedStyle(s).listStyleType }));

// The projects run each test in both themes.
{
  test("a lone foldout has no line above, stacked planets do, and the marker is orange and turns", async ({ page }) => {
    await page.goto("/generate");
    expect(await topLine(lone(page))).toBe("0px");
    // Nauvis is the first planet row, so no line; Gleba follows another row.
    expect(await topLine(planet(page, "nauvis"))).toBe("0px");
    expect(await topLine(planet(page, "gleba"))).toBe("1px");

    const before = await marker(lone(page));
    expect(before.color).toBe(await primary(page));
    expect(before.type).toBe("disclosure-closed");
    await lone(page).locator("> summary").click();
    await expect(lone(page)).toHaveJSProperty("open", true);
    expect((await marker(lone(page))).type).toBe("disclosure-open");

    // Keyboard: the summary takes focus with a visible ring.
    await lone(page).locator("> summary").focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    const outline = await lone(page).locator("> summary").evaluate((s) => getComputedStyle(s).outlineStyle);
    expect(outline).not.toBe("none");
    await checkPage(page);
  });
}

// Ruling 8: Emergency actions keeps its danger label; only the marker is orange.
test("Emergency actions keeps its danger label with an orange marker", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto("/design");
  const e = page.locator("details.emergency").first();
  const m = await marker(e);
  expect(m.color).toBe(await primary(page));
  const label = await e.locator("> summary").evaluate((s) => getComputedStyle(s).color);
  expect(label).not.toBe(m.color);
});

for (const lang of ["de", "ru"]) {
  test(`at 320 px and 200% text in ${lang}, Generate's foldouts fit`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await pickLanguage(page, lang);
    await page.goto("/generate");
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    for (const d of [lone(page), planet(page, "nauvis")]) {
      const b = (await d.locator("> summary").boundingBox())!;
      expect(b.x).toBeGreaterThanOrEqual(0);
      expect(b.x + b.width).toBeLessThanOrEqual(320);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
    await checkPage(page);
  });
}

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, isMobile: true });

  test("foldout rows are 44 px tall", async ({ page }) => {
    await page.goto("/generate");
    expect(await page.evaluate(() => matchMedia("(pointer: coarse)").matches)).toBe(true);
    for (const d of [lone(page), planet(page, "nauvis"), planet(page, "gleba")]) {
      expect((await d.locator("> summary").boundingBox())!.height).toBeGreaterThanOrEqual(44);
    }
  });
});
