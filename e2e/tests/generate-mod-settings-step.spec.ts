import { test, expect, checkPage, createAdmin, seedWorld, image21, live, pickLanguage } from "./foreman.js";
import { openNewMap, newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 9.6: step 3 of the guided Generate is the startup settings of
// the world's mods. Saving goes on to step 4, whose planets follow the new
// values; the fake game adds the planet fake-moon when fm-sta-int is above 7.
test.use({ fakeGameOn: true });

const stepPath = (id: string) => `${newMapPath(id)}/mod-settings`;
const steps = (page: Page) => page.getByRole("navigation", { name: "Steps to make the world" });

// Adds the fake planet mod to a world and enables it, as a Mods install would.
function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: [{ name: "base", enabled: true }] }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

async function modded(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }): Promise<string> {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  return id;
}

test("step 3 shows the startup settings, and saving one adds the planet it turns on to step 4", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  const id = await modded(page, foreman, fakeGame!);

  // The form's load is held until the moving bar Generate uses has been seen beside the loading words.
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  await page.route(`**${stepPath(id)}/form`, async (route) => {
    await held;
    await route.continue();
  });

  // Continue opens step 3: a mod has startup settings and none are saved.
  await page.goto(`/worlds/${id}`);
  await page.getByRole("link", { name: "Continue setting up" }).click();
  await expect(page).toHaveURL(stepPath(id));
  const wait = page.locator("#mod-settings-form .wait");
  await expect(wait).toBeVisible();
  await expect(wait).toHaveAttribute("role", "status");
  await expect(wait.locator("progress")).toBeVisible();
  release();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Mod settings");
  await expect(steps(page).getByRole("link", { name: "3. Mod settings" })).toHaveAttribute("aria-current", "step");
  await expect(steps(page).getByRole("link")).toHaveCount(4);
  const field = page.getByLabel("fm-sta-int");
  await expect(field).toBeVisible(live);
  // Startup settings only: the map settings of the mods wait for the world page.
  await expect(page.locator("#mod-settings-map-heading")).toHaveCount(0);
  // Steps 1 and 2 are ticked, and the footer has Back first, then the save (SPEC 5.6).
  await expect(steps(page).locator(".flow-step--done")).toHaveCount(2);
  const foot = page.locator(".form-foot");
  await expect(foot.getByRole("link", { name: "Back to the mods" })).toHaveAttribute("href", `/worlds/${id}/mods`);
  await expect(foot.getByRole("link", { name: "Cancel" })).toHaveCount(0);
  await expect(foot.locator("> *").last()).toHaveClass(/button--primary/);
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/step3-${test.info().project.name}.png`, fullPage: true });

  // Before the save step 4 has no fake-moon.
  await page.goto(newMapPath(id));
  await expect(page.locator('#generate-form details[data-planet="fake-planet"]').first()).toBeAttached({ timeout: 60_000 });
  await expect(page.locator('#generate-form details[data-planet="fake-moon"]')).toHaveCount(0);

  await page.goto(stepPath(id));
  await field.fill("9");
  // Saving shows its busy state (SPEC 17.19). The first press is stopped at
  // the window, after the page's own handler has seen it, so the busy state
  // can be read; the second press saves.
  await page.evaluate(() => window.addEventListener("submit", (e) => e.preventDefault(), { once: true }));
  const save = page.getByRole("button", { name: "Next: Map settings" });
  await save.click();
  await expect(save).toBeDisabled();
  await expect(page.locator("#flow-modsettings-saving")).toHaveText("Saving the mod settings…");
  await checkPage(page);
  await save.evaluate((b) => { (b as HTMLButtonElement).disabled = false; });
  await save.click();
  await expect(page).toHaveURL(newMapPath(id), live);
  await expect(page.locator("#newmap-skipped")).toHaveCount(0);
  await expect(page.locator('#generate-form details[data-planet="fake-moon"]').first()).toBeAttached({ timeout: 60_000 });
  await checkPage(page);

  // With the settings saved, Continue goes on to step 4.
  await page.goto(`/worlds/${id}`);
  await page.getByRole("link", { name: "Continue setting up" }).click();
  await expect(page).toHaveURL(new RegExp(newMapPath(id)));
});

test("Skip to map settings goes on without saving", async ({ page, foreman, fakeGame }) => {
  const id = await modded(page, foreman, fakeGame!);
  await page.goto(stepPath(id));
  await expect(page.getByLabel("fm-sta-int")).toBeVisible(live);
  const skip = page.getByRole("link", { name: "Skip to map settings" });
  await expect(skip).toHaveAttribute("href", newMapPath(id));
  await skip.click();
  await expect(page).toHaveURL(newMapPath(id));
  expect(existsSync(join(foreman.dataDir, "worlds", id, "mods", "mod-settings.dat"))).toBe(false);
  // A Skip chosen by hand is not the automatic one: no note.
  await expect(page.locator("#newmap-skipped")).toHaveCount(0);

  // Once step 4 is saved the world is Ready, and Change setup opens step 4, not the skipped step 3.
  await page.locator("#newmap-save").click();
  await expect(page.locator("#world-ready")).toBeVisible(live);
  await page.goto(`/worlds/${id}`);
  await page.getByRole("link", { name: "Change setup" }).click();
  await expect(page).toHaveURL(new RegExp(`${newMapPath(id)}(\\?|$)`));
});

test("a world with only built-in mods skips step 3 and marks it (none)", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Plain" });
  await page.goto(stepPath(id));
  await expect(page).toHaveURL(`${newMapPath(id)}?skipped=1`);
  await expect(page.locator("#newmap-skipped")).toHaveText("No mod has startup settings, so step 3 was skipped.");
  const bar = steps(page);
  await expect(bar.getByRole("listitem").nth(2)).toHaveText("3. Mod settings (none)");
  // Skipped looks apart from a step that cannot be opened yet.
  await expect(bar.locator(".flow-step--skipped")).toHaveText("3. Mod settings (none)");
  await expect(bar.locator(".flow-step--skipped .flow-step__name")).toHaveCSS("font-style", "italic");
  await expect(bar.getByRole("link", { name: /Mod settings/ })).toHaveCount(0);
  await expect(bar.getByRole("link")).toHaveCount(3);
  await checkPage(page);
});

test.describe("step 3 without script", () => {
  test.use({ javaScriptEnabled: false });
  test("the page claims no load and its link has the server read the form", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    const id = await modded(page, foreman, fakeGame!);
    await page.goto(stepPath(id));
    // As on the mod settings page (SPEC 9.6): no loading words without script.
    await expect(page.locator(".mod-settings-loading")).toBeHidden();
    await page.locator("#mod-settings-wait").click();
    await expect(page.getByLabel("fm-sta-int")).toBeVisible(live);
  });
});

// SPEC 17.2: at 320 px the four-step bar shows its marks and the current name in the longest languages,
// with no sideways scroll.
for (const lang of ["pl", "ru"]) {
  test(`at 320 px in ${lang} the step bar fits without a sideways scroll`, { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    const id = await modded(page, foreman, fakeGame!);
    await pickLanguage(page, lang);
    await page.setViewportSize({ width: 320, height: 640 });
    await page.goto(stepPath(id));
    const bar = page.locator("#flow-steps");
    await expect(bar.getByRole("listitem")).toHaveCount(4);
    // Narrow, only the marks show, with the current step's name under them.
    await expect(bar.locator(".flow-steps__now")).toBeVisible();
    await expect(bar.locator(".flow-step__name").first()).toHaveCSS("clip-path", "inset(50%)");
    const b = (await bar.boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(0);
    expect(b.x + b.width).toBeLessThanOrEqual(320);
    const scroll = await page.evaluate(() => ({ doc: document.documentElement.scrollWidth, bar: document.querySelector("#flow-steps")!.scrollWidth, barClient: document.querySelector("#flow-steps")!.clientWidth }));
    expect(scroll.doc).toBeLessThanOrEqual(320);
    expect(scroll.bar).toBeLessThanOrEqual(scroll.barClient);
    await checkPage(page);
  });
}
