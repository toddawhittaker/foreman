import { test, expect, checkPage, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { mkdirSync, readFileSync, rmSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Snapshots and restores ask Docker whether the game is running first
// (SPEC 6.5, 6.10), so these tests use the fake game as Docker.
test.use({ fakeGameOn: true });

const admin = "admin";
const password = "correct horse battery";

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill(admin);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
  await page.goto("/worlds");
}

const savedAt = new Date("2026-11-03T14:25:00.123Z");

// A world is a folder a person can make by hand (SPEC 5.1).
function seedWorld(f: Foreman, id: string): string {
  const dir = join(f.dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(join(dir, "world.json"), JSON.stringify({ format_version: 1, name: "Family base", game_version: "2.0.72", space_age: true, created_at: "2026-11-01T10:00:00Z" }));
  const save = join(dir, "saves", "base.zip");
  writeFileSync(save, "the first save");
  utimesSync(save, savedAt, savedAt);
  return dir;
}

test("empty snapshot list names the next step", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "family-base");
  await page.goto("/worlds/family-base/snapshots");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Snapshots of Family base");
  await expect(page.getByText(/No snapshots yet/)).toBeVisible();
  await expect(page.getByText("Free space")).toBeVisible();
  await expect(page.getByRole("link", { name: "Back to Family base" })).toHaveAttribute("href", "/worlds/family-base");
  await checkPage(page);
});

// SPEC 6.3, 6.4, 6.5 and 6.9: take, list, then restore with a confirmation.
test("take a snapshot, then restore it", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedWorld(foreman, "family-base");
  await page.goto("/worlds/family-base/snapshots");

  await page.getByLabel("Label (optional)").fill("Before the rocket");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.getByText('Done: Foreman took the snapshot "Before the rocket".')).toBeVisible();
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText("Before the rocket");
  await expect(rows.first()).toContainText("Manual");
  // Three columns, the reason above the label (DESIGN 8.3).
  await expect(page.locator("thead th")).toHaveText(["Snapshot", "Taken", "Size"]);
  await expect(rows.first().locator("td").first()).toContainText(/^Manual\s*Before the rocket/);
  await expect(rows.first().locator("time")).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC$/);
  await checkPage(page);

  // Play on: the save is replaced and a new one appears.
  const save = join(dir, "saves", "base.zip");
  rmSync(save);
  writeFileSync(save, "played on");
  writeFileSync(join(dir, "saves", "later.zip"), "later");

  await rows.first().getByRole("link", { name: /Restore/ }).click();
  await expect(page.getByRole("heading", { name: /Restore the snapshot "Before the rocket"/ })).toBeVisible();
  await expect(page.getByText(/labelled "Before restore"\. If the snapshot fails, nothing else happens/)).toBeVisible();
  await checkPage(page);
  await page.getByRole("button", { name: "Restore snapshot" }).click();

  await expect(page).toHaveURL(/\/worlds\/family-base\/snapshots\?undo=/);
  await expect(page.getByText(/The world is restored\. Foreman took the snapshot "Before restore" first/)).toBeVisible();
  await expect(rows).toHaveCount(2);
  await expect(rows.first().locator("td").first()).toHaveText(/^Before restore\s*Restore/);
  expect(readFileSync(save, "utf8")).toBe("the first save");
  expect(statSync(save).mtime.getTime()).toBe(savedAt.getTime());
  expect(() => statSync(join(dir, "saves", "later.zip"))).toThrow();
  await checkPage(page);
});

// The reason shows above the label only when it adds words the label lacks.
test("a snapshot's reason is not repeated by its label", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "family-base");
  await page.goto("/worlds/family-base/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  const cell = page.locator("tbody tr").first().locator("td").first();
  await expect(cell).toHaveText(/^Manual snapshot\s*Restore/);
  await page.getByLabel("Label (optional)").fill("Before the rocket");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.locator("tbody tr").first().locator("td").first()).toHaveText(/^Manual\s*Before the rocket\s*Restore/);
});

test("cancel leaves the world as it is", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedWorld(foreman, "family-base");
  await page.goto("/worlds/family-base/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  writeFileSync(join(dir, "saves", "later.zip"), "later");
  await page.locator("tbody tr").first().getByRole("link", { name: /Restore/ }).click();
  await page.getByRole("link", { name: "Cancel, keep the world as it is" }).click();
  await expect(page).toHaveURL("/worlds/family-base/snapshots");
  expect(readFileSync(join(dir, "saves", "later.zip"), "utf8")).toBe("later");
});

test("an over-long label is refused with a field error", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "family-base");
  await page.goto("/worlds/family-base/snapshots");
  await page.getByLabel("Label (optional)").evaluate((el) => el.removeAttribute("maxlength"));
  await page.getByLabel("Label (optional)").fill("x".repeat(101));
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.locator("#label-error")).toHaveText("Error: Use at most 100 characters");
  await expect(page.locator("tbody tr")).toHaveCount(0);
  await checkPage(page);
});

// "Go to the snapshot" lands on that row's Restore link, and Restore links
// tell apart snapshots taken in the same minute by their seconds.
test("the done message leads to the snapshot's Restore link", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "family-base");
  await page.goto("/worlds/family-base/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await page.waitForTimeout(1100);
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await page.getByRole("link", { name: "Go to the snapshot" }).click();
  await expect(page).toHaveURL(/#restore-\d{8}T\d{6}Z$/);
  const target = page.locator(":target");
  await expect(target).toHaveText(/^Restore/);
  await expect(target).toHaveAttribute("href", /\/restore$/);
  const names = await page.getByRole("link", { name: /^Restore Manual snapshot, taken/ }).evaluateAll((els) =>
    els.map((el) => el.textContent),
  );
  expect(names).toHaveLength(2);
  expect(names[0]).toMatch(/\d{2}:\d{2}:\d{2} UTC$/);
  expect(new Set(names).size).toBe(2);
  await checkPage(page);

  await target.click();
  await expect(page.getByRole("link", { name: "Back to Family base" })).toHaveAttribute("href", "/worlds/family-base");
  await checkPage(page);
});
