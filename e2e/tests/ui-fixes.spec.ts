import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, openMenu } from "./foreman.js";
import type { Page, Request } from "@playwright/test";

// Fixes from the UI batch's reviews: the map preview's large view and its
// announcements (SPEC 5.6, 17; DESIGN 8.8), the world tabs (DESIGN 6), and
// the names, headings and spacing of a few controls (SPEC 17.1).

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const dialog = (page: Page) => page.locator("#large-view");
const tab = (page: Page, name: string) => page.locator("label.tab", { hasText: new RegExp(`^\\s*${name}\\s*$`) });
const field = (r: Request, name: string) => new URLSearchParams(r.postData() ?? "").get(name) ?? undefined;
const isAuto = (r: Request) => r.method() === "POST" && r.url().endsWith("/generate/preview") && field(r, "auto") === "1";

// Every text #announcer held, in order, so a sentence replaced quickly is still seen.
async function recordAnnouncer(page: Page) {
  await page.evaluate(() => {
    const seen: string[] = [];
    (window as unknown as { seen: string[] }).seen = seen;
    const a = document.getElementById("announcer")!;
    new MutationObserver(() => { if (a.textContent) seen.push(a.textContent); }).observe(a, { childList: true, characterData: true, subtree: true });
  });
  return () => page.evaluate(() => (window as unknown as { seen: string[] }).seen);
}

test.describe("Generate", () => {
  test.use({ fakeGameOn: true });
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
  });

  // SPEC 5.6: a resource that is off sends none of its settings, so its fields are off too, at once.
  test("unticking a resource turns its fields off and ticking it turns them on", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Resources").click();
    const iron = page.getByRole("checkbox", { name: "Iron ore", exact: true });
    const group = page.locator("#mg-nauvis-iron-ore");
    const fields = group.locator("select, input[type=range]");
    expect(await fields.count()).toBeGreaterThan(0);
    for (const f of await fields.all()) await expect(f).toBeEnabled();
    await iron.uncheck();
    for (const f of await fields.all()) await expect(f).toBeDisabled();
    // The box itself stays usable: it is in the group's legend.
    await expect(iron).toBeEnabled();
    await iron.check();
    for (const f of await fields.all()) await expect(f).toBeEnabled();
    // The form shown again with an error keeps the unticked resource's fields off.
    await iron.uncheck();
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page.locator("#error-summary")).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "Iron ore", exact: true })).not.toBeChecked();
    for (const f of await page.locator("#mg-nauvis-iron-ore").locator("select, input[type=range]").all()) await expect(f).toBeDisabled();
  });

  // DESIGN 8.8: the first picture's space is held, so it does not push the page down when it lands.
  test("the preview's picture box is held before the first picture arrives", async ({ page }) => {
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/preview", async (route) => {
      await held;
      await route.continue();
    });
    await page.goto("/generate");
    const box = page.locator(".preview-placeholder__map");
    await expect(box).toBeVisible();
    // A faint line says what will fill it, so the space does not read as empty.
    await expect(box).toHaveText("Drawing the map preview for the current settings…");
    await checkPage(page);
    const before = (await box.boundingBox())!;
    const actions = (await page.locator("#generate-button").boundingBox())!;
    expect(before.height).toBeCloseTo(before.width, 0);
    release();
    const img = section(page).getByRole("img");
    await expect(img).toHaveCount(1);
    await expect(box).toHaveCount(0);
    const after = (await img.boundingBox())!;
    // The held box is the picture's own box.
    expect(after.height).toBeCloseTo(before.height, 0);
    expect(after.width).toBeCloseTo(before.width, 0);
    expect(after.x).toBeCloseTo(before.x, 0);
    // What is below moves by a line at most: the seed and file names are not known yet.
    const moved = (await page.locator("#generate-button").boundingBox())!.y - actions.y;
    expect(Math.abs(moved)).toBeLessThan(48);
  });

  test("without script no empty picture box is shown", async ({ browser, foreman }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL: foreman.url });
    const page = await context.newPage();
    await page.goto("/signin");
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password").fill("correct horse battery");
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.goto("/generate");
    await expect(page.locator(".preview-placeholder")).toBeHidden();
    await context.close();
  });

  // DESIGN 8.8: pictures are kept 15 minutes; one gone from the server is drawn again, not shown broken.
  test("a picture the server no longer has is drawn again in the large view", async ({ page }) => {
    let gone = true;
    await page.route(/\/generate\/preview\/[^/]+\/[a-z-]+$/, (route) => (gone ? route.fulfill({ status: 404, body: "" }) : route.continue()));
    const sent: Request[] = [];
    // The picture is back from the moment the new request starts, before its reply asks for the file.
    page.on("request", (r) => {
      if (!isAuto(r)) return;
      sent.push(r);
      if (sent.length === 2) gone = false;
    });
    await page.goto("/generate");
    await expect.poll(() => sent.length).toBe(1);
    await expect(section(page).getByRole("link", { name: /opens the large view/ })).toBeVisible();
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    // The broken picture is hidden, a new one is asked for, and the dialog says so.
    await expect.poll(() => sent.length).toBe(2);
    await expect(dialog(page).locator("#large-frame")).not.toHaveAttribute("data-missing", /.*/);
    await expect.poll(() => dialog(page).locator("#large-img").evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    await expect(dialog(page).locator("#large-status")).toHaveText("Nauvis");
    // Only once: no request goes after the picture is back.
    await page.waitForTimeout(1200);
    expect(sent.length).toBe(2);
  });

  // A failing preview with the large view open is said once and never asked for again.
  test("a failing preview with the large view open makes one request and says why", async ({ page, fakeGame }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await fakeGame!.call("/preview-mode", { mode: "fail" });
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    await page.keyboard.press("ArrowRight");
    // The new planet is named at once, and its drawing said; the old picture is not shown under it.
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await expect(dialog(page).locator("#large-frame")).toHaveAttribute("data-missing", "true");
    await expect(dialog(page).locator("#large-status")).toContainText("The game could not make the map preview.");
    await page.waitForTimeout(1500);
    expect(sent.length).toBe(1);
  });

  test("Next onto a planet not drawn yet names it at once and says it when drawn", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("dialog", { name: "Vulcanus" })).toBeVisible();
    await expect(dialog(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
    await expect(dialog(page).locator("#large-status")).toHaveText("Vulcanus");
    await expect(dialog(page).getByRole("link", { name: "Download Vulcanus map" })).toBeVisible();
  });

  // Previous before a planet's picture arrives drops that draw: the earlier picture, its status and Close's focus stay right.
  test("Previous before an undrawn planet arrives keeps the earlier planet and its focus", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/preview", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await page.keyboard.press("ArrowRight");
    await expect(dialog(page).locator("#large-status")).toContainText("Drawing Vulcanus");
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await expect(dialog(page).locator("#large-status")).toHaveText("Nauvis");
    release();
    await page.waitForTimeout(800);
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await expect(dialog(page).locator("#large-status")).toHaveText("Nauvis");
    await dialog(page).getByRole("button", { name: "Close" }).click();
    await expect(section(page).getByRole("link", { name: /Map preview of Nauvis/ })).toBeFocused();
  });

  // Close while a planet is still being drawn: focus stays on a picture that is there, and the late reply changes nothing.
  test("closing the large view while a planet is drawing keeps focus on a picture", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/preview", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await page.keyboard.press("ArrowRight");
    await expect(dialog(page).locator("#large-status")).toContainText("Drawing Vulcanus");
    await page.keyboard.press("Escape");
    await expect(dialog(page)).toBeHidden();
    release();
    await page.waitForTimeout(800);
    await expect(section(page).getByRole("link", { name: /Map preview of Nauvis/ })).toBeFocused();
  });

  // After one failed draw, Previous and Next still move and try again, one request for each press.
  test("after a failed draw in the large view, Previous and Next still move and retry", async ({ page, fakeGame }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await section(page).getByRole("link", { name: /opens the large view/ }).click();
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await fakeGame!.call("/preview-mode", { mode: "fail" });
    await page.keyboard.press("ArrowRight");
    await expect(dialog(page).locator("#large-status")).toContainText("The game could not make the map preview.");
    await fakeGame!.call("/preview-mode", { mode: "ok" });
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
    await expect(dialog(page).getByRole("img", { name: /^Map preview of Nauvis/ })).toBeVisible();
    await page.waitForTimeout(1200);
    expect(sent.length).toBe(1);
  });

  // WCAG 1.4.10: at 400% zoom (320 by 200) or a phone on its side the picture keeps its room and the dialog scrolls.
  for (const [w, h] of [[320, 200], [568, 320]]) {
    test(`the large view keeps the picture usable at ${w} by ${h}`, async ({ page }) => {
      await page.setViewportSize({ width: w, height: h });
      await page.goto("/generate");
      await expect(section(page).getByRole("img")).toHaveCount(1);
      await section(page).getByRole("link", { name: /opens the large view/ }).click();
      await expect(dialog(page)).toBeVisible();
      const frame = (await dialog(page).locator("#large-frame").boundingBox())!;
      expect(frame.height).toBeGreaterThanOrEqual(255);
      await dialog(page).locator("#large-frame").scrollIntoViewIfNeeded();
      await expect(dialog(page).locator("#large-img")).toBeInViewport();
    });
  }

  // WCAG 4.1.3: problems with the automatic preview are said, without moving focus.
  test("a seed that is not a number is said and marks the field", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    const seed = page.getByLabel("Seed");
    await seed.fill("abc");
    await expect(page.locator("#announcer")).toContainText("Fix this problem to continue");
    await expect(seed).toHaveAttribute("aria-invalid", "true");
    await expect(seed).toBeFocused();
    await seed.fill("123");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await expect(seed).not.toHaveAttribute("aria-invalid", /.*/);
  });

  test("a preview request that fails outright is said", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await page.route("**/generate/preview", (route) => route.abort());
    await page.getByLabel("Seed").fill("77");
    await expect(page.locator("#announcer")).toContainText("The map preview request failed.");
  });

  test("Preview map says at once that it started", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    const seen = await recordAnnouncer(page);
    await page.getByRole("button", { name: "Preview map" }).click();
    await expect.poll(seen).toContain("Making the map preview. This takes a few seconds for each planet.");
  });

  // A focused picture link keeps focus when a pending redraw lands (DESIGN 8.8: nothing automatic moves focus).
  test("a focused picture link keeps focus across a redraw", async ({ page }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    const sent: Request[] = [];
    page.on("request", (r) => { if (isAuto(r)) sent.push(r); });
    // A change made elsewhere, then focus moved to the picture before the redraw lands.
    await page.locator("#generate-seed").evaluate((s) => {
      (s as HTMLInputElement).value = "4242";
      s.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await section(page).getByRole("link", { name: /opens the large view/ }).focus();
    await expect.poll(() => sent.length).toBe(1);
    await expect(section(page)).toContainText("Seed 4242");
    await expect(section(page).getByRole("link", { name: /Map preview of Nauvis, seed 4242/ })).toBeFocused();
  });

  // SPEC 5.6, 17.9 (#318): Reset to defaults throws every change away, so
  // Undo takes its place with the focus, and one press brings them back.
  test("Reset to defaults offers Undo in its place, which brings every change back", async ({ page }) => {
    await page.goto("/generate");
    await page.locator("#generate-preset").selectOption("rich-resources");
    await expect(page.locator("#mg-nauvis-iron-ore-richness")).not.toHaveValue("100");
    const richness = await page.locator("#mg-nauvis-iron-ore-richness").inputValue();
    await tab(page, "Enemy").click();
    await page.getByLabel("Maximum expansion distance").fill("3");
    const reset = page.getByRole("button", { name: "Reset to defaults" });
    const undo = page.getByRole("button", { name: "Undo reset" });
    // Every text the live region is given; the preview's own news follows a moment later.
    await page.evaluate(() => {
      const w = window as unknown as { said: string[] };
      w.said = [];
      const a = document.getElementById("announcer")!;
      new MutationObserver(() => { if (a.textContent) w.said.push(a.textContent); }).observe(a, { childList: true, characterData: true, subtree: true });
    });
    const said = () => page.evaluate(() => (window as unknown as { said: string[] }).said);    await expect(undo).toBeHidden();
    await reset.focus();
    await page.keyboard.press("Enter");
    await expect(undo).toBeFocused();
    await expect(reset).toBeHidden();
    await expect(page.locator("#generate-preset")).toHaveValue("default");
    await expect(page.getByLabel("Maximum expansion distance")).not.toHaveValue("3");
    await expect.poll(said).toContain("Map settings reset to the defaults for this game version. Undo reset brings your changes back.");
    // Undo takes the reset's place, so the row does not grow.
    const u = (await undo.boundingBox())!;
    const p = (await page.locator("#generate-preset").boundingBox())!;
    if ((page.viewportSize()?.width ?? 0) > 600) expect(Math.abs(u.y + u.height / 2 - (p.y + p.height / 2))).toBeLessThan(4);
    await checkPage(page);

    await page.keyboard.press("Enter");
    await expect(page.locator("#generate-preset")).toHaveValue("rich-resources");
    await expect(page.getByLabel("Maximum expansion distance")).toHaveValue("3");
    await expect(page.locator("#mg-nauvis-iron-ore-richness")).toHaveValue(richness);
    await expect(page.locator("#preset-modified")).toHaveText("[modified]");
    await expect(undo).toBeHidden();
    await expect(reset).toBeFocused();
    await expect.poll(said).toContain("Your map settings are back.");
  });

  test("a change after Reset to defaults ends the Undo, so it cannot throw the change away", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    await page.getByLabel("Maximum expansion distance").fill("3");
    await page.getByRole("button", { name: "Reset to defaults" }).click();
    const undo = page.getByRole("button", { name: "Undo reset" });
    await expect(undo).toBeVisible();
    await page.getByLabel("Maximum expansion distance").fill("4");
    await expect(undo).toBeHidden();
    await expect(page.getByRole("button", { name: "Reset to defaults" })).toBeVisible();
  });

  // A tab is a view, not a setting (SPEC 5.6), so showing another one keeps the Undo.
  test("showing another settings tab after Reset to defaults keeps the Undo, which still works", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    await page.getByLabel("Maximum expansion distance").fill("3");
    await page.getByRole("button", { name: "Reset to defaults" }).click();
    const undo = page.getByRole("button", { name: "Undo reset" });
    await expect(undo).toBeVisible();
    await tab(page, "Resources").click();
    await expect(undo).toBeVisible();
    await undo.click();
    await tab(page, "Enemy").click();
    await expect(page.getByLabel("Maximum expansion distance")).toHaveValue("3");
  });

  // Undo would send one game version's settings with another's line, so a version or Space Age change ends it at once.
  test("a Space Age change ends the Undo before the settings are redrawn", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    await page.getByLabel("Maximum expansion distance").fill("3");
    await page.getByRole("button", { name: "Reset to defaults" }).click();
    const undo = page.getByRole("button", { name: "Undo reset" });
    await expect(undo).toBeVisible();
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/refresh", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await page.getByLabel("Space Age is on").uncheck();
    await expect(undo).toBeHidden();
    release();
  });

  // A change made while the reset is on its way ends the Undo, so the reply cannot offer it or say it.
  test("a change made while Reset is on its way leaves focus on the preset and says the reset alone", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    await page.getByLabel("Maximum expansion distance").fill("3");
    const seen = await recordAnnouncer(page);
    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/generate/refresh", async (route) => {
      await held;
      await route.continue().catch(() => {});
    });
    await page.getByRole("button", { name: "Reset to defaults" }).click();
    await page.getByLabel("Maximum expansion distance").fill("4");
    release();
    await expect(page.locator("#generate-preset")).toBeFocused();
    await expect.poll(seen).toContain("Map settings reset to the defaults for this game version.");
    expect(await seen()).not.toContain("Map settings reset to the defaults for this game version. Undo reset brings your changes back.");
    await expect(page.getByRole("button", { name: "Undo reset" })).toBeHidden();
  });

  // Focus never falls to the page when the Undo it was on ends.
  test("when the focused Undo ends, focus goes to Reset to defaults", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    await page.getByLabel("Maximum expansion distance").fill("3");
    await page.getByRole("button", { name: "Reset to defaults" }).press("Enter");
    const undo = page.getByRole("button", { name: "Undo reset" });
    await expect(undo).toBeFocused();
    // A change that does not move focus, as a script or a late redraw would make.
    await page.getByLabel("Maximum expansion distance").evaluate((f) => {
      (f as HTMLInputElement).value = "4";
      f.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await expect(undo).toBeHidden();
    await expect(page.getByRole("button", { name: "Reset to defaults" })).toBeFocused();
  });

  // The way back to one planet stays after a failed draw of every planet (DESIGN 8.8).
  test("after All planets fails to draw, One planet is still there and draws one", async ({ page, fakeGame }) => {
    await page.goto("/generate");
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await tab(page, "All planets").click();
    await expect(section(page).getByRole("img")).toHaveCount(5);
    await fakeGame!.call("/preview-mode", { mode: "fail" });
    await page.getByLabel("Seed").fill("4242");
    await expect(section(page).getByText("The game could not make the map preview.")).toBeVisible();
    await expect(page.getByRole("radio", { name: "All planets" })).toBeChecked();
    await fakeGame!.call("/preview-mode", { mode: "ok" });
    await tab(page, "One planet").click();
    await expect(section(page).getByRole("img")).toHaveCount(1);
    await checkPage(page);
  });

  test("the achievement warning names the setting, and its end is said", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    await page.getByRole("checkbox", { name: "Peaceful mode" }).check();
    await expect(page.locator("#announcer")).toHaveText("Peaceful mode turns off some achievements.");
    await page.getByRole("checkbox", { name: "Peaceful mode" }).uncheck();
    await expect(page.locator("#announcer")).toHaveText("No setting turns off achievements now.");
  });

  // Like the game, a minimum and its maximum stay at least their gap apart: the other field moves,
  // within its own ends, and what moved is said (SPEC 5.6).
  test("on 2.1, the expansion distance and cooldown keep their order", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    const low = page.getByLabel("Minimum expansion distance");
    const high = page.getByLabel("Maximum expansion distance");
    const announcer = page.locator("#announcer");
    await expect(low).toHaveValue("3");
    await expect(high).toHaveValue("5");
    // Raising the minimum past the maximum pushes it up, one more: the game wants it strictly larger.
    await low.fill("9");
    await low.press("Tab");
    await expect(high).toHaveValue("10");
    await expect(announcer).toHaveText("Maximum expansion distance set to 10 to keep the range valid.");
    await expect(page.locator("#preset-modified")).toHaveText("[modified]");
    // At the maximum's top end the minimum gives way instead.
    await low.fill("20");
    await low.press("Tab");
    await expect(high).toHaveValue("20");
    await expect(low).toHaveValue("19");
    await expect(announcer).toHaveText("Minimum expansion distance set to 19 to keep the range valid. Maximum expansion distance set to 20 to keep the range valid.");
    // Lowering the maximum under the minimum pulls the minimum down.
    await high.fill("6");
    await high.press("Tab");
    await expect(low).toHaveValue("5");
    await expect(announcer).toHaveText("Minimum expansion distance set to 5 to keep the range valid.");
    // Never below the minimum's own bottom end: the maximum stops one above it.
    await high.fill("1");
    await high.press("Tab");
    await expect(low).toHaveValue("1");
    await expect(high).toHaveValue("2");
    // With no gap, equal values are fine and nothing moves.
    const cooldownLow = page.getByLabel("Minimum cooldown");
    const cooldownHigh = page.getByLabel("Maximum cooldown");
    await cooldownHigh.fill("10");
    await cooldownHigh.press("Tab");
    await expect(cooldownLow).toHaveValue("10");
    await cooldownLow.fill("30");
    await cooldownLow.press("Tab");
    await expect(cooldownHigh).toHaveValue("30");
    await expect(announcer).toHaveText("Maximum cooldown set to 30 to keep the range valid.");
  });

  test("on 1.1, the settler group sizes keep their order", async ({ page }) => {
    await page.goto("/generate");
    await page.locator("details.disclosure summary").click();
    const refreshed = page.waitForResponse((r) => r.url().endsWith("/generate/refresh"));
    await page.getByLabel("Type a version").fill("1.1.110");
    await page.getByLabel("Type a version").press("Tab");
    await refreshed;
    await expect(page.locator("#announcer")).toHaveText(/^Map options updated/);
    await tab(page, "Enemy").click();
    const low = page.getByLabel("Minimum group size");
    const high = page.getByLabel("Maximum group size");
    await expect(low).toHaveValue("5");
    await expect(high).toHaveValue("20");
    await high.fill("3");
    await high.press("Tab");
    await expect(low).toHaveValue("3");
    await expect(page.locator("#announcer")).toHaveText("Minimum group size set to 3 to keep the range valid.");
    await low.fill("12");
    await low.press("Tab");
    await expect(high).toHaveValue("12");
    await expect(page.locator("#announcer")).toHaveText("Maximum group size set to 12 to keep the range valid.");
  });

  // A change that moves a field and also turns the achievement warning on says both, in one announcement.
  test("a live-fit move that also turns the achievement warning on says both", async ({ page }) => {
    await page.goto("/generate");
    await tab(page, "Enemy").click();
    const low = page.getByLabel("Minimum expansion distance");
    const seen = await recordAnnouncer(page);
    // A spin of the number box sends input and change together.
    await low.evaluate((el) => {
      const box = el as HTMLInputElement;
      box.value = "9";
      box.dispatchEvent(new Event("input", { bubbles: true }));
      box.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await expect.poll(seen).toContain("Minimum expansion distance turns off some achievements. Maximum expansion distance set to 10 to keep the range valid.");
  });

  // A preset chosen after the game line changed fills the new line's settings.
  test("after typing a 1.1 version, choosing Marathon refills the settings", async ({ page }) => {
    await page.goto("/generate");
    await page.locator("details.disclosure summary").click();
    const refreshed = page.waitForResponse((r) => r.url().endsWith("/generate/refresh"));
    await page.getByLabel("Type a version").fill("1.1.110");
    await page.getByLabel("Type a version").press("Tab");
    await refreshed;
    // The redraw has landed once it is said.
    await expect(page.locator("#announcer")).toHaveText(/^Map options updated/);
    const price = page.locator("#ms-difficulty-settings-technology-price-multiplier");
    await expect(price).toHaveValue("1");
    await page.locator("#generate-preset").selectOption({ label: "Marathon" });
    await expect(price).toHaveValue("4");
    await expect(page.locator("#preset-modified")).toHaveText("");
  });

  // WCAG 1.4.11: the slider is drawn in the selected colour, 3:1 or more on both themes (DESIGN 3).
  test("the sliders use the selected colour", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/generate");
    await tab(page, "Resources").click();
    const range = page.locator("input.step__range").first();
    await expect(range).toBeVisible();
    const [accent, selected] = await range.evaluate((r) => {
      const probe = document.createElement("span");
      probe.style.color = "var(--color-selected)";
      document.body.append(probe);
      const want = getComputedStyle(probe).color;
      probe.remove();
      return [getComputedStyle(r).accentColor, want];
    });
    expect(accent).toBe(selected);
  });
});

test.describe("world pages", () => {
  test.use({ fakeGameOn: true });
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
  });

  // DESIGN 6: the world's tabs on every page of a world, the section marked when the page is inside it.
  for (const [path, current] of [
    ["/worlds/second/clone", "Overview"],
    ["/worlds/second/delete", "Overview"],
    ["/worlds/base/settings/raw/server-settings.json", "Settings"],
  ]) {
    test(`${path} shows the world's tabs with ${current} marked`, async ({ page }) => {
      await page.goto(path);
      const tabs = page.getByRole("navigation", { name: "World pages" });
      await expect(tabs.getByRole("link")).toHaveText(["Overview", "Saves", "Snapshots", "Mods", "Settings"]);
      await expect(tabs.getByRole("link", { name: current })).toHaveAttribute("aria-current", "true");
      await expect(tabs.locator("[aria-current=page]")).toHaveCount(0);
      await checkPage(page);
    });
  }

  // The badge on Overview is on every tab of the active world, and only there.
  for (const sub of ["", "/saves", "/snapshots", "/mods", "/settings"]) {
    test(`the Active badge shows on the active world's ${sub || "/"} tab`, async ({ page }) => {
      await page.goto(`/worlds/base${sub}`);
      await expect(page.locator("h1 .badge--active")).toHaveText("Active");
      await page.goto(`/worlds/second${sub}`);
      await expect(page.locator("h1 .badge--active")).toHaveCount(0);
    });
  }

  // A heading with a time in its sentence lays out as text: the time stays inside the line.
  test("the restore question reads as one sentence with its time", async ({ page }) => {
    await page.goto("/worlds/base/snapshots");
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await page.locator("tbody tr").first().getByRole("link", { name: /Restore/ }).click();
    const h1 = page.locator("main h1");
    await expect(h1.locator("> time")).toHaveCount(1);
    expect(await h1.evaluate((h) => getComputedStyle(h).display)).toBe("block");
    await expect(h1.locator("> span")).toHaveCount(0);
  });

  // A confirmation page has one name for its question: the h1, not a landmark repeating it.
  test("confirmation pages do not repeat the heading as a landmark", async ({ page }) => {
    await page.goto("/worlds/second/delete");
    const h1 = page.locator("main h1");
    await expect(h1).not.toHaveAttribute("tabindex", /.*/);
    const name = (await h1.textContent())!.trim();
    await expect(page.getByRole("region", { name })).toHaveCount(0);
    await expect(page.getByRole("form", { name })).toHaveCount(0);
  });
});

test.describe("server page", () => {
  test.use({ fakeGameOn: true, foremanEnv: { FOREMAN_PUBLIC_ADDRESS: "factorio.example.com" } });

  test("the operation heading is set as the step list's heading size", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    const [op, h3] = await page.evaluate(() => {
      const main = document.querySelector("main")!;
      const a = document.createElement("h2");
      a.className = "op-heading";
      const b = document.createElement("h3");
      main.append(a, b);
      const size = (el: Element) => getComputedStyle(el).fontSize + " " + getComputedStyle(el).lineHeight;
      const out = [size(a), size(b)];
      a.remove();
      b.remove();
      return out;
    });
    expect(op).toBe(h3);
  });

  // At phone width Copy address wraps under the address; its words line up with the line above.
  for (const width of [320, 360, 400]) {
    test(`Copy address lines up at ${width} px`, async ({ page, foreman, context, fakeGame }) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await createAdmin(page);
      seedWorld(foreman);
      await fakeGame!.call("/image", { ref: image21, on: true });
      await page.setViewportSize({ width, height: 700 });
      await page.goto("/");
      const copy = page.getByRole("button", { name: "Copy address" });
      await expect(copy).toBeVisible();
      const [text, line] = await copy.evaluate((b) => {
        const r = document.createRange();
        r.selectNodeContents(b);
        return [r.getBoundingClientRect().left, b.parentElement!.getBoundingClientRect().left];
      });
      expect(Math.abs(text - line)).toBeLessThan(2);
      expect((await copy.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    });
  }
});

test.describe("names", () => {
  test("the user menu button says what it opens", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/worlds");
    const summary = page.locator("details.menu > summary");
    await expect(summary).toHaveAccessibleName("admin, English: account and language menu");
    await expect(summary).toHaveText(/admin\s+English/);
    await openMenu(page);
    await expect(page.getByRole("button", { name: "Sign out" })).toBeVisible();
  });

  test("signed out, the menu button names the language menu", async ({ page }) => {
    await page.goto("/signin");
    await expect(page.locator("details.menu > summary")).toHaveAccessibleName("English: language menu");
  });

});

test.describe("with a running game", () => {
  test.use({ fakeGameOn: true });

  test("No warning says it sets the countdown to 0 seconds", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page.locator("main .state").first()).toHaveText("Running", { timeout: 15_000 });
    await page.getByRole("link", { name: "Stop server" }).click();
    const zero = page.getByRole("button", { name: "No warning: set the countdown to 0 seconds" });
    await expect(zero).toBeVisible();
    await zero.click();
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("0");
    await checkPage(page);
  });
});
