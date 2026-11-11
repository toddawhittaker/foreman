import { test, expect, checkPage, createAdmin, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, mkdirSync, readdirSync, readFileSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// The import offers the game image download, which needs Docker; the fake
// game stands in for it (SPEC 3.7). Docker Hub is never reachable here, so
// every typed version is imported unchecked (SPEC 10.7).
test.use({ fakeGameOn: true });

// The smallest valid zip: an end-of-central-directory record and no entries.
const emptyZip = Buffer.concat([Buffer.from("PK\x05\x06"), Buffer.alloc(18)]);

const fixtures = join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata");

function inboxFixture(f: Foreman, folder: string, fixture: string) {
  mkdirSync(join(f.dataDir, "import", folder), { recursive: true });
  copyFileSync(join(fixtures, fixture), join(f.dataDir, "import", folder, "a.zip"));
}

function inboxJunk(f: Foreman, folder: string) {
  mkdirSync(join(f.dataDir, "import", folder), { recursive: true });
  writeFileSync(join(f.dataDir, "import", folder, "a.zip"), "not a zip");
}

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

function worldJSON(f: Foreman, id: string) {
  return JSON.parse(readFileSync(join(f.dataDir, "worlds", id, "world.json"), "utf8"));
}

const folderForm = (page: Page) => page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });

test("imports a folder from the inbox, leaves it unchanged, and says Done", async ({ page, foreman }) => {
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
  // SPEC 3.5: the folder is named where the admin reaches it on the host.
  await expect(folderForm(page)).toContainText(`Put a folder of saves in ${join(foreman.dataDir, "import")}.`);
  await checkPage(page);

  // A wrong version is refused with the error summary (DESIGN 7.2).
  const form = folderForm(page);
  await expect(form.getByLabel(/saves-2025: 2 saves and mods/)).toBeChecked();
  await form.getByLabel("World name").fill("Old game");
  await form.getByLabel("Or type a version").fill("2.0");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page.locator("#error-summary")).toContainText("Enter the game version as three numbers");
  await expect(page).toHaveTitle(/^Error: /);
  await checkPage(page);

  await form.getByLabel("Or type a version").fill("2.0.72");
  await form.getByLabel("Download the game image", { exact: false }).uncheck();
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/old-game?imported=1&unchecked=1");
  await expect(page.locator(".message--success")).toHaveText("Done: Imported Old game.");
  await expect(page.locator(".message--warning")).toContainText("could not reach Docker Hub to check that Factorio 2.0.72 exists");
  await expect(page.locator("dl.facts")).toContainText("Imported fromThe folder saves-2025, save base.zip");
  await expect(page.locator("dl.facts")).toContainText("2.0.72");
  await checkPage(page);

  expect(listing(src)).toEqual(before);
  const world = join(foreman.dataDir, "worlds", "old-game");
  expect(readdirSync(join(world, "saves")).sort()).toEqual(["base.zip", "later.zip"]);
  expect(readdirSync(join(world, "mods"))).toEqual(["mod-list.json"]);
  expect(worldJSON(foreman, "old-game").imported_from).toMatchObject({ folder: "saves-2025", save_size: 22 });
});

// SPEC 5.5: the empty version choice names the save's version, and
// the world name comes from the server settings when they name one.
test("a single folder is chosen, named from its server settings, and uses the save's version", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "only", "2.1.24-space-age.zip");
  mkdirSync(join(foreman.dataDir, "import", "only", "config"));
  writeFileSync(join(foreman.dataDir, "import", "only", "config", "server-settings.json"), '{"name":"Friday factory","game_password":"hush"}');
  await page.goto("/import");
  const form = folderForm(page);
  await expect(form.getByLabel(/only: 1 save.*made with Factorio 2.1.24, Space Age/)).toBeChecked();
  await expect(form.getByLabel("World name")).toHaveValue("Friday factory");
  await expect(form.locator("#folder-version-select option").first()).toHaveText("Same as the save (2.1.24)");
  await expect(form.getByLabel("Or type a version")).toHaveValue("");
  await expect(form.getByLabel("Space Age is on")).toBeChecked();
  await expect(page.locator("body")).not.toContainText("hush");
  await checkPage(page);
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/friday-factory?imported=1");
  await expect(page.locator("dl.facts")).toContainText("2.1.24");
  expect(worldJSON(foreman, "friday-factory")).toMatchObject({ game_version: "2.1.24", space_age: true });
});

test("choosing a folder suggests its name and Space Age, but not over typed text", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "one", "2.1.24-space-age.zip");
  inboxFixture(foreman, "two", "2.0.77-no-space-age.zip");
  await page.goto("/import");
  const form = folderForm(page);
  const same = form.locator("#folder-version-select option").first();
  await expect(form.getByLabel("World name")).toHaveValue("");
  await expect(same).toHaveText("Same as the save");
  await form.getByLabel(/one: /).check();
  await expect(form.getByLabel("World name")).toHaveValue("one");
  await expect(same).toHaveText("Same as the save (2.1.24)");
  await expect(form.getByLabel("Space Age is on")).toBeChecked();
  await expect(page.locator("#announcer")).toHaveText("World name suggested: one.");
  await checkPage(page);
  await form.getByLabel(/one: /).uncheck();
  await form.getByLabel(/two: /).check();
  await expect(form.getByLabel("World name")).toHaveValue("two");
  await expect(same).toHaveText("Same as the save (2.0.77)");
  await expect(form.getByLabel("Space Age is on")).not.toBeChecked();
  await form.getByLabel("World name").fill("Mine");
  await form.getByLabel(/two: /).uncheck();
  await form.getByLabel(/one: /).check();
  await expect(form.getByLabel("World name")).toHaveValue("Mine");

  // Ticking a second folder empties the fields; going back to one puts the
  // typed name back.
  await form.getByLabel(/two: /).check();
  await expect(form.getByLabel("World name")).toHaveValue("");
  await form.getByLabel(/two: /).uncheck();
  await expect(form.getByLabel("World name")).toHaveValue("Mine");
  await expect(form.getByLabel("World name")).toBeEnabled();
});

// A save that cannot be read leaves the version to the admin.
test("an unreadable folder asks for the version", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "good", "2.1.24-space-age.zip");
  inboxJunk(foreman, "junk");
  await page.goto("/import");
  const form = folderForm(page);
  await form.getByLabel(/junk: .*game version unknown/).check();
  await expect(form.locator("#folder-version-select option").first()).toHaveText("Same as the save (could not be read, so choose or type one)");
  await form.getByLabel("World name").fill("Junk");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page.locator("#error-summary")).toContainText("Enter the game version as three numbers");
  await checkPage(page);
});

// SPEC 5.5: Space Age that contradicts the save is warned of beside the box,
// and Space Age on a 1.1 world is refused.
test("Space Age that differs from the save is warned of, and refused for 1.1", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "plain", "2.1.24-no-space-age.zip");
  inboxFixture(foreman, "old", "1.1.110-vanilla.zip");
  await page.goto("/import");
  const form = folderForm(page);
  const warning = form.locator("#folder-space-age-warning");
  await form.getByLabel(/plain: /).check();
  await expect(warning).toBeHidden();
  await form.getByLabel("Space Age is on").check();
  await expect(warning).toHaveText("Warning: The save was made without Space Age. Factorio converts it to Space Age when it first starts, and that cannot be undone.");
  await expect(form.getByLabel("Space Age is on")).toHaveAccessibleDescription(/converts it to Space Age/);
  await checkPage(page);

  await form.getByLabel(/plain: /).uncheck();
  await form.getByLabel(/old: /).check();
  await expect(warning).toContainText("Space Age needs Factorio 2.0 or later");
  await form.getByLabel("World name").fill("Old");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page.locator("#error-summary")).toContainText("Space Age needs Factorio 2.0 or later");
  await checkPage(page);
  expect(readdirSync(join(foreman.dataDir, "worlds")).filter((n) => !n.startsWith("."))).toEqual([]);
});

// SPEC 5.5: several folders import at once, each named after its folder
// with its save's version; a folder that cannot be read is skipped with its
// reason, and one download runs per missing version.
test("several folders import at once, each reported", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  inboxFixture(foreman, "sa", "2.1.24-space-age.zip");
  inboxFixture(foreman, "plain", "2.0.77-no-space-age.zip");
  inboxJunk(foreman, "junk");
  await page.goto("/import");
  const form = folderForm(page);
  await form.getByLabel(/sa: /).check();
  // A typed version and a chosen one are put away with several ticked, and
  // come back when one folder is ticked again.
  await form.getByLabel("Or type a version").fill("2.0.72");
  await form.locator("#folder-version-select").evaluate((el: HTMLSelectElement) => {
    el.add(new Option("9.9.9", "9.9.9"));
    el.value = "9.9.9";
  });
  await form.getByLabel(/plain: /).check();
  await expect(form.getByLabel("Or type a version")).toHaveValue("");
  await expect(form.locator("#folder-version-select")).toHaveValue("");
  await form.getByLabel(/plain: /).uncheck();
  await expect(form.getByLabel("Or type a version")).toHaveValue("2.0.72");
  await expect(form.locator("#folder-version-select")).toHaveValue("9.9.9");
  await form.getByLabel(/plain: /).check();
  await expect(form.getByLabel("World name")).toBeDisabled();
  await expect(page.locator("#announcer")).toContainText("Several folders chosen");
  await form.getByLabel(/junk: /).check();
  // The per-world fields are empty, unticked, off, and say why.
  await expect(form.getByLabel("World name")).toHaveValue("");
  await expect(form.getByLabel("Space Age is on")).not.toBeChecked();
  for (const field of [form.getByLabel("World name"), form.getByLabel("Or type a version"), form.getByLabel("Space Age is on")]) {
    await expect(field).toBeDisabled();
    await expect(field).toHaveAccessibleDescription(/Tick one folder to choose its name and version/);
  }
  await expect(form.locator("#folder-version-select")).toBeDisabled();
  await checkPage(page);
  await form.getByRole("button", { name: "Import folder" }).click();

  const done = page.locator(".message--success");
  await expect(done).toContainText("Done: Imported 2 folders.");
  await expect(done.getByRole("link", { name: "Open sa" })).toHaveAttribute("href", "/worlds/sa");
  await expect(done).toContainText("Downloading the game image for Factorio 2.0.77, 2.1.24.");
  await expect(page.locator(".message--warning")).toContainText("junk: Foreman could not read its save");
  await expect(page.locator("#import-result")).toBeFocused();
  await checkPage(page);

  expect(worldJSON(foreman, "sa")).toMatchObject({ name: "sa", game_version: "2.1.24", space_age: true });
  expect(worldJSON(foreman, "plain")).toMatchObject({ name: "plain", game_version: "2.0.77", space_age: false });
  await expect.poll(async () => (await fakeGame!.state()).calls.filter((c) => c.includes("images/create")).length).toBe(2);

  // SPEC 17.9: the skipped folder has a link that leads to importing it alone.
  await page.getByRole("link", { name: "Import junk on its own" }).click();
  await expect(page).toHaveURL("/import?folder=junk");
  const alone = folderForm(page);
  await expect(alone.getByLabel(/junk: /)).toBeChecked();
  await expect(alone.getByLabel("World name")).toHaveValue("junk");
  await expect(alone.getByLabel("World name")).toBeEnabled();
  // Its version is unknown, so the typed-version field says so and has focus.
  await expect(alone.getByLabel("Or type a version")).toBeFocused();
  await expect(alone.getByLabel("Or type a version")).toHaveAccessibleDescription(/Type the version this save was made with\./);
  await expect(alone.getByText("Type the version this save was made with.")).toBeVisible();
  await checkPage(page);
  await alone.getByLabel("Or type a version").fill("2.0.72");
  await alone.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/junk?imported=1&unchecked=1");

  // A folder whose version is known gets no hint and no focus change.
  await page.goto("/import?folder=sa");
  await expect(folderForm(page).getByLabel(/sa: /)).toBeChecked();
  await expect(folderForm(page).getByLabel("Or type a version")).not.toBeFocused();
  await expect(page.getByText("Type the version this save was made with.")).toHaveCount(0);
});

// SPEC 5.5: a folder already imported is listed apart, names its
// world, and can be imported again; one with a newer save waits again.
test("folders already imported are listed apart", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "done", "2.1.24-no-space-age.zip");
  inboxFixture(foreman, "waiting", "2.1.24-no-space-age.zip");
  await page.goto("/import");
  const form = folderForm(page);
  await form.getByLabel(/done: /).check();
  await form.getByLabel("World name").fill("First");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/first?imported=1");

  await page.goto("/import");
  await expect(form.getByText("Already imported.")).toBeVisible();
  const done = form.getByLabel(/done: .*Imported as First on/);
  await expect(done).toBeEnabled();
  await expect(form.getByRole("link", { name: "Open First" })).toHaveAttribute("href", "/worlds/first");
  await expect(form.getByLabel(/waiting: /)).toBeChecked(); // the one folder still waiting
  await checkPage(page);

  const later = new Date(Date.now() + 60_000);
  copyFileSync(join(fixtures, "2.1.24-no-space-age.zip"), join(foreman.dataDir, "import", "done", "b.zip"));
  utimesSync(join(foreman.dataDir, "import", "done", "b.zip"), later, later);
  await page.reload();
  await expect(form.getByText("Already imported.")).toHaveCount(0);
});

// SPEC 5.5: folders that cannot be imported are listed apart, in words.
test("folders that cannot be imported are listed apart", async ({ page, foreman }) => {
  await createAdmin(page);
  mkdirSync(join(foreman.dataDir, "import", "current", "mods"), { recursive: true });
  await page.goto("/import");
  const form = folderForm(page);
  await expect(form.locator("#unimportable")).toHaveText("current: no save at its top level or in a saves folder, so it cannot be imported");
  await expect(form.getByRole("checkbox", { name: /current/ })).toHaveCount(0);
  await expect(form).toContainText("There is nothing to import");
  await checkPage(page);

  inboxFixture(foreman, "good", "2.1.24-space-age.zip");
  await page.reload();
  await expect(form).not.toContainText("There is nothing to import");
  await expect(form.getByLabel(/good: 1 save/)).toBeChecked();
  await expect(form.getByLabel("World name")).toHaveValue("good");
  await checkPage(page);
});

// DESIGN 7.2: each folder checkbox points at the error when none is ticked.
test("an import with no folder chosen describes the error on every checkbox", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxJunk(foreman, "one");
  inboxJunk(foreman, "two");
  await page.goto("/import");
  const form = folderForm(page);
  await form.getByLabel("World name").fill("Imported");
  await form.getByLabel("Or type a version").fill("2.0.72");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page.locator("#folder-source-error")).toBeVisible();
  const boxes = form.getByRole("checkbox", { name: /: 1 save/ });
  await expect(boxes).toHaveCount(2);
  for (const box of await boxes.all()) {
    await expect(box).toHaveAttribute("aria-invalid", "true");
    await expect(box).toHaveAccessibleDescription(/^Error: /);
  }
  await checkPage(page);
});

test("import folder lists have no default fieldset border", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxJunk(foreman, "my-folder");
  await page.goto("/import");
  const fieldset = page.locator("main fieldset").first();
  await expect(fieldset).toBeVisible();
  expect(await fieldset.evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe("0px");
  await checkPage(page);
});

test("imports a world by uploading a save, and suggests its name", async ({ page, foreman }) => {
  await createAdmin(page);
  await page.goto("/import");
  await expect(page.getByText("The import folder is empty")).toBeVisible();
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Upload a save" }) });
  await form.getByLabel("Save file").setInputFiles({ name: "my save.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(form.getByLabel("World name")).toHaveValue("my save");
  await expect(page.locator("#announcer")).toHaveText("World name suggested: my save.");
  await form.getByLabel("World name").fill("Uploaded");
  await form.getByLabel("Or type a version").fill("1.1.110");
  await form.getByRole("button", { name: "Upload and import" }).click();
  await expect(page).toHaveURL("/worlds/uploaded?imported=1&unchecked=1");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Uploaded Active");
  await expect(page.locator(".message--success")).toHaveText("Done: Imported Uploaded.");
  expect(readdirSync(join(foreman.dataDir, "worlds", "uploaded", "saves"))).toEqual(["my save.zip"]);

  // A file that is not a zip is refused and no world is made.
  await page.goto("/import");
  await form.getByLabel("World name").fill("Second");
  await form.getByLabel("Or type a version").fill("1.1.110");
  await form.getByLabel("Save file").setInputFiles({ name: "fake.zip", mimeType: "application/zip", buffer: Buffer.from("nope") });
  await form.getByRole("button", { name: "Upload and import" }).click();
  await expect(page.locator("#error-summary")).toContainText("This file is not a zip file.");
  await checkPage(page);
  expect(readdirSync(join(foreman.dataDir, "worlds"))).toEqual(["uploaded"]);
});

// SPEC 10.7: with no version list, a typed version still imports.
test("accepts a typed game version while Docker Hub is unreachable", async ({ page, foreman }) => {
  await createAdmin(page);
  inboxFixture(foreman, "old", "2.0.77-no-space-age.zip");
  await page.goto("/import");
  const form = folderForm(page);
  await page.waitForLoadState("networkidle");
  await expect(form.locator("#folder-version-select option")).toHaveText(["Same as the save (2.0.77)", "Docker Hub cannot be reached; type a version below"]);
  await form.getByLabel("World name").fill("Typed");
  await form.getByLabel("Or type a version").fill("2.0.78");
  await checkPage(page);
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/typed?imported=1&unchecked=1");
  await expect(page.locator("dl.facts")).toContainText("2.0.78");
  expect(worldJSON(foreman, "typed").game_version).toBe("2.0.78");
});
