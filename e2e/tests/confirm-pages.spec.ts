import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21 } from "./foreman.js";
import type { Page } from "@playwright/test";

// DESIGN 7.4, SPEC 17: a confirmation page opens with its one heading, names
// the thing, has at most one primary or danger button, and leaves by Cancel or
// the header tabs. The projects run it at 1280 and at 320 px, in both themes.
test.use({ fakeGameOn: true });
test.setTimeout(120_000);

const live = { timeout: 20_000 };

async function expectPlainConfirmation(page: Page, where: string, opts: { backLink?: boolean } = {}) {
  const main = page.locator("main");
  await expect(page.getByRole("heading", { level: 1 }), `${where}: one h1`).toHaveCount(1);
  const h1 = (await page.getByRole("heading", { level: 1 }).innerText()).trim();
  // The heading comes first: nothing, such as a Back link, sits above it.
  expect(await main.evaluate((m) => m.firstElementChild?.tagName), `${where}: the h1 is first`).toBe("H1");
  const others = await main.locator("h2, h3").allInnerTexts();
  expect(others.map((t) => t.trim()), `${where}: no heading repeats the h1`).not.toContain(h1);
  expect(await main.locator("form h2").count(), `${where}: no second heading inside the form`).toBe(0);
  await expect(main.getByRole("link", { name: /^Back to/ }), `${where}: Back links`).toHaveCount(opts.backLink ? 1 : 0);
  expect(await main.locator(".button--primary, .button--danger").count(), `${where}: one main action`).toBeLessThanOrEqual(1);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow, `${where}: no sideways scroll`).toBeLessThanOrEqual(0);
  await checkPage(page);
}

test("every confirmation page has one heading, no Back link and one main action", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });

  // Pages that need the game stopped or running.
  await page.goto("/");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.getByText(/Done: Foreman took the snapshot/)).toBeVisible();
  await page.goto("/worlds/base/update");
  await expectPlainConfirmation(page, "update");
  await page.goto("/worlds/second/switch");
  await expectPlainConfirmation(page, "switch");
  await page.goto("/worlds/base/switch");
  await expectPlainConfirmation(page, "switch refused", { backLink: true });
  await page.goto("/worlds/base/delete");
  await expectPlainConfirmation(page, "delete refused", { backLink: true });

  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main h1 .state")).toHaveText("Running", live);
  await page.goto("/worlds/base/stop");
  await expectPlainConfirmation(page, "stop");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Stop Base");
  await page.goto("/worlds/base/restart");
  await expectPlainConfirmation(page, "restart");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Restart Base");
  await page.goto("/worlds/base/force-stop");
  await expectPlainConfirmation(page, "force stop");
  await page.goto("/worlds/base/stop");
  await page.getByRole("button", { name: "Stop server" }).click();
  await expect(page.locator("main h1 .state")).toHaveText("Stopped", live);

  // Snapshot pages.
  await page.goto("/worlds/base/snapshots");
  await page.getByRole("link", { name: /^Restore Manual snapshot/ }).first().click();
  await expectPlainConfirmation(page, "restore");
  await page.goto("/worlds/base/snapshots");
  await page.getByRole("link", { name: /^Delete Manual snapshot/ }).first().click();
  await expectPlainConfirmation(page, "snapshot delete");
  await page.goto("/snapshots");
  await page.locator('input[name="snap"]').first().check();
  await page.getByRole("button", { name: "Delete selected" }).click();
  await expectPlainConfirmation(page, "bulk delete");

  // A deleted world's page and its restore.
  await page.goto("/worlds/second/delete");
  await expectPlainConfirmation(page, "delete");
  await page.getByLabel("Type the world's name to confirm").fill("Second");
  await page.getByRole("button", { name: "Delete world" }).click();
  await page.goto("/deleted");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Deleted worlds");
  await expect(page.locator("main").getByRole("link", { name: /^Back to/ })).toHaveCount(0);
  expect(await page.locator("main").evaluate((m) => m.firstElementChild?.tagName)).toBe("H1");
  await checkPage(page);
  await page.getByRole("link", { name: /^Restore/ }).first().click();
  await expectPlainConfirmation(page, "restore deleted world");
});
