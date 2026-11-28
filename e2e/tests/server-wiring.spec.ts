import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, openMenu, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Server page after the admin's own action, its live switch panel, and the
// header at large text (SPEC 7.6, 17.7, 17.9; DESIGN 5, 6, 7.1).

const container = "foreman-base";
const live = { timeout: 20_000 };

const badge = (page: Page) => page.locator("main h1 .state");
const done = (page: Page) => page.locator("#toasts .toast--success").filter({ hasText: "Done:" });
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

  // DESIGN 6, "Toasts": the Done message is a toast, read out and never
  // given focus, after the admin's own action or on a plain load.
  test("after Cancel stop the Done toast is read out and takes no focus", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await g.call("/join", { name: container, player: "alice" });
    await expect(page.locator("#pr-alice")).toBeVisible(live);
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Stop server" }).click();
    // Cancel stop lands on the page after the admin's own action; once its
    // toast shows, the toast takes the parameter out of the address, so a
    // reload shows it no more (DESIGN 6, "Toasts").
    const landed = page.waitForResponse((r) => new URL(r.url()).pathname === "/" && new URL(r.url()).search === "?result=1");
    await page.getByRole("button", { name: "Cancel stop" }).click();
    await landed;
    await expect(done(page)).toContainText("The stop was canceled.", live);
    await expect(page).toHaveURL("/");
    await expect(page.locator("#announcer")).toContainText("The stop was canceled.");
    // The live stream's first update replaces the plate; nothing takes focus.
    await page.waitForTimeout(1000);
    expect(await page.evaluate(() => !!document.activeElement?.closest("#toasts, .plate"))).toBe(false);
    await expect(done(page)).toHaveCount(1);
    await checkPage(page);
  });

  test("a Done toast that arrives by a live update comes once", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(done(page)).toHaveCount(0);
    await page.evaluate(() => fetch("/worlds/base/take-snapshot", { method: "POST", body: new URLSearchParams() }));
    await expect(done(page)).toContainText("Foreman took the snapshot", live);
    // Later updates bring no second copy.
    await page.waitForTimeout(1500);
    await expect(done(page)).toHaveCount(1);
    expect(await focusedId(page)).not.toMatch(/^act-/);
  });

  // The switch starts from the Worlds list (SPEC 17.10); the Server page's
  // link to it is there while Start runs and after, with no reload.
  test("a page loaded during Start keeps its Switch world link once the game runs, without a reload", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
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
test("at 320 px and 200% text the header keeps the menu's words whole", { tag: "@looks" }, async ({ page, foreman }) => {
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
  // On a phone the button shows the user only; the language is the open menu's first row.
  await openMenu(page);
  expect(await lines("details.menu .menu__langs > summary .menu__name")).toBe(1);
  const out = page.getByRole("button", { name: "Sign out" });
  expect((await out.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  const size = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(size.scroll).toBeLessThanOrEqual(size.client);
  await expect(page.getByText("Signed in as admin")).toBeVisible();
});
