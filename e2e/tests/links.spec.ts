import { test, expect, checkPage, createAdmin, setUpModsWorld, openModTools } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { rmSync } from "node:fs";
import { join } from "node:path";

// #656, DESIGN 7.1: a link to another tab ends in a chevron, a link out of
// Foreman ends in a diagonal arrow. Both marks are drawn by the style sheet,
// so neither is part of the link's name.
test.use({ fakeGameOn: true, fakePortalOn: true });

const pseudo = (el: Locator, prop: string) => el.evaluate((e, p) => getComputedStyle(e, "::after").getPropertyValue(p), prop);

async function noSaveWorld(page: Page, foreman: Parameters<typeof setUpModsWorld>[1], fakeGame: Parameters<typeof setUpModsWorld>[2]) {
  await setUpModsWorld(page, foreman, fakeGame);
  rmSync(join(foreman.dataDir, "worlds", "base", "saves", "base.zip"));
  await page.goto("/worlds/base/mods");
  await openModTools(page);
}

test("a link to another tab is not a button, ends in a chevron, and opens that tab", async ({ page, foreman, fakeGame }) => {
  await noSaveWorld(page, foreman, fakeGame!);
  const link = page.getByRole("link", { name: "Upload a save", exact: true });
  await expect(link).toHaveClass("link-tab");
  await expect(link).not.toHaveClass(/button/);
  expect(await link.evaluate((e) => getComputedStyle(e).textDecorationLine)).toBe("underline");
  // The chevron is drawn from the style sheet: a mask of the chevron picture, 1rem wide.
  expect(await pseudo(link, "mask-image")).toContain("url(");
  expect(await pseudo(link, "width")).toBe("16px");
  // It is not in the name, so a screen reader and a voice command say only the words.
  await expect(link).toHaveAccessibleName("Upload a save");
  expect(await link.evaluate((e) => e.textContent)).toBe("Upload a save");
  // The link is a 44 px target (SPEC 17.2).
  expect((await link.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await checkPage(page);
  await link.click();
  await expect(page).toHaveURL("/worlds/base/saves");
  await expect(page.locator("#save-file")).toBeVisible();
});

test("a link out of Foreman ends in an arrow that is not in its name", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/about");
  const link = page.locator("main").getByRole("link", { name: /GitHub/ });
  await expect(link).toHaveAttribute("target", "_blank");
  expect(await pseudo(link, "content")).toContain("↗");
  expect(await pseudo(link, "text-decoration-line")).toBe("none");
  const name = (await link.evaluate((e) => e.textContent)) ?? "";
  expect(name).not.toMatch(/[↗›>]/);
  await expect(link).toHaveAccessibleName("GitHub (opens in a new tab)");
  // A link inside Foreman has no arrow.
  const inside = page.getByRole("navigation", { name: "Main" }).getByRole("link").first();
  expect(await pseudo(inside, "content")).not.toContain("↗");
  await checkPage(page);
});

// Plan invariant: 320 px at 200% text in German and Russian, in both themes
// (the project sets the theme), without a sideways scroll or a clipped link.
test.describe("at 320 px and 200% text", () => {
  test.use({ viewport: { width: 320, height: 700 } });
  for (const lang of ["de", "ru"]) {
    test(`the chevron link fits in ${lang}`, async ({ page, foreman, fakeGame }) => {
      await noSaveWorld(page, foreman, fakeGame!);
      await page.context().addCookies([{ name: "foreman_lang", value: lang, url: foreman.url }]);
      await page.reload();
      await openModTools(page);
      await page.addStyleTag({ content: "html { font-size: 200% }" });
      const link = page.locator("#act-upload-save");
      await expect(link).toBeVisible();
      const box = (await link.boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(320);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      await checkPage(page);
    });
  }
});
