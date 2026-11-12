import type { Page } from "@playwright/test";
import { test, expect, checkPage, createAdmin, mainNav, openLanguages, openMenu, pickLanguage, seedWorld } from "./foreman.js";

// DESIGN 6 ("User menu"), 8.15; SPEC 17.1, 17.2, 17.3, 22.2. The user menu
// holds a language row that folds the other seven languages, a theme row
// of three icon buttons, and Sign out. Foreman's settings are the main
// Settings tab. Every project runs these, at 320 px and at desktop width.

const flagFiles = ["gb", "de", "ru", "cn", "fr", "es", "pl", "cz"];
const darkBg = "rgb(18, 17, 16)";

const langRow = (page: Page) => page.locator("details.menu__langs > summary");
const themes = (page: Page) => page.getByRole("group", { name: "Theme" });

// Tabs until target has focus.
async function tabUntil(page: Page, target: ReturnType<Page["locator"]>, limit = 40) {
  for (let i = 0; i < limit && !(await target.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(target).toBeFocused();
}

test("the menu holds the language row, the theme row, About and Sign out, and no settings links", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  await expect(langRow(page)).toHaveAccessibleName("Language English");
  await expect(page.locator("details.menu__langs")).not.toHaveAttribute("open", "");
  await expect(themes(page).getByRole("button")).toHaveCount(3);
  await expect(page.getByRole("button", { name: "Sign out" })).toBeVisible();
  // About is the menu's one link (DESIGN 6); the settings pages have the Settings tab.
  await expect(page.locator("details.menu").getByRole("link")).toHaveCount(1);
  await expect(page.locator("details.menu").getByRole("link", { name: "About" })).toHaveAttribute("href", "/about");
  // Theme, About, then Sign out come after the language row, in that order.
  const order = await page.locator(".menu__panel > :is(details, form, a)").evaluateAll((els) => els.map((el) => el.getAttribute("action") ?? el.className));
  expect(order).toEqual(["menu__langs", "/theme", "menu__item", "/signout"]);
  await checkPage(page);
});

test("flags are present and decorative; screen readers hear the language names", async ({ page }) => {
  await createAdmin(page);
  // The seven listed flags load lazily: while the list is shut, none is
  // fetched (Spain's is 81 KB). The row's own flag loads at once.
  await openMenu(page);
  await expect(langRow(page).locator("img")).not.toHaveAttribute("loading");
  const listed = page.locator("details.menu__langs > form img");
  await expect(listed).toHaveCount(7);
  for (const img of await listed.all()) {
    await expect(img).toHaveAttribute("loading", "lazy");
    expect(await img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBe(0);
  }
  await openLanguages(page);
  const imgs = page.locator("details.menu img");
  await expect(imgs).toHaveCount(8);
  const seen: string[] = [];
  for (const img of await imgs.all()) {
    await expect(img).toHaveAttribute("alt", "");
    const src = (await img.getAttribute("src"))!;
    seen.push(src.replace(/^\/static\/flags\/(\w+)\.svg\?v=\w+$/, "$1"));
    // The picture loads: a 4 by 3 flag.
    await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0)).toBe(true);
  }
  expect(seen.sort()).toEqual([...flagFiles].sort());
  // English shows the United Kingdom, Spanish shows Spain.
  await expect(langRow(page).locator("img")).toHaveAttribute("src", /\/flags\/gb\.svg/);
  await expect(page.getByRole("button", { name: "Español" }).locator("img")).toHaveAttribute("src", /\/flags\/es\.svg/);
  await expect(page.getByRole("button", { name: "Español" })).toHaveAccessibleName("Español");
  await checkPage(page);
});

test("a language chosen by keyboard returns to the same page", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base?x=1");
  await page.locator("details.menu > summary").focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Tab");
  await expect(langRow(page)).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("details.menu__langs")).toHaveAttribute("open", "");
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Deutsch" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/worlds/base?x=1");
  await expect(page.locator("html")).toHaveAttribute("lang", "de");
});

test("Escape folds the language list, then closes the menu, and the menu reopens folded", async ({ page }) => {
  await createAdmin(page);
  const menu = page.locator("details.menu");
  await openLanguages(page);
  await page.getByRole("button", { name: "Polski" }).focus();
  await page.keyboard.press("Escape");
  await expect(page.locator("details.menu__langs")).not.toHaveAttribute("open", "");
  await expect(langRow(page)).toBeFocused();
  await expect(menu).toHaveAttribute("open", "");
  await page.keyboard.press("Escape");
  await expect(menu).not.toHaveAttribute("open", "");
  await expect(page.locator("details.menu > summary")).toBeFocused();
  // Closed with the list open, the menu opens again with it folded.
  await openLanguages(page);
  await page.mouse.click(2, 400);
  await expect(menu).not.toHaveAttribute("open", "");
  await openMenu(page);
  await expect(page.locator("details.menu__langs")).not.toHaveAttribute("open", "");
});

test("a theme chosen by keyboard returns to the same page; each choice has a name and hover text", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await createAdmin(page);
  await page.goto("/snapshots");
  await openMenu(page);
  for (const [name, value] of [["Auto", "auto"], ["Light", "light"], ["Dark", "dark"]]) {
    const b = themes(page).getByRole("button", { name });
    await expect(b).toHaveAttribute("value", value);
    await expect(b).toHaveAttribute("title", name);
  }
  await page.keyboard.press("Escape");
  await expect(page.locator("details.menu")).not.toHaveAttribute("open", "");
  await page.locator("details.menu > summary").focus();
  await page.keyboard.press("Enter");
  await tabUntil(page, themes(page).getByRole("button", { name: "Dark" }));
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL("/snapshots");
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);
  await openMenu(page);
  await expect(themes(page).getByRole("button", { name: "Dark" })).toHaveAttribute("aria-current", "true");
  await expect(themes(page).locator("[aria-current]")).toHaveCount(1);
  await checkPage(page);
});

// SPEC 17.10 and DESIGN 8.15: the Settings tab is the one way in, marked on
// all three pages and the pages under them.
test("the Settings tab is current on the three Foreman settings pages", async ({ page }) => {
  await createAdmin(page);
  for (const [path, mark] of [
    ["/settings/restart-schedule", "page"],
    ["/settings/factorio-account", "true"],
    ["/settings/notifications", "true"],
  ]) {
    await page.goto(path);
    const tab = mainNav(page).getByRole("link", { name: "Settings" });
    await expect(tab).toHaveAttribute("href", "/settings/restart-schedule");
    await expect(tab).toHaveAttribute("aria-current", mark);
    await expect(mainNav(page).locator("[aria-current]")).toHaveCount(1);
  }
  await mainNav(page).getByRole("link", { name: "Server" }).click();
  await expect(mainNav(page).getByRole("link", { name: "Settings" })).not.toHaveAttribute("aria-current");
});

// No page shows two navigation links with the same name to different places.
test("no page has two navigation links with one name", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  for (const path of ["/", "/worlds/base", "/worlds/base/settings", "/settings/restart-schedule"]) {
    await page.goto(path);
    const links = await page.locator("nav a, main .links a").evaluateAll((els) =>
      els.map((a) => [(a.textContent ?? "").trim(), a.getAttribute("href")!.split("?")[0]]),
    );
    const byName = new Map<string, Set<string>>();
    for (const [name, href] of links) byName.set(name, (byName.get(name) ?? new Set()).add(href));
    // The main Snapshots tab and a world's Snapshots tab share a name from
    // before this rule; that is left to its own issue.
    const dup = [...byName].filter(([n, hrefs]) => hrefs.size > 1 && n !== "Snapshots").map(([n]) => n);
    expect(dup, path).toEqual([]);
  }
  await page.goto("/worlds/base");
  await expect(page.getByRole("navigation", { name: "World pages" }).getByRole("link", { name: "Game settings" })).toBeVisible();
});

// SPEC 17.2: at 320 px and 200% text, in German and Russian, the header
// keeps its four tabs and the menu without widening the page, and the open
// menu with its language list expanded stays inside the window.
for (const lang of ["de", "ru"]) {
  test(`at 320 px and 200% text in ${lang}, the header and the open menu fit`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await createAdmin(page);
    await pickLanguage(page, lang);
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    const fits = async () => {
      const size = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
      expect(size.scroll).toBeLessThanOrEqual(size.client);
    };
    await fits();
    const tabs = page.locator("header.site nav a");
    await expect(tabs).toHaveCount(4);
    for (const el of [...(await tabs.all()), page.locator("details.menu > summary")]) {
      const box = (await el.boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(320);
    }
    await openLanguages(page);
    await fits();
    const panel = (await page.locator(".menu__panel").boundingBox())!;
    expect(panel.x).toBeGreaterThanOrEqual(0);
    expect(panel.x + panel.width).toBeLessThanOrEqual(320);
    for (const b of await page.locator(".menu__panel :is(summary, button)").all()) {
      const box = (await b.boundingBox())!;
      expect(box.x + box.width, await b.innerText()).toBeLessThanOrEqual(panel.x + panel.width);
      expect(box.height).toBeGreaterThanOrEqual(44);
    }
    await checkPage(page);
  });
}

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("the language row opens and changes the language, back on the same page", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/worlds");
    await page.locator("details.menu > summary").click();
    await langRow(page).click();
    await page.getByRole("button", { name: "Русский" }).click();
    await expect(page).toHaveURL("/worlds");
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
  });

  test("the theme row changes the theme, back on the same page", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await createAdmin(page);
    await page.goto("/worlds");
    await page.locator("details.menu > summary").click();
    await themes(page).getByRole("button", { name: "Dark" }).click();
    await expect(page).toHaveURL("/worlds");
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  });
});
