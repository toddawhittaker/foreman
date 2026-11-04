import { test, expect, checkPage, createAdmin, seedWorld, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Server page (SPEC 7, 8.1) against the fake game.

const container = "foreman-base";
// The monitor polls Docker every 5 seconds, so a change it has to
// notice can take that long to show.
const live = { timeout: 20_000 };

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: { image?: boolean; fixture?: string; spaceAge?: boolean } = {}) {
  await createAdmin(page);
  seedWorld(f, { fixture: opts.fixture, spaceAge: opts.spaceAge });
  if (opts.image ?? true) await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

async function start(page: Page) {
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page).toHaveURL("/");
  await expect(badge(page)).toHaveText("Running", live);
}

async function commands(g: FakeGame): Promise<string[]> {
  return (await g.state()).containers.find((c) => c.name === container)?.commands ?? [];
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("start, see a player join, then stop with a countdown", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await expect(page.getByText("Nobody can be online while the game is not running.")).toBeVisible();
    await checkPage(page);

    await start(page);
    await expect(page.getByText("Nobody is online.")).toBeVisible(live);
    await fakeGame!.call("/join", { name: container, player: "alice" });
    // The list follows the game without a reload (SPEC 8.1, 16.3).
    await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);
    await expect(page.getByRole("table")).toContainText("1 online");
    await checkPage(page);

    await page.getByRole("link", { name: "Stop server" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Stop the server");
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("60");
    await checkPage(page);
    await page.getByLabel("Countdown in seconds").fill("2");
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page).toHaveURL("/");
    await expect(badge(page)).toHaveText("Stopped", live);
    const sent = await commands(fakeGame!);
    expect(sent).toContain("The server will save and stop in 2 seconds.");
    expect(sent).toContain("/server-save");
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  });

  test("a bad countdown is refused with the field's error", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByLabel("Countdown in seconds").fill("900");
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page.getByRole("heading", { name: /Fix this problem/ })).toBeVisible();
    await expect(page.locator("#countdown-error")).toHaveText("Error: Enter a whole number of seconds from 0 to 600");
    await expect(page.getByLabel("Countdown in seconds")).toHaveAttribute("aria-invalid", "true");
    await checkPage(page);
    await page.goto("/");
    await expect(badge(page)).toHaveText("Running");
  });

  test("a state change shows without a reload, and is announced", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
    // The game is stopped from elsewhere, with the same session; this page
    // follows. A second tab would leave this one in the background, where
    // the browser stops checking it.
    const stopped = await page.request.post("/worlds/base/force-stop", { form: {}, maxRedirects: 0 });
    expect(stopped.status()).toBe(303);
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(page.locator("#announcer")).toHaveText("Server is now stopped");
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
    await expect(page.getByRole("button", { name: "Start server" })).toBeVisible();
  });

  test("restart", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    const creates = async () => (await fakeGame!.state()).calls.filter((c) => c.startsWith("POST ") && c.includes("/containers/create")).length;
    const before = await creates();
    await page.getByRole("link", { name: "Restart server" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Restart the server");
    await checkPage(page);
    await page.getByLabel(/No countdown/).check();
    await page.getByRole("button", { name: "Restart server" }).click();
    await expect(page).toHaveURL("/");
    // A fresh container for every start, running again.
    await expect.poll(creates, live).toBe(before + 1);
    await expect(badge(page)).toHaveText("Running", live);
    expect(await commands(fakeGame!)).toContain("/server-save");
  });

  test("forced stop has its own confirmation", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await page.getByRole("link", { name: "Force stop" }).click();
    const confirm = page.getByRole("region", { name: /Force stop the game running Base/ });
    await expect(confirm).toContainText("Lose all progress since the last save");
    await expect(confirm.getByRole("button", { name: "Force stop" })).toHaveClass(/button--danger/);
    await checkPage(page);
    await confirm.getByRole("button", { name: "Force stop" }).click();
    await expect(badge(page)).toHaveText("Stopped", live);
    expect(await commands(fakeGame!)).not.toContain("/server-save");
  });

  test("a crash is shown", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await fakeGame!.call("/crash", { name: container });
    await expect(badge(page)).toHaveText("Crashed", live);
    await expect(page.getByText(/The game crashed/)).toBeVisible();
    await checkPage(page);
  });

  test("a crash loop is stopped and shown with the last log lines", async ({ page, foreman, fakeGame }) => {
    test.setTimeout(60_000);
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await fakeGame!.call("/crash-loop", { name: container, on: true });
    await expect(badge(page)).toHaveText("Crash-looping", { timeout: 40_000 });
    await expect(page.getByText(/crashed again and again, so Foreman stopped it/)).toBeVisible();
    const crashLog = page.getByRole("region", { name: "The last log lines before the crash" });
    await expect(crashLog).not.toBeEmpty();
    await expect(page.getByRole("button", { name: "Start server" })).toBeVisible();
    // The button is not flush under the log (DESIGN 6, rule 4).
    const logRect = (await crashLog.boundingBox())!;
    const buttonRect = (await page.getByRole("button", { name: "Start server" }).boundingBox())!;
    expect(buttonRect.y - (logRect.y + logRect.height)).toBeGreaterThanOrEqual(8);
    await checkPage(page);
  });

  test("a refused start lists the problems, and start anyway takes a snapshot first", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true });
    await page.getByRole("button", { name: "Start server" }).click();
    const refusal = page.locator(".message--error").filter({ hasText: "does not match the world" });
    await expect(refusal).toContainText("foreman-test", live);
    await expect(badge(page)).toHaveText("Stopped");
    // Steps that will never run read Skipped, not Waiting.
    const steps = page.locator(".steps li");
    await expect(steps.filter({ hasText: "Check the world" })).toContainText("Failed");
    await expect(steps.filter({ hasText: "Check RCON answers" })).toContainText("Skipped");
    await expect(page.locator(".steps")).not.toContainText("Waiting");
    await checkPage(page);

    await refusal.getByRole("link", { name: "Start anyway" }).click();
    const confirm = page.getByRole("region", { name: /Start Base although its save does not match the world/ });
    await expect(confirm).toContainText('labelled "Before start anyway"');
    await expect(confirm).toContainText("foreman-test");
    await checkPage(page);
    await confirm.getByRole("button", { name: "Start anyway" }).click();
    await expect(badge(page)).toHaveText("Running", live);

    await page.goto("/worlds/base/snapshots");
    await expect(page.getByRole("table")).toContainText("Before start anyway");
  });

  // DESIGN 5: a live update never drops keyboard focus to the top of the page.
  test("focus stays on an action across a live update, or moves to the heading", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    const focused = () => page.evaluate(() => document.activeElement?.id || document.activeElement?.tagName);

    // The crash notice is added, and Stop server stays: focus stays on it.
    await page.getByRole("link", { name: "Stop server" }).focus();
    await fakeGame!.call("/crash", { name: container });
    await expect(badge(page)).toHaveText("Crashed", live);
    await expect(page.getByText(/The game crashed/)).toBeVisible();
    expect(await focused()).toBe("act-stop");

    // Force stop goes away with the game: focus moves to the section's heading.
    await page.getByRole("link", { name: "Force stop" }).focus();
    expect((await page.request.post("/worlds/base/force-stop", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(page.getByRole("button", { name: "Start server" })).toBeVisible();
    expect(await focused()).toBe("control-heading");
  });

  // DESIGN 7.5: a failure that arrives live is read out, and the state
  // sentence is not said beside it.
  test("a failed start is announced", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true });
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect(page.locator(".message--error")).toContainText("does not match the world", live);
    await expect(page.locator("#announcer")).toHaveText(/^Error: Foreman did not start the game, because the save that would load does not match the world/);
    await expect(page.locator("#announcer")).not.toContainText("Server is now");
  });

  // DESIGN 7.5: a refused action's error stays on the page, and a crash
  // after it is still read out.
  test("a crash is announced after a refused action", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect(page.locator(".message--error")).toBeVisible(live);
    await expect(page.locator("#announcer")).toHaveText(/^Error: /);
    await fakeGame!.call("/crash", { name: container });
    await expect(badge(page)).toHaveText("Crashed", live);
    await expect(page.locator("#announcer")).toHaveText("Server is now crashed");
  });

  // SPEC 17.1: the step list passes the checks while an operation runs.
  test("the step list of a stop in progress passes the page checks", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await fakeGame!.call("/join", { name: container, player: "alice" });
    await fakeGame!.call("/save-hangs", { name: container, on: true });
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByLabel("Countdown in seconds").fill("3");
    await page.getByRole("button", { name: "Stop server" }).click();
    const steps = page.getByRole("list").filter({ has: page.locator("[aria-current=step]") });
    await expect(page.getByRole("heading", { name: "Stopping the server" })).toBeVisible(live);
    await expect(steps).toBeVisible();
    await checkPage(page);
    // The countdown ends and the save hangs: still in progress.
    await expect(steps.locator("[aria-current=step]")).toContainText("Save the game", live);
    await checkPage(page);
    await page.getByRole("link", { name: "Force stop" }).click();
    await page.getByRole("region", { name: /Force stop/ }).getByRole("button", { name: "Force stop" }).click();
    await expect(badge(page)).toHaveText("Stopped", live);
  });

  // A dropped live connection is said, not silent, and the note goes when
  // the page is live again.
  test("a dropped live connection shows a note until it reconnects", async ({ page, foreman, fakeGame }) => {
    // The stream cannot be reached, as while Foreman restarts.
    const stream = "**/worlds/base/events";
    await page.route(stream, (route) => route.fulfill({ status: 503, body: "" }));
    await setUp(page, foreman, fakeGame!);
    const note = page.locator("#live-lost");
    await expect(note).toHaveText("Note: Live updates stopped. Reload to see the current state.", live);
    await expect(page.locator("#announcer")).toHaveText("Live updates stopped. Reload to see the current state.");
    await checkPage(page);
    await page.unroute(stream);
    await expect(note).toHaveCount(0, { timeout: 60_000 });
    // Live again: a change shows without a reload.
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect(badge(page)).toHaveText("Running", live);
  });

  test("a missing image is downloaded as its own action", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { image: false });
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page.getByText(/game image for this world's Factorio version is not downloaded/)).toBeVisible(live);
    await checkPage(page);
    await page.getByRole("button", { name: "Download game image" }).click();
    await expect(page.getByRole("button", { name: "Download game image" })).toHaveCount(0, live);
    await expect.poll(async () => (await fakeGame!.state()).calls.some((c) => c.startsWith("POST ") && c.includes("/images/create")), live).toBe(true);
    await expect(page.getByRole("heading", { name: "Downloading the game image" })).toHaveCount(0, live);
    await start(page);
  });

  test("a snapshot of a running world saves first, and a restore stops and starts it", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await page.goto("/worlds/base/snapshots");
    await page.getByLabel(/Label/).fill("While running");
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(page.getByText('Foreman took the snapshot "While running".')).toBeVisible();
    expect(await commands(fakeGame!)).toContain("/server-save");

    await page.getByRole("link", { name: /^Restore/ }).first().click();
    await expect(page.getByLabel("Countdown in seconds")).toBeVisible();
    await expect(page.getByRole("region", { name: /Restore the snapshot/ })).toContainText("Start the game again");
    await checkPage(page);
    await page.getByLabel(/No countdown/).check();
    await page.getByRole("button", { name: "Restore snapshot" }).click();
    await expect(page).toHaveURL("/");
    await expect(page.locator(".message--success")).toContainText("The world is restored", live);
    await expect(page.locator("#announcer")).toHaveText("The world is restored.");
    await expect(badge(page)).toHaveText("Running", live);
    await checkPage(page);
  });
});

test.describe("with Docker unreachable", () => {
  test.use({ foremanEnv: { DOCKER_HOST: "unix:///nonexistent/docker.sock" } });

  test("the page says Unknown and why, and offers no action", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/");
    await expect(badge(page)).toHaveText("Unknown", live);
    await expect(page.getByRole("region", { name: "Status" })).toContainText("Foreman cannot reach Docker");
    await expect(page.getByRole("button", { name: "Start server" })).toHaveCount(0);
    await expect(page.getByText("Foreman cannot start or stop the game until it can ask Docker about it.")).toBeVisible();
    await checkPage(page);
  });
});
