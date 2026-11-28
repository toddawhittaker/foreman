import { test, expect, checkPage, createAdmin, pickLanguage, seedWorld, type Foreman } from "./foreman.js";
import { mkdirSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 13.1, 6.6, 7.6, 14.6, 12.9: the General tab holds the settings that
// have no environment form. Every project runs these, so each holds at 320 px
// and at desktop width, in both themes.

const settingsNav = (page: import("@playwright/test").Page) => page.getByRole("navigation", { name: "Foreman settings" });

const retention = (page: import("@playwright/test").Page) => page.getByLabel("Automatic snapshots to keep per world");
const countdown = (page: import("@playwright/test").Page) => page.getByLabel("Default stop countdown (seconds)");
const auditDays = (page: import("@playwright/test").Page) => page.getByLabel("Keep audit entries for (days)");
const saveLimit = (page: import("@playwright/test").Page) => page.getByLabel("Largest save upload (MB)");
const modLimit = (page: import("@playwright/test").Page) => page.getByLabel("Largest mod upload (MB)");

// Writes n automatic snapshots into the world's snapshot folder, as Foreman
// would have taken them, a minute apart.
function seedAutomaticSnapshots(f: Foreman, n: number) {
  const dir = join(f.dataDir, "snapshots", "base");
  for (let i = 0; i < n; i++) {
    const at = new Date(Date.UTC(2026, 10, 1, 10, i));
    const name = at.toISOString().replace(/[-:]/g, "").replace(/\.\d+Z$/, "Z");
    mkdirSync(join(dir, name), { recursive: true });
    writeFileSync(
      join(dir, name, "snapshot.json"),
      JSON.stringify({ label: "Before switch", reason: "switch", taken_at: at.toISOString(), taken_by: "admin", files: [] }),
    );
  }
}

test("the General tab shows each setting with its default and says where it is set", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/restart-schedule");
  await settingsNav(page).getByRole("link", { name: "General" }).click();
  await expect(page).toHaveURL("/settings/general");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("General");
  await expect(settingsNav(page).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "page");
  await expect(retention(page)).toHaveValue("10");
  await expect(countdown(page)).toHaveValue("60");
  await expect(auditDays(page)).toHaveValue("365");
  await expect(saveLimit(page)).toHaveValue("1024");
  await expect(modLimit(page)).toHaveValue("1024");
  await expect(page.getByText("Set here. No environment variable sets it.")).toHaveCount(1);
  await expect(page.locator("#general-trim")).toHaveCount(0);
  await checkPage(page);
});

test("a save stores every value and the page shows them again", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/general");
  await retention(page).fill("5");
  await countdown(page).fill("0");
  await auditDays(page).fill("30");
  await saveLimit(page).fill("2048");
  await modLimit(page).fill("300");
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
  await expect(page.locator("#toasts .toast--success")).toContainText("Saved. The new settings apply from now on.");
  await checkPage(page);
  await page.goto("/settings/general");
  await expect(retention(page)).toHaveValue("5");
  await expect(countdown(page)).toHaveValue("0");
  await expect(auditDays(page)).toHaveValue("30");
  await expect(saveLimit(page)).toHaveValue("2048");
  await expect(modLimit(page)).toHaveValue("300");
});

test("a value outside its range is refused with its error, and nothing is saved", { tag: "@looks" }, async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/general");
  await retention(page).fill("0");
  await modLimit(page).fill("5000");
  await page.getByRole("button", { name: "Save settings" }).click();
  const summary = page.locator("#error-summary");
  await expect(summary).toBeFocused();
  await expect(summary.getByRole("link")).toHaveText(["Enter a whole number from 1 to 100.", "Enter a whole number from 10 to 2,048."]);
  await expect(retention(page)).toHaveAttribute("aria-invalid", "true");
  await expect(retention(page)).toHaveValue("0");
  await expect(modLimit(page)).toHaveValue("5000");
  await checkPage(page);
  // The summary's link takes focus to the field.
  await summary.getByRole("link").first().click();
  await expect(retention(page)).toBeFocused();

  await page.goto("/settings/general");
  await expect(retention(page)).toHaveValue("10");
  await expect(modLimit(page)).toHaveValue("1024");
});

const snapshotFolders = (f: Foreman) => readdirSync(join(f.dataDir, "snapshots", "base")).filter((n) => /^\d{8}T/.test(n));

// SPEC 17.5, 6.6: lowering retention below what snapshots exist asks first;
// Cancel saves nothing; confirming saves, and deletes nothing yet.
test("lowering retention that trims asks first, and Cancel saves nothing", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedAutomaticSnapshots(foreman, 4);
  await page.goto("/settings/general");
  await expect(page.locator("#general-trim")).toHaveCount(0);

  await retention(page).fill("1");
  await modLimit(page).fill("300");
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Keep fewer automatic snapshots?");
  // The confirm page is under General, not General itself.
  await expect(settingsNav(page).getByRole("link", { name: "General" })).toHaveAttribute("aria-current", "true");
  const box = page.locator("section.confirm");
  await expect(box).toContainText("Removed snapshots cannot be restored. To keep them, raise the number again before the next automatic snapshot.");
  await expect(box.locator("#general-trim")).toContainText("3 automatic snapshots will be removed, each at its world's next automatic snapshot.");
  await expect(box.locator("#general-trim")).toContainText("Worlds affected: 1.");
  await expect(box).toContainText("Automatic snapshots to keep per world: 10 to 1");
  await expect(box).toContainText("Largest mod upload (MB): 1,024 to 300");
  await checkPage(page);

  await box.getByRole("link", { name: "Cancel, keep the current settings" }).click();
  await expect(page).toHaveURL("/settings/general");
  await expect(retention(page)).toHaveValue("10");
  await expect(modLimit(page)).toHaveValue("1024");
  expect(snapshotFolders(foreman)).toHaveLength(4);
});

test("confirming saves every value and deletes nothing yet", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedAutomaticSnapshots(foreman, 4);
  await page.goto("/settings/general");
  await retention(page).fill("1");
  await modLimit(page).fill("300");
  await page.getByRole("button", { name: "Save settings" }).click();
  await page.locator("section.confirm").getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
  // The count sums up the save, so it is in the Saved toast.
  const trim = page.locator("#toasts .toast--success");
  await expect(trim).toContainText("3 automatic snapshots will be removed");
  await expect(retention(page)).toHaveValue("1");
  await expect(modLimit(page)).toHaveValue("300");
  await checkPage(page);
  expect(snapshotFolders(foreman)).toHaveLength(4);

  // Raising it again leaves nothing to remove, and saves in one click.
  await retention(page).fill("10");
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator("#toasts .toast--success")).toBeVisible();
  await expect(page.locator("#general-trim")).toHaveCount(0);
});

test("lowering retention with nothing to remove saves at once", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedAutomaticSnapshots(foreman, 2);
  await page.goto("/settings/general");
  await retention(page).fill("2");
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
  await expect(page.locator("#general-trim")).toHaveCount(0);
  await expect(retention(page)).toHaveValue("2");
});

test("the page is translated", async ({ page }) => {
  await createAdmin(page);
  await pickLanguage(page, "de");
  await page.goto("/settings/general");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Allgemein");
  await expect(page.getByRole("button", { name: "Einstellungen speichern" })).toBeVisible();
  await expect(page.getByLabel("Standard-Countdown beim Stoppen (Sekunden)")).toHaveValue("60");
});

test("the page needs a signed-in user", async ({ page }) => {
  const res = await page.request.get("/settings/general", { maxRedirects: 0 });
  expect(res.status()).not.toBe(200);
});
