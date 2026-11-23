import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { openNewMap } from "./newmap.js";

// SPEC 5.6, 14.3, 17.14, 22: on step 3 of a 2.1 world one planet can be drawn
// from a real game. It is asked for, never drawn as the person types; the
// form waits while it draws; the result is said once; it takes the fast
// picture's place, captioned, until "Back to the fast preview". The fake game
// draws a canned chart.
test.use({ fakeGameOn: true });

test("a 2.1 world's planet drawn from a real game, and back", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "Charted" });
  await expect(page.locator("#preview a.picture").first()).toBeVisible({ timeout: 60_000 });

  const draw = page.getByRole("button", { name: "Draw from a real game (about 15 to 40 seconds)" });
  await expect(draw).toBeVisible();
  await page.getByRole("button", { name: /what drawing from a real game does/ }).click();
  await expect(page.locator("#preview-chart-tip")).toBeVisible();
  await checkPage(page);

  await draw.click();
  const chart = page.locator("#preview-chart figure");
  await expect(chart.locator("figcaption")).toContainText("From a real game, 1024 tiles", { timeout: 90_000 });
  await expect(chart.locator("img")).toHaveJSProperty("complete", true);
  expect(await chart.locator("img").evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
  // It stands in for the fast picture, which waits under it.
  await expect(page.locator("#preview section > ul.pictures")).toBeHidden();
  await expect(page.locator("#preview")).toHaveAttribute("aria-busy", "false");
  await expect(page.locator("#preview-button")).not.toHaveAttribute("aria-disabled", "true");
  await expect(page.locator("#announcer")).toHaveText("Real-game picture ready.");
  const back = page.getByRole("button", { name: "Back to the fast preview" });
  await expect(back).toBeFocused();
  await checkPage(page);

  await back.click();
  await expect(chart).toHaveCount(0);
  await expect(page.locator("#preview section > ul.pictures")).toBeVisible();
  await expect(draw).toBeFocused();

  // The picture is kept: asking again answers at once from the store.
  await draw.click();
  await expect(chart.locator("figcaption")).toContainText("From a real game", { timeout: 10_000 });
});

test("the form waits while the picture draws", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "Waiting" });
  await expect(page.locator("#preview a.picture").first()).toBeVisible({ timeout: 60_000 });
  // The reply is held back so the wait can be seen.
  let release: () => void = () => {};
  const held = new Promise<void>((r) => { release = r; });
  await page.route("**/generate/preview/chart", async (route) => {
    await held;
    await route.continue();
  });
  await page.getByRole("button", { name: "Draw from a real game (about 15 to 40 seconds)" }).click();
  await expect(page.locator("#preview-chart-wait")).toBeVisible();
  await expect(page.locator("#preview")).toHaveAttribute("aria-busy", "true");
  for (const id of ["#preview-button", "#newmap-save", "#preview-chart-button"]) {
    await expect(page.locator(id)).toHaveAttribute("aria-disabled", "true");
  }
  await expect(page.locator("#announcer")).toContainText("Drawing from a real game.");
  await checkPage(page);
  release();
  await expect(page.locator("#preview-chart figure")).toBeVisible({ timeout: 90_000 });
  await expect(page.locator("#preview-chart-wait")).toBeHidden();
  await expect(page.locator("#newmap-save")).not.toHaveAttribute("aria-disabled", "true");
});

// Step 1 has no map and no preview, so nothing to draw from a real game (SPEC 5.6).
test("step 1 offers no preview and no real-game picture", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/generate");
  await expect(page.locator("#generate-name")).toBeVisible();
  await expect(page.locator("#preview")).toHaveCount(0);
  await expect(page.locator("#preview-chart-button")).toHaveCount(0);
});

// DESIGN 8.8, SPEC 17.1: at 320 px the Download line wraps inside the page, with no sideways scroll.
test("the picture's Download line fits a 320 px phone", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "Narrow" });
  const draw = page.locator("#preview-chart-button");
  await expect(draw).toBeVisible({ timeout: 60_000 });
  await draw.scrollIntoViewIfNeeded();
  const wide = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(wide, "no sideways scroll").toBeLessThanOrEqual(0);
  const box = (await page.locator("#preview .pictures .picture-actions").boundingBox())!;
  expect(box.x + box.width).toBeLessThanOrEqual(320);
  await checkPage(page);
});

// DESIGN 8.8: on a wide pane the offer shares the picture's Download line, so the picture keeps its room.
test("the real-game offer sits on the picture's Download line", { tag: "@looks" }, async ({ page, foreman, fakeGame }, info) => {
  test.skip(info.project.name.startsWith("phone"), "a desktop layout");
  await page.setViewportSize({ width: 1920, height: 950 });
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "One line" });
  const download = page.locator("#preview .pictures .picture-actions > a.download");
  const draw = page.locator("#preview-chart-button");
  await expect(draw).toBeVisible({ timeout: 60_000 });
  const [a, b] = [(await download.boundingBox())!, (await draw.boundingBox())!];
  expect(Math.abs(a.y + a.height / 2 - (b.y + b.height / 2))).toBeLessThanOrEqual(2);
});
