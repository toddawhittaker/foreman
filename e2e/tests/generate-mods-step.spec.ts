import { test, expect, checkPage, createAdmin, seedWorld, image21, live } from "./foreman.js";
import { newMapPath } from "./newmap.js";
import type { Locator, Page } from "@playwright/test";

// SPEC 5.6, 9.14, 17.9, 17.10: step 2 of the guided Generate is the world's
// Mods tab and its review, with the step bar and a way on to step 3.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

async function setUp(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

// Makes an Unfinished world on /generate and returns its id.
async function generate(page: Page, name: string): Promise<string> {
  await page.goto("/generate");
  await page.locator("#generate-name").fill(name);
  await page.locator("#generate-button").click();
  await expect(page).toHaveURL(/\/worlds\/[^/?#]+(\/mods)?(\?|$)/);
  return new URL(page.url()).pathname.split("/")[2];
}

// The link is at least 24 by 24 CSS pixels at 320 px wide (WCAG 2.5.8).
async function expectTarget(page: Page, link: Locator) {
  await page.setViewportSize({ width: 320, height: 800 });
  const box = await link.boundingBox();
  expect(box!.width).toBeGreaterThanOrEqual(24);
  expect(box!.height).toBeGreaterThanOrEqual(24);
}

const steps = (page: Page) => page.getByRole("navigation", { name: "Steps to make the world" });

test("the Mods tab of an Unfinished world has the step bar and skips to step 3", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const id = await generate(page, "Flow world");
  await page.goto(`/worlds/${id}/mods`);
  await expect(steps(page).getByRole("link")).toHaveCount(3);
  await expect(steps(page).getByRole("link", { name: "2. Mods" })).toHaveAttribute("aria-current", "step");
  await expect(steps(page).locator("[aria-current]")).toHaveCount(1);
  // Only built-in mods, so step 3 has nothing to set and is plain text (SPEC 5.6).
  await expect(steps(page).getByRole("listitem").nth(2)).toHaveText("3. Mod settings (none)");
  await checkPage(page);
  // The step ends with the wizard's footer: Back to step 1 first, the way on as the primary button (SPEC 5.6).
  const foot = page.locator(".flow-foot");
  await expect(foot.getByRole("link")).toHaveText(["Back to the world", "Next: Map settings"]);
  await expect(foot.getByRole("link", { name: "Back to the world" })).toHaveAttribute("href", `/worlds/${id}`);
  await expect(foot.getByRole("link", { name: "Next: Map settings" })).toHaveClass(/button--primary/);
  await page.screenshot({ path: `../screenshots/step2-footer-${test.info().project.name}.png`, fullPage: true });

  const skip = page.getByRole("link", { name: "Skip to map settings" });
  await expect(skip).toHaveAttribute("href", `${newMapPath(id)}/mod-settings`);
  await expectTarget(page, skip);
  await skip.click();
  await expect(page).toHaveURL(`${newMapPath(id)}?skipped=1`);
  await expect(page.locator("#newmap-skipped")).toBeVisible();
  await expect(steps(page).getByRole("link", { name: "4. Map settings" })).toHaveAttribute("aria-current", "step");
  await expect(page.locator("#generate-form")).toBeVisible({ timeout: 60_000 });
});

test("the review shows the step bar, and the result after an install offers Next: Mod settings", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-flow", "1.0.0", ["base >= 2.0"], "2.1");
  await setUp(page, foreman, fakeGame!);
  const id = await generate(page, "Flow world");
  await page.goto(`/worlds/${id}/mods`);
  await page.getByLabel("Mod name").fill("fp-flow");
  await page.getByRole("button", { name: "Add fp-flow to the plan" }).click();
  await page.getByRole("link", { name: "Review and install 1 mod" }).click();

  await expect(page.getByRole("heading", { level: 1, name: "Install fp-flow in Flow world?" })).toBeVisible();
  await expect(steps(page).getByRole("link", { name: "2. Mods" })).toHaveAttribute("aria-current", "step");
  const skipReview = page.getByRole("link", { name: "Skip to mod settings" });
  await expect(skipReview).toHaveAttribute("href", `${newMapPath(id)}/mod-settings`);
  await checkPage(page);
  await expectTarget(page, skipReview);

  await page.getByRole("button", { name: "Install 1 mod" }).click();
  await expect(page).toHaveURL(new RegExp(`/worlds/${id}/mods`));
  await expect(page.locator("#world-result")).toContainText("Installed fp-flow in Flow world.", live);
  await checkPage(page);
  // The result's next step is a control, and Skip is not offered beside it (SPEC 17.9).
  await expect(page.getByRole("link", { name: /Skip to (mod|map) settings/ })).toHaveCount(0);
  const next = page.locator("#world-result").getByRole("link", { name: "Next: Mod settings" });
  await expect(next).toHaveAttribute("href", `${newMapPath(id)}/mod-settings`);

  // With the mod installed, a review that changes nothing offers Next too.
  await page.goto(`/worlds/${id}/mods/plan?kind=update`);
  const nothing = page.getByRole("link", { name: "Next: Mod settings" });
  await expect(page.getByText("Nothing needs to change")).toBeVisible();
  await expect(nothing).toHaveAttribute("href", `${newMapPath(id)}/mod-settings`);
  await expect(page.getByRole("link", { name: /Skip to (mod|map) settings/ })).toHaveCount(0);
  await checkPage(page);
  // The fake game gives every modded world startup settings, so step 3 shows them.
  await nothing.click();
  await expect(page).toHaveURL(`${newMapPath(id)}/mod-settings`);
  await expect(page.getByLabel("fm-sta-int")).toBeVisible(live);
});

test("a world with a save has no step bar on its Mods tab or review", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-made", "1.0.0", ["base >= 2.0"]);
  await setUp(page, foreman, fakeGame!);
  await page.goto("/worlds/base/mods");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(steps(page)).toHaveCount(0);
  await expect(page.getByRole("link", { name: /Skip to (mod|map) settings/ })).toHaveCount(0);
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-made");
  await expect(page.getByRole("heading", { level: 1, name: /Install fp-made/ })).toBeVisible();
  await expect(steps(page)).toHaveCount(0);
  await expect(page.getByRole("link", { name: /Skip to (mod|map) settings/ })).toHaveCount(0);
});
