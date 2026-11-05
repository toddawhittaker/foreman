import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, type FakeGame, type Foreman } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";

// The phone layout (SPEC 17.2, VISION success test, DESIGN 6): the header is
// two short rows, the Server page leads with its controls and progress, the
// console's field sits above the log, and a world in the list stays compact.
// Every project runs these, so each holds at 320 px and at desktop width.

const container = "foreman-base";
const live = { timeout: 20_000 };

const phone = (page: Page) => (page.viewportSize()?.width ?? 1000) <= 320;
const box = async (l: Locator) => (await l.boundingBox())!;
const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");

async function running(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  seedSecond(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(badge(page)).toHaveText("Running", live);
  await g.call("/join", { name: container, player: "alice" });
  await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);
}

test("the header keeps every tab on one row and Sign out beside the name", async ({ page }) => {
  await createAdmin(page);
  const tabs = page.getByRole("navigation", { name: "Main" }).getByRole("link");
  await expect(tabs).toHaveCount(4);
  const tops = new Set<number>();
  for (const tab of await tabs.all()) tops.add(Math.round((await box(tab)).y));
  expect(tops.size, "tabs on one row").toBe(1);

  const product = await box(page.getByRole("link", { name: "Foreman" }));
  const signOut = await box(page.getByRole("button", { name: "Sign out" }));
  const header = await box(page.locator("header.site"));
  // Sign out is on the first row, level with the product name, never on a row of its own.
  expect(Math.abs(signOut.y + signOut.height / 2 - (product.y + product.height / 2))).toBeLessThan(4);
  // Two rows of 44 px targets at most on a phone; one on a wide screen.
  expect(header.height).toBeLessThanOrEqual(phone(page) ? 120 : 64);
  await checkPage(page);
});

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("the Server page puts its controls first, and Force stop weighs less than Stop", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);

    const regions = await page.locator("main > section h2").allTextContents();
    expect(regions.slice(0, 3)).toEqual(["Start and stop", "Status", "Players online"]);

    // On a phone the main action is on the first screen, without scrolling.
    const stop = page.getByRole("link", { name: "Stop server" });
    const viewport = page.viewportSize()!;
    expect((await box(stop)).y + (await box(stop)).height).toBeLessThanOrEqual(viewport.height);

    // Every button in the row has the same width on a phone, forms included.
    const snapshot = page.getByRole("button", { name: "Take snapshot" });
    if (phone(page)) expect((await box(snapshot)).width).toBe((await box(stop)).width);

    // Force stop is a plain link after the buttons, not a button face of its own.
    const force = page.getByRole("link", { name: "Force stop" });
    await expect(force).not.toHaveClass(/button/);
    expect((await box(force)).height).toBeGreaterThanOrEqual(44);
    expect((await box(force)).y).toBeGreaterThanOrEqual((await box(stop)).y);
    expect(await force.evaluate((el) => getComputedStyle(el).backgroundColor)).toBe("rgba(0, 0, 0, 0)");

    // At 320 px the status facts sit two to a row.
    if (phone(page)) {
      const facts = page.getByRole("region", { name: "Status" }).locator(".facts > div");
      expect(Math.round((await box(facts.nth(0))).y)).toBe(Math.round((await box(facts.nth(1))).y));
    }
    await checkPage(page);
  });

  test("a running operation shows each step on one row, under the controls heading", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByLabel("Countdown in seconds").fill("60");
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page).toHaveURL("/");

    const steps = page.locator(".steps > li");
    await expect(steps.filter({ hasText: "Warn players" })).toContainText(/seconds left/, live);
    for (const step of await steps.all()) {
      const word = await box(step.locator(".step-state"));
      const name = await box(step.locator(".step-name"));
      expect(Math.round(name.y), "word and step share a row").toBe(Math.round(word.y));
      expect(name.x).toBeGreaterThan(word.x + word.width);
    }
    // The progress and Cancel stop are on the first screen of a phone.
    const cancel = page.getByRole("button", { name: "Cancel stop" });
    expect((await box(cancel)).y + 44).toBeLessThanOrEqual(page.viewportSize()!.height);
    await checkPage(page);
    await cancel.click();
    await expect(badge(page)).toHaveText("Running", live);
  });

  test("the console's command field sits above the game log, with the reply under it", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await page.getByRole("link", { name: "Console" }).click();

    const field = page.getByLabel("Command");
    const log = page.getByRole("region", { name: "Game log lines" });
    expect((await box(field)).y).toBeLessThan((await box(log)).y);
    expect((await box(field)).y + 44).toBeLessThanOrEqual(page.viewportSize()!.height);

    await field.fill("/players online");
    await page.getByRole("button", { name: "Send" }).click();
    const reply = page.locator("#last-reply");
    await expect(reply).toContainText("alice", live);
    // The newest reply is right under the field, before the log, and the history box fits it.
    const history = page.getByRole("region", { name: "Console history" });
    expect((await box(history)).y).toBeLessThan((await box(log)).y);
    expect((await box(history)).height).toBeLessThan((await box(log)).height);
    await expect(reply).toBeInViewport();
    await checkPage(page);
  });
});

test("a world in the list is compact at 320 px, with Switch beside its name", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await page.goto("/worlds");

  const second = page.locator("ul.worlds > li").filter({ hasText: "Second" });
  const name = await box(second.getByRole("heading", { name: "Second" }));
  const switchTo = await box(second.getByRole("link", { name: "Switch to Second" }));
  // The Switch button shares the name's row.
  expect(switchTo.y).toBeLessThanOrEqual(name.y + name.height);
  expect(switchTo.x).toBeGreaterThan(name.x);
  // Five facts in three short rows, so a world still takes less than the 250 px it used to.
  expect((await box(second)).height).toBeLessThan(phone(page) ? 225 : 140);
  await checkPage(page);
});

test("a long world name wraps beside Switch without widening the page", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await page.goto("/worlds/second");
  const long = "A world with a very long name that goes on and on past sixty chars";
  await page.getByLabel("Name", { exact: true }).fill(long);
  await page.getByRole("button", { name: "Rename world" }).click();
  await page.goto("/worlds");
  const item = page.locator("ul.worlds > li").filter({ hasText: long });
  const name = await box(item.getByRole("heading", { name: long }));
  const switchTo = await box(item.getByRole("link", { name: `Switch to ${long}` }));
  expect(switchTo.x).toBeGreaterThanOrEqual(name.x + name.width);
  await checkPage(page);
});
