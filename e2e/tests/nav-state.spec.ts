import { test, expect, checkPage, createAdmin, seedWorld, image21, mainNav, pickLanguage, live, stateBadge } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 17.4, DESIGN 7.5: the server's state beside Server in the main
// navigation is said in words on every page, not by the lamp's shape alone.
// On a wide screen the word sits under Server in its tab; on a phone it sits
// beside the logo, and the tab says it to screen readers.

test.use({ fakeGameOn: true });

const isPhone = (page: Page) => (page.viewportSize()?.width ?? 1000) <= 320;

// How wide the state's word is drawn inside el: 0 when it is hidden or clipped.
const drawnWord = (el: Element) => {
  const text = [...el.childNodes].find((n) => n.nodeType === Node.TEXT_NODE && n.textContent!.trim());
  if (!text || getComputedStyle(el).clipPath !== "none") return 0;
  const range = document.createRange();
  range.selectNodeContents(text);
  return range.getBoundingClientRect().width;
};

// The state word a sighted person reads in the header, wherever it is shown.
async function expectStateWord(page: Page, word: string) {
  const tab = mainNav(page).getByRole("link", { name: new RegExp(`^Server\\s+${word}$`) });
  await expect(tab).toBeVisible(live);
  const inTab = tab.locator("#nav-lamp");
  const beside = page.locator("header.site #site-state");
  // The copy beside the logo is for the eye only; the tab's name carries the word.
  await expect(beside).toHaveAttribute("aria-hidden", "true");
  await expect(beside).toContainText(word);
  const [shown, hidden] = isPhone(page) ? [beside, inTab] : [inTab, beside];
  await expect(shown).toBeVisible();
  expect(await shown.evaluate(drawnWord), "the word is drawn").toBeGreaterThan(20);
  expect(await hidden.evaluate(drawnWord), "the word is drawn once").toBe(0);
}

test("the header says the server's state in words, and follows it live", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/worlds");
  await expectStateWord(page, "Stopped");
  await checkPage(page);

  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
  // The Server page's live facts carry the header's state too, without a reload.
  await expectStateWord(page, "Running");

  await fakeGame!.call("/crash", { name: "foreman-base" });
  await expect(stateBadge(page)).toHaveText("Crashed", live);
  await expectStateWord(page, "Crashed");
  await page.goto("/snapshots");
  await expectStateWord(page, "Crashed");
  await checkPage(page);
});

// SPEC 17.2, 22.7: in German, at 320 px, the word fits beside the logo and
// the menu, and the four tabs keep one row.
test("in German the state word fits the header", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await pickLanguage(page, "de");
  await page.goto("/worlds");
  await expect(page.locator("header.site nav").getByRole("link", { name: /^Server\s+Gestoppt$/ })).toBeVisible();
  const width = page.viewportSize()!.width;
  const box = async (sel: string) => (await page.locator(sel).boundingBox())!;
  const logo = await box("header.site .product");
  const menu = await box("details.menu > summary");
  // Logo and menu share the first row, and the tabs keep one row under it.
  expect(Math.abs(menu.y + menu.height / 2 - (logo.y + logo.height / 2))).toBeLessThan(4);
  expect(menu.x + menu.width).toBeLessThanOrEqual(width);
  const tops = await page.locator("header.site nav a").evaluateAll((els) => new Set(els.map((a) => Math.round(a.getBoundingClientRect().top))).size);
  expect(tops).toBe(1);
  if (isPhone(page)) {
    const state = await box("header.site #site-state");
    expect(state.x).toBeGreaterThanOrEqual(logo.x + logo.width);
    expect(state.x + state.width).toBeLessThanOrEqual(menu.x);
  }
  await checkPage(page);
});

// DESIGN 7.5: a long state wraps inside Server's tab rather than widen it,
// so the header keeps its height when the state changes live, at any width,
// in the languages with the longest words.
for (const [lang, short, long] of [
  ["de", "Läuft", "Stürzt wiederholt ab"],
  ["ru", "Работает", "Циклические сбои"],
  ["ru", "Работает", "Останавливается"],
] as const) {
  test(`in ${lang} the header keeps its height from "${short}" to "${long}"`, async ({ page, foreman, fakeGame }) => {
    test.skip(isPhone(page), "the widths are set here");
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await pickLanguage(page, lang);
    await page.goto("/worlds");
    // The words a live update would put in both copies (server.html, "server-facts").
    const show = (state: string, word: string) =>
      page.evaluate(([s, w]) => {
        for (const el of document.querySelectorAll("#nav-lamp, #site-state")) el.innerHTML = `<span class="lamp lamp--${s}"></span>&nbsp;${w}`;
        return document.querySelector("header.site")!.getBoundingClientRect().height;
      }, [state, word]);
    for (const width of [320, 660, 900, 1280]) {
      await page.setViewportSize({ width, height: 800 });
      const before = await show("running", short);
      const after = await show("crash-looping", long);
      expect(after, `${width} px`).toBe(before);
    }
  });
}
