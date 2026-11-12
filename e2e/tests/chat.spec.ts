import { test, expect, checkPage, createAdmin, seedWorld, live, image21, pickLanguage, type Foreman, type FakeGame } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// The Chat tab (SPEC 8.5) against the fake game.

const container = "foreman-base";

function chatAudit(f: Foreman): { username: string; world_id: string; detail: string }[] {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return db.prepare("SELECT username, world_id, detail FROM audit_log WHERE action = 'chat' ORDER BY id").all() as never;
  } finally {
    db.close();
  }
}

async function commands(g: FakeGame): Promise<string[]> {
  return (await g.state()).containers.find((c) => c.name === container)?.commands ?? [];
}

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: { running?: boolean; visit?: boolean } = {}) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  if (opts.running ?? true) {
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("state--running");
  }
  if (opts.visit ?? true) {
    await page.goto("/chat");
    await expect(page.getByRole("link", { name: "Chat", exact: true })).toHaveAttribute("aria-current", "page");
  }
}

const log = (page: Page) => page.getByRole("log", { name: "Chat messages" });
const rows = (page: Page) => log(page).locator("pre > span");
const atBottom = (box: Locator) => box.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight <= 4);
const field = (page: Page) => page.getByLabel("Message", { exact: true });

// Sends a message and waits until its answer has been swapped in. The panel is
// replaced whole, field included, so typing the next message before then is
// lost, and an error left from the last refusal would pass for this one.
async function send(page: Page, message: string) {
  await field(page).fill(message);
  await page.locator("#chat-panel").evaluate((el) => el.setAttribute("data-sent", ""));
  await field(page).press("Enter");
  await expect(page.locator("#chat-panel[data-sent]")).toHaveCount(0);
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("the tab shows the history, follows the game live and sends as the server", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { visit: false });
    await fakeGame!.call("/join", { name: container, player: "alice" });
    await fakeGame!.call("/chat", { name: container, player: "alice", text: "is anyone near the oil field?" });
    // The history is in the page as it loads.
    await expect.poll(async () => (await page.request.get("/chat")).text(), live).toContain("is anyone near the oil field?");
    await page.goto("/chat");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^Base\s+Running$/);
    await expect(page.getByRole("navigation", { name: "Server pages" }).getByRole("link", { name: "Chat" })).toHaveAttribute("aria-current", "page");
    await expect(rows(page)).toHaveCount(1);
    await expect(rows(page).first()).toContainText("alice: is anyone near the oil field?");
    await expect(page.getByText("No chat since the server started.")).toHaveCount(0);
    await checkPage(page);

    // A new line arrives without reloading the page.
    // The first line is marked: it must still be the same node afterwards, so
    // the polite log never reads the history out again (DESIGN 8.14).
    await page.evaluate(() => {
      (window as unknown as { marker: number }).marker = 1;
      (document.querySelector(".log pre > span") as unknown as { marker: number }).marker = 1;
    });
    await fakeGame!.call("/chat", { name: container, player: "alice", text: "<b>on my way</b>" });
    await expect(log(page)).toContainText("<b>on my way</b>", live);
    await expect(log(page).locator("b")).toHaveCount(0);
    await expect(rows(page)).toHaveCount(2);
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
    expect(await rows(page).first().evaluate((el) => (el as unknown as { marker?: number }).marker)).toBe(1);

    // Sending: the field empties and keeps focus, the message arrives once
    // from the game's own echo, and it is recorded.
    await field(page).focus();
    await send(page, "welcome, all");
    await expect(rows(page).filter({ hasText: "welcome, all" })).toHaveCount(1, live);
    await expect(rows(page).filter({ hasText: "welcome, all" })).toContainText("Server: welcome, all");
    await expect(field(page)).toHaveValue("");
    await expect(field(page)).toBeFocused();
    await expect(page.locator("#announcer")).toHaveText("Message sent");
    expect(await commands(fakeGame!)).toContain("welcome, all");
    expect(chatAudit(foreman)).toEqual([{ username: "admin", world_id: "base", detail: "welcome, all" }]);
    await checkPage(page);
  });

  test("the log follows new lines only while at the bottom, offers Jump to newest, and keeps 200 lines", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await fakeGame!.call("/join", { name: container, player: "alice" });
    const box = log(page);
    const jump = page.getByRole("button", { name: "Jump to newest" });
    for (let i = 0; i < 40; i++) await fakeGame!.call("/chat", { name: container, player: "alice", text: `line ${i}` });
    await expect(box).toContainText("line 39", live);
    await expect.poll(() => atBottom(box)).toBe(true);
    await expect(jump).toBeHidden();
    await checkPage(page);

    // Reading older lines: new ones arrive but the view stays where it is.
    await field(page).focus();
    await box.evaluate((el) => (el.scrollTop = 0));
    await fakeGame!.call("/chat", { name: container, player: "alice", text: "late line" });
    await expect(box).toContainText("late line", live);
    expect(await box.evaluate((el) => el.scrollTop)).toBe(0);
    await expect(jump).toBeVisible();
    await expect(field(page)).toBeFocused();
    await jump.click();
    await expect.poll(() => atBottom(box)).toBe(true);
    await expect(jump).toBeHidden();

    // The page keeps at most the last 200 lines.
    for (let i = 0; i < 205; i += 5) {
      await Promise.all([0, 1, 2, 3, 4].map((k) => fakeGame!.call("/chat", { name: container, player: "alice", text: `bulk ${i + k}` })));
    }
    await expect(box).toContainText("bulk 204", live);
    await expect(rows(page)).toHaveCount(200);
    await expect(box).not.toContainText("line 0");
  });

  test("a message that starts with / is refused, however it is spaced, and never sent", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    for (const message of ["/c game.print(1)", "  /c rcon.print('ran')", "\t/silent-command x"]) {
      await send(page, message);
      const error = page.locator("#chat-error");
      await expect(error).toContainText("A chat message cannot start with /. To run a command, use the Console.");
      await expect(field(page)).toHaveAttribute("aria-invalid", "true");
      await expect(field(page)).toHaveValue(message);
      await expect(page.locator("#chat-panel").getByRole("link", { name: "Go to the Console" })).toHaveAttribute("href", "/console");
    }
    await checkPage(page);
    // Without a script the refusal is a whole page with the same words.
    const res = await page.request.post("/worlds/base/chat", { form: { message: "   /c rcon.print('ran')" } });
    expect(res.status()).toBe(400);
    expect(await res.text()).toContain("A chat message cannot start with /.");
    // Foreman itself asks the game who is online now and then, so look for the messages.
    expect((await commands(fakeGame!)).filter((c) => /c |silent-command|game\.print/.test(c))).toEqual([]);
    expect(chatAudit(foreman)).toEqual([]);
  });

  test("an empty, a long and a multi-line message are refused", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await send(page, "   ");
    await expect(page.locator("#chat-error")).toContainText("Enter a message to send");
    for (const [message, words] of [
      ["x".repeat(501), "at most 500 characters"],
      ["a\nb", "one line"],
    ] as const) {
      const res = await page.request.post("/worlds/base/chat", { form: { message } });
      expect(res.status()).toBe(400);
      expect(await res.text()).toContain(words);
    }
    expect(chatAudit(foreman)).toEqual([]);
  });

  test("a stopped server says so, and a message is not sent", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { running: false });
    await expect(page.locator("#chat-not-running")).toContainText("The server is not running, so no one can read chat.");
    await expect(page.locator("section.panel").getByRole("link", { name: "Go to the Server page" })).toHaveAttribute("href", "/");
    await send(page, "hello");
    await expect(page.locator("#chat-panel")).toContainText("The game is not running, so the message was not sent.");
    await expect(field(page)).toHaveValue("hello");
    expect(chatAudit(foreman)).toEqual([]);
    await checkPage(page);
  });

  test("the tab is translated", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await pickLanguage(page, "de");
    await page.goto("/chat");
    await expect(page.getByRole("log", { name: "Chatnachrichten" })).toBeVisible();
    await expect(page.getByLabel("Nachricht", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Senden" })).toBeVisible();
  });
});

test("with no world the Chat tab says there is none", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/chat");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Chat");
  await expect(page.getByText("No world is active yet")).toBeVisible();
  await expect(page.getByLabel("Message", { exact: true })).toHaveCount(0);
  await checkPage(page);
});

test("the Chat tab and its stream need a signed-in user", async ({ page }) => {
  for (const path of ["/chat", "/worlds/base/chat/events"]) {
    const res = await page.request.get(path, { maxRedirects: 0 });
    expect(res.status(), path).not.toBe(200);
  }
  const post = await page.request.post("/worlds/base/chat", { form: { message: "hi" }, maxRedirects: 0 });
  expect(post.status()).not.toBe(200);
});
