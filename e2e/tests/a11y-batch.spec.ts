import { test, expect, createAdmin, seedWorld, image21, type FakeGame, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";

// Accessibility fixes found in the UI batch review (SPEC 17, WCAG 2.2 AA):
// state changes on the Console page are spoken, the open user menu never
// hides the focus, headings follow on from the h1, and Copy address is
// announced every time.

const live = { timeout: 20_000 };
const badge = (page: Page) => page.locator("main h1 .state");

async function started(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(badge(page)).toHaveText("Running", live);
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  // The Console page follows the live stream, so a crash is spoken there too.
  test("the Console page announces a crash", async ({ page, foreman, fakeGame }) => {
    await started(page, foreman, fakeGame!);
    await page.goto("/console");
    await expect(badge(page)).toHaveText("Running");
    await fakeGame!.call("/crash", { name: "foreman-base" });
    await expect(page.locator("#announcer")).toHaveText("Server is now crashed", live);
  });

  // Headings go h1, then h2, never h3 straight after the h1, even while an
  // operation shows its step list.
  test("the headings during an operation do not skip a level", async ({ page, foreman, fakeGame }) => {
    await started(page, foreman, fakeGame!);
    await fakeGame!.call("/join", { name: "foreman-base", player: "alice" });
    await expect(page.locator("#player-row-alice")).toBeVisible(live);
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page.locator(".steps li").filter({ hasText: "Warn players" })).toBeVisible(live);
    const levels = await page.locator("main").getByRole("heading").evaluateAll((els) => els.map((el) => Number(el.tagName.slice(1))));
    expect(levels[0]).toBe(1);
    expect(levels.length).toBeGreaterThan(2);
    levels.forEach((level, i) => {
      if (i > 0) expect(level, `heading ${i} after ${levels.slice(0, i)}`).toBeLessThanOrEqual(levels[i - 1] + 1);
    });
  });
});

// Tab from the last item of the open menu moves on and closes the menu, so
// the control that takes focus is not behind the panel (WCAG 2.4.11).
test("tabbing out of the user menu closes it and leaves the focus visible", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/");
  const menu = page.locator("details.menu");
  await menu.locator("summary").focus();
  await page.keyboard.press("Enter");
  await expect(menu).toHaveAttribute("open", "");
  await page.getByRole("button", { name: "Sign out" }).focus();
  await page.keyboard.press("Tab");
  await expect(menu).not.toHaveAttribute("open", "");
  const covered = await page.evaluate(() => {
    const el = document.activeElement!;
    const r = el.getBoundingClientRect();
    const top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return !(top === el || el.contains(top));
  });
  expect(covered).toBe(false);
});

// The other ways to close still work after the focus rule is added.
test("Escape returns focus to the menu button and a click outside closes the menu", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/");
  const menu = page.locator("details.menu");
  await menu.locator("summary").focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Escape");
  await expect(menu).not.toHaveAttribute("open", "");
  await expect(menu.locator("summary")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(menu).toHaveAttribute("open", "");
  await page.mouse.click(5, 600);
  await expect(menu).not.toHaveAttribute("open", "");
});

test.describe("with a public address", () => {
  test.use({ foremanEnv: { FOREMAN_PUBLIC_ADDRESS: "factorio.example.com" } });

  // A screen reader speaks a live region only when its text changes, so the
  // second press has to empty the region and fill it again.
  test("Copy address is announced on every press", async ({ page, foreman, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/");
    await page.evaluate(() => {
      const seen: string[] = [];
      (window as unknown as { seen: string[] }).seen = seen;
      const announcer = document.getElementById("announcer")!;
      new MutationObserver(() => seen.push(announcer.textContent!)).observe(announcer, { childList: true, characterData: true, subtree: true });
    });
    const copy = page.getByRole("button", { name: "Copy address" });
    const announcer = page.locator("#announcer");
    await copy.click();
    await expect(announcer).toHaveText("Address copied");
    await copy.click();
    await expect(async () => {
      const seen = await page.evaluate(() => (window as unknown as { seen: string[] }).seen);
      expect(seen.filter((s) => s === "Address copied").length).toBe(2);
      expect(seen).toContain("");
    }).toPass();
    await expect(announcer).toHaveText("Address copied");
  });
});
