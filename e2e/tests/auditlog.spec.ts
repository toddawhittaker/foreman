import { test, expect, checkPage, createAdmin, mainNav, pickLanguage, seedWorld } from "./foreman.js";
import type { Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// SPEC 12.9 and 17.11, DESIGN 8.15: the Audit log tab. It lists the log newest
// first, 50 to a page, with an Older link, filters by user, action and world,
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

const rows = (page: Page) => page.locator("#audit-list > li");

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

  const start = page.locator("#audit-list > li", { hasText: "Server started" });
  await expect(start.locator(".badge")).toHaveText("Claude");
  await expect(start.getByRole("link", { name: "Base" })).toHaveAttribute("href", "/worlds/base");
  const time = start.locator("time");
  await expect(time).toHaveAttribute("datetime", /Z$/);
  await expect(time).toContainText(/\b(EST|EDT)\b/);
  await expect(time).toHaveText(/\((now|\d+ seconds? ago)\)$/);

  const odd = page.locator("#audit-list > li", { hasText: "frobnicate" });
  await expect(odd).toContainText("gone-world (deleted)");
  await expect(odd.getByRole("link")).toHaveCount(0);
  await expect(odd).toContainText(`<img src=x onerror="window.__pwned=1"> & "q"`);
  await expect(page.locator("#audit-list img")).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBeUndefined();

  await expect(page.locator("#audit-list > li", { hasText: "Console command" })).toContainText("/c game.print(***)");
  const tidy = page.locator("#audit-list > li", { hasText: "Autosaves tidied" });
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

test("an empty log says so", async ({ page, foreman }) => {
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
