import { test, expect, checkPage, createAdmin, seedWorld, image21, stateBadge, live, creates, sidecarSecret, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { request } from "node:http";

// SPEC 4.13: a game in a VPN sidecar's network. The fake game seeds the sidecar
// (internal/fakegame/sidecar.go); Foreman is told its name. The Server page warns,
// without ever blocking the start, when the sidecar publishes no host port for
// UDP 34197, and a start the sidecar's checks refuse says why.

const container = "foreman-base";
const warning = "LAN players cannot reach the game: vpn does not publish UDP 34197 on the host.";
const lanBox = (page: Page) => page.locator("#lan-unpublished");

async function setUp(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(stateBadge(page)).toHaveText("Stopped", live);
}

async function start(page: Page) {
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
}

// Nothing the sidecar holds, such as a VPN key in its environment, reaches the page, the log or Foreman's reply.
async function expectNoSecret(page: Page, f: Foreman) {
  expect(await page.content()).not.toContain(sidecarSecret);
  expect(await (await page.request.get("/")).text()).not.toContain(sidecarSecret);
  expect(f.logs()).not.toContain(sidecarSecret);
}

// Stops the sidecar through the fake Docker's API, as `docker stop` does; the fake has no hook for it.
function stopContainer(g: FakeGame, name: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const req = request({ socketPath: g.socket, path: `/containers/${name}/stop`, method: "POST" }, (res) => {
      res.resume();
      res.on("end", () => (res.statusCode === 204 ? resolve() : reject(new Error(`stop ${name}: ${res.statusCode}`))));
    });
    req.on("error", reject);
    req.end();
  });
}

test.describe("a sidecar that publishes the game port", () => {
  // Any host port counts: the operator may map 34197 to another one.
  test.use({ fakeGameOn: true, sidecar: { name: "vpn", port: "40000" } });

  test("starts with no warning, and the console works", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    expect((await fakeGame!.state()).sidecar?.port).toBe("40000");
    await start(page);
    await expect(lanBox(page)).toHaveCount(0);
    await expect(page.getByText("LAN players cannot reach the game")).toHaveCount(0);
    await checkPage(page);

    await page.goto("/console");
    await page.getByLabel("Command").fill("/players online");
    await page.getByLabel("Command").press("Enter");
    await expect(page.locator("main")).toContainText("Online players (0):", live);
    await expectNoSecret(page, foreman);
  });
});

test.describe("a sidecar that publishes no port", () => {
  test.use({ fakeGameOn: true, sidecar: { name: "vpn" } });

  test("starts with a warning that is not an alert, then follows the sidecar's publication live", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    // A second tab of the same session watches the page; it is never reloaded, so every change it
    // shows arrived by live update.
    const watcher = await page.context().newPage();
    await watcher.goto("/");
    await watcher.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
    await expect(lanBox(watcher)).toHaveCount(0);
    await expect(lanBox(page)).toHaveCount(0);

    await start(page);
    await expect(lanBox(watcher)).toContainText(warning, live);
    // The start went ahead; the warning came with it.
    await expect(lanBox(page)).toContainText(warning, live);
    await expect(lanBox(page)).toContainText("Warning:");
    // A fact about the page, not a result: it is not announced.
    await expect(lanBox(page).locator('[role="alert"], [role="status"], [aria-live]')).toHaveCount(0);
    await expect(page.locator("#announcer")).not.toContainText("LAN players");
    await checkPage(page);

    // A game keeps what it was started with: publishing now changes nothing until the next start.
    await g.call("/sidecar-port", { port: "34197" });
    await expect(lanBox(page)).toContainText(warning);
    const before = await creates(g);
    await page.getByRole("link", { name: "Restart server" }).click();
    await page.getByRole("button", { name: "Restart server" }).click();
    await expect.poll(() => creates(g), live).toBe(before + 1);
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(lanBox(page)).toHaveCount(0, live);
    await expect(lanBox(watcher)).toHaveCount(0, live);

    // Unpublishing, then a fresh start, brings the warning back.
    await g.call("/sidecar-port", { port: "" });
    const again = await creates(g);
    await page.getByRole("link", { name: "Restart server" }).click();
    await page.getByRole("button", { name: "Restart server" }).click();
    await expect.poll(() => creates(g), live).toBe(again + 1);
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(lanBox(page)).toContainText(warning, live);
    await expect(lanBox(watcher)).toContainText(warning, live);
    expect(await watcher.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
    await checkPage(page);
    await expectNoSecret(page, foreman);
  });

  test("the game's console works beside the warning", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await page.goto("/console");
    await page.getByLabel("Command").fill("/players online");
    await page.getByLabel("Command").press("Enter");
    await expect(page.locator("main")).toContainText("Online players (0):", live);
  });

  test("stops being a warning once the game stops", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await start(page);
    await expect(lanBox(page)).toContainText(warning, live);
    await page.getByRole("link", { name: "Stop server" }).click();
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(stateBadge(page)).toHaveText("Stopped", live);
    await expect(lanBox(page)).toHaveCount(0, live);
  });
});

test.describe("a sidecar Foreman cannot use", () => {
  test.describe("one Docker does not have", () => {
    test.use({ fakeGameOn: true, foremanEnv: { FOREMAN_GAME_NETWORK_CONTAINER: "ghost" } });

    test("refuses the start before any game is made, and says what to do", async ({ page, foreman, fakeGame }) => {
      await setUp(page, foreman, fakeGame!);
      await page.getByRole("button", { name: "Start server" }).click();
      const box = page.locator(".message").filter({ hasText: "FOREMAN_GAME_NETWORK_CONTAINER names \"ghost\"" });
      await expect(box).toContainText(
        "Docker has no container by that name, so the game has no VPN sidecar to share a network with. Start the sidecar, or unset FOREMAN_GAME_NETWORK_CONTAINER.",
        live,
      );
      await expect(stateBadge(page)).toHaveText("Stopped");
      expect(await creates(fakeGame!)).toBe(0);
      await expect(lanBox(page)).toHaveCount(0);
      await checkPage(page);
    });
  });

  test.describe("one that is stopped", () => {
    test.use({ fakeGameOn: true, sidecar: { name: "vpn", port: "34197" } });

    test("refuses the start before any game is made, and says to start the sidecar", async ({ page, foreman, fakeGame }) => {
      const g = fakeGame!;
      await setUp(page, foreman, g);
      await stopContainer(g, "vpn");
      await expect.poll(async () => (await g.state()).sidecar?.status).not.toBe("running");
      await page.getByRole("button", { name: "Start server" }).click();
      const box = page.locator(".message").filter({ hasText: "The VPN sidecar \"vpn\" is not running" });
      await expect(box).toContainText("so the game cannot share its network. Start the sidecar, then try again.", live);
      await expect(stateBadge(page)).toHaveText("Stopped");
      expect(await creates(g)).toBe(0);
      await checkPage(page);
      await expectNoSecret(page, foreman);
    });
  });
});

// The same pages with no script: the server draws the warning, and a start posts a form.
test.describe("without JavaScript", () => {
  test.use({ fakeGameOn: true, javaScriptEnabled: false, sidecar: { name: "vpn" } });

  test("the warning is in the page, and the start went ahead", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect.poll(async () => (await page.request.get("/")).text(), live).toContain("state--running");
    await page.goto("/");
    await expect(lanBox(page)).toContainText(warning);
    expect((await fakeGame!.state()).containers.some((c) => c.name === container && c.status === "running")).toBe(true);
    // axe needs script, so the accessibility checks above cover the same markup.
    await expectNoSecret(page, foreman);
  });
});
