import { test, expect, checkPage, createAdmin, seedWorld, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Player actions and the ban list and whitelist (SPEC 8.2, 8.3, DESIGN 8.12,
// 8.13) against the fake game, which writes the ban and admin lists at once
// and keeps the whitelist in a file only when started with both whitelist
// flags.

test.use({ fakeGameOn: true });

const container = "foreman-base";
const live = { timeout: 20_000 };
const config = (f: Foreman, file: string) => join(f.dataDir, "worlds", "base", "config", file);
const readConfig = (f: Foreman, file: string) => (existsSync(config(f, file)) ? readFileSync(config(f, file), "utf8") : "");

async function commands(g: FakeGame): Promise<string[]> {
  return (await g.state()).containers.find((c) => c.name === container)?.commands ?? [];
}

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: { running: boolean }) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  if (opts.running) await start(page);
}

async function start(page: Page) {
  expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
  await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("state--running");
}

test("a stopped world's ban list and whitelist are saved through the preview, with a snapshot", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, { running: false });
  writeFileSync(config(foreman, "server-banlist.json"), JSON.stringify([{ username: "bob", reason: "griefing" }]));
  await page.goto("/worlds/base/settings");
  await expect(page.getByLabel("Banned players")).toHaveValue("bob");
  await checkPage(page);

  await page.getByLabel("Banned players").fill("bob\nbad name");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.locator("#bans-error")).toContainText("Line 2 is not a usable player name");
  await expect(page.getByLabel("Banned players")).toHaveAttribute("aria-invalid", "true");

  await page.getByLabel("Banned players").fill("bob\ncarl");
  await page.getByLabel("Whitelisted players", { exact: true }).fill("hank");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("heading", { name: "Changes to the ban list" })).toBeVisible();
  await expect(page.getByText("Banned carl")).toBeVisible();
  await expect(page.getByText("Added hank to the whitelist")).toBeVisible();
  await checkPage(page);
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator(".message--success")).toContainText("Settings saved.");

  const bans = JSON.parse(readConfig(foreman, "server-banlist.json"));
  expect(bans).toEqual([{ username: "bob", reason: "griefing" }, "carl"]);
  expect(JSON.parse(readConfig(foreman, "server-whitelist.json"))).toEqual(["hank"]);
});

test("the whitelist switch is saved per world, and the next start enforces the whitelist", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, { running: false });
  await page.goto("/worlds/base/settings");
  await expect(page.getByText('The game checks this list only while "Only whitelisted players can join" is on.')).toBeVisible();
  const box = page.getByLabel("Only whitelisted players can join");
  await expect(box).not.toBeChecked();
  await box.check();
  await page.getByRole("button", { name: "Save whitelist enforcement" }).click();
  await expect(page.locator("#done-message")).toContainText("From its next start, only whitelisted players can join Base.");
  await expect(page.getByLabel("Only whitelisted players can join")).toBeChecked();
  expect(JSON.parse(readFileSync(join(foreman.dataDir, "worlds", "base", "world.json"), "utf8")).use_whitelist).toBe(true);
  await checkPage(page);

  // Started with the switch on, the game keeps the whitelist: it is edited live.
  await start(page);
  await page.goto("/worlds/base/settings");
  const card = page.locator("section#settings-whitelist");
  await expect(card.getByText("Nobody is on the whitelist.")).toBeVisible();
  await card.getByLabel("Add a player").fill("hank");
  await card.getByRole("button", { name: "Add to whitelist" }).click();
  await expect(page.locator("#player-done")).toContainText("hank is on the whitelist of Base.");
  expect(JSON.parse(readConfig(foreman, "server-whitelist.json"))).toEqual(["hank"]);
  await checkPage(page);

  // Undo removes the name at once.
  await page.getByRole("button", { name: "Undo: remove hank from the whitelist" }).click();
  await expect(page.locator("#player-done")).toContainText("hank is no longer on the whitelist of Base.");
  expect(JSON.parse(readConfig(foreman, "server-whitelist.json"))).toEqual([]);

  // Turning it off while running needs a restart, and says so with the button.
  await page.getByLabel("Only whitelisted players can join").uncheck();
  await page.getByRole("button", { name: "Save whitelist enforcement" }).click();
  await expect(page.locator("#done-message")).toContainText("From its next start, anyone can join Base.");
  await expect(page.locator("#whitelist-restart-hint")).toContainText("Restart it so anyone can join.");
  await expect(page.locator("#act-whitelist-restart")).toBeVisible();
});

test("on a running world a ban confirms with a reason, and unban acts at once with Undo", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, { running: true });
  await page.goto("/worlds/base/settings");
  const card = page.locator("section#settings-bans");
  await expect(card.getByText("The server is running, so changes here go to the game at once. No snapshot is taken.")).toBeVisible();
  await expect(card.getByText("Nobody is banned.")).toBeVisible();
  await checkPage(page);

  // A name the console would misread is refused, never sent.
  await card.getByLabel("Ban a player").fill("bob smith");
  await card.getByRole("button", { name: "Ban", exact: true }).click();
  await expect(page.locator("#ban-name-error")).toContainText("This is not a usable player name.");
  expect((await commands(fakeGame!)).filter((c) => c.startsWith("/ban"))).toEqual([]);

  await card.getByLabel("Ban a player").fill("bob");
  await card.getByRole("button", { name: "Ban", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Ban bob from Base?");
  await expect(page.getByText("Take no snapshot. The game records the ban in its own ban list.")).toBeVisible();
  await checkPage(page);
  await page.getByLabel("Reason").fill("griefing");
  await page.getByRole("button", { name: "Ban bob" }).click();

  await expect(page).toHaveURL(/\/worlds\/base\/settings$/);
  await expect(page.locator("#player-done")).toContainText("bob is banned from Base.");
  expect(await commands(fakeGame!)).toContain("/ban bob griefing");
  expect(readConfig(foreman, "server-banlist.json")).toContain('"reason": "griefing"');
  await expect(card.getByText("griefing")).toBeVisible();

  await card.getByRole("button", { name: "Unban bob" }).click();
  await expect(page.locator("#player-done")).toContainText("bob is unbanned from Base.");
  await checkPage(page);
  // Undo bans again with the reason the ban had.
  await page.getByRole("button", { name: "Undo: ban bob again" }).click();
  await expect(page.locator("#player-done")).toContainText("bob is banned from Base.");
  expect((await commands(fakeGame!)).filter((c) => c.startsWith("/ban"))).toEqual(["/ban bob griefing", "/ban bob griefing"]);
});

test("a kick confirms, a reason that is too long is refused, and a player who left cannot be kicked", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, { running: true });
  await fakeGame!.call("/join", { name: container, player: "alice" });

  await page.goto("/worlds/base/players/kick?name=alice");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Kick alice from Base?");
  await expect(page.getByText("Disconnect alice now. They can join again at once.")).toBeVisible();
  await expect(page.getByRole("link", { name: "Cancel, keep alice in the game" })).toHaveAttribute("href", "/");
  await checkPage(page);

  // A text box cannot hold a line break, so the browser path checks the
  // length; the unit tests send line breaks. Nothing is sent.
  await page.getByLabel("Reason").evaluate((el: HTMLInputElement) => {
    el.removeAttribute("maxlength");
    el.value = "r".repeat(201);
  });
  await page.getByLabel("Reason").evaluate((el: HTMLInputElement) => el.form!.submit());
  await expect(page.locator("#player-reason-error")).toContainText("The reason must be one line");
  expect((await commands(fakeGame!)).filter((c) => c.startsWith("/kick") || c.startsWith("/promote"))).toEqual([]);

  await page.getByLabel("Reason").fill("afk");
  await page.getByRole("button", { name: "Kick alice" }).click();
  await expect(page).toHaveURL(/\/$/);
  expect(await commands(fakeGame!)).toContain("/kick alice afk");

  await page.goto("/worlds/base/players/kick?name=alice");
  await expect(page.locator("#player-gone")).toContainText("alice is no longer in the game, so there is no one to kick.");
  await expect(page.getByRole("button", { name: "Kick alice" })).toHaveCount(0);
  await checkPage(page);
});
