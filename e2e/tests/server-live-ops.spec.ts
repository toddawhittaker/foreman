import { test, expect, checkPage, createAdmin, seedWorld, image21, live, pickLanguage, type FakeGame } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import type { Page } from "@playwright/test";

// The Server page's live operations (Epic 9): player actions from the
// players card (SPEC 8.2, DESIGN 8.12), the crash reason in plain words
// (SPEC 8.7), and the restart schedule's next time, wait and skip (SPEC
// 7.12, DESIGN 8.15). Every project runs these, at 320 px and at desktop
// width, in both themes. The low-disk warning (SPEC 8.13, DESIGN 8.17)
// needs a full disk, so its wording and region are covered by the Go
// render tests instead.

test.use({ fakeGameOn: true });

const container = "foreman-base";
const row = (page: Page, name: string) => page.locator(`#pd-${name}`);

async function commands(g: FakeGame): Promise<string[]> {
  return (await g.state()).containers.find((c) => c.name === container)?.commands ?? [];
}

async function running(page: Page, f: Parameters<typeof seedWorld>[0], g: FakeGame, players: string[] = []) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
  await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("state--running");
  for (const player of players) await g.call("/join", { name: container, player });
  await page.goto("/");
  for (const player of players) await expect(row(page, player)).toBeVisible(live);
}

// The schedule's time, in UTC, at least `ahead` seconds from now, as the
// form takes it (HH:MM).
function minuteAhead(ahead = 8): string {
  const at = new Date(Date.now() + ahead * 1000 + 60_000);
  return `${String(at.getUTCHours()).padStart(2, "0")}:${String(at.getUTCMinutes()).padStart(2, "0")}`;
}

async function schedule(page: Page, time: string, limitHours: number) {
  const resp = await page.request.post("/settings/restart-schedule", {
    form: { enabled: "on", time, zone: "UTC", limit_hours: String(limitHours) },
    maxRedirects: 0,
  });
  expect(resp.status()).toBe(303);
}

test("make an online player admin in two clicks, then undo it in one more", async ({ page, foreman, fakeGame }) => {
  await running(page, foreman, fakeGame!, ["alice"]);
  await checkPage(page);
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.locator("#ps-alice").click();
  await expect(row(page, "alice")).toHaveAttribute("open", "");
  // The order is the design's: the change that acts at once, then the two that confirm.
  await expect(row(page, "alice").locator(".actions").locator("a, button")).toHaveText([
    "Make admin alice",
    "Kick alice",
    "Ban alice",
  ]);
  await checkPage(page);
  await page.getByRole("button", { name: "Make admin alice" }).click();

  await expect(page).toHaveURL("/");
  await expect(page.locator("#player-done")).toContainText("alice is now an admin of Base.");
  await expect(page.locator("#player-done")).toBeFocused();
  await expectWithin(clicks, 2, "Make an online player admin");
  expect(await commands(fakeGame!)).toContain("/promote alice");
  // The row reads the admin list the game wrote: the mark, and the opposite change.
  await expect(page.locator("#ps-alice .badge")).toHaveText("Admin");
  await expect(page.locator("#pa-demote-alice")).toHaveCount(1);
  await checkPage(page);

  await page.getByRole("button", { name: "Undo: remove alice as admin" }).click();
  await expect(page.locator("#player-done")).toContainText("alice is no longer an admin of Base.");
  await expectWithin(clicks, 3, "Make a player admin, then undo it");
  expect(await commands(fakeGame!)).toContain("/demote alice");
  await page.locator("#ps-alice").click();
  await expect(page.getByRole("button", { name: "Make admin alice" })).toBeVisible();
  await checkPage(page);
  // The result shows once: a reload drops it.
  await page.reload();
  await expect(page.locator("#player-done")).toHaveCount(0);
});

test("kick and ban confirm and come back to the Server page, three clicks each", async ({ page, foreman, fakeGame }) => {
  await running(page, foreman, fakeGame!, ["alice", "bob"]);
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.locator("#ps-alice").click();
  await page.getByRole("link", { name: "Kick alice" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Kick alice from Base?");
  await checkPage(page);
  await page.getByRole("button", { name: "Kick alice" }).click();
  await expect(page).toHaveURL("/");
  await expect(page.locator("#player-done")).toContainText("alice was kicked from Base.");
  await expectWithin(clicks, 3, "Kick a player");
  expect((await commands(fakeGame!)).some((c) => c.startsWith("/kick alice"))).toBe(true);
  await checkPage(page);

  await clicks.reset();
  await page.locator("#ps-bob").click();
  await page.getByRole("link", { name: "Ban bob" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Ban bob from Base?");
  // Cancel goes back to the Server page.
  await expect(page.getByRole("link", { name: "Cancel, keep bob in the game" })).toHaveAttribute("href", "/");
  await page.getByRole("button", { name: "Ban bob" }).click();
  await expect(page).toHaveURL("/");
  await expect(page.locator("#player-done")).toContainText("bob is banned from Base.");
  await expect(page.getByRole("button", { name: "Unban bob" })).toBeVisible();
  await expectWithin(clicks, 3, "Ban a player");
  expect((await commands(fakeGame!)).some((c) => c.startsWith("/ban bob"))).toBe(true);
});

test("an open player row stays open and keeps focus while the list updates, and focus goes to the heading when the player leaves", async ({
  page,
  foreman,
  fakeGame,
}) => {
  await running(page, foreman, fakeGame!, ["alice"]);
  await page.locator("#ps-alice").click();
  await page.getByRole("link", { name: "Kick alice" }).focus();

  // Another player joins: the players region is swapped.
  await fakeGame!.call("/join", { name: container, player: "carol" });
  await expect(row(page, "carol")).toBeVisible(live);
  await expect(row(page, "alice")).toHaveAttribute("open", "");
  await expect(page.getByRole("link", { name: "Kick alice" })).toBeFocused();
  await expect(row(page, "carol")).not.toHaveAttribute("open", "");

  // The player whose row had focus leaves.
  await fakeGame!.call("/leave", { name: container, player: "alice" });
  await expect(row(page, "alice")).toHaveCount(0, live);
  await expect(page.locator("#players-heading")).toBeFocused();
});

// The longest words are Russian and German (docs/I18N.md): an open row, its
// admin mark and its buttons still fit 320 px at 200% text.
for (const lang of ["ru", "de"]) {
  test(`${lang}: an open player row and the next restart fit 320 px at 200% text`, async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!, ["alice"]);
    // The schedule's fact holds a time with its age inside a button face.
    await schedule(page, "04:00", 1);
    // A long name and the admin mark, so the summary wraps.
    await fakeGame!.call("/join", { name: container, player: "Maximilian_the_Engineer_of_Nauvis" });
    await page.locator("#ps-alice").click();
    await page.locator("#pa-promote-alice").click();
    await expect(page.locator("#player-done")).toBeVisible();
    await pickLanguage(page, lang);
    await expect(page.locator("html")).toHaveAttribute("lang", lang);
    await expect(row(page, "Maximilian_the_Engineer_of_Nauvis")).toBeVisible(live);
    await page.setViewportSize({ width: 320, height: 640 });
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    await page.locator("#ps-alice").click();
    await expect(row(page, "alice")).toHaveAttribute("open", "");
    await expect(page.locator("#act-schedule time")).toHaveAttribute("datetime", /T04:00:00Z$/);
    await expect(page.locator("#act-schedule")).toBeVisible();
    await checkPage(page);
  });
}

test("the whitelist buttons show only while the running game keeps the whitelist", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  // The switch is read at the start, so it is set before.
  await page.goto("/worlds/base/settings");
  await page.getByLabel("Only whitelisted players can join").check();
  await page.getByRole("button", { name: "Save whitelist enforcement" }).click();
  await expect(page.locator("#done-message")).toBeVisible();
  expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
  await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("state--running");
  await fakeGame!.call("/join", { name: container, player: "dave" });
  await page.goto("/");
  await expect(row(page, "dave")).toBeVisible(live);

  const clicks = await countClicks(page);
  await clicks.reset();
  await page.locator("#ps-dave").click();
  await page.getByRole("button", { name: "Add to whitelist dave" }).click();
  await expect(page.locator("#player-done")).toContainText("dave is on the whitelist of Base.");
  await expectWithin(clicks, 2, "Add an online player to the whitelist");
  await page.locator("#ps-dave").click();
  await expect(page.getByRole("button", { name: "Remove from whitelist dave" })).toBeVisible();
  await checkPage(page);
});

test("a game started without the whitelist offers no whitelist button", async ({ page, foreman, fakeGame }) => {
  await running(page, foreman, fakeGame!, ["erin"]);
  await page.locator("#ps-erin").click();
  await expect(row(page, "erin").getByRole("button", { name: /whitelist/ })).toHaveCount(0);
});

test("the crash notice names the reason in plain words, with no click", async ({ page, foreman, fakeGame }) => {
  await running(page, foreman, fakeGame!);
  // Every start now ends as the out-of-memory killer ends it.
  await fakeGame!.call("/oom", { name: container, on: true });
  await expect(page.locator(".crash-cause")).toHaveText("The game ran out of memory, so the system stopped it.", { timeout: 30_000 });
  await checkPage(page);
});

test("a start refused because the port is in use says so", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await fakeGame!.call("/port-in-use", { name: container, port: "34197" });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("#act-start")).toBeVisible(live);
  await expect(page.locator("main")).toContainText("The game did not start. Another program on this host already uses the game's port.", live);
  await checkPage(page);
});

test("the next scheduled restart shows while the game runs, as a time still to come", async ({ page, foreman, fakeGame }) => {
  await running(page, foreman, fakeGame!);
  await schedule(page, "04:00", 1);
  await page.goto("/");
  const next = page.locator("#act-schedule");
  await expect(page.locator(".schedule-next dt")).toHaveText("Next restart");
  await expect(next).toHaveAttribute("href", "/settings/restart-schedule");
  await expect(next.locator("time")).toHaveAttribute("datetime", /T04:00:00Z$/);
  // time.js says how far off it is (SPEC 17.11).
  await expect(next).toContainText(/\(in \d+ (hours?|minutes?)\)|\(tomorrow\)/);
  await checkPage(page);

  // Changing the schedule is one click from it, and the save the second.
  const clicks = await countClicks(page);
  await clicks.reset();
  await next.click();
  await expect(page).toHaveURL("/settings/restart-schedule");
  await page.getByRole("button", { name: "Save schedule" }).click();
  await expectWithin(clicks, 2, "Change the restart schedule while one is set");

  // A stopped game is never restarted, so the fact goes.
  expect((await page.request.post("/worlds/base/stop", { form: { countdown: "0" }, maxRedirects: 0 })).status()).toBe(303);
  await page.goto("/");
  await expect(page.locator("main h1 .state")).toHaveText("Stopped", live);
  await expect(page.locator("#act-schedule")).toHaveCount(0);
});

test("a scheduled restart waits while players are online, and the page shows it live", async ({ page, foreman, fakeGame }) => {
  test.setTimeout(120_000);
  await running(page, foreman, fakeGame!, ["alice"]);
  await schedule(page, minuteAhead(), 1);
  await page.goto("/");
  const note = page.locator("#schedule-waiting");
  await expect(note).toContainText("A scheduled restart is waiting for nobody to be online.", { timeout: 90_000 });
  await expect(note.getByRole("link", { name: "Restart schedule" })).toHaveAttribute("href", "/settings/restart-schedule");
  // Restart and the other controls stay, so the admin can still restart by hand.
  await expect(page.locator("#act-restart")).toBeVisible();
  await checkPage(page);
});

test("a scheduled restart with no wait is skipped while players are online, and says why", async ({ page, foreman, fakeGame }) => {
  test.setTimeout(120_000);
  await running(page, foreman, fakeGame!, ["alice"]);
  await schedule(page, minuteAhead(), 0);
  await page.goto("/");
  const note = page.locator("#schedule-skipped");
  await expect(note).toContainText("because players were still online.", { timeout: 90_000 });
  await expect(note).toContainText("The next restart is");
  expect((await commands(fakeGame!)).filter((c) => c.startsWith("/quit"))).toEqual([]);
  await checkPage(page);
});

test("the Chat tab's not-running note follows the game without a reload", async ({ page, foreman, fakeGame }) => {
  await running(page, foreman, fakeGame!);
  await page.goto("/chat");
  const note = page.locator("#chat-not-running");
  await expect(note).toHaveCount(1);
  await expect(note).toBeHidden();
  await checkPage(page);
  expect((await page.request.post("/worlds/base/stop", { form: { countdown: "0" }, maxRedirects: 0 })).status()).toBe(303);
  await expect(note).toBeVisible(live);
  await expect(note).toContainText("The server is not running, so no one can read chat.");
  await checkPage(page);
});
