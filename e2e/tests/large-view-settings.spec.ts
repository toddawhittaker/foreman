import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Page, Request } from "@playwright/test";

// SPEC 5.6, 17; DESIGN 8.8: on a screen wide enough for both, the
// large view holds the viewed planet's Resources and Terrain settings to the
// left of the map. They are the form's own fields, moved there while the view
// is open, so a change redraws the map after the page's own pause and is
// still there once the view closes. Focus decides who owns the arrow keys.
// Narrower, the view stays map only.
test.use({ fakeGameOn: true });

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const dialog = (page: Page) => page.locator("#large-view");
const frame = (page: Page) => page.locator("#large-frame");
const panel = (page: Page) => page.locator("#large-settings");
const status = (page: Page) => page.locator("#large-status");
const field = (r: Request, name: string) => new URLSearchParams(r.postData() ?? "").get(name) ?? undefined;
const isAuto = (r: Request) => r.method() === "POST" && r.url().endsWith("/generate/preview") && field(r, "auto") === "1";
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png` });
// The view needs 77rem; the desktop projects are 1280 wide, the phone ones 320.
const isWide = (page: Page) => (page.viewportSize()?.width ?? 0) >= 1240;
const keysInSetting = "In a setting, the keys change the setting: an arrow moves a slider, not the map.";

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

// The id of a planet's first setting of steps in a tab, read from its row on the page.
const firstSelect = (page: Page, tab: string, planet: string) =>
  page.locator(`#mg-${tab}-${planet} .step select[name]`).first().getAttribute("id");

test.describe("on a wide screen", () => {
  test.beforeEach(({ page }) => test.skip(!isWide(page), "the settings show beside the map only where both fit"));

  test("the planet's Resources and Terrain settings sit left of the map, and are the form's own fields", async ({ page }) => {
    await page.goto("/generate");
    const iron = (await firstSelect(page, "resources", "nauvis"))!;
    await openLarge(page);
    await expect(panel(page).locator(`#${iron}`)).toHaveCount(1);
    await expect(panel(page)).toBeVisible();
    await expect(panel(page).getByRole("heading", { level: 3, name: "Resources" })).toBeVisible();
    await expect(panel(page).getByRole("heading", { level: 3, name: "Terrain" })).toBeVisible();
    // One copy: the field is in the view, inside the form, and no longer in its row.
    await expect(page.locator(`[id="${iron}"]`)).toHaveCount(1);
    expect(await page.locator(`#${iron}`).evaluate((s) => Boolean(s.closest("#generate-form #large-view")))).toBe(true);
    await expect(page.locator(`#mg-resources-nauvis #${iron}`)).toHaveCount(0);
    // Left of the map, and the map no narrower than the settings.
    const p = (await panel(page).boundingBox())!;
    const f = (await frame(page).boundingBox())!;
    expect(p.x + p.width).toBeLessThanOrEqual(f.x);
    expect(f.width).toBeGreaterThanOrEqual(p.width);
    // Wide enough for sliders, as the page has them.
    await expect(page.locator(`#${iron}-range`)).toBeVisible();
    await expect(page.locator(`#${iron}`)).toBeHidden();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
    await shot(page, "large-view-settings");
    await checkPage(page);
    // Closing puts the field back in its row.
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await expect(page.locator(`#mg-resources-nauvis #${iron}`)).toHaveCount(1);
    await expect(page.locator(`[id="${iron}"]`)).toHaveCount(1);
  });

  test("a change there redraws the map after the pause, says so, and stays once the view closes", async ({ page }) => {
    await openLarge(page);
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    const select = page.locator("#large-resources .step select[name]").first();
    const id = (await select.getAttribute("id"))!;
    const name = (await select.getAttribute("name"))!;
    const before = await select.inputValue();
    const oldSrc = await page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).src);
    const range = page.locator(`#${id}-range`);
    // The redraw is held until its state has been seen.
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/preview", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await range.focus();
    // The arrows belong to the slider: it moves, and the view stays on Nauvis.
    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await expect(range).toBeFocused();
    const after = await select.inputValue();
    expect(after).not.toBe(before);
    // Zoom keys belong to the field too.
    await page.keyboard.press("+");
    await expect(status(page)).not.toHaveText("Zoom 115%");
    // The slow redraw shows as on the page: the picture fades and a line, not read out, says so; then the map is ready.
    await expect(frame(page)).toHaveAttribute("aria-busy", "true");
    const drawing = page.locator("#large-drawing");
    await expect(drawing).toHaveText("Drawing the map preview for the current settings…");
    await expect(drawing).toBeVisible();
    expect(await drawing.evaluate((p) => p.closest("[role=status], [aria-live]"))).toBeNull();
    await expect(status(page)).not.toHaveText(/Drawing/);
    release();
    await expect(drawing).toHaveText("");
    await expect(status(page)).toHaveText("Map preview ready.");
    await expect(frame(page)).toHaveAttribute("aria-busy", "false");
    // One request for the two steps, after the pause, with the new value.
    await page.waitForTimeout(1200);
    expect(sent.length).toBe(1);
    expect(field(sent[0], name)).toBe(after);
    expect(field(sent[0], "planet")).toBe("nauvis");
    // The view shows the new picture.
    const shown = await page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).src);
    expect(shown).not.toBe(oldSrc);
    expect(shown).toBe(await section(page).locator("a.picture img").evaluate((i) => (i as HTMLImageElement).src));
    await expect(range).toBeFocused();

    // Escape closes the view from the slider; the change is the page's.
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await expect(section(page).getByRole("link", { name: /Map preview of Nauvis/ })).toBeFocused();
    await expect(page.locator(`#mg-resources-nauvis #${id}`)).toHaveValue(after);
    await expect(page.locator("#preset-modified")).toHaveText("[modified]");
    // Reset and Undo reach it as any other setting.
    await page.getByRole("button", { name: "Reset to defaults" }).click();
    await expect(page.locator(`#${id}`)).toHaveValue(before);
    await page.getByRole("button", { name: "Undo reset" }).click();
    await expect(page.locator(`#${id}`)).toHaveValue(after);
  });

  test("a change in the view counts as unsaved, so leaving asks first", async ({ page }) => {
    const asked: string[] = [];
    page.on("dialog", async (d) => {
      asked.push(d.type());
      await d.dismiss();
    });
    await openLarge(page);
    await page.locator("#large-terrain .step__range").first().focus();
    await page.keyboard.press("ArrowLeft");
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    await page.getByRole("link", { name: "Cancel" }).click();
    await expect(page.getByRole("dialog", { name: "Leave without saving these map settings?" })).toBeVisible();
    await page.getByRole("button", { name: "Stay on this page" }).click();
    expect(asked).toEqual([]);
    await expect(page).toHaveURL("/generate");
  });

  test("another planet viewed brings its own settings", async ({ page }) => {
    await page.goto("/generate");
    const nauvis = (await firstSelect(page, "resources", "nauvis"))!;
    const vulcanus = (await firstSelect(page, "resources", "vulcanus"))!;
    const gleba = (await firstSelect(page, "terrain", "gleba"))!;
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(panel(page).locator(`#${nauvis}`)).toHaveCount(1);
    // The arrows on the map still step between planets.
    await expect(frame(page)).toBeFocused();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await expect(panel(page).locator(`#${vulcanus}`)).toHaveCount(1);
    await expect(panel(page).locator(`#${nauvis}`)).toHaveCount(0);
    await expect(page.locator(`#mg-resources-nauvis #${nauvis}`)).toHaveCount(1);
    await dialog(page).getByRole("button", { name: "Next planet: Gleba" }).click();
    await expect(page.getByRole("dialog", { name: "Gleba" })).toBeVisible();
    await expect(panel(page).locator(`#${gleba}`)).toHaveCount(1);
    await expect(page.locator("#open-planet")).toHaveValue("gleba");
    await dialog(page).getByRole("button", { name: "Close" }).click();
    await expect(page.locator(`#mg-terrain-gleba #${gleba}`)).toHaveCount(1);
    await expect(page.locator(`#mg-resources-vulcanus #${vulcanus}`)).toHaveCount(1);
  });

  // A settings redraw on its way when the view opens: the view takes the redrawn fields once it lands, so there is one copy of each.
  test("opening the view while the settings are redrawn shows the new fields, one copy of each", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/refresh", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await page.locator("#generate-preset").selectOption("rich-resources");
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(dialog(page)).toBeVisible();
    release();
    await expect(panel(page).locator(".step select[name]").first()).toBeAttached();
    const twice = await panel(page).locator("[id]").evaluateAll((els) => els.map((e) => e.id).filter((id) => document.querySelectorAll(`[id="${id}"]`).length !== 1));
    expect(twice).toEqual([]);
    // An edit there reaches the next preview and stays once the view closes.
    const select = panel(page).locator(".step select[name]").first();
    const id = (await select.getAttribute("id"))!;
    const name = (await select.getAttribute("name"))!;
    await page.locator(`#${id}-range`).focus();
    await page.keyboard.press("ArrowLeft");
    const after = await select.inputValue();
    await expect.poll(() => sent.some((r) => field(r, name) === after)).toBe(true);
    await page.keyboard.press("Escape");
    await expect(page.locator(`#mg-resources-nauvis #${id}`)).toHaveValue(after);
    await expect(page.locator(`[id="${id}"]`)).toHaveCount(1);
  });

  // Every way the view closes puts the fields back, not only Close and Escape. The view fills the
  // screen, so there is no backdrop to click; a script's close stands in for any other way.
  test("closing the view another way puts the fields back", async ({ page }) => {
    await page.goto("/generate");
    const iron = (await firstSelect(page, "resources", "nauvis"))!;
    await openLarge(page);
    await expect(panel(page).locator(`#${iron}`)).toHaveCount(1);
    await page.evaluate(() => (document.getElementById("large-view") as HTMLDialogElement).close());
    await expect(dialog(page)).toBeHidden();
    await expect(page.locator(`#mg-resources-nauvis #${iron}`)).toHaveCount(1);
  });

  // The page around the dialog is inert, so a warning is said in the view's own status line.
  test("a warning a change in the view brings is still said", async ({ page }) => {
    await openLarge(page);
    await page.evaluate(() => {
      const seen: string[] = [];
      (window as unknown as { seen: string[] }).seen = seen;
      const s = document.getElementById("large-status")!;
      new MutationObserver(() => { if (s.textContent) seen.push(s.textContent); }).observe(s, { childList: true, characterData: true, subtree: true });
    });
    const seen = () => page.evaluate(() => (window as unknown as { seen: string[] }).seen);
    const box = panel(page).locator("input[type=checkbox][data-need]").first();
    const need = (await box.getAttribute("data-need"))!;
    await box.uncheck();
    await expect.poll(seen).toContain(need);
    await expect.poll(seen).toContain("Map preview ready.");
    expect((await seen()).filter((t) => t.startsWith("Drawing"))).toEqual([]);
  });

  // A field that the view hides as it narrows does not keep the focus.
  test("narrowing the view with a settings field focused moves focus to the map", async ({ page }) => {
    await openLarge(page);
    const range = panel(page).locator(".step__range").first();
    await range.focus();
    await page.setViewportSize({ width: 1000, height: 700 });
    await expect(panel(page)).toBeHidden();
    await expect(frame(page)).toBeFocused();
  });

  // SPEC 17.10: looking at a planet in the large view, change its setting and see the new map.
  test("changing the setting of the planet in the large view takes one click", async ({ page }) => {
    await openLarge(page);
    const clicks = await countClicks(page);
    await clicks.reset();
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    const range = page.locator("#large-resources .step__range").first();
    const box = (await range.boundingBox())!;
    await range.click({ position: { x: box.width * 0.9, y: box.height / 2 } });
    await expect(status(page)).toHaveText("Map preview ready.");
    expect(sent.length).toBe(1);
    await expectWithin(clicks, 1, "Change the setting of the planet in the large view and see it");
  });

  // DESIGN 8.8: the column's headings read as headings, a step above the fields' names,
  // and the column says that a setting keeps its own keys.
  test("the settings' headings are set as headings, and the column says a setting keeps its keys", async ({ page }) => {
    await openLarge(page);
    const heading = panel(page).getByRole("heading", { level: 3, name: "Resources" });
    const look = await heading.evaluate((h) => { const s = getComputedStyle(h); return { size: s.fontSize, weight: s.fontWeight }; });
    expect(look).toEqual({ size: "18px", weight: "600" });
    // A step above the groups' names inside the column.
    const group = await panel(page).locator("legend").first().evaluate((l) => parseFloat(getComputedStyle(l).fontSize));
    expect(group).toBeLessThan(18);
    // At the column's top, above its headings; the map's own key help is left as it is.
    const hint = panel(page).getByText(keysInSetting);
    await expect(hint).toBeVisible();
    expect((await hint.boundingBox())!.y).toBeLessThan((await heading.boundingBox())!.y);
    await expect(page.locator("#large-keys")).not.toContainText(keysInSetting);
  });

  // The view is a modal dialog: Tab goes through its controls in the order they are
  // seen, the settings, then the map and the controls over it, and never leaves it.
  test("Tab moves through the view in order and stays inside it", async ({ page }) => {
    await openLarge(page);
    await expect(frame(page)).toBeFocused();
    // Each stop's place in the document, or -1 when focus is outside the dialog.
    const where = () => page.evaluate(() => {
      const a = document.activeElement;
      const d = document.getElementById("large-view")!;
      if (!a || !d.contains(a)) return { at: -1, id: a ? a.id || a.tagName : "" };
      const all = [...d.querySelectorAll("*")];
      return { at: all.indexOf(a), id: a.matches(".step__range") ? "a slider" : a.id || a.className };
    });
    const start = await where();
    const seen: { at: number; id: string }[] = [];
    for (let i = 0; i < 400; i++) {
      await page.keyboard.press("Tab");
      const now = await where();
      expect(now.at, `Tab ${i + 1} left the view for ${now.id}`).toBeGreaterThanOrEqual(0);
      seen.push(now);
      if (now.at === start.at) break;
    }
    expect(seen.at(-1)!.at, "Tab came back round to the map").toBe(start.at);
    // In document order, wrapping once from the last control to the first.
    const drops = seen.filter((s, i) => i > 0 && s.at < seen[i - 1].at).length;
    expect(drops).toBe(1);
    const ids = seen.map((s) => s.id);
    for (const id of ["large-prev", "large-next", "large-close", "large-out", "large-in", "large-download", "large-amounts"]) expect(ids).toContain(id);
    expect(ids).toContain("a slider");
    // From the map: the controls over it, then round to the bar, and the settings before the map.
    const at = (id: string) => ids.indexOf(id);
    expect(at("large-in")).toBeLessThan(at("large-download"));
    expect(at("large-amounts")).toBeLessThan(at("a slider"));
    expect(at("large-prev")).toBeLessThan(at("large-next"));
    expect(at("large-close")).toBeLessThan(at("large-in"));
    // Shift+Tab from the first control goes round to the last.
    await page.locator("#large-download").focus();
    await page.keyboard.press("Shift+Tab");
    await expect(page.locator("#large-in")).toBeFocused();
  });

  // DESIGN 7.7, 8.8: Escape closes the open info tip first; the next one closes the view.
  test("Escape on an info tip in the settings closes the tip, then the view", async ({ page }) => {
    await openLarge(page);
    // No Resources or Terrain setting has a tip yet, so Enemy's starting area lends its field.
    await page.evaluate(() => {
      const field = document.querySelector("#panel-enemy button.tip")!.closest(".field")!;
      document.getElementById("large-resources")!.append(field);
    });
    const tip = panel(page).locator("button.tip").first();
    await tabTo(page, tip);
    await expect(tip).toHaveAttribute("aria-expanded", "true");
    const text = page.locator(`[id="${await tip.getAttribute("aria-controls")}"]`);
    await expect(text).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(tip).toHaveAttribute("aria-expanded", "false");
    await expect(text).toBeHidden();
    await expect(dialog(page)).toBeVisible();
    await expect(tip).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
  });
});

test("where the settings and the map do not both fit, the view is map only", async ({ page }) => {
  if (isWide(page)) await page.setViewportSize({ width: 1100, height: 760 });
  await openLarge(page);
  await expect(panel(page)).toBeHidden();
  // With no settings beside the map, the key help does not speak of them.
  await expect(page.getByText(keysInSetting)).toBeHidden();
  const d = (await dialog(page).boundingBox())!;
  const f = (await frame(page).boundingBox())!;
  // The frame takes the dialog's width less its padding.
  expect(f.width).toBeGreaterThan(d.width - 40);
  await shot(page, "large-view-narrow");
  await checkPage(page);
  // The fields are back in their rows once it closes.
  await page.keyboard.press("Escape");
  await expect(page.locator("#mg-resources-nauvis .step select[name]").first()).toBeAttached();
  await expect(page.locator("#large-settings select")).toHaveCount(0);
});
