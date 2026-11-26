import { test, expect, checkPage, createAdmin, openMenu, seedWorld, signOut } from "./foreman.js";
import type { Page } from "@playwright/test";

// The header (DESIGN 6, Shell): the rusty wordmark, linking home; the
// nav; the user menu with the languages and Sign out. Every project runs
// these, so they hold at 1280 and 320 px in both themes.

const phone = (page: Page) => (page.viewportSize()?.width ?? 1000) <= 320;

test("the wordmark links home, is named Foreman, and is sharp on a 2x screen", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds");
  const home = page.getByRole("link", { name: "Foreman", exact: true });
  await expect(home).toHaveAttribute("href", "/");
  const logo = home.locator("img");
  // The wordmark's alt text is the link's name.
  await expect(logo).toHaveAttribute("alt", "Foreman");
  // The 260 px file drawn at about 130 px, so a 2x screen gets every pixel.
  await expect(logo).toHaveAttribute("src", /wordmark-260\.webp\?v=\w+/);
  const size = (await logo.boundingBox())!;
  // 40 px tall in the bar, smaller on a phone, so the word on the plate reads.
  if (phone(page)) expect(size.height).toBeGreaterThanOrEqual(24);
  else expect(size.height).toBe(40);
  expect(size.width).toBeGreaterThan(size.height * 3);
  await home.click();
  await expect(page).toHaveURL("/");
  await checkPage(page);
});

test("at 200% text the header still fits without sideways scroll", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/");
  await page.evaluate(() => {
    document.documentElement.style.fontSize = "200%";
  });
  const size = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(size.scroll).toBeLessThanOrEqual(size.client);
  await openMenu(page);
  const panel = await page.locator(".menu__panel").boundingBox();
  expect(panel!.x).toBeGreaterThanOrEqual(0);
  expect(panel!.x + panel!.width).toBeLessThanOrEqual(size.client);
});

// SPEC 22.2, DESIGN 6: the menu holds the language row, the theme row and
// Sign out, and opens and closes from the keyboard.
test("the user menu shows who is signed in, the languages and Sign out", { tag: "@looks" }, async ({ page }) => {
  await createAdmin(page);
  const menu = page.locator("details.menu");
  await expect(menu.locator(":scope > summary")).toHaveText(/admin\s+English/);
  await openMenu(page);
  await expect(page.getByText("Signed in as admin")).toBeVisible();
  const row = page.locator("details.menu__langs > summary");
  await expect(row).toHaveAccessibleName("Language English");
  await row.click();
  const langs = page.locator("details.menu__langs").getByRole("button");
  await expect(langs).toHaveCount(7);
  await expect(page.getByRole("button", { name: "Deutsch" })).toHaveAttribute("lang", "de");
  await checkPage(page);

  // Escape folds the open list first, then closes the menu, focus on the summary each time.
  await page.getByRole("button", { name: "Deutsch" }).focus();
  await page.keyboard.press("Escape");
  await expect(page.locator("details.menu__langs")).not.toHaveAttribute("open", "");
  await expect(row).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(menu).not.toHaveAttribute("open", "");
  await expect(menu.locator(":scope > summary")).toBeFocused();
  // A click outside closes it too.
  await openMenu(page);
  // The panel covers most of a phone, so the click lands at the very edge.
  await page.mouse.click(2, 300);
  await expect(menu).not.toHaveAttribute("open", "");

  await signOut(page);
  await expect(page).toHaveURL("/signed-out");
  // Signed out, the menu offers the languages and the theme, no Sign out.
  await openMenu(page);
  await expect(page.getByRole("button", { name: "Sign out" })).toHaveCount(0);
  await page.locator("details.menu__langs > summary").click();
  await expect(page.locator("details.menu__langs").getByRole("button")).toHaveCount(7);
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("the menu opens and changes the language", { tag: "@looks" }, async ({ page }) => {
    await page.goto("/setup");
    await page.locator("details.menu > summary").click();
    await page.locator("details.menu__langs > summary").click();
    await page.getByRole("button", { name: "Français" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "fr");
    await expect(page).toHaveURL("/setup");
  });
});

// DESIGN 6, User menu: the panel is sized for its rows, not its button, so
// every language's name keeps one line and the theme buttons sit inside it,
// on a wide screen and at 390 px.
test("the user menu panel holds its rows on one line at 1920 and 390 px", { tag: "@looks" }, async ({ page }) => {
  test.skip(test.info().project.name.startsWith("phone"), "the widths are set here on desktop");
  await createAdmin(page);
  for (const width of [1920, 390]) {
    await page.setViewportSize({ width, height: 1000 });
    await openMenu(page);
    const panel = (await page.locator(".menu__panel").boundingBox())!;
    expect(panel.x).toBeGreaterThanOrEqual(0);
    expect(panel.x + panel.width).toBeLessThanOrEqual(width);
    const inside = async (sel: string) => {
      for (const b of await page.locator(sel).all()) {
        const r = (await b.boundingBox())!;
        expect(r.x).toBeGreaterThanOrEqual(panel.x);
        expect(r.x + r.width).toBeLessThanOrEqual(panel.x + panel.width);
      }
    };
    await inside(".menu__icon");
    await inside("details.menu__langs > summary");
    await page.locator("details.menu__langs > summary").click();
    // Each name is one line: its box is no taller than one line of its text.
    for (const n of await page.locator(".menu__panel .menu__name").all()) {
      const lines = await n.evaluate((e) => e.getBoundingClientRect().height / parseFloat(getComputedStyle(e).lineHeight));
      expect(lines).toBeLessThan(1.5);
    }
    await inside(".menu__langs .menu__item");
    if (width === 1920) await page.screenshot({ path: `test-results/menu-${test.info().project.name}.png` });
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
  }
});
