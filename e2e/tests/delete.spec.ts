import { test, expect, checkPage, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

const password = "correct horse battery";

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/worlds");
}

const savedAt = new Date("2026-11-03T14:25:00.123Z");

// A world is a folder a person can make by hand (SPEC 5.1).
function seedWorld(f: Foreman, id: string, name: string): string {
  const dir = join(f.dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(join(dir, "world.json"), JSON.stringify({ format_version: 1, name, game_version: "2.0.72", space_age: true, created_at: "2026-11-01T10:00:00Z" }));
  const save = join(dir, "saves", "base.zip");
  writeFileSync(save, `the save of ${name}`);
  utimesSync(save, savedAt, savedAt);
  return dir;
}

function setActive(f: Foreman, id: string) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    db.prepare("INSERT INTO active_worlds (world_id, since) VALUES (?, ?)").run(id, 0);
  } finally {
    db.close();
  }
}

// Two worlds, the first active, so the second can be deleted.
function seedPair(f: Foreman): string {
  seedWorld(f, "base", "Base");
  setActive(f, "base");
  return seedWorld(f, "family-base", "Family base");
}

test("delete with a final snapshot, then restore the world", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedPair(foreman);

  await page.goto("/worlds/family-base/delete");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete Family base");
  await expect(page.getByRole("heading", { name: 'Delete the world "Family base"?' })).toBeVisible();
  await expect(page.getByLabel(/Take a final snapshot first/)).toBeChecked();
  await expect(page.getByRole("button", { name: "Delete world" })).toHaveClass(/button--danger/);
  await expect(page.getByRole("link", { name: "Cancel, keep the world" })).toHaveAttribute("href", "/worlds/family-base");
  await checkPage(page);

  await page.getByLabel("Type the world's name to confirm").fill("Family base");
  await page.getByRole("button", { name: "Delete world" }).click();

  await expect(page).toHaveURL("/deleted?deleted=family-base");
  await expect(page.getByText('The world "Family base" is deleted.')).toBeVisible();
  expect(existsSync(dir)).toBe(false);
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText("Before delete");
  await expect(rows.first().locator("time")).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC$/);
  await checkPage(page);

  // The world list no longer shows it.
  await page.goto("/worlds");
  await expect(page.getByRole("link", { name: "Family base" })).toHaveCount(0);

  await page.goto("/deleted");
  await page.getByRole("link", { name: /^Restore/ }).click();
  await expect(page.getByRole("heading", { name: /Restore the snapshot "Before delete"/ })).toBeVisible();
  await checkPage(page);
  await page.getByRole("button", { name: "Restore snapshot" }).click();

  await expect(page).toHaveURL(/\/worlds\/family-base\/snapshots\?restored=/);
  await expect(page.getByText(/The world is restored from the snapshot "Before delete"/)).toBeVisible();
  const save = join(dir, "saves", "base.zip");
  expect(readFileSync(save, "utf8")).toBe("the save of Family base");
  expect(statSync(save).mtime.getTime()).toBe(savedAt.getTime());
  // The snapshot it came from is kept, and no "Before restore" was taken.
  await expect(page.locator("tbody tr")).toHaveCount(1);
  await checkPage(page);

  // Restoring did not displace the active world.
  await page.goto("/worlds");
  await expect(page.getByRole("link", { name: "Family base" })).toBeVisible();
  await expect(page.locator("li", { hasText: "Base" }).first().locator(".badge--active")).toBeVisible();
  await page.goto("/deleted");
  await expect(page.getByText(/No deleted worlds/)).toBeVisible();
  await checkPage(page);
});

test("delete without a final snapshot keeps no snapshot", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedPair(foreman);

  await page.goto("/worlds/family-base/delete");
  await page.getByLabel(/Take a final snapshot first/).uncheck();
  await page.getByLabel("Type the world's name to confirm").fill("Family base");
  await page.getByRole("button", { name: "Delete world" }).click();

  await expect(page).toHaveURL("/deleted?deleted=family-base");
  await expect(page.getByText(/It had no snapshots, so it cannot be restored/)).toBeVisible();
  await expect(page.locator("tbody tr")).toHaveCount(0);
  expect(existsSync(dir)).toBe(false);
  expect(readdirSync(join(foreman.dataDir, "snapshots", "family-base"))).toEqual([]);
  await checkPage(page);
});

test("a wrong name is refused and the world stays", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedPair(foreman);

  await page.goto("/worlds/family-base/delete");
  await page.getByLabel("Type the world's name to confirm").fill("family base");
  await page.getByRole("button", { name: "Delete world" }).click();

  await expect(page.locator("#error-summary")).toContainText("Type Family base exactly, or cancel");
  await expect(page.locator("#confirm-name-error")).toHaveText("Error: Type Family base exactly, or cancel");
  await expect(page).toHaveTitle(/^Error: /);
  await expect(page.getByLabel("Type the world's name to confirm")).toHaveValue("family base");
  expect(existsSync(join(dir, "saves", "base.zip"))).toBe(true);
  await checkPage(page);

  await page.getByRole("link", { name: "Cancel, keep the world" }).click();
  expect(existsSync(join(dir, "saves", "base.zip"))).toBe(true);
});

test("the active world cannot be deleted", async ({ page, foreman }) => {
  await createAdmin(page);
  seedPair(foreman);

  await page.goto("/worlds/base/delete");
  await page.getByLabel("Type the world's name to confirm").fill("Base");
  await page.getByRole("button", { name: "Delete world" }).click();

  await expect(page.locator("#action-error")).toContainText("the active world cannot be deleted");
  expect(existsSync(join(foreman.dataDir, "worlds", "base", "saves", "base.zip"))).toBe(true);
  expect(existsSync(join(foreman.dataDir, "snapshots", "base"))).toBe(false);
  await checkPage(page);
});

test("a deleted world's name taken by another world blocks the restore", async ({ page, foreman }) => {
  await createAdmin(page);
  seedPair(foreman);
  await page.goto("/worlds/family-base/delete");
  await page.getByLabel("Type the world's name to confirm").fill("Family base");
  await page.getByRole("button", { name: "Delete world" }).click();
  await expect(page).toHaveURL("/deleted?deleted=family-base");

  seedWorld(foreman, "newer", "family BASE");
  await page.getByRole("link", { name: /^Restore/ }).click();
  await page.getByRole("button", { name: "Restore snapshot" }).click();
  await expect(page.locator("#action-error")).toContainText("another world now has this world's name");
  expect(existsSync(join(foreman.dataDir, "worlds", "family-base"))).toBe(false);
  await checkPage(page);
});
