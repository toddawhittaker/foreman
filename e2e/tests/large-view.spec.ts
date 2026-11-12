import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 17; DESIGN 8.8: the large view is a picture viewer. The wheel,
// plus and minus zoom by 1.15 around the pointer or the centre, up to the
// larger of 8 times the fitted size and 8 CSS pixels per tile; a drag or one
// finger pans and two fingers pinch; the arrows, the X and the zoom buttons
// sit over the picture, and the download icon sits by the heading. The
// preview picture fills its column, capped by the window's height.
test.use({ fakeGameOn: true });

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const dialog = (page: Page) => page.locator("#large-view");
const frame = (page: Page) => page.locator("#large-frame");
const largeImg = (page: Page) => page.locator("#large-img");
const width = (page: Page) => largeImg(page).evaluate((i) => i.getBoundingClientRect().width);
const opacity = (page: Page, id: string) => page.locator(`#${id}`).evaluate((b) => getComputedStyle(b).opacity);
const scroll = (page: Page) => frame(page).evaluate((f) => ({ x: f.scrollLeft, y: f.scrollTop }));

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
  await expect.poll(() => largeImg(page).evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
}

// The spot of the picture under a point of the window, as fractions of the picture.
async function spotAt(page: Page, x: number, y: number) {
  return largeImg(page).evaluate((i, [px, py]) => {
    const r = i.getBoundingClientRect();
    return { u: (px - r.left) / r.width, v: (py - r.top) / r.height };
  }, [x, y]);
}

async function middleOf(page: Page) {
  const box = (await frame(page).boundingBox())!;
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

test("the wheel zooms by 1.15 a notch around the pointer, from the fitted size", async ({ page }) => {
  await openLarge(page);
  const fitted = await width(page);
  const pageAt = await page.evaluate(() => window.scrollY);
  const mid = await middleOf(page);
  await page.mouse.move(mid.x, mid.y);
  await page.mouse.wheel(0, -100);
  await expect.poll(() => width(page)).toBeCloseTo(fitted * 1.15, 0);
  await page.mouse.wheel(0, -100);
  await expect.poll(() => width(page)).toBeCloseTo(fitted * 1.15 * 1.15, 0);
  // Once the picture overflows the frame both ways, the spot under the
  // pointer stays under it, to within a pixel. (Until then it stays centred.)
  const frameBox = (await frame(page).boundingBox())!;
  while ((await width(page)) < Math.max(frameBox.width, frameBox.height) + 100) await page.mouse.wheel(0, -100);
  const at = { x: mid.x + frameBox.width / 5, y: mid.y - frameBox.height / 5 };
  await page.mouse.move(at.x, at.y);
  const spot = await spotAt(page, at.x, at.y);
  const before = await width(page);
  await page.mouse.wheel(0, -100);
  await expect.poll(() => width(page)).toBeCloseTo(before * 1.15, 0);
  const after = await spotAt(page, at.x, at.y);
  const w = await width(page);
  expect(Math.abs(after.u - spot.u) * w).toBeLessThan(1.5);
  expect(Math.abs(after.v - spot.v) * w).toBeLessThan(1.5);
  // Out again, and no further than the fitted size.
  for (let i = 0; i < 30; i++) await page.mouse.wheel(0, 100);
  await expect.poll(() => width(page)).toBeCloseTo(fitted, 0);
  // A wheel turn is not the browser's scroll: the page stays put.
  expect(await page.evaluate(() => window.scrollY)).toBe(pageAt);
});

test("plus and minus zoom around the centre, 0 fits, and the level is said", async ({ page }) => {
  await openLarge(page);
  await expect(frame(page)).toBeFocused();
  const fitted = await width(page);
  const mid = await middleOf(page);
  const spot = await spotAt(page, mid.x, mid.y);
  await page.keyboard.press("+");
  await expect(page.locator("#large-status")).toHaveText("Zoom 115%");
  await page.keyboard.press("=");
  await expect(page.locator("#large-status")).toHaveText("Zoom 132%");
  expect(await width(page)).toBeCloseTo(fitted * 1.3225, 0);
  const after = await spotAt(page, mid.x, mid.y);
  expect(Math.abs(after.u - spot.u) * (await width(page))).toBeLessThan(1.5);
  await page.keyboard.press("-");
  await expect(page.locator("#large-status")).toHaveText("Zoom 115%");
  // The buttons do the same, and say it too.
  await dialog(page).getByRole("button", { name: "Zoom in" }).press("Enter");
  await expect(page.locator("#large-status")).toHaveText("Zoom 132%");
  await dialog(page).getByRole("button", { name: "Zoom out" }).press("Enter");
  await expect(page.locator("#large-status")).toHaveText("Zoom 115%");
  await page.keyboard.press("0");
  await expect(page.locator("#large-status")).toHaveText("Zoom 100%");
  expect(await width(page)).toBeCloseTo(fitted, 0);
  await expect(dialog(page).getByRole("button", { name: "Zoom out" })).toHaveAttribute("aria-disabled", "true");
  // The pixels stay sharp: one tile is one block of pixels.
  expect(await largeImg(page).evaluate((i) => getComputedStyle(i).imageRendering)).toBe("pixelated");
});

// The planet's name is said a moment after a step; a zoom made in that
// moment is newer, so it is what the status keeps.
test("a zoom right after a step is not overwritten by the planet's name", async ({ page }) => {
  // Every planet drawn, so a step shows its picture at once and says its name.
  await page.goto("/generate");
  await page.getByRole("button", { name: "Preview map" }).click();
  await expect(section(page).getByRole("img")).toHaveCount(5);
  await section(page).getByRole("link", { name: /Vulcanus.*opens the large view/ }).click();
  await expect(largeImg(page)).toHaveAttribute("alt", /^Map preview of Vulcanus/);
  await page.evaluate(() => {
    const view = document.getElementById("large-view")!;
    for (const key of ["ArrowLeft", "+"]) view.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));
  });
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
  await expect(page.locator("#large-status")).toHaveText("Zoom 115%");
  // Longer than the announcement's delay.
  await page.waitForTimeout(200);
  await expect(page.locator("#large-status")).toHaveText("Zoom 115%");
});

test("dragging moves a zoomed picture, and the arrow keys on it pan instead of stepping", async ({ page }) => {
  await openLarge(page);
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
  await expect(largeImg(page)).toHaveAttribute("draggable", "false");
  await frame(page).focus();
  await page.keyboard.press("ArrowRight");
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
  await expect.poll(async () => (await scroll(page)).x).toBeGreaterThan(moved.x);
});

test("the arrows and the X show on hover and on keyboard focus, named after where they lead", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await openLarge(page);
  const prev = dialog(page).getByRole("button", { name: "Previous planet: Aquilo" });
  const next = dialog(page).getByRole("button", { name: "Next planet: Vulcanus" });
  const close = dialog(page).getByRole("button", { name: "Close" });
  await expect(prev).toBeAttached();
  await expect(next).toBeAttached();
  // Nothing animates under reduced motion.
  expect(await next.evaluate((b) => getComputedStyle(b).transitionDuration)).toBe("0s");
  // Away from the picture they are out of sight.
  await page.mouse.move(1, 1);
  await expect.poll(() => opacity(page, "large-next")).toBe("0");
  await page.mouse.move(...Object.values(await middleOf(page)) as [number, number]);
  for (const id of ["large-prev", "large-next", "large-close", "large-in", "large-out"]) await expect.poll(() => opacity(page, id)).toBe("1");
  // Over the picture's left and right edges, the X in the corner.
  const box = (await frame(page).boundingBox())!;
  const p = (await prev.boundingBox())!;
  const n = (await next.boundingBox())!;
  const c = (await close.boundingBox())!;
  expect(p.x - box.x).toBeLessThan(box.width / 4);
  expect(box.x + box.width - (n.x + n.width)).toBeLessThan(box.width / 4);
  expect(Math.abs(p.y + p.height / 2 - (box.y + box.height / 2))).toBeLessThan(2);
  expect(c.y - box.y).toBeLessThan(box.height / 4);
  expect(c.x).toBeGreaterThan(box.x + (box.width * 3) / 4);
  await checkPage(page);

  // By keyboard alone they show when one has focus.
  await page.mouse.move(1, 1);
  await expect.poll(() => opacity(page, "large-next")).toBe("0");
  await tabTo(page, next);
  await expect.poll(() => opacity(page, "large-next")).toBe("1");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  await expect(dialog(page).getByRole("button", { name: "Previous planet: Nauvis" })).toBeAttached();
  await expect(dialog(page).getByRole("button", { name: "Next planet: Gleba" })).toBeAttached();
  await dialog(page).getByRole("button", { name: "Previous planet: Nauvis" }).click();
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();

  // The X closes, and focus goes back to the picture.
  await close.click();
  await expect(dialog(page)).toBeHidden();
  await expect(section(page).getByRole("link", { name: /Map preview of Nauvis/ })).toBeFocused();
});

// The key help is one short line at the top, beside the planet's name on a
// desktop, and the frame still names it as its description. At 320 px the
// sentence about panning and tapping cannot fit one line, so it may take two.
test("the key help is one line in the bar above the picture", async ({ page }) => {
  await openLarge(page);
  const keys = page.locator("#large-keys");
  await expect(keys).toHaveText("Keys: ← → planet (pan when zoomed), + − zoom, 0 fit, Esc close. Tap to center.");
  await expect(frame(page)).toHaveAccessibleDescription("Keys: ← → planet (pan when zoomed), + − zoom, 0 fit, Esc close. Tap to center.");
  const k = (await keys.boundingBox())!;
  const f = (await frame(page).boundingBox())!;
  expect(k.y + k.height).toBeLessThanOrEqual(f.y);
  const lines = await keys.evaluate((p) => p.getBoundingClientRect().height / parseFloat(getComputedStyle(p).lineHeight));
  expect(lines).toBeLessThan((page.viewportSize()?.width ?? 0) > 320 ? 1.5 : 2.5);
  if ((page.viewportSize()?.width ?? 0) > 320) {
    const h = (await page.locator("#large-title").boundingBox())!;
    expect(k.y).toBeLessThan(h.y + h.height);
    expect(k.y + k.height).toBeGreaterThan(h.y);
  }
  await checkPage(page);
});

test("the download icon by the heading downloads the planet shown", async ({ page }) => {
  await openLarge(page);
  const icon = dialog(page).getByRole("link", { name: "Download Nauvis map" });
  await expect(icon).toBeVisible();
  const png = section(page).getByRole("link", { name: "Download Nauvis (PNG)" });
  expect(await icon.evaluate((a) => (a as HTMLAnchorElement).href)).toBe(await png.evaluate((a) => (a as HTMLAnchorElement).href));
  // On the heading's line.
  const h = (await page.locator("#large-title").boundingBox())!;
  const i = (await icon.boundingBox())!;
  expect(i.y).toBeLessThan(h.y + h.height);
  expect(i.y + i.height).toBeGreaterThan(h.y);
  const [download] = await Promise.all([page.waitForEvent("download"), icon.click()]);
  expect(download.suggestedFilename()).toMatch(/nauvis\.png$/);
  await page.keyboard.press("ArrowRight");
  await expect(dialog(page).getByRole("link", { name: "Download Vulcanus map" })).toBeVisible();
});

// SPEC 17.15: the most zoom is the larger of 8 times the fitted size and 8 CSS pixels per tile.
for (const [size, viewport] of [["1280", { width: 1920, height: 1600 }], ["3072", null]] as const) {
  test(`the most zoom of a ${size} picture is the larger of 8 times fit and 8 px a tile`, async ({ page }) => {
    if (viewport) await page.setViewportSize(viewport);
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    if (size !== "1280") {
      await page.locator("#generate-preview-size").selectOption(size, { force: true });
      await expect(section(page).getByRole("img")).toHaveAttribute("width", size);
    }
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    const fitted = await width(page);
    const most = Math.max(8 * fitted, 8 * Number(size));
    const zoomIn = dialog(page).getByRole("button", { name: "Zoom in" });
    for (let i = 0; i < 80 && (await zoomIn.getAttribute("aria-disabled")) !== "true"; i++) await page.keyboard.press("+");
    await expect(zoomIn).toHaveAttribute("aria-disabled", "true");
    expect(Math.abs((await width(page)) / most - 1)).toBeLessThan(0.001);
    await expect(page.locator("#large-status")).toHaveText(`Zoom ${Math.round((most / fitted) * 100).toLocaleString("en")}%`);
    // One more step changes nothing.
    await page.keyboard.press("+");
    expect(Math.abs((await width(page)) / most - 1)).toBeLessThan(0.001);
  });
}

// DESIGN 8.8: Tab goes round the view's controls and never out of it,
// on a phone too, and also when Zoom in is aria-disabled at the most zoom.
test("Tab goes round the large view's controls and never leaves it, even at the most zoom", async ({ page }) => {
  await openLarge(page);
  const zoomIn = dialog(page).getByRole("button", { name: "Zoom in" });
  for (let i = 0; i < 80 && (await zoomIn.getAttribute("aria-disabled")) !== "true"; i++) await page.keyboard.press("+");
  await expect(zoomIn).toHaveAttribute("aria-disabled", "true");
  const where = () => page.evaluate(() => {
    const a = document.activeElement;
    return { id: a?.id ?? "", inside: Boolean(a?.closest("#large-view")) };
  });
  // Zoom in is aria-disabled but still a stop, and the last one: Tab wraps to the first.
  await zoomIn.focus();
  await page.keyboard.press("Tab");
  const first = await where();
  expect(first.inside).toBe(true);
  expect(first.id).not.toBe("large-in");
  // Shift+Tab from the first leads back to the last.
  await page.keyboard.press("Shift+Tab");
  expect((await where()).id).toBe("large-in");
  // A whole round: focus stays in the view and comes back to Zoom in.
  await page.keyboard.press("Tab");
  const round: string[] = [first.id];
  for (let i = 0; i < 300 && round.at(-1) !== "large-in"; i++) {
    await page.keyboard.press("Tab");
    const at = await where();
    expect(at.inside).toBe(true);
    round.push(at.id);
  }
  expect(round.at(-1)).toBe("large-in");
  expect(round.length).toBeGreaterThanOrEqual(3);
});

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, isMobile: true });

  test("the controls stay shown, are finger-sized, and two fingers pinch while one drags", async ({ page }) => {
    await openLarge(page);
    expect(await page.evaluate(() => matchMedia("(hover: none) and (pointer: coarse)").matches)).toBe(true);
    for (const id of ["large-prev", "large-next", "large-close", "large-in", "large-out"]) {
      expect(await opacity(page, id)).toBe("1");
      const b = (await page.locator(`#${id}`).boundingBox())!;
      expect(Math.min(b.width, b.height)).toBeGreaterThanOrEqual(44);
    }
    await checkPage(page);

    const cdp = await page.context().newCDPSession(page);
    const touch = (type: "touchStart" | "touchMove" | "touchEnd", points: { x: number; y: number }[]) =>
      cdp.send("Input.dispatchTouchEvent", { type, touchPoints: points.map((p, id) => ({ ...p, id })) });
    const fitted = await width(page);
    const mid = await middleOf(page);
    await touch("touchStart", [{ x: mid.x - 20, y: mid.y }, { x: mid.x + 20, y: mid.y }]);
    for (const d of [30, 40, 50, 60]) await touch("touchMove", [{ x: mid.x - d, y: mid.y }, { x: mid.x + d, y: mid.y }]);
    await touch("touchEnd", []);
    // The fingers went from 40 to 120 px apart: three times as big.
    await expect.poll(() => width(page)).toBeCloseTo(fitted * 3, -1);

    const start = await scroll(page);
    await touch("touchStart", [{ x: mid.x, y: mid.y }]);
    for (const d of [10, 20, 30]) await touch("touchMove", [{ x: mid.x - d, y: mid.y - d }]);
    await touch("touchEnd", []);
    const moved = await scroll(page);
    expect(moved.x - start.x).toBeCloseTo(30, 0);
    expect(moved.y - start.y).toBeCloseTo(30, 0);
    // The tapped arrow still moves between planets; a pause first, so the browser does not take it for a double tap.
    await page.waitForTimeout(400);
    await dialog(page).getByRole("button", { name: "Next planet: Vulcanus" }).tap();
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  });
});

test.describe("on a touch screen, without dragging", () => {
  test.use({ hasTouch: true, isMobile: true });

  // WCAG 2.5.7: one tap on a zoomed picture centres the spot tapped.
  test("a tap centres the spot tapped, and a drag still pans", async ({ page }) => {
    await openLarge(page);
    const cdp = await page.context().newCDPSession(page);
    const touch = (type: "touchStart" | "touchMove" | "touchEnd", points: { x: number; y: number }[]) =>
      cdp.send("Input.dispatchTouchEvent", { type, touchPoints: points.map((p, id) => ({ ...p, id })) });
    const mid = await middleOf(page);
    // At the fitted size a tap does nothing.
    await touch("touchStart", [{ x: mid.x + 40, y: mid.y + 30 }]);
    await touch("touchEnd", []);
    expect(await scroll(page)).toEqual({ x: 0, y: 0 });
    for (let i = 0; i < 8; i++) await dialog(page).getByRole("button", { name: "Zoom in" }).tap();
    await page.waitForTimeout(400);
    const start = await scroll(page);
    const spot = await spotAt(page, mid.x + 40, mid.y + 30);
    await touch("touchStart", [{ x: mid.x + 40, y: mid.y + 30 }]);
    await touch("touchMove", [{ x: mid.x + 42, y: mid.y + 31 }]);
    await touch("touchEnd", []);
    const tapped = await scroll(page);
    expect(tapped.x - start.x).toBeCloseTo(40, -1);
    expect(tapped.y - start.y).toBeCloseTo(30, -1);
    const now = await spotAt(page, mid.x, mid.y);
    expect(now.u).toBeCloseTo(spot.u, 2);
    expect(now.v).toBeCloseTo(spot.v, 2);

    await page.waitForTimeout(400);
    await touch("touchStart", [{ x: mid.x, y: mid.y }]);
    for (const d of [10, 20, 30]) await touch("touchMove", [{ x: mid.x - d, y: mid.y - d }]);
    await touch("touchEnd", []);
    const dragged = await scroll(page);
    expect(dragged.x - tapped.x).toBeCloseTo(30, 0);
    expect(dragged.y - tapped.y).toBeCloseTo(30, 0);
  });
});

test.describe("the planet viewed", () => {
  // The settings' rows follow the planet viewed through this event.
  async function recordEvents(page: Page) {
    await page.addInitScript(() => {
      const seen: string[] = [];
      (window as unknown as { seen: string[] }).seen = seen;
      document.addEventListener("preview:planet", (e) => seen.push((e as CustomEvent).detail.planet));
    });
    return () => page.evaluate(() => (window as unknown as { seen: string[] }).seen);
  }

  test("moving in the large view tells the page; opening a row is not told back", async ({ page }) => {
    const seen = await recordEvents(page);
    await openLarge(page);
    expect(await seen()).toEqual([]);
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    expect(await seen()).toEqual(["vulcanus", "nauvis"]);
    await expect(dialog(page).getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
    await page.keyboard.press("Escape");
    await page.locator("#mg-resources-fulgora summary").click();
    await expect(section(page).getByRole("img", { name: /^Map preview of Fulgora/ })).toBeVisible();
    expect(await seen()).toEqual(["vulcanus", "nauvis"]);
  });

  // A tab leaves out a planet with nothing to set there, so the row of the
  // planet before stays open in it. A row opened to follow the planet viewed
  // says so a moment later, when the view may have moved on: it must not
  // pull the view back (DESIGN 8.7).
  test("two quick steps leave the planet viewed where the steps ended", async ({ page }) => {
    await openLarge(page);
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await page.evaluate(() => {
      document.getElementById("large-prev")!.click();
      document.getElementById("large-prev")!.click();
    });
    await expect(page.getByRole("dialog", { name: "Aquilo" })).toBeVisible();
    await page.waitForTimeout(300);
    await expect(page.getByRole("dialog", { name: "Aquilo" })).toBeVisible();
    await expect(page.locator("#large-next-name")).toHaveText("Next planet: Nauvis");
    await expect(page.locator("#open-planet")).toHaveValue("aquilo");
  });

  // The dialog's close event comes a moment after it shuts; a planet opened
  // in that moment is still drawn, and the planet viewed stays the one opened.
  test("a planet opened as the large view closes is drawn", async ({ page }) => {
    await openLarge(page);
    await page.evaluate(() => {
      (document.getElementById("large-view") as HTMLDialogElement).close();
      (document.querySelector("#mg-resources-fulgora summary") as HTMLElement).click();
    });
    await expect(section(page).getByRole("img", { name: /^Map preview of Fulgora/ })).toBeVisible();
    await expect(page.locator("#open-planet")).toHaveValue("fulgora");
  });

  test("the preview starts from the planet open in the settings", async ({ page }) => {
    const seen = await recordEvents(page);
    // Stands in for the settings' hidden field where the server does not write it.
    await page.addInitScript(() => {
      document.addEventListener("DOMContentLoaded", () => {
        let open = document.getElementById("open-planet") as HTMLInputElement | null;
        if (!open) {
          open = document.createElement("input");
          open.type = "hidden";
          open.id = "open-planet";
          document.getElementById("generate-form")!.append(open);
        }
        open.value = "gleba";
      });
    });
    await page.goto("/generate");
    await expect(section(page).getByRole("img", { name: /^Map preview of Gleba/ })).toBeVisible();
    expect(await seen()).toEqual([]);
  });

  // A planet the new settings do not have is never asked for: the preview goes back to one they have.
  for (const change of ["Space Age off", "version 1.1"] as const) {
    test(`after picking Vulcanus, ${change} draws a planet the line has`, async ({ page, fakeGame }) => {
      // The 1.1 game image is here too, so its preview can be drawn.
      await fakeGame!.call("/image", { ref: image21.replace(/:[^:]+$/, ":1.1.110"), on: true });
      await page.goto("/generate");
      await expect(section(page).getByRole("img")).toHaveCount(1);
      await page.locator("#mg-resources-vulcanus summary").click();
      await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
      const replies: { planet: string | null; status: number }[] = [];
      page.on("response", (r) => {
        const q = r.request();
        if (q.method() !== "POST" || !q.url().endsWith("/generate/preview")) return;
        const form = new URLSearchParams(q.postData() ?? "");
        if (form.get("auto") === "1") replies.push({ planet: form.get("planet"), status: r.status() });
      });
      if (change === "Space Age off") {
        await page.getByLabel("Space Age is on").uncheck();
      } else {
        const d = page.locator("details.disclosure");
        if (!(await d.evaluate((e) => (e as HTMLDetailsElement).open))) await d.locator("summary").click();
        await page.getByLabel("Or type a version").fill("1.1.110");
        await page.getByLabel("Or type a version").press("Tab");
        // 1.1 has no Space Age, and the preview says so until it is off.
        await expect(page.locator("#preview").getByText("Space Age needs Factorio 2.0 or later")).toBeVisible();
        await page.getByLabel("Space Age is on").uncheck();
      }
      await expect(section(page).getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
      await page.waitForTimeout(1200);
      expect(replies.length).toBeGreaterThan(0);
      for (const r of replies) {
        expect(r.status).toBe(200);
        expect([null, "nauvis"]).toContain(r.planet);
      }
      await expect(section(page).getByRole("alert")).toHaveCount(0);
    });
  }
});

// The preview picture fills its column, capped by the window's height and centred.
// At 1920 the preview is a column beside the settings, so a short window is
// what caps it there.
for (const [w, h] of [[1280, 1600], [1920, 900], [1920, 400], [320, 640]]) {
  test(`the preview picture fills its column at ${w} by ${h}`, async ({ page }) => {
    await page.setViewportSize({ width: w, height: h });
    await page.goto("/generate");
    const img = section(page).getByRole("img");
    await expect(img).toHaveCount(1);
    const column = (await section(page).locator("ul.pictures").boundingBox())!;
    const box = (await img.boundingBox())!;
    const cap = Math.max(256, h - 2 * 44 - 16);
    expect(box.width).toBeCloseTo(Math.min(column.width, cap), 0);
    expect(box.height).toBeCloseTo(box.width, 0);
    expect(box.x + box.width / 2).toBeCloseTo(column.x + column.width / 2, 0);
    if (w === 1280) expect(box.width).toBeCloseTo(column.width, 0);
    if (w === 1920 && h === 400) expect(box.width).toBeLessThan(column.width);
    expect(await img.evaluate((i) => getComputedStyle(i).imageRendering)).toBe("pixelated");
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(w);
  });
}

// SPEC 5.6: Enter in a field of the settings beside the large map does
// nothing; the value commits when the field is left, once. Enter in a field of
// the main form outside the large view still generates the world, and Enter on
// a button in the view still presses it.
test.describe("Enter in the large view's settings", () => {
  test.beforeEach(({ page }) => test.skip((page.viewportSize()?.width ?? 0) < 1240, "the settings show beside the map only where both fit"));

  const worlds = (foreman: { dataDir: string }) => {
    try {
      return readdirSync(join(foreman.dataDir, "worlds")).sort();
    } catch {
      return [];
    }
  };

  // The large view holds only sliders and selects today. A text field there
  // is the form's own decimal field moved in, as preview.js moves a planet's
  // settings, so the test moves one in the same way.
  test("does not create the world, and the value is kept", async ({ page, foreman }) => {
    await page.goto("/generate");
    await page.getByLabel("World name").fill("Not yet");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    const id = await page.locator('#generate-form input[type="text"][inputmode="decimal"][name]').first().getAttribute("id");
    expect(id).toBeTruthy();
    await page.evaluate((i) => {
      const panel = document.getElementById("large-settings")!;
      panel.hidden = false;
      panel.append(document.getElementById(i)!);
    }, id!);
    const decimal = page.locator(`#large-settings #${id}`);
    await expect(decimal).toBeVisible();
    const before = worlds(foreman);
    const posts: string[] = [];
    page.on("request", (r) => { if (r.method() === "POST" && r.url().endsWith("/generate")) posts.push(r.url()); });
    const changes = await decimal.evaluate((i) => {
      const seen: string[] = [];
      (window as unknown as { changes: string[] }).changes = seen;
      i.addEventListener("change", () => seen.push((i as HTMLInputElement).value));
      return seen.length;
    });
    expect(changes).toBe(0);
    await decimal.fill("1.5");
    await decimal.press("Enter");
    await page.waitForTimeout(800);
    await expect(page).toHaveURL("/generate");
    await expect(dialog(page)).toBeVisible();
    await expect(decimal).toBeFocused();
    await expect(decimal).toHaveValue("1.5");
    expect(await page.evaluate(() => (window as unknown as { changes: string[] }).changes)).toEqual([]);
    await decimal.blur();
    expect(await page.evaluate(() => (window as unknown as { changes: string[] }).changes)).toEqual(["1.5"]);
    expect(posts).toEqual([]);
    expect(worlds(foreman)).toEqual(before);
  });

  test("does not create the world from a slider, a box or a list there", async ({ page, foreman }) => {
    await openLarge(page);
    const before = worlds(foreman);
    const posts: string[] = [];
    page.on("request", (r) => { if (r.method() === "POST" && r.url().endsWith("/generate")) posts.push(r.url()); });
    // Each kind of control shown there: the sliders stand in for most lists.
    for (const kind of ['input[type="range"]', 'input[type="checkbox"]', "select"]) {
      const control = page.locator(`#large-settings ${kind}`).filter({ visible: true }).first();
      if (kind === 'input[type="range"]') await expect(control).toBeVisible();
      if (await control.count()) await control.press("Enter");
    }
    await page.waitForTimeout(800);
    await expect(page).toHaveURL("/generate");
    await expect(dialog(page)).toBeVisible();
    expect(posts).toEqual([]);
    expect(worlds(foreman)).toEqual(before);
  });

  test("still presses a button there", async ({ page }) => {
    await openLarge(page);
    await page.locator("#large-next").press("Enter");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
  });
});

test("Enter in a field of the main form, outside the large view, still generates the world", async ({ page }) => {
  await page.goto("/generate");
  await expect(section(page).getByRole("img")).toHaveCount(1);
  await page.getByLabel("World name").fill("Plain enter");
  await page.getByLabel("World name").press("Enter");
  await expect(page).toHaveURL("/worlds/plain-enter?generated=1");
});
