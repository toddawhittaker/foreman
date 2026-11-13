import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, type FakeGame, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Server page as the hub (SPEC 17.7, 17.10) and the forms it opens.

const live = { timeout: 20_000 };

async function running(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  seedSecond(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main .state").first()).toHaveText("Running", live);
}

const heading = (page: Page) => page.getByRole("heading", { level: 1 });
const badge = (page: Page) => page.locator("main h1 .state");

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  // DESIGN 6, 7.5: the heading is the world's name with its state, which
  // follows the game live, and focus lands on it when the controls change.
  test("the heading names the world and its state, live", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await expect(heading(page)).toHaveText(/^Base\s+Stopped$/);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await expect(badge(page)).toHaveClass(/state--running/);
    await expect(page.locator("#server-title")).toHaveCount(1);
    // A crash changes the heading in place, without a reload.
    await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
    await fakeGame!.call("/crash", { name: "foreman-base" });
    await expect(badge(page)).not.toHaveText("Running", live);
    await expect(heading(page)).toContainText("Base");
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
    // The facts no longer repeat the state.
    await expect(page.locator(".facts .state")).toHaveCount(0);
    await checkPage(page);
  });

  // SPEC 17.7, 17.10: the Server page has no list of worlds; Switch world
  // goes to the Worlds list, where each row has its Switch button.
  test("Switch world goes to the Worlds list", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await expect(page.getByRole("link", { name: "Switch to Second" })).toHaveCount(0);
    const links = page.locator("main .links a");
    await expect(links).toHaveText(["World page", "Mods", "Game settings", "Switch world"]);
    await page.getByRole("link", { name: "Switch world" }).click();
    await expect(page).toHaveURL("/worlds");
    await expect(page.getByRole("link", { name: "Switch to Second" })).toBeVisible();
  });

  // DESIGN 6: Overview and Console are tabs of the server view; Console
  // carries the same heading and live badge.
  test("Overview and Console are tabs with the same heading", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    const tabs = page.getByRole("navigation", { name: "Server pages" });
    await expect(tabs.getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");
    await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Server" })).toHaveAttribute("aria-current", "page");
    await tabs.getByRole("link", { name: "Console" }).click();
    await expect(page).toHaveURL("/console");
    await expect(tabs.getByRole("link", { name: "Console" })).toHaveAttribute("aria-current", "page");
    await expect(tabs.getByRole("link", { name: "Overview" })).not.toHaveAttribute("aria-current", /.*/);
    // On Console the Server tab marks the section, not the page.
    await expect(page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Server" })).toHaveAttribute("aria-current", "true");
    await expect(heading(page)).toHaveText(/^Base\s+Running$/);
    await checkPage(page);
    // The badge follows the game here too.
    await fakeGame!.call("/crash", { name: "foreman-base" });
    await expect(badge(page)).not.toHaveText("Running", live);
  });

  // SPEC 8.6: the game's own Error lines are coloured, and the log can be
  // downloaded as the Console shows it.
  test("the log colours Error lines and downloads", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await fakeGame!.call("/crash-loop", { name: "foreman-base", on: true });
    await page.goto("/");
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText(/Crash/, { timeout: 60_000 });
    await page.goto("/console");
    const errorLine = page.locator(".log pre > span.log-error").first();
    await expect(errorLine).toContainText("Error");
    const plain = await page.locator(".log pre > span:not([class])").first().evaluate((el) => getComputedStyle(el).color);
    expect(await errorLine.evaluate((el) => getComputedStyle(el).color)).not.toBe(plain);
    await checkPage(page);

    const [download] = await Promise.all([page.waitForEvent("download"), page.getByRole("link", { name: "Download log" }).click()]);
    expect(download.suggestedFilename()).toBe("foreman-base-log.txt");
    const text = await (await download.createReadStream()).toArray();
    expect(Buffer.concat(text).toString()).toContain("Error");
  });

  // SPEC 7.6: one tap sets the countdown to 0, which means no warning; there
  // is still no separate checkbox.
  test("No warning sets the countdown to 0 and the stop needs no wait", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await page.getByRole("link", { name: "Stop server" }).click();
    const field = page.getByLabel("Countdown in seconds");
    await expect(field).toHaveValue("60");
    await expect(page.getByRole("checkbox")).toHaveCount(0);
    await checkPage(page);
    await page.getByRole("button", { name: "No warning" }).click();
    await expect(field).toHaveValue("0");
    await expect(field).toBeFocused();
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page).toHaveURL("/");
    await expect(page.locator("main .state").first()).toHaveText("Stopped", live);
  });

  // The same button is on every form that stops the game: here, a switch.
  test("the switch form has No warning beside its countdown", async ({ page, foreman, fakeGame }) => {
    await running(page, foreman, fakeGame!);
    await page.goto("/worlds/second/switch");
    await page.getByRole("button", { name: "No warning" }).click();
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("0");
  });
});

test.describe("with the fake game and no JavaScript", () => {
  test.use({ fakeGameOn: true, javaScriptEnabled: false });

  // Without script the button cannot work, so it is not shown; the hint
  // says that 0 means no warning.
  test("No warning is hidden and the hint says to type 0", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await page.getByRole("button", { name: "Start server" }).click();
    await expect.poll(async () => {
      await page.goto("/");
      return page.locator("main .state--running").count();
    }, { timeout: 30_000, intervals: [1000] }).toBeGreaterThan(0);
    await page.goto("/worlds/base/stop");
    await expect(page.getByRole("button", { name: "No warning" })).toBeHidden();
    await expect(page.getByText("0 means no warning")).toBeVisible();
  });
});

// SPEC 3.2: the join line shows only the address the operator set; Foreman
// never guesses one.
test("with no public address the Server page shows no join line", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/");
  await expect(page.getByText("Players join at")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Copy address" })).toHaveCount(0);
});

test.describe("with a public address", () => {
  test.use({ foremanEnv: { FOREMAN_PUBLIC_ADDRESS: "factorio.example.com" } });

  test("the join line names the address and Copy address copies it", async ({ page, foreman, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/");
    const join = page.locator(".join");
    await expect(join).toContainText("Players join at factorio.example.com:34197");
    await expect(join.locator("code")).toHaveText("factorio.example.com:34197");
    await checkPage(page);
    // The test page is on 127.0.0.1, a secure context, so the button shows.
    await page.getByRole("button", { name: "Copy address" }).click();
    await expect(page.locator("#copy-done")).toBeVisible();
    await expect(page.locator("#announcer")).toHaveText("Address copied");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("factorio.example.com:34197");
  });

  test("where the Clipboard API is missing, Copy address is not offered", async ({ page, foreman }) => {
    await page.addInitScript(() => Object.defineProperty(navigator, "clipboard", { value: undefined }));
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/");
    await expect(page.locator(".join")).toContainText("factorio.example.com:34197");
    await expect(page.getByRole("button", { name: "Copy address" })).toBeHidden();
  });
});
