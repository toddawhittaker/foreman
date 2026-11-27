import { test, expect, checkPage, createAdmin, seedWorld, image21, live, openModTools } from "./foreman.js";
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

test("the Mods tab of an Unfinished world has the step bar and skips to step 3", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
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

// SPEC 5.6: with a plan, step 2's main button is "Next: Review N mods". The
// review keeps step 2 current and ends with the step's footer; its Next
// installs the plan with the same checks and snapshot as Install, shows the
// install's progress, then goes on to step 3 with the mod installed.
test("step 2's Next reviews the plan, and the review's Next installs it and goes on to step 3", { tag: "@looks" }, async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-flow", "1.0.0", ["base >= 2.0"], "2.1");
  await setUp(page, foreman, fakeGame!);
  const id = await generate(page, "Flow world");
  await page.goto(`/worlds/${id}/mods`);
  await page.getByLabel("Mod name").fill("fp-flow");
  await page.getByRole("button", { name: "Add fp-flow to the plan" }).click();
  const foot = page.locator(".flow-foot");
  // The footer's main button now reviews the plan, and the panel has no Review of its own.
  const next = foot.getByRole("link", { name: "Next: Review 1 mod" });
  await expect(next).toHaveClass(/button--primary/);
  await expect(page.getByRole("link", { name: /Review and install/ })).toHaveCount(0);
  // At the top of the page nothing covers it; on a phone the plan bar does not hold to the foot over it.
  await page.evaluate(() => window.scrollTo(0, 0));
  expect(await next.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return el.contains(document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2));
  })).toBe(true);
  await checkPage(page);
  const phone = test.info().project.name.startsWith("phone");
  const shot = (name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png` });
  if (!phone) await page.setViewportSize({ width: 1280, height: 900 });
  await shot("step2-plan");
  await next.click();

  await expect(page.getByRole("heading", { level: 1, name: "Install fp-flow in Flow world?" })).toBeVisible();
  await expect(steps(page).getByRole("link", { name: "2. Mods" })).toHaveAttribute("aria-current", "step");
  // Back first, the skip as the secondary way on, then Next; no Cancel or Install of its own.
  await expect(foot.locator("a, button")).toHaveText(["Back to the mods", "Skip to mod settings", "Next: Mod settings"]);
  await expect(foot.getByRole("link", { name: "Back to the mods" })).toHaveAttribute("href", `/worlds/${id}/mods`);
  await expect(foot.getByRole("link", { name: "Skip to mod settings" })).toHaveAttribute("href", `${newMapPath(id)}/mod-settings`);
  await expect(page.getByRole("button", { name: /^Install/ })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Cancel, change nothing" })).toHaveCount(0);
  // What Next does is said beside it: the snapshot first, then the change.
  await expect(page.locator("#plan-steps + ol")).toContainText('Take a snapshot labeled "Before mod change".');
  // The install goes on to step 3, so the page promises no Undo beside a result.
  await expect(page.locator("#mod-plan")).not.toContainText("one-click Undo");
  await checkPage(page);
  await expectTarget(page, foot.getByRole("link", { name: "Skip to mod settings" }));
  if (!phone) await page.setViewportSize({ width: 1280, height: 900 });
  await shot("step2-review");

  await foot.getByRole("button", { name: "Next: Mod settings" }).click();
  // The fake game gives every modded world startup settings, so step 3 shows them.
  await expect(page).toHaveURL(`${newMapPath(id)}/mod-settings`, live);
  await expect(page.getByLabel("fm-sta-int")).toBeVisible(live);
  await expect(steps(page).getByRole("link", { name: "3. Mod settings" })).toHaveAttribute("aria-current", "step");
  await page.goto(`/worlds/${id}/mods`);
  await expect(page.locator('section[aria-labelledby="installed-heading"]')).toContainText("fp-flow");
  await expect(page.locator("#plan-panel")).toHaveClass(/plan-panel--empty/);

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

// SPEC 5.6, 9.14: skipping step 2 with a plan installs nothing and keeps
// the plan, so the Worlds list's Continue opens step 2 again with it; the
// review's Back to the mods keeps it too.
// Step 2 opens on Find a mod, but a refused upload's error is in the mod
// tools under Installed mods, so the refused page shows that view, with the
// error and its field in sight.
test("a refused mod upload in step 2 shows its error", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await generate(page, "Refused");
  await expect(page.locator("#mods-view-find")).toBeChecked();
  await openModTools(page);
  await page.locator("#mod-upload > summary").click();
  await page.locator("#mod-file").setInputFiles({ name: "junk.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
  await page.getByRole("button", { name: "Upload mod", exact: true }).click();
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.locator("#mods-view-installed")).toBeChecked();
  await expect(page.locator("#mod-file-error")).toBeVisible();
  await expect(page.locator("#mod-file-error")).toContainText("This file is not a mod zip");
  await page.locator("#error-summary a").first().click();
  await expect(page.locator("#mod-file")).toBeFocused();
  await checkPage(page);
});

test("a skip or Back with a plan keeps the plan for Continue", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-kept", "1.0.0", ["base >= 2.0"], "2.1");
  await setUp(page, foreman, fakeGame!);
  const id = await generate(page, "Kept world");
  await page.goto(`/worlds/${id}/mods`);
  await page.getByLabel("Mod name").fill("fp-kept");
  await page.getByRole("button", { name: "Add fp-kept to the plan" }).click();
  await page.locator(".flow-foot").getByRole("link", { name: "Next: Review 1 mod" }).click();
  await page.locator(".flow-foot").getByRole("link", { name: "Back to the mods" }).click();
  await expect(page).toHaveURL(`/worlds/${id}/mods`);
  await expect(page.locator("#plan-panel .plan-tree__pick")).toHaveCount(1);
  await page.getByRole("link", { name: "Skip to map settings" }).click();
  await expect(page).toHaveURL(new RegExp(`${newMapPath(id)}`));
  await page.goto("/worlds");
  await page.getByRole("link", { name: "Continue setting up Kept world" }).click();
  await expect(page).toHaveURL(`/worlds/${id}/mods`);
  await expect(page.locator("#plan-panel .plan-tree__pick")).toHaveCount(1);
  await expect(page.locator(".flow-foot").getByRole("link", { name: "Next: Review 1 mod" })).toBeVisible();
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
