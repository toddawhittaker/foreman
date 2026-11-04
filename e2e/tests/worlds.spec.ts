import { test, expect, checkPage, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, mkdirSync, readdirSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// Taking a snapshot asks Docker whether the game is running first
// (SPEC 6.10), so these tests use the fake game as Docker.
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

// DESIGN 6: "/" is the Server page; the world list stays at /worlds.
test("/ is the Server page and the world list is at /worlds", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/");
  await expect(page).toHaveURL("/");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Server");
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Worlds" }).click();
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

// The smallest valid zip: an end-of-central-directory record and no entries.
const emptyZip = Buffer.concat([Buffer.from("PK\x05\x06"), Buffer.alloc(18)]);

// Every path under dir with its size and modification time, to prove an
// import left its source exactly as it was (SPEC 5.5).
function listing(dir: string): string[] {
  return readdirSync(dir, { recursive: true, encoding: "utf8" })
    .sort()
    .map((p) => {
      const s = statSync(join(dir, p));
      return `${p} ${s.size} ${s.mtimeMs}`;
    });
}

test("imports a folder from the inbox and leaves it unchanged", async ({ page, foreman }) => {
  await createAdmin(page);
  const src = join(foreman.dataDir, "import", "saves-2025");
  mkdirSync(join(src, "mods"), { recursive: true });
  const old = new Date("2025-06-01T12:00:00Z");
  for (const f of ["base.zip", "later.zip", "notes.txt", join("mods", "mod-list.json")]) {
    writeFileSync(join(src, f), f.endsWith(".zip") ? emptyZip : "{}");
    utimesSync(join(src, f), old, old);
  }
  const before = listing(src);

  await page.goto("/worlds");
  await page.getByRole("link", { name: "Import a world" }).click();
  await expect(page).toHaveURL("/import");
  await expect(page.getByLabel(/saves-2025: 2 saves and mods/)).toBeVisible();
  await checkPage(page);

  // A wrong version is refused with the error summary (DESIGN 7.2).
  const folderForm = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await folderForm.getByLabel(/saves-2025/).check();
  await folderForm.getByLabel("World name").fill("Old game");
  await folderForm.getByLabel("Game version").fill("2.0");
  await folderForm.getByRole("button", { name: "Import folder" }).click();
  await expect(page.locator("#error-summary")).toContainText("Enter the game version as three numbers");
  await expect(page).toHaveTitle(/^Error: /);
  await checkPage(page);

  await folderForm.getByLabel("Game version").fill("2.0.72");
  await folderForm.getByLabel("Space Age is on").check();
  await folderForm.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/old-game");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Old game Active");
  await expect(page.locator("dl.facts")).toContainText("2.0.72");
  await expect(page.locator("time")).toHaveAttribute("datetime", "2025-06-01T12:00:00Z");
  await checkPage(page);

  expect(listing(src)).toEqual(before);
  const world = join(foreman.dataDir, "worlds", "old-game");
  expect(readdirSync(join(world, "saves")).sort()).toEqual(["base.zip", "later.zip"]);
  expect(readdirSync(join(world, "mods"))).toEqual(["mod-list.json"]);
});

const fixtures = join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata");

function inboxFixture(f: Foreman, folder: string, fixture: string) {
  mkdirSync(join(f.dataDir, "import", folder), { recursive: true });
  copyFileSync(join(fixtures, fixture), join(f.dataDir, "import", folder, "a.zip"));
}

// SPEC 5.5: the version and Space Age come from the save.
test("a single inbox folder fills in the version and Space Age, and imports", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "only", "2.1.24-space-age.zip");
  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await expect(form.getByLabel(/only: 1 save.*made with Factorio 2.1.24, Space Age/)).toBeChecked();
  await expect(form.getByLabel("Game version")).toHaveValue("2.1.24");
  await expect(form.getByLabel("Space Age is on")).toBeChecked();
  await checkPage(page);
  await form.getByLabel("World name").fill("Prefilled");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/prefilled");
  await expect(page.locator("dl.facts")).toContainText("2.1.24");
});

test("choosing a folder fills the version, but not over typed text", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "one", "2.1.24-space-age.zip");
  inboxFixture(foreman, "two", "2.0.77-no-space-age.zip");
  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await expect(form.getByLabel("Game version")).toHaveValue("");
  await form.getByLabel(/one: /).check();
  await expect(form.getByLabel("Game version")).toHaveValue("2.1.24");
  await expect(form.getByLabel("Space Age is on")).toBeChecked();
  await expect(page.locator("#announcer")).toHaveText("Game version set to 2.1.24 from the save, with Space Age on.");
  await checkPage(page);
  await form.getByLabel(/two: /).check();
  await expect(form.getByLabel("Game version")).toHaveValue("2.0.77");
  await expect(form.getByLabel("Space Age is on")).not.toBeChecked();
  await expect(page.locator("#announcer")).toHaveText("Game version set to 2.0.77 from the save.");
  await form.getByLabel("Game version").fill("2.0.1");
  await form.getByLabel(/one: /).check();
  await expect(form.getByLabel("Game version")).toHaveValue("2.0.1");
});

// A folder whose save cannot be read clears what the script filled in, so
// the previous folder's version is never imported by mistake.
test("choosing an unreadable folder after a readable one clears the filled fields", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "good", "2.1.24-space-age.zip");
  mkdirSync(join(foreman.dataDir, "import", "junk"), { recursive: true });
  writeFileSync(join(foreman.dataDir, "import", "junk", "a.zip"), "not a zip");
  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await form.getByLabel(/good: /).check();
  await expect(form.getByLabel("Game version")).toHaveValue("2.1.24");
  await form.getByLabel(/junk: /).check();
  await expect(form.getByLabel("Game version")).toHaveValue("");
  await expect(form.getByLabel("Space Age is on")).not.toBeChecked();
  await expect(page.locator("#announcer")).toContainText("could not read this folder's save");
  await checkPage(page);
});

test("imports a world by uploading a save", async ({ page, foreman }) => {
  await createAdmin(page);
  await page.goto("/import");
  await expect(page.getByText("The import folder is empty")).toBeVisible();
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Upload a save" }) });
  await form.getByLabel("World name").fill("Uploaded");
  await form.getByLabel("Game version").fill("1.1.110");
  await form.getByLabel("Save file").setInputFiles({ name: "my save.zip", mimeType: "application/zip", buffer: emptyZip });
  await form.getByRole("button", { name: "Upload and import" }).click();
  await expect(page).toHaveURL("/worlds/uploaded");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Uploaded Active");
  expect(readdirSync(join(foreman.dataDir, "worlds", "uploaded", "saves"))).toEqual(["my save.zip"]);

  // A file that is not a zip is refused and no world is made.
  await page.goto("/import");
  await form.getByLabel("World name").fill("Second");
  await form.getByLabel("Game version").fill("1.1.110");
  await form.getByLabel("Save file").setInputFiles({ name: "fake.zip", mimeType: "application/zip", buffer: Buffer.from("nope") });
  await form.getByRole("button", { name: "Upload and import" }).click();
  await expect(page.locator("#error-summary")).toContainText("This file is not a zip file.");
  await checkPage(page);
  expect(readdirSync(join(foreman.dataDir, "worlds"))).toEqual(["uploaded"]);
});

// SPEC 5.2, 5.8: the display name changes, the id does not, and two
// worlds may not share a name ignoring case.
test("renames a world and refuses a duplicate name", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "base", { name: "Base", game_version: "2.0.72", space_age: false });
  seedWorld(foreman, "other", { name: "Other", game_version: "2.0.72", space_age: false });

  await page.goto("/worlds");
  await page.getByRole("link", { name: "Base" }).click();
  await expect(page).toHaveURL("/worlds/base");
  await expect(page.getByRole("link", { name: "Saves" })).toHaveAttribute("href", "/worlds/base/saves");
  await expect(page.getByRole("link", { name: "Snapshots" })).toHaveAttribute("href", "/worlds/base/snapshots");
  await checkPage(page);

  await page.getByLabel("Name", { exact: true }).fill("OTHER");
  await page.getByRole("button", { name: "Rename world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Another world already has this name");
  await expect(page.getByLabel("Name", { exact: true })).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);

  await page.getByLabel("Name", { exact: true }).fill("Main base");
  await page.getByRole("button", { name: "Rename world" }).click();
  await expect(page).toHaveURL("/worlds/base");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Main base");
});

test("a world a newer Foreman wrote cannot be renamed", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "future", { name: "Future", game_version: "3.0.0", space_age: false, format_version: 9 });
  await page.goto("/worlds/future");
  await expect(page.getByText("A newer version of Foreman wrote this world")).toBeVisible();
  await expect(page.getByRole("button", { name: "Rename world" })).toHaveCount(0);
  await checkPage(page);
});

// DESIGN 6, "The 320 px rules": a long name wraps; checkPage fails on sideways scroll at 320 px.
test("a very long world name wraps on the list and the world page", async ({ page, foreman }) => {
  await createAdmin(page);
  const long = "Family-base-".repeat(8) + "x".repeat(40);
  seedWorld(foreman, "long", { name: long, game_version: "2.0.72", space_age: true });
  await page.goto("/worlds");
  await expect(page.getByRole("link", { name: long })).toBeVisible();
  await checkPage(page);
  await page.goto("/worlds/long");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(long);
  await checkPage(page);
});

// DESIGN 6, Shell: the current tab is orange, a hovered tab is grey.
test("the Worlds tab marks the current page and hovers grey", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "alpha", { name: "Alpha", game_version: "2.0.72", space_age: false });
  const tab = page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Worlds" });
  await page.goto("/worlds/alpha/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  const restore = await page.getByRole("link", { name: /^Restore/ }).getAttribute("href");
  // Every page about worlds marks the tab (DESIGN 7.3).
  for (const path of ["/worlds", "/worlds/alpha", "/import", "/worlds/alpha/saves", "/worlds/alpha/snapshots",
    restore!, "/worlds/alpha/delete", "/deleted"]) {
    await page.goto(path);
    await expect(tab, path).toHaveAttribute("aria-current", "page");
  }
  const face = () => tab.evaluate((el) => getComputedStyle(el).backgroundColor);
  const current = await face();
  // Every signed-in page is about worlds, so drop the mark to see the idle tab.
  await tab.evaluate((el) => el.removeAttribute("aria-current"));
  const idle = await face();
  await tab.hover();
  const hover = await face();
  const button = await page.evaluate(() => {
    const probe = document.createElement("span");
    probe.style.background = "var(--color-button-hover)";
    document.body.append(probe);
    const c = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return c;
  });
  expect(hover).toBe(button);
  expect(hover).not.toBe(current);
  expect(idle).not.toBe(current);
});

// DESIGN 7.2: each radio of a group with an error points at the error text.
test("an import with no folder chosen describes the error on every radio", async ({ page, foreman }) => {
  await createAdmin(page);
  for (const name of ["one", "two"]) {
    mkdirSync(join(foreman.dataDir, "import", name), { recursive: true });
    writeFileSync(join(foreman.dataDir, "import", name, "a.zip"), "x");
  }
  await page.goto("/import");
  const folderForm = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await folderForm.getByLabel("World name").fill("Imported");
  await folderForm.getByLabel("Game version").fill("2.0.72");
  await folderForm.getByRole("button", { name: "Import folder" }).click();
  await expect(page.locator("#folder-source-error")).toBeVisible();
  const radios = folderForm.getByRole("radio");
  await expect(radios).toHaveCount(2);
  for (const radio of await radios.all()) {
    await expect(radio).toHaveAttribute("aria-invalid", "true");
    await expect(radio).toHaveAttribute("aria-describedby", "folder-source-error");
    await expect(radio).toHaveAccessibleDescription(/^Error: /);
  }
  await checkPage(page);
});

test("import radio groups have no default fieldset border", async ({ page, foreman }) => {
  await createAdmin(page);
  mkdirSync(join(foreman.dataDir, "import", "my-folder"), { recursive: true });
  writeFileSync(join(foreman.dataDir, "import", "my-folder", "a.zip"), "x");
  await page.goto("/import");
  const fieldset = page.locator("fieldset").first();
  await expect(fieldset).toBeVisible();
  expect(await fieldset.evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe("0px");
  await checkPage(page);
});
