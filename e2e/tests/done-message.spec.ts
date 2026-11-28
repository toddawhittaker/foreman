import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, live, stateBadge, pickLanguage } from "./foreman.js";

// DESIGN 6, 7.3, SPEC 17.9: after a switch the Done message is a toast, and
// the plate keeps the way back, Switch back, with the close that drops it, in
// a row under the actions. In every language the two sit side by side and
// never overlap.

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  for (const lang of ["en", "de", "ru"]) {
    test(`in ${lang}, the Undo row's button and its close never overlap`, { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
      await createAdmin(page);
      if (lang !== "en") await pickLanguage(page, lang);
      seedWorld(foreman);
      seedSecond(foreman);
      await fakeGame!.call("/image", { ref: image21, on: true });
      await page.goto("/worlds/second");
      await page.locator("#act-switch").click();
      await page.locator("section.confirm button[type=submit]").first().click();
      await expect(stateBadge(page)).toHaveClass(/state--running/, live);
      await expect(page.locator("#toasts .toast--success")).toBeVisible(live);
      await expect(page.locator("#act-dismiss")).toBeVisible();

      const undo = (await page.locator("#act-undo").boundingBox())!;
      const close = (await page.locator("#act-dismiss").boundingBox())!;
      const overlap = undo.x < close.x + close.width && close.x < undo.x + undo.width && undo.y < close.y + close.height && close.y < undo.y + undo.height;
      expect(overlap).toBe(false);
      await checkPage(page);
    });
  }

  // DESIGN 6, "Toasts": the Done toast shows once. A reload after it showed,
  // and a reconnect of the live stream, bring no toast back, while the Undo
  // row stays until it is closed.
  test("a reload after the Done toast shows no toast but keeps the Undo row", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/worlds/second");
    await page.locator("#act-switch").click();
    await page.locator("section.confirm button[type=submit]").first().click();
    await expect(stateBadge(page)).toHaveClass(/state--running/, live);
    await expect(page.locator("#toasts .toast--success")).toHaveCount(1, live);
    await expect(page.locator("#result-undo")).toBeVisible();

    await page.reload();
    await expect(stateBadge(page)).toHaveClass(/state--running/, live);
    await expect(page.locator("#result-undo")).toBeVisible();
    // Long enough for the live stream to open and send its first update.
    await page.waitForTimeout(1_500);
    await expect(page.locator("#toasts [data-toast]")).toHaveCount(0);
    await page.reload();
    await expect(page.locator("#result-undo")).toBeVisible();
    await expect(page.locator("#toasts [data-toast]")).toHaveCount(0);
  });
});
