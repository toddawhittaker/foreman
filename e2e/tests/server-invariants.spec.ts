import { test, expect, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, mkdirSync, readFileSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// Trust boundaries and invariants of server control, end to end (SPEC 3.8,
// 7.6, 14.3, 14.7, 14.8), against the real binary and the fake game.

const password = "correct horse battery";
const gamePassword = "e2e-game-pw-Vb93";
const repo = join(import.meta.dirname, "..", "..");
const image21 = /^GAME_IMAGE_2_1\s*:=\s*(\S+)\s*$/m.exec(readFileSync(join(repo, "Makefile"), "utf8"))![1];
const live = { timeout: 20_000 };

// SPEC 14.3, managed mode, as "METHOD path".
const allowed =
  /^(GET \/_ping|GET \/version|GET \/containers\/json|GET \/containers\/[^/]+\/(json|logs|stats)|POST \/containers\/[^/]+\/(start|stop|kill)|POST \/containers\/create|DELETE \/containers\/[^/]+|POST \/images\/create|GET \/images\/.+\/json|GET \/events)$/;

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/worlds");
}

// The active world "base" with a real save and a game password set.
function seedWorld(f: Foreman) {
  const dir = join(f.dataDir, "worlds", "base");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name: "Base", game_version: "2.1.24", space_age: false, created_at: "2026-11-01T10:00:00Z" }),
  );
  writeFileSync(join(dir, "config", "server-settings.json"), JSON.stringify({ name: "Base", game_password: gamePassword }));
  const save = join(dir, "saves", "base.zip");
  copyFileSync(join(repo, "internal", "savefile", "testdata", "2.1.24-no-space-age.zip"), save);
  const at = new Date(Date.now() - 3_600_000);
  utimesSync(save, at, at);
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    db.prepare("INSERT INTO active_worlds (world_id, since) VALUES (?, ?)").run("base", 0);
  } finally {
    db.close();
  }
}

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");

async function setUp(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

async function mutations(g: FakeGame): Promise<string[]> {
  return (await g.state()).calls.filter((c) => c.startsWith("POST ") || c.startsWith("DELETE "));
}

test.describe("server control invariants", () => {
  test.use({ fakeGameOn: true });

  test("signed-out and cross-origin requests never reach Docker", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    const before = await mutations(fakeGame!);
    const posts = ["start", "stop", "restart", "force-stop", "pull-image", "snapshots"];

    // SPEC 12.8: no session, no action.
    for (const action of posts) {
      const resp = await fetch(`${foreman.url}/worlds/base/${action}`, {
        method: "POST",
        redirect: "manual",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: "no_countdown=on&anyway=1",
      });
      expect(resp.status, `signed-out ${action}`).toBe(303);
      expect(resp.headers.get("location"), `signed-out ${action}`).not.toBe("/");
    }
    const events = await fetch(`${foreman.url}/worlds/base/events`, { redirect: "manual" });
    expect(events.status).toBe(303);

    // SPEC 14.7: a signed-in browser posting from another site is refused.
    for (const action of posts) {
      const resp = await page.request.post(`/worlds/base/${action}`, {
        form: { no_countdown: "on", anyway: "1" },
        headers: { Origin: "https://evil.example", "Sec-Fetch-Site": "cross-site" },
        maxRedirects: 0,
      });
      expect(resp.status(), `cross-site ${action}`).toBe(403);
    }

    await page.waitForTimeout(500);
    expect(await mutations(fakeGame!)).toEqual(before);
    await page.reload();
    await expect(badge(page)).toHaveText("Stopped");
  });

  test("start and stop: only listed Docker calls, no secret shown, never a crash", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    const rconPassword = readFileSync(join(foreman.dataDir, "worlds", "base", "config", "rconpw"), "utf8").trim();
    expect(rconPassword).not.toBe("");

    const pages: string[] = [await page.content()];
    await page.getByRole("link", { name: "Stop server" }).click();
    pages.push(await page.content());
    await page.getByLabel("Countdown in seconds").fill("0");
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(badge(page)).toHaveText("Stopped", live);

    // SPEC 7.6: the monitor looks again within 5 seconds; still not a crash.
    await page.waitForTimeout(6_000);
    await page.reload();
    await expect(badge(page)).toHaveText("Stopped");
    pages.push(await page.content());

    for (const secret of [rconPassword, gamePassword]) {
      for (const html of pages) expect(html).not.toContain(secret);
      expect(foreman.logs()).not.toContain(secret);
    }
    const calls = (await fakeGame!.state()).calls;
    for (const c of calls) expect(c, "SPEC 14.3").toMatch(allowed);
    expect(calls.filter((c) => c.endsWith("/kill")), "a graceful stop never kills").toEqual([]);
  });
});
