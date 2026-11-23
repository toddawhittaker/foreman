import { test, expect, checkPage, makeZip, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { closeSync, ftruncateSync, mkdirSync, openSync, readFileSync, readdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

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

  // The world's name leads, and the Saves tab says which page this is (DESIGN 6).
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Family base");
  await expect(page.getByRole("navigation", { name: "World pages" }).getByRole("link", { name: "Saves" })).toHaveAttribute("aria-current", "page");
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0).getByRole("link")).toHaveText("new.zip");
  await expect(rows.nth(0)).toContainText("Loads next");
  await expect(rows.nth(0).locator("time")).toHaveAttribute("datetime", "2026-11-03T14:25:00Z");
  await expect(rows.nth(0).locator("time")).toHaveText(/Nov 3.*\(.+ ago\)$/);
  await expect(rows.nth(0)).toContainText(`${newSave.length} B`);
  await expect(rows.nth(1).getByRole("link")).toHaveText("old.zip");
  await expect(rows.nth(1)).not.toContainText("Loads next");
  await expect(page.getByRole("navigation", { name: "World pages" }).getByRole("link", { name: "Overview" })).toHaveAttribute("href", "/worlds/family-base");
  await checkPage(page);
});

test("a world with no saves says so and still offers the upload", { tag: "@looks" }, async ({ page, foreman }) => {
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

test("an uploaded save appears, is kept byte for byte, and loads as the newest", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const upload = makeZip({ "level/level.dat0": "a map from elsewhere" });
  await page.getByLabel("Save file").setInputFiles({ name: "from elsewhere.zip", mimeType: "application/zip", buffer: upload });
  await page.getByRole("button", { name: "Upload save" }).click();

  await expect(page).toHaveURL(/\/worlds\/family-base\/saves\?uploaded=/);
  await expect(page.getByText("Done: Uploaded from elsewhere.zip.")).toBeVisible();
  // DESIGN 7.6: the reload puts focus on the result.
  await expect(page.locator(".message--success", { hasText: "Uploaded from elsewhere.zip." })).toBeFocused();
  await expect(page.locator("tbody tr")).toHaveCount(3);
  await expect(page.locator("tbody tr").first().getByRole("link")).toHaveText("from elsewhere.zip");
  expect(readFileSync(join(dir, "saves", "from elsewhere.zip")).equals(upload)).toBe(true);
  expect(readdirSync(join(dir, "saves")).sort()).toEqual(["from elsewhere.zip", "new.zip", "old.zip"]);
  await checkPage(page);
});

test("a duplicate name is refused and the save is untouched", { tag: "@looks" }, async ({ page, foreman }) => {
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

// The drop zone refuses a wrong type or a file over the limit before it is
// sent (DESIGN 7.10, in uploads.spec.ts), so the server's own checks (SPEC 14.6)
// are tried without script, where the field posts whatever it holds. The
// accessibility scan needs script; the duplicate-name test above scans a
// server error with it.
test.describe("the server's checks on a posted save", () => {
  test.use({ javaScriptEnabled: false });

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
    expect(readdirSync(join(dir, "saves")).sort()).toEqual(["new.zip", "old.zip"]);
  });

  test("an upload over the limit is refused before it is stored", async ({ page, foreman }) => {
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

    await expect(page.locator("#save-file-error")).toContainText("larger than the limit of 1 GB.");
    expect(readdirSync(join(dir, "saves")).sort()).toEqual(["new.zip", "old.zip"]);
  });
});

// SPEC 7.11: one list of saves; each row that does not load next has a
// "Load next" button, so an older save is one click away, and the newest
// row's button goes back to the newest save.
test("choosing which save loads is kept, and going back to the newest clears it", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const rows = page.locator("tbody tr");
  await expect(page.getByRole("radio")).toHaveCount(0);
  await expect(rows.nth(0)).toContainText("Loads next");
  await expect(rows.nth(0).getByRole("button")).toHaveCount(0);
  await page.getByRole("button", { name: "Load next: old.zip" }).click();

  await expect(page).toHaveURL(/saves\?chosen=1/);
  await expect(page.getByText("The server will load old.zip the next time it starts.")).toBeVisible();
  // DESIGN 7.6: the reload puts focus on the result.
  await expect(page.locator(".message--success", { hasText: "The server will load old.zip" })).toBeFocused();
  // SPEC 7.11: the choice applies once, and the page says so.
  await expect(page.getByText("The game loads old.zip at the next start, then the newest save again.")).toBeVisible();
  await expect(rows.nth(1)).toContainText("Loads next");
  await expect(rows.nth(1).getByRole("button")).toHaveCount(0);
  await expect(rows.nth(0)).not.toContainText("Loads next");
  expect(loadSaveOf(dir)).toBe("old.zip");
  // Everything else in world.json is kept.
  expect(JSON.parse(readFileSync(join(dir, "world.json"), "utf8"))).toMatchObject({ name: "Family base", game_version: "2.0.72", space_age: true });
  await checkPage(page);

  await page.getByRole("button", { name: "Load next: the newest save" }).click();
  await expect(page.getByText("will load the newest save the next time it starts")).toBeVisible();
  await expect(rows.nth(0)).toContainText("Loads next");
  expect(loadSaveOf(dir) ?? "").toBe("");
});

// Without JavaScript the row buttons are plain form posts.
test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });
  test("a row's Load next still chooses the save", { tag: "@looks" }, async ({ page, foreman }) => {
    await createAdmin(page);
    const dir = seedTwoSaves(foreman);
    await page.goto("/worlds/family-base/saves");
    await page.getByRole("button", { name: "Load next: old.zip" }).click();
    await expect(page.getByText("The server will load old.zip the next time it starts.")).toBeVisible();
    expect(loadSaveOf(dir)).toBe("old.zip");
  });
});

test("a chosen save that was removed by hand is explained", async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman, { load_save: "gone.zip" });
  await page.goto("/worlds/family-base/saves");
  await expect(page.getByText("The chosen save gone.zip is no longer in the saves folder")).toBeVisible();
  const newest = page.locator("tbody tr").nth(0);
  await expect(newest).toContainText("Loads next");
  await checkPage(page);
  // The stale choice can be cleared from the newest row (SPEC 7.11).
  await newest.getByRole("button", { name: "Load next: the newest save" }).click();
  await expect(page.getByText("will load the newest save the next time it starts")).toBeVisible();
  await expect(page.getByText("is no longer in the saves folder")).toHaveCount(0);
  await expect(newest).toContainText("Loads next");
  expect(loadSaveOf(dir) ?? "").toBe("");
});

test("a world a newer Foreman wrote can be downloaded from but not changed", async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman, { format_version: 9 });
  await page.goto("/worlds/family-base/saves");
  await expect(page.getByText("A newer version of Foreman wrote this world")).toBeVisible();
  await expect(page.getByRole("link", { name: "new.zip", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Upload save" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Load next/ })).toHaveCount(0);
  await checkPage(page);
});

test("a long list ends in Show older", { tag: "@looks" }, async ({ page, foreman }) => {
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
  // One list: each save's name appears once on the page, in its row.
  await expect(page.getByText("save-30.zip", { exact: true })).toHaveCount(1);
  await expect(page.getByRole("button", { name: /^Load next/ })).toHaveCount(49);
  await checkPage(page);
  await page.getByRole("link", { name: "Show older saves" }).click();
  await expect(page.locator("tbody tr")).toHaveCount(52);
  await expect(page.getByRole("link", { name: "Show older saves" })).toHaveCount(0);
});

// DESIGN 6, "The 320 px rules": a long save name wraps instead of widening the page.
test("a very long save name wraps in the table", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  const name = "autosave-with-a-very-long-name-".repeat(4) + ".zip";
  const older = "older-" + name;
  seedWorld(foreman, "longs", { name: "Long " + "w".repeat(80) }, [
    { name: older, content: oldSave, at: "2026-11-01T09:00:00Z" },
    { name, content: newSave, at: "2026-11-03T14:25:00Z" },
  ]);
  await page.goto("/worlds/longs/saves");
  await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: `Load next: ${older}` })).toBeVisible();
  await checkPage(page);
});

// SPEC 17.12: at 200% text on a phone the rows stack, keep their table
// roles, and the Load next button stays inside its panel.
test("at large text the rows stack and keep their roles", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");
  await page.addStyleTag({ content: "html { font-size: 200% }" });
  await expect(page.getByRole("table")).toBeVisible();
  await expect(page.getByRole("row")).toHaveCount(3);
  const button = page.getByRole("button", { name: "Load next: old.zip" });
  const panel = page.locator("section.panel").filter({ has: button });
  const b = (await button.boundingBox())!;
  const p = (await panel.boundingBox())!;
  expect(b.x + b.width).toBeLessThanOrEqual(p.x + p.width + 0.5);
  await checkPage(page);
});

// DESIGN 7.3: the success and note kinds are drawn, not left as plain text.
test("success and note messages have their own colours", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman, { load_save: "gone.zip" });
  await page.goto("/worlds/family-base/saves");
  const note = page.locator(".message--note");
  await expect(note).toBeVisible();
  const bg = (loc: typeof note) => loc.evaluate((el) => getComputedStyle(el).backgroundColor);
  const plain = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  const noteBg = await bg(note);
  expect(noteBg).not.toBe(plain);
  await page.getByRole("button", { name: "Load next: old.zip" }).click();
  const ok = page.locator(".message--success");
  await expect(ok).toBeVisible();
  expect(await bg(ok)).not.toBe(noteBg);
  expect(await bg(ok)).not.toBe(plain);
  await checkPage(page);
});

// DESIGN 7.2: a save that vanished between showing the page and choosing it
// is refused in words above the list, and the summary leads to that error.
test("choosing a save that has since gone is refused in words", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  const dir = seedTwoSaves(foreman);
  await page.goto("/worlds/family-base/saves");
  rmSync(join(dir, "saves", "old.zip"));
  await page.getByRole("button", { name: "Load next: old.zip" }).click();

  const msg = "Choose a save from the list. That save is no longer in the saves folder.";
  const error = page.locator("#load-save-error");
  await expect(error).toHaveText("Error: " + msg);
  const link = page.locator("#error-summary").getByRole("link", { name: msg });
  await expect(link).toHaveAttribute("href", "#load-save-error");
  await link.click();
  await expect(error).toBeFocused();
  expect(loadSaveOf(dir) ?? "").toBe("");
  await checkPage(page);
});

// DESIGN 8.3: the Load next buttons share one column, so they line up
// whatever the lengths of the names.
test("the Load next buttons line up when names differ in length", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "aligned", { name: "Aligned" }, [
    { name: "a.zip", content: oldSave, at: "2026-11-01T09:00:00Z" },
    { name: "a-much-longer-save-name.zip", content: oldSave, at: "2026-11-02T09:00:00Z" },
    { name: "newest.zip", content: newSave, at: "2026-11-03T14:25:00Z" },
  ]);
  await page.goto("/worlds/aligned/saves");
  const left = async (name: string) => (await page.getByRole("button", { name: `Load next: ${name}` }).boundingBox())!.x;
  expect(Math.abs((await left("a.zip")) - (await left("a-much-longer-save-name.zip")))).toBeLessThanOrEqual(1);
  await checkPage(page);
});

// SPEC 17.2, 22.7: with the button in its own column, a phone still shows
// the whole row without a sideways scroll.
test.describe("at phone width", () => {
  test.use({ viewport: { width: 320, height: 700 } });
  test("the table with its button column fits", { tag: "@looks" }, async ({ page, foreman }) => {
    await createAdmin(page);
    seedTwoSaves(foreman);
    await page.goto("/worlds/family-base/saves");
    await expect(page.getByRole("button", { name: "Load next: old.zip" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    // A row with no button has no empty line under it.
    await expect(page.locator("tbody tr").nth(0).locator("td.save-load")).toHaveCSS("display", "none");
    await checkPage(page);
  });

  // A world a newer Foreman wrote has no button column, so the time stays
  // beside the name under its own header.
  test("a newer world's rows keep their time beside the name", { tag: "@looks" }, async ({ page, foreman }) => {
    await createAdmin(page);
    seedTwoSaves(foreman, { format_version: 9 });
    await page.goto("/worlds/family-base/saves");
    const row = page.locator("tbody tr").nth(0);
    await expect(row.locator("td.save-load")).toHaveCount(0);
    const name = (await row.locator("td").nth(0).boundingBox())!;
    const saved = (await row.locator("td").nth(2).boundingBox())!;
    expect(saved.y).toBeLessThan(name.y + name.height);
    expect(saved.x).toBeGreaterThan(name.x + name.width - 1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  });
});

// An autosave reads as the world's name and its time, with the file name as
// smaller text; a save the person named keeps its own name (SPEC 5.11).
function seedAutosaves(f: Foreman, meta: Record<string, unknown> = {}) {
  return seedWorld(f, "family-base", { name: "Family base", ...meta }, [
    { name: "named.zip", content: oldSave, at: "2026-11-01T09:00:00Z" },
    { name: "_autosave1.zip", content: oldSave, at: "2026-11-03T14:00:05Z" },
    { name: "_autosave2.zip", content: newSave, at: "2026-11-03T14:25:07Z" },
  ]);
}

test("autosaves show the world's name and time, newest first, and keep the file name beside them", async ({ page, foreman }) => {
  await createAdmin(page);
  seedAutosaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(3);
  const newest = rows.nth(0);
  const link = newest.locator('a[href$="/saves/_autosave2.zip"]');
  await expect(link).toContainText("Family base, ");
  // What is drawn, not just the text: the name, a comma and a space, then the time on the same line.
  expect(await link.innerText()).toMatch(/^Family base, \S/);
  await expect(link.locator("time")).toHaveAttribute("datetime", "2026-11-03T14:25:07Z");
  await expect(newest.locator(".save-file")).toHaveText("_autosave2.zip");
  await expect(newest).toContainText("Loads next");
  await expect(rows.nth(1).locator(".save-file")).toHaveText("_autosave1.zip");
  // A save with a name of its own is not renamed, and the Load next button still names its file.
  await expect(rows.nth(2).getByRole("link")).toHaveText("named.zip");
  await expect(rows.nth(2).locator(".save-file")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Load next: _autosave1.zip" })).toBeVisible();
  await expect(page.locator("#saves-time-hint")).toContainText("last changed on disk");
  await checkPage(page);
});

test("an autosave downloads under the world's name and its save time", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedAutosaves(foreman);
  await page.goto("/worlds/family-base/saves");

  const [download] = await Promise.all([page.waitForEvent("download"), page.locator('a[href$="/saves/_autosave2.zip"]').click()]);
  expect(download.suggestedFilename()).toBe("Family_base-2026-11-03-14-25-07UTC.zip");
  expect(readFileSync(await download.path()).equals(newSave)).toBe(true);
});

// #659, DESIGN 8.3: the file name is on its own line under the label, so every
// file name starts at the label's left edge however long the label is. The
// project's viewport sets desktop or 320 px.
test("every autosave's file name starts at the left edge of its label, on a line of its own", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  // Times with one and two digits in the hour, so the labels differ in width.
  seedWorld(foreman, "family-base", { name: "Family base" }, [
    { name: "_autosave1.zip", content: oldSave, at: "2026-11-03T08:05:05Z" },
    { name: "_autosave2.zip", content: newSave, at: "2026-11-03T10:37:07Z" },
    { name: "_autosave3.zip", content: newSave, at: "2026-11-03T22:48:09Z" },
    { name: "named.zip", content: oldSave, at: "2026-11-01T09:00:00Z" },
  ]);
  await page.goto("/worlds/family-base/saves");
  const rows = page.locator("tbody tr");
  await expect(rows).toHaveCount(4);
  const labels = new Set<number>();
  for (const name of ["_autosave1.zip", "_autosave2.zip", "_autosave3.zip"]) {
    const row = rows.filter({ has: page.locator(`a[href$="/saves/${name}"]`) });
    const link = row.locator("a").first();
    const file = row.locator(".save-file");
    await expect(file).toHaveText(name);
    const l = (await link.boundingBox())!;
    const f = (await file.boundingBox())!;
    expect(Math.abs(f.x - l.x), `${name} starts at the label's left edge`).toBeLessThanOrEqual(0.5);
    expect(f.y, `${name} is under the label`).toBeGreaterThanOrEqual(l.y + l.height - 0.5);
    // The words' own width, summed over their lines, since the label's box may span its cell.
    labels.add(await row.locator(".save-label").evaluate((el) => {
      const r = document.createRange();
      r.selectNodeContents(el);
      return Math.round([...r.getClientRects()].reduce((sum, x) => sum + x.width, 0));
    }));
  }
  // On a desktop the labels really are different widths, so the check above could fail
  // (at 320 px a label wraps to the column's width).
  if ((page.viewportSize()?.width ?? 1280) > 600) expect(labels.size).toBeGreaterThan(1);
  // The label's digits are tabular, as in the other time cells.
  expect(await rows.first().locator(".save-label").evaluate((el) => getComputedStyle(el).fontVariantNumeric)).toContain("tabular-nums");
  // The badge is never between the label and the file name.
  const badge = (await rows.first().locator(".badge").boundingBox())!;
  const lastFile = (await rows.first().locator(".save-file").boundingBox())!;
  expect(badge.y + badge.height).toBeLessThanOrEqual(lastFile.y + 0.5);
  await checkPage(page);
});

test.describe("autosave rows at phone width", () => {
  test.use({ viewport: { width: 320, height: 700 } });
  test("a long world name and 200% text still fit", { tag: "@looks" }, async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman, "long-world", { name: "Long " + "w".repeat(60) }, [
      { name: "_autosave1.zip", content: oldSave, at: "2026-11-03T14:00:05Z" },
      { name: "_autosave2.zip", content: newSave, at: "2026-11-03T14:25:07Z" },
    ]);
    await page.goto("/worlds/long-world/saves");
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    await expect(page.getByRole("button", { name: "Load next: _autosave1.zip" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await checkPage(page);
  });
});
