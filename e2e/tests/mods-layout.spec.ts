import { test, expect, checkPage, createAdmin, seedWorld, openModTools, setUpModsWorld, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";

// Issue 761: on a wide screen the Plan card sticks in view while the page
// scrolls, clear of the top strip and over the "Match mods to the save"
// card that scrolls under it (DESIGN 8.11). The projects run this in both
// themes; a phone holds the plan as a bar at the foot instead.
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
  // By then the Match card has scrolled up under the plan.
  const planBox = (await page.locator("#plan-panel").boundingBox())!;
  const syncBox = (await sync.boundingBox())!;
  expect(syncBox.y).toBeLessThan(planBox.y + planBox.height);
  await checkPage(page);
});

// Todd's ruling on #761: on a wide screen the Plan card is never taller than
// the window below the top strip. Its heading and Review stay in view, only
// the list of mods scrolls, the newest pick is at its top, and keyboard
// focus in the cards under it is never hidden behind it (WCAG 2.1.1, 2.4.11).
test.describe("a long plan on a wide screen", () => {
  test.use({ fakePortalOn: true, portalAccountOn: true });
  const caps = Array.from({ length: 8 }, (_, i) => `fp-cap-${i}`);
  const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png` });

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

  test("the card holds to the window, its list scrolls by keyboard, newest first, and focus below it stays in view", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 600 });
    await page.goto("/worlds/base/mods");
    await page.getByLabel("Mod name").fill("fp-cap");
    const plan = page.locator("#plan-panel");
    const list = plan.locator(".plan-tree");
    const add = async (n: string) => {
      await page.locator(`#mod-${n}-hit`).getByRole("button", { name: `Add ${n} to the plan`, exact: true }).click();
      await expect(page.locator(`#mod-${n}-hit`)).toContainText("In plan");
      await expect(plan.locator(".plan-tree__pick")).toHaveCount(caps.indexOf(n) + 1);
    };
    for (const n of caps.slice(0, -1)) await add(n);
    // The list is longer than the card lets it be, so it scrolls inside the card.
    expect(await list.evaluate((el) => el.scrollHeight > el.clientHeight + 20)).toBe(true);

    // Scrolled to its end, then one more "+": the list is back at its top,
    // where the new pick is.
    await list.evaluate((el) => (el.scrollTop = el.scrollHeight));
    const last = caps[caps.length - 1];
    await add(last);
    expect(await list.evaluate((el) => el.scrollTop)).toBe(0);
    const picks = plan.locator(".plan-tree__pick > .plan-tree__row .plan-tree__name");
    await expect(picks).toHaveText([...caps].reverse().map((n) => new RegExp(`^${n} on the mod portal`)));

    const inView = async (sel: string) =>
      page.evaluate((sel) => {
        const el = document.querySelector(sel)!;
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
        return r.top >= 0 && r.bottom <= innerHeight && !!hit && (el === hit || el.contains(hit));
      }, sel);
    const fits = async () => {
      // The card ends inside the window, its heading and Review are seen, and so is the newest pick.
      const box = (await plan.boundingBox())!;
      expect(box.y + box.height).toBeLessThanOrEqual(600 + 1);
      expect(await inView("#plan-heading")).toBe(true);
      expect(await inView("#act-review-plan")).toBe(true);
      expect(await inView(`#mod-${last}-unpick`)).toBe(true);
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
    expect(await inView(`#mod-${caps[0]}-unpick`)).toBe(true);

    // On through Review to "Match mods to the save", which is never behind the card.
    await openModTools(page);
    const match = page.getByRole("link", { name: "Match mods to the save" });
    for (let i = 0; i < 40; i++) {
      if (await match.evaluate((el) => el === document.activeElement)) break;
      await page.keyboard.press("Tab");
    }
    await expect(match).toBeFocused();
    expect(await match.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return r.top >= 0 && r.bottom <= innerHeight && !!hit && (el === hit || el.contains(hit));
    })).toBe(true);
    await checkPage(page);
  });
});
