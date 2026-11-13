import type { Page } from "@playwright/test";
import { test, expect, checkPage, createAdmin, openMenu } from "./foreman.js";

// The page background is --color-bg: #121110 in dark, #f1efeb in light (DESIGN 3).
const darkBg = "rgb(20, 17, 14)";
const lightBg = "rgb(236, 233, 227)";

async function pickTheme(page: Page, name: string) {
  await openMenu(page);
  await page.getByRole("group", { name: "Theme" }).getByRole("button", { name }).click();
}

// SPEC 17.3: Dark overrides a light system, holds across pages, and Auto
// follows the system again.
test("Dark overrides a light system and Auto follows the system again", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await createAdmin(page);
  await expect(page.locator("body")).toHaveCSS("background-color", lightBg);
  await openMenu(page);
  const group = page.getByRole("group", { name: "Theme" });
  await expect(group.getByRole("button", { name: "Auto" })).toHaveAttribute("aria-current", "true");

  await page.goto("/worlds");
  await pickTheme(page, "Dark");
  await expect(page).toHaveURL("/worlds");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);
  await openMenu(page);
  await expect(group.getByRole("button", { name: "Dark" })).toHaveAttribute("aria-current", "true");
  await expect(group.getByRole("button", { name: "Auto" })).not.toHaveAttribute("aria-current");
  await checkPage(page);

  // The choice is kept for the browser, not the page.
  await page.goto("/snapshots");
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);

  await pickTheme(page, "Auto");
  await expect(page).toHaveURL("/snapshots");
  await expect(page.locator("html")).not.toHaveAttribute("data-theme");
  await expect(page.locator("body")).toHaveCSS("background-color", lightBg);
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);
});

test("Light overrides a dark system", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await createAdmin(page);
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);
  await pickTheme(page, "Light");
  await expect(page).toHaveURL("/");
  await expect(page.locator("body")).toHaveCSS("background-color", lightBg);
  await checkPage(page);
});

// The choices are plain buttons in the details menu: reachable by keyboard alone.
test("the theme choice works from the keyboard", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await createAdmin(page);
  await page.locator("details.menu > summary").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("details.menu")).toHaveAttribute("open", "");
  const dark = page.getByRole("group", { name: "Theme" }).getByRole("button", { name: "Dark" });
  for (let i = 0; i < 20 && !(await dark.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(dark).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);
});

// SPEC 17.2: each choice is a 44 px target.
test("the theme choices are 44 px targets", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  for (const name of ["Auto", "Light", "Dark"]) {
    const box = await page.getByRole("group", { name: "Theme" }).getByRole("button", { name }).boundingBox();
    expect(box?.height, name).toBeGreaterThanOrEqual(44);
    expect(box?.width, name).toBeGreaterThanOrEqual(44);
  }
});

// WCAG 1.4.4: at 320 px and 200% text the open menu's theme choices stay
// inside the window and can still be pressed.
test("at 320 px and 200% text the theme choices fit", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await page.emulateMedia({ colorScheme: "light" });
  await createAdmin(page);
  await page.addStyleTag({ content: "html { font-size: 200% }" });
  await openMenu(page);
  for (const name of ["Auto", "Light", "Dark"]) {
    const box = await page.getByRole("group", { name: "Theme" }).getByRole("button", { name }).boundingBox();
    expect(box && box.x >= 0 && box.x + box.width <= 320, name).toBe(true);
  }
  const scroll = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(scroll).toBeLessThanOrEqual(0);
  await pickTheme(page, "Dark");
  await expect(page.locator("body")).toHaveCSS("background-color", darkBg);
});
