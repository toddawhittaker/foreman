import { test, expect, checkPage, expectFollowed, createAdmin, seedWorld, seedSecond, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// Switching worlds from the web pages (SPEC 7.9, 7.10, DESIGN 7.4, 7.6)
// against the fake game.

const live = { timeout: 20_000 };

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");
const active = (page: Page, name: string) => page.getByRole("link", { name: `Active world: ${name}` });

async function setUp(page: Page, f: Foreman, g: FakeGame, second: Parameters<typeof seedSecond>[1] = {}) {
  await createAdmin(page);
  seedWorld(f);
  seedSecond(f, second);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

async function startBase(page: Page) {
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(badge(page)).toHaveText("Running", live);
}

async function openSwitch(page: Page, id: string, name: string) {
  await page.goto(`/worlds/${id}`);
  await page.getByRole("link", { name: "Switch to this world" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(`Switch to ${name}`);
}

const creates = async (g: FakeGame) =>
  (await g.state()).calls.filter((c) => c.startsWith("POST ") && c.includes("/containers/create")).length;

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("a running world switches to another and back, with a step list", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await startBase(page);
    await g.call("/join", { name: "foreman-base", player: "alice" });

    await openSwitch(page, "second", "Second");
    const confirm = page.getByRole("region", { name: /Switch from Base to Second/ });
    await expect(confirm).toContainText("save the game and stop Base");
    await expect(confirm).toContainText('both labelled "Before switch"');
    await expect(confirm).toContainText("Start the game on Second");
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("60");
    await expect(confirm.getByRole("button", { name: "Switch to Second" })).toHaveClass(/button--danger/);
    await checkPage(page);

    await page.getByLabel("Countdown in seconds").fill("3");
    await confirm.getByRole("button", { name: "Switch to Second" }).click();
    await expect(page).toHaveURL("/");

    // The countdown holds the first step open, so the list can be seen.
    await expect(page.getByRole("heading", { name: "Switching from Base to Second" })).toBeVisible(live);
    await expect(page.getByRole("listitem").filter({ hasText: "Warn players" })).toContainText("In progress");
    await expect(page.getByRole("listitem").filter({ hasText: "Take a snapshot of both worlds" })).toContainText("Waiting");
    await checkPage(page);

    // The page follows the active world, which is now Second.
    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
    // Within main: the announcer says the same sentence, so a page-wide match is two elements.
    await expect(page.locator("main").getByText("The worlds are switched")).toBeVisible(live);
    await expectFollowed(page, "Second is now the active world. The worlds are switched.");
    await checkPage(page);
    expect((await g.state()).containers.find((c) => c.name === "foreman-second")?.status).toBe("running");
    expect((await g.state()).containers.find((c) => c.name === "foreman-base")?.status).not.toBe("running");

    // Both worlds were snapshotted first (SPEC 6.2).
    for (const id of ["base", "second"]) {
      await page.goto(`/worlds/${id}/snapshots`);
      await expect(page.getByRole("table")).toContainText("Before switch");
    }

    // Back again is a full switch with its own confirmation.
    await openSwitch(page, "base", "Base");
    await expect(page.getByRole("region", { name: /Switch from Second to Base/ })).toBeVisible();
    await page.getByLabel("Countdown in seconds").fill("0");
    await page.getByRole("button", { name: "Switch to Base" }).click();
    await expect(active(page, "Base")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  });

  test("a stopped world switches and, unticked, stays stopped", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    const before = await creates(g);

    await openSwitch(page, "second", "Second");
    const startAfter = page.getByRole("checkbox", { name: "Start Second after switching" });
    await expect(startAfter).toBeChecked();
    await expect(page.getByLabel("Countdown in seconds")).toHaveCount(0);
    await checkPage(page);
    await startAfter.uncheck();
    await page.getByRole("button", { name: "Switch to Second" }).click();

    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(page.locator("main").getByText("The worlds are switched")).toBeVisible();
    await expectFollowed(page, "Second is now the active world. The worlds are switched.");
    // The hidden event targets leave no blank band (DESIGN 6).
    for (const name of ["announce", "moved"]) {
      await expect(page.locator(`[sse-swap="${name}"]`)).toBeHidden();
      expect(await page.locator(`[sse-swap="${name}"]`).evaluate((el) => getComputedStyle(el).display)).toBe("none");
    }
    expect(await creates(g)).toBe(before);
    await expect(page.getByRole("button", { name: "Start server" })).toBeVisible();
    await checkPage(page);
  });

  test("a switch that cannot go ahead is refused in words", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);

    await page.goto("/worlds/base/switch");
    const refusal = page.locator(".message--error");
    await expect(refusal).toContainText("Error: This is already the active world.");
    await expect(refusal.locator("p strong")).toHaveText("Error:");
    await expect(page.getByRole("link", { name: "Back to Base" }).last()).toBeVisible();
    await expect(page.getByRole("button", { name: /Switch to/ })).toHaveCount(0);
    await checkPage(page);

    // A form for a world that has stopped being the active one is refused.
    const stale = await page.request.post("/worlds/second/switch", { form: { from: "second" }, maxRedirects: 0 });
    expect(stale.status()).toBe(409);
    expect(await stale.text()).toContain("no longer the active world");
    await expect.poll(() => creates(fakeGame!)).toBe(0);
    // A form that does not say which world was active is refused outright.
    const bare = await page.request.post("/worlds/second/switch", { form: {}, maxRedirects: 0 });
    expect(bare.status()).toBe(400);
    // The active world has no Switch button on its own page, and says why.
    await page.goto("/worlds/base");
    await expect(page.getByRole("link", { name: "Switch to this world" })).toHaveCount(0);
    await expect(page.getByRole("region", { name: "Switch" })).toContainText("This is the active world.");
    await checkPage(page);
    await page.goto("/worlds/second");
    await expect(page.getByRole("region", { name: "Switch" })).toContainText("leaves the server stopped if it is stopped");
  });

  test("a bad countdown keeps the form with the field's error", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await startBase(page);
    await openSwitch(page, "second", "Second");
    await page.getByLabel("Countdown in seconds").fill("900");
    await page.getByRole("button", { name: "Switch to Second" }).click();
    await expect(page.getByRole("heading", { name: /Fix this problem/ })).toBeVisible();
    await expect(page.locator("#countdown-error")).toHaveText("Error: Enter a whole number of seconds from 0 to 600");
    await checkPage(page);
    await page.goto("/");
    await expect(badge(page)).toHaveText("Running");
  });

  // SPEC 7.9, 7.4, 17.8: the problems show on the confirmation, which is the
  // only page that asks; "Switch and start anyway" goes ahead at once.
  test("a new world the check refuses can be switched to anyway", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true });
    await openSwitch(page, "second", "Second");

    const confirm = page.getByRole("region", { name: /Switch from Base to Second/ });
    const problems = confirm.locator(".message--warning");
    await expect(problems).toContainText("foreman-test");
    await expect(problems).toContainText("Second's own mods folder");
    await expect(confirm).toContainText('both labelled "Before switch"');
    await expect(confirm).toContainText('labelled "Before start anyway"');
    await expect(confirm).toContainText("To undo, switch back to Base");
    await expect(confirm.getByRole("button", { name: "Switch, leave stopped" })).toBeVisible();
    await expect(confirm.getByRole("button", { name: "Switch to Second" })).toHaveCount(0);
    await checkPage(page);
    await confirm.getByRole("button", { name: "Switch and start anyway" }).click();
    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
    await expect(page.locator("main").getByText("The worlds are switched")).toBeVisible(live);
    await page.goto("/worlds/second/snapshots");
    await expect(page.getByRole("table")).toContainText("Before start anyway");
  });

  test("a failure after the new world is active offers a switch back", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await startBase(page);
    await g.call("/rcon-refuse", { name: "foreman-second", on: true });

    await openSwitch(page, "second", "Second");
    await page.getByLabel("Countdown in seconds").fill("0");
    await page.getByRole("button", { name: "Switch to Second" }).click();

    await expect(page.getByRole("heading", { name: "The switch from Base to Second failed" })).toBeVisible(live);
    await expect(active(page, "Second")).toBeVisible();
    await expect(page.getByText("Second is now the active world, but the switch did not finish.")).toBeVisible();
    await expectFollowed(page, "Second is now the active world. Error:");
    await expect(page.getByRole("listitem").filter({ hasText: "Failed" })).toBeVisible();
    await checkPage(page);

    await g.call("/rcon-refuse", { name: "foreman-second", on: false });
    await page.getByRole("link", { name: "Switch back to Base" }).click();
    await expect(page.getByRole("region", { name: /Switch from Second to Base/ })).toBeVisible();
    await checkPage(page);
  });

  // DESIGN 5: when a live update takes away the control that had focus,
  // focus goes to the section's heading, not the top of the page.
  test("focus moves to the heading when a switch takes the buttons away", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await startBase(page);
    await g.call("/join", { name: "foreman-base", player: "alice" });
    const focused = () => page.evaluate(() => document.activeElement?.id || document.activeElement?.tagName);

    await page.getByRole("link", { name: "Restart server" }).focus();
    const started = await page.request.post("/worlds/second/switch", { form: { from: "base", countdown: "4" }, maxRedirects: 0 });
    expect(started.status()).toBe(303);
    await expect(page.getByRole("heading", { name: "Switching from Base to Second" })).toBeVisible(live);
    expect(await focused()).toBe("control-heading");

    // The page then loads again for the new active world.
    await expect(active(page, "Second")).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
  });
});
