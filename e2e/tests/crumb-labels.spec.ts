import { test, expect, checkPage, createAdmin, seedWorld } from "./foreman.js";
import type { Locator } from "@playwright/test";

// The breadcrumb is navigation (DESIGN 6), and labels are sentence case at
// 13 px with capitals kept for the overline (DESIGN 2).

const style = (l: Locator, prop: "textDecorationLine" | "textDecorationColor" | "textTransform" | "fontSize") =>
  l.evaluate((el, p) => getComputedStyle(el)[p], prop);

test("the breadcrumb is a navigation landmark holding an ordered list", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/saves");

  const crumb = page.getByRole("navigation", { name: "Breadcrumb" });
  const items = crumb.locator("ol > li");
  await expect(items).toHaveCount(3);
  await expect(items.last().locator('[aria-current="page"]')).toHaveText("Saves");
  await expect(crumb.locator("[aria-current]")).toHaveCount(1);

  // Links carry an underline at rest; the current page has none.
  const link = crumb.getByRole("link", { name: "Worlds" });
  expect(await style(link, "textDecorationLine")).toBe("underline");
  expect(await style(link, "textDecorationColor")).not.toMatch(/rgba\(0, 0, 0, 0\)|transparent/);
  expect(await style(crumb.locator("[aria-current]"), "textDecorationLine")).toBe("none");
  await checkPage(page);

  // The chevron is a mask; with forced colours it is painted in the text
  // colour, so it does not vanish into the page.
  await page.emulateMedia({ forcedColors: "active" });
  const paint = await link.evaluate((el) => [getComputedStyle(el, "::after").backgroundColor, getComputedStyle(document.body).backgroundColor]);
  expect(paint[0], "the chevron's paint differs from the page").not.toBe(paint[1]);
  expect(paint[0], "the chevron is in the text colour").toBe(await page.evaluate(() => getComputedStyle(document.body).color));
  await page.emulateMedia({ forcedColors: "none" });
});

test("column heads and fact names are 13 px sentence case; the overline keeps capitals", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);

  await page.goto("/worlds/base/saves");
  const head = page.locator('main table th[scope="col"]').first();
  expect(await style(head, "textTransform")).toBe("none");
  expect(await style(head, "fontSize")).toBe("13px");

  await page.goto("/worlds/base");
  const fact = page.locator("main .facts dt").first();
  expect(await style(fact, "textTransform")).toBe("none");
  expect(await style(fact, "fontSize")).toBe("13px");

  await page.goto("/worlds");
  expect(await style(page.locator("main .eyebrow").first(), "textTransform")).toBe("uppercase");
  await checkPage(page);
});

test("a Settings page opens with its breadcrumb in place of the overline", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/restart-schedule");
  const crumb = page.getByRole("navigation", { name: "Breadcrumb" });
  await expect(crumb.getByRole("link", { name: "Settings" })).toHaveAttribute("href", "/settings/general");
  await expect(crumb.locator('li:last-child [aria-current="page"]')).toHaveText("Restart schedule");
  await expect(page.locator("main .eyebrow")).toHaveCount(0);
  await checkPage(page);
});
