/// <reference types="node" />
import { test as base, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { createInterface } from "node:readline";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Shared by every spec that needs a Foreman process of its own.

const binary = join(import.meta.dirname, "..", "..", "bin", "foreman");
const fakeGameBinary = join(import.meta.dirname, "..", "..", "bin", "fakegame");

// SPEC 17.1 names WCAG 2.2 AA.
export const wcagTags = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = createServer();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      srv.close(() => resolve(port));
    });
  });
}

export interface Foreman {
  url: string;
  dataDir: string;
  logs: () => string;
}

// Starts the real binary on a free port over a fresh data directory, so each
// test sees a first run of its own.
async function startForeman(env: Record<string, string>): Promise<Foreman & { stop: () => Promise<void> }> {
  const dataDir = mkdtempSync(join(tmpdir(), "foreman-e2e-"));
  const port = await freePort();
  let out = "";
  const proc = spawn(binary, ["serve"], {
    env: {
      ...process.env,
      FOREMAN_DATA_DIR: dataDir,
      FOREMAN_GAME_UID: String(process.getuid?.() ?? 0),
      FOREMAN_GAME_GID: String(process.getgid?.() ?? 0),
      // Managed mode with the host path given, so no Docker call is made.
      FOREMAN_MODE: "managed",
      FOREMAN_HOST_DATA_DIR: dataDir,
      FOREMAN_LISTEN: `127.0.0.1:${port}`,
      ...env,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  proc.stdout.on("data", (d) => (out += d));
  proc.stderr.on("data", (d) => (out += d));
  const url = `http://127.0.0.1:${port}`;
  const deadline = Date.now() + 10_000;
  for (;;) {
    if (proc.exitCode !== null) throw new Error(`foreman exited ${proc.exitCode}:\n${out}`);
    try {
      if ((await fetch(url + "/healthz")).ok) break;
    } catch {
      // not listening yet
    }
    if (Date.now() > deadline) throw new Error(`foreman did not start:\n${out}`);
    await new Promise((r) => setTimeout(r, 50));
  }
  return {
    url,
    dataDir,
    logs: () => out,
    stop: async () => {
      if (proc.exitCode === null) {
        const exited = new Promise((r) => proc.once("exit", r));
        proc.kill("SIGTERM");
        await exited;
      }
      rmSync(dataDir, { recursive: true, force: true });
    },
  };
}

// A fake Docker host with a fake game in it (internal/fakegame). Its hooks are
// POSTs of JSON to the hook address; see HookHandler for the list.
export interface FakeGame {
  socket: string;
  hook: string;
  // Calls a hook and fails the test on a refusal.
  call: (path: string, body: Record<string, unknown>) => Promise<void>;
  // Everything the fake knows: containers with their players and RCON
  // commands, and every Docker call received.
  state: () => Promise<{
    containers: { name: string; status: string; ip: string; players: string[]; commands: string[]; restart_count: number }[];
    calls: string[];
  }>;
}

async function startFakeGame(): Promise<FakeGame & { stop: () => Promise<void> }> {
  const dir = mkdtempSync(join(tmpdir(), "fakegame-e2e-"));
  const proc = spawn(fakeGameBinary, ["-dir", dir], { stdio: ["ignore", "pipe", "pipe"] });
  let err = "";
  proc.stderr.on("data", (d) => (err += d));
  const lines = createInterface({ input: proc.stdout })[Symbol.asyncIterator]();
  const timeout = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error(`fakegame did not start:\n${err}`)), 10_000),
  );
  const exited = new Promise<never>((_, reject) =>
    proc.once("exit", (code) => reject(new Error(`fakegame exited ${code}:\n${err}`))),
  );
  const first = await Promise.race([lines.next(), timeout, exited]);
  const ready = JSON.parse(first.value as string) as { socket: string; hook: string };
  const call = async (path: string, body: Record<string, unknown>) => {
    const resp = await fetch(ready.hook + path, { method: "POST", body: JSON.stringify(body) });
    if (!resp.ok) throw new Error(`fakegame ${path} ${JSON.stringify(body)}: ${resp.status} ${await resp.text()}`);
  };
  return {
    ...ready,
    call,
    state: async () => (await fetch(ready.hook + "/state")).json(),
    stop: async () => {
      if (proc.exitCode === null) {
        const done = new Promise((r) => proc.once("exit", r));
        proc.kill("SIGTERM");
        await done;
      }
      rmSync(dir, { recursive: true, force: true });
    },
  };
}

export const test = base.extend<{
  foreman: Foreman;
  foremanEnv: Record<string, string>;
  // Opt in with test.use({ fakeGameOn: true }): Foreman then gets the fake as its Docker host.
  fakeGameOn: boolean;
  fakeGame: FakeGame | undefined;
}>({
  foremanEnv: [{}, { option: true }],
  fakeGameOn: [false, { option: true }],
  fakeGame: async ({ fakeGameOn }, use) => {
    if (!fakeGameOn) {
      await use(undefined);
      return;
    }
    const g = await startFakeGame();
    await use(g);
    await g.stop();
  },
  foreman: async ({ foremanEnv, fakeGame }, use) => {
    const env = fakeGame ? { DOCKER_HOST: "unix://" + fakeGame.socket, ...foremanEnv } : foremanEnv;
    const f = await startForeman(env);
    await use(f);
    await f.stop();
  },
  baseURL: async ({ foreman }, use) => use(foreman.url),
});

export { expect };

// Every page visited passes these (SPEC 17.1 and 17.2, DESIGN 10).
export async function checkPage(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(wcagTags).analyze();
  expect(results.violations, `axe on ${page.url()}`).toEqual([]);
  if ((page.viewportSize()?.width ?? 1000) > 320) return;

  const size = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(size.scroll, `horizontal scroll on ${page.url()}`).toBeLessThanOrEqual(size.client);

  // DESIGN 8.3: a date, a time or a size never breaks inside itself.
  const broken = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>(".nowrap, td.num")]
      .filter((el) => {
        const range = document.createRange();
        range.selectNodeContents(el);
        const tops = new Set([...range.getClientRects()].filter((r) => r.width > 0).map((r) => Math.round(r.top)));
        return tops.size > 1;
      })
      .map((el) => el.textContent),
  );
  expect(broken, `text that wraps inside itself on ${page.url()}`).toEqual([]);

  const targets = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>("a, button, input, select, textarea")]
      .filter((el) => {
        const r = el.getBoundingClientRect();
        const s = getComputedStyle(el);
        // The skip link is clipped until focused; the keyboard test measures it then.
        return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.clipPath === "none";
      })
      .map((el) => {
        // A radio or checkbox inside its label is tapped through the label (DESIGN 7.2).
        const box = el instanceof HTMLInputElement ? (el.closest("label") ?? el) : el;
        const r = box.getBoundingClientRect();
        return { what: el.outerHTML.slice(0, 80), w: r.width, h: r.height };
      }),
  );
  for (const t of targets) {
    const least = Math.min(t.w, t.h);
    // SPEC 17.2 sets 24 px as the floor.
    expect(least, `target size of ${t.what} on ${page.url()}`).toBeGreaterThanOrEqual(24);
    // DESIGN asks for 44 px; a smaller target is reported, not failed.
    if (least < 44) {
      test.info().annotations.push({
        type: "finding",
        description: `${new URL(page.url()).pathname}: ${t.what} is ${Math.round(t.w)} by ${Math.round(t.h)} px, under 44`,
      });
    }
  }
}
