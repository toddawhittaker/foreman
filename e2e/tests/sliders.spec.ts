import { test, expect, checkPage, createAdmin, seedWorld, image21, pickLanguage } from "./foreman.js";
import type { Page } from "@playwright/test";

// DESIGN 8.7, SPEC 5.6 and 17: on a wide panel each setting of steps is a
// slider. Its row has a fixed shape, so the slider never moves while it is
// dragged, whatever the length of the step's words in any language; and its
// knob reaches both ends of the track at the first and last step.
test.use({ fakeGameOn: true });

test.beforeEach(async ({ page, foreman, fakeGame }, info) => {
  test.skip(info.project.name.startsWith("phone") && !info.title.includes("[modified]"), "phones show selects, not sliders (DESIGN 8.7)");
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

const tab = (page: Page, name: RegExp) => page.locator("label.tab", { hasText: name });
const tabNames: Record<string, RegExp[]> = {
  en: [/^\s*Resources\s*$/, /^\s*Terrain\s*$/, /^\s*Enemy\s*$/],
  de: [/^\s*Ressourcen\s*$/, /^\s*Gelände\s*$/, /^\s*Feinde\s*$/],
  ru: [/^\s*Ресурсы\s*$/, /^\s*Ландшафт\s*$/, /^\s*Враг\s*$/],
};

// Moves every shown slider through all its steps as a drag does (an input
// event per step) and returns the sliders whose box moved in their tab, and
// how. Boxes are measured from the tab's top: the preset's "[modified]" above
// the tabs is not the slider row's doing.
async function movedWhileStepping(page: Page) {
  return page.evaluate(() => {
    const moved: string[] = [];
    let top = 0;
    const where = (el: Element) => {
      const r = el.getBoundingClientRect();
      return `${Math.round(r.x * 10)},${Math.round((r.y - top) * 10)},${Math.round(r.width * 10)},${Math.round(r.height * 10)}`;
    };
    for (const range of document.querySelectorAll<HTMLInputElement>("#map-settings input.step__range")) {
      if (range.getClientRects().length === 0) continue;
      const panel = range.closest(".tab-panel")!;
      top = panel.getBoundingClientRect().y;
      const start = range.value;
      const label = document.getElementById(range.dataset.for + "-label")!;
      const before = where(range) + " " + where(label);
      for (let i = Number(range.min); i <= Number(range.max); i++) {
        range.value = String(i);
        range.dispatchEvent(new Event("input", { bubbles: true }));
        top = panel.getBoundingClientRect().y;
        const now = where(range) + " " + where(label);
        if (now !== before) moved.push(`${range.id} at step ${i}: ${before} became ${now}`);
      }
      range.value = start;
      range.dispatchEvent(new Event("input", { bubbles: true }));
    }
    return moved;
  });
}

// German and Russian have the longest "(default)". At 700 px the map
// settings take the window's width, where a short step's words once let the
// slider fit beside its name.
for (const lang of ["en", "de", "ru"]) {
  for (const width of [700, 1280, 1920]) {
    test(`no slider moves while it is stepped, in ${lang} at ${width} px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto("/generate");
      if (lang !== "en") await pickLanguage(page, lang);
      await page.goto("/generate");
      for (const name of tabNames[lang]) {
        await tab(page, name).click();
        const shown = page.locator("#map-settings input.step__range:visible");
        expect(await shown.count()).toBeGreaterThan(1);
        expect(await movedWhileStepping(page)).toEqual([]);
      }
    });
  }
}

// The rows size themselves from the map settings' own width (DESIGN 8.7),
// so every width of that box keeps them still, not only the window widths above.
for (const lang of ["de", "ru"]) {
  test(`no slider moves while it is stepped, in ${lang} at any width of the map settings`, async ({ page }) => {
    test.setTimeout(90_000);
    await page.setViewportSize({ width: 1920, height: 900 });
    await page.goto("/generate");
    await pickLanguage(page, lang);
    await page.goto("/generate");
    for (let width = 560; width <= 1400; width += 40) {
      await page.locator("#map-section").evaluate((el, w) => { el.style.inlineSize = `${w}px`; }, width);
      for (const name of tabNames[lang]) {
        await tab(page, name).click();
        expect(await movedWhileStepping(page), `${width} px`).toEqual([]);
      }
    }
  });
}

// DESIGN 8.6: "[modified]" and the reset button appear by the preset as soon
// as a setting leaves it. Their line is kept for them, so nothing under the
// preset, the setting being moved included, shifts when they come or go.
for (const lang of ["en", "de"]) {
  test(`"[modified]" coming and going moves nothing below the preset, in ${lang}`, async ({ page }) => {
    await page.goto("/generate");
    if (lang !== "en") await pickLanguage(page, lang);
    for (const width of [320, 480, 700, 1280]) {
      await page.setViewportSize({ width, height: 800 });
      await page.goto("/generate");
      // The setting as shown: a slider on a wide panel, else its select.
      const where = () => page.evaluate(() => {
        const range = document.getElementById("mg-nauvis-iron-ore-frequency-range")!;
        const shown = range.getClientRects().length > 0 ? range : document.getElementById("mg-nauvis-iron-ore-frequency")!;
        const top = (el: Element) => Math.round((el.getBoundingClientRect().y + window.scrollY) * 10);
        return [top(shown), top(document.querySelector(".tablist")!)];
      });
      const step = (to: string) => page.locator("#mg-nauvis-iron-ore-frequency").evaluate((s: HTMLSelectElement, v) => {
        s.value = v;
        s.dispatchEvent(new Event("change", { bubbles: true }));
      }, to);
      const before = await where();
      await step("50");
      await expect(page.locator("#preset-reset")).toBeVisible();
      await expect(page.locator("#preset-modified")).not.toBeEmpty();
      expect(await where(), `${width} px, modified`).toEqual(before);
      if (width === 320) {
        await page.locator("#generate-preset").scrollIntoViewIfNeeded();
        await page.screenshot({ path: `../screenshots/preset-modified-${lang}-${test.info().project.name}.png` });
      }
      await step("100");
      await expect(page.locator("#preset-reset")).toBeHidden();
      expect(await where(), `${width} px, back to the preset`).toEqual(before);
    }
  });
}

// The slider where the row once jumped (Gleba cliffs, Frequency), stepped
// with the keyboard from its default to each end and back.
test("a slider stepped with the keyboard keeps its place", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/generate");
  await pickLanguage(page, "de");
  await page.goto("/generate");
  await tab(page, tabNames.de[1]).click();
  await page.locator("#mg-terrain-gleba > summary").click();
  const range = page.locator("#mg-gleba-gleba_cliff-frequency-range");
  await expect(range).toBeVisible();
  await range.focus();
  const before = await range.boundingBox();
  // From the default, to each end and back past the default.
  for (const key of ["End", "Home", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowRight", "ArrowLeft"]) {
    await page.keyboard.press(key);
    expect(await range.boundingBox(), key).toEqual(before);
  }
  await expect(page.locator("#mg-gleba-gleba_cliff-frequency-value")).not.toContainText("Standard");
  await page.screenshot({ path: `../screenshots/sliders-de-${test.info().project.name}.png` });
});

// A resource's on/off box is a control: its name keeps full strength while
// off, and only the settings it turns off grey.
test("an off box keeps its name at full strength and greys its settings", async ({ page }) => {
  await page.goto("/generate");
  const colour = (sel: string) => page.locator(sel).evaluate((el) => getComputedStyle(el).color);
  const text = await page.locator("#map-settings").evaluate((el) => getComputedStyle(el).color);
  const name = "#mg-nauvis-uranium-ore > legend label";
  const setting = "#mg-nauvis-uranium-ore-frequency-label";
  expect(await colour(name)).toBe(text);
  expect(await colour(setting)).toBe(text);
  await page.locator("#mg-nauvis-uranium-ore-on").uncheck();
  expect(await colour(name)).toBe(text);
  expect(await colour(setting)).not.toBe(text);
  await page.locator("#mg-nauvis-uranium-ore-on").check();
  expect(await colour(setting)).toBe(text);
});

// The colour of a pixel of a picture, read in a blank page (the app's own pages allow no data: pictures).
async function pixels(page: Page, png: Buffer, points: [number, number][]) {
  const blank = await page.context().newPage();
  const out = await blank.evaluate(async ({ b64, points }) => {
    const img = new Image();
    img.src = `data:image/png;base64,${b64}`;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.naturalWidth;
    c.height = img.naturalHeight;
    const g = c.getContext("2d")!;
    g.drawImage(img, 0, 0);
    return points.map(([x, y]) => [...g.getImageData(Math.round(x * c.width), Math.round(y * c.height), 1, 1).data.slice(0, 3)]);
  }, { b64: png.toString("base64"), points });
  await blank.close();
  return out;
}

const apart = (a: number[], b: number[]) => Math.max(...a.map((v, i) => Math.abs(v - b[i])));

// One slider of each kind: frequency, size, richness, scale, coverage,
// continuity, bias, and the starting area.
const kinds: [number, string][] = [
  [0, "mg-nauvis-iron-ore-frequency"], [0, "mg-nauvis-iron-ore-size"], [0, "mg-nauvis-iron-ore-richness"],
  [1, "mg-nauvis-trees-frequency"], [1, "mg-nauvis-trees-size"], [1, "mg-nauvis-nauvis_cliff-size"],
  [1, "mg-moisture-bias"], [2, "mg-start"],
];

async function expectEnds(page: Page) {
  await page.goto("/generate");
  for (const [t, id] of kinds) {
    await tab(page, tabNames.en[t]).click();
    const range = page.locator(`#${id}-range`);
    await expect(range).toBeVisible();
    // Nothing pads or edges the track, so its ends are the range's own box.
    const style = await range.evaluate((el) => {
      const s = getComputedStyle(el);
      return [s.paddingLeft, s.paddingRight, s.borderLeftWidth, s.borderRightWidth].join(" ");
    });
    expect(style, id).toBe("0px 0px 0px 0px");
    const box = (await range.boundingBox())!;
    const column = (await range.locator("xpath=..").boundingBox())!;
    expect(Math.abs(box.width - column.width), `${id} fills its column`).toBeLessThanOrEqual(1);

    const words = await page.locator(`#${id} option`).allTextContents();
    const value = page.locator(`#${id}-value`);
    await range.focus();
    await page.keyboard.press("Home");
    // The step's words still say the step's value.
    await expect(value).toHaveText(words[0]);
    const first = await range.screenshot({ animations: "disabled" });
    await page.keyboard.press("End");
    await expect(value).toHaveText(words[words.length - 1]);
    const end = await range.screenshot({ animations: "disabled" });
    // Sampled at mid height: 6 CSS px in from each end (inside a knob that touches the end, past its rim),
    // the middle, and the top edge (the page behind).
    const px = 6 / box.width;
    const [firstLeft, firstMid, back] = await pixels(page, first, [[px, 0.5], [0.5, 0.5], [0.5, 0]]);
    const [endRight, endMid] = await pixels(page, end, [[1 - px, 0.5], [0.5, 0.5]]);
    // The knob covers the track's very ends; a padded gap would show the page there.
    expect(apart(firstLeft, back), `${id}: knob at the left end`).toBeGreaterThan(60);
    expect(apart(endRight, back), `${id}: knob at the right end`).toBeGreaterThan(60);
    // The fill is empty at the first step and full at the last.
    expect(apart(firstMid, endMid), `${id}: fill differs between the ends`).toBeGreaterThan(60);
    await page.keyboard.press("Home");
  }
  await checkPage(page);
}

test("each kind of slider reaches the track's ends at its first and last step", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await expectEnds(page);
});

test.describe("at 200% zoom", () => {
  // Half the CSS pixels across, each drawn as two device pixels.
  test.use({ viewport: { width: 640, height: 450 }, deviceScaleFactor: 2 });

  test("each kind of slider reaches the track's ends at its first and last step", async ({ page }) => {
    await expectEnds(page);
  });
});
