import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, openMenu, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Server page after the admin's own action, its live switch panel, and the
// header at large text (SPEC 7.6, 17.7, 17.9; DESIGN 5, 6, 7.1).

const container = "foreman-base";
const live = { timeout: 20_000 };

const badge = (page: Page) => page.locator("main h1 .state");
const done = (page: Page) => page.locator(".message--success").filter({ hasText: "Done:" });
const focusedId = (page: Page) => page.evaluate(() => document.activeElement?.id ?? "");

async function setUp(page: Page, f: Foreman, g: FakeGame, second = false) {
  await createAdmin(page);
  seedWorld(f);
  if (second) seedSecond(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("after Cancel stop the Done message has focus", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await g.call("/join", { name: container, player: "alice" });
    await expect(page.locator("#pr-alice")).toBeVisible(live);
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Cancel stop" }).click();

    await expect(page).toHaveURL("/?result=1");
    await expect(done(page)).toContainText("The stop was canceled.", live);
    await expect.poll(() => focusedId(page)).toBe("done-message");
    // The live stream's first update replaces the region; focus stays.
    await page.waitForTimeout(1000);
    expect(await focusedId(page)).toBe("done-message");
    await checkPage(page);

    // A plain load of the page, with the same Done message, leaves focus alone.
    await page.goto("/");
    await expect(done(page)).toBeVisible(live);
    expect(await focusedId(page)).not.toBe("done-message");
  });

  test("after Take snapshot the Done message gets focus when it appears, once", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(done(page)).toContainText('Foreman took the snapshot "Manual snapshot" of Base.', live);
    await expect.poll(() => focusedId(page)).toBe("done-message");

    // Once: the mark is spent, so a later update cannot pull focus back.
    await expect(page.locator("[data-focus-result]")).toHaveCount(0);
  });

  test("a Done message that arrives by a live update takes focus too", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    // Loaded as after the admin's own action, with nothing done yet.
    await page.goto("/?result=1");
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(done(page)).toHaveCount(0);
    await page.evaluate(() => fetch("/worlds/base/take-snapshot", { method: "POST", body: new URLSearchParams() }));
    await expect(done(page)).toContainText("Foreman took the snapshot", live);
    await expect.poll(() => focusedId(page)).toBe("done-message");
  });

  // The switch starts from the Worlds list (SPEC 17.10); the Server page's
  // link to it is there while Start runs and after, with no reload.
  test("a page loaded during Start keeps its Switch world link once the game runs, without a reload", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, true);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page).toHaveURL("/");
    await page.evaluate(() => {
      (window as unknown as { loadedOnce: boolean }).loadedOnce = true;
    });
    await expect(badge(page)).toHaveText("Running", live);
    await expect(page.getByRole("link", { name: "Switch world" })).toHaveAttribute("href", "/worlds");
    expect(await page.evaluate(() => (window as unknown as { loadedOnce?: boolean }).loadedOnce)).toBe(true);
    await checkPage(page);
  });
});

// WCAG 1.4.4: at 200% text on a phone, the menu's words each stay whole, the
// menu opens inside the page, and nothing scrolls sideways.
test("at 320 px and 200% text the header keeps the menu's words whole", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto("/worlds");
  await page.evaluate(() => {
    document.documentElement.style.fontSize = "200%";
  });
  const lines = (sel: string) =>
    page.locator(sel).evaluate((el) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      return new Set([...range.getClientRects()].filter((r) => r.width > 0).map((r) => Math.round(r.top))).size;
    });
  expect(await lines("details.menu .menu__user")).toBe(1);
  expect(await lines("details.menu .menu__lang")).toBe(1);
  await openMenu(page);
  const out = page.getByRole("button", { name: "Sign out" });
  expect((await out.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  const size = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(size.scroll).toBeLessThanOrEqual(size.client);
  await expect(page.getByText("Signed in as admin")).toBeVisible();
});
