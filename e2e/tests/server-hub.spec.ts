import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readFileSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// The Server page is the hub for the active world (SPEC 7.6, 7.9, 17.7 to
// 17.10): stopping can be cancelled, results offer their undo as a button,
// and a switch or a snapshot starts here.

const container = "foreman-base";
const live = { timeout: 20_000 };

const badge = (page: Page) => page.locator("main h1 .state");
const done = (page: Page) => page.locator(".message--success").filter({ hasText: "Done:" });

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: { second?: Parameters<typeof seedSecond>[1]; fixture?: string; spaceAge?: boolean } = {}) {
  await createAdmin(page);
  seedWorld(f, { fixture: opts.fixture, spaceAge: opts.spaceAge });
  if (opts.second) seedSecond(f, opts.second);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

async function startWithAlice(page: Page, g: FakeGame) {
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(badge(page)).toHaveText("Running", live);
  await g.call("/join", { name: container, player: "alice" });
  await expect(page.locator("#pr-alice")).toBeVisible(live);
}

// On a phone Force stop folds under the closed "Emergency actions" disclosure;
// on a wide screen the script has already opened it (DESIGN 7.5).
async function openEmergency(page: Page) {
  const emergency = page.locator("details.emergency");
  if (!(await emergency.evaluate((d) => (d as HTMLDetailsElement).open))) await emergency.locator("summary").click();
}

async function commands(g: FakeGame): Promise<string[]> {
  return (await g.state()).containers.find((c) => c.name === container)?.commands ?? [];
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  // DESIGN 6, "What a phone shows first": the world's pages come before the
  // players on a phone, and the code has them in that order, so Tab meets
  // them in the order they are drawn. On a wide screen the players sit on
  // the left and the pages on the right (SPEC 17.1, WCAG 2.4.3).
  test("the world's pages come before the players, in the code and on a phone", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    const pages = page.locator(".server-grid > .shortcuts");
    const players = page.locator(".server-grid > section:has(#players-heading)");
    const order = await page.locator(".server-grid > *").evaluateAll((els) => els.map((el) => (el.classList.contains("shortcuts") ? "pages" : "players")));
    expect(order).toEqual(["pages", "players"]);
    const p = (await pages.boundingBox())!;
    const q = (await players.boundingBox())!;
    const wide = page.viewportSize()!.width >= 1024;
    if (wide) {
      expect(q.x + q.width, "players on the left").toBeLessThanOrEqual(p.x);
      expect(Math.abs(q.y - p.y), "side by side").toBeLessThan(1);
    } else {
      expect(p.y + p.height, "pages above the players").toBeLessThanOrEqual(q.y);
    }
    await checkPage(page);
  });

  test("a stop names who is online, counts down, and can be cancelled", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await startWithAlice(page, g);

    await openEmergency(page);
    await page.getByRole("link", { name: "Force stop" }).click();
    await expect(page.locator("section.confirm")).toContainText("These players are disconnected: alice.");
    await page.goto("/");

    await page.getByRole("link", { name: "Stop server" }).click();
    await expect(page.locator("#online")).toHaveText("1 player online: alice.");
    // One control for the warning: 0 seconds means none (SPEC 7.6).
    await expect(page.getByRole("checkbox")).toHaveCount(0);
    await checkPage(page);
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page).toHaveURL("/");

    const warn = page.locator(".steps li").filter({ hasText: "Warn players" });
    await expect(warn).toContainText(/Warn players: \d+ seconds left/, live);
    const first = await warn.innerText();
    await expect(warn).not.toHaveText(first, { timeout: 5000 });
    // Who is being warned stays listed while the game stops.
    await expect(page.getByRole("table")).toContainText("1 online when the stop began");
    await expect(page.locator("#pr-alice")).toBeVisible();
    await checkPage(page);

    await page.getByRole("button", { name: "Cancel stop" }).click();
    await expect(done(page)).toContainText("The stop was canceled. Nothing was saved or stopped, and nothing changed.", live);
    await expect(done(page)).toContainText(/Finished at .*\d{1,2}:\d{2}.* \((now|\d+ seconds? ago)\)\./);
    await expect(badge(page)).toHaveText("Running");
    await expect(page.locator(".message--error")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Cancel stop" })).toHaveCount(0);
    const sent = await commands(g);
    expect(sent).toContain("The server stop was canceled.");
    expect(sent).not.toContain("/server-save");
    await checkPage(page);
  });

  test("a switch starts from the Server page, can be cancelled while warning, and offers the way back", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g, { second: {} });
    await startWithAlice(page, g);

    // Server page, Switch world, Switch to Second, confirm: SPEC 17.10's budget of 3.
    await page.getByRole("link", { name: "Switch world" }).click();
    await page.getByRole("link", { name: "Switch to Second" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Switch from Base to Second?");
    await expect(page.locator("#online")).toHaveText("1 player online: alice.");
    await page.getByRole("button", { name: "Switch to Second" }).click();

    // While players are warned, the status names both worlds.
    await expect(page.locator("main [sse-swap=facts]")).toContainText("From Base to Second", live);
    await page.getByRole("button", { name: "Cancel stop" }).click();
    await expect(done(page)).toContainText("The switch to Second was canceled. Base is still the active world, and nothing changed.", live);
    await expect(page.locator("#server-title", { hasText: "Base" })).toBeVisible();
    await expect(badge(page)).toHaveText("Running");

    await page.getByRole("link", { name: "Switch world" }).click();
    await page.getByRole("link", { name: "Switch to Second" }).click();
    await page.getByLabel("Countdown in seconds").fill("0");
    await page.getByRole("button", { name: "Switch to Second" }).click();
    await expect(page.locator("#server-title", { hasText: "Second" })).toBeVisible(live);
    await expect(done(page)).toContainText("The worlds are switched, from Base to Second.", live);
    await expect(badge(page)).toHaveText("Running", live);
    await checkPage(page);

    // Switch back after a switch: 2 clicks (SPEC 17.10).
    await done(page).getByRole("link", { name: "Switch back to Base" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Switch from Second to Base?");
  });

  test("a snapshot is one click, and the next action clears the last Done message", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(done(page)).toContainText('Foreman took the snapshot "Manual snapshot" of Base.', live);
    await expect(done(page)).toContainText(/Finished at .*\d{1,2}:\d{2}.* \((now|\d+ seconds? ago)\)\./);
    await checkPage(page);

    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await expect(done(page)).toHaveCount(0);
  });

  test("a restore done from a running game offers its undo as a button", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(done(page)).toBeVisible(live);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);

    await page.goto("/worlds/base/snapshots");
    await page.getByRole("link", { name: /^Restore Manual snapshot/ }).click();
    await page.getByLabel("Countdown in seconds").fill("0");
    await page.getByRole("button", { name: "Restore snapshot" }).click();
    await expect(done(page)).toContainText("The world is restored.", live);
    await expect(badge(page)).toHaveText("Running", live);
    await done(page).getByRole("link", { name: "Undo this restore" }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Restore the snapshot "Before restore"/ })).toBeVisible();
  });

  // SPEC 17.9: after a start anyway whose game then crash-loops, one box
  // says so and offers the restore of "Before start anyway".
  test("a crash loop after start anyway offers the restore", async ({ page, foreman, fakeGame }) => {
    test.setTimeout(90_000);
    const g = fakeGame!;
    await setUp(page, foreman, g, { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true });
    await page.getByRole("button", { name: "Start server" }).click();
    await page.locator("summary", { hasText: "Or start anyway" }).click();
    await page.getByRole("button", { name: "Start anyway" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await g.call("/crash-loop", { name: container, on: true });
    await expect(badge(page)).toHaveText("Crash-looping", { timeout: 40_000 });

    const crash = page.locator(".message--warning").filter({ hasText: "crashed again and again" });
    await expect(crash).toContainText("then try to start it again. Or put things back as they were before:");
    await expect(page.locator(".message--note")).toHaveCount(0);
    await checkPage(page);
    await crash.getByRole("link", { name: /^Restore "Before start anyway" \(/ }).click();
    await expect(page.getByRole("heading", { level: 1, name: /Restore the snapshot "Before start anyway"/ })).toBeVisible();
  });

  // SPEC 7.11, 17.7: with an older save chosen, the Server page names it and
  // links to the Saves tab, stopped or running; with none chosen, it says nothing.
  test("the page says which older save loads next, stopped or running", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    const facts = page.locator("main [sse-swap=loads]");
    await expect(facts).not.toContainText("Loads next");

    // base.zip is older than a newer autosave, and is chosen.
    const dir = join(foreman.dataDir, "worlds", "base");
    writeFileSync(join(dir, "saves", "newer.zip"), "x");
    const now = new Date();
    utimesSync(join(dir, "saves", "newer.zip"), now, now);
    const meta = JSON.parse(readFileSync(join(dir, "world.json"), "utf8"));
    writeFileSync(join(dir, "world.json"), JSON.stringify({ ...meta, load_save: "base.zip" }));

    await page.goto("/");
    const link = facts.getByRole("link", { name: /^base\.zip ?, on the Saves tab$/ });
    await expect(facts.getByText("Loads next")).toBeVisible();
    await expect(link).toHaveAttribute("href", "/worlds/base/saves");
    await checkPage(page);

    // A start uses the choice up (SPEC 7.11), so a newer autosave and the choice come again while the game runs.
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await expect(page.getByText("Save loaded")).toBeVisible(live);
    // The start used the choice up, and the page says so without a reload.
    await expect(facts).not.toContainText("Loads next");
    const later = new Date(Date.now() + 60_000);
    utimesSync(join(dir, "saves", "newer.zip"), later, later);
    writeFileSync(join(dir, "world.json"), JSON.stringify({ ...meta, load_save: "base.zip" }));
    await page.goto("/");
    await expect(facts.getByText("Loads next")).toBeVisible();
    await expect(link).toBeVisible();

    await link.click();
    await expect(page).toHaveURL("/worlds/base/saves");

    // Choosing the newest again clears the line.
    writeFileSync(join(dir, "world.json"), JSON.stringify({ ...meta }));
    await page.goto("/");
    await expect(page.getByText("Save loaded")).toBeVisible(live);
    await expect(facts).not.toContainText("Loads next");
  });

  // SPEC 3.7: a world's page says before any switch that its image is missing.
  test("a world page says when its game image is missing", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { second: { version: "2.0.77", fixture: "2.0.77-no-space-age.zip" } });
    await page.goto("/worlds/second");
    await expect(page.getByRole("region", { name: "Switch", exact: true })).toContainText(
      "This world runs on the game image factoriotools/factorio:2.0.77, which is not on this host, so it cannot start yet.",
    );
    await checkPage(page);
  });
});

// SPEC 7.1: when Docker cannot be asked, the page shows nothing it cannot
// know, and does not claim that nobody is online.
test.describe("with Docker unreachable", () => {
  test.use({ foremanEnv: { DOCKER_HOST: "unix:///nonexistent/docker.sock" } });

  test("the status leaves out what cannot be known", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/");
    await expect(badge(page)).toHaveText("Unknown", live);
    const status = page.locator("main [sse-swap=facts]");
    await expect(status).not.toContainText("Uptime");
    await expect(status).not.toContainText("Save loaded");
    await expect(page.getByRole("region", { name: "Players online" })).toContainText("Foreman cannot see whether anyone is online.");
    await expect(page.getByRole("button", { name: "Take snapshot" })).toHaveCount(0);
    await checkPage(page);
  });
});
