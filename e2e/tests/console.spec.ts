import { test, expect, checkPage, createAdmin, seedWorld, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// The Console tab (SPEC 8.4, 8.6) against the fake game.

const container = "foreman-base";
// The monitor polls Docker every 5 seconds, so a change it has to notice can take that long.
const live = { timeout: 20_000 };

// The audit log's console rows, oldest first.
function consoleAudit(f: Foreman): { username: string; world_id: string; detail: string }[] {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return db.prepare("SELECT username, world_id, detail FROM audit_log WHERE action = 'console' ORDER BY id").all() as never;
  } finally {
    db.close();
  }
}

async function commands(g: FakeGame): Promise<string[]> {
  return (await g.state()).containers.find((c) => c.name === container)?.commands ?? [];
}

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: { running?: boolean } = {}) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  if (opts.running ?? true) {
    // Through the form post, so this works with JavaScript off too.
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("state--running");
  }
  await page.goto("/console");
  await expect(page.getByRole("link", { name: "Console" })).toHaveAttribute("aria-current", "page");
}

const atBottom = (box: Locator) => box.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight <= 4);
const logBox = (page: Page) => page.getByRole("region", { name: "Game log lines" });
const history = (page: Page) => page.getByRole("region", { name: "Console history" });

async function send(page: Page, command: string) {
  await page.getByLabel("Command").fill(command);
  await page.getByLabel("Command").press("Enter");
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("the Console tab opens, and the log follows the game only while at the bottom", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Console");
    await expect(page.getByRole("link", { name: "Console" })).toHaveAttribute("aria-current", "page");
    await checkPage(page);

    const log = logBox(page);
    // The page was not reloaded for any of this.
    await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
    await fakeGame!.call("/join", { name: container, player: "first" });
    await expect(log).toContainText("first joined the game", live);
    for (let i = 0; i < 40; i++) await fakeGame!.call("/join", { name: container, player: `p${i}` });
    await expect(log).toContainText("p39 joined the game", live);
    await expect.poll(() => atBottom(log)).toBe(true);
    const jump = page.getByRole("button", { name: "Jump to newest" });
    await expect(jump).toBeHidden();
    // Its row is kept while hidden, so showing it moves nothing below the log.
    const consoleTop = () => page.locator("#console-panel").evaluate((el) => el.getBoundingClientRect().top + window.scrollY);
    const topBefore = await consoleTop();

    // Reading older lines: new ones arrive but the view stays where it is.
    const command = page.getByLabel("Command");
    await command.focus();
    await log.evaluate((el) => (el.scrollTop = 0));
    await fakeGame!.call("/join", { name: container, player: "late" });
    await expect(log).toContainText("late joined the game", live);
    expect(await log.evaluate((el) => el.scrollTop)).toBe(0);
    await expect(jump).toBeVisible();
    // The button sits under the box and covers no line (DESIGN 8.5).
    const logRect = (await log.boundingBox())!;
    const jumpBox = (await jump.boundingBox())!;
    expect(jumpBox.y).toBeGreaterThanOrEqual(logRect.y + logRect.height);
    expect(await consoleTop()).toBe(topBefore);
    await expect(command).toBeFocused();
    await checkPage(page);

    await jump.click();
    await expect.poll(() => atBottom(log)).toBe(true);
    await expect(jump).toBeHidden();
    await expect(log).toBeFocused();
    await fakeGame!.call("/join", { name: container, player: "after" });
    await expect(log).toContainText("after joined the game", live);
    await expect.poll(() => atBottom(log)).toBe(true);
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
  });

  test("the page keeps at most 1,000 log lines", async ({ page, foreman, fakeGame }) => {
    test.setTimeout(90_000);
    await setUp(page, foreman, fakeGame!);
    for (let chunk = 0; chunk < 12; chunk++) {
      await Promise.all(Array.from({ length: 100 }, (_, i) => fakeGame!.call("/join", { name: container, player: `q${chunk * 100 + i}` })));
    }
    await expect(logBox(page)).toContainText(/q\d+ joined the game/, live);
    await expect.poll(() => page.locator(".log pre > span").count(), live).toBe(1000);
    // A reload shows the server's own copy, which holds the same limit.
    await page.reload();
    expect(await page.locator(".log pre > span").count()).toBeLessThanOrEqual(1000);
  });

  test("a command gets its reply, is announced, and is audited", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
    await send(page, "/players online");
    await expect(history(page)).toContainText("You sent /players online");
    await expect(history(page)).toContainText("Online players (0):");
    await expect(page.locator("#announcer")).toContainText("Reply to /players online: Online players (0):");
    // Script posted the form: no reload, the field is empty and ready for the next command.
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
    await expect(page.getByLabel("Command")).toHaveValue("");
    await expect(page.getByLabel("Command")).toBeFocused();

    // The Send button keeps the keyboard where it was, too.
    await page.getByLabel("Command").fill("/server-save");
    await page.getByRole("button", { name: "Send" }).click();
    await expect(history(page)).toContainText("Saving the map");
    await expect(page.getByRole("button", { name: "Send" })).toBeFocused();
    await expect(page.locator("#announcer")).toContainText("Reply to /server-save: Saving the map");
    await expect(history(page).locator("li")).toHaveCount(2);
    await checkPage(page);

    expect(await commands(fakeGame!)).toEqual(expect.arrayContaining(["/players online", "/server-save"]));
    expect(consoleAudit(foreman)).toEqual([
      { username: "admin", world_id: "base", detail: "/players online" },
      { username: "admin", world_id: "base", detail: "/server-save" },
    ]);
  });

  test("a command that sets a password is sent as typed and shown and recorded masked", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await send(page, "/config set password hunter2");
    await expect(history(page)).toContainText("/config set password ••••");
    await expect(history(page)).toContainText("The game sent no reply.");
    await expect(page.locator("#announcer")).toContainText("The game sent no reply to /config set password ••••");
    expect(await page.content()).not.toContain("hunter2");
    await checkPage(page);

    expect(await commands(fakeGame!)).toContain("/config set password hunter2");
    expect(consoleAudit(foreman)).toEqual([{ username: "admin", world_id: "base", detail: "/config set password ••••" }]);
    expect(foreman.logs()).not.toContain("hunter2");
  });

  test("an empty or over-long command is refused with the field's error", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await send(page, "   ");
    await expect(page.getByRole("heading", { name: /Fix this problem/ })).toBeVisible();
    await expect(page.locator("#command-error")).toHaveText("Error: Enter a command to send");
    await expect(page.getByLabel("Command")).toHaveAttribute("aria-invalid", "true");
    await checkPage(page);

    await send(page, "x".repeat(1001));
    await expect(page.locator("#command-error")).toHaveText("Error: Enter a command of at most 1,000 characters");
    await checkPage(page);
    expect(consoleAudit(foreman)).toEqual([]);
    expect(await commands(fakeGame!)).not.toContain("x".repeat(1001));
  });

  test("with the game stopped a command is refused in plain words", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { running: false });
    await send(page, "/players online");
    const message = page.locator(".message--error");
    await expect(message).toContainText("The game is not running, so it cannot take commands.");
    await expect(message.getByRole("link", { name: "Go to the Server page" })).toBeVisible();
    // Nothing was sent, so the field keeps the command to try again.
    await expect(page.getByLabel("Command")).toHaveValue("/players online");
    await checkPage(page);
    expect(consoleAudit(foreman)).toEqual([]);
  });

  test("when RCON cannot be reached the message offers the Server page", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await fakeGame!.call("/rcon-refuse", { name: container, on: true });
    await send(page, "/players online");
    const message = page.locator(".message--error");
    await expect(message).toContainText("could not reach the game over RCON");
    await expect(message.getByRole("link", { name: "Go to the Server page" })).toBeVisible();
    await checkPage(page);
  });

  test("a log stream that cannot connect is said, not silent", async ({ page, foreman, fakeGame }) => {
    await page.route("**/worlds/base/console/events", (route) => route.fulfill({ status: 503, body: "" }));
    await setUp(page, foreman, fakeGame!, { running: false });
    await expect(page.locator("#live-lost")).toHaveText("Note: Live updates stopped. Reload to see the current state.", live);
    await expect(page.locator("#announcer")).toHaveText("Live updates stopped. Reload to see the current state.");
    await checkPage(page);
  });
});

test.describe("without JavaScript", () => {
  test.use({ fakeGameOn: true, javaScriptEnabled: false });

  test("the log is drawn by the server and a command redirects to its reply", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await expect(logBox(page)).toContainText("Hosting game at");
    await send(page, "/players online");
    await expect(page).toHaveURL("/console#last-reply");
    await expect(page.locator("#last-reply")).toContainText("Online players (0):");
    await expect(page.getByRole("button", { name: "Jump to newest" })).toBeHidden();
  });
});
