import { test, expect, checkPage, createAdmin, seedWorld, image21, live, stateBadge } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readdirSync, readFileSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// The autosave tidy after the slot count goes down (SPEC 11.6): the
// settings preview says what the next start will do, and the start does it
// after a snapshot under its own label.

test.use({ fakeGameOn: true });

const worldDir = (dataDir: string) => join(dataDir, "worlds", "base");

// The world "base" with 20 slots and autosaves 1 to 20, 20 the newest.
async function setUp(page: Page, f: Parameters<typeof seedWorld>[0]) {
  await createAdmin(page);
  seedWorld(f, { gamePassword: "tidy-secret" });
  const dir = worldDir(f.dataDir);
  const file = join(dir, "config", "server-settings.json");
  writeFileSync(file, JSON.stringify({ ...JSON.parse(readFileSync(file, "utf8")), autosave_slots: 20, autosave_interval: 10 }));
  const save = readFileSync(join(dir, "saves", "base.zip"));
  const start = Date.now() - 30 * 60_000;
  for (let n = 1; n <= 20; n++) {
    const p = join(dir, "saves", `_autosave${n}.zip`);
    writeFileSync(p, save);
    const at = new Date(start + n * 60_000);
    utimesSync(p, at, at);
  }
  await page.goto("/worlds/base/settings");
}

test("lowering the slots says what the next start keeps and removes, and the start does it", async ({ page, foreman, fakeGame }) => {
  await fakeGame!.call("/image", { ref: image21, on: true });
  await setUp(page, foreman);
  await page.getByLabel("Autosave slots").fill("10");
  await page.getByRole("button", { name: "Preview changes" }).click();

  const note = page.locator("#autosave-tidy");
  await expect(note).toHaveText(
    "Note: At the next start, Foreman keeps the newest 10 autosaves. It removes 10 other autosaves. A snapshot is taken first.",
  );
  await checkPage(page);
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL(/\/worlds\/base\/settings\?saved=1/);

  await page.goto("/");
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);

  const autosaves = readdirSync(join(worldDir(foreman.dataDir), "saves"))
    .filter((n) => n.startsWith("_autosave"))
    .sort();
  expect(autosaves).toEqual(Array.from({ length: 10 }, (_, i) => `_autosave${i + 1}.zip`).sort());

  await page.goto("/worlds/base/snapshots");
  await expect(page.getByText("Before autosave tidy").first()).toBeVisible();
  await checkPage(page);
});

test("raising the slots shows no tidy note", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.getByLabel("Autosave slots").fill("25");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Save these changes to Base?");
  await expect(page.locator("#autosave-tidy")).toHaveCount(0);
});
