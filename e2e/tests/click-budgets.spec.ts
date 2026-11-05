import { test, expect, createAdmin, seedWorld, seedSecond, image21, type Foreman, type FakeGame } from "./foreman.js";
import { countClicks, expectWithin, type Clicks } from "./clicks.js";
import type { Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 17.10: the common tasks, started on the Server page with the
// defaults accepted, take no more clicks than the table allows. Each test
// does the task, checks it finished, and checks the count. Controls are
// found by role and name only, so a change of layout does not break them.

const container = "foreman-base";
const live = { timeout: 20_000 };

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");
const done = (page: Page) => page.locator(".message--success").filter({ hasText: "Done:" });
const active = (page: Page, name: string) => page.getByRole("link", { name: `Active world: ${name}` });

const diskVersion = (f: Foreman, id: string): string =>
  JSON.parse(readFileSync(join(f.dataDir, "worlds", id, "world.json"), "utf8")).game_version;

const creates = async (g: FakeGame) =>
  (await g.state()).calls.filter((c) => c.startsWith("POST ") && c.includes("/containers/create")).length;

// Signs in as the admin, seeds the active world "base" (and "second" when
// asked) and ends on the Server page with the game stopped.
async function setUp(page: Page, f: Foreman, g: FakeGame, opts: { second?: Parameters<typeof seedSecond>[1]; world?: Parameters<typeof seedWorld>[1] } = {}): Promise<Clicks> {
  await createAdmin(page);
  seedWorld(f, opts.world);
  if (opts.second) seedSecond(f, opts.second);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
  return countClicks(page);
}

// Setup the budget does not cover: it is not counted, and the test is back
// on the Server page with the count at zero when it returns.
async function startGame(page: Page, clicks: Clicks) {
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(badge(page)).toHaveText("Running", live);
  await clicks.reset();
}

test.describe("click budgets, with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("see who is online and what is running: 0 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g);
    await startGame(page, clicks);
    await g.call("/join", { name: container, player: "alice" });

    // The Server page, as it loads, answers both questions.
    await expect(page.getByRole("region", { name: "Status" })).toContainText("Running");
    await expect(page.getByRole("region", { name: "Status" })).toContainText("2.1.24");
    await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);
    await expectWithin(clicks, 0, "see who is online");
  });

  test("start the active world: 1 click", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await expectWithin(clicks, 1, "start");
  });

  test("stop it, warning players: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    await startGame(page, clicks);

    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(badge(page)).toHaveText("Stopped", live);
    await expectWithin(clicks, 2, "stop");
  });

  test("stop it with a player online: the two clicks begin the warning countdown", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g);
    await startGame(page, clicks);
    await g.call("/join", { name: container, player: "alice" });
    await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);

    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Stop server" }).click();
    // The default countdown is 60 seconds, so the test stops at its start; Cancel stop is not part of the task.
    await expect(page.locator(".steps li").filter({ hasText: "Warn players" })).toContainText(/\d+ seconds left/, live);
    await expectWithin(clicks, 2, "stop with a player online");
  });

  test("restart it, warning players: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g);
    await startGame(page, clicks);
    const before = await creates(g);

    await page.getByRole("link", { name: "Restart server" }).click();
    await page.getByRole("button", { name: "Restart server" }).click();
    // A fresh container for every start, running again.
    await expect.poll(() => creates(g), live).toBe(before + 1);
    await expect(badge(page)).toHaveText("Running", live);
    await expectWithin(clicks, 2, "restart");
  });

  test("take a snapshot: 1 click", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(done(page)).toContainText('Foreman took the snapshot "Manual snapshot" of Base.', live);
    await expectWithin(clicks, 1, "snapshot");
  });

  // The second world's game image is not on the host, so the switch downloads it.
  test("switch to another world, downloading its image: 3 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g, { second: { version: "2.0.77", fixture: "2.0.77-no-space-age.zip" } });
    await startGame(page, clicks);

    await page.getByRole("link", { name: "Switch to Second" }).click();
    await page.getByRole("button", { name: "Switch to Second" }).click();
    await expect(active(page, "Second")).toBeVisible(live);
    await expect(done(page)).toContainText("The worlds are switched, from Base to Second.", live);
    await expect(badge(page)).toHaveText("Running", live);
    const second = (await g.state()).containers.find((c) => c.name === "foreman-second");
    expect(second?.image).toContain(":2.0.77");
    await expectWithin(clicks, 3, "switch with a download");
  });

  test("switch back after a switch: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g, { second: {} });
    await startGame(page, clicks);
    await page.getByRole("link", { name: "Switch to Second" }).click();
    await page.getByRole("button", { name: "Switch to Second" }).click();
    await expect(done(page)).toContainText("The worlds are switched, from Base to Second.", live);
    await expect(badge(page)).toHaveText("Running", live);
    await clicks.reset();

    await page.getByRole("link", { name: "Switch back to Base" }).click();
    await page.getByRole("button", { name: "Switch to Base" }).click();
    await expect(active(page, "Base")).toBeVisible(live);
    await expect(done(page)).toContainText("The worlds are switched, from Second to Base.", live);
    await expect(badge(page)).toHaveText("Running", live);
    await expectWithin(clicks, 2, "switch back");
  });

  // SPEC 17.10 lists the undo of a switch, an update, a restore and a mod change.
  // The switch is the "switch back" test above; the mod change waits for Epic 6.
  test("undo a restore: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(done(page)).toBeVisible(live);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await page.goto("/worlds/base/snapshots");
    await page.getByRole("link", { name: /^Restore Manual snapshot/ }).click();
    await page.getByRole("button", { name: "Restore snapshot" }).click();
    await expect(done(page)).toContainText("The world is restored.", live);
    await expect(badge(page)).toHaveText("Running", live);
    const before = await creates(g);
    await clicks.reset();

    await page.getByRole("link", { name: "Undo this restore" }).click();
    await page.getByRole("button", { name: "Restore snapshot" }).click();
    await expect.poll(() => creates(g), live).toBe(before + 1);
    await expect(done(page)).toContainText("The world is restored.", live);
    await expect(badge(page)).toHaveText("Running", live);
    await expectWithin(clicks, 2, "undo a restore");
  });

  test("undo a game version update: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    await page.goto("/worlds/base/update");
    await page.getByLabel("Or type a version").fill("2.1.25");
    await page.getByRole("button", { name: "Update game version" }).click();
    await expect(done(page)).toContainText("The game version is updated.", live);
    expect(diskVersion(foreman, "base")).toBe("2.1.25");
    await clicks.reset();

    await page.getByRole("link", { name: "Roll back to 2.1.24" }).click();
    await page.getByRole("button", { name: "Roll back to 2.1.24" }).click();
    await expect.poll(() => diskVersion(foreman, "base"), live).toBe("2.1.24");
    await expectWithin(clicks, 2, "undo an update");
  });

  // Epic 6 adds the mod change. Its undo must then take 2 clicks too.
  test.skip("undo a mod change: 2 clicks", () => {});

  // A save that uses a mod the world lacks makes the game crash on every start, so a start anyway loops.
  async function crashLoopAfterStartAnyway(page: Page, g: FakeGame) {
    test.setTimeout(120_000);
    await page.getByRole("button", { name: "Start server" }).click();
    await page.getByRole("button", { name: "Start anyway" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await g.call("/crash-loop", { name: container, on: true });
    await expect(badge(page)).toHaveText("Crash-looping", { timeout: 40_000 });
  }

  test("recover after a crash loop by restoring: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g, { world: { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true } });
    await crashLoopAfterStartAnyway(page, g);
    await clicks.reset();

    await page.getByRole("link", { name: /^Restore "Before start anyway"/ }).click();
    await page.getByRole("button", { name: "Restore snapshot" }).click();
    await expect(done(page)).toContainText("The world is restored.", live);
    await expectWithin(clicks, 2, "recover by restoring");
  });

  test("recover after a crash loop by switching back: 2 clicks", async ({ page, foreman, fakeGame }) => {
    test.setTimeout(120_000);
    const g = fakeGame!;
    // Base is fine; Second's save needs a mod it lacks, so starting it anyway loops.
    const clicks = await setUp(page, foreman, g, { second: { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true } });
    await startGame(page, clicks);
    await page.getByRole("link", { name: "Switch to Second" }).click();
    await page.getByRole("button", { name: "Switch and start anyway" }).click();
    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
    await g.call("/crash-loop", { name: "foreman-second", on: true });
    await expect(badge(page)).toHaveText("Crash-looping", { timeout: 40_000 });
    await clicks.reset();

    await page.getByRole("link", { name: "Switch back to Base" }).click();
    await page.getByRole("button", { name: "Switch to Base" }).click();
    await expect(active(page, "Base")).toBeVisible(live);
    await expect(done(page)).toContainText("The worlds are switched, from Second to Base.", live);
    await expectWithin(clicks, 2, "recover by switching back");
  });

  // A start the checks refuse leaves its fix on the page: starting anyway is one click.
  test("recover after a refused start: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!, { world: { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true } });
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page.getByRole("button", { name: "Start anyway" })).toBeVisible(live);
    await clicks.reset();

    await page.getByRole("button", { name: "Start anyway" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await expectWithin(clicks, 2, "recover after a refused start");
  });
});
