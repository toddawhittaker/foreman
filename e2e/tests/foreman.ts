/// <reference types="node" />
import { test as base, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { spawn } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { createInterface } from "node:readline";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

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

// The fake seeds a container for the one Foreman runs in, on this network,
// so Foreman can check the game network as it does in a real install (SPEC 14.4).
const fakeGameNetwork = "foreman-games";

async function startFakeGame(extraArgs: string[] = []): Promise<FakeGame & { stop: () => Promise<void> }> {
  const dir = mkdtempSync(join(tmpdir(), "fakegame-e2e-"));
  const proc = spawn(fakeGameBinary, ["-dir", dir, "-self-network", fakeGameNetwork, ...extraArgs], { stdio: ["ignore", "pipe", "pipe"] });
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

// Adopted mode (SPEC 4.7): the game container was there before Foreman, and
// the game folder is bind-mounted into both.
export interface AdoptedSetup {
  // What the game folder holds before the first adoption.
  folder: "unadopted" | "broken";
  // Added to the container's environment.
  env?: string[];
  // Publishes the container's RCON port on this host port.
  rconHostPort?: string;
  // The container is running when Foreman starts.
  running?: boolean;
}

// The name of the container the fake seeds in adopted mode.
export const adoptedContainer = "factorio";

// Lays out a game folder the way the image leaves it (unadopted: real saves,
// mods and config, with one save) or in a shape Foreman refuses.
function prepareGameDir(setup: AdoptedSetup): string {
  const dir = mkdtempSync(join(tmpdir(), "foreman-gamedir-"));
  if (setup.folder === "broken") {
    writeFileSync(join(dir, "saves"), "not a folder");
    return dir;
  }
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub));
  const save = join(dir, "saves", "base.zip");
  copyFileSync(join(repo, "internal", "savefile", "testdata", "2.1.24-no-space-age.zip"), save);
  const at = new Date(Date.now() - 3_600_000);
  utimesSync(save, at, at);
  writeFileSync(join(dir, "config", "rconpw"), "pw-original\n", { mode: 0o640 });
  return dir;
}

export const test = base.extend<{
  foreman: Foreman;
  foremanEnv: Record<string, string>;
  // Opt in with test.use({ fakeGameOn: true }): Foreman then gets the fake as its Docker host.
  fakeGameOn: boolean;
  fakeGame: FakeGame | undefined;
  // Opt in with test.use({ adoptedOn: {...} }): Foreman runs in adopted mode on a
  // seeded container, and the fake game is on without fakeGameOn.
  adoptedOn: AdoptedSetup | undefined;
  // The game folder in adopted mode.
  gameDir: string | undefined;
}>({
  foremanEnv: [{}, { option: true }],
  fakeGameOn: [false, { option: true }],
  adoptedOn: [undefined, { option: true }],
  gameDir: async ({ adoptedOn }, use) => {
    if (!adoptedOn) {
      await use(undefined);
      return;
    }
    const dir = prepareGameDir(adoptedOn);
    await use(dir);
    rmSync(dir, { recursive: true, force: true });
  },
  fakeGame: async ({ fakeGameOn, adoptedOn, gameDir }, use) => {
    if (!fakeGameOn && !adoptedOn) {
      await use(undefined);
      return;
    }
    const args: string[] = [];
    if (adoptedOn) {
      // The image's PUID and PGID must be the user Foreman runs the game as (SPEC 3.6).
      const ids = [`PUID=${process.getuid?.() ?? 0}`, `PGID=${process.getgid?.() ?? 0}`];
      args.push(
        "-adopted",
        JSON.stringify({
          name: adoptedContainer,
          gameDir,
          env: ["DLC_SPACE_AGE=false", ...ids, ...(adoptedOn.env ?? [])],
          rconHostPort: adoptedOn.rconHostPort,
          running: adoptedOn.running ?? false,
        }),
      );
    }
    const g = await startFakeGame(args);
    await use(g);
    await g.stop();
  },
  foreman: async ({ foremanEnv, fakeGame, gameDir }, use) => {
    const adopted: Record<string, string> = gameDir
      ? { FOREMAN_MODE: "adopted", FOREMAN_GAME_CONTAINER: adoptedContainer, FOREMAN_GAME_DIR: gameDir }
      : {};
    const env = fakeGame
      ? { DOCKER_HOST: "unix://" + fakeGame.socket, FOREMAN_GAME_NETWORK: fakeGameNetwork, ...adopted, ...foremanEnv }
      : foremanEnv;
    const f = await startForeman(env);
    await use(f);
    await f.stop();
  },
  baseURL: async ({ foreman }, use) => use(foreman.url),
});

export { expect };

const repo = join(import.meta.dirname, "..", "..");

// The image Foreman runs for 2.1 worlds, read from the one place it is pinned.
export const image21 = /^GAME_IMAGE_2_1\s*:=\s*(\S+)\s*$/m.exec(readFileSync(join(repo, "Makefile"), "utf8"))![1];

export const adminPassword = "correct horse battery";

// Creates the first admin, "admin", through the setup page; it lands on the
// Server page (managed mode has server control).
export async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(adminPassword);
  await page.getByLabel("Confirm password").fill(adminPassword);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
}

// Makes the active world "base", a 2.1 world holding one real save (SPEC
// 5.1: a world is a folder a person can make by hand), with a game password
// when one is given.
export function seedWorld(
  f: Foreman,
  opts: { fixture?: string; spaceAge?: boolean; gamePassword?: string } = {},
) {
  const dir = join(f.dataDir, "worlds", "base");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name: "Base", game_version: "2.1.24", space_age: opts.spaceAge ?? false, created_at: "2026-11-01T10:00:00Z" }),
  );
  if (opts.gamePassword) {
    writeFileSync(join(dir, "config", "server-settings.json"), JSON.stringify({ name: "Base", game_password: opts.gamePassword }));
  }
  const save = join(dir, "saves", "base.zip");
  copyFileSync(join(repo, "internal", "savefile", "testdata", opts.fixture ?? "2.1.24-no-space-age.zip"), save);
  const at = new Date(Date.now() - 3_600_000);
  utimesSync(save, at, at);
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    db.prepare("INSERT INTO active_worlds (world_id, since) VALUES (?, ?)").run("base", 0);
  } finally {
    db.close();
  }
}

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
