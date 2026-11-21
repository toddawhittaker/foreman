import { test, expect, createAdmin, seedWorld, openModTools } from "./foreman.js";

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
});
