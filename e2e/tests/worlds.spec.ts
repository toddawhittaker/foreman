import { test, expect, checkPage, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { mkdirSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

const admin = "admin";
const password = "correct horse battery";

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill(admin);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/worlds");
}

// Worlds are folders a person can make by hand (SPEC 5.1), so the list is
// tested from folders written directly, not through Foreman.
function seedWorld(f: Foreman, id: string, meta: Record<string, unknown>, saveTime?: Date) {
  const dir = join(f.dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(join(dir, "world.json"), JSON.stringify({ format_version: 1, created_at: "2026-11-01T10:00:00Z", ...meta }));
  if (saveTime) {
    const save = join(dir, "saves", "base.zip");
    writeFileSync(save, Buffer.alloc(2_100_000));
    utimesSync(save, saveTime, saveTime);
  }
}

function setActive(f: Foreman, id: string) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    db.prepare("INSERT INTO active_worlds (world_id, since) VALUES (?, ?)").run(id, 0);
  } finally {
    db.close();
  }
}

test("/ goes to the world list", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/");
  await expect(page).toHaveURL("/worlds");
});

test("empty world list names the next step", async ({ page }) => {
  await createAdmin(page);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Worlds");
  await expect(page.getByText(/No worlds yet\. Import a folder of saves/)).toBeVisible();
  await checkPage(page);
});

// SPEC 5.4: name, game version, Space Age, last played, disk use, and which is active.
test("world list shows each world, the active one first", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "alpha", { name: "Alpha", game_version: "1.1.110", space_age: false });
  seedWorld(foreman, "zeta-base", { name: "Zeta base", game_version: "2.0.72", space_age: true }, new Date("2026-11-03T14:25:00Z"));
  seedWorld(foreman, "future", { name: "Future", game_version: "3.0.0", space_age: false, format_version: 9 });
  mkdirSync(join(foreman.dataDir, "worlds", ".staging-half-made"), { recursive: true });
  setActive(foreman, "zeta-base");

  await page.goto("/worlds");
  const items = page.locator("ul.worlds > li");
  await expect(items).toHaveCount(3);

  const first = items.nth(0);
  await expect(first.getByRole("heading")).toHaveText("Zeta base Active");
  await expect(first.locator(".badge--active")).toHaveText("Active");
  await expect(first).toContainText("2.0.72");
  await expect(first.locator("dd").nth(1)).toHaveText("On");
  await expect(first.locator("time")).toHaveAttribute("datetime", "2026-11-03T14:25:00Z");
  await expect(first.locator("time")).toHaveText("2026-11-03 14:25 UTC");
  await expect(first).toContainText("2.1 MB");

  await expect(items.nth(1).getByRole("heading")).toHaveText("Alpha");
  await expect(items.nth(1)).toContainText("Never");
  await expect(items.nth(2)).toContainText("A newer version of Foreman wrote this world");
  await expect(page.locator(".badge--active")).toHaveCount(1);
  await expect(page.getByText("No worlds yet")).toHaveCount(0);
  await checkPage(page);
});
