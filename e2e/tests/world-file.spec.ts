import { test, expect, checkPage, createAdmin, seedWorld, setUpModsWorld, type Foreman } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Download, Page } from "@playwright/test";
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, statSync, symlinkSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { crc32, inflateRawSync } from "node:zlib";
import { DatabaseSync } from "node:sqlite";

// SPEC 5.13 (export a world file), 5.14 (import one), 14.5 and 14.6 (the file is hostile), ADR 0031 (the
// format). Element ids are the contract of design-774 section 2.1: act-export-world, export-saves-all,
// export-saves-loaded, export-warning on the world page; world-file and act-upload-world-file on the Import
// page; world-file-name, world-file-download, act-import-world-file and world-file-password-warning on the
// preview. The fake game stands in for Docker (the image download of 5.5).
test.use({ fakeGameOn: true });

const fixture = join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata", "2.1.24-no-space-age.zip");
const saveBytes = readFileSync(fixture);

// Values that must never leave the host in a world file (SPEC 5.13, 14.8).
const secrets = {
  game: "canary-game-password-7f3a",
  pass: "canary-portal-password-91bc",
  token: "canary-portal-token-4d20",
  rcon: "canary-rcon-password-e8a1",
};
const secretValues = Object.values(secrets);

// --- A small ZIP reader and writer, so the tests never trust Foreman to check Foreman's file. ---

interface Entry {
  name: string;
  data: Buffer;
  method: number;
  // Seconds since the epoch, from the extended timestamp when there is one, else the DOS time read as UTC.
  mtime: number;
}

function readZip(buf: Buffer): Entry[] {
  let end = buf.length - 22;
  while (end >= 0 && buf.readUInt32LE(end) !== 0x06054b50) end--;
  if (end < 0) throw new Error("no end-of-central-directory record: not a complete zip");
  const count = buf.readUInt16LE(end + 10);
  let p = buf.readUInt32LE(end + 16);
  const entries: Entry[] = [];
  for (let i = 0; i < count; i++) {
    if (buf.readUInt32LE(p) !== 0x02014b50) throw new Error("bad central directory");
    const method = buf.readUInt16LE(p + 10);
    const dosTime = buf.readUInt16LE(p + 12);
    const dosDate = buf.readUInt16LE(p + 14);
    const csize = buf.readUInt32LE(p + 20);
    const nameLen = buf.readUInt16LE(p + 28);
    const extraLen = buf.readUInt16LE(p + 30);
    const commentLen = buf.readUInt16LE(p + 32);
    const local = buf.readUInt32LE(p + 42);
    const name = buf.subarray(p + 46, p + 46 + nameLen).toString("utf8");
    let mtime = Date.UTC(1980 + (dosDate >> 9), ((dosDate >> 5) & 15) - 1, dosDate & 31, dosTime >> 11, (dosTime >> 5) & 63, (dosTime & 31) * 2) / 1000;
    for (let e = p + 46 + nameLen; e + 4 <= p + 46 + nameLen + extraLen; ) {
      const id = buf.readUInt16LE(e);
      const size = buf.readUInt16LE(e + 2);
      if (id === 0x5455 && (buf[e + 4] & 1) !== 0) mtime = buf.readUInt32LE(e + 5);
      e += 4 + size;
    }
    const start = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const raw = buf.subarray(start, start + csize);
    entries.push({ name, method, mtime, data: method === 0 ? Buffer.from(raw) : inflateRawSync(raw) });
    p += 46 + nameLen + extraLen + commentLen;
  }
  return entries;
}

// A stored ZIP with names, bytes and times of our choosing, including names no honest writer would use.
function buildZip(entries: { name: string; data: Buffer | string; mtime?: Date }[]): Buffer {
  const parts: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const { name, data, mtime = new Date("2026-10-01T12:00:00Z") } of entries) {
    const nameBuf = Buffer.from(name);
    const body = Buffer.from(data);
    const crc = crc32(body);
    const dosTime = (mtime.getUTCHours() << 11) | (mtime.getUTCMinutes() << 5) | (mtime.getUTCSeconds() >> 1);
    const dosDate = ((mtime.getUTCFullYear() - 1980) << 9) | ((mtime.getUTCMonth() + 1) << 5) | mtime.getUTCDate();
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(dosTime, 10);
    local.writeUInt16LE(dosDate, 12);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(body.length, 22);
    local.writeUInt16LE(nameBuf.length, 26);
    parts.push(local, nameBuf, body);
    const head = Buffer.alloc(46);
    head.writeUInt32LE(0x02014b50, 0);
    head.writeUInt16LE(20, 4);
    head.writeUInt16LE(20, 6);
    head.writeUInt16LE(dosTime, 12);
    head.writeUInt16LE(dosDate, 14);
    head.writeUInt32LE(crc, 16);
    head.writeUInt32LE(body.length, 20);
    head.writeUInt32LE(body.length, 24);
    head.writeUInt16LE(nameBuf.length, 28);
    head.writeUInt32LE(offset, 42);
    central.push(head, nameBuf);
    offset += local.length + nameBuf.length + body.length;
  }
  const dir = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(central.length / 2, 8);
  end.writeUInt16LE(central.length / 2, 10);
  end.writeUInt32LE(dir.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...parts, dir, end]);
}

// A well-formed world file with one part changed, for the refusals. The save is a real one.
function worldFile(over: { format?: Record<string, unknown>; world?: Record<string, unknown>; extra?: { name: string; data: string }[]; skip?: string[] } = {}): Buffer {
  const entries = [
    { name: "foreman-world.json", data: JSON.stringify({ format_version: 1, exported_at: "2026-11-20T10:00:00Z", foreman_version: "1.0.0", saves: "all", game_password_set: false, ...over.format }) },
    { name: "world.json", data: JSON.stringify({ format_version: 1, name: "Crafted", game_version: "2.1.24", space_age: false, created_at: "2026-11-01T10:00:00Z", ...over.world }) },
    { name: "saves/base.zip", data: saveBytes as Buffer | string },
    ...(over.extra ?? []),
  ].filter((e) => !over.skip?.includes(e.name));
  return buildZip(entries);
}

// --- Seeding and driving ---

const worldDir = (f: Foreman, id = "base") => join(f.dataDir, "worlds", id);
const secretsAnywhere = (buf: Buffer | string) => secretValues.filter((s) => buf.toString("latin1").includes(s));

// World "base" with everything an export must keep and everything it must leave behind (SPEC 5.13).
function seedFull(f: Foreman, opts: { gamePassword?: boolean; extraSave?: boolean } = {}) {
  seedWorld(f, { mods: { "some-mod": "2.0" } });
  const dir = worldDir(f);
  const world = JSON.parse(readFileSync(join(dir, "world.json"), "utf8"));
  Object.assign(world, { imported_from: { folder: "old-host-folder", save_size: 1 }, archived: true, load_save: "base.zip" });
  writeFileSync(join(dir, "world.json"), JSON.stringify(world));
  mkdirSync(join(dir, "mods", "folder-mod", "locale"), { recursive: true });
  writeFileSync(join(dir, "mods", "folder-mod", "info.json"), "{}");
  writeFileSync(join(dir, "mods", "folder-mod", "locale", "en.cfg"), "a=b");
  writeFileSync(join(dir, "mods", "mod-settings.dat"), "settings");
  writeFileSync(join(dir, "config", "rconpw"), secrets.rcon);
  writeFileSync(join(dir, "config", "map-gen-settings.json"), '{"seed": 7}');
  writeFileSync(
    join(dir, "config", "server-settings.json"),
    JSON.stringify({ name: "Base", description: "keep me", visibility: { public: false, lan: false }, username: "someone", password: secrets.pass, token: secrets.token, ...(opts.gamePassword === false ? {} : { game_password: secrets.game }) }),
  );
  // What an export must never carry.
  writeFileSync(join(dir, "saves", "stale.tmp.zip"), "half a save");
  writeFileSync(join(dir, ".hidden"), "hidden");
  writeFileSync(join(dir, "players.json"), "{}");
  writeFileSync(join(dir, "notes.txt"), "stray");
  const old = new Date("2026-09-15T08:30:10Z");
  utimesSync(join(dir, "saves", "base.zip"), old, old);
  if (opts.extraSave) {
    copyFileSync(fixture, join(dir, "saves", "second.zip"));
    const t = new Date("2026-09-16T08:30:10Z");
    utimesSync(join(dir, "saves", "second.zip"), t, t);
  }
}

async function exportFrom(page: Page): Promise<Download> {
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export world" }).click();
  return download;
}

async function exported(page: Page): Promise<{ name: string; entries: Entry[]; raw: Buffer }> {
  const d = await exportFrom(page);
  const raw = readFileSync((await d.path())!);
  return { name: d.suggestedFilename(), entries: readZip(raw), raw };
}

const byName = (entries: Entry[], name: string) => entries.find((e) => e.name === name);
const json = (e: Entry | undefined) => JSON.parse(e!.data.toString("utf8"));

const worldIds = (f: Foreman) => readdirSync(join(f.dataDir, "worlds")).filter((n) => !n.startsWith(".")).sort();
const staged = (f: Foreman) => readdirSync(join(f.dataDir, "worlds")).filter((n) => n.startsWith(".partial-worldfile-"));

const auditRows = (f: Foreman, action: string) => {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return db.prepare("SELECT world_id, detail FROM audit_log WHERE action = ?").all(action) as { world_id: string | null; detail: string }[];
  } finally {
    db.close();
  }
};

// The Import page's third form: one drop zone and its upload button.
async function upload(page: Page, file: Buffer, name = "world.zip") {
  await page.goto("/import");
  await page.locator("#world-file").setInputFiles({ name, mimeType: "application/zip", buffer: file });
  await page.locator("#act-upload-world-file").click();
}

const nameField = (page: Page) => page.locator("#world-file-name");
const confirm = (page: Page) => page.locator("#act-import-world-file");

// --- Export ---

test("the world page offers the export with its choice and a warning tied to the button", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman, { extraSave: true });
  await page.goto("/worlds/base");
  const button = page.locator("#act-export-world");
  await expect(button).toHaveText("Export world");
  // D3: all saves is the default; the other is the save that loads next.
  await expect(page.locator("#export-saves-all")).toBeChecked();
  await expect(page.locator("#export-saves-loaded")).not.toBeChecked();
  await expect(page.getByLabel("All saves")).toBeVisible();
  await expect(page.getByLabel("Only the save that loads next")).toBeVisible();
  // The Warning (DESIGN 7.11): what a mod stores may be in the file, and the game password is not.
  const warning = page.locator("#export-warning");
  await expect(warning).toBeVisible();
  await expect(warning).toContainText("mod-settings.dat");
  await expect(warning).toContainText(/game password/i);
  expect(await button.getAttribute("aria-describedby")).toContain("export-warning");
  // The card says what stays behind in the database (SPEC 5.13).
  await expect(page.locator("main")).toContainText(/audit log/i);
  await checkPage(page);
});

test("the export holds the world folder's files, nothing else, and no secret", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman, { extraSave: true });
  await page.goto("/worlds/base");
  const { name, entries, raw } = await exported(page);

  expect(name).toMatch(/-world\.zip$/i);
  // The format file is first (ADR 0031).
  expect(entries[0].name).toBe("foreman-world.json");
  const doc = json(entries[0]);
  expect(doc).toMatchObject({ format_version: 1, saves: "all", game_password_set: true });
  expect(typeof doc.foreman_version).toBe("string");
  expect(Date.now() - new Date(doc.exported_at).getTime()).toBeLessThan(120_000);

  const names = entries.map((e) => e.name).sort();
  expect(names).toEqual([
    "config/map-gen-settings.json",
    "config/server-settings.json",
    "foreman-world.json",
    "mods/folder-mod/info.json",
    "mods/folder-mod/locale/en.cfg",
    "mods/mod-list.json",
    "mods/mod-settings.dat",
    "mods/some-mod_1.0.0.zip",
    "saves/base.zip",
    "saves/second.zip",
    "world.json",
  ]);
  // Never: the RCON password, work files, hidden and stray files, the players record.
  for (const never of ["config/rconpw", "players.json", "notes.txt", ".hidden", "saves/stale.tmp.zip"]) expect(byName(entries, never), never).toBeUndefined();

  // world.json loses what names the old host or the archive, and keeps the rest.
  const world = json(byName(entries, "world.json"));
  expect(world).not.toHaveProperty("imported_from");
  expect(world).not.toHaveProperty("archived");
  expect(world).toMatchObject({ name: "Base", game_version: "2.1.24", load_save: "base.zip" });

  // server-settings.json keeps what is not secret and nothing else (14.8). No value appears anywhere in the file.
  const settings = json(byName(entries, "config/server-settings.json"));
  expect(settings.description).toBe("keep me");
  for (const key of ["game_password", "password", "token", "username"]) expect(settings, key).not.toHaveProperty(key);
  expect(secretsAnywhere(raw)).toEqual([]);

  // Saves are the file's own bytes, and each entry keeps its modification time.
  expect(byName(entries, "saves/base.zip")!.data.equals(saveBytes)).toBe(true);
  expect(Math.abs(byName(entries, "saves/base.zip")!.mtime - Date.parse("2026-09-15T08:30:10Z") / 1000)).toBeLessThanOrEqual(2);
  expect(Math.abs(byName(entries, "saves/second.zip")!.mtime - Date.parse("2026-09-16T08:30:10Z") / 1000)).toBeLessThanOrEqual(2);
  // A save and a mod zip are stored, not compressed again.
  expect(byName(entries, "saves/base.zip")!.method).toBe(0);
  expect(byName(entries, "mods/some-mod_1.0.0.zip")!.method).toBe(0);

  // The world folder is as it was (export only reads), and the staging folder is gone.
  expect(readFileSync(join(worldDir(foreman), "config", "rconpw"), "utf8")).toBe(secrets.rcon);
  expect(readdirSync(join(foreman.dataDir, "worlds")).filter((n) => n.startsWith(".staging-export-"))).toEqual([]);
  const rows = auditRows(foreman, "world-export");
  expect(rows).toHaveLength(1);
  expect(rows[0].world_id).toBe("base");
  expect(rows[0].detail).toMatch(/all saves/i);
  expect(JSON.stringify(rows)).not.toContain(secrets.game);
});

test("the other choice exports only the save that loads next", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman, { extraSave: true });
  const dir = worldDir(foreman);
  const world = JSON.parse(readFileSync(join(dir, "world.json"), "utf8"));
  world.load_save = "second.zip";
  writeFileSync(join(dir, "world.json"), JSON.stringify(world));
  await page.goto("/worlds/base");
  await page.locator("#export-saves-loaded").check();
  const { entries } = await exported(page);
  expect(entries.map((e) => e.name).filter((n) => n.startsWith("saves/"))).toEqual(["saves/second.zip"]);
  expect(json(entries[0]).saves).toBe("loaded");
  expect(json(byName(entries, "world.json")).load_save).toBe("second.zip");
  // Mods and config come along either way.
  expect(byName(entries, "mods/mod-list.json")).toBeDefined();
  expect(byName(entries, "config/server-settings.json")).toBeDefined();
  expect(auditRows(foreman, "world-export")[0].detail).toMatch(/loaded save/i);
});

test("a world with no game password says so in the file, and Export is two clicks from the Server page", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman, { gamePassword: false });
  await page.goto("/");
  const clicks = await countClicks(page);
  await page.getByRole("link", { name: "World page" }).click();
  await expect(page).toHaveURL("/worlds/base");
  const { entries } = await exported(page);
  expect(json(entries[0]).game_password_set).toBe(false);
  await expectWithin(clicks, 2, "export a world");
});

test("a world with a symlink in it is refused, naming the path, and nothing is downloaded", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman);
  symlinkSync("/etc/hostname", join(worldDir(foreman), "mods", "sneaky-link"));
  await page.goto("/worlds/base");
  let downloaded = false;
  page.on("download", () => (downloaded = true));
  const reply = page.waitForResponse((r) => r.url().endsWith("/worlds/base/export"));
  await page.getByRole("button", { name: "Export world" }).click();
  expect((await reply).status()).toBeGreaterThanOrEqual(400);
  await expect(page.locator("main")).toContainText("sneaky-link");
  expect(downloaded).toBe(false);
  expect(auditRows(foreman, "world-export")).toEqual([]);
  expect(readdirSync(join(foreman.dataDir, "worlds")).filter((n) => n.startsWith(".staging-export-"))).toEqual([]);
  await checkPage(page);
});

test("a world a newer Foreman wrote has no Export", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman);
  const dir = worldDir(foreman);
  const world = JSON.parse(readFileSync(join(dir, "world.json"), "utf8"));
  world.format_version = 99;
  writeFileSync(join(dir, "world.json"), JSON.stringify(world));
  await page.goto("/worlds/base");
  await expect(page.locator("main h1")).toBeVisible();
  await expect(page.locator("#act-export-world")).toHaveCount(0);
});

test("a running world exports the last save on disk, and the card says so", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { start: true });
  await page.goto("/worlds/base");
  await expect(page.locator("main")).toContainText(/last save/i);
  const { entries } = await exported(page);
  expect(byName(entries, "saves/base.zip")).toBeDefined();
  expect(json(entries[0]).format_version).toBe(1);
});

// --- Import ---

test("the Import page has a third form with its own drop zone", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a world file" }) });
  await expect(form).toBeVisible();
  await expect(form.locator("#world-file")).toHaveAttribute("type", "file");
  await expect(form.locator("#world-file")).toHaveAttribute("accept", /\.zip/);
  await expect(form.locator("#act-upload-world-file")).toHaveText("Upload world file");
  await checkPage(page);
});

test("a world file round trip: preview, confirm, and the new world holds the same files", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedFull(foreman, { extraSave: true });
  await page.goto("/worlds/base");
  const { entries, raw } = await exported(page);

  await upload(page, raw, "base-world.zip");
  // The preview is its own page; the staged file's name is in a posted field, never in the address (17.17).
  await expect(nameField(page)).toHaveValue("Base");
  expect(page.url()).not.toMatch(/partial|worldfile-[0-9a-f]/i);
  expect(staged(foreman)).toHaveLength(1);
  await expect(page.locator("main")).toContainText("2.1.24");
  await expect(page.locator("main")).toContainText(/2 saves/);
  // The game password was not in the file (5.14).
  await expect(page.locator("#world-file-password-warning")).toBeVisible();
  await expect(page.locator("#world-file-password-warning")).toContainText(/no password/i);
  // This host has no 2.1.24 image yet, so the download is ticked by default (5.5).
  await expect(page.locator("#world-file-download")).toBeChecked();
  await checkPage(page);

  await nameField(page).fill("Moved");
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/moved\?imported=1/);
  expect(staged(foreman)).toEqual([]);
  await expect.poll(async () => (await fakeGame!.state()).calls.filter((c) => c.includes("images/create")).length).toBeGreaterThan(0);
  await checkPage(page);

  // A new world with a fresh id; the old one is untouched.
  expect(worldIds(foreman)).toEqual(["base", "moved"]);
  const dir = worldDir(foreman, "moved");
  const world = JSON.parse(readFileSync(join(dir, "world.json"), "utf8"));
  expect(world).toMatchObject({ name: "Moved", game_version: "2.1.24", load_save: "base.zip" });
  for (const gone of ["imported_from", "archived"]) expect(world).not.toHaveProperty(gone);

  // The files are byte for byte what was exported, with their times.
  for (const e of entries.filter((e) => /^(saves|mods)\//.test(e.name))) {
    const path = join(dir, e.name);
    expect(readFileSync(path).equals(e.data), e.name).toBe(true);
  }
  expect(Math.abs(statSync(join(dir, "saves", "base.zip")).mtimeMs / 1000 - Date.parse("2026-09-15T08:30:10Z") / 1000)).toBeLessThanOrEqual(2);
  expect(readFileSync(join(dir, "config", "map-gen-settings.json"), "utf8")).toBe('{"seed": 7}');
  // Secrets stay out, and the RCON password is made at the first start, not carried.
  expect(existsSync(join(dir, "config", "rconpw"))).toBe(false);
  const settings = JSON.parse(readFileSync(join(dir, "config", "server-settings.json"), "utf8"));
  expect(settings.description).toBe("keep me");
  expect(Object.keys(settings)).not.toEqual(expect.arrayContaining(["game_password"]));
  for (const f of ["server-settings.json", "world.json"]) expect(secretsAnywhere(readFileSync(join(dir, f)))).toEqual([]);

  const rows = auditRows(foreman, "world-import");
  expect(rows).toHaveLength(1);
  expect(rows[0].world_id).toBe("moved");
  expect(JSON.stringify(rows)).not.toContain(secrets.game);
});

test("a name in use is refused on the name field and the staged file is kept", async ({ page, foreman }) => {
  await createAdmin(page);
  seedFull(foreman, { gamePassword: false });
  await page.goto("/worlds/base");
  const { raw } = await exported(page);

  await upload(page, raw);
  await expect(nameField(page)).toHaveValue("Base");
  // No game password was set, so no password warning.
  await expect(page.locator("#world-file-password-warning")).toHaveCount(0);
  const [stagedBefore] = staged(foreman);
  expect(stagedBefore).toBeDefined();

  // "Base" is taken: refused, with the error summary (DESIGN 7.2), on the same page and no second upload.
  await confirm(page).click();
  await expect(page.locator("#error-summary")).toContainText(/name/i);
  await expect(page).toHaveTitle(/^Error: /);
  await expect(nameField(page)).toHaveAttribute("aria-invalid", "true");
  expect(worldIds(foreman)).toEqual(["base"]);
  expect(staged(foreman)).toEqual([stagedBefore]);
  await checkPage(page);

  // Another name works with no second upload; the name's case does not get around the clash.
  await nameField(page).fill("BASE");
  await confirm(page).click();
  await expect(page.locator("#error-summary")).toBeVisible();
  expect(staged(foreman)).toEqual([stagedBefore]);
  await nameField(page).fill("Second try");
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/second-try\?imported=1/);
  expect(worldIds(foreman)).toEqual(["base", "second-try"]);
  expect(staged(foreman)).toEqual([]);
});

test("the game image download is ticked when the image is missing, and is only started when ticked", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  const images = async () => (await fakeGame!.state()).calls.filter((c) => c.includes("images/create")).length;

  await upload(page, worldFile());
  await expect(page.locator("#world-file-download")).toBeChecked();
  await page.locator("#world-file-download").uncheck();
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/crafted\?imported=1/);
  expect(await images()).toBe(0);

  await upload(page, worldFile({ world: { name: "Crafted again" } }));
  await expect(page.locator("#world-file-download")).toBeChecked();
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/crafted-again\?imported=1/);
  await expect.poll(images).toBe(1);
});

test("a world file made by a newer Foreman is refused, and says so", async ({ page, foreman }) => {
  await createAdmin(page);
  await upload(page, worldFile({ format: { format_version: 2 } }));
  await expect(page.locator("#error-summary")).toContainText(/newer/i);
  expect(worldIds(foreman)).toEqual([]);
  expect(staged(foreman)).toEqual([]);
  await checkPage(page);
});

// SPEC 14.5 and 14.6: the file is hostile. Each refusal uses one fixed message that never repeats what the file
// holds, makes no world and leaves no staged file behind.
const canary = "canary-entry-name-2c91";
const refusals: [string, () => Buffer][] = [
  ["a file that is not a zip", () => Buffer.from("this is not a zip file")],
  ["an empty zip", () => Buffer.concat([Buffer.from("PK\x05\x06"), Buffer.alloc(18)])],
  ["a truncated zip", () => worldFile().subarray(0, 4000)],
  ["a path that climbs out of the folder", () => worldFile({ extra: [{ name: `saves/../../${canary}`, data: "x" }] })],
  ["an absolute path", () => worldFile({ extra: [{ name: `/${canary}`, data: "x" }] })],
  ["a backslash path", () => worldFile({ extra: [{ name: `mods\\${canary}`, data: "x" }] })],
  ["a mod path that climbs out", () => worldFile({ extra: [{ name: `mods/../${canary}`, data: "x" }] })],
  ["the RCON password", () => worldFile({ extra: [{ name: "config/rconpw", data: secrets.rcon }] })],
  ["an entry outside the layout", () => worldFile({ extra: [{ name: `${canary}.txt`, data: "x" }] })],
  ["a save in a subfolder", () => worldFile({ extra: [{ name: `saves/sub/${canary}.zip`, data: "x" }] })],
  ["a repeated name", () => worldFile({ extra: [{ name: "saves/base.zip", data: "again" }] })],
  ["no saves and no new map", () => worldFile({ skip: ["saves/base.zip"] })],
  ["a game version this Foreman does not support", () => worldFile({ world: { game_version: "0.0.1" } })],
  ["a world.json that is not JSON", () => buildZip([{ name: "foreman-world.json", data: '{"format_version":1}' }, { name: "world.json", data: canary }, { name: "saves/base.zip", data: saveBytes }])],
];

for (const [what, make] of refusals) {
  test(`a bad file is refused with a fixed message: ${what}`, async ({ page, foreman }) => {
    await createAdmin(page);
    await upload(page, make());
    await expect(page.locator("#error-summary")).toBeVisible();
    await expect(page).toHaveTitle(/^Error: /);
    // The page never repeats a name, a value or a secret from the file.
    const body = await page.content();
    for (const never of [canary, ...secretValues]) expect(body, never).not.toContain(never);
    expect(worldIds(foreman)).toEqual([]);
    expect(staged(foreman)).toEqual([]);
    expect(auditRows(foreman, "world-import")).toEqual([]);
    // The form is still there to try again.
    await expect(page.locator("#world-file")).toBeVisible();
    await checkPage(page);
  });
}

test("a crafted file with a load_save that is not in it imports", async ({ page, foreman }) => {
  await createAdmin(page);
  await upload(page, worldFile({ world: { load_save: "gone.zip" } }));
  await page.locator("#world-file-download").uncheck();
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/crafted\?imported=1/);
  const world = JSON.parse(readFileSync(join(worldDir(foreman, "crafted"), "world.json"), "utf8"));
  // 5.14: a load_save that names no save in the file is dropped.
  expect(world.load_save ?? "").toBe("");
});

test("secrets in a file are stripped again on import, and the password warning follows the format file", async ({ page, foreman }) => {
  await createAdmin(page);
  const dirty = JSON.stringify({ name: "Dirty", description: "keep", game_password: secrets.game, password: secrets.pass, token: secrets.token, username: "x" });
  await upload(page, worldFile({ format: { game_password_set: true }, extra: [{ name: "config/server-settings.json", data: dirty }] }));
  await expect(page.locator("#world-file-password-warning")).toBeVisible();
  await page.locator("#world-file-download").uncheck();
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/crafted\?imported=1/);
  const text = readFileSync(join(worldDir(foreman, "crafted"), "config", "server-settings.json"));
  expect(secretsAnywhere(text)).toEqual([]);
  expect(JSON.parse(text.toString("utf8")).description).toBe("keep");
});

test("a staged file the form cannot confirm stays out of the address and goes when the world is made", async ({ page, foreman }) => {
  await createAdmin(page);
  await upload(page, worldFile());
  const [one] = staged(foreman);
  expect(one).toBeDefined();
  // The address and the page's links never name the staged file (17.17).
  expect(page.url()).not.toContain(one.replace(".partial-", ""));
  for (const href of await page.locator("a[href]").evaluateAll((as) => as.map((a) => a.getAttribute("href") ?? ""))) expect(href).not.toContain("worldfile-");
  await page.locator("#world-file-download").uncheck();
  await confirm(page).click();
  await expect(page).toHaveURL(/\/worlds\/crafted\?imported=1/);
  expect(staged(foreman)).toEqual([]);
  // The world page then shows the imported message (5.14).
  await expect(page.locator(".message--success")).toContainText(/imported/i);
  await checkPage(page);
});

test("a third staged file removes the oldest", async ({ page, foreman }) => {
  await createAdmin(page);
  for (const name of ["A one", "B two", "C three"]) {
    await upload(page, worldFile({ world: { name } }));
    await expect(nameField(page)).toHaveValue(name);
  }
  // At most two stay staged (5.14).
  expect(staged(foreman).length).toBeLessThanOrEqual(2);
  expect(staged(foreman).length).toBeGreaterThan(0);
});
