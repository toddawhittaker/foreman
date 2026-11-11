import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import type { Page, Request, Route } from "@playwright/test";

// SPEC 5.6, 17: "Show resource amounts" in the large view. Off on load and
// kept while the page lives; ticked, the planet shown is counted once its
// picture is there, each patch is labelled with its amount (an infinite
// resource with its yield), and the same amounts are a collapsed text list
// for keyboard and screen-reader users. Labels never overlap one another, a
// demolisher label or the pin; zooming in makes room for more. Unticking,
// closing, another planet or a settings change drops the count on its way.
// The fake game's count on every planet: a large ore of three patches (two
// labelled within a few tiles of each other), a small ore of two and an
// infinite resource of two wells, all near the start.
test.use({ fakeGameOn: true });

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const dialog = (page: Page) => page.locator("#large-view");
const box = (page: Page) => dialog(page).getByRole("checkbox", { name: "Show resource amounts" });
const labels = (page: Page) => dialog(page).locator(".resources__label");
const counting = (page: Page) => page.locator("#large-counting");
const status = (page: Page) => page.locator("#large-status");
const result = (page: Page) => page.locator("#large-amounts-result");
const summary = (page: Page) => result(page).locator("summary");
const isResources = (url: string) => new URL(url).pathname.endsWith("/generate/preview/resources");
// One matcher, so unroute finds the route it set.
const resourceURL = (u: URL) => u.pathname.endsWith("/generate/preview/resources");
const planetOf = (r: Request) => new URLSearchParams(r.postData() ?? "").get("planet");
// The view needs 77rem for the settings beside the map; the desktop projects are 1280 wide.
const isWide = (page: Page) => (page.viewportSize()?.width ?? 0) >= 1240;
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png` });

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

async function openLarge(page: Page) {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await expect(dialog(page)).toBeVisible();
  await expect.poll(() => page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
}

// Holds the count requests until released, so a test sees what comes before.
function holdCounts(page: Page) {
  const held: Route[] = [];
  return {
    held,
    start: () => page.route(resourceURL, (r) => { held.push(r); }),
    // Requests the page dropped are handled already, so only the rest go on.
    release: async () => {
      for (const r of held.splice(0)) await r.continue().catch(() => {});
      await page.unroute(resourceURL);
    },
  };
}

// The labels shown, with their boxes, and the boxes always kept: the pin and
// any demolisher label.
function layout(page: Page) {
  return dialog(page).locator(".map").evaluate((map) => {
    const box = (e: Element) => { const r = e.getBoundingClientRect(); return { x0: r.left, y0: r.top, x1: r.right, y1: r.bottom, text: e.textContent ?? "" }; };
    return {
      map: box(map),
      shown: [...map.querySelectorAll(".resources__label:not([hidden])")].map(box),
      kept: [...map.querySelectorAll(".pin, .territories__label")].map(box),
      all: map.querySelectorAll(".resources__label").length,
    };
  });
}

type Box = { x0: number; y0: number; x1: number; y1: number };
const meet = (a: Box, b: Box) => a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1;

async function expectNoOverlap(page: Page) {
  const l = await layout(page);
  for (const [i, a] of l.shown.entries()) {
    for (const b of [...l.shown.slice(i + 1), ...l.kept]) expect(meet(a, b), `${a.text} meets ${b.text || "the pin"}`).toBe(false);
    // Inside the map, as the labels are pulled in at its edges.
    expect(a.x0).toBeGreaterThanOrEqual(l.map.x0 - 0.5);
    expect(a.x1).toBeLessThanOrEqual(l.map.x1 + 0.5);
  }
  return l;
}

test("off on load: nothing is counted until the box is ticked", async ({ page }) => {
  const asked: string[] = [];
  page.on("request", (r) => { if (isResources(r.url())) asked.push(r.url()); });
  await openLarge(page);
  await expect(box(page)).not.toBeChecked();
  await expect(box(page)).not.toHaveAttribute("name");
  await expect(counting(page)).toBeHidden();
  await expect(labels(page)).toHaveCount(0);
  await expect(result(page)).toBeEmpty();
  await page.waitForTimeout(300);
  expect(asked).toEqual([]);
  // The thumbnail never has labels.
  await expect(section(page).locator(".resources")).toHaveCount(0);
});

test("ticked, the count comes after the picture: labels on the map and the amounts as text", async ({ page }) => {
  await openLarge(page);
  const hold = holdCounts(page);
  await hold.start();
  await box(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  expect(planetOf(hold.held[0].request())).toBe("nauvis");
  // The wait is seen: a line and an indeterminate bar.
  await expect(counting(page)).toBeVisible();
  await expect(counting(page)).toContainText("Counting resources in the map… This can take up to a minute.");
  // Said once as it starts.
  await expect(status(page)).toHaveText("Counting resources in the map… This can take up to a minute.");
  await expect(labels(page)).toHaveCount(0);
  await shot(page, "resources-counting");
  await hold.release();
  await expect(labels(page)).toHaveCount(7);
  await expect(counting(page)).toBeHidden();
  await shot(page, "resources-labels");
  await expect(status(page)).toHaveText("Resource amounts ready.");
  // Short amounts, an infinite resource as its yield; hidden from screen readers.
  await expect(dialog(page).locator(".resources")).toHaveAttribute("aria-hidden", "true");
  const texts = await labels(page).allTextContents();
  expect(texts).toContain("12.3M");
  expect(texts).toContain("300%");
  expect(texts).toContain("50%");
  expect(texts).toContain("3K");
  // The text list is collapsed, and names each resource with its total.
  await expect(summary(page)).toHaveText("Resource amounts as text (7 patches)");
  await expect(result(page).locator("details")).not.toHaveAttribute("open");
  await summary(page).click();
  // The totals sit under one heading for the list, a level below the settings' sections.
  await expect(result(page).getByRole("heading", { level: 3 })).toHaveText("Resource amounts as text (7 patches)");
  await expect(result(page).getByRole("heading", { level: 4 })).toHaveText([
    "Iron ore: 12,685,678 in total",
    "Copper ore: 1,503,000 in total",
    "Crude oil: 350% yield in total",
  ]);
  await expect(result(page).getByRole("listitem").first()).toHaveText("12,345,678 at (-50, -30)");
  await expect(result(page).getByRole("listitem").filter({ hasText: "yield" })).toHaveText(["300% yield at (50, -50)", "50% yield at (-60, 60)"]);
  await shot(page, "resources-text");
  await checkPage(page);
  // Kept while the page lives: closed and opened again, it is still ticked and shows at once.
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await expect(box(page)).toBeChecked();
  await expect(labels(page)).toHaveCount(7);
});

test("labels never overlap at the fitted size, and zooming in shows more", async ({ page }) => {
  await openLarge(page);
  await box(page).check();
  await expect(labels(page)).toHaveCount(7);
  const fitted = await expectNoOverlap(page);
  // Two of the large ore's patches are a few tiles apart, so not every label fits.
  expect(fitted.shown.length).toBeGreaterThan(0);
  expect(fitted.shown.length).toBeLessThan(7);
  await dialog(page).locator("#large-frame").focus();
  for (let i = 0; i < 20; i++) await page.keyboard.press("+");
  await expect.poll(async () => (await layout(page)).shown.length).toBeGreaterThan(fitted.shown.length);
  await expectNoOverlap(page);
  await shot(page, "resources-zoomed");
  // Back at the fitted size, the same few again.
  await page.keyboard.press("0");
  await expect.poll(async () => (await layout(page)).shown.length).toBe(fitted.shown.length);
  await expectNoOverlap(page);
});

test("unticking clears the status line's news of the count", async ({ page }) => {
  await openLarge(page);
  await box(page).check();
  await expect(status(page)).toHaveText("Resource amounts ready.");
  await box(page).uncheck();
  await expect(status(page)).toBeEmpty();
  await expect(labels(page)).toHaveCount(0);
});

test("a tick during the pause after a settings change waits for the new picture", async ({ page }) => {
  test.skip(!isWide(page), "the settings show beside the map only where both fit");
  await openLarge(page);
  const events: string[] = [];
  page.on("request", (r) => { if (isResources(r.url())) events.push("count"); });
  page.on("response", (r) => { if (r.url().endsWith("/generate/preview")) events.push("picture"); });
  await page.locator("#large-settings .step__range").first().focus();
  await page.keyboard.press("ArrowLeft");
  await box(page).check();
  await expect.poll(() => events.includes("count")).toBe(true);
  expect(events.indexOf("picture")).toBeGreaterThanOrEqual(0);
  expect(events.indexOf("picture")).toBeLessThan(events.indexOf("count"));
  await expect(labels(page)).toHaveCount(7);
});

test("Preview map clicked during a change's pause leaves the box working", async ({ page }) => {
  const asked: string[] = [];
  page.on("request", (r) => { if (isResources(r.url())) asked.push(r.url()); });
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  // The change starts the pause; Preview map, at once, drops its draw and draws every planet.
  await page.locator("#generate-seed").fill("4242");
  await page.getByRole("button", { name: "Preview map" }).click();
  await expect(section(page).getByRole("img")).toHaveCount(5);
  await expect(section(page).getByRole("img").first()).toHaveAccessibleName(/seed 4242/);
  await section(page).getByRole("link", { name: /opens the large view/ }).first().click();
  await expect(dialog(page)).toBeVisible();
  await box(page).check();
  await expect.poll(() => asked.length).toBe(1);
  await expect(labels(page)).toHaveCount(7);
});

test("unticking or closing the view drops the count on its way", async ({ page }) => {
  await openLarge(page);
  const hold = holdCounts(page);
  await hold.start();
  const aborted: string[] = [];
  page.on("requestfailed", (r) => { if (isResources(r.url())) aborted.push(r.url()); });
  await box(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  await box(page).uncheck();
  await expect.poll(() => aborted.length).toBe(1);
  await expect(counting(page)).toBeHidden();
  await box(page).check();
  await expect.poll(() => hold.held.length).toBe(2);
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  await expect.poll(() => aborted.length).toBe(2);
  await hold.release();
  await expect(labels(page)).toHaveCount(0);
});

test("another planet drops the count and counts the planet shown, around its demolisher labels", async ({ page }) => {
  // Vulcanus's territories are ticked on the page first, so its large view has demolisher labels.
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
  await page.getByLabel("Show demolisher territories").check();
  await expect(section(page).locator(".territory-counts")).toHaveText(/^Demolisher territories in this picture/);
  await page.locator("#mg-resources-nauvis summary").click();
  await expect(section(page).getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
  const hold = holdCounts(page);
  await hold.start();
  const aborted: string[] = [];
  page.on("requestfailed", (r) => { if (isResources(r.url())) aborted.push(r.url()); });
  await box(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  await dialog(page).locator("#large-frame").focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  await expect.poll(() => aborted.length).toBe(1);
  // Vulcanus is counted once its picture is there.
  await expect.poll(() => hold.held.length).toBe(2);
  expect(planetOf(hold.held[1].request())).toBe("vulcanus");
  // The planet's name does not hide that a count began (SPEC 17).
  await expect(status(page)).toHaveText("Vulcanus. Counting resources in the map… This can take up to a minute.");
  await hold.release();
  await expect(labels(page)).toHaveCount(7);
  await expect(dialog(page).locator(".territories__label")).toHaveCount(3);
  // A demolisher label is always kept: one put over the largest amount's label hides that label.
  const covered = await dialog(page).locator(".map").evaluate((map) => {
    const label = map.querySelector<HTMLElement>(".resources__label:not([hidden])")!;
    const terr = map.querySelector<HTMLElement>(".territories__label")!;
    for (const p of ["left", "top", "--x", "--y"]) terr.style.setProperty(p, label.style.getPropertyValue(p));
    return [...map.querySelectorAll(".resources__label")].indexOf(label);
  });
  await dialog(page).locator("#large-frame").focus();
  // A zoom places the labels again; each step lands before the next, so the map's size changes.
  await page.keyboard.press("+");
  await page.waitForFunction(() => document.getElementById("large-frame")!.hasAttribute("data-zoomed"));
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  await page.keyboard.press("0");
  await expect(labels(page).nth(covered)).toBeHidden();
  const l = await expectNoOverlap(page);
  expect(l.kept.length).toBeGreaterThan(1);
  // Named as the planet's own map setting is.
  await summary(page).click();
  await expect(result(page).getByRole("heading", { level: 4 }).first()).toHaveText(/^Coal: /);
});

test("a settings change drops the labels at once and counts again after the new picture", async ({ page }) => {
  test.skip(!isWide(page), "the settings show beside the map only where both fit");
  await openLarge(page);
  await box(page).check();
  await expect(labels(page)).toHaveCount(7);
  const asked: Request[] = [];
  page.on("request", (r) => { if (isResources(r.url())) asked.push(r); });
  const draws: string[] = [];
  page.on("response", (r) => { if (r.url().endsWith("/generate/preview")) draws.push(r.url()); });
  const hold = holdCounts(page);
  await hold.start();
  // On a wide view the setting is a slider beside the map.
  await page.locator("#large-settings .step__range").first().focus();
  await page.keyboard.press("ArrowLeft");
  await expect(labels(page)).toHaveCount(0);
  await expect(result(page)).toBeEmpty();
  // Asked only once the redraw's picture has come.
  await expect.poll(() => asked.length).toBe(1);
  expect(draws.length).toBeGreaterThan(0);
  // The picture's "ready" does not hide that a count began (SPEC 17): both are said.
  await expect(status(page)).toHaveText(/^Map preview ready\. Counting resources in the map… This can take up to a minute\.$/);
  await hold.release();
  await expect(labels(page)).toHaveCount(7);
  await expect(status(page)).toHaveText("Resource amounts ready.");
});

test("a failed request says so and offers to try again", async ({ page }) => {
  await openLarge(page);
  await page.route(resourceURL, (r) => r.fulfill({ status: 500, body: "" }));
  await box(page).check();
  await expect(result(page)).toContainText("The request for the resource amounts failed.");
  // Said once, unseen, as the message shows it: the sentence alone, without the button's label.
  const news = page.locator("#large-amounts-news");
  await expect(news).toHaveText("Error: The request for the resource amounts failed. Check that Foreman is reachable and that you are still signed in, then try again.");
  await expect(news).toHaveAttribute("role", "status");
  await expect(status(page)).not.toContainText("The request for the resource amounts failed.");
  await expect(counting(page)).toBeHidden();
  await expect(labels(page)).toHaveCount(0);
  await shot(page, "resources-failed");
  await checkPage(page);
  await page.unroute(resourceURL);
  await result(page).getByRole("button", { name: "Try again" }).click();
  await expect(labels(page)).toHaveCount(7);
  await expect(box(page)).toBeFocused();
  await expect(result(page).getByRole("button", { name: "Try again" })).toHaveCount(0);
});

test("from the keyboard: tick the box, then read the amounts as text", async ({ page }) => {
  await openLarge(page);
  await tabTo(page, box(page));
  await page.keyboard.press("Space");
  await expect(box(page)).toBeChecked();
  await expect(status(page)).toHaveText("Resource amounts ready.");
  await tabTo(page, summary(page));
  await page.keyboard.press("Enter");
  await expect(result(page).locator("details")).toHaveAttribute("open");
  await expect(result(page).getByRole("listitem")).toHaveCount(7);
  await expect(result(page).getByText("Positions are in tiles from where players start, as (x, y).")).toBeVisible();
  // Another planet empties the list; focus goes back to the box rather than being lost.
  await summary(page).focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  await expect(box(page)).toBeFocused();
  await expect(labels(page)).toHaveCount(7);
  // Focus stays inside the view, which still closes with Escape.
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
});

// SPEC 5.6, 17: the same box on the One planet picture. Off on load, kept
// while the page lives, its own choice rather than the large view's; the
// same count, wait, failure and labels, on the picture shown on the page.
test.describe("on the One planet picture", () => {
  const pageBox = (page: Page) => section(page).getByRole("checkbox", { name: "Show resource amounts" });
  const pageLabels = (page: Page) => section(page).locator(".resources__label");
  const pageResult = (page: Page) => page.locator("#preview-amounts-result");
  const announcer = (page: Page) => page.locator("#announcer");

  async function onePlanet(page: Page) {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
  }

  test("off on load, and only on the One planet view", async ({ page }) => {
    const asked: string[] = [];
    page.on("request", (r) => { if (isResources(r.url())) asked.push(r.url()); });
    await onePlanet(page);
    await expect(pageBox(page)).toBeVisible();
    await expect(pageBox(page)).not.toBeChecked();
    await expect(pageBox(page)).not.toHaveAttribute("name");
    await page.waitForTimeout(300);
    expect(asked).toEqual([]);
    await section(page).locator("label.tab", { hasText: "All planets" }).click();
    await expect(section(page).getByRole("img")).toHaveCount(5);
    await expect(pageBox(page)).toHaveCount(0);
  });

  test("ticked, the planet shown is counted after its picture and labelled there", async ({ page }) => {
    await onePlanet(page);
    const hold = holdCounts(page);
    await hold.start();
    await pageBox(page).check();
    await expect.poll(() => hold.held.length).toBe(1);
    expect(planetOf(hold.held[0].request())).toBe("nauvis");
    await expect(page.locator("#preview-counting")).toBeVisible();
    await expect(page.locator("#preview-counting")).toContainText("Counting resources in the map… This can take up to a minute.");
    await expect(announcer(page)).toHaveText("Counting resources in the map… This can take up to a minute.");
    await hold.release();
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(page.locator("#preview-counting")).toBeHidden();
    await expect(announcer(page)).toHaveText("Resource amounts ready.");
    await expect(section(page).locator(".resources")).toHaveAttribute("aria-hidden", "true");
    expect(await pageLabels(page).allTextContents()).toContain("12.3M");
    await expect(pageResult(page).locator("summary")).toHaveText("Resource amounts as text (7 patches)");
    // Labels never overlap one another or the pin on the small picture either.
    const l = await section(page).locator("a.picture .map").evaluate((map) => {
      const box = (e: Element) => { const r = e.getBoundingClientRect(); return { x0: r.left, y0: r.top, x1: r.right, y1: r.bottom }; };
      return { shown: [...map.querySelectorAll(".resources__label:not([hidden])")].map(box), kept: [...map.querySelectorAll(".pin")].map(box) };
    });
    expect(l.shown.length).toBeGreaterThan(0);
    for (const [i, a] of l.shown.entries()) for (const b of [...l.shown.slice(i + 1), ...l.kept]) expect(meet(a, b)).toBe(false);
    await shot(page, "resources-one-planet");
    await checkPage(page);
    // Another planet's picture keeps the box ticked and counts that planet.
    await page.locator("#mg-resources-vulcanus summary").click();
    await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
    await expect(pageBox(page)).toBeChecked();
    await expect(pageLabels(page)).toHaveCount(7);
  });

  test("the large view's box is its own; the page's labels, list and count stay while it is open", async ({ page }) => {
    const asked: string[] = [];
    page.on("request", (r) => { if (isResources(r.url())) asked.push(r.url()); });
    await onePlanet(page);
    await pageBox(page).check();
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(announcer(page)).toHaveText("Resource amounts ready.");
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    await expect(box(page)).not.toBeChecked();
    await expect(labels(page)).toHaveCount(0);
    await expect(pageLabels(page)).toHaveCount(7);
    // Ticked and unticked there, only the view's own amounts come and go.
    await box(page).check();
    await expect(labels(page)).toHaveCount(7);
    await box(page).uncheck();
    await expect(labels(page)).toHaveCount(0);
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(pageResult(page).locator("summary")).toHaveText("Resource amounts as text (7 patches)");
    await box(page).check();
    await expect(labels(page)).toHaveCount(7);
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    // Closed, the page's are still there: nothing is asked again or said again.
    await expect(pageBox(page)).toBeChecked();
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(pageResult(page).locator("summary")).toHaveText("Resource amounts as text (7 patches)");
    await page.waitForTimeout(300);
    expect(asked.length).toBe(3);
    await expect(announcer(page)).toHaveText("Resource amounts ready.");
  });

  test("a page count that lands while the view is open is shown, not said there", async ({ page }) => {
    await onePlanet(page);
    const hold = holdCounts(page);
    await hold.start();
    await pageBox(page).check();
    await expect.poll(() => hold.held.length).toBe(1);
    await expect(announcer(page)).toHaveText("Counting resources in the map… This can take up to a minute.");
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    await status(page).evaluate((s) => { s.textContent = ""; });
    await hold.release();
    await expect(pageLabels(page)).toHaveCount(7);
    await page.waitForTimeout(200);
    // The view's status line never reads the page's news.
    await expect(status(page)).toBeEmpty();
    await expect(announcer(page)).toHaveText("Counting resources in the map… This can take up to a minute.");
    await announcer(page).evaluate((s) => {
      const said: string[] = [];
      (window as unknown as { said: string[] }).said = said;
      new MutationObserver(() => { if (s.textContent) said.push(s.textContent); }).observe(s, { childList: true, characterData: true, subtree: true });
    });
    await page.keyboard.press("Escape");
    await expect(pageResult(page).locator("summary")).toHaveText("Resource amounts as text (7 patches)");
    // Back on the page, it is said once there, as the page could not say it while the view was open.
    await expect(announcer(page)).toHaveText("Resource amounts ready.");
    await page.waitForTimeout(300);
    const said = await page.evaluate(() => (window as unknown as { said: string[] }).said);
    expect(said.filter((t) => t === "Resource amounts ready.")).toHaveLength(1);
    expect(hold.held.length).toBe(0);
  });

  test("a page failure while the view is open is shown, then said once the view closes", async ({ page }) => {
    await onePlanet(page);
    let fail!: () => void;
    const go = new Promise<void>((r) => (fail = r));
    await page.route(resourceURL, async (r) => { await go; await r.fulfill({ status: 500, body: "" }); });
    await pageBox(page).check();
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    fail();
    await expect(pageResult(page)).toContainText("The request for the resource amounts failed.");
    await page.waitForTimeout(200);
    await expect(page.locator("#preview-amounts-news")).toBeEmpty();
    await expect(status(page)).not.toContainText("failed");
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await expect(pageResult(page)).toContainText("The request for the resource amounts failed.");
    await expect(pageResult(page).getByRole("button", { name: "Try again" })).toBeVisible();
    // Said once on the page, the sentence alone, now that it can be heard.
    await expect(page.locator("#preview-amounts-news")).toHaveText(/^Error: The request for the resource amounts failed\./);
    await expect(announcer(page)).not.toHaveText("Resource amounts ready.");
  });

  // At 320 px the counting line is under the picture, out of sight of the box, so the box
  // says beside itself that it is counting; the line under the picture says the rest.
  test("ticking the box shows beside it that it is counting", async ({ page }) => {
    await onePlanet(page);
    const busy = page.locator("#preview-amounts-busy");
    await expect(busy).toBeHidden();
    const hold = holdCounts(page);
    await hold.start();
    await pageBox(page).check();
    await expect.poll(() => hold.held.length).toBe(1);
    await expect(busy).toBeVisible();
    await expect(busy).toHaveText("Counting…");
    // Seen, not said: the status line already says the whole sentence once.
    await expect(busy).toHaveAttribute("aria-hidden", "true");
    // On the box's own line, in view with it.
    const b = (await pageBox(page).locator("xpath=..").boundingBox())!;
    const h = (await busy.boundingBox())!;
    expect(h.y).toBeLessThan(b.y + b.height);
    expect(h.y + h.height).toBeGreaterThan(b.y);
    expect(h.x + h.width).toBeLessThanOrEqual(page.viewportSize()!.width);
    await expect(busy).toBeInViewport();
    await shot(page, "resources-one-planet-counting");
    await hold.release();
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(busy).toBeHidden();
  });

  // DESIGN 8.8: the thumbnail's labels are the territory labels' small type, over the map only.
  test("the thumbnail's labels are small and lie over the map inside its border", async ({ page }) => {
    await onePlanet(page);
    await pageBox(page).check();
    await expect(pageLabels(page)).toHaveCount(7);
    const look = await pageLabels(page).first().evaluate((l) => { const s = getComputedStyle(l); return { size: s.fontSize, weight: s.fontWeight }; });
    expect(look).toEqual({ size: "12px", weight: "400" });
    const img = (await section(page).locator("a.picture img").boundingBox())!;
    const o = (await section(page).locator("a.picture .resources").boundingBox())!;
    expect(Math.abs(o.x - img.x - 1)).toBeLessThan(0.6);
    expect(Math.abs(o.y - img.y - 1)).toBeLessThan(0.6);
    expect(Math.abs(o.width - (img.width - 2))).toBeLessThan(0.6);
    // Forced colours drop the dark ring, so the labels keep their own colours, as territory labels do.
    await page.emulateMedia({ forcedColors: "active" });
    expect(await pageLabels(page).first().evaluate((l) => getComputedStyle(l).forcedColorAdjust)).toBe("none");
  });

  test("the picture does not move as the count comes and goes", async ({ page }) => {
    await onePlanet(page);
    const img = section(page).getByRole("img");
    // In page coordinates, as ticking the box scrolls it into view.
    const top = () => img.evaluate((i) => i.getBoundingClientRect().top + window.scrollY);
    const before = await top();
    const hold = holdCounts(page);
    await hold.start();
    await pageBox(page).check();
    await expect(page.locator("#preview-counting")).toBeVisible();
    // The bar is not named again; the sentence beside it is said.
    await expect(page.locator("#preview-counting progress")).toHaveAttribute("aria-hidden", "true");
    expect(await top()).toBeCloseTo(before, 0);
    await hold.release();
    await expect(pageLabels(page)).toHaveCount(7);
    expect(await top()).toBeCloseTo(before, 0);
  });

  test("unticking drops the count on its way; a failure says so and offers to try again", async ({ page }) => {
    await onePlanet(page);
    const hold = holdCounts(page);
    await hold.start();
    const aborted: string[] = [];
    page.on("requestfailed", (r) => { if (isResources(r.url())) aborted.push(r.url()); });
    await pageBox(page).check();
    await expect.poll(() => hold.held.length).toBe(1);
    await pageBox(page).uncheck();
    await expect.poll(() => aborted.length).toBe(1);
    await expect(page.locator("#preview-counting")).toBeHidden();
    await hold.release();
    await expect(pageLabels(page)).toHaveCount(0);
    await page.route(resourceURL, (r) => r.fulfill({ status: 500, body: "" }));
    await pageBox(page).check();
    await expect(pageResult(page)).toContainText("The request for the resource amounts failed.");
    await expect(page.locator("#preview-amounts-news")).toHaveText(/^Error: The request for the resource amounts failed\./);
    await page.unroute(resourceURL);
    await pageResult(page).getByRole("button", { name: "Try again" }).click();
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(pageBox(page)).toBeFocused();
  });
  test("downloads draw in the held amounts while the box is ticked, and never count", async ({ page }) => {
    const asked: string[] = [];
    page.on("request", (r) => { if (isResources(r.url())) asked.push(r.url()); });
    await onePlanet(page);
    const one = section(page).locator("li a.download");
    const zip = section(page).getByRole("link", { name: "Download all drawn planets (zip)" });
    await expect(one).toHaveText("Download Nauvis (PNG)");
    await pageBox(page).check();
    await expect(pageLabels(page)).toHaveCount(7);
    await expect(one).toHaveText("Download Nauvis with resource amounts (PNG)");
    await expect(one).toHaveAttribute("href", /\/nauvis\/download\?amounts=1$/);
    await expect(zip).toHaveAttribute("href", /\/all\.zip\?amounts=1$/);
    await expect(zip).toHaveText("Download all drawn planets (zip), with resource amounts where counted");
    const res = await page.request.get(await one.getAttribute("href") ?? "");
    expect(res.status()).toBe(200);
    expect(res.headers()["content-disposition"]).toContain("-nauvis-amounts.png");
    expect((await page.request.get(await zip.getAttribute("href") ?? "")).status()).toBe(200);
    // The large view's download follows its own box.
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    await expect(page.locator("#large-download")).not.toHaveAttribute("href", /amounts/);
    await expect(page.locator("#large-download-name")).toHaveText("Download Nauvis map");
    await box(page).check();
    await expect(labels(page)).toHaveCount(7);
    await expect(page.locator("#large-download")).toHaveAttribute("href", /\?amounts=1$/);
    await expect(page.locator("#large-download-name")).toHaveText("Download Nauvis map with resource amounts");
    await page.keyboard.press("Escape");
    // One count for the page and one for the view, answered from what was held; the downloads asked for none.
    expect(asked.length).toBe(2);
    await pageBox(page).uncheck();
    await expect(one).toHaveText("Download Nauvis (PNG)");
    await expect(one).not.toHaveAttribute("href", /amounts/);
    await expect(zip).not.toHaveAttribute("href", /amounts/);
    await expect(zip).toHaveText("Download all drawn planets (zip)");
  });

  test("Vulcanus's download says when it draws territories and amounts", async ({ page }) => {
    await onePlanet(page);
    await page.locator("#mg-resources-vulcanus summary").click();
    await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
    await page.getByLabel("Show demolisher territories").check();
    await expect(section(page).locator(".territory-counts")).toHaveText(/^Demolisher territories in this picture/);
    await pageBox(page).check();
    await expect(pageLabels(page)).toHaveCount(7);
    const one = section(page).locator("li a.download");
    await expect(one).toHaveText("Download Vulcanus with territories and resource amounts (PNG)");
    const res = await page.request.get(await one.getAttribute("href") ?? "");
    expect(res.headers()["content-disposition"]).toContain("-vulcanus-territories-amounts.png");
    await page.getByLabel("Show demolisher territories").uncheck();
    await expect(one).toHaveText("Download Vulcanus with resource amounts (PNG)");
  });
});
