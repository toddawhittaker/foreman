import { test, expect, createAdmin, seedWorld, image21 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { openNewMap, newMapPath } from "./newmap.js";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

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
      // The tab's own word, its first text; Server's state word sits on a line of its own under it.
      const walker = document.createTreeWalker(a, NodeFilter.SHOW_TEXT, { acceptNode: (n) => (n.textContent!.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP) });
      const range = document.createRange();
      range.selectNodeContents(walker.nextNode()!);
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
      // Step 3 holds the map settings' action bar (SPEC 5.6).
      const id = await openNewMap(page);
      for (const size of [100, 200]) {
        await page.goto(newMapPath(id));
        await expect(page.locator("#generate-form")).toBeVisible({ timeout: 60_000 });
        await expect(page.locator("html")).toHaveAttribute("lang", lang);
        await page.evaluate((p) => (document.documentElement.style.fontSize = `${p}%`), size);
        await page.evaluate(() => document.fonts.ready);
        await expect.poll(() => layoutProblems(page), { message: `${lang} step 3 at ${size}%` }).toEqual([]);
      }
    });
  }
});

// SPEC 12.9, DESIGN 8.15: the audit log's table stacks each row at 320 px, and a
// long user, token, world and detail wrap inside it in every long language, at
// normal and large text.
test.describe("the audit log at 320 px", () => {
  for (const lang of ["de", "fr", "es", "pl", "ru"]) {
    test(`${lang}: the table's rows hold their words`, async ({ page, foreman }) => {
      await page.setViewportSize({ width: 320, height: 640 });
      await createAdmin(page);
      seedWorld(foreman);
      const db = new DatabaseSync(join(foreman.dataDir, "foreman.db"));
      try {
        const now = Math.floor(Date.now() / 1000);
        const add = db.prepare("INSERT INTO audit_log (time, username, action, world_id, detail, token_name) VALUES (?, ?, ?, ?, ?, ?)");
        add.run(now - 20, "a-user-with-a-very-long-name-indeed", "start", "base", "x".repeat(150), "A-token-name-of-32-characters-xx");
        add.run(now - 10, "carol", "frobnicate-the-unrecognised-action-name", "gone-world-with-a-long-id", "/c game.print(" + "word ".repeat(30) + ")", "");
      } finally {
        db.close();
      }
      await page.context().addCookies([{ name: "foreman_lang", value: lang, url: foreman.url }]);
      for (const size of [100, 200]) {
        await page.goto("/settings/audit-log");
        await expect(page.locator("html")).toHaveAttribute("lang", lang);
        await page.evaluate((p) => (document.documentElement.style.fontSize = `${p}%`), size);
        await page.evaluate(() => document.fonts.ready);
        await expect.poll(() => layoutProblems(page), { message: `${lang} at ${size}%` }).toEqual([]);
        await expect(page.locator("#audit-list tbody tr").first()).toBeVisible();
      }
    });
  }
});
