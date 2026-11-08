import { test, expect, checkPage, setUpModsWorld, doneBox, live, stateBadge, version20, type FakeGame, type FakePortal, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync } from "node:fs";
import { join } from "node:path";

// SPEC 7.4, 9.3: the pre-start check refuses a world using a mod version the
// mod portal no longer offers, offers the update first, and fails safe when
// the portal cannot be asked. The Server page's notice follows the fix
// without a reload (issue 362). The portal's generated mods are made for
// Factorio 2.0, so these worlds are 2.0 worlds.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const notice = (page: Page) => page.locator("#mods-notice");
const refusal = (page: Page) => page.locator(".message").filter({ hasText: "Foreman did not start the game, because players could not join it:" });
const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });

// A Space Age world whose visible-planets 1.5.5 is gone from the portal, which has 1.7.2.
async function goneWorld(page: Page, f: Foreman, g: FakeGame, portal: FakePortal) {
  await portal.add("visible-planets", "1.7.2", ["base >= 2.0", "space-age >= 2.0"]);
  await setUpModsWorld(page, f, g, {
    version: version20,
    fixture: "2.0.77-space-age.zip",
    spaceAge: true,
    mods: { "visible-planets": { factorio: "2.0", version: "1.5.5", deps: ["base >= 2.0", "space-age >= 2.0"] } },
  });
}

test("a start refused for a mod players cannot download: update first, then start", async ({ page, foreman, fakeGame, fakePortal }) => {
  await goneWorld(page, foreman, fakeGame!, fakePortal!);
  await page.getByRole("button", { name: "Start server" }).click();
  const box = refusal(page);
  await expect(box).toContainText("The mod portal no longer has visible-planets 1.5.5, so players' games cannot download it and they cannot join. Going ahead anyway deletes nothing from the save.", live);
  await expect(stateBadge(page)).toHaveText("Stopped");
  // The update is the first button; start anyway waits behind its own choice.
  const fix = box.getByRole("link", { name: "Update visible-planets" });
  await expect(fix).toHaveAttribute("href", "/worlds/base/mods/plan?kind=update&name=visible-planets");
  await expect(box.locator(".actions").first().locator("a, button").first()).toHaveText("Update visible-planets");
  await expect(box.getByText("Or start anyway")).toBeVisible();
  await checkPage(page);
  await shot(page, "prestart-gone-refused");

  await fix.click();
  await page.getByRole("button", { name: "Update 1 mod" }).click();
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "visible-planets_1.7.2.zip"))).toBe(true);
  await expect(notice(page)).toBeEmpty(live);

  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
  // The stop link comes in the same render as the note would, once the start has ended.
  await expect(page.locator("#act-stop")).toBeVisible(live);
  await expect(page.locator("#portal-unchecked")).toHaveCount(0);
});

for (const down of [
  { name: "the portal cannot be reached", set: (p: FakePortal) => p.setDown(true) },
  { name: "the portal answers with an error", set: (p: FakePortal) => p.failApi(503) },
]) {
  test(`${down.name}: the start goes ahead and says the portal was not checked`, async ({ page, foreman, fakeGame, fakePortal }) => {
    // Down before the first page asks, so no answer is cached (SPEC 9.1).
    await down.set(fakePortal!);
    await goneWorld(page, foreman, fakeGame!, fakePortal!);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(page.locator("#portal-unchecked")).toHaveText(
      "Note: Foreman could not reach the mod portal in time, so it did not check that players can download this world's mods.",
    );
    await expect(refusal(page)).toHaveCount(0);
    await checkPage(page);
  });
}

// Issue 362: the notice loads again when the fix ends, and takes no room once empty.
test("the Server page notice goes once its fix is done, without a reload", async ({ page, foreman, fakeGame, fakePortal }) => {
  // The game runs with the mod: it started while the portal could not be asked.
  await fakePortal!.setDown(true);
  await goneWorld(page, foreman, fakeGame!, fakePortal!);
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
  await fakePortal!.setDown(false);
  await page.goto("/");
  await expect(notice(page)).toContainText("visible-planets 1.5.5 is gone from the mod portal, so players cannot join.", live);

  await notice(page).getByRole("link", { name: "Update visible-planets" }).click();
  await page.getByRole("button", { name: "Update 1 mod" }).click();
  await expect(doneBox(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
  await expect(stateBadge(page)).toHaveText("Running", live);
  await expect(notice(page)).toBeEmpty(live);
  await expect(notice(page)).toHaveAttribute("role", "status");
  expect((await notice(page).boundingBox())?.height ?? 0).toBe(0);
  await checkPage(page);
});

// SPEC 9.3: the notice loads again when any operation ends, even one this page did not start,
// but an unchanged answer is not swapped in, so a screen reader does not read it again.
test("an operation that leaves the notice as it was reloads it without swapping it", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.setDown(true);
  await goneWorld(page, foreman, fakeGame!, fakePortal!);
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
  await fakePortal!.setDown(false);
  await page.goto("/");
  await expect(notice(page)).toContainText("visible-planets 1.5.5 is gone from the mod portal, so players cannot join.", live);
  await notice(page).evaluate((el) => {
    const w = window as unknown as { noticeChanges: number; noticeLoads: number };
    w.noticeChanges = 0;
    w.noticeLoads = 0;
    new MutationObserver(() => w.noticeChanges++).observe(el, { childList: true, subtree: true, characterData: true });
    // htmx fires this once the answer is handled, swapped or not.
    el.addEventListener("htmx:afterRequest", () => w.noticeLoads++);
  });

  // Another tab stops the game; this page only follows it live.
  const reloaded = page.waitForResponse("**/worlds/base/mods/notice");
  const other = await page.context().newPage();
  await other.goto("/");
  await other.getByRole("link", { name: "Stop server" }).click();
  await other.getByRole("button", { name: "Stop server" }).click();
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  expect((await reloaded).status()).toBe(200);
  await expect.poll(() => page.evaluate(() => (window as unknown as { noticeLoads: number }).noticeLoads), live).toBe(1);
  await expect(notice(page)).toContainText("visible-planets 1.5.5 is gone");
  expect(await page.evaluate(() => (window as unknown as { noticeChanges: number }).noticeChanges)).toBe(0);
  await other.close();
});
