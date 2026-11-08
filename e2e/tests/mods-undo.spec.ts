import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, live, type Foreman } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.4 (ADR 0014), 17.5, 17.9, 17.10: on a stopped world a run of mod
// changes shares one "Before mod change" snapshot. After each change the
// Server page offers the undo; after the first of a run it names when the run
// began, and its confirmation says what the undo does to the mods.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const mods = { "fp-a": "2.1", "fp-b": "2.1" };
const kept = /Foreman took no new snapshot: "Before mod change" from .+ undoes this change and the ones before it\./;
const took = 'Foreman took the snapshot "Before mod change" first.';
const undoOne = (page: Page) => page.getByRole("link", { name: "Undo this mod change", exact: true });
const undoRun = (page: Page) => page.getByRole("link", { name: /^Undo the mod changes since \d{4}-\d\d-\d\d \d\d:\d\d UTC$/ });

// The world's "Before mod change" snapshots, oldest first.
function modSnapshots(f: Foreman): { name: string; takenAt: string }[] {
  const dir = join(f.dataDir, "snapshots", "base");
  if (!existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isDirectory() && existsSync(join(dir, e.name, "snapshot.json")))
    .map((e) => ({ name: e.name, ...JSON.parse(readFileSync(join(dir, e.name, "snapshot.json"), "utf8")) }))
    .filter((s) => s.reason === "mods")
    .map((s) => ({ name: s.name, takenAt: s.taken_at }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

const modOn = (f: Foreman, name: string): boolean | undefined =>
  (JSON.parse(readFileSync(join(f.dataDir, "worlds", "base", "mods", "mod-list.json"), "utf8")).mods as { name: string; enabled: boolean }[]).find((m) => m.name === name)?.enabled;

// One mod change from its plan page; the active world's result is on the Server page.
async function change(page: Page, kind: string, name: string, button: string) {
  await page.goto(`/worlds/base/mods/plan?kind=${kind}&name=${name}`);
  // A running game's save uses every enabled mod, so turning one off asks first.
  const drops = page.locator("#confirm-drops");
  if (await drops.count()) await drops.check();
  await page.getByRole("button", { name: button }).click();
  await expect(page).toHaveURL("/");
  await expect(doneBox(page)).toBeVisible(live);
}

// The time the Done box gives for the snapshot the change kept.
const keptAt = (page: Page) => doneBox(page).locator("time").first().getAttribute("datetime");

test("three changes on a stopped world share one snapshot, and undo lists and puts back the mods from before the first", async ({ page, foreman, fakeGame, fakePortal }) => {
  await fakePortal!.add("fp-new", "1.0.0", ["base >= 2.0"], "2.1");
  await setUpModsWorld(page, foreman, fakeGame!, { mods });

  await change(page, "disable", "fp-a", "Disable 1 mod");
  await expect(doneBox(page)).toContainText(`Turned off fp-a in Base. ${took}`);
  await expect(undoOne(page)).toBeVisible();
  const [first] = modSnapshots(foreman);
  expect(first).toBeDefined();
  await expect(undoOne(page)).toHaveAttribute("href", `/worlds/base/snapshots/${first.name}/restore`);

  await change(page, "install", "fp-new", "Install 1 mod");
  await expect(doneBox(page)).toContainText("Installed fp-new in Base.");
  await expect(doneBox(page)).toContainText(kept);
  expect(await keptAt(page)).toBe(first.takenAt.replace(/\.\d+Z$/, "Z"));
  await expect(undoRun(page)).toHaveAttribute("href", `/worlds/base/snapshots/${first.name}/restore`);
  await expect(undoOne(page)).toHaveCount(0);

  await change(page, "disable", "fp-b", "Disable 1 mod");
  await expect(doneBox(page)).toContainText("Turned off fp-b in Base.");
  await expect(doneBox(page)).toContainText(kept);
  await expect(undoRun(page)).toHaveAttribute("href", `/worlds/base/snapshots/${first.name}/restore`);
  expect(modSnapshots(foreman)).toHaveLength(1);
  await checkPage(page);

  // Undo is two clicks, and its confirmation says what it rolls back.
  const clicks = await countClicks(page);
  await clicks.reset();
  await undoRun(page).click();
  await expect(page.getByRole("heading", { level: 1, name: "Undo the mod changes in Base?" })).toBeVisible();
  await expect(page.locator("main")).toContainText('The mods go back to how they were in the snapshot "Before mod change" from');
  const steps = page.locator("#mod-undo > li");
  await expect(steps).toHaveText(["Remove fp-new 1.0.0.", "Turn fp-a and fp-b back on."]);
  await checkPage(page);
  await page.getByRole("button", { name: "Restore snapshot" }).click();
  await expect(doneBox(page)).toContainText("The world is restored.", live);
  await expectWithin(clicks, 2, "undo a run of mod changes");
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "fp-new_1.0.0.zip"))).toBe(false);
  expect(modOn(foreman, "fp-a")).toBe(true);
  expect(modOn(foreman, "fp-b")).toBe(true);
});

test("after a start and a stop the next change takes a new snapshot, and the undo moves to it", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { mods });
  await change(page, "disable", "fp-a", "Disable 1 mod");
  const [first] = modSnapshots(foreman);

  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
  await page.getByRole("link", { name: "Stop server" }).click();
  await page.getByRole("button", { name: "Stop server" }).click();
  await expect(stateBadge(page)).toHaveText("Stopped", live);

  await change(page, "disable", "fp-b", "Disable 1 mod");
  await expect(doneBox(page)).toContainText(`Turned off fp-b in Base. ${took}`);
  const snaps = modSnapshots(foreman);
  expect(snaps).toHaveLength(2);
  expect(snaps[1].takenAt > first.takenAt).toBe(true);
  await expect(undoOne(page)).toHaveAttribute("href", `/worlds/base/snapshots/${snaps[1].name}/restore`);

  // The new run shares the new snapshot.
  await change(page, "enable", "fp-a", "Enable 1 mod");
  await expect(doneBox(page)).toContainText(kept);
  expect(await keptAt(page)).toBe(snaps[1].takenAt.replace(/\.\d+Z$/, "Z"));
  await expect(undoRun(page)).toHaveAttribute("href", `/worlds/base/snapshots/${snaps[1].name}/restore`);
  expect(modSnapshots(foreman)).toHaveLength(2);
});

test("on a running world each change takes its own snapshot", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { mods, start: true });
  await change(page, "disable", "fp-a", "Disable 1 mod");
  await expect(doneBox(page)).toContainText(`Turned off fp-a in Base. ${took}`, live);
  await expect(stateBadge(page)).toHaveText("Running", live);
  await change(page, "disable", "fp-b", "Disable 1 mod");
  await expect(doneBox(page)).toContainText(`Turned off fp-b in Base. ${took}`, live);
  await expect(stateBadge(page)).toHaveText("Running", live);
  const snaps = modSnapshots(foreman);
  expect(snaps).toHaveLength(2);
  await expect(undoOne(page)).toHaveAttribute("href", `/worlds/base/snapshots/${snaps[1].name}/restore`);
  await expect(undoRun(page)).toHaveCount(0);
});

test("a settings save between two mod changes ends the sharing", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { mods });
  await change(page, "disable", "fp-a", "Disable 1 mod");

  await page.goto("/worlds/base/settings");
  await page.getByLabel("Maximum players").fill("8");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toContainText("Settings saved.");

  await change(page, "disable", "fp-b", "Disable 1 mod");
  await expect(doneBox(page)).toContainText(`Turned off fp-b in Base. ${took}`);
  const snaps = modSnapshots(foreman);
  expect(snaps).toHaveLength(2);
  await expect(undoOne(page)).toHaveAttribute("href", `/worlds/base/snapshots/${snaps[1].name}/restore`);
});
