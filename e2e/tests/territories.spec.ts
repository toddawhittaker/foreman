import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import type { Page, Route } from "@playwright/test";

// SPEC 5.6, DESIGN 8.8: Vulcanus's demolisher territories on the map preview.
// One box, beside the planet choice, off by default and shown only for
// Vulcanus on a Space Age world; the territories are asked for after the
// picture, drawn over it and in the large view, counted in words under it for
// screen readers, and drawn into the download while the box is ticked. The
// thumbnail's labels are short and stay inside the picture; the large view's
// are whole words. The fake game's run gives one small, one medium and one big
// territory, in three corners.
test.use({ fakeGameOn: true });

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
// The page's box; the large view has its own, found by largeBox.
const box = (page: Page) => section(page).getByLabel("Show demolisher territories");
// The same box by id, for while the large view is open and makes the page inert.
const pageBox = (page: Page) => page.locator("#preview-territories");
const overlay = (page: Page) => section(page).locator("a.picture .territories");
const list = (page: Page) => section(page).locator(".territory-counts");
// The words a label shows, not the other length kept for the other view.
const labels = (o: ReturnType<Page["locator"]>) => o.locator(".territories__label").evaluateAll((ls) => ls.map((l) => (l as HTMLElement).innerText));
const counts = "Demolisher territories in this picture: 1 small, 1 medium, 1 big.";
const isTerritories = (url: string) => url.endsWith("/generate/preview/territories");
// One matcher, so unroute finds the route it set.
const territoryURL = (u: URL) => isTerritories(u.pathname);

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

async function vulcanus(page: Page) {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
}

// Holds the territory requests until released, so a test sees the picture first.
function holdTerritories(page: Page) {
  const held: Route[] = [];
  return {
    held,
    start: () => page.route(territoryURL, (r) => { held.push(r); }),
    // Requests the page dropped are handled already, so only the rest go on.
    release: async () => {
      for (const r of held.splice(0)) await r.continue().catch(() => {});
      await page.unroute(territoryURL);
    },
  };
}

test("the box shows only beside Vulcanus on a Space Age world, and is off", async ({ page }) => {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await expect(box(page)).toHaveCount(0);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(box(page)).toBeVisible();
  await expect(box(page)).not.toBeChecked();
  await expect(box(page)).toHaveAccessibleDescription("Finding them takes a few seconds more.");
  // Off, nothing runs and nothing is drawn.
  await expect(overlay(page)).toHaveCount(0);
  await checkPage(page);
  // Another planet has no box.
  await page.locator("#mg-resources-gleba summary").click();
  await expect(section(page).getByRole("img", { name: /^Map preview of Gleba/ })).toBeVisible();
  await expect(box(page)).toHaveCount(0);
  // Without Space Age there is no Vulcanus, so no box.
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(box(page)).toBeVisible();
  await page.getByLabel("Space Age is on").uncheck();
  await expect(section(page).getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
  await expect(box(page)).toHaveCount(0);
});

test("ticked, the territories come after the picture, over it and in words under it", async ({ page }) => {
  await vulcanus(page);
  const hold = holdTerritories(page);
  await hold.start();
  await box(page).check();
  // The request waits; the picture is there, with a note and no overlay yet.
  await expect.poll(() => hold.held.length).toBe(1);
  await expect(section(page).locator("#territory-result")).toHaveText("Finding the demolisher territories…");
  await expect(overlay(page)).toHaveCount(0);
  await hold.release();
  await expect(list(page)).toHaveText(counts);
  await expect(page.locator("#announcer")).toHaveText(counts);
  await expect(box(page)).toBeFocused();
  // The overlay: stripes in picture pixels, one border path, sizes in words, all hidden from screen readers.
  const o = overlay(page);
  await expect(o).toHaveAttribute("aria-hidden", "true");
  await expect(o.locator("svg")).toHaveAttribute("viewBox", "0 0 1280 1280");
  await expect(o.locator("svg")).toHaveAttribute("preserveAspectRatio", "none");
  await expect(o.locator("rect")).toHaveCount(9); // three runs a territory, three chunks wide
  await expect(o.locator("path.territories__border")).toHaveAttribute("vector-effect", "non-scaling-stroke");
  expect(await labels(o)).toEqual(["Sm.", "Med.", "Big"]);
  // The picture's name is not changed by the labels inside its link.
  await expect(section(page).getByRole("link", { name: /^Map preview of Vulcanus, seed \d+\. The pin is where players start\. \(opens the large view\)$/ })).toBeVisible();
  // The overlay covers the map exactly, and the labels keep the text size.
  const img = (await section(page).locator("a.picture img").boundingBox())!;
  const svg = (await o.locator("svg").boundingBox())!;
  expect(Math.abs(svg.x - img.x - 1)).toBeLessThan(0.6);
  expect(Math.abs(svg.width - (img.width - 2))).toBeLessThan(0.6);
  // Small plain type, so the labels do not cover the map.
  expect(await o.locator(".territories__label").first().evaluate((l) => getComputedStyle(l).fontSize)).toBe("12px");
  // The small territory is at the top left: its label is in the picture's top left part.
  const small = (await o.locator(".territories__label").first().boundingBox())!;
  expect(small.x + small.width / 2 - img.x).toBeLessThan(img.width / 6);
  // The territories are in the corners, and every label stays inside the picture.
  for (const label of await o.locator(".territories__label").all()) {
    const b = (await label.boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(img.x);
    expect(b.y).toBeGreaterThanOrEqual(img.y);
    expect(b.x + b.width).toBeLessThanOrEqual(img.x + img.width);
    expect(b.y + b.height).toBeLessThanOrEqual(img.y + img.height);
  }
  // The counts are for screen readers only: no caption under the picture.
  expect(await list(page).evaluate((p) => p.getBoundingClientRect().height)).toBeLessThanOrEqual(1);
  await checkPage(page);
  // Off again, they go, and nothing more is asked.
  await box(page).uncheck();
  await expect(overlay(page)).toHaveCount(0);
  await expect(list(page)).toHaveCount(0);
});

test("the labels' white text and dark ring meet the contrast needed", async ({ page }) => {
  await vulcanus(page);
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  const style = await overlay(page).locator(".territories__label").first().evaluate((l) => {
    const s = getComputedStyle(l);
    return { color: s.color, shadow: s.textShadow, weight: s.fontWeight };
  });
  expect(style.color).toBe("rgb(255, 255, 255)");
  expect(style.weight).toBe("400");
  // The ring is near black on all eight sides: white on it is about 18 to 1.
  expect(style.shadow.match(/rgb\(16, 16, 16\)/g)?.length).toBe(8);
});

test("the download link draws the territories in while the box is ticked", async ({ page }) => {
  await vulcanus(page);
  const link = section(page).getByRole("link", { name: /^Download Vulcanus/ });
  await expect(link).toHaveText("Download Vulcanus (PNG)");
  await expect(link).toHaveAttribute("href", /\/vulcanus\/download$/);
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  await expect(link).toHaveText("Download Vulcanus with territories (PNG)");
  await expect(link).toHaveAttribute("href", /\/vulcanus\/download\?territories=1$/);
  const download = page.waitForEvent("download");
  await link.click();
  expect((await download).suggestedFilename()).toMatch(/-vulcanus-territories\.png$/);
  await box(page).uncheck();
  await expect(link).toHaveText("Download Vulcanus (PNG)");
  await expect(link).toHaveAttribute("href", /\/vulcanus\/download$/);
});

test("the large view shows the same overlay, aligned under zoom", async ({ page }) => {
  await vulcanus(page);
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  const dialog = page.locator("#large-view");
  await expect(dialog).toBeVisible();
  const o = dialog.locator(".territories");
  expect(await labels(o)).toEqual(["Small", "Medium", "Big"]);
  // At the fitted size, on a phone too, the corner labels stay inside the picture.
  const fitted = (await dialog.locator("#large-img").boundingBox())!;
  for (const label of await o.locator(".territories__label").all()) {
    const b = (await label.boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(fitted.x);
    expect(b.x + b.width).toBeLessThanOrEqual(fitted.x + fitted.width);
  }
  // The overlay is hidden from screen readers, so the dialog carries the page's sentence itself (SPEC 17).
  const words = dialog.locator("#large-territory-counts");
  await expect(words).toHaveText(counts);
  await expect(words).toBeVisible();
  await expect(dialog.getByText(counts)).toHaveCount(1);
  // Its copy uses the page's pattern: the id is there once.
  await expect(page.locator("#territory-stripes")).toHaveCount(1);
  await expect(dialog.locator("#large-download")).toHaveAttribute("href", /territories=1$/);
  await expect(dialog.locator("#large-download-name")).toHaveText("Download Vulcanus map with territories");
  for (let i = 0; i < 6; i++) await page.keyboard.press("+");
  const img = (await dialog.locator("#large-img").boundingBox())!;
  const svg = (await o.locator("svg").boundingBox())!;
  expect(Math.abs(svg.x - img.x)).toBeLessThan(0.6);
  expect(Math.abs(svg.width - img.width)).toBeLessThan(0.6);
  expect(await o.locator(".territories__label").first().evaluate((l) => getComputedStyle(l).fontSize)).toBe("14px");
  await checkPage(page);
  // Another planet in the view has none.
  await page.keyboard.press("0");
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("dialog", { name: "Gleba" })).toBeVisible();
  await expect(dialog.locator(".territories")).toHaveCount(0);
  await expect(words).toBeEmpty();
});

test("a change of settings drops the territory request on its way", async ({ page }) => {
  await vulcanus(page);
  const hold = holdTerritories(page);
  await hold.start();
  const aborted: string[] = [];
  page.on("requestfailed", (r) => { if (isTerritories(new URL(r.url()).pathname)) aborted.push(r.failure()?.errorText ?? ""); });
  await box(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  await page.locator("#generate-seed").fill("777");
  await expect.poll(() => aborted.length).toBe(1);
  // The new picture comes, then a new request for its territories.
  await expect(section(page).getByRole("img", { name: "Map preview of Vulcanus, seed 777. The pin is where players start." })).toBeVisible();
  await expect.poll(() => hold.held.length).toBe(2);
  await hold.release();
  await expect(list(page)).toHaveText(counts);
  await expect(overlay(page)).toHaveCount(1);
});

test("looking at another planet drops the territory request", async ({ page }) => {
  await vulcanus(page);
  const hold = holdTerritories(page);
  await hold.start();
  const aborted: string[] = [];
  page.on("requestfailed", (r) => { if (isTerritories(new URL(r.url()).pathname)) aborted.push(r.url()); });
  await box(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  await page.locator("#mg-resources-nauvis summary").click();
  await expect.poll(() => aborted.length).toBe(1);
  await expect(section(page).getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
  await page.waitForTimeout(500);
  expect(hold.held.length).toBe(1);
  // Back on Vulcanus the box is still ticked, and they are asked for again.
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(box(page)).toBeChecked();
  await expect.poll(() => hold.held.length).toBe(2);
  await hold.release();
  await expect(list(page)).toHaveText(counts);
});

test("a failed request says so and offers to try again", async ({ page }) => {
  await vulcanus(page);
  await page.route(territoryURL, (r) => r.fulfill({ status: 500, body: "" }));
  await box(page).check();
  const result = section(page).locator("#territory-result");
  await expect(result).toContainText("The request for the demolisher territories failed.");
  await expect(page.locator("#announcer")).toContainText("The request for the demolisher territories failed.");
  // The sentence alone, without the button's label after it.
  await expect(page.locator("#announcer")).not.toContainText("Try again");
  await checkPage(page);
  await page.unroute(territoryURL);
  await result.getByRole("button", { name: "Try again" }).click();
  await expect(list(page)).toHaveText(counts);
  await expect(box(page)).toBeFocused();
});

test("unticking while a picture is drawn wins over the reply", async ({ page }) => {
  await vulcanus(page);
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  // Hold the next picture's reply, which was asked for with the box ticked.
  const held: Route[] = [];
  const picture = (u: URL) => u.pathname.endsWith("/generate/preview");
  await page.route(picture, (r) => { held.push(r); });
  // Left before the request goes, so ticking the box later fires no change on it.
  await page.locator("#generate-seed").fill("888");
  await page.locator("#generate-seed").blur();
  await expect.poll(() => held.length).toBe(1);
  await box(page).uncheck();
  await held[0].continue();
  await page.unroute(picture);
  await expect(section(page).getByRole("img", { name: "Map preview of Vulcanus, seed 888. The pin is where players start." })).toBeVisible();
  await expect(box(page)).not.toBeChecked();
  await expect(overlay(page)).toHaveCount(0);
  await expect(list(page)).toHaveCount(0);
  await expect(section(page).getByRole("link", { name: "Download Vulcanus (PNG)" })).toHaveAttribute("href", /\/vulcanus\/download$/);
});

// Issue 499, SPEC 5.6: the large view offers the same box, wherever the planet
// viewed has territories. One choice, held by the form's "territories" field.
const largeBox = (page: Page) => page.locator("#large-view").getByLabel("Show demolisher territories");
const largeOverlay = (page: Page) => page.locator("#large-view .territories");

async function openLarge(page: Page) {
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await expect(page.locator("#large-view")).toBeVisible();
}

test("the large view's box unticks and ticks the overlay, and the page's box matches", async ({ page }) => {
  await vulcanus(page);
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  await openLarge(page);
  const dialog = page.locator("#large-view");
  await expect(largeBox(page)).toBeVisible();
  await expect(largeBox(page)).toBeChecked();
  await expect(largeBox(page)).toHaveAccessibleDescription("Finding them takes a few seconds more.");
  await expect(largeOverlay(page)).toHaveCount(1);
  // Off: the overlay, its words and the download's territories go, and the page's box follows.
  await largeBox(page).uncheck();
  await expect(largeOverlay(page)).toHaveCount(0);
  await expect(dialog.locator("#large-territory-counts")).toBeEmpty();
  await expect(dialog.locator("#large-download")).toHaveAttribute("href", /\/vulcanus\/download$/);
  await expect(pageBox(page)).not.toBeChecked();
  // On: the box says it is looking, then the overlay and its words return.
  const hold = holdTerritories(page);
  await hold.start();
  await largeBox(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  await expect(dialog.locator("#large-status")).toHaveText("Finding the demolisher territories…");
  await expect(largeOverlay(page)).toHaveCount(0);
  await hold.release();
  await expect(largeOverlay(page)).toHaveCount(1);
  await expect(dialog.locator("#large-territory-counts")).toHaveText(counts);
  await expect(dialog.locator("#large-status")).toHaveText(counts);
  await expect(largeBox(page)).toBeChecked();
  await expect(dialog.locator("#large-download")).toHaveAttribute("href", /territories=1$/);
  await expect(pageBox(page)).toBeChecked();
  await checkPage(page);
  // Closing leaves the page as the large view had it.
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(box(page)).toBeChecked();
  await expect(overlay(page)).toHaveCount(1);
});

test("a choice made in the large view holds after Previous and Next, and the box is only on Vulcanus", async ({ page }) => {
  await vulcanus(page);
  await openLarge(page);
  const dialog = page.locator("#large-view");
  await expect(largeBox(page)).not.toBeChecked();
  await largeBox(page).check();
  await expect(largeOverlay(page)).toHaveCount(1);
  // Another planet has no territories, so no box; the choice is not lost.
  await dialog.locator("#large-next").click();
  await expect(page.getByRole("dialog", { name: "Gleba" })).toBeVisible();
  await expect(dialog.getByRole("img", { name: /^Map preview of Gleba/ })).toBeVisible();
  await expect(largeBox(page)).toBeHidden();
  await expect(dialog.locator(".territories")).toHaveCount(0);
  await dialog.locator("#large-prev").click();
  await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  await expect(largeBox(page)).toBeVisible();
  await expect(largeBox(page)).toBeChecked();
  await expect(largeOverlay(page)).toHaveCount(1);
  // Off, it stays off the same way.
  await largeBox(page).uncheck();
  await expect(largeOverlay(page)).toHaveCount(0);
  await dialog.locator("#large-next").click();
  await expect(page.getByRole("dialog", { name: "Gleba" })).toBeVisible();
  await expect(largeBox(page)).toBeHidden();
  await dialog.locator("#large-prev").click();
  await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  await expect(largeBox(page)).toBeVisible();
  await expect(largeBox(page)).not.toBeChecked();
  await expect(largeOverlay(page)).toHaveCount(0);
  // Closing, the page's box is the same choice, and ticking it there shows in the view again.
  await dialog.locator("#large-close").click();
  await expect(box(page)).not.toBeChecked();
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  await openLarge(page);
  await expect(largeBox(page)).toBeChecked();
  await expect(largeOverlay(page)).toHaveCount(1);
});

test("with every planet shown, the large view's box follows the planet viewed", async ({ page }) => {
  await vulcanus(page);
  await section(page).locator("label.tab", { hasText: "All planets" }).click();
  await expect(section(page).getByRole("img")).toHaveCount(5);
  await box(page).check();
  await expect(list(page)).toHaveText(counts);
  await section(page).locator('a.picture[data-planet="nauvis"]').click();
  const dialog = page.locator("#large-view");
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
  await expect(largeBox(page)).toBeHidden();
  await dialog.locator("#large-next").click();
  await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  await expect(largeBox(page)).toBeChecked();
  await expect(largeOverlay(page)).toHaveCount(1);
  await largeBox(page).uncheck();
  await expect(largeOverlay(page)).toHaveCount(0);
  await expect(pageBox(page)).not.toBeChecked();
  await dialog.locator("#large-close").click();
  await expect(overlay(page)).toHaveCount(0);
});

test("the large view's box is reached and worked by keyboard, with focus shown", async ({ page }) => {
  await vulcanus(page);
  await section(page).getByRole("link", { name: /opens the large view/ }).focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#large-view")).toBeVisible();
  await tabTo(page, largeBox(page));
  await expect(largeBox(page)).toBeFocused();
  expect(await largeBox(page).evaluate((b) => getComputedStyle(b).outlineStyle)).not.toBe("none");
  await page.keyboard.press("Space");
  await expect(largeBox(page)).toBeChecked();
  await expect(largeOverlay(page)).toHaveCount(1);
  await expect(page.locator("#large-view #large-status")).toHaveText(counts);
  await expect(largeBox(page)).toBeFocused();
  await page.keyboard.press("Space");
  await expect(largeBox(page)).not.toBeChecked();
  await expect(largeOverlay(page)).toHaveCount(0);
  // Stepping to a planet without territories takes the box away; focus is not lost.
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("dialog", { name: "Gleba" })).toBeVisible();
  await expect(largeBox(page)).toBeHidden();
  await expect(page.locator("#large-view").locator(":focus")).toHaveCount(1);
});

test("a failed request in the large view is said there", async ({ page }) => {
  await vulcanus(page);
  await openLarge(page);
  await page.route(territoryURL, (r) => r.fulfill({ status: 500, body: "" }));
  await largeBox(page).check();
  await expect(page.locator("#large-view #large-status")).toContainText("The request for the demolisher territories failed.");
  await expect(largeOverlay(page)).toHaveCount(0);
  await page.unroute(territoryURL);
  // Off and on again asks once more.
  await largeBox(page).uncheck();
  await largeBox(page).check();
  await expect(largeOverlay(page)).toHaveCount(1);
});

test.describe("on a phone", () => {
  test.use({ viewport: { width: 320, height: 640 }, hasTouch: true });

  test("the large view's box is a 44 px target and fits at 320 px, at 200% text too", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await page.locator("#mg-resources-vulcanus").evaluate((d) => ((d as HTMLDetailsElement).open = true));
    await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
    await section(page).getByRole("link", { name: /opens the large view/ }).tap();
    const dialog = page.locator("#large-view");
    await expect(largeBox(page)).toBeVisible();
    const fits = async () => {
      const size = await dialog.evaluate((d) => ({ scroll: d.scrollWidth, client: d.clientWidth }));
      expect(size.scroll).toBeLessThanOrEqual(size.client);
      const row = (await dialog.locator("#large-territories-box").boundingBox())!;
      expect(row.x).toBeGreaterThanOrEqual(0);
      expect(row.x + row.width).toBeLessThanOrEqual(320);
    };
    const target = (await largeBox(page).locator("xpath=ancestor::label").boundingBox())!;
    expect(target.height).toBeGreaterThanOrEqual(44);
    await fits();
    await checkPage(page);
    await largeBox(page).tap();
    await expect(largeBox(page)).toBeChecked();
    await expect(largeOverlay(page)).toHaveCount(1);
    // The picture keeps a usable height beside it.
    expect((await dialog.locator("#large-frame").boundingBox())!.height).toBeGreaterThan(150);
    await page.evaluate(() => { document.documentElement.style.fontSize = "200%"; });
    await fits();
    await checkPage(page);
  });
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("Preview map with the box ticked draws the territories after the pictures", async ({ page }) => {
    await page.goto("/generate");
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(section(page).getByRole("img")).toHaveCount(5);
    await box(page).check();
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(list(page)).toHaveText(counts);
    await expect(overlay(page)).toHaveCount(1);
    await expect(section(page).getByRole("link", { name: "Download Vulcanus with territories (PNG)" })).toHaveAttribute("href", /territories=1$/);
    await expect(section(page).getByRole("link", { name: "Download all drawn planets (zip)" })).toHaveAttribute("href", /all\.zip\?territories=1$/);
    await expect(section(page).getByRole("button", { name: "Try again" })).toHaveCount(0);
  });
});
