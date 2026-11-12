import { test, expect, checkPage, createAdmin } from "./foreman.js";

// --color-bg in each theme (DESIGN 3).
const darkBg = "rgb(18, 17, 16)";
const lightBg = "rgb(241, 239, 235)";

// The component page draws every group once in each theme, whatever the
// system's, passes axe and fits 320 px (DESIGN 7, 10).
test("the component page shows each group in both themes", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/design");
  await expect(page.getByRole("heading", { level: 1, name: "Components" })).toBeVisible();
  const light = page.locator('.design-pane[data-theme="light"]');
  const dark = page.locator('.design-pane[data-theme="dark"]');
  await expect(light).toHaveCount(11);
  await expect(dark).toHaveCount(11);
  await expect(light.first()).toHaveCSS("background-color", lightBg);
  await expect(dark.first()).toHaveCSS("background-color", darkBg);
  // Inherited text takes the pane's theme, not the page's.
  await expect(light.locator(".confirm li").first()).toHaveCSS("color", "rgb(27, 25, 22)");
  await expect(dark.locator(".confirm li").first()).toHaveCSS("color", "rgb(236, 232, 225)");
  await checkPage(page);
});
