import { test, expect, createAdmin, seedWorld, seedSecond, image21, live, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { statSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Ruling 1 of fixes 20 (#985, #986, #988, #990, #991): in the Server area a
// control and its siblings keep their exact box, in x and y, when something
// else on the page appears, goes or changes, and the page does not scroll
// unless the person scrolls. Each test walks 1920 and 390 wide in both
// themes itself, so it runs once, in the first project.

test.use({ fakeGameOn: true });

const container = "foreman-base";
const widths = [
  { width: 1920, height: 1080 },
  { width: 390, height: 844 },
];
const themes = ["light", "dark"] as const;

async function each(page: Page, run: (label: string) => Promise<void>) {
  for (const size of widths) {
    await page.setViewportSize(size);
    for (const theme of themes) {
      await page.emulateMedia({ colorScheme: theme });
      await run(`${size.width} ${theme}`);
    }
  }
}

// Every match's box, in page coordinates, so a scroll would show too.
async function boxes(page: Page, selector: string) {
  return page.locator(selector).evaluateAll((els) =>
    els.map((el) => {
      const r = el.getBoundingClientRect();
      return { x: r.x + window.scrollX, y: r.y + window.scrollY, width: r.width, height: r.height };
    }),
  );
}

const scrollY = (page: Page) => page.evaluate(() => window.scrollY);

// Where a box starts and how wide it is: a cell or a row grows downwards
// with what opens in it, and its top-left corner and width must not move.
const corners = (bs: { x: number; y: number; width: number }[]) => bs.map(({ x, y, width }) => ({ x, y, width }));

// Writes a players.json that counts as current for the save base loads (SPEC
// 8.14), so a player online has a total under their name.
function writeRecord(f: Foreman, players: { name: string; hours: number }[]) {
  const hour = 3600 * 60;
  const dir = join(f.dataDir, "worlds", "base");
  const st = statSync(join(dir, "saves", "base.zip"), { bigint: true });
  const ms = Number(st.mtimeNs / 1_000_000n);
  const frac = String(st.mtimeNs % 1_000_000_000n).padStart(9, "0").replace(/0+$/, "");
  const modified = new Date(ms).toISOString().replace(/\.\d+Z$/, frac ? `.${frac}Z` : "Z");
  const now = new Date().toISOString();
  writeFileSync(
    join(dir, "players.json"),
    JSON.stringify({
      format_version: 1,
      basis: { save: "base.zip", size: Number(st.size), modified },
      as_of: now,
      seed: { at: now, save: "base.zip", game_version: "2.1.24", result: "ok" },
      tracked_since: now,
      inexact: false,
      game_ticks: players.reduce((n, p) => n + p.hours * hour, 0),
      players: players.map((p) => ({ name: p.name, seeded_ticks: p.hours * hour, tracked_ms: 0, last_seen: "2026-11-21T12:00:00Z" })),
    }),
  );
}

async function setUp(page: Page, foreman: Foreman, fakeGame: FakeGame) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

async function start(page: Page) {
  await page.goto("/");
  await page.locator("#act-start").click();
  await expect(page.locator("main h1 .state")).toHaveText("Running", live);
}

const plateControls = ".server-actions > *, .emergency > summary";

// @slow: it takes eight snapshots, one per width, theme and state.
test("the Server plate's buttons keep their place when a result comes, stopped and running", { tag: "@slow" }, async ({ page, foreman, fakeGame }) => {
  test.slow();
  await setUp(page, foreman, fakeGame!);
  for (const state of ["stopped", "running"]) {
    if (state === "running") await start(page);
    await each(page, async (label) => {
      await page.goto("/");
      await expect(page.locator("#act-snapshot")).toBeVisible(live);
      // A toast from the last round would not move anything, but it is closed
      // so this round's toast is the one waited for.
      await page.evaluate(() => document.querySelectorAll("#toasts > *").forEach((t) => t.remove()));
      const before = await boxes(page, plateControls);
      const y = await scrollY(page);
      await page.locator("#act-snapshot").click();
      await expect(page.locator("#toasts .toast--success")).toContainText("Foreman took the snapshot", live);
      await expect(page.locator("#act-snapshot")).toBeEnabled(live);
      expect(await boxes(page, plateControls), `${state} ${label}: the plate's controls`).toEqual(before);
      expect(await scrollY(page), `${state} ${label}: scroll`).toBe(y);
      // The result is a toast, never a box in the plate.
      await expect(page.locator(".plate .message--success")).toHaveCount(0);
    });
  }
});

test("Overview, Console and Chat put their headers in the same boxes", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  // A tab's own width follows its words' weight when current, so a tab is
  // held to its place and height.
  const parts = [".pagehead", ".pagehead__crumb", ".pagehead > h1", ".pagehead > nav", ".pagehead > nav a"];
  const place = (part: string, bs: { x: number; y: number; width: number; height: number }[]) => (part.endsWith(" a") ? bs.map(({ x, y, height }) => ({ x, y, height })) : bs);
  await each(page, async (label) => {
    const seen: Record<string, unknown> = {};
    for (const path of ["/", "/console", "/chat"]) {
      await page.goto(path);
      for (const part of parts) {
        const got = place(part, await boxes(page, part));
        expect(got.length, `${label} ${path}: ${part}`).toBeGreaterThan(0);
        if (seen[part] === undefined) seen[part] = got;
        else expect(got, `${label} ${path}: ${part} against the Overview`).toEqual(seen[part]);
      }
    }
  });
});

test("opening a player's actions moves nothing in their row", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman, {});
  writeRecord(foreman, [{ name: "carol", hours: 2 }]);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await start(page);
  await fakeGame!.call("/join", { name: container, player: "carol" });
  const row = "#server-players-table tbody tr:first-child";
  const parts = `${row} summary > :not(.visually-hidden)`;
  await each(page, async (label) => {
    await page.goto("/");
    await expect(page.locator(`${row} .player-actions__total`)).toContainText("total", live);
    // The click would scroll the row into view; that scroll is the test's, not the page's.
    await page.locator(row).scrollIntoViewIfNeeded();
    const before = await boxes(page, parts);
    const time = corners(await boxes(page, `${row} td.num`));
    const y = await scrollY(page);
    await page.locator(`${row} summary`).click();
    await expect(page.locator(`${row} details`)).toHaveAttribute("open", "");
    expect(await boxes(page, parts), `${label}: the row's name, chips and total`).toEqual(before);
    expect(corners(await boxes(page, `${row} td.num`)), `${label}: the time's cell`).toEqual(time);
    expect(await scrollY(page), `${label}: scroll`).toBe(y);
  });
});

test("the Console's find that matches nothing moves no control and no line", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await start(page);
  await fakeGame!.call("/join", { name: container, player: "first" });
  // The count's own box is its words; it floats, so it is left out.
  const parts = ".log-tools > :not(.log-tools__count), .terminal .log-frame > .log, #command, #send";
  await each(page, async (label) => {
    await page.goto("/console");
    await expect(page.locator(".log-tools")).toBeVisible();
    await expect(page.locator(".terminal .log pre > span").first()).toBeVisible(live);
    await page.locator("#log-find").fill("");
    const before = await boxes(page, parts);
    const y = await scrollY(page);
    await page.locator("#log-find").fill("no-such-line-anywhere");
    await expect(page.locator("#log-no-match")).toBeVisible();
    await expect(page.locator("#log-count")).not.toBeEmpty();
    expect(await boxes(page, parts), `${label}: the find bar, the log and the command row`).toEqual(before);
    expect(await scrollY(page), `${label}: scroll`).toBe(y);
  });
});

test("sending a command leaves the page where it is", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await start(page);
  await each(page, async (label) => {
    await page.goto("/console");
    await expect(page.locator(".log-tools")).toBeVisible();
    // The click would scroll Send into view; that scroll is the test's, not the page's.
    await page.locator("#send").scrollIntoViewIfNeeded();
    const before = await boxes(page, "#command, #send, .terminal .log-frame > .log");
    const y = await scrollY(page);
    await page.locator("#command").fill("/time");
    await page.locator("#send").click();
    await expect(page.locator("#last-reply")).toBeVisible(live);
    await expect(page.locator("#command")).toHaveValue("");
    expect(await boxes(page, "#command, #send, .terminal .log-frame > .log"), `${label}: the command row and the log`).toEqual(before);
    expect(await scrollY(page), `${label}: scroll`).toBe(y);
  });
});

test("stopped, Chat shows only that it is not running", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await each(page, async (label) => {
    await page.goto("/chat");
    await expect(page.locator("#chat-not-running")).toBeVisible();
    await expect(page.locator("#chat-panel"), `${label}: no form while stopped`).toBeHidden();
    await expect(page.locator(".chat-plate .log-frame"), `${label}: no log while stopped`).toBeHidden();
    await expect(page.locator(".chat-plate .empty:visible"), `${label}: one empty state`).toHaveCount(1);
  });
});

// A first line comes once per run, so each width and theme has a run of its own.
for (const size of widths) {
  for (const theme of themes) {
    test(`the first chat line moves nothing, at ${size.width} in ${theme}`, async ({ page, foreman, fakeGame }) => {
      await setUp(page, foreman, fakeGame!);
      await start(page);
      await page.setViewportSize(size);
      await page.emulateMedia({ colorScheme: theme });
      await page.goto("/chat");
      await expect(page.locator("#log-empty")).toBeVisible(live);
      await page.locator("#act-chat-send").scrollIntoViewIfNeeded();
      const parts = "#chat-message, #act-chat-send, .chat-plate .log";
      const before = await boxes(page, parts);
      const y = await scrollY(page);
      await fakeGame!.call("/join", { name: container, player: "alice" });
      await fakeGame!.call("/chat", { name: container, player: "alice", text: "first line" });
      await expect(page.locator(".chat-lines li")).toHaveCount(1, live);
      await expect(page.locator("#log-empty")).toHaveCount(0);
      expect(await boxes(page, parts), "the form and the log").toEqual(before);
      expect(await scrollY(page), "scroll").toBe(y);
    });
  }
}

test("the stopped plate's facts sit side by side with no stretch of nothing", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.setViewportSize(widths[0]);
  await page.goto("/");
  const cells = await boxes(page, ".facts-block .stat, .facts-block > :is([sse-swap=playtime], [sse-swap=loads], [sse-swap=schedule], [sse-swap=saveplayers]):visible");
  expect(cells.length).toBeGreaterThan(1);
  const row = cells.filter((c) => c.y === cells[0].y).sort((a, b) => a.x - b.x);
  for (let i = 1; i < row.length; i++) {
    expect(row[i].x - (row[i - 1].x + row[i - 1].width), `gap before cell ${i}`).toBeLessThanOrEqual(0.5);
  }
});

test("the Game log's title is set as every plate title; empty states draw their art at one size", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const look = (sel: string) =>
    page.locator(sel).first().evaluate((el) => {
      const s = getComputedStyle(el);
      return `${s.fontWeight} ${s.lineHeight} ${s.fontSize}`;
    });
  const art = (sel: string) =>
    page.locator(sel).first().evaluate((el) => {
      const s = getComputedStyle(el, "::before");
      return `${s.width} ${s.height}`;
    });
  await page.goto("/");
  const title = await look("#recent-snaps-heading");
  const players = await art(".recent-snaps .empty--art");
  await page.goto("/console");
  expect(await look("#log-heading"), "Game log").toBe(title);
  expect(await art("#console-panel .empty--art"), "the Console's empty art").toBe(players);
  await page.goto("/chat");
  expect(await art("#chat-not-running"), "Chat's empty art").toBe(players);
});

test("every Server confirmation uses the confirm box, the action last", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  seedSecond(foreman);
  await start(page);
  await fakeGame!.call("/join", { name: container, player: "carol" });
  await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("carol");
  for (const path of [
    "/worlds/base/stop",
    "/worlds/base/restart",
    "/worlds/base/force-stop",
    "/worlds/second/switch",
    "/worlds/base/players/kick?name=carol&return=/",
    "/worlds/base/players/ban?name=carol&return=/",
  ]) {
    await page.goto(path);
    await expect(page.locator("main section.confirm"), path).toHaveCount(1);
    await expect(page.locator("main form.panel"), `${path}: no plain form`).toHaveCount(0);
    const last = await page.locator("main section.confirm .actions").last().evaluate((el) => el.lastElementChild?.className ?? "");
    expect(last, `${path}: the action is last`).toMatch(/button--(primary|danger)/);
  }
});
