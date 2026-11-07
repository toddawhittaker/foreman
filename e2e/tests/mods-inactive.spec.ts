import { test, expect, checkPage, setUpModsWorld, seedSecond, live, version20 } from "./foreman.js";
import { existsSync } from "node:fs";
import { join } from "node:path";

// SPEC 17.9: a mod change of a world that is not the active one reports on its
// own Mods tab, not on the Server page, with the undo beside the result.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

test("a change to the other world ends on its Mods tab, with an undo that restores its mods", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-other", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!);
  seedSecond(foreman, { version: version20, fixture: "2.0.77-no-space-age.zip" });
  const installed = join(foreman.dataDir, "worlds", "second", "mods", "fp-other_1.0.0.zip");

  await page.goto("/worlds/second/mods");
  await expect(page.getByRole("heading", { level: 1, name: "Second" })).toBeVisible();
  await page.getByLabel("Mod name").fill("fp-other");
  await page.getByRole("button", { name: "Search" }).click();
  await page.getByRole("link", { name: "Install fp-other" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install fp-other in Second?" })).toBeVisible();
  // The world is stopped, so the plan has no countdown and says it stays stopped.
  await expect(page.locator("#countdown")).toHaveCount(0);
  await page.getByRole("button", { name: "Install 1 mod" }).click();

  // The result is on this world's own tab; the Server page is not the place for it.
  await expect(page).toHaveURL(/\/worlds\/second\/mods/);
  const result = page.locator("#world-result");
  await expect(result).toContainText("The mods of Second are changed. Foreman took the snapshot \"Before mod change\" first.", live);
  expect(existsSync(installed)).toBe(true);
  await expect(page.getByRole("table", { name: /Mods of Second/ }).getByRole("row", { name: /fp-other/ })).toContainText("Enabled");
  await checkPage(page);

  // Undo is two clicks: the link, then Restore snapshot.
  await page.getByRole("link", { name: "Undo this mod change" }).click();
  await page.getByRole("button", { name: "Restore snapshot" }).click();
  await expect.poll(() => existsSync(installed), live).toBe(false);
  await page.goto("/worlds/second/mods");
  await expect(page.getByRole("table", { name: /Mods of Second/ }).getByRole("row", { name: /fp-other/ })).toHaveCount(0);
});

test("the active world's change still reports on the Server page", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-here", "1.0.0", ["base >= 2.0"]);
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip" });
  await page.goto("/worlds/base/mods/plan?kind=install&name=fp-here");
  await page.getByRole("button", { name: "Install 1 mod" }).click();
  await expect(page).toHaveURL("/");
  await expect(page.locator(".message--success").filter({ hasText: "Done:" })).toContainText("The mods of Base are changed.", live);
});
