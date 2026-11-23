import { test, expect, checkPage, createAdmin, seedWorld, image21, live, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { statSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// The Server page's Players panel (SPEC 8.1, 8.14, 16.3): every player of the save, online
// players first with an "Online" chip and their session, then the rest by play time, live.

test.use({ fakeGameOn: true });

const container = "foreman-base";
const hour = 3600 * 60; // game ticks in an hour

// Writes a players.json that counts as current for the save base loads (SPEC 8.14), so the
// fake game's own read does not replace it.
function writeRecord(f: Foreman, players: { name: string; hours: number }[]) {
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

const panel = (page: Page) => page.locator("main section[aria-labelledby=players-heading]");
const rows = (page: Page) => panel(page).locator("tbody tr");

// Counts every text the announcer takes from now on.
async function watchAnnouncer(page: Page) {
  await page.evaluate(() => {
    const w = window as unknown as { said: string[] };
    w.said = [];
    const a = document.getElementById("announcer")!;
    new MutationObserver(() => {
      if (a.textContent?.trim()) w.said.push(a.textContent.trim());
    }).observe(a, { childList: true, subtree: true, characterData: true });
  });
}
const said = (page: Page) => page.evaluate(() => (window as unknown as { said: string[] }).said);

test("online first with a chip, then by play time; a leave sorts again, live", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman, {});
  writeRecord(foreman, [
    { name: "carol", hours: 2 },
    { name: "bob", hours: 5 },
  ]);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");

  // Stopped: the same list, no chips.
  await expect(panel(page).getByRole("heading", { name: "Players", exact: true })).toBeVisible();
  await expect(rows(page)).toHaveCount(2, live);
  await expect(rows(page).nth(0)).toContainText("bob");
  await expect(rows(page).nth(0)).toContainText("5 h 0 min");
  await expect(rows(page).nth(0)).toContainText("Last seen");
  await expect(panel(page).locator(".badge--online")).toHaveCount(0);
  await expect(panel(page).locator("caption")).toHaveText("Players: 2 in this save");
  await checkPage(page);

  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main h1 .state")).toHaveText("Running", live);
  await watchAnnouncer(page);

  await fakeGame!.call("/join", { name: container, player: "carol" });
  await expect(rows(page).nth(0)).toContainText("carol", live);
  await expect(rows(page).nth(0).locator(".badge--online")).toHaveText("Online");
  // The session and the save's total play time, and a caption with both counts.
  await expect(rows(page).nth(0)).toContainText(/2 h 0 min total/);
  await expect(rows(page).nth(0).locator("td.num")).toHaveText(/^(less than 1 min|\d+ min)$/);
  await expect(panel(page).locator("caption")).toHaveText("Players: 1 online, 2 in this save");
  // The chip is beside the name, inside the actions button, before the actions open.
  await expect(rows(page).nth(0).locator("summary .badge--online")).toHaveText("Online");
  await expect(rows(page).nth(1)).toContainText("bob");
  await expect(rows(page).nth(1).locator(".badge--online")).toHaveCount(0);
  await expect.poll(() => said(page), live).toContain("carol joined.");
  await checkPage(page);

  await fakeGame!.call("/leave", { name: container, player: "carol" });
  await expect(panel(page).locator(".badge--online")).toHaveCount(0, live);
  await expect(rows(page).nth(0)).toContainText("bob");
  await expect(rows(page).nth(1)).toContainText("carol");
  await expect.poll(() => said(page), live).toContain("carol left.");
  // One sentence per change, not one per row that moved.
  expect((await said(page)).filter((s) => s.includes("carol"))).toEqual(["carol joined.", "carol left."]);
  await checkPage(page);
});

test("a save with no players shows the empty state", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman, {});
  writeRecord(foreman, []);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(panel(page).getByText("No one has played this world yet.")).toBeVisible(live);
  await expect(rows(page)).toHaveCount(0);
  await checkPage(page);
});

test("more than ten players link to the full list", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman, {});
  writeRecord(foreman, Array.from({ length: 12 }, (_, i) => ({ name: `p${String(i).padStart(2, "0")}`, hours: i + 1 })));
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(rows(page)).toHaveCount(10, live);
  await expect(rows(page).nth(0)).toContainText("p11");
  const all = panel(page).getByRole("link", { name: "All 12 players" });
  await expect(all).toHaveAttribute("href", "/worlds/base#save-players");
  await checkPage(page);
});

test("when a player leaves while focus is in their actions menu, focus goes to the panel heading and their row stays", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman, {});
  writeRecord(foreman, [{ name: "carol", hours: 2 }]);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main h1 .state")).toHaveText("Running", live);
  await fakeGame!.call("/join", { name: container, player: "carol" });
  await expect(rows(page).nth(0).locator(".badge--online")).toHaveText("Online", live);
  await page.locator("#ps-carol").click();
  await page.getByRole("link", { name: "Kick carol" }).focus();

  await fakeGame!.call("/leave", { name: container, player: "carol" });
  await expect(panel(page).locator(".badge--online")).toHaveCount(0, live);
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).nth(0)).toContainText("carol");
  await expect(page.locator("#players-heading")).toBeFocused();
  await checkPage(page);
});
