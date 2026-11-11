import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import type { Download, Page, Request, Route } from "@playwright/test";
import { readFileSync } from "node:fs";

// SPEC 5.6, "Downloads"; 17.15: while a box is ticked, every download
// carries what it asks for. Before it starts, the page counts (or reads the
// territories of) each planet it holds that has nothing held yet, one at a
// time, saying which planet of how many, with Cancel. A planet that fails
// is named, with Try again and, for the zip, the zip without it. What is
// counted is kept, so the page and later downloads reuse it.
test.use({ fakeGameOn: true });

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const pageBox = (page: Page) => section(page).getByRole("checkbox", { name: "Show resource amounts" });
const pageLabels = (page: Page) => section(page).locator(".resources__label");
const zip = (page: Page) => section(page).getByRole("link", { name: /^Download all drawn planets \(zip\)/ });
const prep = (page: Page) => page.locator(".download-prep");
const announcer = (page: Page) => page.locator("#announcer");
const resourceURL = (u: URL) => u.pathname.endsWith("/generate/preview/resources");
const isResources = (url: string) => new URL(url).pathname.endsWith("/generate/preview/resources");
const planetOf = (r: Request) => new URLSearchParams(r.postData() ?? "").get("planet");

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

// Nauvis, Vulcanus and Gleba drawn, one at a time, so the zip holds three
// planets; Gleba is shown and counted.
async function threeDrawn(page: Page, asked: string[]) {
  page.on("request", (r) => { if (isResources(r.url())) asked.push(planetOf(r) ?? ""); });
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  for (const planet of ["vulcanus", "gleba"]) {
    await page.locator(`#mg-resources-${planet} summary`).click();
    await expect(section(page).locator(`a.picture[data-planet="${planet}"]`)).toBeVisible();
  }
  await expect(zip(page)).toHaveAttribute("data-planets", "nauvis vulcanus gleba");
  await pageBox(page).check();
  await expect(pageLabels(page)).toHaveCount(4);
  await expect(zip(page)).toHaveText("Download all drawn planets (zip), with resource amounts");
  await expect(zip(page)).toHaveAttribute("href", /\/all\.zip\?amounts=1$/);
}

function names(d: Download) {
  return d.path().then((p) => {
    // A stored zip's local headers name each file; enough to list them.
    const buf = readFileSync(p);
    const out: string[] = [];
    for (let i = buf.indexOf("PK\x03\x04"); i >= 0; i = buf.indexOf("PK\x03\x04", i + 4)) {
      const len = buf.readUInt16LE(i + 26);
      out.push(buf.subarray(i + 30, i + 30 + len).toString());
    }
    return out;
  });
}

test("the zip counts each planet without amounts, one at a time, then downloads them all", async ({ page }) => {
  const asked: string[] = [];
  await threeDrawn(page, asked);
  expect(asked).toEqual(["gleba"]);
  const held: Route[] = [];
  await page.route(resourceURL, (r) => { held.push(r); });
  const download = page.waitForEvent("download");
  await zip(page).click();
  await expect(prep(page).locator("[data-prep-text]")).toHaveText("Counting resources for the download, planet 1 of 2: Nauvis…");
  await expect(announcer(page)).toHaveText(/planet 1 of 2: Nauvis/);
  await expect(prep(page).getByRole("button", { name: "Cancel" })).toBeVisible();
  await expect.poll(() => held.length).toBe(1);
  await held.shift()!.continue();
  await expect(prep(page).locator("[data-prep-text]")).toHaveText("Counting resources for the download, planet 2 of 2: Vulcanus…");
  await expect.poll(() => held.length).toBe(1);
  await held.shift()!.continue();
  await page.unroute(resourceURL);
  const files = await names(await download);
  expect(files.map((f) => f.replace(/^.*-(\w+-amounts\.png)$/, "$1")).sort()).toEqual(["gleba-amounts.png", "nauvis-amounts.png", "vulcanus-amounts.png"]);
  await expect(prep(page)).toHaveCount(0);
  // One count a planet: the second zip, and the page going back to Nauvis, reuse them.
  expect(asked).toEqual(["gleba", "nauvis", "vulcanus"]);
  const again = page.waitForEvent("download");
  await zip(page).click();
  await again;
  await expect(prep(page)).toHaveCount(0);
  expect(asked).toEqual(["gleba", "nauvis", "vulcanus"]);
});

test("Cancel stops the counting, says so, and puts focus back on the download", async ({ page }) => {
  const asked: string[] = [];
  await threeDrawn(page, asked);
  const aborted: string[] = [];
  page.on("requestfailed", (r) => { if (isResources(r.url())) aborted.push(planetOf(r) ?? ""); });
  const held: Route[] = [];
  await page.route(resourceURL, (r) => { held.push(r); });
  let downloads = 0;
  page.on("download", () => { downloads++; });
  await zip(page).focus();
  await page.keyboard.press("Enter");
  await expect.poll(() => held.length).toBe(1);
  await page.keyboard.press("Tab");
  await expect(prep(page).getByRole("button", { name: "Cancel" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(prep(page)).toHaveCount(0);
  await expect.poll(() => aborted).toEqual(["nauvis"]);
  await expect(zip(page)).toBeFocused();
  await expect(announcer(page)).toHaveText("Download canceled.");
  await page.unroute(resourceURL);
  expect(downloads).toBe(0);
});

test("a planet that fails is named, with Try again and the zip without it", async ({ page }) => {
  const asked: string[] = [];
  await threeDrawn(page, asked);
  await page.route(resourceURL, (r) => (planetOf(r.request()) === "nauvis" ? r.fulfill({ status: 500, body: "" }) : r.continue()));
  await zip(page).click();
  const failed = prep(page).locator(".message");
  await expect(failed).toHaveText(/^Error: The download stopped at Nauvis\. The request for the resource amounts failed\./);
  await expect(announcer(page)).toHaveText(/^Error: The download stopped at Nauvis\./);
  const without = prep(page).getByRole("button", { name: "Download without Nauvis" });
  const retry = prep(page).getByRole("button", { name: "Try again" });
  await expect(without).toBeVisible();
  await expect(retry).toBeVisible();
  await checkPage(page);
  // Try again counts Nauvis again, and stops there again.
  await retry.click();
  await expect(failed).toBeVisible();
  expect(asked.filter((p) => p === "nauvis").length).toBe(2);
  // Without Nauvis: the rest are counted and the zip leaves Nauvis out.
  const download = page.waitForEvent("download");
  await without.focus();
  await page.keyboard.press("Enter");
  const d = await download;
  expect(d.url()).toMatch(/without=nauvis/);
  const files = await names(d);
  expect(files.length).toBe(2);
  expect(files.every((f) => f.endsWith("-amounts.png") && !f.includes("nauvis"))).toBe(true);
  await page.unroute(resourceURL);
});

test("the planet's own download waits for its count on the way, and counts after a failure", async ({ page }) => {
  const asked: string[] = [];
  page.on("request", (r) => { if (isResources(r.url())) asked.push(planetOf(r) ?? ""); });
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  const one = section(page).locator("li a.download");
  await page.route(resourceURL, (r) => r.fulfill({ status: 500, body: "" }));
  await pageBox(page).check();
  await expect(page.locator("#preview-amounts-result")).toContainText("The request for the resource amounts failed.");
  await page.unroute(resourceURL);
  // Ticked, the download asks for the amounts, held or not, and says so.
  await expect(one).toHaveText("Download Nauvis with resource amounts (PNG)");
  await expect(one).toHaveAttribute("href", /\/nauvis\/download\?amounts=1$/);
  const download = page.waitForEvent("download");
  await one.click();
  const d = await download;
  expect(d.suggestedFilename()).toMatch(/-nauvis-amounts\.png$/);
  // The failed count, then the download's own; no zip-only choice for one planet.
  expect(asked).toEqual(["nauvis", "nauvis"]);
  await expect(prep(page)).toHaveCount(0);
});

test("with territories ticked and none held, the zip reads them before it downloads", async ({ page }) => {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).locator('a.picture[data-planet="vulcanus"]')).toBeVisible();
  const terrURL = (u: URL) => u.pathname.endsWith("/generate/preview/territories");
  await page.route(terrURL, (r) => r.fulfill({ status: 500, body: "" }));
  await page.getByLabel("Show demolisher territories").check();
  await expect(section(page).locator("#territory-result")).toContainText("The request for the demolisher territories failed.");
  await page.unroute(terrURL);
  // The box is ticked, so the zip asks for them though none are held yet.
  await expect(zip(page)).toHaveAttribute("href", /\/all\.zip\?territories=1$/);
  const download = page.waitForEvent("download");
  await zip(page).click();
  const files = await names(await download);
  expect(files.some((f) => f.endsWith("-vulcanus-territories.png"))).toBe(true);
  await expect(section(page).locator("a.picture .territories")).toHaveCount(1);
});

// Records every text a live region shows from now on, as a screen reader
// hears each one even when newer news replaces it moments later.
async function listen(page: Page, selector: string) {
  await page.locator(selector).evaluate((n) => {
    const w = window as unknown as { heard: string[] };
    w.heard = [];
    new MutationObserver(() => { if (n.textContent) w.heard.push(n.textContent); }).observe(n, { childList: true, characterData: true, subtree: true });
  });
  return () => page.evaluate(() => (window as unknown as { heard: string[] }).heard.join(" | "));
}

// True when the control, with room for its focus ring, is on top at its middle and edges.
function uncovered(page: Page, selector: string) {
  return page.locator(selector).evaluate((n) => {
    const r = n.getBoundingClientRect();
    const ring = 3;
    const points = [[r.left + r.width / 2, r.top + r.height / 2], [r.left - ring, r.top + r.height / 2], [r.right + ring, r.top + r.height / 2], [r.left + r.width / 2, r.top - ring], [r.left + r.width / 2, r.bottom + ring]];
    return points.every(([x, y]) => { const at = document.elementFromPoint(x, y); return !at || !at.closest(".download-prep"); });
  });
}

// Holds every count request until let go, one at a time, in order.
function holdAll(page: Page) {
  const held: Route[] = [];
  return {
    held,
    start: () => page.route(resourceURL, (r) => { held.push(r); }),
    next: async () => { await expect.poll(() => held.length).toBeGreaterThan(0); await held.shift()!.continue(); },
    stop: () => page.unroute(resourceURL),
  };
}

test("ticked then Download all at once: the zip waits for the count on its way, so nothing is busy", async ({ page }) => {
  const asked: string[] = [];
  page.on("request", (r) => { if (isResources(r.url())) asked.push(planetOf(r) ?? ""); });
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).locator('a.picture[data-planet="vulcanus"]')).toBeVisible();
  const hold = holdAll(page);
  await hold.start();
  await pageBox(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  const download = page.waitForEvent("download");
  await zip(page).click();
  await expect(prep(page)).toBeVisible();
  // The waiting state passes the scan, and nothing more is sent meanwhile.
  await checkPage(page);
  await page.waitForTimeout(300);
  expect(hold.held.length).toBe(1);
  await hold.next();
  await hold.next();
  await hold.stop();
  const files = await names(await download);
  expect(files.length).toBe(2);
  await expect(prep(page).locator(".message")).toHaveCount(0);
  expect(asked).toEqual(["vulcanus", "nauvis"]);
});

test("the planet's own download waits for the count already on its way", async ({ page }) => {
  const asked: string[] = [];
  page.on("request", (r) => { if (isResources(r.url())) asked.push(planetOf(r) ?? ""); });
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  const hold = holdAll(page);
  await hold.start();
  await pageBox(page).check();
  await expect.poll(() => hold.held.length).toBe(1);
  const download = page.waitForEvent("download");
  await section(page).locator("li a.download").click();
  await expect(prep(page).locator("[data-prep-text]")).toHaveText("Counting resources for the download, planet 1 of 1: Nauvis…");
  await page.waitForTimeout(300);
  expect(hold.held.length).toBe(1);
  await hold.next();
  await hold.stop();
  expect((await download).suggestedFilename()).toMatch(/-nauvis-amounts\.png$/);
  // The download found the count held and asked for none of its own.
  expect(asked).toEqual(["nauvis"]);
});

test("opening the large view during the preparation does not count a planet twice at once", async ({ page }) => {
  const asked: string[] = [];
  await threeDrawn(page, asked);
  const hold = holdAll(page);
  await hold.start();
  const download = page.waitForEvent("download");
  await zip(page).click();
  await expect.poll(() => hold.held.length).toBe(1);
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await page.locator("#large-view").getByRole("checkbox", { name: "Show resource amounts" }).check();
  await page.waitForTimeout(300);
  expect(hold.held.length).toBe(1);
  await hold.next();
  await hold.next();
  await hold.next();
  await hold.stop();
  await download;
  // One request at a time, each planet once; the order of the last two may vary.
  expect(asked.slice(1).sort()).toEqual(["gleba", "nauvis", "vulcanus"]);
});

test("another download while one is prepared says so; a settings change ends it and says so", async ({ page }) => {
  const asked: string[] = [];
  await threeDrawn(page, asked);
  const hold = holdAll(page);
  await hold.start();
  await zip(page).click();
  await expect.poll(() => hold.held.length).toBe(1);
  await section(page).locator("li a.download").click();
  await expect(announcer(page)).toHaveText("Another download is being prepared. Wait for it, or cancel it first.");
  await page.locator("#generate-seed").fill("4242");
  await expect(prep(page)).toHaveCount(0);
  await expect(announcer(page)).toHaveText(/Download canceled\./);
  await hold.stop();
});

test("in the large view the preparation lies over the stage without moving it, and closing or another planet ends it", async ({ page }) => {
  const asked: string[] = [];
  await threeDrawn(page, asked);
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  const view = page.locator("#large-view");
  await expect(view).toBeVisible();
  await page.route(resourceURL, (r) => r.fulfill({ status: 500, body: "" }));
  // Gleba is counted already; the view's count of Nauvis fails, so its download counts first.
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator("#large-title")).toHaveText("Nauvis");
  await view.getByRole("checkbox", { name: "Show resource amounts" }).check();
  await expect(page.locator("#large-amounts-result")).toContainText("The request for the resource amounts failed.");
  await page.unroute(resourceURL);
  const hold = holdAll(page);
  await hold.start();
  const stage = await page.locator("#large-frame").boundingBox();
  await page.locator("#large-download").click();
  await expect(view.locator(".large-view__stage .download-prep")).toBeVisible();
  await expect.poll(() => hold.held.length).toBe(1);
  expect(await page.locator("#large-frame").boundingBox()).toEqual(stage);
  await checkPage(page);
  const heard = await listen(page, "#large-status");
  await page.keyboard.press("ArrowRight");
  await expect(view.locator(".download-prep")).toHaveCount(0);
  await expect.poll(heard).toMatch(/Download canceled\./);
  await page.keyboard.press("ArrowLeft");
  await page.locator("#large-download").click();
  await expect(view.locator(".download-prep")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(view).toBeHidden();
  await expect(prep(page)).toHaveCount(0);
  await expect(announcer(page)).toHaveText(/Download canceled\./);
  await hold.stop();
});

test("in the large view the preparation leaves the arrows and zoom free, and an undrawn planet still says it ended", async ({ page }) => {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).locator('a.picture[data-planet="vulcanus"]')).toBeVisible();
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  const view = page.locator("#large-view");
  await expect(page.locator("#large-title")).toHaveText("Vulcanus");
  await page.route(resourceURL, (r) => r.fulfill({ status: 500, body: "" }));
  await view.getByRole("checkbox", { name: "Show resource amounts" }).check();
  await expect(page.locator("#large-amounts-result")).toContainText("The request for the resource amounts failed.");
  await page.unroute(resourceURL);
  const hold = holdAll(page);
  await hold.start();
  await page.locator("#large-download").click();
  const block = view.locator(".large-view__stage .download-prep");
  await expect(block).toBeVisible();
  // Cancel comes first in the stage, before the picture and its controls.
  await expect(view.locator(".large-view__stage > *").first()).toHaveClass(/download-prep/);
  // Each control is what lies under its own middle, so nothing covers it or its focus ring (#large-out is aria-disabled at zoom 1, so a trial click would wait).
  for (const id of ["#large-prev", "#large-out", "#large-next", "#large-close"]) expect(await uncovered(page, id), id).toBe(true);
  await expect.poll(() => hold.held.length).toBe(1);
  await hold.held.shift()!.fulfill({ status: 500, body: "" });
  await expect(block.locator(".message")).toBeVisible();
  // Each control is what lies under its own middle, so nothing covers it or its focus ring (#large-out is aria-disabled at zoom 1, so a trial click would wait).
  for (const id of ["#large-prev", "#large-out", "#large-next", "#large-close"]) expect(await uncovered(page, id), id).toBe(true);
  // The same link again says where its preparation is.
  await page.locator("#large-download").click();
  await expect(page.locator("#large-status")).toHaveText(/The download stopped at Vulcanus\./);
  // Gleba is not drawn yet: its "Drawing" line still says the download ended.
  const heard = await listen(page, "#large-status");
  await page.keyboard.press("ArrowRight");
  await expect.poll(heard).toMatch(/Drawing Gleba….*Download canceled\./);
  await expect(block).toHaveCount(0);
  await hold.stop();
});

test("a draw that lands during a preparation ends it and says so; the next download works", async ({ page }) => {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).locator('a.picture[data-planet="vulcanus"]')).toBeVisible();
  await pageBox(page).check();
  await expect(pageLabels(page)).toHaveCount(4);
  // Hold the next draw, start one, then start a preparation while it is on its way.
  const draws: Route[] = [];
  const isDraw = (u: URL) => u.pathname === "/generate/preview";
  await page.route(isDraw, (r) => { draws.push(r); });
  await page.locator("#mg-resources-gleba summary").click();
  await expect.poll(() => draws.length).toBe(1);
  const hold = holdAll(page);
  await hold.start();
  await zip(page).click();
  await expect(prep(page)).toBeVisible();
  await page.unroute(isDraw);
  await draws.shift()!.continue();
  await expect(section(page).locator('a.picture[data-planet="gleba"]')).toBeVisible();
  await expect(prep(page)).toHaveCount(0);
  await expect(announcer(page)).toHaveText(/Download canceled\./);
  await hold.stop();
  // The next download is prepared afresh from the new section, not refused as busy.
  const download = page.waitForEvent("download");
  await section(page).locator("li a.download").click();
  expect((await download).suggestedFilename()).toMatch(/-gleba-amounts\.png$/);
});
