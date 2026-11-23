import { test, expect, checkPage, createAdmin, seedWorld, makeZip, image21, live, type FakeGame, type Foreman } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { existsSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// The players of a save, and the total play time (SPEC 8.14, 8.15, 8.16): the list on the
// world's Overview page, the read of a copy of the save, the tracking from join and leave
// lines, and the Server page's play time and "N players in this save" link.
//
// The record is worlds/<id>/players.json (5.1). Tests that need exact figures write it
// themselves, for the save the world loads, so they do not depend on what the fake game's
// save read invents. Tests of the read check what is true of any read: the record on disk
// and the list on the page.

test.use({ fakeGameOn: true });

const hour = 3600 * 60; // game ticks in an hour

function worldDir(f: Foreman) {
  return join(f.dataDir, "worlds", "base");
}

function recordPath(f: Foreman) {
  return join(worldDir(f), "players.json");
}

function readRecord(f: Foreman) {
  return JSON.parse(readFileSync(recordPath(f), "utf8"));
}

interface SeededPlayer {
  name: string;
  hours?: number; // seeded_ticks, in hours
  trackedSeconds?: number;
  lastSeen?: string | null;
}

// Writes the players.json a good read of seedWorld's save would leave (SPEC 8.14), matching the
// save the world loads next so it counts as current. The basis carries the file's own modification
// time to the nanosecond, as the game folder has it.
function writeRecord(
  f: Foreman,
  opts: { players: SeededPlayer[]; gameTicks?: number; result?: string; inexact?: boolean; trackedSince?: string } ,
) {
  const save = join(worldDir(f), "saves", "base.zip");
  const st = statSync(save, { bigint: true });
  const ms = Number(st.mtimeNs / 1_000_000n);
  const frac = String(st.mtimeNs % 1_000_000_000n).padStart(9, "0").replace(/0+$/, "");
  const modified = new Date(ms).toISOString().replace(/\.\d+Z$/, frac ? `.${frac}Z` : "Z");
  const now = new Date().toISOString();
  const result = opts.result ?? "ok";
  const seeded = result === "ok";
  const record = {
    format_version: 1,
    basis: { save: "base.zip", size: Number(st.size), modified },
    as_of: now,
    seed: { at: opts.trackedSince ?? now, save: "base.zip", game_version: "2.1.24", result },
    tracked_since: opts.trackedSince ?? now,
    inexact: opts.inexact ?? false,
    game_ticks: opts.gameTicks ?? 0,
    players: opts.players.map((p) => ({
      name: p.name,
      seeded_ticks: seeded ? Math.round((p.hours ?? 0) * hour) : 0,
      tracked_ms: (p.trackedSeconds ?? 0) * 1000,
      last_seen: p.lastSeen ?? null,
    })),
  };
  writeFileSync(recordPath(f), JSON.stringify(record));
  return record;
}

// The world's Overview page and its players section, found by its heading so the test does not
// depend on the section's ids.
async function overview(page: Page) {
  await page.goto("/worlds/base");
  return playersSection(page);
}

function playersSection(page: Page): Locator {
  return page.locator("main section", { has: page.getByRole("heading", { level: 2, name: /players/i }) }).first();
}

// One entry per player, whether the partial shows a list or a table.
const entries = (section: Locator) => section.locator("li, tbody tr");

const readAgain = (page: Page) => page.getByRole("button", { name: /read again/i });
const readNewest = (page: Page) => page.getByRole("button", { name: /read players from the newest save/i });

// How many save reads the fake has been asked to start: every container it has been asked to
// create, less the game's own, which the test counts itself.
const creates = async (g: FakeGame) =>
  (await g.state()).calls.filter((c) => c.startsWith("POST ") && c.includes("/containers/create")).length;

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: Parameters<typeof seedWorld>[1] = {}) {
  await createAdmin(page);
  seedWorld(f, opts);
  await g.call("/image", { ref: image21, on: true });
}

async function start(page: Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main h1 .state")).toHaveText("Running", live);
}

test.describe("the list on the world's Overview page", () => {
  // SPEC 8.14, 8.16: while the world is stopped, a view with no record reads a copy of the save
  // by itself, and the result arrives on the page without a reload. The record is kept.
  test("a stopped world with no record reads its save and shows the players, live", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    expect(existsSync(recordPath(foreman))).toBe(false);
    const before = await creates(fakeGame!);

    const section = await overview(page);
    await expect(section).toBeVisible();
    await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));

    // The record lands on disk, the page follows it, and a read was one container.
    await expect.poll(() => existsSync(recordPath(foreman)), live).toBe(true);
    await expect.poll(() => entries(section).count(), live).toBeGreaterThan(0);
    expect(await creates(fakeGame!)).toBe(before + 1);
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);

    // The fake's canned read (fakegame.DefaultSavePlayers): game tick 59,760,005 (276 h 40 min),
    // alice online for 36,000,000 ticks (166 h 40 min) and bob for 216,000 (1 h).
    const record = readRecord(foreman);
    expect(record.format_version).toBe(1);
    expect(record.basis.save).toBe("base.zip");
    expect(record.seed.result).toBe("ok");
    expect(record.game_ticks).toBe(59_760_005);
    expect(record.players.map((p: { name: string; seeded_ticks: number; tracked_ms: number }) => [p.name, p.seeded_ticks, p.tracked_ms]).sort()).toEqual([
      ["alice", 36_000_000, 0],
      ["bob", 216_000, 0],
    ]);
    await expect(entries(section)).toHaveCount(2);
    const alice = entries(section).filter({ hasText: "alice" });
    const bob = entries(section).filter({ hasText: "bob" });
    await expect(alice).toContainText(/166\D+40/);
    await expect(bob).toContainText(/\b1\s*(h|hours?)\b/i);
    // Neither was seen by Foreman, so neither has a date, and both say their time is from before it looked.
    await expect(alice).toContainText(/before foreman/i);
    await expect(bob).toContainText(/before foreman/i);
    await expect(section.locator("time")).toHaveCount(0);
    // The total play time, 276 hours and 40 minutes, is on the panel and on the Server page.
    await expect(section).toContainText(/276\D+40/);
    await page.goto("/");
    await expect(page.locator("main")).toContainText(/276\D+40/);
    await expect(page.locator("main").getByRole("link", { name: /2 players in this save/i })).toBeVisible();
    await page.goBack();
    await expect(readAgain(page)).toBeVisible();
    await checkPage(page);
  });

  // SPEC 8.14: the list shows each player's play time, and the time a player had before the read
  // is told apart from time Foreman tracked.
  test("each player shows play time, and time before the read is called that", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, {
      gameTicks: 12 * hour,
      players: [
        { name: "alice", hours: 5 },
        { name: "bob", hours: 1, trackedSeconds: 1800, lastSeen: "2026-11-20T18:30:00Z" },
      ],
    });
    const before = await creates(fakeGame!);
    const section = await overview(page);
    await expect(entries(section)).toHaveCount(2);
    const alice = entries(section).filter({ hasText: "alice" });
    const bob = entries(section).filter({ hasText: "bob" });
    await expect(alice).toContainText(/5\s*(h|hours?)\b/i);
    await expect(alice).toContainText(/before foreman/i);
    // Bob was seen by Foreman: 1 hour seeded and 30 minutes tracked is 1 h 30 min, and the
    // date is a real one (never the save's own last_online, 8.14).
    // No \b after the minutes: the text that follows them in the same cell has no space before it.
    await expect(bob).toContainText(/1\s*(h|hours?)\b.*30\s*(m|min|minutes?)/i);
    await expect(bob).toContainText(/Nov 20/);
    await expect(bob.locator("time")).toHaveAttribute("datetime", "2026-11-20T18:30:00Z");
    // Alice was never seen by Foreman: no invented date.
    await expect(alice.locator("time")).toHaveCount(0);
    // A current, exact record is not read again by a view.
    await page.reload();
    await expect(entries(section)).toHaveCount(2);
    expect(await creates(fakeGame!)).toBe(before);
    await checkPage(page);
  });

  // SPEC 8.14: a read that failed leaves the list "tracked since" a date, with no earlier totals,
  // and is not tried again for the same save until the admin asks.
  test("a failed read says tracked since, and is not retried on its own", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await fakeGame!.call("/preview-mode", { mode: "oom" });
    const before = await creates(fakeGame!);

    const section = await overview(page);
    await expect.poll(() => existsSync(recordPath(foreman)), live).toBe(true);
    expect(readRecord(foreman).seed.result).toBe("out-of-memory");
    await expect(section).toContainText(/tracked this world since/i, live);
    await expect(section).not.toContainText(/before foreman/i);
    await expect(readAgain(page)).toBeVisible();
    expect(await creates(fakeGame!)).toBe(before + 1);

    // Views again, now that the fake would succeed: still no new read.
    await fakeGame!.call("/preview-mode", { mode: "ok" });
    await page.reload();
    await page.reload();
    await expect(section).toContainText(/tracked this world since/i);
    expect(await creates(fakeGame!)).toBe(before + 1);
    await checkPage(page);

    // "Read again" is the admin's way out, and a read that works replaces the failed record.
    await readAgain(page).click();
    await expect.poll(async () => (existsSync(recordPath(foreman)) ? readRecord(foreman).seed.result : ""), live).toBe("ok");
    await expect.poll(() => creates(fakeGame!), live).toBe(before + 2);
    await expect(section).not.toContainText(/tracked this world since/i, live);
    await expect(entries(section).first()).toBeVisible();
  });

  // SPEC 8.14 "Drift": a record that may be inexact is read again at the next view while the
  // world is stopped, with no click.
  test("an inexact record is read again at the next view", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { inexact: true, players: [{ name: "stale-player", hours: 2 }] });
    const before = await creates(fakeGame!);
    const section = await overview(page);
    await expect.poll(() => creates(fakeGame!), live).toBe(before + 1);
    await expect.poll(() => readRecord(foreman).inexact, live).toBe(false);
    await expect(section).not.toContainText("stale-player", live);
  });

  // SPEC 8.14, 8.16: the record matches only the save that loads next. A different newest save
  // makes the old record not match, and the view reads again.
  test("a record for another save does not match, and the view reads the newer save", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { players: [{ name: "old-save-player", hours: 3 }] });
    const record = readRecord(foreman);
    record.basis.size += 1; // the record was made for a save of another size
    writeFileSync(recordPath(foreman), JSON.stringify(record));
    const before = await creates(fakeGame!);
    const section = await overview(page);
    await expect.poll(() => creates(fakeGame!), live).toBe(before + 1);
    await expect(section).not.toContainText("old-save-player", live);
    await expect.poll(() => readRecord(foreman).basis.size, live).toBe(statSync(join(worldDir(foreman), "saves", "base.zip")).size);
  });

  // The page is for a phone as much as a desk (SPEC 17.1): a long name and many players do not
  // scroll the page sideways.
  test("a long name and a long list fit a phone", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    const players: SeededPlayer[] = [{ name: "a-very-long-player-name-with-many-parts-0123456789", hours: 250 }];
    for (let i = 0; i < 25; i++) players.push({ name: `player${i}`, hours: i + 1 });
    writeRecord(foreman, { players });
    await page.setViewportSize({ width: 320, height: 700 });
    const section = await overview(page);
    await expect(entries(section)).toHaveCount(players.length);
    await checkPage(page);
  });
});

// DESIGN 7 and SPEC 17.1: the panel passes axe at WCAG 2.2 AA (checkPage runs wcagTags) in each
// state it can be in, and a read that takes a while does not take the keyboard from the person.
// A "hang" preview never ends by itself, so a read stays "reading" until the world starts, which
// cancels it (SPEC 8.16).
test.describe("accessibility of the players panel", () => {
  // The note is not itself a live region: a poll swaps it every few seconds, and a live region
  // there would say it again each time. Its start and end are said in #save-players-live.
  const readingNote = (section: Locator) => section.getByText(/reading the newest save to find its players/i);
  const worldTab = (page: Page) => page.getByRole("navigation", { name: "World pages" }).getByRole("link", { name: "Saves" });
  const liveRegion = (page: Page) => page.locator("#save-players-live");
  const focusedId = (page: Page) => page.evaluate(() => document.activeElement?.id || document.activeElement?.tagName || "");

  test("the reading state passes axe", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await fakeGame!.call("/preview-mode", { mode: "hang" });
    const section = await overview(page);
    await expect(readingNote(section)).toBeVisible(live);
    await checkPage(page);
  });

  test("the ready state with a note that it may be inexact passes axe", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { inexact: true, gameTicks: 12 * hour, players: [{ name: "alice", hours: 5 }, { name: "bob", hours: 1, trackedSeconds: 600, lastSeen: "2026-11-20T18:30:00Z" }] });
    // The Server page's view before Start sees a stopped world with an inexact record, so it reads
    // again by itself (8.14). That read hangs and the start cancels it, so the record stays.
    await fakeGame!.call("/preview-mode", { mode: "hang" });
    // The game runs, so the inexact record is shown as it is and no read starts (D2).
    await start(page);
    const section = await overview(page);
    await expect(entries(section)).toHaveCount(2);
    await expect(section).toContainText(/inexact|exact|may/i);
    // A record exists, so the button is "Read again" (8.14: always there); "Read players from the
    // newest save" is its name only before the first read. It says the memory a read needs.
    await expect(readAgain(page)).toBeVisible();
    await expect(section).toContainText(/memory/i);
    await checkPage(page);
  });

  test("the tracked-only state passes axe", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { result: "mods-differ", trackedSince: "2026-11-10T08:00:00Z", players: [{ name: "carol", trackedSeconds: 5400, lastSeen: "2026-11-20T18:30:00Z" }] });
    const section = await overview(page);
    await expect(section).toContainText(/tracked this world since/i);
    await expect(readAgain(page)).toBeVisible();
    await checkPage(page);
  });

  test("the Server page with the players link passes axe", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { gameTicks: 59_760_005, players: [{ name: "alice", hours: 5 }, { name: "bob", hours: 2 }] });
    await page.goto("/");
    await expect(page.locator("main").getByRole("link", { name: /2 players in this save/i })).toBeVisible();
    await checkPage(page);
  });

  // A read can take minutes, and the panel asks again every few seconds. Neither the poll nor
  // the swap may put focus back on the "Reading" note: a person who has moved on keeps their place.
  test("during a read, focus is not put back on the Reading note", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { result: "out-of-memory", players: [{ name: "carol", trackedSeconds: 60 }] });
    await fakeGame!.call("/preview-mode", { mode: "hang" });
    const section = await overview(page);

    // The click is announced in the live region that no swap replaces, and takes no focus.
    await readAgain(page).click();
    await expect(readingNote(section)).toBeVisible(live);
    await expect(liveRegion(page)).toHaveText("Reading the newest save.");
    await expect(page.locator("#save-players-status")).not.toBeFocused();

    // The person moves on to another link. Two polls later (every 3 s) they are still there.
    await worldTab(page).focus();
    expect(await focusedId(page)).toBe("A");
    await page.waitForTimeout(7_500);
    await expect(readingNote(section)).toBeVisible();
    await expect(worldTab(page)).toBeFocused();
    // Nor does a page view that starts the read by itself take focus (no click).
    await page.reload();
    await expect(readingNote(section)).toBeVisible(live);
    await worldTab(page).focus();
    await page.waitForTimeout(4_000);
    await expect(worldTab(page)).toBeFocused();

    // Starting the world cancels the read (8.16); the test ends with nothing left running.
    await start(page);
  });

  // The button that was pressed is replaced when the read ends. The end is said once in the
  // live region that stays (A9: nothing takes focus), and the page is not left saying "Reading".
  test("when a read ends, its outcome is announced once", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { result: "out-of-memory", players: [{ name: "carol", trackedSeconds: 60 }] });
    await fakeGame!.call("/preview-mode", { mode: "slow" });
    const section = await overview(page);
    // Record every text the live region takes, so a repeated announcement is seen.
    await page.evaluate(() => {
      const w = window as unknown as { said: string[] };
      w.said = [];
      const el = document.querySelector("#save-players-live")!;
      new MutationObserver(() => w.said.push(el.textContent ?? "")).observe(el, { childList: true, characterData: true, subtree: true });
    });
    await readAgain(page).click();
    await expect.poll(async () => (existsSync(recordPath(foreman)) ? readRecord(foreman).seed.result : ""), live).toBe("ok");
    await expect(section).not.toContainText(/tracked this world since/i, live);
    await expect(readingNote(section)).toHaveCount(0, live);
    await expect(liveRegion(page)).toHaveText("The players of this save were read.", live);
    // Two more polls' time: the announcement is not made again, and focus has not been taken.
    await page.waitForTimeout(7_000);
    const said = await page.evaluate(() => (window as unknown as { said: string[] }).said.filter((t) => t !== ""));
    expect(said.filter((t) => /were read/.test(t))).toHaveLength(1);
    expect(["save-players-heading", "save-players-status"]).not.toContain(await focusedId(page));
    await expect(section.locator("#save-players-status")).toHaveCount(0);
  });
});

test.describe("while the game runs", () => {
  // SPEC 8.14, D2: a running world is never read by a view. The admin's click does it, and the
  // button says what it costs in memory.
  test("a view does not read, and the button does, after saying what it needs", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    // The Server page's view before Start is a view of a stopped world, so it starts a read by
    // itself (8.14). That read hangs, and the start cancels it, which leaves no record (8.16).
    await fakeGame!.call("/preview-mode", { mode: "hang" });
    await start(page);
    await fakeGame!.call("/preview-mode", { mode: "ok" });
    const before = await creates(fakeGame!);

    const section = await overview(page);
    await expect(readNewest(page)).toBeVisible();
    // The note names the memory a read needs, as much as the running game uses now.
    await expect(section).toContainText(/memory/i);
    await page.reload();
    await expect(readNewest(page)).toBeVisible();
    expect(existsSync(recordPath(foreman))).toBe(false);
    expect(await creates(fakeGame!)).toBe(before);
    await checkPage(page);

    await readNewest(page).click();
    await expect.poll(() => existsSync(recordPath(foreman)), live).toBe(true);
    await expect.poll(() => creates(fakeGame!), live).toBe(before + 1);
    await expect.poll(() => entries(section).count(), live).toBeGreaterThan(0);
    expect(readRecord(foreman).seed.result).toBe("ok");
  });

  // SPEC 8.14: after the first read, Foreman adds each session from the game's join and leave
  // lines. The player shows as online while joined, and has a last-seen date after leaving.
  test("a join and a leave are tracked into the list and the record", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { players: [{ name: "alice", hours: 5 }] });
    await start(page);
    const section = await overview(page);
    await expect(entries(section).filter({ hasText: "alice" })).toBeVisible();

    await fakeGame!.call("/join", { name: "foreman-base", player: "newcomer" });
    const newcomer = entries(section).filter({ hasText: "newcomer" });
    await expect(newcomer).toBeVisible(live);
    await expect(newcomer).toContainText(/online/i);
    // Alice is not online, and the one who is not there is not marked.
    await expect(entries(section).filter({ hasText: "alice" })).not.toContainText(/online/i);

    // Long enough for a whole second of tracked time to count.
    await page.waitForTimeout(1500);
    await fakeGame!.call("/leave", { name: "foreman-base", player: "newcomer" });
    await expect(newcomer).not.toContainText(/online/i, live);
    await expect(newcomer.locator("time")).toHaveCount(1);
    await expect.poll(() => readRecord(foreman).players.find((p: { name: string }) => p.name === "newcomer")?.last_seen ?? null, live).not.toBeNull();
    const kept = readRecord(foreman);
    expect(kept.players.find((p: { name: string }) => p.name === "newcomer").tracked_ms).toBeGreaterThanOrEqual(1000);
    // The seeded player's figures are untouched by anyone else's session.
    expect(kept.players.find((p: { name: string }) => p.name === "alice").seeded_ticks).toBe(5 * hour);
  });
});

test.describe("the Server page", () => {
  // SPEC 8.15, 8.14: a stopped world shows its total play time from the record, as hours and
  // minutes, and a link that counts the players and leads to the list.
  test("shows total play time and an N players in this save link to the list", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    // 276 hours and 40 minutes is tick 59,760,005 (what /time reports for that save).
    writeRecord(foreman, { gameTicks: 59_760_005, players: [{ name: "alice", hours: 5 }, { name: "bob", hours: 2 }, { name: "carol", hours: 1 }] });
    await page.goto("/");
    const main = page.locator("main");
    await expect(main).toContainText(/276\D+40/);
    const link = main.getByRole("link", { name: /3 players in this save/i });
    await expect(link).toBeVisible();
    await checkPage(page);

    await link.click();
    await expect(page).toHaveURL(/\/worlds\/base(\?|#|$)/);
    await expect(entries(playersSection(page))).toHaveCount(3);
  });

  // SPEC 8.15: with no read yet the page does not invent a time or a count.
  test("shows no invented figures without a record", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    // The view before Start reads by itself while the world is stopped (8.14). That read hangs,
    // and the start cancels it, so no record is written (8.16).
    await fakeGame!.call("/preview-mode", { mode: "hang" });
    await start(page);
    await page.goto("/");
    // The game runs, so nothing reads the save by itself.
    expect(existsSync(recordPath(foreman))).toBe(false);
    await expect(page.locator("main").getByRole("link", { name: /\d+ players? in this save/i })).toHaveCount(0);
    await checkPage(page);
  });

  // SPEC 8.15: while the game runs, play time comes from the game's own /time over RCON, never
  // from Lua.
  test("a running world asks the game for its time with /time, never Lua", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { gameTicks: 10 * hour, players: [{ name: "alice", hours: 5 }] });
    await start(page);
    await page.goto("/");
    // The fake answers /time with the time since the game started, so it may be seconds only.
    await expect(page.locator("main")).toContainText(/\d+\s*(h|hours?|min|minutes?|s|seconds?)\b/i);
    await expect.poll(async () => (await fakeGame!.state()).containers.flatMap((c) => c.commands), live).toContain("/time");
    const commands = (await fakeGame!.state()).containers.flatMap((c) => c.commands);
    expect(commands.filter((c) => /^\/(c|sc|silent-command|command)\b/.test(c))).toEqual([]);
    // Never more than once a minute: reloading does not ask again.
    const asked = () => fakeGame!.state().then((s) => s.containers.flatMap((c) => c.commands).filter((c) => c === "/time").length);
    const once = await asked();
    await page.reload();
    await page.reload();
    expect(await asked()).toBe(once);
  });
});

test.describe("an upload", () => {
  // SPEC 8.14: an upload of a save removes the record, because its modification time could pass as
  // the game's own save.
  test("an uploaded save removes the record", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    writeRecord(foreman, { players: [{ name: "alice", hours: 5 }] });
    await page.goto("/worlds/base/saves");
    await page.getByLabel("Save file").setInputFiles({ name: "from-elsewhere.zip", mimeType: "application/zip", buffer: makeZip({ "level/level.dat0": "a map from elsewhere" }) });
    await page.getByRole("button", { name: "Upload save" }).click();
    await expect(page).toHaveURL(/\/worlds\/base\/saves\?uploaded=/);
    expect(existsSync(recordPath(foreman))).toBe(false);
  });
});
