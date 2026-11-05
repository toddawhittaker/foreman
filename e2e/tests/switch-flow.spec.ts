import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// One confirmation, one run (SPEC 3.7, 7.9, 17.8): the switch page shows
// every step and every problem before the admin confirms, downloads a
// missing game image itself, and never asks twice.

const live = { timeout: 20_000 };
const image20 = "factoriotools/factorio:2.0.77";

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");
const active = (page: Page, name: string) => page.getByRole("link", { name: `Active world: ${name}` });

async function setUp(page: Page, f: Foreman, g: FakeGame, second: Parameters<typeof seedSecond>[1] = {}) {
  await createAdmin(page);
  seedWorld(f);
  seedSecond(f, second);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

async function openSwitch(page: Page) {
  await page.goto("/worlds/second");
  await page.getByRole("link", { name: "Switch to this world" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Switch to Second");
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("a switch downloads the new world's missing image and starts it in one run", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g, { version: "2.0.77", fixture: "2.0.77-no-space-age.zip" });

    await openSwitch(page);
    const confirm = page.getByRole("region", { name: /Switch from Base to Second/ });
    const steps = confirm.getByRole("listitem");
    await expect(steps.first()).toContainText(`Download the game image ${image20}`);
    await expect(steps.first()).toContainText("If the download fails, nothing else happens.");
    await expect(confirm.getByRole("checkbox", { name: "Start Second after switching" })).toBeChecked();
    // The download is a step of the switch, not a separate action.
    await expect(page.getByRole("button", { name: "Download game image" })).toHaveCount(0);
    await checkPage(page);

    await confirm.getByRole("button", { name: "Switch to Second" }).click();
    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
    await expect(page.locator("main").getByText("The worlds are switched")).toBeVisible(live);
    await checkPage(page);
    const second = (await g.state()).containers.find((c) => c.name === "foreman-second");
    expect(second?.image).toBe(image20);
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  });

  test("problems show before the switch, and leave stopped defers them to Start", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true });
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);

    await openSwitch(page);
    const confirm = page.getByRole("region", { name: /Switch from Base to Second/ });
    await expect(confirm.locator(".message--warning")).toContainText("foreman-test");
    await expect(confirm.getByRole("button", { name: "Switch and start anyway" })).toBeVisible();
    await expect(confirm.getByRole("checkbox")).toHaveCount(1); // the countdown's "No countdown" only
    await checkPage(page);
    await page.getByLabel("Countdown in seconds").fill("0");
    await confirm.getByRole("button", { name: "Switch, leave stopped" }).click();

    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(page.locator("main").getByText("The worlds are switched")).toBeVisible(live);

    // The problems wait for the next Start, which refuses with them once.
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page.getByRole("heading", { name: "Start refused" })).toBeVisible(live);
    await expect(page.locator("main")).toContainText("foreman-test");
    await expect(page.getByRole("button", { name: "Start anyway" })).toBeVisible();
    await expect(page.locator(".steps")).not.toContainText("Skipped");
    await checkPage(page);
  });
});
