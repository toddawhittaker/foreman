import { test, expect, createAdmin, seedWorld, image21, live, stateBadge, type Foreman, type FakeGame } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";

// What the wave 4 re-check of fixes batch 20 left open (DESIGN 6, "Controls
// stay put"; DESIGN 7): a Game settings sub-tab keeps its box when it becomes
// current, the first plate on Server, Console and Chat sits 16 px under the
// header as on every other page, the Server plate's first two buttons keep
// their x in every state, and General's first tile starts where it did before
// the required note changed. Each test walks the widths it needs itself, so it
// runs once, in the first project. Every layout check is 0 px.

test.use({ fakeGameOn: true });

const container = "foreman-base";

async function setUp(page: Page, foreman: Foreman, fakeGame: FakeGame) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

// A box in page coordinates, so a scroll does not count.
const pageBox = (l: Locator) =>
  l.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const n = (v: number) => Math.round(v * 100) / 100;
    return { x: n(r.x + scrollX), y: n(r.y + scrollY), width: n(r.width), height: n(r.height) };
  });

test("a Game settings sub-tab keeps its width and x when it becomes current", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  for (const width of [320, 390, 480, 600, 800, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/worlds/base/settings");
    const subs = page.locator(".subnav__sub a");
    const hrefs = await subs.evaluateAll((as) => as.map((a) => a.getAttribute("href")!));
    expect(hrefs.length).toBeGreaterThan(1);
    const boxes = async () => {
      const out: Record<string, { x: number; width: number }> = {};
      for (const a of await subs.all()) {
        const b = await pageBox(a);
        out[(await a.innerText()).trim()] = { x: b.x, width: b.width };
      }
      return out;
    };
    const first = await boxes();
    for (const href of hrefs) {
      await page.goto(href);
      expect(await boxes(), `${width} ${href}`).toEqual(first);
    }
  }
});

// The distance from the page header's foot to the first part drawn under it.
async function headGap(page: Page, path: string) {
  await page.goto(path);
  return page.locator("main > .pagehead").evaluate((head) => {
    let el = head.nextElementSibling;
    while (el) {
      const s = getComputedStyle(el);
      const r = el.getBoundingClientRect();
      if (s.display !== "none" && s.position !== "absolute" && r.height > 0) break;
      el = el.nextElementSibling;
    }
    return Math.round((el!.getBoundingClientRect().top - head.getBoundingClientRect().bottom) * 100) / 100;
  });
}

test("the first plate on Server, Console and Chat is 16 px under the header", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  for (const width of [390, 600, 768, 1280, 1920]) {
    await page.setViewportSize({ width, height: 1000 });
    for (const path of ["/", "/console", "/chat", "/worlds"]) {
      expect(await headGap(page, path), `${width} ${path}`).toBe(16);
    }
  }
});

// @slow: it walks four server states at four widths.
test("Take snapshot and the button after it keep their x in every server state", { tag: "@slow" }, async ({ page, foreman, fakeGame }) => {
  test.slow();
  const g = fakeGame!;
  await setUp(page, foreman, g);
  const widths = [1280, 1440, 1920, 390];
  const seen: Record<string, Record<string, number>> = {};
  const measure = async (state: string) => {
    for (const width of widths) {
      await page.setViewportSize({ width, height: 1000 });
      await page.goto("/");
      await expect(page.locator("#act-snapshot")).toBeVisible(live);
      // The first action after Take snapshot, whichever the state shows.
      const next = page.locator(".server-actions > *:nth-child(2)").locator("xpath=descendant-or-self::*[self::a or self::button][1]");
      for (const [part, l] of [["snapshot", page.locator("#act-snapshot")], ["next", next]] as const) {
        const key = `${width} ${part}`;
        seen[key] = { ...seen[key], [state]: (await pageBox(l)).x };
      }
    }
  };
  const start = async () => {
    await page.goto("/");
    await page.locator("#act-start").click();
    await expect(stateBadge(page)).toHaveText("Running", live);
  };

  await page.goto("/");
  await measure("stopped");

  // The fake game cannot be held in Starting, which shows Stopped's buttons
  // plus Emergency actions: narrower than Running's row, so the band's
  // column is the same (serverpages_fixes_test.go checks its buttons).
  await start();
  await measure("running");

  // A stop whose save hangs stays in progress.
  await g.call("/save-hangs", { name: container, on: true });
  await page.setViewportSize({ width: 1280, height: 1000 });
  await page.goto("/worlds/base/stop");
  await page.getByLabel("Countdown in seconds").fill("0");
  await page.getByRole("button", { name: "Stop server" }).click();
  await expect(page.locator("#act-stop")).toBeDisabled(live);
  await measure("stopping");

  await page.goto("/worlds/base/force-stop");
  await page.locator("section.confirm").getByRole("button", { name: "Force stop" }).click();
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  await g.call("/save-hangs", { name: container, on: false });
  await start();
  await g.call("/save-hangs", { name: container, on: true });
  await page.goto("/worlds/base/restart");
  await page.getByLabel("Countdown in seconds").fill("0");
  await page.getByRole("button", { name: "Restart server" }).click();
  await expect(page.locator("#act-restart")).toBeDisabled(live);
  await measure("restarting");

  for (const [part, states] of Object.entries(seen)) {
    const [, x] = Object.entries(states)[0];
    expect(states, part).toEqual(Object.fromEntries(Object.keys(states).map((s) => [s, x])));
  }
});

test("General's first tile and label start where they did before the required note changed", async ({ page }) => {
  await createAdmin(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/settings/general");
  const at = await page.locator("form.general-grid").evaluate((form) => {
    const top = form.getBoundingClientRect().top;
    const n = (v: number) => Math.round(v * 100) / 100;
    return {
      rule: n(form.querySelector(".setting-row")!.getBoundingClientRect().top - top),
      label: n(form.querySelector(".setting-row h2")!.getBoundingClientRect().top - top),
      note: getComputedStyle(form.querySelector(".required-note")!).marginBottom,
    };
  });
  expect(at).toEqual({ rule: 56.5, label: 81.5, note: "0px" });
});
