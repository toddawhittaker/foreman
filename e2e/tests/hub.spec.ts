import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, type FakeGame, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Server page as the hub (SPEC 17.7, 17.10) and the forms it opens.

const live = { timeout: 20_000 };

async function running(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  seedSecond(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main .state").first()).toHaveText("Running", live);
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  // SPEC 7.6: one tap sets the countdown to 0, which means no warning; there
  // is still no separate checkbox.
  test("No warning sets the countdown to 0 and the stop needs no wait", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await page.getByRole("link", { name: "Stop server" }).click();
    const field = page.getByLabel("Countdown in seconds");
    await expect(field).toHaveValue("60");
    await expect(page.getByRole("checkbox")).toHaveCount(0);
    await checkPage(page);
    await page.getByRole("button", { name: "No warning" }).click();
    await expect(field).toHaveValue("0");
    await expect(field).toBeFocused();
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page).toHaveURL("/");
    await expect(page.locator("main .state").first()).toHaveText("Stopped", live);
  });

  // The same button is on every form that stops the game: here, a switch.
  test("the switch form has No warning beside its countdown", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await page.goto("/worlds/second/switch");
    await page.getByRole("button", { name: "No warning" }).click();
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("0");
  });
});

test.describe("with the fake game and no JavaScript", () => {
  test.use({ fakeGameOn: true, javaScriptEnabled: false });

  // Without script the button cannot work, so it is not shown; the hint
  // says that 0 means no warning.
  test("No warning is hidden and the hint says to type 0", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await page.getByRole("button", { name: "Start server" }).click();
    await expect.poll(async () => {
      await page.goto("/");
      return page.locator("main .state--running").count();
    }, { timeout: 30_000, intervals: [1000] }).toBeGreaterThan(0);
    await page.goto("/worlds/base/stop");
    await expect(page.getByRole("button", { name: "No warning" })).toBeHidden();
    await expect(page.getByText("0 means no warning")).toBeVisible();
  });
});
