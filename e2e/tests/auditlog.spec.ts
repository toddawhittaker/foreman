import { test, expect, checkPage, createAdmin, mainNav, pickLanguage, seedWorld } from "./foreman.js";
import type { Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// SPEC 12.9 and 17.11, DESIGN 8.15: the Audit log tab. It lists the log newest
// first in a table of six fixed columns, 50 to a page (25, 100 or 200 by choice)
// with Newer and Older links, filters by user, action and world on one row,
// and shows each entry's facts.

test.use({ timezoneId: "America/New_York", locale: "en-US" });

interface Seed {
  time: number;
  username: string;
  action: string;
  world: string | null;
  detail: string;
  token: string;
}

// Writes entries straight into the database, as the real code would have.
function seed(f: Foreman, rows: Seed[]) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    const add = db.prepare("INSERT INTO audit_log (time, username, action, world_id, detail, token_name) VALUES (?, ?, ?, ?, ?, ?)");
    for (const r of rows) add.run(r.time, r.username, r.action, r.world, r.detail, r.token);
  } finally {
    db.close();
  }
}

function count(f: Foreman, where = "1 = 1") {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return Number((db.prepare(`SELECT count(*) AS n FROM audit_log WHERE ${where}`).get() as { n: number }).n);
  } finally {
    db.close();
  }
}

// 120 entries an hour from now, oldest first, so entry-119 is the newest, ahead of the entries the app records itself.
function manyEntries(): Seed[] {
  const base = Math.floor(Date.now() / 1000) + 3600;
  return Array.from({ length: 120 }, (_, i) => ({
    time: base + i,
    username: i % 2 === 0 ? "alice" : "bob",
    action: ["start", "stop", "console"][i % 3],
    world: i % 4 === 0 ? "base" : i % 4 === 1 ? "gone-world" : null,
    detail: `entry-${i}`,
    token: i % 5 === 0 ? "Claude" : "",
  }));
}

async function openAuditLog(page: Page) {
  await page.goto("/");
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "Audit log" }).click();
  await expect(page).toHaveURL("/settings/audit-log");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Audit log");
}

const rows = (page: Page) => page.locator("#audit-list tbody > tr");

test("the log pages newest first, 50 at a time, with an Older link", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seed(foreman, manyEntries());
  const total = count(foreman);
  await openAuditLog(page);
  await expect(page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "Audit log" })).toHaveAttribute("aria-current", "page");
  await checkPage(page);

  await expect(rows(page)).toHaveCount(50);
  await expect(rows(page).first()).toContainText("entry-119");
  await expect(rows(page).nth(1)).toContainText("entry-118");

  const seen: string[] = [];
  let pages = 0;
  for (;;) {
    pages++;
    for (const id of await rows(page).evaluateAll((els) => els.map((e) => e.id))) seen.push(id);
    const older = page.getByRole("link", { name: "Older entries" });
    if ((await older.count()) === 0) break;
    await older.click();
    await expect(page).toHaveURL(/before=\d+\.\d+/);
  }
  expect(pages).toBe(Math.ceil(total / 50));
  expect(seen).toHaveLength(total);
  expect(new Set(seen).size).toBe(total);
  await checkPage(page);
});

test("a page-size choice sets the page, and Newer and Older walk it both ways", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seed(foreman, manyEntries());
  const total = count(foreman);
  await openAuditLog(page);

  // The newest page has no Newer link; the default is 50.
  await expect(page.getByLabel("Entries per page")).toHaveValue("50");
  await expect(page.getByRole("link", { name: "Newer entries" })).toHaveCount(0);

  await page.getByLabel("Entries per page").selectOption("25");
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await expect(rows(page)).toHaveCount(25);
  const top = await rows(page).evaluateAll((els) => els.map((e) => e.id));
  await page.getByRole("link", { name: "Older entries" }).click();
  await expect(page).toHaveURL(/size=25/);
  await expect(page.getByLabel("Entries per page")).toHaveValue("25");
  await expect(rows(page)).toHaveCount(25);
  expect(await rows(page).evaluateAll((els) => els.map((e) => e.id))).not.toEqual(top);
  await checkPage(page);

  // Newer returns to the page above, at the same size.
  await page.getByRole("link", { name: "Newer entries" }).click();
  await expect(page).toHaveURL(/size=25/);
  expect(await rows(page).evaluateAll((els) => els.map((e) => e.id))).toEqual(top);
  await expect(page.getByRole("link", { name: "Newer entries" })).toHaveCount(0);

  // The last page has Newer and no Older, at each size.
  for (const size of ["100", "200"]) {
    await page.getByLabel("Entries per page").selectOption(size);
    await page.getByRole("button", { name: "Filter", exact: true }).click();
    await expect(rows(page)).toHaveCount(Math.min(Number(size), total));
    let pages = 1;
    while ((await page.getByRole("link", { name: "Older entries" }).count()) > 0) {
      await page.getByRole("link", { name: "Older entries" }).click();
      pages++;
    }
    expect(pages).toBe(Math.ceil(total / Number(size)));
    await expect(rows(page)).toHaveCount(total - (pages - 1) * Number(size));
    await expect(page.getByRole("link", { name: "Older entries" })).toHaveCount(0);
    if (pages > 1) await expect(page.getByRole("link", { name: "Newer entries" })).toBeVisible();
  }

  // A size that is not offered is the default.
  await page.goto("/settings/audit-log?size=7");
  await expect(rows(page)).toHaveCount(50);
  await expect(page.getByLabel("Entries per page")).toHaveValue("50");
});

test("on a wide screen the filters share one row and the table keeps six fixed columns", { tag: "@looks" }, async ({ page, foreman }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await createAdmin(page);
  seedWorld(foreman);
  seed(foreman, manyEntries());
  await openAuditLog(page);

  // The three filters, the size and the button sit on one row.
  const tops = await Promise.all(
    [page.getByLabel("User", { exact: true }), page.getByLabel("Action", { exact: true }), page.getByLabel("World", { exact: true }), page.getByLabel("Entries per page"), page.getByRole("button", { name: "Filter", exact: true })].map(
      async (l) => Math.round((await l.boundingBox())!.y),
    ),
  );
  expect(Math.max(...tops) - Math.min(...tops), `control tops ${tops}`).toBeLessThanOrEqual(8);

  // A real table: an accessible name, six column headers in order, six cells to a row.
  const table = page.getByRole("table", { name: "Entries" });
  await expect(table.getByRole("columnheader")).toHaveText(["Time", "User", "API token", "Action", "World", "Detail"]);
  await expect(table.locator("th[scope=col]")).toHaveCount(6);
  for (const row of await rows(page).all()) await expect(row.getByRole("cell")).toHaveCount(6);

  // Every column starts at the same place in every row, token or not.
  const lefts = await page.evaluate(() => {
    const headers = [...document.querySelectorAll<HTMLElement>("#audit-list thead th")].map((h) => Math.round(h.getBoundingClientRect().left));
    const bad: string[] = [];
    for (const tr of document.querySelectorAll("#audit-list tbody tr")) {
      [...tr.children].forEach((td, i) => {
        if (Math.round(td.getBoundingClientRect().left) !== headers[i]) bad.push(`${tr.id} column ${i}`);
      });
    }
    return bad;
  });
  expect(lefts).toEqual([]);

  // The text of the time, user, action and world cells share a baseline in each row.
  const crooked = await page.evaluate(() => {
    const bad: string[] = [];
    for (const tr of document.querySelectorAll("#audit-list tbody tr")) {
      const bottoms = [0, 1, 3, 4].map((i) => {
        const cell = tr.children[i];
        const walker = document.createTreeWalker(cell, NodeFilter.SHOW_TEXT, { acceptNode: (n) => (n.textContent!.trim() ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP) });
        const node = walker.nextNode();
        if (!node) return null;
        const r = document.createRange();
        r.selectNodeContents(node);
        return r.getClientRects()[0].bottom;
      }).filter((b): b is number => b !== null);
      if (Math.max(...bottoms) - Math.min(...bottoms) > 2) bad.push(`${tr.id}: ${bottoms.join(", ")}`);
    }
    return bad;
  });
  expect(crooked).toEqual([]);

  // A row is one line of text tall, not a block of stacked facts: many fit on a screen.
  expect(await rows(page).first().evaluate((el) => el.getBoundingClientRect().height)).toBeLessThan(90);
  await checkPage(page);
});

// Time, user and action stay whole on one line, and the detail takes what is left and wraps.
test("on a wide screen one long detail does not squeeze the other columns", { tag: "@looks" }, async ({ page, foreman }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await createAdmin(page);
  seedWorld(foreman);
  const now = Math.floor(Date.now() / 1000);
  seed(
    foreman,
    Array.from({ length: 12 }, (_, i) => ({
      time: now - 60 * (i + 1),
      username: "alice",
      action: "console",
      world: i % 3 === 0 ? "base" : null,
      detail: i === 5 ? "x".repeat(300) : `entry-${i}`,
      token: i % 4 === 0 ? "Claude" : "",
    })),
  );
  await openAuditLog(page);
  await expect(page.locator("#audit-list")).toHaveCSS("display", "table");

  for (const cls of ["audit-time", "audit-user", "audit-action", "audit-token", "audit-world"]) {
    const bad = await page.evaluate((c) => {
      const out: string[] = [];
      for (const td of document.querySelectorAll<HTMLElement>(`#audit-list td.${c}`)) {
        if (!td.textContent!.trim()) continue;
        // The text's own line boxes, not a badge's border box: one line has one top.
        const tops = new Set<number>();
        const walker = document.createTreeWalker(td, NodeFilter.SHOW_TEXT);
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          if (!n.textContent!.trim()) continue;
          const r = document.createRange();
          r.selectNodeContents(n);
          for (const x of r.getClientRects()) tops.add(Math.round(x.top));
        }
        if (tops.size > 1) out.push(`${td.parentElement!.id}: ${tops.size} lines`);
      }
      return out;
    }, cls);
    expect(bad, `${cls} cells that wrap`).toEqual([]);
  }

  // Every row but the one with the long detail is one line of text tall.
  // A short row is the yardstick, so the check holds for any font and line height.
  const heights = await rows(page).evaluateAll((trs) => trs.map((tr) => tr.getBoundingClientRect().height));
  const short = Math.min(...heights);
  expect(Number.isFinite(short) && short > 0).toBe(true);
  const tall = heights.filter((h) => h > short * 1.5);
  expect(tall.length, `tall rows ${heights}`).toBeLessThanOrEqual(1);

  // The long detail wraps inside its column; the page does not scroll sideways.
  const root = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(root.scroll).toBeLessThanOrEqual(root.client);
  await checkPage(page);
});

// The table layout holds down to the narrowest container that still gets it, even
// with the longest user and token names the app allows.
test("just above the stacking width, long names and a long detail do not scroll the page sideways", { tag: "@looks" }, async ({ page, foreman }) => {
  await page.setViewportSize({ width: 1000, height: 900 });
  await createAdmin(page);
  seedWorld(foreman);
  const now = Math.floor(Date.now() / 1000);
  seed(foreman, [
    { time: now - 30, username: "u".repeat(32), action: "start", world: "base", detail: "x".repeat(300), token: "T".repeat(32) },
    { time: now - 20, username: "alice", action: "console", world: null, detail: "short", token: "Claude" },
  ]);
  await openAuditLog(page);
  await expect(page.locator("#audit-list")).toHaveCSS("display", "table");
  const root = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(root.scroll).toBeLessThanOrEqual(root.client);
  // A table is pulled out past its parent by its end cells' padding, so a highlighted row keeps them clear of its edge (DESIGN 8.3).
  const table = await page.locator("#audit-list").evaluate((el) => ({ right: el.getBoundingClientRect().right, parent: el.parentElement!.getBoundingClientRect().right - parseFloat(getComputedStyle(el).marginRight) }));
  expect(table.right).toBeLessThanOrEqual(table.parent + 1);
  await checkPage(page);
});

// The longest English action label beside a long time, user, token and
// detail fits the table in its plate on a laptop-wide screen too, where the
// cells keep one line where they can (DESIGN 8.3).
for (const width of [1920, 1440, 1280, 1100]) {
  test(`at ${width} px the longest action label and long names keep the table inside its plate`, { tag: "@looks" }, async ({ page, foreman }) => {
    await page.setViewportSize({ width, height: 900 });
    await createAdmin(page);
    seedWorld(foreman);
    const now = Math.floor(Date.now() / 1000);
    seed(foreman, [
      { time: now - 30, username: "u".repeat(32), action: "api-set", world: "base", detail: `token="${"T".repeat(32)}" on=true ` + "x".repeat(120), token: "T".repeat(32) },
      { time: now - 20, username: "alice", action: "console", world: null, detail: "short", token: "Claude" },
    ]);
    await openAuditLog(page);
    await expect(page.locator("#audit-list")).toHaveCSS("display", "table");
    await expect(page.locator("#audit-list")).toContainText("REST API access turned on or off");
    const root = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
    expect(root.scroll).toBeLessThanOrEqual(root.client);
    const table = await page.locator("#audit-list").evaluate((el) => {
      const plate = el.closest(".panel")!.getBoundingClientRect();
      const r = el.getBoundingClientRect();
      return { inPlate: r.left >= plate.left && r.right <= plate.right, fitsParent: r.right <= el.parentElement!.getBoundingClientRect().right - parseFloat(getComputedStyle(el).marginRight) + 1 };
    });
    expect(table).toEqual({ inPlate: true, fitsParent: true });
  });
}

// Action labels run long in other languages (German's longest is 52 characters);
// they wrap between words instead of pushing the table past its container.
test("just above the stacking width, a long German action label does not scroll the page sideways", { tag: "@looks" }, async ({ page, foreman, context }) => {
  await page.setViewportSize({ width: 1000, height: 900 });
  await createAdmin(page);
  seedWorld(foreman);
  const now = Math.floor(Date.now() / 1000);
  seed(foreman, [
    { time: now - 30, username: "u".repeat(32), action: "save-upload-limit-set", world: "base", detail: "x".repeat(300), token: "T".repeat(32) },
    { time: now - 20, username: "alice", action: "public-mods-set", world: null, detail: "short", token: "Claude" },
  ]);
  await context.addCookies([{ name: "foreman_lang", value: "de", url: foreman.url }]);
  await page.goto("/settings/audit-log");
  await expect(page.locator("#audit-list")).toHaveCSS("display", "table");
  await expect(page.locator("#audit-list")).toContainText("Limit für das Hochladen");
  const root = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(root.scroll).toBeLessThanOrEqual(root.client);
  // A table is pulled out past its parent by its end cells' padding, so a highlighted row keeps them clear of its edge (DESIGN 8.3).
  const table = await page.locator("#audit-list").evaluate((el) => ({ right: el.getBoundingClientRect().right, parent: el.parentElement!.getBoundingClientRect().right - parseFloat(getComputedStyle(el).marginRight) }));
  expect(table.right).toBeLessThanOrEqual(table.parent + 1);
});

// At 1024 px (where the laptop-width rules start) German's long action labels
// wrap between words, and a user name is never broken inside the word; wider,
// the table still fits.
for (const width of [1024, 1280, 1440]) {
test(`at ${width} px in German nothing scrolls sideways and no user name breaks inside a word`, { tag: "@looks" }, async ({ page, foreman, context }) => {
  await page.setViewportSize({ width, height: 900 });
  await createAdmin(page);
  seedWorld(foreman);
  const now = Math.floor(Date.now() / 1000);
  seed(foreman, [
    { time: now - 30, username: "toddwhittaker", action: "save-upload-limit-set", world: "base", detail: "x".repeat(120), token: "Claude" },
    { time: now - 20, username: "toddwhittaker", action: "public-mods-set", world: null, detail: "short", token: "" },
  ]);
  await context.addCookies([{ name: "foreman_lang", value: "de", url: foreman.url }]);
  await page.goto("/settings/audit-log");
  await expect(page.locator("#audit-list")).toHaveCSS("display", "table");
  await expect(page.locator("#audit-list")).toContainText("Limit für das Hochladen");
  const root = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(root.scroll).toBeLessThanOrEqual(root.client);
  const table = await page.locator("#audit-list").evaluate((el) => ({ right: el.getBoundingClientRect().right, parent: el.parentElement!.getBoundingClientRect().right - parseFloat(getComputedStyle(el).marginRight) }));
  expect(table.right).toBeLessThanOrEqual(table.parent + 1);
  // A name with no space is one word, so it must fill one line of its cell.
  const lines = await page.locator("#audit-list .audit-user").evaluateAll((cells) =>
    cells.map((cell) => {
      const r = document.createRange();
      r.selectNodeContents(cell);
      return new Set([...r.getClientRects()].map((q) => Math.round(q.top))).size;
    }),
  );
  expect(lines.length).toBeGreaterThan(1);
  for (const n of lines) expect(n).toBe(1);
  // The action never runs past its cell, and at 1024 px may wrap between words.
  for (const cell of await page.locator("#audit-list .audit-action").all()) {
    if (width === 1024) await expect(cell).toHaveCSS("white-space", "normal");
    expect(await cell.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
  }
});
}


test("at 320 px the table stacks each row, with no sideways scroll", { tag: "@looks" }, async ({ page, foreman }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await createAdmin(page);
  seedWorld(foreman);
  const now = Math.floor(Date.now() / 1000);
  seed(foreman, [
    { time: now - 30, username: "a-very-long-user-name-here", action: "start", world: "base", detail: "x".repeat(120), token: "A-token-name-of-32-characters-long" },
    { time: now - 20, username: "carol", action: "frobnicate", world: "gone-world", detail: "/c game.print(" + "word ".repeat(30) + ")", token: "" },
    { time: now - 10, username: "system", action: "autosave-tidy", world: null, detail: "", token: "" },
  ]);
  await openAuditLog(page);
  const root = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(root.scroll).toBeLessThanOrEqual(root.client);
  // The column headers are for screen readers only; each value carries its label.
  await expect(page.getByRole("table", { name: "Entries" }).getByRole("columnheader")).toHaveCount(6);
  const withToken = rows(page).filter({ hasText: "Server started" });
  await expect(withToken.locator(".audit-user")).toBeVisible();
  const box = await withToken.boundingBox();
  expect(box!.width).toBeLessThanOrEqual(320);
  // An entry with no token shows no empty gap or label for one, but its
  // empty cell stays in the table, so a screen reader reads every cell under
  // its own column.
  const tidyRow = rows(page).filter({ hasText: "Autosaves tidied" });
  const tokenBox = await tidyRow.locator(".audit-token").boundingBox();
  expect(tokenBox === null || (tokenBox.width <= 1 && tokenBox.height <= 1)).toBe(true);
  for (const r of await rows(page).all()) await expect(r.getByRole("cell")).toHaveCount(6);
  // Compact: a short entry takes four lines or fewer, not a screen of stacked facts.
  const tidy = await rows(page).filter({ hasText: "Autosaves tidied" }).boundingBox();
  expect(tidy!.height).toBeLessThan(150);
  await checkPage(page);
});

test("filters by user, action and world, and clears them", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seed(foreman, manyEntries());
  await openAuditLog(page);

  const bobs = count(foreman, "username = 'bob'");
  await page.getByLabel("User", { exact: true }).selectOption("bob");
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await expect(page).toHaveURL(/user=bob/);
  await expect(rows(page)).toHaveCount(Math.min(bobs, 50));
  await expect(rows(page).filter({ hasText: "alice" })).toHaveCount(0);
  // The filter is in force again on the next page.
  await page.getByRole("link", { name: "Older entries" }).click();
  await expect(page.getByLabel("User", { exact: true })).toHaveValue("bob");
  await expect(rows(page)).toHaveCount(bobs - 50);
  await checkPage(page);

  await page.getByLabel("Action", { exact: true }).selectOption("start");
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await expect(rows(page)).toHaveCount(count(foreman, "username = 'bob' AND action = 'start'"));
  await expect(rows(page).filter({ hasNotText: "Server started" })).toHaveCount(0);

  await page.getByLabel("World", { exact: true }).selectOption("gone-world");
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await expect(rows(page)).toHaveCount(count(foreman, "username = 'bob' AND action = 'start' AND world_id = 'gone-world'"));
  await expect(page.locator("#audit-empty")).toHaveCount(0);
  await checkPage(page);

  await page.getByLabel("User", { exact: true }).selectOption("alice");
  await page.getByLabel("Action", { exact: true }).selectOption("stop");
  await page.getByLabel("World", { exact: true }).selectOption("gone-world");
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await expect(page.locator("#audit-empty")).toHaveText("No entries match these filters.");
  await checkPage(page);

  await page.getByRole("link", { name: "Clear filters" }).click();
  await expect(page).toHaveURL("/settings/audit-log");
  await expect(rows(page)).toHaveCount(50);
  await expect(page.getByLabel("User", { exact: true })).toHaveValue("");
});

test("an entry shows its time, user, token, action, world and escaped detail", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  const now = Math.floor(Date.now() / 1000);
  seed(foreman, [
    { time: now - 40, username: "carol", action: "start", world: "base", detail: "", token: "Claude" },
    { time: now - 30, username: "carol", action: "frobnicate", world: "gone-world", detail: `<img src=x onerror="window.__pwned=1"> & "q"`, token: "" },
    { time: now - 20, username: "carol", action: "console", world: "base", detail: "/c game.print(***)", token: "" },
    { time: now - 10, username: "system", action: "autosave-tidy", world: null, detail: "", token: "" },
  ]);
  await openAuditLog(page);

  const start = page.locator("#audit-list tbody > tr", { hasText: "Server started" });
  await expect(start.locator(".badge")).toHaveText("Claude");
  await expect(start.getByRole("link", { name: "Base" })).toHaveAttribute("href", "/worlds/base");
  const time = start.locator("time");
  await expect(time).toHaveAttribute("datetime", /Z$/);
  await expect(time).toContainText(/\b(EST|EDT)\b/);
  await expect(time).toHaveText(/\((now|\d+ seconds? ago)\)$/);

  const odd = page.locator("#audit-list tbody > tr", { hasText: "frobnicate" });
  await expect(odd).toContainText("gone-world (deleted)");
  await expect(odd.getByRole("link")).toHaveCount(0);
  await expect(odd).toContainText(`<img src=x onerror="window.__pwned=1"> & "q"`);
  await expect(page.locator("#audit-list img")).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBeUndefined();

  await expect(page.locator("#audit-list tbody > tr", { hasText: "Console command" })).toContainText("/c game.print(***)");
  const tidy = page.locator("#audit-list tbody > tr", { hasText: "Autosaves tidied" });
  await expect(tidy).toContainText("system");
  await expect(tidy.locator(".badge")).toHaveCount(0);
  await checkPage(page);
});

test("an action done in the app shows in the log at once", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/api-tokens");
  await page.getByLabel("Name").fill("Claude");
  await page.getByRole("button", { name: "Create token" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Token Claude created");
  await openAuditLog(page);
  const first = rows(page).first();
  await expect(first).toContainText("API token created");
  await expect(first).toContainText("admin");
  await expect(first).toContainText(`token="Claude"`);
  await checkPage(page);
});

test("an empty log says so", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  const db = new DatabaseSync(join(foreman.dataDir, "foreman.db"));
  db.exec("DELETE FROM audit_log");
  db.close();
  await openAuditLog(page);
  await expect(page.locator("#audit-empty")).toHaveText("Nothing has been recorded yet.");
  await expect(page.getByRole("link", { name: "Older entries" })).toHaveCount(0);
  await checkPage(page);
});

// Action words and labels follow the language, and the longer languages still fit (SPEC 22.7).
for (const [tag, title, started] of [
  ["de", "Audit-Protokoll", "Server gestartet"],
  ["ru", "Журнал аудита", "Сервер запущен"],
] as const) {
  test(`the log is translated: ${tag}`, async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seed(foreman, [{ time: Math.floor(Date.now() / 1000) - 5, username: "carol", action: "start", world: "gone-world", detail: "", token: "Claude" }]);
    await pickLanguage(page, tag);
    await page.goto("/settings/audit-log");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(title);
    await expect(rows(page).filter({ hasText: started }).first()).toContainText(started);
    await checkPage(page);
  });
}
