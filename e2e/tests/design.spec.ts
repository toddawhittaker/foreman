import { darkBg, lightBg, test, expect, checkPage, createAdmin } from "./foreman.js";

// The component page draws every group once in each theme, whatever the
// system's, passes axe and fits 320 px (DESIGN 7, 10).
test("the component page shows each group in both themes", { tag: "@looks" }, async ({ page }) => {
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
  await expect(light.locator(".confirm li").first()).toHaveCSS("color", "rgb(28, 25, 21)");
  await expect(dark.locator(".confirm li").first()).toHaveCSS("color", "rgb(236, 231, 223)");
  // DESIGN 7.5: the crash-looping tag is solid in both themes, on-danger ink
  // on the danger fill, not the other tags' tint of their own ink.
  for (const pane of [light, dark]) {
    const tag = pane.locator(".state--crash-looping").first();
    await expect(tag).toBeVisible();
    const [fill, ink] = await tag.evaluate((el) => {
      const probe = document.createElement("span");
      el.parentElement!.append(probe);
      probe.style.color = "var(--color-danger)";
      const danger = getComputedStyle(probe).color;
      probe.style.color = "var(--color-on-danger)";
      const onDanger = getComputedStyle(probe).color;
      probe.remove();
      return [getComputedStyle(el).backgroundColor === danger, getComputedStyle(el).color === onDanger];
    });
    expect(fill, "the fill is --color-danger").toBe(true);
    expect(ink, "the ink is --color-on-danger").toBe(true);
  }
  await checkPage(page);
});
