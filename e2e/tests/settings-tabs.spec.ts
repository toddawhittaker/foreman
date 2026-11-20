import { test, expect, createAdmin, checkPage } from "./foreman.js";

// DESIGN 6, "Sub-navigation": Foreman's settings have eight tabs. On a phone
// (below 40rem) they are one row that scrolls sideways inside itself, each
// tab whole, with the current one scrolled into view; from 40rem the row
// wraps rather than cutting a tab off. German has the longest words, so it
// is the stress case.

const tabs = 8;

for (const lang of ["en", "de"]) {
  for (const width of [320, 390, 639, 640, 800, 1280]) {
    test(`${lang}: the settings tabs all show at ${width} px`, async ({ page, foreman }) => {
      await createAdmin(page);
      await page.context().addCookies([{ name: "foreman_lang", value: lang, url: foreman.url }]);
      await page.setViewportSize({ width, height: 800 });
      await page.goto("/settings/general");
      await expect(page.locator("html")).toHaveAttribute("lang", lang);

      const nav = page.locator("nav.subnav--settings:visible");
      const links = nav.locator("li a");
      await expect(links).toHaveCount(tabs);

      const phoneRow = width < 640;
      if (!phoneRow) {
        const row = await nav.locator("ul").evaluate((ul) => ({ scroll: ul.scrollWidth, client: ul.clientWidth }));
        expect(row.scroll, "the tab row scrolls sideways").toBeLessThanOrEqual(row.client);
      }

      for (let i = 0; i < tabs; i++) {
        const link = links.nth(i);
        const box = await link.boundingBox();
        expect(box, `tab ${i + 1} is drawn`).not.toBeNull();
        if (phoneRow) {
          // Each tab is whole: one line of words that fits its 44 px cell.
          expect(box!.height, `tab ${i + 1} is a 44 px target`).toBeGreaterThanOrEqual(44);
          const whole = await link.evaluate((a) => {
            const r = document.createRange();
            r.selectNodeContents(a);
            const lines = new Set([...r.getClientRects()].filter((x) => x.width > 0).map((x) => Math.round(x.top)));
            return lines.size === 1 && a.scrollWidth <= a.clientWidth + 1;
          });
          expect(whole, `tab ${i + 1} is whole`).toBe(true);
        } else {
          // No tab is cut off by the viewport.
          expect(box!.x, `tab ${i + 1} starts inside the page`).toBeGreaterThanOrEqual(0);
          expect(box!.x + box!.width, `tab ${i + 1} ends inside the page`).toBeLessThanOrEqual(width + 0.5);
        }
      }
      // The row scrolls inside itself, never the page.
      expect(await page.evaluate(() => document.documentElement.scrollWidth), "the page scrolls sideways").toBeLessThanOrEqual(width);
      if (phoneRow) {
        // The last tab's page opens with its tab scrolled into view.
        await page.goto("/settings/factorio-account");
        const current = nav.locator('a[aria-current="page"]');
        await expect(current).toHaveCount(1);
        await expect(current).toBeInViewport({ ratio: 1 });
      }
      await checkPage(page);
    });
  }
}
