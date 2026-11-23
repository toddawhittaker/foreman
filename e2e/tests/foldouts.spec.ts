import { test, expect, checkPage, createAdmin, seedWorld, image21, pickLanguage } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { openNewMap } from "./newmap.js";

// DESIGN 7.8: every foldout has the muted triangle at its start, a line
// only between stacked foldouts, and 44 px rows on touch.
test.use({ fakeGameOn: true });

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

const lone = (page: Page) => page.locator(".version-fields > details.disclosure");
const planet = (page: Page, name: string) => page.locator(`#mg-resources-${name}`);
const topLine = (d: Locator) => d.evaluate((e) => getComputedStyle(e).borderTopWidth);
// A colour token's value, read inside the element, so a pane drawn in the
// other theme (the component page) is measured in its own theme.
const token = (inside: Locator, name: string) =>
  inside.evaluate((el, name) => {
    const probe = document.createElement("span");
    probe.style.color = `var(${name})`;
    el.append(probe);
    const c = getComputedStyle(probe).color;
    probe.remove();
    return c;
  }, name);
const muted = (inside: Locator) => token(inside, "--color-text-muted");
const marker = (d: Locator) =>
  d.locator("> summary").evaluate((s) => ({ color: getComputedStyle(s, "::marker").color, type: getComputedStyle(s).listStyleType }));

// The projects run each test in both themes.
{
  test("a lone foldout has no line above, stacked planets do, and the marker is muted and turns", { tag: "@looks" }, async ({ page }) => {
    await page.goto("/generate");
    expect(await topLine(lone(page))).toBe("0px");

    const before = await marker(lone(page));
    expect(before.color).toBe(await muted(lone(page)));
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

  // The planet rows are the map settings of step 4 (SPEC 5.6).
  test("the first planet row has no line above and the next one does", async ({ page }) => {
    await openNewMap(page);
    // Nauvis is the first planet row, so no line; Gleba follows another row.
    expect(await topLine(planet(page, "nauvis"))).toBe("0px");
    expect(await topLine(planet(page, "gleba"))).toBe("1px");
  });
}

// DESIGN 7.8: Emergency actions is as quiet as any foldout, a step smaller;
// only its opened area is red.
test("Emergency actions is a quiet foldout whose opened area is red", { tag: "@looks" }, async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto("/design");
  const e = page.locator("details.emergency").first();
  const m = await marker(e);
  expect(m.color).toBe(await muted(e));
  const label = await e.locator("> summary").evaluate((s) => ({ color: getComputedStyle(s).color, size: getComputedStyle(s).fontSize }));
  expect(label.color).toBe(await muted(e));
  expect(label.size).toBe("14px");
  await e.locator("> summary").click();
  await expect(e).toHaveJSProperty("open", true);
  expect(await e.locator("> .danger-row").evaluate((r) => getComputedStyle(r).borderTopColor)).toBe(await token(e, "--color-danger"));
});

for (const lang of ["de", "ru"]) {
  test(`at 320 px and 200% text in ${lang}, Generate's foldouts fit`, { tag: "@looks" }, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await pickLanguage(page, lang);
    await page.goto("/generate");
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    const b = (await lone(page).locator("> summary").boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(0);
    expect(b.x + b.width).toBeLessThanOrEqual(320);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
    await checkPage(page);
  });

  test(`at 320 px and 200% text in ${lang}, the map settings' foldouts fit`, { tag: "@looks" }, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await pickLanguage(page, lang);
    await openNewMap(page);
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    const b = (await planet(page, "nauvis").locator("> summary").boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(0);
    expect(b.x + b.width).toBeLessThanOrEqual(320);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
    await checkPage(page);
  });
}

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, isMobile: true });

  test("foldout rows are 44 px tall", { tag: "@looks" }, async ({ page }) => {
    await page.goto("/generate");
    expect(await page.evaluate(() => matchMedia("(pointer: coarse)").matches)).toBe(true);
    expect((await lone(page).locator("> summary").boundingBox())!.height).toBeGreaterThanOrEqual(44);
  });

  test("planet foldout rows are 44 px tall", { tag: "@looks" }, async ({ page }) => {
    await openNewMap(page);
    expect(await page.evaluate(() => matchMedia("(pointer: coarse)").matches)).toBe(true);
    for (const d of [planet(page, "nauvis"), planet(page, "gleba")]) {
      expect((await d.locator("> summary").boundingBox())!.height).toBeGreaterThanOrEqual(44);
    }
  });
});
