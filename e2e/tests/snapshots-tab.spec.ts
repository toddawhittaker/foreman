import { test, expect, checkPage, createAdmin, seedWorld } from "./foreman.js";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// Taking a snapshot asks Docker whether the game is running (SPEC 6.10).
test.use({ fakeGameOn: true });

// SPEC 6.12: the Snapshots tab lists every snapshot with its disk use, and
// deleting one goes through a confirmation that removes only that snapshot.
test("Snapshots tab lists and deletes a snapshot", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  const save = join(foreman.dataDir, "worlds", "base", "saves", "base.zip");
  const saveBefore = readFileSync(save);

  for (const label of ["Keep me", "Delete me"]) {
    await page.goto("/worlds/base/snapshots");
    await page.getByLabel("Label (optional)").fill(label);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(page.getByText(`Done: Foreman took the snapshot "${label}".`)).toBeVisible();
  }

  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Snapshots" }).click();
  await expect(page).toHaveURL("/snapshots");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Snapshots");
  await expect(page.getByRole("link", { name: "Snapshots", exact: true })).toHaveAttribute("aria-current", "page");
  await expect(page.getByText("Snapshots add", { exact: true })).toBeVisible();
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(2);
  await expect(page.getByRole("heading", { level: 3 })).toHaveText("Base");
  await expect(rows.first()).toContainText("by admin");
  await expect(page.locator("thead th")).toHaveText(["Snapshot", "Taken", "Size"]);
  await checkPage(page);

  await page.getByRole("link", { name: /^Delete Delete me of Base/ }).click();
  await expect(page.getByRole("heading", { name: /Delete the snapshot "Delete me" of Base from/ })).toBeVisible();
  await expect(page.getByText(/^Free .* of disk space/)).toBeVisible();
  await checkPage(page);
  await page.getByRole("button", { name: "Delete snapshot" }).click();

  await expect(page).toHaveURL(/\/snapshots\?deleted=/);
  await expect(page.getByText(/Done: Foreman deleted the snapshot taken/)).toBeVisible();
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText("Keep me");
  await checkPage(page);

  // The world and the other snapshot's hard-linked save are untouched.
  expect(readFileSync(save).equals(saveBefore)).toBe(true);
  const snaps = join(foreman.dataDir, "snapshots", "base");
  const left = readdirSync(snaps).filter((n) => /^\d{8}T\d{6}Z/.test(n));
  expect(left).toHaveLength(1);
  expect(existsSync(join(snaps, left[0], "saves", "base.zip"))).toBe(true);
});

test("delete from a world's Snapshots page, and cancel", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(1);

  await rows.first().getByRole("link", { name: /^Delete/ }).click();
  await page.getByRole("link", { name: "Cancel, keep the snapshot" }).click();
  await expect(page).toHaveURL("/worlds/base/snapshots");
  await expect(rows).toHaveCount(1);

  await rows.first().getByRole("link", { name: /^Delete/ }).click();
  await page.getByRole("button", { name: "Delete snapshot" }).click();
  await expect(page).toHaveURL(/\/worlds\/base\/snapshots\?deleted=/);
  await expect(page.getByText(/Done: Foreman deleted the snapshot taken/)).toBeVisible();
  await expect(page.getByText(/No snapshots yet/)).toBeVisible();
  await checkPage(page);
});
