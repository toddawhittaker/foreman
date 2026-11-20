import { test, expect, checkPage, createAdmin, seedWorld } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// Game settings' sub-pages share one draft (DESIGN 6): an edit on one
// sub-page is kept on the next, counted in the foot bar and marked on its
// link, so one Preview changes covers them all. A typed password is never
// kept in it (SPEC 11.4).

const secret = "hunter2-secret";
const typed = "newpass-typed";
const read = (dir: string) => readFileSync(join(dir, "worlds", "base", "config", "server-settings.json"), "utf8");

async function setUp(page: Page, f: Parameters<typeof seedWorld>[0]) {
  await createAdmin(page);
  seedWorld(f, { gamePassword: secret });
}

const worldPages = (page: Page) => page.getByRole("navigation", { name: "World pages" });

test("edits on one sub-page are counted, marked and kept on the next, and Cancel drops them", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  const note = page.locator("#settings-unsaved");
  await expect(note).toHaveText("");
  await expect(note).toHaveAttribute("role", "status");
  await expect(worldPages(page).getByRole("link", { name: "Listing (unsaved changes)" })).toHaveCount(0);

  await page.getByLabel("Description").fill("Bring snacks");
  await expect(note).toHaveText("1 unsaved change, kept while you move between sections.");
  await expect(worldPages(page).getByRole("link", { name: "Listing (unsaved changes)" })).toHaveAttribute("data-unsaved", "");
  await checkPage(page);

  await worldPages(page).getByRole("link", { name: "Players and access" }).click();
  await expect(page).toHaveURL(/section=players/);
  // The edit on Listing is still counted and marked here.
  await expect(note).toHaveText("1 unsaved change, kept while you move between sections.");
  await expect(worldPages(page).getByRole("link", { name: "Listing (unsaved changes)" })).toHaveAttribute("data-unsaved", "");
  const players = worldPages(page).getByRole("link", { name: "Players and access", exact: true });
  await expect(players).not.toHaveAttribute("data-unsaved");

  await page.getByLabel("Maximum players").fill("8");
  await expect(note).toHaveText("2 unsaved changes, kept while you move between sections.");
  await expect(worldPages(page).getByRole("link", { name: "Players and access (unsaved changes)" })).toBeVisible();

  // A typed password is not part of the draft: not counted, not stored, and
  // gone after moving to another sub-page and back.
  await page.getByLabel("New password").fill(typed);
  await expect(note).toHaveText("2 unsaved changes, kept while you move between sections.");
  expect(await page.evaluate(() => JSON.stringify({ ...sessionStorage }))).not.toContain(typed);
  await worldPages(page).getByRole("link", { name: "Gameplay" }).click();
  await worldPages(page).getByRole("link", { name: /^Players and access/ }).click();
  await expect(page.getByLabel("New password")).toHaveValue("");
  await expect(page.getByLabel("Maximum players")).toHaveValue("8");
  await checkPage(page);

  // Cancel leaves without saving and drops the draft.
  await page.getByRole("link", { name: "Cancel" }).click();
  await page.goto("/worlds/base/settings");
  await expect(note).toHaveText("");
  await expect(page.getByLabel("Description")).toHaveValue("");
  expect(read(foreman.dataDir)).not.toContain("Bring snacks");
});

test("an edit made after Go back and edit is kept across sub-pages and previewed", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings?section=players");
  await page.getByLabel("Maximum players").fill("5");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await page.getByRole("link", { name: "Go back and edit" }).click();
  await expect(page.getByLabel("Maximum players")).toHaveValue("5");

  await page.getByLabel("Maximum players").fill("6");
  await worldPages(page).getByRole("link", { name: "Listing", exact: true }).click();
  await expect(page).toHaveURL(/section=listing&edit=/);
  await page.getByRole("button", { name: "Preview changes" }).click();
  const row = page.locator("ul.changes > li").filter({ hasText: "max_players" });
  await expect(row.locator("ins")).toHaveText("6");
  await checkPage(page);
});
