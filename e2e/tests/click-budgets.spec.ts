import { test, expect, createAdmin, seedWorld, seedSecond, setUpModsWorld, image21, version20, live, creates, stateBadge as badge, doneBox as done, type Foreman, type FakeGame } from "./foreman.js";
import { countClicks, expectWithin, type Clicks } from "./clicks.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, readdirSync, utimesSync } from "node:fs";
import { join } from "node:path";

// SPEC 17.10: the common tasks, started on the Server page with the
// defaults accepted, take no more clicks than the table allows. Each test
// does the task, checks it finished, and checks the count. Controls are
// found by role and name only, so a change of layout does not break them.

const container = "foreman-base";
const active = (page: Page, name: string) => page.locator("#server-title", { hasText: name });

const diskVersion = (f: Foreman, id: string): string =>
  JSON.parse(readFileSync(join(f.dataDir, "worlds", id, "world.json"), "utf8")).game_version;

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
    await expect(badge(page)).toHaveText("Running");
    await expect(page.locator("main [sse-swap=facts]")).toContainText("2.1.24");
    await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);
    await expectWithin(clicks, 0, "see who is online");
  });

  // SPEC 17.10 (Generate row): from the Server page, Worlds, "Generate a
  // world", then "Generate world" with every default kept but the name.
  test("generate a world with the defaults: 3 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    await page.getByRole("link", { name: "Worlds", exact: true }).click();
    await page.getByRole("link", { name: "Generate a world" }).click();
    await page.getByLabel("World name").fill("Fresh");
    await page.getByRole("button", { name: "Generate world" }).click();
    await expect(page).toHaveURL("/worlds/fresh?generated=1");
    await expectWithin(clicks, 3, "generate");
  });

  // DESIGN 8.9: the active world's link, "Clone world", then "Clone world" on the form.
  test("clone the active world: 3 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    await page.getByRole("link", { name: "World page" }).click();
    await page.getByRole("link", { name: "Clone world" }).click();
    await page.getByRole("button", { name: "Clone world" }).click();
    await expect(page.locator(".message--success")).toBeVisible(live);
    await expectWithin(clicks, 3, "clone");
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

  // World page, Saves, then the older save's own Load next button (it was 4,
  // with a radio list and its button).
  test("choose an older save to load: 3 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!);
    const saves = join(foreman.dataDir, "worlds", "base", "saves");
    copyFileSync(join(saves, "base.zip"), join(saves, "older.zip"));
    const old = new Date(Date.now() - 2 * 3_600_000);
    utimesSync(join(saves, "older.zip"), old, old);
    await page.getByRole("link", { name: "World page" }).click();
    await page.getByRole("navigation", { name: "World pages" }).getByRole("link", { name: "Saves" }).click();
    await page.getByRole("button", { name: "Load next: older.zip" }).click();
    await expect(page.getByText("The server will load older.zip the next time it starts.")).toBeVisible();
    expect(JSON.parse(readFileSync(join(foreman.dataDir, "worlds", "base", "world.json"), "utf8")).load_save).toBe("older.zip");
    await expectWithin(clicks, 3, "choose an older save");
  });

  // The second world's game image is not on the host, so the switch downloads it.
  test("switch to another world, downloading its image: 3 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    const clicks = await setUp(page, foreman, g, { second: { version: "2.0.77", fixture: "2.0.77-no-space-age.zip" } });
    await startGame(page, clicks);

    // From the Server page: Switch world, then the world's Switch on the list (SPEC 17.10).
    await page.getByRole("link", { name: "Switch world" }).click();
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
    // From the Server page: Switch world, then the world's Switch on the list (SPEC 17.10).
    await page.getByRole("link", { name: "Switch world" }).click();
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
  // The switch is the "switch back" test above; the mod change is in the mods rows at the end.
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

  // A save that uses a mod the world lacks makes the game crash on every start, so a start anyway loops.
  async function crashLoopAfterStartAnyway(page: Page, g: FakeGame) {
    test.setTimeout(120_000);
    await page.getByRole("button", { name: "Start server" }).click();
    await page.locator("summary", { hasText: "Or start anyway" }).click();
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
    // From the Server page: Switch world, then the world's Switch on the list (SPEC 17.10).
    await page.getByRole("link", { name: "Switch world" }).click();
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

  // A start the checks refuse leaves its fix on the page: starting anyway is
  // its choice, then its button.
  test("recover after a refused start: 2 clicks", async ({ page, foreman, fakeGame }) => {
    const clicks = await setUp(page, foreman, fakeGame!, { world: { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true } });
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page.locator("summary", { hasText: "Or start anyway" })).toBeVisible(live);
    await clicks.reset();

    await page.locator("summary", { hasText: "Or start anyway" }).click();
    await page.getByRole("button", { name: "Start anyway" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await expectWithin(clicks, 2, "recover after a refused start");
  });
});

// SPEC 17.10, mods rows: the fake portal's generated mods are made for Factorio
// 2.0, so these worlds are 2.0 worlds. Downloads are on (SPEC 9.11).
test.describe("click budgets for mods, with the fake game and the fake portal", () => {
  test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

  const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };

  test("search for a mod and install it: 4 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    const g = fakeGame!;
    await fakePortal!.add("fp-signs", "1.0.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, g, world);
    const clicks = await countClicks(page);
    await startGame(page, clicks);
    const before = await creates(g);

    // Typing the name is not a click; the clicks are Mods, Search, Add to plan, and the plan's button.
    await page.getByRole("link", { name: "Mods", exact: true }).click();
    await page.getByLabel("Mod name").fill("fp-signs");
    await page.getByRole("button", { name: "Search" }).click();
    await page.getByRole("link", { name: "Add to plan fp-signs", exact: true }).click();
    await page.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    await expectWithin(clicks, 4, "search and install a mod");
  });

  // A second mod from the same search joins the plan: one more click, still one snapshot and one restart.
  test("search for two mods and install both: 5 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    const g = fakeGame!;
    await fakePortal!.add("fp-signs", "1.0.0", ["base >= 2.0"]);
    await fakePortal!.add("fp-signs-extra", "1.0.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, g, world);
    const clicks = await countClicks(page);
    await startGame(page, clicks);
    const before = await creates(g);

    await page.getByRole("link", { name: "Mods", exact: true }).click();
    await page.getByLabel("Mod name").fill("fp-signs");
    await page.getByRole("button", { name: "Search" }).click();
    await page.getByRole("link", { name: "Add to plan fp-signs", exact: true }).click();
    await page.getByRole("link", { name: "Add to plan fp-signs-extra", exact: true }).click();
    await page.getByRole("button", { name: "Install 2 mods" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    // One restart, and one snapshot for both mods.
    expect(await creates(g)).toBe(before + 1);
    const snaps = readdirSync(join(foreman.dataDir, "snapshots", "base")).filter((n) => /^\d{8}T\d{6}Z/.test(n));
    expect(snaps).toHaveLength(1);
    for (const f of ["fp-signs_1.0.0.zip", "fp-signs-extra_1.0.0.zip"]) expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", f))).toBe(true);
    await expectWithin(clicks, 5, "search and install two mods");
  });

  test("update all the mods: 3 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    const portal = fakePortal!;
    for (const name of ["fp-up1", "fp-up2"]) {
      await portal.add(name, "1.0.0", ["base >= 2.0"]);
      await portal.add(name, "1.1.0", ["base >= 2.0"]);
    }
    await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-up1": "2.0", "fp-up2": "2.0" } });
    const clicks = await countClicks(page);
    await startGame(page, clicks);

    await page.getByRole("link", { name: "Mods", exact: true }).click();
    // The portal's answer loads after the page; waiting for it is not a click.
    await page.getByRole("link", { name: "Update all" }).click();
    await page.getByRole("button", { name: "Update 2 mods" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    for (const f of ["fp-up1_1.1.0.zip", "fp-up2_1.1.0.zip"]) expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", f))).toBe(true);
    await expectWithin(clicks, 3, "update all");
  });

  // SPEC 9.6: Mods, Edit mod settings, the setting, then Save and restart.
  test("change a startup mod setting: 4 clicks", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUpModsWorld(page, foreman, g, { ...world, mods: { fmtest: "2.0" } });
    const clicks = await countClicks(page);
    await startGame(page, clicks);
    const before = await creates(g);

    await page.getByRole("link", { name: "Mods", exact: true }).click();
    await page.getByRole("link", { name: "Edit mod settings" }).click();
    // The form loads after the page; waiting for it is not a click.
    await page.getByRole("checkbox", { name: "fm-sta-bool" }).click(live);
    await page.getByRole("button", { name: "Save and restart" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "mod-settings.dat"))).toBe(true);
    await expectWithin(clicks, 4, "change a startup mod setting");
  });

  // Issue 131: the portal has no 1.5.5 of visible-planets.
  test("fix a mod players cannot get, from the Server page notice: 2 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    await fakePortal!.add("visible-planets", "1.7.2", ["base >= 2.0", "space-age >= 2.0"]);
    // The game runs with the mod: it started while the portal could not be asked (SPEC 7.4),
    // down from the first page on, so no answer is cached.
    await fakePortal!.setDown(true);
    await setUpModsWorld(page, foreman, fakeGame!, {
      version: version20,
      fixture: "2.0.77-space-age.zip",
      spaceAge: true,
      mods: { "visible-planets": { factorio: "2.0", version: "1.5.5", deps: ["base >= 2.0", "space-age >= 2.0"] } },
    });
    const clicks = await countClicks(page);
    await startGame(page, clicks);
    await fakePortal!.setDown(false);
    await page.goto("/");
    // The notice loads after the Server page; the count starts when it is there.
    await expect(page.locator("#mods-notice")).toContainText("cannot join", live);
    await clicks.reset();

    await page.getByRole("link", { name: "Update visible-planets" }).click();
    await page.getByRole("button", { name: "Update 1 mod" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "visible-planets_1.7.2.zip"))).toBe(true);
    await expectWithin(clicks, 2, "fix a mod players cannot get");
  });

  // Issue 359: the start refuses the mod players cannot download, and the
  // refusal's first button opens the same plan as the notice.
  test("fix and start a world refused for a mod players cannot download: 4 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    await fakePortal!.add("visible-planets", "1.7.2", ["base >= 2.0", "space-age >= 2.0"]);
    await setUpModsWorld(page, foreman, fakeGame!, {
      version: version20,
      fixture: "2.0.77-space-age.zip",
      spaceAge: true,
      mods: { "visible-planets": { factorio: "2.0", version: "1.5.5", deps: ["base >= 2.0", "space-age >= 2.0"] } },
    });
    const clicks = await countClicks(page);

    await page.getByRole("button", { name: "Start server" }).click();
    const refusal = page.locator(".message").filter({ hasText: "Foreman did not start the game, because players could not join it:" });
    await expect(refusal).toBeVisible(live);
    // The Server page's notice offers the same plan; the refusal's own button is the one used here.
    await refusal.getByRole("link", { name: "Update visible-planets" }).click();
    await page.getByRole("button", { name: "Update 1 mod" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "visible-planets_1.7.2.zip"))).toBe(true);
    await expectWithin(clicks, 4, "fix and start a world refused for a mod");
  });

  test("undo a mod change: 2 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    const g = fakeGame!;
    await fakePortal!.add("fp-signs", "1.0.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, g, world);
    const clicks = await countClicks(page);
    await startGame(page, clicks);
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-signs");
    await page.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    const installed = join(foreman.dataDir, "worlds", "base", "mods", "fp-signs_1.0.0.zip");
    expect(existsSync(installed)).toBe(true);
    const before = await creates(g);
    await clicks.reset();

    await page.getByRole("link", { name: "Undo this mod change" }).click();
    await page.getByRole("button", { name: "Restore snapshot" }).click();
    await expect.poll(() => creates(g), live).toBe(before + 1);
    await expect(done(page)).toContainText("The world is restored.", live);
    await expect(badge(page)).toHaveText("Running", live);
    // The old mods are back.
    expect(existsSync(installed)).toBe(false);
    await expectWithin(clicks, 2, "undo a mod change");
  });

  // Choosing from the list takes a click in a browser; selectOption makes none,
  // so the count here is 3, and the budget still covers the real 4.
  test("change a mod's version: 4 clicks", async ({ page, foreman, fakeGame, fakePortal }) => {
    const portal = fakePortal!;
    await portal.add("fp-back", "1.0.0", ["base >= 2.0"]);
    await portal.add("fp-back", "1.1.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-back": { factorio: "2.0", version: "1.1.0", deps: ["base >= 2.0"] } } });
    const clicks = await countClicks(page);
    await startGame(page, clicks);

    await page.getByRole("link", { name: "Mods", exact: true }).click();
    await page.getByRole("link", { name: "Change version fp-back", exact: true }).click();
    await page.getByLabel("Version of fp-back").selectOption("1.0.0");
    await expect(page.locator("#mod-plan .plan-groups")).toContainText("fp-back 1.1.0 to 1.0.0");
    await page.getByRole("button", { name: "Change 1 mod" }).click();
    await expect(done(page)).toContainText(/ Base\. Foreman took the snapshot "Before mod change" first\./, live);
    await expect(badge(page)).toHaveText("Running", live);
    expect(existsSync(join(foreman.dataDir, "worlds", "base", "mods", "fp-back_1.0.0.zip"))).toBe(true);
    await expectWithin(clicks, 4, "change a mod's version");
  });
});
