import { test, expect, checkPage, setUpModsWorld, doneBox, live, makeZip, version20, openModTools, redrawn } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.8 and 9.10: make a world's mods the ones its save was made with, and
// what the plan asks when the portal lacks a mod's version.
//
// The save is the real 2.1.24 save that uses foreman-test 0.1.0, with Space Age
// on. The fake portal's mods are made for Factorio 2.0, so a 2.1 world can offer
// no "nearest release"; the test of that choice uses a 2.0 world, which makes
// the save's built-in recycler unresolved too. The plan reads the save's mod
// list only and the game is stopped, so no start check compares game versions.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const save = "2.1.24-space-age-testmod.zip";
const world21 = { fixture: save, spaceAge: true };
const world20 = { version: version20, fixture: save, spaceAge: true };
const syncPlan = "/worlds/base/mods/plan?kind=sync";
const modsFolder = (dataDir: string) => join(dataDir, "worlds", "base", "mods");
const modsTable = (page: Page) => page.getByRole("table", { name: /Mods of Base/ });

// At 320 px each choice is its box and one run of words: a mod name inside
// the words must not become a flex column of its own.
async function expectWholeChoiceLabels(page: Page) {
  if ((page.viewportSize()?.width ?? 1000) > 320) return;
  const split = await page.locator("#mod-plan label.check").evaluateAll((labels) =>
    labels
      .filter((l) => [...l.childNodes].filter((n) => n.nodeType === Node.ELEMENT_NODE || n.textContent!.trim() !== "").length > 2)
      .map((l) => l.textContent!.trim()),
  );
  expect(split).toEqual([]);
  expect(await page.locator("#mod-plan label.check").count()).toBeGreaterThan(0);
}

// A zip of foreman-test, as the admin would upload it.
const testModZip = (version = "0.1.0") =>
  makeZip({ [`foreman-test_${version}/info.json`]: JSON.stringify({ name: "foreman-test", version, factorio_version: "2.1", dependencies: ["base"] }) });

test.describe("a save's mod the portal does not have", () => {
  test.beforeEach(async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, world21);
  });

  test("the plan offers upload first, with its zip field, and continue without", { tag: "@looks" }, async ({ page }) => {
    await page.goto("/worlds/base/mods");
    await openModTools(page);
    await page.getByRole("link", { name: "Match mods to the save" }).click();
    await expect(page).toHaveURL(syncPlan);
    await expect(page.getByRole("heading", { level: 1, name: "Match the mods of Base to the save base.zip?" })).toBeVisible();
    const choices = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
    // The portal has nothing near this version for Factorio 2.1, so there is no "nearest" option.
    await expect(choices.getByRole("radio")).toHaveCount(2);
    await expect(choices.getByLabel("Upload the zip of foreman-test 0.1.0")).toBeChecked();
    await expect(choices.getByLabel("Continue without foreman-test")).not.toBeChecked();
    // Upload is the default and no zip is uploaded yet, so there is nothing to confirm.
    await expect(page.locator("#act-apply")).toHaveCount(0);
    await expect(page.getByLabel("Zip of foreman-test 0.1.0", { exact: true })).toBeVisible();
    await expectWholeChoiceLabels(page);
    await checkPage(page);
    await page.screenshot({ path: `../screenshots/mods-plan-unresolved-${test.info().project.name}.png`, fullPage: true });
  });

  test("continue without: the box is required, and a post without it is a field error", { tag: "@looks" }, async ({ page, foreman }) => {
    await page.goto(syncPlan);
    await page.getByLabel("Continue without foreman-test").check();
    const apply = page.locator("#act-apply");
    await expect(apply).toBeVisible();
    await expect(page.getByLabel("Upload the zip of foreman-test 0.1.0")).not.toBeChecked();
    await checkPage(page);

    await apply.click();
    const summary = page.locator("#error-summary");
    await expect(summary).toBeFocused();
    await expect(summary).toContainText("Tick the box to confirm that the game will delete what foreman-test added");
    const box = page.locator("#mod-foreman-test-confirm");
    await expect(box).toHaveAttribute("aria-invalid", "true");
    await expect(box).not.toBeChecked();
    await expect(page.locator("#mod-foreman-test-confirm-error")).toContainText("Tick the box");
    // The choice is kept across the refusal.
    await expect(page.getByLabel("Continue without foreman-test")).toBeChecked();
    await checkPage(page);

    // The summary's link takes focus to the box.
    await summary.getByRole("link").click();
    await expect(box).toBeFocused();
    await box.check();
    await page.locator("#act-apply").click();
    await expect(page.locator("#error-summary")).toHaveCount(0);
    // The plan changes no mod file, so the Done box keeps the plain sentence.
    await expect(doneBox(page)).toContainText("The mods of Base were changed.", live);
    // The save's mod stays out of the world.
    expect(existsSync(join(modsFolder(foreman.dataDir), "foreman-test_0.1.0.zip"))).toBe(false);
    await page.goto("/worlds/base/mods");
    await expect(modsTable(page).getByRole("row", { name: /foreman-test/ })).toHaveCount(0);
  });

  test("an uploaded zip answers the choice in the plan, and is installed", { tag: "@looks" }, async ({ page, foreman }) => {
    await page.goto(syncPlan);
    const form = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
    await form.getByLabel("Zip of foreman-test 0.1.0", { exact: true }).setInputFiles({ name: "foreman-test_0.1.0.zip", mimeType: "application/zip", buffer: testModZip() });
    await form.getByRole("button", { name: "Upload zip" }).click();

    await expect(page.getByLabel("Use the uploaded zip of foreman-test 0.1.0")).toBeChecked();
    await expect(page.locator("#mod-plan")).toContainText("foreman-test 0.1.0, from the uploaded zip");
    await checkPage(page);
    await page.locator("#act-apply").click();
    await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    expect(readFileSync(join(modsFolder(foreman.dataDir), "foreman-test_0.1.0.zip")).equals(testModZip())).toBe(true);
    await page.goto("/worlds/base/mods");
    await expect(modsTable(page).getByRole("row", { name: /foreman-test/ })).not.toContainText("Disabled");
  });

  test("a zip of another mod is refused where it was sent, and the choice is kept", { tag: "@looks" }, async ({ page }) => {
    await page.goto(syncPlan);
    const other = makeZip({ "other_1.0.0/info.json": JSON.stringify({ name: "other", version: "1.0.0", factorio_version: "2.1" }) });
    const form = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
    await form.getByLabel("Zip of foreman-test 0.1.0", { exact: true }).setInputFiles({ name: "other_1.0.0.zip", mimeType: "application/zip", buffer: other });
    await form.getByRole("button", { name: "Upload zip" }).click();
    await expect(page.locator("#mod-foreman-test-zip-error")).toContainText("This zip holds other, not foreman-test.");
    await expect(page.locator("#mod-foreman-test-zip")).toHaveAttribute("aria-invalid", "true");
    await expect(page.locator("#act-apply")).toHaveCount(0);
    await checkPage(page);
  });

  // DESIGN 7.2: a refused zip is in the error summary, which takes focus, and
  // its link leads to the zip field inside the mod's own group.
  test("a refused zip gets an error summary with focus on it", { tag: "@looks" }, async ({ page }) => {
    await page.goto(syncPlan);
    const form = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
    await form.getByLabel("Zip of foreman-test 0.1.0", { exact: true }).setInputFiles({ name: "junk.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
    await form.getByRole("button", { name: "Upload zip" }).click();
    const summary = page.locator("#error-summary");
    await expect(summary).toBeFocused();
    await expect(summary).toContainText("This file is not a mod zip");
    await summary.getByRole("link").click();
    await expect(form.getByLabel("Zip of foreman-test 0.1.0", { exact: true })).toBeFocused();
    await checkPage(page);
  });

  // WCAG 4.1.3: a choice redraws the plan in place; the new plan is announced
  // and focus stays on the choice, whose id follows the mod.
  test("a choice redraws the plan, says so, and keeps focus on the choice", { tag: "@looks" }, async ({ page }) => {
    await page.goto(syncPlan);
    // The confirmation box is there only once "continue without" is chosen.
    await expect(page.locator("#mod-foreman-test-confirm")).toHaveCount(0);
    const without = page.getByLabel("Continue without foreman-test");
    await without.focus();
    await page.keyboard.press("Space");
    const box = page.locator("#mod-foreman-test-confirm");
    await expect(box).toBeVisible();
    await expect(without).toBeFocused();
    await expect(page.locator("#announcer")).toHaveText(/^Plan updated: /);
    // The box is required: marked, and explained once before it.
    await expect(box).toHaveAttribute("required", "");
    await expect(page.locator('label[for="mod-foreman-test-confirm"]')).toHaveClass(/\brequired\b/);
    await expect(page.locator("#mod-plan .required-note")).toHaveText("* Required");
    await checkPage(page);
  });
});

// A refused zip comes back with every other answer on the plan still given.
test("a refused zip keeps the other mods' answers", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  // This 2.0 world lacks the save's built-in recycler as well as foreman-test.
  await setUpModsWorld(page, foreman, fakeGame!, world20);
  await page.goto(syncPlan);
  await page.getByLabel("Continue without recycler").check();
  const box = page.locator("#mod-recycler-confirm");
  await box.check();
  await expect(page.locator("#announcer")).toHaveText(/^Plan updated: /);
  const group = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
  await group.getByLabel("Zip of foreman-test 0.1.0", { exact: true }).setInputFiles({ name: "junk.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
  await group.getByRole("button", { name: "Upload zip" }).click();
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.getByLabel("Continue without recycler")).toBeChecked();
  await expect(box).toBeChecked();
  await expect(group.getByLabel("Upload the zip of foreman-test 0.1.0")).toBeChecked();
});

test("the nearest release is one of three choices, and it is downloaded and installed", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
  // The portal has no foreman-test 0.1.0; 0.9.0 is the lowest release above it for Factorio 2.0.
  await fakePortal!.add("foreman-test", "0.9.0", ["base >= 2.0"]);
  await fakePortal!.add("foreman-test", "1.1.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, world20);
  await page.goto(syncPlan);
  const choices = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
  await expect(choices.getByRole("radio")).toHaveCount(3);
  await expect(choices.getByLabel("Upload the zip of foreman-test 0.1.0")).toBeChecked();
  await expect(choices.getByLabel("Continue without foreman-test")).not.toBeChecked();
  await choices.getByLabel("Use foreman-test 0.9.0, the nearest release for Factorio 2.0").check();
  await expect(page.locator("#mod-plan")).toContainText("foreman-test 0.9.0");
  // The save's built-in recycler is a 2.1 mod, which this 2.0 world lacks; it is dropped knowingly.
  await page.getByLabel("Continue without recycler").check();
  await page.locator("#mod-recycler-confirm").check();
  await expectWholeChoiceLabels(page);
  await checkPage(page);

  await page.locator("#act-apply").click();
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  expect(existsSync(join(modsFolder(foreman.dataDir), "foreman-test_0.9.0.zip"))).toBe(true);
  expect((await fakePortal!.state()).refused).toEqual([]);
});

test("in a 2.1 world the nearest release is the next one up on the 2.1 line", async ({ page, foreman, fakeGame, fakePortal }) => {
  // The portal has no foreman-test 0.1.0 for 2.1: it has 0.0.5 below, and 0.9.0 and 1.1.0 above.
  // The 2.0 release at 0.1.5 is nearer, but it is for another game line.
  await fakePortal!.add("foreman-test", "0.0.5", ["base >= 2.1"], "2.1");
  await fakePortal!.add("foreman-test", "0.9.0", ["base >= 2.1"], "2.1");
  await fakePortal!.add("foreman-test", "1.1.0", ["base >= 2.1"], "2.1");
  await fakePortal!.add("foreman-test", "0.1.5", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, world21);
  await page.goto(syncPlan);
  const choices = page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" });
  await expect(choices.getByRole("radio")).toHaveCount(3);
  await choices.getByLabel("Use foreman-test 0.9.0, the nearest release for Factorio 2.1").check();
  await expect(page.locator("#mod-plan")).toContainText("foreman-test 0.9.0");
  await checkPage(page);

  await page.locator("#act-apply").click();
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  expect(existsSync(join(modsFolder(foreman.dataDir), "foreman-test_0.9.0.zip"))).toBe(true);
  expect((await fakePortal!.state()).refused).toEqual([]);
});

// A redraw that finds a conflict answers 200 so htmx swaps it in, and the
// conflict is announced. Two offers that each fit on their own conflict
// once both are ticked; an offer that conflicts with a mod already on is
// left out with a note instead of a box (SPEC 9.2); one that conflicts only
// with another offer keeps its box, where it was.
test("ticking an optional mod that conflicts shows the conflict", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  await portal.add("fp-conf", "1.0.0", ["base >= 2.0", "! fp-other"]);
  await portal.add("fp-main", "1.0.0", ["base >= 2.0", "? fp-conf", "? fp-a", "? fp-b"]);
  await portal.add("fp-a", "1.0.0", ["base >= 2.0"]);
  await portal.add("fp-b", "1.0.0", ["base >= 2.0", "! fp-a"]);
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { "fp-other": "2.0" } });
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main");
  await expectWholeChoiceLabels(page);
  await expect(page.getByRole("checkbox", { name: "fp-conf offered by fp-main" })).toHaveCount(0);
  await expect(page.locator("#plan-unavailable")).toContainText("fp-conf, offered by fp-main, is left out");
  // Ticking fp-a keeps fp-b's box (the rows never change with what is
  // ticked).
  const fpB = page.getByRole("checkbox", { name: "fp-b offered by fp-main" });
  const before = await fpB.boundingBox();
  await page.getByRole("checkbox", { name: "fp-a offered by fp-main" }).check();
  await expect(page.getByRole("checkbox", { name: "fp-a offered by fp-main" })).toBeChecked();
  await expect(page.locator("#announcer")).toContainText("Plan updated");
  await expect(fpB).toBeVisible();
  // fp-a's change row lands below the offers, so fp-b keeps its box exactly.
  const after = await fpB.boundingBox();
  expect({ x: after!.x, y: after!.y }).toEqual({ x: before!.x, y: before!.y });
  await fpB.check();
  await expect(page.locator("#plan-conflicts")).toContainText("fp-b cannot run together with fp-a.", { timeout: 3_000 });
  await expect(page.locator("#announcer")).toHaveText("Plan updated: it has a conflict, so it cannot be applied.");
  // A redraw never moves focus to the conflict box.
  await expect(page.locator("#plan-conflicts")).not.toBeFocused();
  await expect(page.locator("#act-apply")).toHaveCount(0);
});

// Ticking or clearing an offer redraws the plan in place: every box already
// shown keeps its exact place and the page does not scroll (SPEC 9.14,
// DESIGN 7.8). A box's change row goes below the offers, and an offer the
// tick brings goes at the end of the list, even when its name sorts first.
test("ticking or clearing an offer moves no box and does not scroll", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  const names = Array.from({ length: 12 }, (_, i) => `fp-${String(i + 1).padStart(2, "0")}`);
  await portal.add("fp-main", "1.0.0", ["base >= 2.0", "+ fp-rec", ...names.map((n) => `? ${n}`)]);
  await portal.add("fp-rec", "1.0.0", ["base >= 2.0"]);
  for (const n of names) await portal.add(n, "1.0.0", n === "fp-05" ? ["base >= 2.0", "? fp-00x"] : ["base >= 2.0"]);
  await portal.add("fp-00x", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip" });

  type Layout = { scrollY: number; boxes: Record<string, { x: number; y: number; w: number; h: number }> };
  const layout = (): Promise<Layout> =>
    page.locator("#mod-plan .plan-offer input[type=checkbox]").evaluateAll((els) => ({
      scrollY: window.scrollY,
      boxes: Object.fromEntries(
        els.map((el) => {
          const r = el.getBoundingClientRect();
          return [el.id, { x: r.x, y: r.y, w: r.width, h: r.height }];
        }),
      ),
    }));
  const box = (name: string, of = "fp-main") => page.getByRole("checkbox", { name: `${name} offered by ${of}` });
  const recommended = page.getByRole("checkbox", { name: "fp-rec recommended by fp-main" });
  // Each step: act on one box, wait for the redraw, then every box shown
  // before is where it was, the page has not scrolled, and focus stayed.
  const step = async (target: ReturnType<typeof box>, act: "check" | "uncheck", label: string) => {
    // The admin scrolls the box into view first; only the redraw must not scroll.
    await target.scrollIntoViewIfNeeded();
    const before = await layout();
    await redrawn(page, "mod-plan", () => target[act]());
    await expect(page.locator("#announcer")).toContainText("Plan updated");
    await expect(target).toBeFocused();
    const after = await layout();
    expect(after.scrollY, `${label}: scrollY`).toBe(before.scrollY);
    for (const [id, b] of Object.entries(before.boxes)) {
      if (id in after.boxes) expect(after.boxes[id], `${label}: ${id}`).toEqual(b);
    }
    return after;
  };

  for (const size of [{ width: 1920, height: 1080 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(size);
    for (const theme of ["dark", "light"] as const) {
      await page.emulateMedia({ colorScheme: theme });
      const label = `${size.width} ${theme}`;
      await page.goto("/worlds/base/mods/plan?kind=install&name=fp-main");
      // The middle of a list long enough to scroll.
      await box("fp-05").evaluate((el) => el.scrollIntoView({ block: "center" }));
      expect(await page.evaluate(() => window.scrollY), `${label}: the page scrolls`).toBeGreaterThan(0);

      // An offer with an offer of its own: the new one goes at the end.
      let now = await step(box("fp-05"), "check", `${label} tick fp-05`);
      // The tick's sentence names what it adds.
      await expect(page.locator("#announcer")).toHaveText(/^Plan updated: adds fp-05\. \d+ mods change\.$/);
      const ids = Object.keys(now.boxes);
      expect(ids.at(-1), label).toBe(await box("fp-00x", "fp-05").getAttribute("id"));
      await step(box("fp-07"), "check", `${label} tick fp-07`);
      await step(box("fp-07"), "uncheck", `${label} clear fp-07`);
      await step(recommended, "uncheck", `${label} clear fp-rec`);
      await step(box("fp-05"), "uncheck", `${label} clear fp-05`);
      await expect(box("fp-00x", "fp-05")).toHaveCount(0);
    }
  }
});
