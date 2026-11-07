import { test, expect, checkPage, createAdmin, seedWorld, image21, tabTo } from "./foreman.js";
import type { Page, Request } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, DESIGN 8.8: the map preview draws by itself, for the planet
// being viewed, from the current unsaved settings; the newest request wins;
// the large view opens in the page.
test.use({ fakeGameOn: true });

type Hooks = { call: (p: string, b: Record<string, unknown>) => Promise<void>; state: () => Promise<{ calls: string[] }> };

async function setUp(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: Hooks) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const pictures = (page: Page) => section(page).getByRole("img");

// The automatic requests the page sends, as they go out.
function autoRequests(page: Page) {
  const sent: Request[] = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().endsWith("/generate/preview") && field(r, "auto") === "1") sent.push(r);
  });
  return sent;
}
const field = (r: Request, name: string) => new URLSearchParams(r.postData() ?? "").get(name) ?? undefined;

// Preview containers made so far: each run creates one.
async function runs(fakeGame: Hooks) {
  return (await fakeGame.state()).calls.filter((c) => c.includes("/containers/create")).length;
}

test.describe("with script", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
  });

  test("draws Nauvis on load, keeps the seed it used, and says the settings are unsaved", async ({ page }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Nauvis, seed \d+$/);
    const seed = await page.locator("#generate-seed").inputValue();
    expect(seed).toMatch(/^\d+$/);
    await expect(section(page)).toContainText(`Drawn from the current settings, which are not saved yet, with seed ${seed}.`);
    await expect(section(page)).toContainText("The pin marks the centre of the map, where players start.");
    // The pin is drawn over the picture and hidden from screen readers.
    await expect(section(page).locator("svg.pin")).toHaveAttribute("aria-hidden", "true");
    await expect(page.getByLabel("Planet")).toHaveValue("nauvis");
    await expect(page.getByLabel("Planet").locator("option")).toHaveCount(5);
    const link = section(page).getByRole("link", { name: /Map preview of Nauvis, seed \d+ \(opens the large view\)/ });
    await expect(link).toBeVisible();
    // The page load does not announce anything or move focus.
    await expect(page.locator("#announcer")).toHaveText("");
    await checkPage(page);
  });

  test("typing a seed sends one request after the pause, and older replies are dropped", async ({ page }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    const sent = autoRequests(page);
    const seed = page.locator("#generate-seed");
    await seed.fill("");
    await seed.pressSequentially("424242", { delay: 60 });
    await expect(pictures(page)).toHaveAccessibleName("Map preview of Nauvis, seed 424242");
    await expect(page.locator("#announcer")).toHaveText("Map preview ready.");
    // The first request goes out after the pause, and only once.
    await page.waitForTimeout(1200);
    expect(sent.map((r) => field(r, "seed"))).toEqual(["424242"]);
    expect(field(sent[0], "planet")).toBeUndefined();
    await expect(seed).toBeFocused();
  });

  test("a change mid-run drops the running request; the newest one wins", async ({ page, fakeGame }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    await fakeGame!.call("/preview-mode", { mode: "slow" });
    const failed: string[] = [];
    page.on("requestfailed", (r) => { if (r.url().endsWith("/generate/preview")) failed.push(field(r, "seed") ?? ""); });
    const seed = page.locator("#generate-seed");
    const first = page.waitForRequest((r) => r.url().endsWith("/generate/preview") && field(r, "seed") === "111");
    await seed.fill("111");
    await first;
    // The first run is under way (the slow game takes half a second); change again.
    await expect(page.locator("#preview-status")).toHaveText("Drawing the map preview for the current settings…");
    await seed.fill("222");
    await expect(pictures(page)).toHaveAccessibleName("Map preview of Nauvis, seed 222");
    expect(failed).toEqual(["111"]);
    // The dropped run never shows, even if it ends later.
    await page.waitForTimeout(800);
    await expect(pictures(page)).toHaveAccessibleName("Map preview of Nauvis, seed 222");
    await expect(page.locator("#preview-status")).toHaveText("");
  });

  test("a setting change in any part of the form redraws, but the name does not", async ({ page }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    const sent = autoRequests(page);
    await page.getByLabel("World name").fill("Only a name");
    await page.waitForTimeout(1200);
    expect(sent).toHaveLength(0);
    await page.locator("#mg-nauvis-iron-ore-size").evaluate((s) => {
      (s as HTMLSelectElement).value = "200";
      s.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await expect.poll(() => sent.length).toBe(1);
  });

  test("picking a planet draws only that planet, and going back reuses the stored picture", async ({ page, fakeGame }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    const sent = autoRequests(page);
    const before = await runs(fakeGame!);
    const planet = page.getByLabel("Planet");
    await planet.focus();
    await planet.selectOption("vulcanus");
    await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Vulcanus, seed \d+$/);
    await expect(pictures(page)).toHaveCount(1);
    expect(sent.map((r) => field(r, "planet"))).toEqual(["vulcanus"]);
    // An automatic request never moves focus, and keeping it on the choice is not a move.
    await expect(page.getByLabel("Planet")).toBeFocused();
    await expect(page.locator("#announcer")).toHaveText("Map preview ready.");
    expect(await runs(fakeGame!)).toBe(before + 1);

    await page.getByLabel("Planet").selectOption("nauvis");
    await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Nauvis, seed \d+$/);
    expect(await runs(fakeGame!)).toBe(before + 1);
  });

  // SPEC 5.6, DESIGN 8.7, 8.8: the preview's size is a slider on a wide panel
  // and a select on a phone; steps of one change send one request after the
  // pause; the picture is drawn at the size; and the size survives a form
  // that comes back with an error.
  test("the preview size redraws once after the pause and is kept over an error", async ({ page }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    const select = page.locator("#generate-preview-size");
    const img = pictures(page);
    await expect(select).toHaveValue("1280");
    await expect(select.locator("option")).toHaveCount(25);
    await expect(select.locator("option").first()).toHaveText("1024 × 1024 tiles");
    await expect(select.locator("option:checked")).toHaveText("1280 × 1280 tiles (default)");
    await expect(select.locator("option").last()).toHaveText("4096 × 4096 tiles");
    await expect(page.locator("#generate-preview-size-hint")).toContainText("Big sizes show their detail in the large view.");
    await expect(img).toHaveAttribute("width", "1280");
    await img.scrollIntoViewIfNeeded(); // a picture below the fold is not loaded yet
    await expect.poll(() => img.evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBe(1280);

    const sent = autoRequests(page);
    if ((page.viewportSize()?.width ?? 1280) > 600) {
      const slider = page.locator("#generate-preview-size-range");
      await expect(slider).toBeVisible();
      await expect(select).toBeHidden();
      await expect(slider).toHaveAccessibleName("Preview size");
      await expect(page.locator("#generate-preview-size-value")).toHaveText("1280 × 1280 tiles (default)");
      await slider.focus();
      // Three steps in a row are one request, not three.
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("ArrowRight");
      await page.keyboard.press("ArrowRight");
      await expect(select).toHaveValue("1664");
      await expect(page.locator("#generate-preview-size-value")).toHaveText("1664 × 1664 tiles");
    } else {
      await expect(select).toBeVisible();
      await expect(page.locator("#generate-preview-size-range")).toBeHidden();
      await select.selectOption("1664");
    }
    await expect(img).toHaveAttribute("width", "1664");
    await img.scrollIntoViewIfNeeded(); // a picture below the fold is not loaded yet
    await expect.poll(() => img.evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBe(1664);
    await expect(page.locator("#announcer")).toHaveText("Map preview ready.");
    await page.waitForTimeout(1200);
    expect(sent.map((r) => field(r, "preview_size"))).toEqual(["1664"]);

    // The largest size draws too.
    await select.selectOption("4096", { force: true });
    await expect(img).toHaveAttribute("width", "4096");
    await img.scrollIntoViewIfNeeded(); // a picture below the fold is not loaded yet
    await expect.poll(() => img.evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBe(4096);

    // With no name the form comes back with its error, the size kept.
    await page.getByLabel("World name").press("Enter");
    await expect(page.locator("#error-summary")).toBeFocused();
    await expect(page.locator("#generate-preview-size")).toHaveValue("4096");
  });

  test("the large view works by keyboard alone at 320 px", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    const link = section(page).getByRole("link", { name: /opens the large view/ });
    await tabTo(page, link);
    await page.keyboard.press("Enter");
    const dialog = page.locator("#large-view");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await expect(dialog.getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
    // Opened from the keyboard, focus is on the picture and the controls over it show.
    await expect(dialog.locator("#large-frame")).toBeFocused();
    await expect.poll(() => dialog.locator("#large-close").evaluate((b) => getComputedStyle(b).opacity)).toBe("1");
    await checkPage(page);

    // Next draws Vulcanus, which was not drawn yet, and shows it in place.
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await expect(dialog.getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    // Previous from Nauvis wraps to the last planet.
    await dialog.getByRole("button", { name: "Previous planet: Aquilo" }).press("Enter");
    await expect(page.getByRole("dialog", { name: "Aquilo" })).toBeVisible();
    await dialog.getByRole("button", { name: "Next planet: Nauvis" }).press("Enter");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    // Its news is said before the zoom is, so the two do not cross.
    await expect(dialog.locator("#large-status")).toHaveText("Nauvis");

    // Zoom scales the picture with sharp pixels.
    const width = () => dialog.locator("#large-img").evaluate((i) => i.getBoundingClientRect().width);
    const fitted = await width();
    await page.keyboard.press("+");
    await expect(dialog.locator("#large-status")).toHaveText("Zoom 115%");
    expect(await width()).toBeCloseTo(fitted * 1.15, 0);
    await dialog.getByRole("button", { name: "Zoom in" }).press("Enter");
    await expect(dialog.locator("#large-status")).toHaveText("Zoom 132%");
    await page.keyboard.press("-");
    await expect(dialog.locator("#large-status")).toHaveText("Zoom 115%");
    expect(await dialog.locator("#large-img").evaluate((i) => getComputedStyle(i).imageRendering)).toBe("pixelated");
    // Nothing scrolls sideways on the page itself.
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(section(page).getByRole("link", { name: /Map preview of Nauvis/ })).toBeFocused();

    // Close by its button too, from Vulcanus: focus lands on the picture shown.
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await expect(dialog.getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
    await page.getByRole("button", { name: "Close" }).press("Enter");
    await expect(section(page).getByRole("link", { name: /Map preview of Vulcanus/ })).toBeFocused();
  });

  test("downloads one planet as a PNG and every drawn planet as a zip", async ({ page }) => {
    await page.goto("/generate");
    await page.getByLabel("World name").fill("Mega base");
    await expect(pictures(page)).toHaveCount(1);
    const seed = await page.locator("#generate-seed").inputValue();
    await page.getByLabel("Planet").selectOption("gleba");
    await expect(pictures(page)).toHaveAccessibleName(/Gleba/);

    const png = section(page).getByRole("link", { name: "Download Gleba (PNG)" });
    await expect(png).toHaveAttribute("download", "");
    const [one] = await Promise.all([page.waitForEvent("download"), png.click()]);
    expect(one.suggestedFilename()).toBe(`Mega_base-${seed}-gleba.png`);

    const [all] = await Promise.all([page.waitForEvent("download"), section(page).getByRole("link", { name: "Download all drawn planets (zip)" }).click()]);
    expect(all.suggestedFilename()).toBe(`Mega_base-${seed}.zip`);
    const zip = await page.request.get(await section(page).getByRole("link", { name: "Download all drawn planets (zip)" }).getAttribute("href") ?? "");
    const body = (await zip.body()).toString("latin1");
    // Both planets drawn so far are in it, each named as its own download.
    expect(body).toContain(`Mega_base-${seed}-nauvis.png`);
    expect(body).toContain(`Mega_base-${seed}-gleba.png`);
  });

  test("a box with data-disables turns its element off and redraws the preview", async ({ page }) => {
    await page.goto("/generate");
    await expect(pictures(page)).toHaveCount(1);
    // A minimal stand-in for a resource's on and off box and its settings.
    await page.locator("#map-settings").evaluate((el) => {
      el.insertAdjacentHTML("beforeend", '<label class="check"><input type="checkbox" id="t-on" data-disables="t-group" checked> On</label><fieldset id="t-group"><legend>Group</legend><input id="t-inner" aria-label="Inner"></fieldset>');
    });
    const sent = autoRequests(page);
    await page.locator("#t-on").uncheck();
    await expect(page.locator("#t-inner")).toBeDisabled();
    await page.locator("#t-on").check();
    await expect(page.locator("#t-inner")).toBeEnabled();
    await expect.poll(() => sent.length).toBe(1);
  });
});

// Enter in a text field presses Generate world, never the hidden preset reset.
test("Enter in the World name field generates the world with the changed settings", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  const refreshes: string[] = [];
  page.on("request", (r) => { if (r.url().endsWith("/generate/refresh")) refreshes.push(r.url()); });
  await page.locator("#mg-nauvis-iron-ore-size").evaluate((s) => {
    (s as HTMLSelectElement).value = "200";
    s.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await expect(page.locator("#preset-modified")).toHaveText("[modified]");
  // With no name the form comes back with its error, the change kept.
  await page.getByLabel("World name").press("Enter");
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.locator("#mg-nauvis-iron-ore-size")).toHaveValue("200");
  await page.getByLabel("World name").fill("Enter key");
  await page.getByLabel("World name").press("Enter");
  await expect(page).toHaveURL("/worlds/enter-key?generated=1");
  expect(refreshes).toEqual([]);
  const mapGen = JSON.parse(readFileSync(join(foreman.dataDir, "worlds", "enter-key", "config", "map-gen-settings.json"), "utf8"));
  expect(mapGen.autoplace_controls["iron-ore"].size).toBe(2);
});

// Without script the page works as before: Preview map draws every planet
// and each picture link opens the picture itself.
test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("Preview map shows every planet and a link opens the picture", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/generate");
    await expect(section(page)).toHaveCount(0);
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect(pictures(page)).toHaveCount(5);
    await expect(page.getByLabel("Planet")).toHaveCount(0);
    const link = section(page).getByRole("link", { name: /^Map preview of Nauvis, seed \d+ \(opens the full-size picture\)$/ });
    const href = await link.getAttribute("href");
    await link.click();
    await expect(page).toHaveURL(href!);
  });
});
