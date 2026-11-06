import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Game settings (SPEC 11, DESIGN 8.10): the form, the JSON editor, the diff
// and the confirmation. The fake game stands in for Docker, so a stopped
// world is saved by Foreman alone.

test.use({ fakeGameOn: true });

const secret = "hunter2-secret";
const typed = "newpass-typed";
const live = { timeout: 20_000 };

const settingsFile = (dir: string) => join(dir, "worlds", "base", "config", "server-settings.json");
const read = (dir: string) => readFileSync(settingsFile(dir), "utf8");

// The world "base", with a game password, on the Server page with the game
// stopped.
async function setUp(page: Page, f: Parameters<typeof seedWorld>[0], extra: Record<string, unknown> = {}) {
  await createAdmin(page);
  seedWorld(f, { gamePassword: secret });
  if (Object.keys(extra).length > 0) {
    const file = settingsFile(f.dataDir);
    writeFileSync(file, JSON.stringify({ ...JSON.parse(readFileSync(file, "utf8")), ...extra }, null, 2));
  }
  await page.goto("/");
  await expect(page.getByRole("region", { name: "Status" }).locator(".state")).toHaveText("Stopped", live);
}

// Neither the page text nor its markup holds the password (SPEC 11.4).
async function expectNoSecret(page: Page) {
  const html = await page.content();
  for (const s of [secret, typed]) expect(html, `the page shows ${s}`).not.toContain(s);
}

test("change a setting from the Server page in three clicks, and the password never shows", async ({ page, foreman }) => {
  await setUp(page, foreman);
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.getByRole("link", { name: "Settings" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Settings of Base");
  await expect(page.getByText("A password is set.")).toBeVisible();
  await expect(page.getByLabel("New password")).toHaveValue("");
  await expectNoSecret(page);
  await checkPage(page);

  await page.getByLabel("Maximum players").fill("8");
  await page.getByLabel("Description").fill("Come play\nBring snacks");
  await page.getByRole("button", { name: "Preview changes" }).click();

  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Save these changes to Base?");
  const rows = page.locator("ul.changes > li");
  await expect(rows).toHaveCount(2);
  await expect(rows.filter({ hasText: "max_players" })).toContainText("Added");
  await expect(rows.filter({ hasText: "max_players" }).locator("ins")).toHaveText("8");
  await expect(rows.filter({ hasText: "description" }).locator("ins")).toHaveText('"Come play\\nBring snacks"');
  await expect(page.getByText("Take an automatic snapshot of Base")).toBeVisible();
  await expect(page.getByText("To undo, restore that snapshot from the Snapshots page.")).toBeVisible();
  expect(read(foreman.dataDir)).not.toContain("max_players");
  await expectNoSecret(page);
  await checkPage(page);

  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL(/\/worlds\/base\/settings\?saved=1/);
  const done = page.locator(".message--success");
  await expect(done).toContainText('Settings saved. Foreman took the snapshot "Before settings change" first.');
  await expect(done.getByRole("link", { name: "Go to the snapshot" })).toBeVisible();
  await expect(page.getByLabel("Maximum players")).toHaveValue("8");
  await expectNoSecret(page);
  await checkPage(page);
  await expectWithin(clicks, 3, "change a setting from the Server page");

  const saved = JSON.parse(read(foreman.dataDir));
  expect(saved).toMatchObject({ name: "Base", max_players: 8, description: "Come play\nBring snacks", game_password: secret });
  const snaps = readdirSync(join(foreman.dataDir, "snapshots", "base"));
  expect(snaps.length).toBe(1);
});

test("a new password is set without being shown, and the diff only says so", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("New password").fill(typed);
  await page.getByRole("button", { name: "Preview changes" }).click();
  const row = page.locator("ul.changes > li");
  await expect(row).toHaveCount(1);
  await expect(row).toContainText("Changed");
  await expect(row).toContainText("game_password");
  await expect(row).toContainText("The value is not shown. It will be changed.");
  await expectNoSecret(page);
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toBeVisible();
  await expectNoSecret(page);
  expect(JSON.parse(read(foreman.dataDir)).game_password).toBe(typed);

  // Removing it, and typing one at the same time is an error.
  await page.getByLabel("New password").fill("x");
  await page.getByLabel("Remove the password").check();
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("alert").or(page.locator(".message--error")).first()).toContainText("Fix this problem to continue");
  await expect(page.getByText("Enter a new password or remove it, not both.").first()).toBeVisible();
  await expect(page.getByLabel("New password")).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);
});

test("the form shows every error at once and keeps what was typed", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Server name").fill("");
  await page.getByLabel("Maximum players").fill("ten");
  await page.getByLabel("Admin names").fill("alice\nbad\u0001name");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.locator("#error-summary")).toContainText("Fix 3 problems to continue");
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.getByLabel("Maximum players")).toHaveValue("ten");
  await expect(page.getByLabel("Maximum players")).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);
  // A summary link goes to its field.
  await page.locator("#error-summary").getByRole("link", { name: /whole number/ }).click();
  await expect(page.getByLabel("Maximum players")).toBeFocused();
  expect(JSON.parse(read(foreman.dataDir)).max_players).toBeUndefined();
});

test("nothing changed offers no save", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Nothing changed in Base");
  await expect(page.getByText("Nothing changed. The settings already hold these values.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Save settings" })).toHaveCount(0);
  await checkPage(page);
  await page.getByRole("link", { name: "Back to the settings" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Settings of Base");
});

test("go back and edit keeps the edits", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Maximum players").fill("5");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await page.getByRole("link", { name: "Go back and edit" }).click();
  await expect(page.getByLabel("Maximum players")).toHaveValue("5");
  await page.getByLabel("Maximum players").fill("6");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.locator("ul.changes ins")).toHaveText("6");
});

test("admins are edited one per line, and the list is written while stopped", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Admin names").fill("Alice\nBob");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByText("Added admin Alice")).toBeVisible();
  await expect(page.getByText("Added admin Bob")).toBeVisible();
  await expect(page.locator("code", { hasText: "server-adminlist.json" })).toBeVisible();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toBeVisible();
  await expect(page.getByLabel("Admin names")).toHaveValue("Alice\nBob");
  expect(JSON.parse(readFileSync(join(foreman.dataDir, "worlds", "base", "config", "server-adminlist.json"), "utf8"))).toEqual(["Alice", "Bob"]);
});

test("the JSON editor refuses a bad edit with the line, then saves a good one and keeps the password", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByRole("link", { name: "Edit server-settings.json as JSON" }).click();
  const editor = page.getByLabel("Contents of server-settings.json");
  await expect(editor).toHaveAttribute("wrap", "off");
  await expect(editor).not.toContainText(secret);
  await expect(page.getByText("are not shown here")).toBeVisible();
  await expectNoSecret(page);
  await checkPage(page);

  const bad = '{\n  "name": "Base",\n  "max_players": "ten",\n  "auto_pause": 3\n}\n';
  await editor.fill(bad);
  await page.getByRole("button", { name: "Preview changes" }).click();
  const summary = page.locator("#error-summary");
  await expect(summary).toContainText("Fix 2 problems to continue");
  await expect(summary).toContainText('Line 3: max_players must be a whole number. The line reads: "max_players": "ten",');
  await expect(summary).toContainText("Line 4: auto_pause must be true or false.");
  await expect(editor).toHaveValue(bad);
  await expect(editor).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);
  expect(JSON.parse(read(foreman.dataDir)).max_players).toBeUndefined();

  await editor.fill('{\n  "name": "Base",\n  "max_players": 3,\n  "mystery_key": [1, 2]\n}\n');
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Save these changes to Base?");
  await expect(page.locator("ul.changes > li").filter({ hasText: "mystery_key" })).toContainText("Foreman does not check this key. It is saved as written.");
  await expect(page.locator("ul.changes").getByText("game_password")).toHaveCount(0);
  await checkPage(page);
  await page.getByRole("link", { name: "Go back and edit" }).click();
  await expect(page.getByLabel("Contents of server-settings.json")).toHaveValue(/"mystery_key": \[1, 2\]/);
  await page.getByRole("button", { name: "Preview changes" }).click();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toBeVisible();
  expect(JSON.parse(read(foreman.dataDir))).toEqual({ name: "Base", max_players: 3, mystery_key: [1, 2], game_password: secret });
});

test("a file changed after the preview shows the new changes with a warning", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Maximum players").fill("5");
  await page.getByRole("button", { name: "Preview changes" }).click();
  writeFileSync(settingsFile(foreman.dataDir), JSON.stringify({ name: "Base", game_password: secret, autosave_slots: 7 }));
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--warning")).toContainText("server-settings.json changed since you previewed. These are the changes against the file as it is now.");
  // The new diff is announced by focus, as the "nothing left" variant is (a11y: the title and heading are the same as before).
  await expect(page.locator("#file-changed")).toBeFocused();
  await expect(page.locator("ul.changes ins")).toHaveText("5");
  expect(JSON.parse(read(foreman.dataDir)).max_players).toBeUndefined();
  await checkPage(page);
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toBeVisible();
  // The edit that moved the file is kept.
  expect(JSON.parse(read(foreman.dataDir))).toMatchObject({ max_players: 5, autosave_slots: 7, game_password: secret });
});

test("a preview already saved cannot be saved again, and says so", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Maximum players").fill("5");
  await page.getByRole("button", { name: "Preview changes" }).click();
  const token = await page.locator('input[name="token"]').inputValue();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toBeVisible();

  const again = await page.request.post("/worlds/base/settings/confirm", { form: { token } });
  expect(again.status()).toBe(409);
  expect(await again.text()).toContain("This preview has expired, or its changes were already saved.");
  expect(readdirSync(join(foreman.dataDir, "snapshots", "base")).length).toBe(1);
});

test("a world a newer Foreman wrote is read-only", async ({ page, foreman }) => {
  await setUp(page, foreman);
  const meta = join(foreman.dataDir, "worlds", "base", "world.json");
  writeFileSync(meta, readFileSync(meta, "utf8").replace('"format_version":1', '"format_version":99'));
  await page.goto("/worlds/base/settings");
  await expect(page.getByText("A newer version of Foreman wrote this world, so this version cannot change it.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Preview changes" })).toHaveCount(0);
  await expect(page.getByLabel("Maximum players")).toBeDisabled();
  // The admin list is read-only, not disabled, so the keyboard can reach and scroll it.
  const admins = page.getByLabel("Admin names");
  await expect(admins).toHaveAttribute("readonly", "");
  await expect(admins).toBeEnabled();
  await expect(admins).not.toBeEditable();
  await admins.focus();
  await expect(admins).toBeFocused();
  await checkPage(page);
  await page.getByRole("link", { name: "Edit server-settings.json as JSON" }).click();
  await expect(page.getByLabel("Contents of server-settings.json")).toHaveAttribute("readonly", "");
  await expect(page.getByRole("button", { name: "Preview changes" })).toHaveCount(0);
  await checkPage(page);
});

test("the form works with the keyboard alone", async ({ page, foreman }) => {
  await setUp(page, foreman);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Maximum players").focus();
  await page.keyboard.type("4");
  const preview = page.getByRole("button", { name: "Preview changes" });
  for (let i = 0; i < 30 && !(await preview.evaluate((el) => el === document.activeElement)); i++) await page.keyboard.press("Tab");
  await expect(preview).toBeFocused();
  const ring = await preview.evaluate((el) => getComputedStyle(el).outlineStyle + " " + getComputedStyle(el).outlineWidth);
  expect(ring, "a visible focus ring").not.toMatch(/^none|0px$/);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Save these changes to Base?");
  const save = page.getByRole("button", { name: "Save settings" });
  for (let i = 0; i < 30 && !(await save.evaluate((el) => el === document.activeElement)); i++) await page.keyboard.press("Tab");
  await page.keyboard.press("Enter");
  await expect(page.locator(".message--success")).toBeVisible();
});

test("a long name and a long description do not break the layout", async ({ page, foreman }) => {
  await setUp(page, foreman, { description: "word ".repeat(200) });
  const long = "N".repeat(60);
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Server name").fill(long);
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.locator("ul.changes ins").first()).toContainText(long);
  await checkPage(page);
});

test.describe("a running world", () => {
  test("the form and the preview say changes need a restart, and saving offers Restart", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page.getByRole("region", { name: "Status" }).locator(".state")).toHaveText("Running", live);

    await page.getByRole("link", { name: "Settings" }).click();
    await expect(page.getByText("The server is running. Changes to these settings take effect after a restart. Changes to admins apply at once.")).toBeVisible();
    await checkPage(page);
    await page.getByLabel("Maximum players").fill("8");
    await page.getByLabel("Admin names").fill("Alice");
    await page.getByRole("button", { name: "Preview changes" }).click();
    await expect(page.getByText("The server is running. These changes take effect after a restart.")).toBeVisible();
    await expect(page.getByText("The server is running. Changes to admins apply at once.")).toBeVisible();
    await expect(page.getByText("Save the running game.")).toBeVisible();
    await expect(page.getByText("Promote and demote these players in the running game")).toBeVisible();
    await checkPage(page);
    expect(existsSync(join(foreman.dataDir, "snapshots", "base"))).toBe(false);

    // Saving saves the running game, takes the snapshot and sends the admin change to the game; the
    // message then offers Restart, so the next step is a control.
    await page.getByRole("button", { name: "Save settings" }).click();
    const done = page.locator(".message--success");
    await expect(done).toContainText("Settings saved. Foreman took the snapshot");
    await expect(done).toContainText("take effect after a restart");
    await expect(done.getByRole("link", { name: "Restart server" })).toHaveAttribute("href", "/worlds/base/restart");
    await checkPage(page);
    expect(JSON.parse(read(foreman.dataDir)).max_players).toBe(8);
    // One confirmation of two files takes one snapshot (SPEC 11.3).
    expect(readdirSync(join(foreman.dataDir, "snapshots", "base")).length).toBe(1);
    expect((await fakeGame!.state()).containers[0].commands).toContain("/promote Alice");
  });
});
