import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// Accessibility of the Server page's stop countdown and step list, and the
// Worlds list's Switch links (SPEC 7.6, 17; DESIGN 3, 5, 7.6).

const container = "foreman-base";
const live = { timeout: 20_000 };

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");
const done = (page: Page) => page.locator(".message--success").filter({ hasText: "Done:" });
const warn = (page: Page) => page.locator(".steps li").filter({ hasText: "Warn players" });

async function stopWithAlice(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(badge(page)).toHaveText("Running", live);
  await g.call("/join", { name: container, player: "alice" });
  await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);
  await page.getByRole("link", { name: "Stop server" }).click();
  await page.getByRole("button", { name: "Stop server" }).click();
  await expect(warn(page)).toContainText(/\d+ seconds left/, live);
}

test.describe("stop countdown", () => {
  test.use({ fakeGameOn: true });

  test("the time left counts down in place, and the buttons stay put", async ({ page, foreman, fakeGame }) => {
    await stopWithAlice(page, foreman, fakeGame!);

    // The text changes each second without the step or its list being replaced.
    const step = await warn(page).elementHandle();
    const first = await warn(page).innerText();
    await expect(warn(page)).not.toHaveText(first, { timeout: 3000 });
    expect(await step!.evaluate((el) => el.isConnected)).toBe(true);
    // Not a live region: a screen reader is not told every second.
    expect(await page.locator("[data-until]").evaluate((el) => el.closest("[aria-live], [role=status], [role=alert]"))).toBeNull();

    // Focus stays on the same Cancel stop element across the countdown.
    const cancel = page.getByRole("button", { name: "Cancel stop" });
    await cancel.focus();
    const held = await cancel.elementHandle();
    await page.waitForTimeout(3000);
    expect(await held!.evaluate((el) => el.isConnected && document.activeElement === el)).toBe(true);
    await checkPage(page);

    // A slow tap that spans more than a second still cancels the stop.
    await cancel.hover();
    await page.mouse.down();
    await page.waitForTimeout(1500);
    await page.mouse.up();
    await expect(done(page)).toContainText("The stop was cancelled.", live);
    await expect(badge(page)).toHaveText("Running");
  });

  test("at 320 px and 200% text, each step's name stays readable", async ({ page, foreman, fakeGame }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await stopWithAlice(page, foreman, fakeGame!);
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    const names = page.locator(".steps .step-name");
    expect(await names.count()).toBeGreaterThan(1);
    for (const width of await names.evaluateAll((els) => els.map((el) => el.getBoundingClientRect().width))) {
      expect(width).toBeGreaterThan(100);
    }
    await expect(warn(page).locator(".step-name")).toBeVisible();
  });
});

test("each Switch on the Worlds list names its world to a screen reader", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await page.goto("/worlds");
  const link = page.getByRole("link", { name: "Switch to Second", exact: true });
  await expect(link).toBeVisible();
  // The visible word stays "Switch", beside the world's name.
  await expect(link.locator('[aria-hidden="true"]')).toHaveText("Switch");
  await checkPage(page);
});
