import { test, expect, checkPage, createAdmin, openMenu, seedWorld, signOut } from "./foreman.js";
import type { Page } from "@playwright/test";

// The header (DESIGN 6, Shell): the logo beside the name, linking home; the
// nav; the user menu with the languages and Sign out. Every project runs
// these, so they hold at 1280 and 320 px in both themes.

const phone = (page: Page) => (page.viewportSize()?.width ?? 1000) <= 320;

test("the logo links home, is decorative, and is sharp on a 2x screen", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds");
  const home = page.getByRole("link", { name: "Foreman", exact: true });
  await expect(home).toHaveAttribute("href", "/");
  const logo = home.locator("img");
  // Empty alt: the link's name is the word "Foreman", not a picture's description.
  await expect(logo).toHaveAttribute("alt", "");
  await expect(logo).toHaveAttribute("srcset", /logo-64\.png\?v=\w+ 2x/);
  const size = await logo.boundingBox();
  expect(size!.width).toBe(32);
  expect(size!.height).toBe(32);
  // At 320 px only the icon shows; the name is still the link's name.
  const word = await home.locator(".product__name").boundingBox();
  if (phone(page)) expect(word!.width).toBeLessThanOrEqual(1);
  else expect(word!.width).toBeGreaterThan(20);
  await home.click();
  await expect(page).toHaveURL("/");
  await checkPage(page);
});

test("at 200% text the header still fits without sideways scroll", async ({ page, foreman }) => {
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

// SPEC 22.2: the menu holds the eight languages and Sign out, and opens and
// closes from the keyboard.
test("the user menu shows who is signed in, the languages and Sign out", async ({ page }) => {
  await createAdmin(page);
  const menu = page.locator("details.menu");
  await expect(menu.locator("summary")).toHaveText(/admin\s+English/);
  await openMenu(page);
  await expect(page.getByText("Signed in as admin")).toBeVisible();
  const langs = page.getByRole("group", { name: "Language" }).getByRole("button");
  await expect(langs).toHaveCount(8);
  await expect(langs.first()).toHaveAttribute("aria-current", "true");
  await expect(page.getByRole("button", { name: "Deutsch" })).toHaveAttribute("lang", "de");
  await checkPage(page);

  // Escape closes it and puts focus back on the summary.
  await page.getByRole("button", { name: "Deutsch" }).focus();
  await page.keyboard.press("Escape");
  await expect(menu).not.toHaveAttribute("open", "");
  await expect(menu.locator("summary")).toBeFocused();
  // A click outside closes it too.
  await openMenu(page);
  // The panel covers most of a phone, so the click lands at the very edge.
  await page.mouse.click(2, 300);
  await expect(menu).not.toHaveAttribute("open", "");

  await signOut(page);
  await expect(page).toHaveURL("/signed-out");
  // Signed out, the menu offers only the languages.
  await openMenu(page);
  await expect(page.getByRole("button", { name: "Sign out" })).toHaveCount(0);
  await expect(page.getByRole("group", { name: "Language" }).getByRole("button")).toHaveCount(8);
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("the menu opens and changes the language", async ({ page }) => {
    await page.goto("/setup");
    await page.locator("details.menu > summary").click();
    await page.getByRole("button", { name: "Français" }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "fr");
    await expect(page).toHaveURL("/setup");
  });
});
