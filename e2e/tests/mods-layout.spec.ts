import { test, expect, checkPage, createAdmin, seedWorld, openModTools, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";

// Issue 761: on a wide screen the Plan card sticks in view while the page
// scrolls, clear of the top strip, alone in its rail: "Match mods to the
// save" sits above Installed mods and the other tools below it, in the main
// column, so nothing passes under the plan (DESIGN 8.11). The projects run
// this in both themes; a phone holds the plan as a bar at the foot instead.
test.use({ fakeGameOn: true });

test("the sticking Plan card stays on top of the cards under it", async ({ page, foreman }, info) => {
  test.skip(info.project.name.startsWith("phone"), "the plan is a bar at the foot on a phone");
  await createAdmin(page);
  seedWorld(foreman, { mods: { other: "2.1", b: "2.1", c: "2.1" } });
  await page.setViewportSize({ width: 1440, height: 700 });
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await expect(page.getByRole("link", { name: "Match mods to the save" })).toBeVisible();
  const sync = page.locator('section[aria-labelledby="sync-heading"]');
  for (const by of [150, 300, 500]) {
    await page.evaluate((by) => window.scrollTo(0, by), by);
    const covered = await page.evaluate(() => {
      const plan = document.querySelector("#plan-panel")!;
      const r = plan.getBoundingClientRect();
      const out: string[] = [];
      for (let y = r.top + 2; y < r.bottom - 2; y += 8)
        for (let x = r.left + 2; x < r.right - 2; x += 24) {
          const el = document.elementFromPoint(x, y);
          if (!el || !plan.contains(el)) out.push(`${Math.round(x)},${Math.round(y)} ${el?.tagName}.${el?.className}`);
        }
      return out;
    });
    expect(covered, `scrolled ${by} px`).toEqual([]);
  }
  // The plan is alone in its rail; Match sits in the main column above
  // Installed mods, and Upload below it.
  expect(await page.evaluate(() => [...document.querySelector(".mod-side")!.children].map((el) => el.id))).toEqual(["plan-panel"]);
  const planBox = (await page.locator("#plan-panel").boundingBox())!;
  const syncBox = (await sync.boundingBox())!;
  const installedBox = (await page.locator('section[aria-labelledby="installed-heading"]').boundingBox())!;
  const uploadBox = (await page.locator('section[aria-labelledby="upload-heading"]').boundingBox())!;
  expect(syncBox.x + syncBox.width).toBeLessThan(planBox.x);
  expect(syncBox.x).toBe(installedBox.x);
  expect(syncBox.y + syncBox.height).toBeLessThan(installedBox.y);
  expect(installedBox.y + installedBox.height).toBeLessThan(uploadBox.y);
  expect(uploadBox.x).toBe(installedBox.x);
  await checkPage(page);
});

// DESIGN 8.11, WCAG 1.3.2 and 2.4.3: the cards' markup order is the order
// they are seen in, on a phone and on a wide screen, so focus follows it.
for (const width of [390, 1280]) {
  test(`at ${width} px the Mods cards are seen in their markup order`, async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman, { mods: { other: "2.1", b: "2.1", c: "2.1" } });
    await page.setViewportSize({ width, height: 800 });
    await page.goto("/worlds/base/mods");
    const cards = ["search-heading", "results-heading", "sync-heading", "installed-heading", "mod-tools"];
    const order = await page.evaluate((cards) => {
      const els = cards.map((id) => {
        const el = document.getElementById(id)!;
        return el.tagName === "H2" ? el.closest("section")! : el;
      });
      const inDom = els.every((el, i) => i === 0 || els[i - 1].compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING);
      const tops = els.map((el) => el.getBoundingClientRect().top + scrollY);
      return { inDom, tops };
    }, cards);
    expect(order.inDom).toBe(true);
    for (let i = 1; i < order.tops.length; i++) expect(order.tops[i], cards[i]).toBeGreaterThan(order.tops[i - 1]);
    if (width < 1024) await expect(page.locator("#mod-tools")).toHaveJSProperty("open", false);
    await checkPage(page);
  });
}

// Todd's ruling on #761: on a wide screen the Plan card is never taller than
// the window below the top strip. Its heading and Review stay in view, only
// the list of mods scrolls, the newest pick is at its top, and no control
// reached by keyboard is ever behind it (WCAG 2.1.1, 2.4.11). In a window
// too short to hold it, it does not stick and Review is still reached.
test.describe("a long plan on a wide screen", () => {
  test.use({ fakePortalOn: true, portalAccountOn: true });
  const caps = Array.from({ length: 8 }, (_, i) => `fp-cap-${i}`);
  const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png` });
  const plan = (page: Page) => page.locator("#plan-panel");
  const add = async (page: Page, n: string) => {
    await page.locator(`#mod-${n}-hit`).getByRole("button", { name: `Add ${n} to the plan`, exact: true }).click();
    await expect(page.locator(`#mod-${n}-hit`)).toContainText("In plan");
    await expect(plan(page).locator(".plan-tree__pick")).toHaveCount(caps.indexOf(n) + 1);
  };
  // The element is inside the window and is what a click at its centre hits.
  const seen = (page: Page, sel: string) =>
    page.evaluate((sel) => {
      const el = document.querySelector(sel)!;
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return r.top >= 0 && r.bottom <= innerHeight && !!hit && (el === hit || el.contains(hit));
    }, sel);

  test.beforeEach(async ({ page, foreman, fakeGame, fakePortal }, info) => {
    test.skip(info.project.name.startsWith("phone"), "the plan is a bar at the foot on a phone");
    // Each pick needs a mod of its own, so each takes two rows in the plan.
    for (const [i, n] of caps.entries()) {
      await fakePortal!.add(`fp-dep-${i}`, "1.0.0");
      await fakePortal!.add(n, "1.0.0", ["base >= 2.0", `fp-dep-${i}`]);
    }
    // A mod the save lacks, so the tools offer "Match mods to the save".
    await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { other: "2.0" } });
  });

  test("the card holds to the window, its list scrolls by keyboard, newest first, and no focused control is behind it", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 600 });
    await page.goto("/worlds/base/mods");
    await page.getByLabel("Mod name").fill("fp-cap");
    const list = plan(page).locator(".plan-tree");
    for (const n of caps.slice(0, -1)) await add(page, n);
    // The list is longer than the card lets it be, so it scrolls inside the card.
    expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight + 20)).toBe(true);

    // Scrolled to its end, then one more "+": the list is back at its top,
    // where the new pick is.
    await list.evaluate((el) => (el.scrollTop = el.scrollHeight));
    const last = caps[caps.length - 1];
    await add(page, last);
    expect(await list.evaluate((el) => el.scrollTop)).toBe(0);
    const picks = plan(page).locator(".plan-tree__pick > .plan-tree__row .plan-tree__name");
    await expect(picks).toHaveText([...caps].reverse().map((n) => new RegExp(`^${n} on the mod portal`)));

    const fits = async () => {
      // The card ends inside the window, its heading and Review are seen, and so is the newest pick.
      const box = (await plan(page).boundingBox())!;
      expect(box.y + box.height).toBeLessThanOrEqual(600 + 1);
      expect(await seen(page, "#plan-heading")).toBe(true);
      expect(await seen(page, "#act-review-plan")).toBe(true);
      expect(await seen(page, `#mod-${last}-unpick`)).toBe(true);
    };
    await fits();
    // Once the page scrolls the card sticks, and still fits.
    await page.evaluate(() => window.scrollTo(0, 400));
    await fits();
    await checkPage(page);
    await shot(page, "mods-plan-capped");

    // The list is reached and scrolled by keyboard: Tab through its crosses
    // to the oldest pick, which the list scrolls into view.
    await page.locator(`#mod-${last}-unpick`).focus();
    for (let i = 0; i < 40; i++) {
      if (await page.evaluate((id) => document.activeElement?.id === id, `mod-${caps[0]}-unpick`)) break;
      await page.keyboard.press("Tab");
    }
    await expect(page.locator(`#mod-${caps[0]}-unpick`)).toBeFocused();
    expect(await list.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    expect(await seen(page, `#mod-${caps[0]}-unpick`)).toBe(true);

    // On out of the plan, through Match, Installed mods and the tools below
    // it: every control focused is seen, never behind the sticking plan.
    await openModTools(page);
    const visited: string[] = [];
    for (let i = 0; i < 80; i++) {
      await page.keyboard.press("Tab");
      const at = await page.evaluate(() => {
        const el = document.activeElement as HTMLElement | null;
        if (!el || !el.closest(".split--mods")) return null;
        if (el.closest("#plan-panel")) return { name: "plan", ok: true };
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return { name: el.id || el.textContent!.trim(), ok: r.top >= 0 && r.bottom <= innerHeight && !!hit && (el === hit || el.contains(hit)) };
      });
      if (!at) break;
      if (at.name === "plan") continue;
      expect(at.ok, `${at.name} is seen when focused`).toBe(true);
      visited.push(at.name);
    }
    expect(visited).toContain("act-sync");
    expect(visited).toContain("mod-upload-summary");
    await checkPage(page);
  });

  test("in a window too short for the card, it does not stick and Review is still reached", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 420 });
    await page.goto("/worlds/base/mods");
    await page.getByLabel("Mod name").fill("fp-cap");
    for (const n of caps) await add(page, n);
    expect(await plan(page).evaluate((el) => getComputedStyle(el).position)).not.toBe("sticky");
    const review = page.locator("#act-review-plan");
    await review.focus();
    expect(await seen(page, "#act-review-plan")).toBe(true);
    await checkPage(page);
    await shot(page, "mods-plan-short");
  });
});
