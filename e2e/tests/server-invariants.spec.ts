import { test, expect, createAdmin, seedWorld, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// Trust boundaries and invariants of server control, end to end (SPEC 3.8,
// 7.6, 14.3, 14.7, 14.8), against the real binary and the fake game.

const gamePassword = "e2e-game-pw-Vb93";
const live = { timeout: 20_000 };

// SPEC 14.3, managed mode, as "METHOD path".
const allowed =
  /^(GET \/_ping|GET \/version|GET \/containers\/json|GET \/containers\/[^/]+\/(json|logs|stats)|POST \/containers\/[^/]+\/(start|stop|kill)|POST \/containers\/create|DELETE \/containers\/[^/]+|POST \/images\/create|GET \/images\/.+\/json|GET \/events)$/;

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");

async function setUp(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f, { gamePassword });
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

async function mutations(g: FakeGame): Promise<string[]> {
  return (await g.state()).calls.filter((c) => c.startsWith("POST ") || c.startsWith("DELETE "));
}

test.describe("server control invariants", () => {
  test.use({ fakeGameOn: true });

  test("signed-out, cross-origin, wrong-method and unknown-world requests never reach Docker or the game", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    // Running, so a console command that got through would reach the game.
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    const before = await mutations(fakeGame!);
    const marker = "/e2e-must-not-arrive";
    const body = `no_countdown=on&anyway=1&command=${encodeURIComponent(marker)}`;
    const posts = ["start", "stop", "restart", "force-stop", "pull-image", "snapshots", "console"];
    const gets = ["events", "console/events"];

    // SPEC 12.8: no session, no action.
    for (const action of posts) {
      const resp = await fetch(`${foreman.url}/worlds/base/${action}`, {
        method: "POST",
        redirect: "manual",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body,
      });
      expect(resp.status, `signed-out ${action}`).toBe(303);
      expect(resp.headers.get("location"), `signed-out ${action}`).not.toBe("/");
    }
    for (const path of gets) {
      const resp = await fetch(`${foreman.url}/worlds/base/${path}`, { redirect: "manual" });
      expect(resp.status, `signed-out ${path}`).toBe(303);
    }

    // SPEC 14.7: a signed-in browser posting from another site is refused.
    for (const action of posts) {
      const resp = await page.request.post(`/worlds/base/${action}`, {
        form: { no_countdown: "on", anyway: "1", command: marker },
        headers: { Origin: "https://evil.example", "Sec-Fetch-Site": "cross-site" },
        maxRedirects: 0,
      });
      expect(resp.status(), `cross-site ${action}`).toBe(403);
    }

    // The wrong method, and a world that does not exist, signed in.
    for (const action of posts) {
      const resp = await page.request.fetch(`/worlds/base/${action}`, { method: "PUT", form: { command: marker }, maxRedirects: 0 });
      expect(resp.status(), `PUT ${action}`).toBe(405);
    }
    for (const path of gets) {
      const resp = await page.request.post(`/worlds/base/${path}`, { form: { command: marker }, maxRedirects: 0 });
      expect(resp.status(), `POST ${path}`).toBe(405);
    }
    for (const bad of ["nope", "%2e%2e", "..%2f..%2fetc"]) {
      for (const action of posts) {
        const resp = await page.request.post(`/worlds/${bad}/${action}`, { form: { no_countdown: "on", command: marker }, maxRedirects: 0 });
        expect(resp.status(), `${bad} ${action}`).toBeGreaterThanOrEqual(300);
        expect(resp.headers()["location"] ?? "", `${bad} ${action}`).not.toBe("/");
      }
      for (const path of gets) {
        const resp = await page.request.get(`/worlds/${bad}/${path}`, { maxRedirects: 0 });
        expect(resp.status(), `${bad} ${path}`).toBeGreaterThanOrEqual(300);
      }
    }

    await page.waitForTimeout(500);
    expect(await mutations(fakeGame!)).toEqual(before);
    const sent = (await fakeGame!.state()).containers.find((c) => c.name === "foreman-base")?.commands ?? [];
    expect(sent).not.toContain(marker);
    await page.reload();
    await expect(badge(page)).toHaveText("Running");
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
