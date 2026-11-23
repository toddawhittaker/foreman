import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, live, stateBadge, pickLanguage } from "./foreman.js";
import type { Page } from "@playwright/test";

// DESIGN 7.3: the Done box's close button sits at its top end corner, and the
// text keeps room for it, so no line of the message runs under the button.

// Every line box of the message's paragraph, against the close button's box.
async function textUnderButton(page: Page) {
  return page.locator("#done-message").evaluate((box) => {
    const button = box.querySelector(".message__close")!.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(box.querySelector("p")!);
    return [...range.getClientRects()].filter(
      (r) => r.width > 0 && r.height > 0 && r.left < button.right && r.right > button.left && r.top < button.bottom && r.bottom > button.top,
    ).length;
  });
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  for (const lang of ["en", "de", "ru"]) {
    test(`in ${lang}, no line of the Done message runs under the close button`, async ({ page, foreman, fakeGame }) => {
      await createAdmin(page);
      if (lang !== "en") await pickLanguage(page, lang);
      seedWorld(foreman);
      seedSecond(foreman);
      await fakeGame!.call("/image", { ref: image21, on: true });
      await page.goto("/worlds/second");
      await page.locator("#act-switch").click();
      await page.locator("section.confirm button[type=submit]").first().click();
      await expect(stateBadge(page)).toHaveClass(/state--running/, live);
      await expect(page.locator("#done-message")).toBeVisible(live);
      await expect(page.locator("#act-dismiss")).toBeVisible();

      expect(await textUnderButton(page)).toBe(0);
      await checkPage(page);
    });
  }
});
