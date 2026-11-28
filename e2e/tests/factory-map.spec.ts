import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, live, stateBadge, tabTo } from "./foreman.js";
import type { Foreman, FakeGame } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.16 (P1), plan rules 14 to 21: a world's factory map. The map page,
// /worlds/{id}/map, is a world tab after Snapshots. It draws one picture per
// surface from a read of the newest save, has a viewer that works like the
// Generate page's large view (zoom by wheel, keys, buttons or pinch in steps
// of 1.15; pan by drag, keys or a tap), a checkbox per category that stacks
// its layer, and downloads of the base PNG or the base with the ticked layers
// combined on the server. Each world has a mode (automatic, manual, off) and
// "skip while players are online", kept in world.json, never audited, never
// snapshotted. The fake game answers the helper mod's run (task F9).
//
// What the page's markup must offer is gathered in `ui` below, because the
// viewer (task F8) and the handlers (task F7) were written in parallel with
// this file. Where a name is a guess it says so; reconcile it there, once.
test.use({ fakeGameOn: true });

// The picture's size limit (SPEC 5.16): at most 4096 pixels a side.
const maxSide = 4096;
const pngSignature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

// The categories in the page's order (SPEC 5.16, internal/factorymap/types.go).
const categories = ["Belts and inserters", "Production", "Power", "Fluids", "Rail", "Defense", "Logistic network and storage", "Other"];

const ui = {
  // The scrolling frame holding the pictures; the one place focus lands to use keys.
  frame: (p: Page) => p.locator("#map-frame"),
  // The state region (the plan's page contract); GET ?fragment=1 serves it alone, and while a run is going
  // the page polls it every 3 seconds, so a change is waited for with the 20-second live timeout.
  state: (p: Page) => p.locator("#factorymap-state"),
  // The base picture is /worlds/{id}/map/{surface}.png with no layer; each ticked layer is the same with
  // ?layer=<category> (the plan's page contract). ASSUMED: they are img elements in the frame.
  base: (p: Page) => p.locator('#map-frame img[src^="/worlds/"]:not([src*="layer="])').first(),
  // Every category has its img in the frame, hidden until ticked; a ticked one is shown.
  layers: (p: Page) => p.locator("#map-frame img.factorymap__layer:not([hidden])"),
  // The zoom level is said here, as the large view says "Zoom 115%" (ASSUMED id).
  status: (p: Page) => p.locator("#map-status"),
  zoomIn: (p: Page) => p.getByRole("button", { name: "Zoom in" }),
  zoomOut: (p: Page) => p.getByRole("button", { name: "Zoom out" }),
  // The list of surfaces, one control each, naming the surface (ASSUMED id).
  surfaces: (p: Page) => p.locator("#map-surfaces"),
  // A checkbox per category, named for it.
  layer: (p: Page, name: string) => p.getByRole("checkbox", { name, exact: true }),
  draw: (p: Page) => p.getByRole("button", { name: /^Draw factory map/ }),
  // The download link is /worlds/{id}/map/{surface}/download, with ?layer=a&layer=b for the ticked layers
  // (the plan's page contract). ASSUMED: one link whose href the viewer rewrites as boxes are ticked.
  download: (p: Page) => p.locator("#map-download"),
  fit: (p: Page) => p.getByRole("button", { name: "Fit" }),
  // With a game running, a click on the draw button brings the memory question, and its confirm button
  // REPLACES the draw button (SPEC 5.16, rule 21).
  confirm: (p: Page) => p.getByRole("button", { name: "Draw it beside the running game" }),
  // The mode form: three radios in a group named "Mode" (ASSUMED), a checkbox, and its save button.
  // The mode form posts to /worlds/{id}/map/mode with mode and skip_online.
  mode: (p: Page, name: "Automatic" | "Manual" | "Off") => p.getByRole("radio", { name: new RegExp(`^${name}`) }),
  skipOnline: (p: Page) => p.getByRole("checkbox", { name: /players are online/i }),
  saveMode: (p: Page) => p.getByRole("button", { name: /^Save/ }),
};

const base = "/worlds/base/map";
const container = "foreman-base";
const worldJson = (f: Foreman) => join(f.dataDir, "worlds", "base", "world.json");
const factoryMapOf = (f: Foreman) => JSON.parse(readFileSync(worldJson(f), "utf8")).factory_map;
const factoryMap = (f: Foreman) => JSON.parse(readFileSync(worldJson(f), "utf8")).factory_map;
const snapshotCount = (f: Foreman) => {
  const dir = join(f.dataDir, "snapshots", "base");
  return existsSync(dir) ? readdirSync(dir).filter((n) => /^\d{8}T/.test(n)).length : 0;
};

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  // A mod list is always written, as the game's pre-start check expects (the map run checks the mods first).
  seedWorld(foreman, { mods: {} });
  await fakeGame!.call("/image", { ref: image21, on: true });
});

// A drawing made: the click, and the picture that arrives with no reload.
async function draw(page: Page, opts: { running?: boolean } = {}) {
  await ui.draw(page).click();
  if (opts.running) await confirmMemory(page);
  await expect(ui.base(page)).toBeVisible(live);
  await expect.poll(() => ui.base(page).evaluate((i) => (i as HTMLImageElement).naturalWidth), live).toBeGreaterThan(0);
}

// Beside a running game the draw button's click brings the memory question, with its size, and a confirm
// button that replaces the draw button (SPEC 5.16, rule 21). The answer posts confirm_memory=1.
async function confirmMemory(page: Page) {
  await expect(ui.confirm(page)).toBeVisible();
  await expect(page.locator("#fm-memory-note")).toContainText(/memory/i);
  await expect(ui.draw(page)).toHaveCount(0);
  await ui.confirm(page).click();
}

// The surface index in a picture's address.
const surfaceOf = async (page: Page) => /\/map\/(\d+)\.png/.exec((await ui.base(page).getAttribute("src"))!)![1];

async function pngOf(page: Page, link: Locator) {
  const [download] = await Promise.all([page.waitForEvent("download"), link.click()]);
  const path = await download.path();
  const bytes = readFileSync(path);
  return { name: download.suggestedFilename(), bytes, w: bytes.readUInt32BE(16), h: bytes.readUInt32BE(20) };
}

const width = (page: Page) => ui.base(page).evaluate((i) => i.getBoundingClientRect().width);
const scroll = (page: Page) => ui.frame(page).evaluate((f) => ({ x: f.scrollLeft, y: f.scrollTop }));
// The middle of the part of the frame in the window: on a phone the frame can reach below the fold.
async function middleOf(page: Page) {
  await ui.frame(page).scrollIntoViewIfNeeded();
  const box = (await ui.frame(page).boundingBox())!;
  const top = Math.max(box.y, 0);
  const bottom = Math.min(box.y + box.height, page.viewportSize()!.height);
  return { x: box.x + box.width / 2, y: (top + bottom) / 2 };
}

async function startWorld(page: Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
}

// ---------------------------------------------------------------- the tab

test("the Factory map tab follows Snapshots and opens the page, which is accessible", { tag: "@looks" }, async ({ page }) => {
  await page.goto("/worlds/base");
  const tabs = page.getByRole("navigation", { name: "World pages" });
  const labels = await tabs.getByRole("link").allTextContents();
  const at = labels.map((l) => l.trim());
  expect(at).toContain("Factory map");
  expect(at.indexOf("Factory map")).toBeGreaterThan(at.indexOf("Snapshots"));
  await tabs.getByRole("link", { name: "Factory map" }).click();
  await expect(page).toHaveURL(base);
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Base");
  await expect(tabs.getByRole("link", { name: "Factory map" })).toHaveAttribute("aria-current", /page|true/);
  await checkPage(page);
});

test("a world with nothing drawn says so, defaults to manual, and offers the button", async ({ page, foreman }) => {
  await page.goto(base);
  await expect(ui.draw(page)).toBeEnabled();
  await expect(ui.base(page)).toHaveCount(0);
  await expect(ui.mode(page, "Manual")).toBeChecked();
  await expect(ui.skipOnline(page)).toBeChecked();
  // Only an explicit change writes the setting (SPEC 5.16: the default is manual, new or existing).
  expect(JSON.parse(readFileSync(worldJson(foreman), "utf8")).factory_map ?? { mode: "manual" }).toMatchObject({ mode: "manual" });
  await checkPage(page);
});

// ---------------------------------------------------------------- draw and view

test("a click draws the picture, which appears live and is within the size limit", async ({ page }) => {
  await page.goto(base);
  await draw(page);
  const size = await ui.base(page).evaluate((i) => ({ w: (i as HTMLImageElement).naturalWidth, h: (i as HTMLImageElement).naturalHeight }));
  expect(size.w).toBeGreaterThan(0);
  expect(size.w).toBeLessThanOrEqual(maxSide);
  expect(size.h).toBeLessThanOrEqual(maxSide);
  // The picture says what it is for people who cannot see it.
  await expect(ui.base(page)).toHaveAttribute("alt", /nauvis/i);
  await expect(page.getByText(/drawn/i).first()).toBeVisible();
  await checkPage(page);
  // It is kept: a fresh page view shows it with no run.
  await page.reload();
  await expect(ui.base(page)).toBeVisible(live);
  await expect(ui.draw(page)).toBeEnabled();
});

test("the surfaces are listed and choosing one shows its picture", async ({ page }) => {
  await page.goto(base);
  await draw(page);
  const first = await ui.base(page).getAttribute("src");
  await expect(ui.surfaces(page)).toContainText("nauvis");
  // The fake game charts more than one surface (task F9 gives a planet and a platform).
  const others = ui.surfaces(page).getByRole("link");
  expect(await others.count()).toBeGreaterThanOrEqual(2);
  await expect(others.first()).toHaveAttribute("aria-current", "page");
  await others.nth(1).click();
  await expect.poll(() => ui.base(page).getAttribute("src")).not.toBe(first);
  await expect.poll(() => ui.base(page).evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
  await expect(ui.base(page)).not.toHaveAttribute("alt", /^$/);
});

test("the wheel zooms by 1.15 a notch around the pointer and stops at the fitted size", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  const fitted = await width(page);
  const mid = await middleOf(page);
  await page.mouse.move(mid.x, mid.y);
  await page.mouse.wheel(0, -100);
  await expect.poll(() => width(page)).toBeCloseTo(fitted * 1.15, 0);
  await page.mouse.wheel(0, -100);
  await expect.poll(() => width(page)).toBeCloseTo(fitted * 1.15 * 1.15, 0);
  // A wheel turn over the picture is not the browser's page scroll.
  const at = await page.evaluate(() => window.scrollY);
  await page.mouse.wheel(0, -100);
  expect(await page.evaluate(() => window.scrollY)).toBe(at);
  // Out again, and no further than the fitted size.
  for (let i = 0; i < 30; i++) await page.mouse.wheel(0, 100);
  await expect.poll(() => width(page)).toBeCloseTo(fitted, 0);
});

test("the keys and the buttons zoom by 1.15, 0 fits, and the level is said", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  const fitted = await width(page);
  await ui.frame(page).focus();
  await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 115%");
  await page.keyboard.press("=");
  await expect(ui.status(page)).toHaveText("Zoom 132%");
  expect(await width(page)).toBeCloseTo(fitted * 1.3225, 0);
  await page.keyboard.press("-");
  await expect(ui.status(page)).toHaveText("Zoom 115%");
  await ui.zoomIn(page).press("Enter");
  await expect(ui.status(page)).toHaveText("Zoom 132%");
  await ui.zoomOut(page).press("Enter");
  await expect(ui.status(page)).toHaveText("Zoom 115%");
  await ui.fit(page).press("Enter");
  await expect(ui.status(page)).toHaveText("Zoom 100%");
  await ui.frame(page).focus();
  await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 115%");
  await ui.frame(page).focus();
  await page.keyboard.press("0");
  await expect(ui.status(page)).toHaveText("Zoom 100%");
  expect(await width(page)).toBeCloseTo(fitted, 0);
  await expect(ui.zoomOut(page)).toHaveAttribute("aria-disabled", "true");
  await expect(ui.fit(page)).toHaveAttribute("aria-disabled", "true");
  // One tile is a block of pixels, never smoothed.
  expect(await ui.base(page).evaluate((i) => getComputedStyle(i).imageRendering)).toMatch(/pixelated|crisp-edges/);
});

// Without script the zoom buttons would do nothing, so they are not shown;
// the picture is still there to scroll.
test("without script the picture shows and the zoom buttons do not", async ({ page, browser, foreman }) => {
  await page.goto(base);
  await draw(page);
  const plain = await browser.newContext({ baseURL: foreman.url, javaScriptEnabled: false, storageState: await page.context().storageState() });
  const noScript = await plain.newPage();
  await noScript.goto(base);
  await expect(ui.base(noScript)).toBeVisible();
  await expect(noScript.locator(".factorymap__zoom")).toBeHidden();
  await expect(ui.zoomIn(noScript)).toBeHidden();
  await plain.close();
});

test("a drag pans a zoomed picture, and the arrow keys pan it too", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  await ui.frame(page).focus();
  for (let i = 0; i < 8; i++) await page.keyboard.press("+");
  const start = await scroll(page);
  const mid = await middleOf(page);
  await page.mouse.move(mid.x, mid.y);
  await page.mouse.down();
  await page.mouse.move(mid.x - 60, mid.y - 40, { steps: 4 });
  await page.mouse.up();
  const moved = await scroll(page);
  expect(moved.x - start.x).toBeCloseTo(60, 0);
  expect(moved.y - start.y).toBeCloseTo(40, 0);
  // The picture is not dragged out as an image.
  await expect(ui.base(page)).toHaveAttribute("draggable", "false");
  await ui.frame(page).focus();
  await page.keyboard.press("ArrowRight");
  await expect.poll(async () => (await scroll(page)).x).toBeGreaterThan(moved.x);
  await page.keyboard.press("ArrowDown");
  await expect.poll(async () => (await scroll(page)).y).toBeGreaterThan(moved.y);
});

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, isMobile: true });

  test("two fingers pinch and one finger drags", { tag: "@looks" }, async ({ page }) => {
    await page.goto(base);
    await draw(page);
    const fitted = await width(page);
    const mid = await middleOf(page);
    const cdp = await page.context().newCDPSession(page);
    const touch = (type: "touchStart" | "touchMove" | "touchEnd", points: { x: number; y: number }[]) =>
      cdp.send("Input.dispatchTouchEvent", { type, touchPoints: points.map((p, id) => ({ ...p, id })) });
    await touch("touchStart", [{ x: mid.x - 20, y: mid.y }, { x: mid.x + 20, y: mid.y }]);
    for (const d of [30, 40, 50, 60]) await touch("touchMove", [{ x: mid.x - d, y: mid.y }, { x: mid.x + d, y: mid.y }]);
    await touch("touchEnd", []);
    // The fingers moved three times apart, so the picture grew about that much; the exact step is the viewer's.
    await expect.poll(() => width(page)).toBeGreaterThan(fitted * 1.5);
    const start = await scroll(page);
    await touch("touchStart", [{ x: mid.x, y: mid.y }]);
    for (const d of [10, 20, 30]) await touch("touchMove", [{ x: mid.x - d, y: mid.y - d }]);
    await touch("touchEnd", []);
    const moved = await scroll(page);
    expect(moved.x).toBeGreaterThan(start.x);
    expect(moved.y).toBeGreaterThan(start.y);
    await checkPage(page);
  });
});

// ---------------------------------------------------------------- layers and downloads

test("each category has a checkbox in a fixed order, and ticking one stacks its layer", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  for (const name of categories) await expect(ui.layer(page, name)).toBeVisible();
  // The page's order is the plan's: belts first, other last.
  const boxes = await page.getByRole("checkbox").evaluateAll((els) => els.map((e) => (e as HTMLInputElement).labels?.[0]?.textContent?.trim() ?? ""));
  const listed = boxes.filter((l) => categories.includes(l));
  expect(listed).toEqual(categories);
  await expect(ui.layers(page)).toHaveCount(0);
  await ui.layer(page, "Production").check();
  await expect(ui.layers(page)).toHaveCount(1);
  await ui.layer(page, "Belts and inserters").check();
  await expect(ui.layers(page)).toHaveCount(2);
  // Every layer sits on the base's pixel grid: the same size, laid over it.
  const base0 = await ui.base(page).boundingBox();
  for (const box of await ui.layers(page).evaluateAll((l) => l.map((i) => i.getBoundingClientRect().toJSON()))) {
    expect(box.width).toBeCloseTo(base0!.width, 0);
    expect(box.height).toBeCloseTo(base0!.height, 0);
    expect(box.x).toBeCloseTo(base0!.x, 0);
    expect(box.y).toBeCloseTo(base0!.y, 0);
  }
  await ui.layer(page, "Production").uncheck();
  await expect(ui.layers(page)).toHaveCount(1);
  await ui.layer(page, "Belts and inserters").uncheck();
  await expect(ui.layers(page)).toHaveCount(0);
  await checkPage(page);
});

test("a layer is laid over the base at the same size and follows a zoom", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  await ui.layer(page, "Other").check();
  await expect(ui.layers(page)).toHaveCount(1);
  await ui.frame(page).focus();
  await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 115%");
  const b = (await ui.base(page).boundingBox())!;
  const l = (await ui.layers(page).first().boundingBox())!;
  expect(l.width).toBeCloseTo(b.width, 0);
  expect(l.height).toBeCloseTo(b.height, 0);
  expect(l.x).toBeCloseTo(b.x, 0);
  expect(l.y).toBeCloseTo(b.y, 0);
  // A layer's picture is fetched when first ticked, from the layer address.
  await expect(ui.layers(page).first()).toHaveAttribute("src", /\/map\/\d+\.png\?layer=other$/);
});

test("the download link gives the base, and with layers ticked the base combined with them", async ({ page }) => {
  await page.goto(base);
  await draw(page);
  const plain = await pngOf(page, ui.download(page));
  expect(plain.name).toMatch(/\.png$/);
  expect(plain.bytes.subarray(0, 8).equals(pngSignature)).toBe(true);
  const shown = await ui.base(page).evaluate((i) => ({ w: (i as HTMLImageElement).naturalWidth, h: (i as HTMLImageElement).naturalHeight }));
  expect({ w: plain.w, h: plain.h }).toEqual(shown);

  for (const name of ["Production", "Belts and inserters", "Other"]) await ui.layer(page, name).check();
  // The link now names the ticked layers, and its answer is the base with them on it.
  await expect(ui.download(page)).toHaveAttribute("href", /layer=production/);
  await expect(ui.download(page)).toHaveAttribute("href", /layer=belts/);
  await expect(ui.download(page)).toHaveAttribute("href", /layer=other/);
  const combined = await pngOf(page, ui.download(page));
  expect(combined.name).toMatch(/\.png$/);
  expect(combined.bytes.subarray(0, 8).equals(pngSignature)).toBe(true);
  // Same grid as the base, a different picture: the layers are in it.
  expect({ w: combined.w, h: combined.h }).toEqual(shown);
  expect(combined.bytes.equals(plain.bytes)).toBe(false);
  // Unticked again, the link is the base's.
  for (const name of ["Production", "Belts and inserters", "Other"]) await ui.layer(page, name).uncheck();
  await expect(ui.download(page)).not.toHaveAttribute("href", /layer=/);
});

// The plan's page contract: GET /worlds/{id}/map/{surface}.png?layer=<category> (no layer: the base) and
// GET /worlds/{id}/map/{surface}/download?layer=a&layer=b (an attachment, combined on the server).
test("the picture and download addresses answer as the contract says, and refuse what is unknown", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  const i = await surfaceOf(page);
  const at = (path: string) => page.request.get(`${base}/${path}`, { maxRedirects: 0 });

  const plain = await at(`${i}.png`);
  expect(plain.status()).toBe(200);
  expect(plain.headers()["content-type"]).toMatch(/^image\/png/);
  const plainBytes = await plain.body();
  expect(plainBytes.subarray(0, 8).equals(pngSignature)).toBe(true);
  for (const layer of ["belts", "production", "power", "fluids", "rail", "defence", "logistics", "other"]) {
    const r = await at(`${i}.png?layer=${layer}`);
    expect(r.status(), `layer ${layer}`).toBe(200);
    expect(r.headers()["content-type"]).toMatch(/^image\/png/);
    const b = await r.body();
    // Same pixel grid as the base: the same width and height.
    expect([b.readUInt32BE(16), b.readUInt32BE(20)]).toEqual([plainBytes.readUInt32BE(16), plainBytes.readUInt32BE(20)]);
  }

  const dl = await at(`${i}/download?layer=production&layer=belts`);
  expect(dl.status()).toBe(200);
  expect(dl.headers()["content-type"]).toMatch(/^image\/png/);
  expect(dl.headers()["content-disposition"]).toMatch(/^attachment/);
  const dlBytes = await dl.body();
  expect(dlBytes.subarray(0, 8).equals(pngSignature)).toBe(true);
  expect([dlBytes.readUInt32BE(16), dlBytes.readUInt32BE(20)]).toEqual([plainBytes.readUInt32BE(16), plainBytes.readUInt32BE(20)]);
  expect((await at(`${i}/download`)).headers()["content-disposition"]).toMatch(/^attachment/);

  // An unknown surface or layer is a 404, and nothing is read from the path.
  for (const path of ["9999.png", `${i}.png?layer=nope`, `${i}.png?layer=..%2F..%2Fworld`, "9999/download", `${i}/download?layer=production&layer=nope`, "abc.png", "-1.png"]) {
    expect((await at(path)).status(), path).toBe(404);
  }
});

test("a picture is only for a signed-in user", async ({ page, playwright, foreman }) => {
  await page.goto(base);
  await draw(page);
  const i = await surfaceOf(page);
  const anonymous = await playwright.request.newContext({ baseURL: foreman.url });
  try {
    for (const path of [`${base}/${i}.png`, `${base}/${i}/download`, `${base}/${i}.png?layer=production`, base]) {
      const r = await anonymous.get(path, { maxRedirects: 0 });
      // Sent to sign in or refused, never the picture or the page.
      expect(r.headers()["content-type"] ?? "", path).not.toMatch(/image\/png/);
      expect(r.status(), path).not.toBe(200);
    }
    // And no drawing or mode change from outside.
    for (const path of [`${base}/draw`, `${base}/mode`]) {
      const r = await anonymous.post(path, { form: { mode: "off", confirm_memory: "1" }, maxRedirects: 0 });
      expect(r.status(), path).not.toBe(200);
    }
  } finally {
    await anonymous.dispose();
  }
  await page.goto(base);
  await expect(ui.state(page)).toBeVisible();
  expect(factoryMapOf(foreman)).toBeFalsy();
});

// ---------------------------------------------------------------- modes

test("the mode is set on the page, kept in world.json, and neither audited nor snapshotted", async ({ page, foreman }) => {
  const before = snapshotCount(foreman);
  await page.goto(base);
  await ui.mode(page, "Automatic").check();
  await ui.skipOnline(page).uncheck();
  // The save posts the form and redirects back here; reloading before that
  // page has loaded races the redirect ("Not attached to an active page").
  const landed = page.waitForEvent("load");
  await ui.saveMode(page).click();
  await landed;
  await expect.poll(() => factoryMap(foreman)).toEqual({ mode: "automatic", skip_online: false });
  await page.reload();
  await expect(ui.mode(page, "Automatic")).toBeChecked();
  await expect(ui.skipOnline(page)).not.toBeChecked();
  // Foreman's own setting, like the display name: no snapshot, no audit row.
  expect(snapshotCount(foreman)).toBe(before);
  await page.goto("/settings/audit-log");
  await expect(page.locator("#audit-list")).not.toContainText(/factory/i);
  await checkPage(page);
});

test("Off hides the button, and the last picture stays", async ({ page, foreman }) => {
  await page.goto(base);
  await draw(page);
  await ui.mode(page, "Off").check();
  await ui.saveMode(page).click();
  await expect.poll(() => factoryMap(foreman)).toMatchObject({ mode: "off" });
  await expect(ui.draw(page)).toHaveCount(0);
  await expect(ui.base(page)).toBeVisible();
  await page.reload();
  await expect(ui.draw(page)).toHaveCount(0);
  await expect(ui.base(page)).toBeVisible();
  await checkPage(page);
});

test("a mode set for one world does not change another", async ({ page, foreman }) => {
  seedSecond(foreman);
  await page.goto(base);
  await ui.mode(page, "Automatic").check();
  await ui.saveMode(page).click();
  await expect.poll(() => factoryMap(foreman)).toMatchObject({ mode: "automatic" });
  await page.goto("/worlds/second/map");
  await expect(ui.mode(page, "Manual")).toBeChecked();
  expect(JSON.parse(readFileSync(join(foreman.dataDir, "worlds", "second", "world.json"), "utf8")).factory_map ?? null).toBeNull();
});

test("Off with nothing drawn says so and offers no button", async ({ page, foreman }) => {
  await page.goto(base);
  await ui.mode(page, "Off").check();
  await ui.saveMode(page).click();
  await expect.poll(() => factoryMap(foreman)).toMatchObject({ mode: "off" });
  await expect(ui.draw(page)).toHaveCount(0);
  await expect(ui.state(page)).toContainText(/off/i);
  await expect(ui.base(page)).toHaveCount(0);
  await checkPage(page);
});

test("a world on a game too old for the chart says so and offers no drawing", { tag: "@looks" }, async ({ page, foreman }) => {
  // A 1.1 world: the game has no chart to read (SPEC 5.16: "Factory maps need Factorio 2.0.61 or newer").
  seedSecond(foreman, { version: "1.1.110" });
  await page.goto("/worlds/second/map");
  await expect(page.locator("#factorymap-state")).toContainText(/2\.0\.61 or newer/);
  await expect(page.getByRole("button", { name: /^Draw factory map/ })).toHaveCount(0);
  await checkPage(page);
});

test("the state region is also served alone, for the page's poll", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  const r = await page.request.get(`${base}?fragment=1`);
  expect(r.status()).toBe(200);
  const html = await r.text();
  expect(html).toContain('id="factorymap-state"');
  // A fragment, not the page: no layout around it.
  expect(html).not.toContain("<html");
  expect(html).not.toContain('class="subnav"');
});

// ---------------------------------------------------------------- beside a running game

test("beside a running game the page asks first, saying what memory the run needs, then draws", async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await startWorld(page);
  await page.goto(base);
  await ui.draw(page).click();
  // Nothing runs until the question is answered (rule 21).
  await expect(page.getByText(/memory/i).first()).toBeVisible();
  expect((await g.state()).calls.filter((c) => /factorymap/.test(c))).toEqual([]);
  await checkPage(page);
  await confirmMemory(page);
  await expect(ui.base(page)).toBeVisible(live);
  await checkPage(page);
});

test("a post to draw a running world without confirm_memory does not run", async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await startWorld(page);
  await page.goto(base);
  await page.request.post(`${base}/draw`, { maxRedirects: 0 });
  await page.waitForTimeout(1500);
  expect((await g.state()).calls.filter((c) => /factorymap/.test(c))).toEqual([]);
  await page.goto(base);
  await expect(ui.base(page)).toHaveCount(0);
});

test("skip while players are online refuses the draw, and a leave lifts it", async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await startWorld(page);
  await g.call("/join", { name: container, player: "alice" });
  await page.goto(base);
  await expect(ui.skipOnline(page)).toBeChecked();
  // Refused by the post itself, even with the memory confirmed.
  await page.request.post(`${base}/draw`, { form: { confirm_memory: "1" }, maxRedirects: 0 });
  await page.waitForTimeout(1500);
  expect((await g.state()).calls.filter((c) => /factorymap/.test(c))).toEqual([]);
  // And by the page, which says why, whether it disables the button or answers the click.
  await expect(ui.draw(page)).toHaveCount(0);
  await expect(ui.confirm(page)).toHaveCount(0);
  await expect(ui.state(page)).toContainText(/Players are online/, live);
  await expect(ui.base(page)).toHaveCount(0);
  expect((await g.state()).calls.filter((c) => /factorymap/.test(c))).toEqual([]);
  await checkPage(page);

  await g.call("/leave", { name: container, player: "alice" });
  await page.goto(base);
  await draw(page, { running: true });
});

test("with skip off, a click draws a running world even with players online", async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await startWorld(page);
  await g.call("/join", { name: container, player: "alice" });
  await page.goto(base);
  await ui.skipOnline(page).uncheck();
  await ui.saveMode(page).click();
  await expect(ui.skipOnline(page)).not.toBeChecked();
  await page.reload();
  await draw(page, { running: true });
});

// ---------------------------------------------------------------- failure

test("a failed drawing says so, keeps no picture, and can be tried again", { tag: "@looks" }, async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await g.call("/preview-mode", { mode: "oom" });
  try {
    await page.goto(base);
    await ui.draw(page).click();
    // The failure's words come from factorymap.failure.<id>; the fake's out-of-memory ending names memory.
    await expect(ui.state(page)).toContainText(/memory/i, live);
    await expect(ui.base(page)).toHaveCount(0);
    await checkPage(page);
    await expect(ui.draw(page)).toBeEnabled();
  } finally {
    await g.call("/preview-mode", { mode: "ok" });
  }
  await draw(page);
});

// ---------------------------------------------------------------- axe in every state, and focus across the poll

// checkPage is axe with the WCAG 2.2 AA rules (foreman.ts, wcagTags); the four projects run it in both
// themes. The drawn, failed and memory-note states are scanned in their own tests above; these cover
// the states those leave: a run going, with and without a kept picture, and the drawn page worked on.
test("a drawing in progress with no picture kept yet passes axe", { tag: "@looks" }, async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await g.call("/preview-mode", { mode: "hang" });
  await page.goto(base);
  await ui.draw(page).click();
  await expect(ui.state(page)).toHaveAttribute("data-state", "drawing", live);
  await expect(ui.draw(page)).toHaveCount(0);
  await checkPage(page);
});

test("a drawing in progress over a kept picture passes axe and keeps the picture", { tag: "@looks" }, async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await page.goto(base);
  await draw(page);
  await g.call("/preview-mode", { mode: "hang" });
  await ui.draw(page).click();
  await expect(ui.state(page)).toHaveAttribute("data-state", "drawing", live);
  await expect(ui.base(page)).toBeVisible();
  await checkPage(page);
});

test("the drawn page passes axe with layers ticked and the picture zoomed", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  await ui.layer(page, "Production").check();
  await ui.layer(page, "Power").check();
  await ui.frame(page).focus();
  for (let i = 0; i < 4; i++) await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 175%");
  await checkPage(page);
});

test("a failure over a kept picture passes axe", { tag: "@looks" }, async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await page.goto(base);
  await draw(page);
  await g.call("/preview-mode", { mode: "oom" });
  try {
    await ui.draw(page).click();
    await expect(ui.state(page)).toHaveAttribute("data-state", "failed", live);
    await expect(ui.base(page)).toBeVisible();
    await checkPage(page);
  } finally {
    await g.call("/preview-mode", { mode: "ok" });
  }
});

test("the memory question, with a kept picture on the page, passes axe", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  await draw(page);
  await startWorld(page);
  await page.goto(base);
  await ui.draw(page).click();
  await expect(ui.confirm(page)).toBeVisible();
  await checkPage(page);
});

// A poll swaps what the state region shows while a drawing runs (3 seconds, the plan's page contract). Someone using
// the keys on the picture must not lose their place: focus stays on the frame through a poll that finds
// the run still going, and comes back to the same control when the drawing ends.
test("focus, zoom and ticked layers survive polls while drawing over a kept picture", { tag: "@looks" }, async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await page.goto(base);
  await draw(page);
  await g.call("/preview-mode", { mode: "hang" });
  await ui.draw(page).click();
  await expect(ui.state(page)).toHaveAttribute("data-state", "drawing", live);
  // Work on the picture: a layer ticked, zoomed in, focus on the frame.
  await ui.layer(page, "Production").check();
  await expect(ui.layers(page)).toHaveCount(1);
  await ui.frame(page).focus();
  for (let i = 0; i < 3; i++) await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 152%");
  const zoomed = await width(page);
  const picture = await ui.base(page).getAttribute("src");
  // Wait for the page's own polls (the region's fragment), however it swaps what it gets in, twice, and a
  // moment for the second swap.
  const poll = () => page.waitForResponse((r) => r.url().includes("fragment=1"), { timeout: 8_000 });
  await poll();
  await poll();
  await page.waitForTimeout(500);
  await expect(ui.state(page)).toHaveAttribute("data-state", "drawing");
  await expect(ui.frame(page)).toBeFocused();
  expect(await width(page)).toBeCloseTo(zoomed, 0);
  await expect(ui.layer(page, "Production")).toBeChecked();
  await expect(ui.layers(page)).toHaveCount(1);
  await expect(ui.base(page)).toHaveAttribute("src", picture!);
  // The keys still work from there.
  await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 175%");
});

test("focus returns to the same control when the drawing ends", { tag: "@looks" }, async ({ page, fakeGame }) => {
  const g = fakeGame as FakeGame;
  await page.goto(base);
  await draw(page);
  // The slow ending: the run is over a moment after it starts, and the page learns of it at its next poll.
  await g.call("/preview-mode", { mode: "slow" });
  try {
    await ui.draw(page).click();
    await expect(ui.state(page)).toHaveAttribute("data-state", "drawing");
    await ui.frame(page).focus();
    await expect(ui.frame(page)).toBeFocused();
    await expect(ui.state(page)).toHaveAttribute("data-state", "drawn", live);
    await expect(ui.frame(page)).toBeFocused();
    await expect(ui.base(page)).toBeVisible();
    // And the keys work again at once.
    await page.keyboard.press("+");
    await expect(ui.status(page)).toHaveText("Zoom 115%");
  } finally {
    await g.call("/preview-mode", { mode: "ok" });
  }
});

// ---------------------------------------------------------------- keyboard and phone

test("everything works from the keyboard alone", { tag: "@looks" }, async ({ page }) => {
  await page.goto(base);
  // The button first: Enter draws.
  await tabTo(page, ui.draw(page));
  await page.keyboard.press("Enter");
  await expect(ui.base(page)).toBeVisible(live);
  // Each control can be reached by Tab with a visible focus ring.
  for (const target of [ui.frame(page), ui.zoomIn(page), ui.zoomOut(page), ui.layer(page, "Production"), ui.download(page)]) {
    await tabTo(page, target);
    const ring = await target.evaluate((el) => {
      const s = getComputedStyle(el);
      return s.outlineStyle !== "none" && parseFloat(s.outlineWidth) > 0;
    });
    expect(ring, "a focused control shows a ring").toBe(true);
  }
  // A layer is ticked with the space bar.
  await tabTo(page, ui.layer(page, "Production"));
  await page.keyboard.press("Space");
  await expect(ui.layers(page)).toHaveCount(1);
  // The frame answers the keys: zoom and pan.
  await tabTo(page, ui.frame(page));
  await page.keyboard.press("+");
  await expect(ui.status(page)).toHaveText("Zoom 115%");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("0");
  await expect(ui.status(page)).toHaveText("Zoom 100%");
  // The mode form is reachable and the radios move with the arrow keys.
  await tabTo(page, ui.mode(page, "Manual"));
  await page.keyboard.press("ArrowDown");
  await expect(ui.mode(page, "Off")).toBeChecked();
});

test("at phone width the page has no sideways scroll and the controls are reachable", { tag: "@looks" }, async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto(base);
  await checkPage(page);
  await draw(page);
  await checkPage(page);
  await ui.layer(page, "Production").check();
  await checkPage(page);
  for (const c of [ui.draw(page), ui.zoomIn(page), ui.zoomOut(page), ui.layer(page, "Production"), ui.saveMode(page)]) {
    await c.scrollIntoViewIfNeeded();
    await expect(c).toBeVisible();
  }
});

// Ruling 1: Draw keeps its exact place in x and y before and after a drawing,
// at both widths; the state under it and the map may come and go.
test("Draw stands in one place before and after a drawing", async ({ page }) => {
  const sizes = [
    { width: 1920, height: 1080 },
    { width: 390, height: 844 },
  ];
  const place = async () => {
    await page.goto(base);
    return ui.draw(page).evaluate((e) => {
      const r = e.getBoundingClientRect();
      return { x: r.x + window.scrollX, y: r.y + window.scrollY };
    });
  };
  const before: { x: number; y: number }[] = [];
  for (const size of sizes) {
    await page.setViewportSize(size);
    before.push(await place());
  }
  await draw(page);
  for (const [i, size] of sizes.entries()) {
    await page.setViewportSize(size);
    expect(await place(), `${size.width}`).toEqual(before[i]);
  }
});
