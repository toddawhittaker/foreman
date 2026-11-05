import { test, expect, checkPage, createAdmin, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { mkdirSync, readdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Taking and restoring a snapshot asks Docker whether the game is running.
test.use({ fakeGameOn: true });

const world = "/worlds/family-base";

// A world that is not the active one, so a restore finishes on its Snapshots page.
function seedWorld(f: Foreman): string {
  const dir = join(f.dataDir, "worlds", "family-base");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(join(dir, "world.json"), JSON.stringify({ format_version: 1, name: "Family base", game_version: "2.0.72", space_age: true, created_at: "2026-11-01T10:00:00Z" }));
  const save = join(dir, "saves", "base.zip");
  writeFileSync(save, "the first save");
  utimesSync(save, new Date("2026-11-03T14:25:00.123Z"), new Date("2026-11-03T14:25:00.123Z"));
  return dir;
}

async function takeSnapshot(page: Page, label: string) {
  await page.goto(`${world}/snapshots`);
  await page.getByLabel("Label (optional)").fill(label);
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.getByText(`Done: Foreman took the snapshot "${label}".`)).toBeVisible();
}

// A restore takes a "Before restore" snapshot first, which is an automatic one.
async function restore(page: Page, label: string) {
  await page.goto(`${world}/snapshots`);
  await page.getByRole("link", { name: new RegExp(`^Restore ${label}`) }).click();
  await page.getByRole("button", { name: "Restore snapshot" }).click();
  await expect(page).toHaveURL(/\/worlds\/family-base\/snapshots\?undo=/);
}

// Each table sits inside its panel's border, at any width; on a phone it may use the panel's padding.
async function expectTablesFit(page: Page) {
  const outside = await page.evaluate(() =>
    [...document.querySelectorAll("table")]
      .filter((t) => {
        const panel = t.closest<HTMLElement>(".panel")!;
        const p = panel.getBoundingClientRect();
        const style = getComputedStyle(panel);
        const left = p.left + parseFloat(style.borderLeftWidth);
        const right = p.right - parseFloat(style.borderRightWidth);
        const r = t.getBoundingClientRect();
        return r.left < left - 0.5 || r.right > right + 0.5;
      })
      .map((t) => t.querySelector("caption")?.textContent),
  );
  expect(outside, `tables outside their panel on ${page.url()}`).toEqual([]);
}

const rowsOf = (page: Page) => page.locator("tbody tr");
const ticked = (page: Page) => page.locator('input[name="snap"]:checked');

// SPEC 6.12, 17.2: the tab, a world's page and the confirmation fit at any
// width, including 320 px.
test("snapshot pages keep their tables inside the panel", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await takeSnapshot(page, "A label that is long enough to wrap onto several lines on a phone screen");
  await restore(page, "A label");

  await page.goto(`${world}/snapshots`);
  await expectTablesFit(page);
  await checkPage(page);
  await page.goto("/snapshots");
  await expectTablesFit(page);
  await checkPage(page);
  await page.getByRole("button", { name: "Select all automatic", exact: true }).click();
  await expectTablesFit(page);
  await page.getByRole("button", { name: "Delete selected" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete 1 snapshot");
  await checkPage(page);
});

// SPEC 6.12: tick, review on one page, delete; the shortcut only ticks.
test("select all automatic ticks, and delete selected asks first", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await takeSnapshot(page, "Keep me");
  await restore(page, "Keep me");
  await restore(page, "Keep me");

  await page.goto("/snapshots");
  await expect(rowsOf(page)).toHaveCount(3);
  await expect(ticked(page)).toHaveCount(0);

  await page.getByRole("button", { name: "Select all automatic", exact: true }).click();
  await expect(page).toHaveURL(/\/snapshots\?.*select=automatic/);
  await expect(rowsOf(page)).toHaveCount(3);
  await expect(ticked(page)).toHaveCount(2);
  await expect(rowsOf(page).filter({ hasText: "Keep me" }).locator('input[name="snap"]')).not.toBeChecked();
  await checkPage(page);

  await page.getByRole("button", { name: "Delete selected" }).click();
  await expect(page).toHaveURL(/\/snapshots\/delete\?/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete 2 snapshots");
  await expect(page.getByText(/^Free .* of disk space/)).toBeVisible();
  await expect(page.locator(".confirm li", { hasText: /^Family base: "Before restore"/ })).toHaveCount(2);
  await checkPage(page);

  // Nothing is deleted until the confirmation; cancelling keeps all three.
  await page.getByRole("link", { name: "Cancel, keep the snapshots" }).click();
  await expect(page).toHaveURL("/snapshots");
  await expect(rowsOf(page)).toHaveCount(3);

  await page.getByRole("button", { name: "Select all automatic", exact: true }).click();
  await page.getByRole("button", { name: "Delete selected" }).click();
  await page.getByRole("button", { name: "Delete 2 snapshots" }).click();
  await expect(page).toHaveURL(/\/snapshots\?deleted=.*&deleted=/);
  await expect(page.getByText("Done: Foreman deleted 2 snapshots.")).toBeVisible();
  await expect(rowsOf(page)).toHaveCount(1);
  await expect(rowsOf(page).first()).toContainText("Keep me");
  await checkPage(page);

  // From a world's page, with one box ticked by hand.
  await page.goto(`${world}/snapshots`);
  await expect(ticked(page)).toHaveCount(0);
  await page.locator('input[name="snap"]').first().check();
  await page.getByRole("button", { name: "Delete selected" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete 1 snapshot");
  await page.getByRole("link", { name: "Cancel, keep the snapshots" }).click();
  await expect(page).toHaveURL(`${world}/snapshots`);
  await expect(rowsOf(page)).toHaveCount(1);
  await page.locator('input[name="snap"]').first().check();
  await page.getByRole("button", { name: "Delete selected" }).click();
  await page.getByRole("button", { name: "Delete 1 snapshot" }).click();
  await expect(page).toHaveURL(/\/worlds\/family-base\/snapshots\?deleted=/);
  await expect(page.getByText(/No snapshots yet/)).toBeVisible();
});

// The tab groups rows under a linked world, links Restore, and shows a
// reason only when the label does not already say it.
test("the tab groups by world, links Restore, and does not repeat the reason", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await takeSnapshot(page, "Keep me");
  await restore(page, "Keep me");

  await page.goto("/snapshots");
  await expect(page.getByRole("heading", { level: 3 })).toHaveText(["Family base"]);
  await expect(page.getByRole("heading", { level: 3 }).getByRole("link", { name: "Family base" })).toHaveAttribute("href", world);
  const auto = rowsOf(page).filter({ hasText: "Before restore" });
  const cell = auto.locator("td").first();
  await expect(cell).toHaveText(/^\s*Before restore \(taken [^]*Deleting it frees/);
  await expect(cell).not.toHaveText(/Before restore[^]*Before restore \(taken/);
  await auto.getByRole("link", { name: /^Restore Before restore of Family base/ }).click();
  await expect(page.getByRole("heading", { name: /Restore the snapshot "Before restore"/ })).toBeVisible();
});

// "Snapshots add" is what deleting every snapshot frees, so it matches the
// confirmation and the disk afterwards.
test("the total is what deleting everything frees", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedWorld(foreman);
  await takeSnapshot(page, "Old save");
  // The world moves on to a new save file, so the snapshot alone holds the old one.
  const saves = join(dir, "saves");
  for (const f of readdirSync(saves)) rmSync(join(saves, f));
  writeFileSync(join(saves, "base.zip"), "a different save with more bytes in it");
  await takeSnapshot(page, "New save");

  await page.goto("/snapshots");
  const added = (await page.locator("dd").first().textContent())!;
  expect(added).not.toBe("0 B");
  await page.locator('input[name="snap"]').first().check();
  await page.locator('input[name="snap"]').last().check();
  await page.getByRole("button", { name: "Delete selected" }).click();
  await expect(page.getByText(`Free ${added} of disk space`)).toBeVisible();
  await page.getByRole("button", { name: "Delete 2 snapshots" }).click();
  await expect(page.getByText("Done: Foreman deleted 2 snapshots.")).toBeVisible();
  await expect(page.locator("dd").first()).toHaveText("0 B");
});

// A snapshot that is gone counts as deleted already, and is not posted.
test("the confirmation says which snapshots are already deleted", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/snapshots/delete?snap=family-base/20991103T142500Z");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete snapshots");
  await expect(page.getByRole("heading", { name: "Already deleted" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "These stay" })).toHaveCount(0);
  await expect(page.getByText(/is deleted already, so nothing is left to do/)).toBeVisible();
  await expect(page.getByRole("button", { name: /^Delete \d/ })).toHaveCount(0);
  await checkPage(page);

  await page.goto("/snapshots/delete");
  await expect(page.getByText("Nothing is ticked.")).toBeVisible();
  await checkPage(page);
});

// A zone name like "GMT+5:30" is among the widest clock text time.js writes.
test.describe("in a wide time zone", () => {
  test.use({ timezoneId: "Asia/Kolkata" });

  test("the tables still fit their panel", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await takeSnapshot(page, "Wide clock");
    await expectTablesFit(page);
    await page.goto("/snapshots");
    await expectTablesFit(page);
    await checkPage(page);
  });
});
