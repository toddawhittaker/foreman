import { test, expect, checkPage, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { closeSync, ftruncateSync, mkdirSync, openSync, readFileSync, readdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { crc32 } from "node:zlib";

const password = "correct horse battery";

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
  await page.goto("/worlds");
}

interface SeedSave {
  name: string;
  content: Buffer | string;
  at: string;
}

// Worlds are folders a person can make by hand (SPEC 5.1), so the saves
// screens are tested over folders written directly.
function seedWorld(f: Foreman, id: string, meta: Record<string, unknown>, saves: SeedSave[]) {
  const dir = join(f.dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name: id, game_version: "2.0.72", space_age: true, created_at: "2026-11-01T10:00:00Z", ...meta }),
  );
  for (const s of saves) {
    const p = join(dir, "saves", s.name);
    writeFileSync(p, s.content);
    utimesSync(p, new Date(s.at), new Date(s.at));
  }
  return dir;
}

// A zip of stored (uncompressed) entries, enough for Foreman to read its directory.
function makeZip(entries: Record<string, string>): Buffer {
  const parts: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const [name, text] of Object.entries(entries)) {
    const nameBuf = Buffer.from(name);
    const body = Buffer.from(text);
    const crc = crc32(body);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(33, 12); // 1980-01-01
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(body.length, 22);
    local.writeUInt16LE(nameBuf.length, 26);
    parts.push(local, nameBuf, body);

    const head = Buffer.alloc(46);
    head.writeUInt32LE(0x02014b50, 0);
    head.writeUInt16LE(20, 4);
    head.writeUInt16LE(20, 6);
    head.writeUInt16LE(33, 14);
    head.writeUInt32LE(crc, 16);
    head.writeUInt32LE(body.length, 20);
    head.writeUInt32LE(body.length, 24);
    head.writeUInt16LE(nameBuf.length, 28);
    head.writeUInt32LE(offset, 42);
    central.push(head, nameBuf);
    offset += local.length + nameBuf.length + body.length;
  }
  const dirBytes = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(central.length / 2, 8);
  end.writeUInt16LE(central.length / 2, 10);
  end.writeUInt32LE(dirBytes.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...parts, dirBytes, end]);
}

const newSave = makeZip({ "level/level.dat0": "the newest map" });
const oldSave = makeZip({ "level/level.dat0": "an older map" });

function seedTwoSaves(f: Foreman, meta: Record<string, unknown> = {}) {
  return seedWorld(f, "family-base", { name: "Family base", ...meta }, [
    { name: "old.zip", content: oldSave, at: "2026-11-01T09:00:00Z" },
    { name: "new.zip", content: newSave, at: "2026-11-03T14:25:00Z" },
  ]);
}

function loadSaveOf(dir: string): unknown {
  return JSON.parse(readFileSync(join(dir, "world.json"), "utf8")).load_save;
}

test("saves page lists a world's saves, newest first", async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Saves of Family base");
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0).getByRole("link")).toHaveText("new.zip");
  await expect(rows.nth(0)).toContainText("Loads next");
  await expect(rows.nth(0).locator("time")).toHaveAttribute("datetime", "2026-11-03T14:25:00Z");
  await expect(rows.nth(0).locator("time")).toHaveText("2026-11-03 14:25 UTC");
  await expect(rows.nth(0)).toContainText(`${newSave.length} B`);
  await expect(rows.nth(1).getByRole("link")).toHaveText("old.zip");
  await expect(rows.nth(1)).not.toContainText("Loads next");
  await expect(page.getByRole("link", { name: "Back to Family base" })).toHaveAttribute("href", "/worlds/family-base");
  await checkPage(page);
});

test("a world with no saves says so and still offers the upload", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "empty", { name: "Empty" }, []);
  await page.goto("/worlds/empty/saves");
  await expect(page.getByText("This world has no saves yet. Upload a save file below.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Upload save" })).toBeVisible();
  await checkPage(page);
});

test("an unknown world is a 404", async ({ page }) => {
  await createAdmin(page);
  const resp = await page.goto("/worlds/nope/saves");
  expect(resp?.status()).toBe(404);
});

test("a save downloads with the same bytes", async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("link", { name: "old.zip", exact: true }).click()]);
  expect(download.suggestedFilename()).toBe("old.zip");
  expect(readFileSync(await download.path()).equals(oldSave)).toBe(true);
});

test("an uploaded save appears, is kept byte for byte, and loads as the newest", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const upload = makeZip({ "level/level.dat0": "a map from elsewhere" });
  await page.getByLabel("Save file").setInputFiles({ name: "from elsewhere.zip", mimeType: "application/zip", buffer: upload });
  await page.getByRole("button", { name: "Upload save" }).click();

  await expect(page).toHaveURL(/\/worlds\/family-base\/saves\?uploaded=/);
  await expect(page.getByText("Done: Uploaded from elsewhere.zip.")).toBeVisible();
  await expect(page.locator("tbody tr")).toHaveCount(3);
  await expect(page.locator("tbody tr").first().getByRole("link")).toHaveText("from elsewhere.zip");
  expect(readFileSync(join(dir, "saves", "from elsewhere.zip")).equals(upload)).toBe(true);
  expect(readdirSync(join(dir, "saves")).sort()).toEqual(["from elsewhere.zip", "new.zip", "old.zip"]);
  await checkPage(page);
});

test("a duplicate name is refused and the save is untouched", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  await page.getByLabel("Save file").setInputFiles({ name: "new.zip", mimeType: "application/zip", buffer: makeZip({ x: "replacement" }) });
  await page.getByRole("button", { name: "Upload save" }).click();

  await expect(page.locator("#error-summary")).toContainText("already has a save with that name");
  await expect(page.locator("#save-file-error")).toContainText("already has a save with that name");
  await expect(page.getByLabel("Save file")).toHaveAttribute("aria-invalid", "true");
  expect(readFileSync(join(dir, "saves", "new.zip")).equals(newSave)).toBe(true);
  expect(readdirSync(join(dir, "saves")).sort()).toEqual(["new.zip", "old.zip"]);
  await checkPage(page);
});

test("a file that is not a zip, or has an unsafe entry, is refused", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const refused = [
    { name: "notes.zip", buffer: Buffer.from("this is plain text"), message: "This file is not a zip file." },
    { name: "evil.zip", buffer: makeZip({ "../escape.txt": "x" }), message: "unsafe name" },
    { name: "map.txt", buffer: newSave, message: "must end in .zip" },
    { name: ".hidden.zip", buffer: newSave, message: "must end in .zip" },
  ];
  for (const r of refused) {
    await page.getByLabel("Save file").setInputFiles({ name: r.name, mimeType: "application/zip", buffer: r.buffer });
    await page.getByRole("button", { name: "Upload save" }).click();
    await expect(page.locator("#save-file-error"), r.name).toContainText(r.message);
    await expect(page.locator("#error-summary h2")).toContainText("Fix this problem to continue");
  }
  await checkPage(page);
  expect(readdirSync(join(dir, "saves")).sort()).toEqual(["new.zip", "old.zip"]);
});

test("an upload over the limit is refused before it is stored", async ({ page, foreman }) => {
  test.setTimeout(120_000);
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  // A sparse file one byte over 1 GiB takes no disk space.
  const big = join(tmpdir(), `foreman-oversized-${process.pid}-${Date.now()}.zip`);
  const fd = openSync(big, "w");
  ftruncateSync(fd, 2 ** 30 + 1);
  closeSync(fd);

  await page.goto("/worlds/family-base/saves");
  await page.getByLabel("Save file").setInputFiles(big);
  await page.getByRole("button", { name: "Upload save" }).click();

  await expect(page.locator("#save-file-error")).toContainText("larger than the limit of");
  expect(readdirSync(join(dir, "saves")).sort()).toEqual(["new.zip", "old.zip"]);
  await checkPage(page);
});

test("choosing which save loads is kept, and going back to the newest clears it", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  await expect(page.getByRole("radio", { name: "The newest save" })).toBeChecked();
  await page.getByRole("radio", { name: "old.zip" }).check();
  await page.getByRole("button", { name: "Use this choice" }).click();

  await expect(page).toHaveURL(/saves\?chosen=1/);
  await expect(page.getByText("The server will load old.zip the next time it starts.")).toBeVisible();
  await expect(page.getByRole("radio", { name: "old.zip" })).toBeChecked();
  await expect(page.locator("tbody tr").nth(1)).toContainText("Loads next");
  await expect(page.locator("tbody tr").nth(0)).not.toContainText("Loads next");
  expect(loadSaveOf(dir)).toBe("old.zip");
  // Everything else in world.json is kept.
  expect(JSON.parse(readFileSync(join(dir, "world.json"), "utf8"))).toMatchObject({ name: "Family base", game_version: "2.0.72", space_age: true });
  await checkPage(page);

  await page.getByRole("radio", { name: "The newest save" }).check();
  await page.getByRole("button", { name: "Use this choice" }).click();
  await expect(page.getByText("will load the newest save the next time it starts")).toBeVisible();
  await expect(page.locator("tbody tr").nth(0)).toContainText("Loads next");
  expect(loadSaveOf(dir) ?? "").toBe("");
});

test("a chosen save that was removed by hand is explained", async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman, { load_save: "gone.zip" });
  await page.goto("/worlds/family-base/saves");
  await expect(page.getByText("The chosen save gone.zip is no longer in the saves folder")).toBeVisible();
  await expect(page.getByRole("radio", { name: "The newest save" })).toBeChecked();
  await checkPage(page);
});

test("a world a newer Foreman wrote can be downloaded from but not changed", async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman, { format_version: 9 });
  await page.goto("/worlds/family-base/saves");
  await expect(page.getByText("A newer version of Foreman wrote this world")).toBeVisible();
  await expect(page.getByRole("link", { name: "new.zip", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Upload save" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Use this choice" })).toHaveCount(0);
  await checkPage(page);
});

test("a long list ends in Show older", async ({ page, foreman }) => {
  await createAdmin(page);
  const saves: SeedSave[] = [];
  for (let i = 0; i < 52; i++) {
    const day = String(1 + Math.floor(i / 24)).padStart(2, "0");
    const hour = String(i % 24).padStart(2, "0");
    saves.push({ name: `save-${String(i).padStart(2, "0")}.zip`, content: "x", at: `2026-10-${day}T${hour}:00:00Z` });
  }
  seedWorld(foreman, "many", { name: "Many" }, saves);
  await page.goto("/worlds/many/saves");
  await expect(page.locator("tbody tr")).toHaveCount(50);
  await checkPage(page);
  await page.getByRole("link", { name: "Show older saves" }).click();
  await expect(page.locator("tbody tr")).toHaveCount(52);
  await expect(page.getByRole("link", { name: "Show older saves" })).toHaveCount(0);
});

// DESIGN 6, "The 320 px rules": a long save name wraps instead of widening the page.
test("a very long save name wraps in the table and the radio list", async ({ page, foreman }) => {
  await createAdmin(page);
  const name = "autosave-with-a-very-long-name-".repeat(4) + ".zip";
  seedWorld(foreman, "longs", { name: "Long " + "w".repeat(80) }, [{ name, content: newSave, at: "2026-11-03T14:25:00Z" }]);
  await page.goto("/worlds/longs/saves");
  await expect(page.getByRole("link", { name })).toBeVisible();
  await expect(page.getByRole("group", { name: "Save to load" })).toBeVisible();
  expect(await page.locator("fieldset").evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe("0px");
  await checkPage(page);
});

// DESIGN 7.3: the success and note kinds are drawn, not left as plain text.
test("success and note messages have their own colours", async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman, { load_save: "gone.zip" });
  await page.goto("/worlds/family-base/saves");
  const note = page.locator(".message--note");
  await expect(note).toBeVisible();
  const bg = (loc: typeof note) => loc.evaluate((el) => getComputedStyle(el).backgroundColor);
  const plain = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  const noteBg = await bg(note);
  expect(noteBg).not.toBe(plain);
  await page.getByRole("radio", { name: "old.zip" }).check();
  await page.getByRole("button", { name: "Use this choice" }).click();
  const ok = page.locator(".message--success");
  await expect(ok).toBeVisible();
  expect(await bg(ok)).not.toBe(noteBg);
  expect(await bg(ok)).not.toBe(plain);
  await checkPage(page);
});

// DESIGN 7.2: a save that vanished between showing the page and choosing it
// is refused in words, the summary leads to the first radio, and every
// radio in the group is marked invalid.
test("choosing a save that has since gone is refused in words", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");
  await page.getByRole("radio", { name: "old.zip" }).check();
  rmSync(join(dir, "saves", "old.zip"));
  await page.getByRole("button", { name: "Use this choice" }).click();

  const msg = "Choose a save from the list. That save is no longer in the saves folder.";
  await expect(page.locator("#load-save-error")).toHaveText("Error: " + msg);
  const link = page.locator("#error-summary").getByRole("link", { name: msg });
  await expect(link).toHaveAttribute("href", "#load-newest");
  const radios = page.getByRole("group", { name: "Save to load" }).getByRole("radio");
  for (const radio of await radios.all()) {
    await expect(radio).toHaveAttribute("aria-invalid", "true");
    await expect(radio).toHaveAccessibleDescription("Error: " + msg);
  }
  await link.click();
  await expect(page.getByRole("radio", { name: "The newest save" })).toBeFocused();
  await checkPage(page);
});
