import { test, expect, createAdmin, checkPage } from "./foreman.js";

// DESIGN 6, "Sub-navigation": Foreman's settings have eight tabs. On a phone
// they are an even grid of two columns, never a sideways-scrolling row; from
// 40 rem the row wraps rather than cutting a tab off. German has the longest
// words, so it is the stress case.

const tabs = 8;

for (const lang of ["en", "de"]) {
  for (const width of [320, 390, 639, 640, 800, 1280]) {
    test(`${lang}: the settings tabs all show at ${width} px`, async ({ page, foreman }) => {
      await createAdmin(page);
      await page.context().addCookies([{ name: "foreman_lang", value: lang, url: foreman.url }]);
      await page.setViewportSize({ width, height: 800 });
      await page.goto("/settings/general");
      await expect(page.locator("html")).toHaveAttribute("lang", lang);

      const nav = page.locator("nav.subnav--settings");
      const links = nav.locator("li a");
      await expect(links).toHaveCount(tabs);

      const row = await nav.locator("ul").evaluate((ul) => ({ scroll: ul.scrollWidth, client: ul.clientWidth }));
      expect(row.scroll, "the tab row scrolls sideways").toBeLessThanOrEqual(row.client);

      const boxes = [];
      for (let i = 0; i < tabs; i++) {
        const box = await links.nth(i).boundingBox();
        expect(box, `tab ${i + 1} is drawn`).not.toBeNull();
        // No tab is cut off by the viewport.
        expect(box!.x, `tab ${i + 1} starts inside the page`).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width, `tab ${i + 1} ends inside the page`).toBeLessThanOrEqual(width + 0.5);
        boxes.push(box!);
      }
      if (width < 640) {
        // Two even columns of 44 px cells.
        const lefts = new Set(boxes.map((b) => Math.round(b.x)));
        expect(lefts.size, "columns").toBe(2);
        for (const b of boxes) expect(b.height).toBeGreaterThanOrEqual(44);
        const widths = new Set(boxes.map((b) => Math.round(b.width)));
        expect(widths.size, "cells are one width").toBe(1);
      }
      await checkPage(page);
    });
  }
}
