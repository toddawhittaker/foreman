import { test, expect, createAdmin, seedWorld, image21 } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 17.2 and 22: at 320 px in a language with long words, Generate's
// action buttons hold their labels and the main tabs never break a word
// (DESIGN 6, 8.6). The page must not scroll sideways.

async function layoutProblems(page: Page) {
  return page.evaluate(() => {
    const problems: string[] = [];
    const root = document.documentElement;
    if (root.scrollWidth > root.clientWidth) problems.push(`page scrolls sideways to ${root.scrollWidth}px`);
    for (const b of document.querySelectorAll<HTMLElement>(".gen-actions > *")) {
      if (b.scrollWidth > b.clientWidth) problems.push(`"${b.textContent?.trim()}" spills out of its button`);
    }
    for (const a of document.querySelectorAll<HTMLElement>(".tabs a")) {
      const lineHeight = parseFloat(getComputedStyle(a).lineHeight);
      const range = document.createRange();
      range.selectNodeContents(a.firstChild!);
      if (range.getBoundingClientRect().height > lineHeight * 1.5) problems.push(`tab "${a.textContent?.trim()}" takes more than one line`);
    }
    return problems;
  });
}

test.describe("long words at 320 px", () => {
  test.use({ fakeGameOn: true });

  for (const lang of ["de", "fr", "es", "pl", "ru"]) {
    test(`${lang}: Generate's buttons and the main tabs hold their words`, async ({ page, foreman, fakeGame }) => {
      await page.setViewportSize({ width: 320, height: 640 });
      await createAdmin(page);
      seedWorld(foreman);
      await fakeGame!.call("/image", { ref: image21, on: true });
      await page.context().addCookies([{ name: "foreman_lang", value: lang, url: foreman.url }]);
      for (const size of [100, 200]) {
        await page.goto("/generate");
        await expect(page.locator("html")).toHaveAttribute("lang", lang);
        await page.evaluate((p) => (document.documentElement.style.fontSize = `${p}%`), size);
        // Under load the web fonts and the scripts' own layout can land after
        // the first read, so wait for the fonts and then for a settled page.
        // A real overflow stays and still fails once the poll times out.
        await page.evaluate(() => document.fonts.ready);
        await expect.poll(() => layoutProblems(page), { message: `${lang} at ${size}%` }).toEqual([]);
      }
    });
  }
});
