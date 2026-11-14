/// <reference types="node" />
import { test as base, expect, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { spawn, type ChildProcess } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { createInterface } from "node:readline";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { crc32 } from "node:zlib";

// Shared by every spec that needs a Foreman process of its own.

const binary = join(import.meta.dirname, "..", "..", "bin", "foreman");
const fakeGameBinary = join(import.meta.dirname, "..", "..", "bin", "fakegame");
const fakePortalBinary = join(import.meta.dirname, "..", "..", "bin", "fakeportal");

// SPEC 17.1 names WCAG 2.2 AA.
export const wcagTags = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

// Resolves with the port Foreman reports in its "listening" log line, which
// it logs only after the bind succeeded. The caller listens on port 0, so no
// other process can have taken the port in between.
export function listeningPort(proc: ChildProcess): Promise<number> {
  return new Promise((resolve, reject) => {
    // A plain listener, not readline: closing a readline pauses the stream, which would stall the caller's own listener.
    let pending = "";
    const timer = setTimeout(() => done(new Error("foreman did not log that it was listening")), 10_000);
    const done = (err?: Error, port?: number) => {
      clearTimeout(timer);
      proc.stdout!.off("data", onData);
      proc.stdout!.off("end", onEnd);
      if (err) reject(err);
      else resolve(port!);
    };
    const onData = (d: Buffer) => {
      const lines = (pending + d).split("\n");
      pending = lines.pop()!;
      for (const line of lines) {
        let entry: { msg?: string; addr?: string };
        try {
          entry = JSON.parse(line);
        } catch {
          continue;
        }
        if (entry.msg === "listening" && entry.addr) return done(undefined, Number(entry.addr.slice(entry.addr.lastIndexOf(":") + 1)));
      }
    };
    // Foreman ended without ever listening; the caller reports its output.
    const onEnd = () => done(new Error("foreman exited before it listened"));
    proc.stdout!.on("data", onData);
    proc.stdout!.on("end", onEnd);
  });
}

export interface Foreman {
  url: string;
  dataDir: string;
  logs: () => string;
  // Declares an ERROR log line this test causes on purpose. It matches the raw
  // JSON line, by substring or regexp. The test fails if no line matches.
  // An ERROR from background work must be waited for in the test (for example
  // until the page shows the failure) before the test ends.
  expectError: (match: string | RegExp) => void;
}

// Starts the real binary on a port the system picks over a fresh data directory, so each
// test sees a first run of its own.
async function startForeman(env: Record<string, string>): Promise<Foreman & { stop: () => Promise<{ exitedEarly: boolean }>; errorLines: () => string[] }> {
  const dataDir = mkdtempSync(join(tmpdir(), "foreman-e2e-"));
  let out = "";
  const proc = spawn(binary, ["serve"], {
    env: {
      ...process.env,
      FOREMAN_DATA_DIR: dataDir,
      FOREMAN_GAME_UID: String(process.getuid?.() ?? 0),
      FOREMAN_GAME_GID: String(process.getgid?.() ?? 0),
      // The host path is given, so no Docker call is made.
      FOREMAN_HOST_DATA_DIR: dataDir,
      FOREMAN_LISTEN: "127.0.0.1:0",
      // Nothing in these tests may reach Docker Hub or factorio.com; port 9 refuses the connection.
      HTTPS_PROXY: "http://127.0.0.1:9",
      HTTP_PROXY: "http://127.0.0.1:9",
      ...env,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  proc.stdout.on("data", (d) => (out += d));
  proc.stderr.on("data", (d) => (out += d));
  // "close" comes after the output streams end, so the last lines are in `out`.
  const closed = new Promise((r) => proc.once("close", r));
  const hasExited = () => proc.exitCode !== null || proc.signalCode !== null;
  const port = await listeningPort(proc).catch((e: Error) => {
    proc.kill("SIGTERM");
    throw new Error(`${e.message}:\n${out}`);
  });
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
    expectError: () => {}, // replaced by the fixture, which owns the list
    errorLines: () => out.split("\n").filter((line) => /"level":"ERROR"/.test(line)),
    // Reports whether Foreman was already gone, which is a crash.
    stop: async () => {
      const exitedEarly = hasExited();
      if (!exitedEarly) proc.kill("SIGTERM");
      await closed;
      rmSync(dataDir, { recursive: true, force: true });
      return { exitedEarly };
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
    containers: { name: string; image: string; status: string; ip: string; players: string[]; commands: string[]; restart_count: number }[];
    calls: string[];
  }>;
}

// The fake seeds a container for the one Foreman runs in, on this network,
// so Foreman can check the game network as it does in a real install (SPEC 14.4).
const fakeGameNetwork = "foreman-games";

async function startFakeGame(): Promise<FakeGame & { stop: () => Promise<void> }> {
  const dir = mkdtempSync(join(tmpdir(), "fakegame-e2e-"));
  const proc = spawn(fakeGameBinary, ["-dir", dir, "-self-network", fakeGameNetwork], { stdio: ["ignore", "pipe", "pipe"] });
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

// A fake Factorio mod portal (cmd/fakeportal). It is an HTTPS proxy: Foreman gets
// HTTPS_PROXY and SSL_CERT_FILE, so its unchanged portal client reaches the fake
// as mods.factorio.com, and the proxy refuses every other host. The hooks are
// listed at controlHandler in cmd/fakeportal/control.go.
export interface FakePortal {
  proxy: string;
  // PEM file of the test certificate authority.
  caFile: string;
  // The fake's own made-up factorio.com credentials; a download needs them.
  username: string;
  token: string;
  // Adds a release of a mod. The mod then shows in search, and its /full reply
  // lists only the releases added for it (a recorded mod of the same name is hidden).
  // The release is for game line "2.0" unless line says another, such as "2.1".
  add: (name: string, version: string, deps?: string[], line?: string) => Promise<{ file_name: string; download_url: string; sha1: string }>;
  // How a release downloads: normal, direct, wrong-sha1, unauthorized,
  // redirect-login, redirect-foreign, server-error, slow, oversized.
  setMode: (name: string, version: string, mode: string) => Promise<void>;
  // Every /api reply carries this status; 0 restores them.
  failApi: (status: number) => Promise<void>;
  // The proxy refuses every tunnel with 503 while true.
  setDown: (down: boolean) => Promise<void>;
  // Requests seen inside tunnels (host and path, never the query), tunnels
  // opened, and the hosts the proxy refused.
  state: () => Promise<PortalState>;
  // How many requests were for this host and a path starting with pathPrefix.
  count: (host: string, pathPrefix: string) => Promise<number>;
}

interface PortalState {
  requests: { host: string; path: string }[];
  connects: number;
  refused: string[];
}

async function startFakePortal(): Promise<FakePortal & { stop: () => Promise<void> }> {
  const dir = mkdtempSync(join(tmpdir(), "fakeportal-e2e-"));
  const proc = spawn(fakePortalBinary, ["-dir", dir], { stdio: ["ignore", "pipe", "pipe"] });
  let err = "";
  proc.stderr.on("data", (d) => (err += d));
  const lines = createInterface({ input: proc.stdout })[Symbol.asyncIterator]();
  const timeout = new Promise<never>((_, reject) =>
    setTimeout(() => reject(new Error(`fakeportal did not start:\n${err}`)), 10_000),
  );
  const exited = new Promise<never>((_, reject) =>
    proc.once("exit", (code) => reject(new Error(`fakeportal exited ${code}:\n${err}`))),
  );
  const first = await Promise.race([lines.next(), timeout, exited]);
  const ready = JSON.parse(first.value as string) as { proxy: string; ca: string; control: string; username: string; token: string };
  const post = async (path: string, body: Record<string, unknown>) => {
    const resp = await fetch(ready.control + path, { method: "POST", body: JSON.stringify(body) });
    if (!resp.ok) throw new Error(`fakeportal ${path} ${JSON.stringify(body)}: ${resp.status} ${await resp.text()}`);
    return resp.json();
  };
  const state = async (): Promise<PortalState> => (await fetch(ready.control + "/state")).json();
  return {
    proxy: ready.proxy,
    caFile: ready.ca,
    username: ready.username,
    token: ready.token,
    add: (name, version, deps = [], line) => post("/add", { name, version, deps, line }),
    setMode: async (name, version, mode) => void (await post("/mode", { name, version, mode })),
    failApi: async (status) => void (await post("/fail", { status })),
    setDown: async (down) => void (await post("/down", { down })),
    state,
    count: async (host, pathPrefix) => (await state()).requests.filter((r) => r.host === host && r.path.startsWith(pathPrefix)).length,
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
  // Opt in with test.use({ fakePortalOn: true }): Foreman then reaches the fake mod portal through its proxy.
  fakePortalOn: boolean;
  fakePortal: FakePortal | undefined;
  // Opt in with test.use({ portalAccountOn: true }) beside fakePortalOn: Foreman then starts with the fake's
  // factorio.com account in its environment (SPEC 9.11), so mod downloads are on.
  portalAccountOn: boolean;
}>({
  foremanEnv: [{}, { option: true }],
  fakeGameOn: [false, { option: true }],
  fakePortalOn: [false, { option: true }],
  portalAccountOn: [false, { option: true }],
  fakePortal: async ({ fakePortalOn }, use) => {
    if (!fakePortalOn) {
      await use(undefined);
      return;
    }
    const p = await startFakePortal();
    await use(p);
    await p.stop();
  },
  fakeGame: async ({ fakeGameOn }, use) => {
    if (!fakeGameOn) {
      await use(undefined);
      return;
    }
    const g = await startFakeGame();
    await use(g);
    await g.stop();
  },
  foreman: async ({ foremanEnv, fakeGame, fakePortal, portalAccountOn }, use, testInfo) => {
    const env = {
      ...(fakeGame ? { DOCKER_HOST: "unix://" + fakeGame.socket, FOREMAN_GAME_NETWORK: fakeGameNetwork } : {}),
      // Loopback addresses are never proxied, so only the portal's hosts go through.
      ...(fakePortal ? { HTTPS_PROXY: fakePortal.proxy, SSL_CERT_FILE: fakePortal.caFile } : {}),
      ...(fakePortal && portalAccountOn ? { FOREMAN_FACTORIO_USERNAME: fakePortal.username, FOREMAN_FACTORIO_TOKEN: fakePortal.token } : {}),
      ...foremanEnv,
    };
    const f = await startForeman(env);
    const expected: (string | RegExp)[] = [];
    f.expectError = (match) => expected.push(match);
    await use(f);
    const { exitedEarly } = await f.stop();
    expect(exitedEarly, `Foreman exited before the test ended. Last output:\n${f.logs().slice(-2000)}`).toBe(false);
    // docs/WORKFLOW.md: a test fails on any ERROR line it did not declare, and on a
    // declaration that matched nothing. This process belongs to this test alone.
    const matches = (line: string, m: string | RegExp) => (typeof m === "string" ? line.includes(m) : m.test(line));
    const lines = f.errorLines();
    const unexpected = lines.filter((line) => !expected.some((m) => matches(line, m)));
    expect(unexpected, "Foreman wrote ERROR log lines the test did not declare with foreman.expectError").toEqual([]);
    // A test that already failed may have stopped before the error came, so only a passing test must show it.
    if (testInfo.status === "passed") {
      const missing = expected.filter((m) => !lines.some((line) => matches(line, m))).map(String);
      expect(missing, "foreman.expectError declared lines Foreman never wrote").toEqual([]);
    }
  },
  baseURL: async ({ foreman }, use) => use(foreman.url),
});

export { expect };

const repo = join(import.meta.dirname, "..", "..");

// The image Foreman runs for 2.1.24 worlds: the exact tag, with the Makefile's digest left off (SPEC 10.1).
export const image21 = /^GAME_IMAGE_2_1\s*:=\s*([^\s@]+)/m.exec(readFileSync(join(repo, "Makefile"), "utf8"))![1];

// The same for 2.0 worlds, and the game version it names. The fake portal's generated mods are made for 2.0.
export const image20 = /^GAME_IMAGE_2_0\s*:=\s*([^\s@]+)/m.exec(readFileSync(join(repo, "Makefile"), "utf8"))![1];
export const version20 = image20.split(":")[1];

// What the Server page and the click budgets read: the state badge, the "Done:" box, and the wait for a slow step.
export const live = { timeout: 20_000 };
export const stateBadge = (page: Page) => page.locator("main h1 .state");
export const doneBox = (page: Page) => page.locator(".message--success").filter({ hasText: "Done:" });

// How many game containers the fake has been asked to create, so a test sees a game start again.
export const creates = async (g: FakeGame) =>
  (await g.state()).calls.filter((c) => c.startsWith("POST ") && c.includes("/containers/create")).length;

export const adminPassword = "correct horse battery";

// Creates the first admin, "admin", through the setup page; it lands on the
// Server page.
export async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill(adminPassword);
  await page.getByLabel("Confirm password").fill(adminPassword);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
}

// Opens the user menu at the top right, which holds the language row, the
// theme row and Sign out (DESIGN 6, "User menu").
export async function openMenu(page: Page) {
  const menu = page.locator("details.menu");
  if (!(await menu.evaluate((el: HTMLDetailsElement) => el.open))) await menu.locator(":scope > summary").click();
}

// Signs out through the user menu: two clicks (DESIGN 6).
export async function signOut(page: Page, name = "Sign out") {
  await openMenu(page);
  await page.getByRole("button", { name }).click();
}

// Opens the user menu's language row, which lists the other seven languages
// (DESIGN 6, "User menu").
export async function openLanguages(page: Page) {
  await openMenu(page);
  const langs = page.locator("details.menu__langs");
  if (!(await langs.evaluate((el: HTMLDetailsElement) => el.open))) await langs.locator("summary").click();
}

// Picks a language in the user menu by its tag, such as "de": one of the
// seven submit buttons to POST /language under the language row (SPEC 22.2).
export async function pickLanguage(page: Page, tag: string) {
  await openLanguages(page);
  await page.locator(`details.menu button[name="lang"][value="${tag}"]`).click();
}

// The main navigation's tabs (DESIGN 6).
export function mainNav(page: Page) {
  return page.getByRole("navigation", { name: "Main" });
}

// A zip of stored (uncompressed) entries, enough for Foreman to read its directory.
export function makeZip(entries: Record<string, string>): Buffer {
  const parts: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const [name, text] of Object.entries(entries)) {
    const nameBuf = Buffer.from(name);
    const body = Buffer.from(text);
    const crc = crc32(body);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(33, 12); // 1980-01-01
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(body.length, 18);
    local.writeUInt32LE(body.length, 22);
    local.writeUInt16LE(nameBuf.length, 26);
    parts.push(local, nameBuf, body);

    const head = Buffer.alloc(46);
    head.writeUInt32LE(0x02014b50, 0);
    head.writeUInt16LE(20, 4);
    head.writeUInt16LE(20, 6);
    head.writeUInt16LE(33, 14);
    head.writeUInt32LE(crc, 16);
    head.writeUInt32LE(body.length, 20);
    head.writeUInt32LE(body.length, 24);
    head.writeUInt16LE(nameBuf.length, 28);
    head.writeUInt32LE(offset, 42);
    central.push(head, nameBuf);
    offset += local.length + nameBuf.length + body.length;
  }
  const dirBytes = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(central.length / 2, 8);
  end.writeUInt16LE(central.length / 2, 10);
  end.writeUInt32LE(dirBytes.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...parts, dirBytes, end]);
}

// A mod zip to seed into the world's mods folder: factorio is the game line its
// info.json declares; the version defaults to 1.0.0.
export interface SeedMod {
  factorio: string;
  version?: string;
  enabled?: boolean;
  deps?: string[];
}

// Makes the active world "base", a 2.1.24 world unless version says otherwise,
// holding one real save (SPEC 5.1: a world is a folder a person can make by
// hand), with a game password when one is given.
export function seedWorld(
  f: Foreman,
  // mods maps the name of each mod to the Factorio version its zip declares, or to a SeedMod.
  opts: { fixture?: string; spaceAge?: boolean; gamePassword?: string; mods?: Record<string, string | SeedMod>; version?: string } = {},
) {
  const version = opts.version ?? "2.1.24";
  const dir = join(f.dataDir, "worlds", "base");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name: "Base", game_version: version, space_age: opts.spaceAge ?? false, created_at: "2026-11-01T10:00:00Z" }),
  );
  if (opts.gamePassword) {
    writeFileSync(join(dir, "config", "server-settings.json"), JSON.stringify({ name: "Base", description: "", visibility: { public: false, lan: false }, game_password: opts.gamePassword }));
  }
  if (opts.mods) {
    const spaceAgeOn = opts.spaceAge ?? false;
    // The 2.1 line has the always-on built-in mod "recycler"; 2.0 does not.
    const list = [
      { name: "base", enabled: true },
      ...(version.startsWith("2.1") ? [{ name: "recycler", enabled: true }] : []),
      ...["elevated-rails", "quality", "space-age"].map((name) => ({ name, enabled: spaceAgeOn })),
    ];
    for (const [name, seed] of Object.entries(opts.mods)) {
      const { factorio, version: modVersion = "1.0.0", enabled = true, deps } = typeof seed === "string" ? ({ factorio: seed } as SeedMod) : seed;
      const info = JSON.stringify({ name, version: modVersion, factorio_version: factorio, ...(deps ? { dependencies: deps } : {}) });
      writeFileSync(join(dir, "mods", `${name}_${modVersion}.zip`), makeZip({ [`${name}_${modVersion}/info.json`]: info }));
      list.push({ name, enabled });
    }
    writeFileSync(join(dir, "mods", "mod-list.json"), JSON.stringify({ mods: list }));
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

// Makes a second, inactive world "second" named "Second", written as a
// folder (SPEC 5.1), with one save an hour old.
// playedAt is when the save was written, the world's "Last played"; the default is an hour ago.
export function seedSecond(f: Foreman, opts: { fixture?: string; spaceAge?: boolean; version?: string; playedAt?: Date } = {}) {
  const { fixture = "2.1.24-no-space-age.zip", spaceAge = false, version = "2.1.24" } = opts;
  const dir = join(f.dataDir, "worlds", "second");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name: "Second", game_version: version, space_age: spaceAge, created_at: "2026-11-02T10:00:00Z" }),
  );
  const save = join(dir, "saves", "base.zip");
  copyFileSync(join(repo, "internal", "savefile", "testdata", fixture), save);
  const at = opts.playedAt ?? new Date(Date.now() - 3_600_000);
  utimesSync(save, at, at);
}

// The mods tests' start: signs in as the admin, seeds the active world "base" and its game image, and
// ends on the Server page. With start, the game is started and the page shows it running.
export async function setUpModsWorld(page: Page, f: Foreman, g: FakeGame, opts: Parameters<typeof seedWorld>[1] & { start?: boolean } = {}) {
  await createAdmin(page);
  // A mod list is always written, so the world's mods show as the game would load them.
  seedWorld(f, { mods: {}, ...opts });
  await g.call("/image", { ref: opts.version?.startsWith("2.0") ? image20 : image21, on: true });
  await page.goto("/");
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  if (!opts.start) return;
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
}

// After a switch the Server page loads again; the sentence is
// read out on the new page and focus is on the control heading (DESIGN 5, 7.5).
export async function expectFollowed(page: Page, sentence: string) {
  await expect(page.locator("#announcer")).toContainText(sentence, { timeout: 5000 });
  expect(await page.evaluate(() => document.activeElement?.id)).toBe("control-heading");
}

// The Snapshots selection bar and Generate's action bar stick to the window's
// foot (DESIGN 8.3, 8.6), so at some scroll positions one lies over a row's
// control and axe's target-size rule counts that control as partly covered.
// That is not a barrier: a pointer user scrolls the control out from under the
// bar, and the page's scroll-padding keeps a focused control clear of it
// (WCAG 2.4.11, tested per bar in sticky-bars.spec.ts). So a target-size
// finding is dropped only when axe says the control is obscured rather than
// small, the control's own box is at least 24 by 24 px, and it lies under one
// of those bars while the bar is sticky. Every other finding still fails.
// Todd approved this exception on 2026-11-13.
const stickyBars = ".select-bar, .generate > .gen-actions";
type AxeCheck = { id: string; data?: unknown };
type AxeNode = { target: unknown[]; any?: AxeCheck[]; all?: AxeCheck[]; none?: AxeCheck[] };
type AxeViolation = { id: string; nodes: AxeNode[] };

// axe-core 4.14 marks a target that is big enough but partly covered with
// this key on its target-size check; a target that is simply too small has
// none. The node's target-offset check is axe's other way to pass (enough
// space around a small target), so it says nothing about why size failed.
function onlyObscured(n: AxeNode): boolean {
  const sizeChecks = [...(n.any ?? []), ...(n.all ?? []), ...(n.none ?? [])].filter((c) => c.id === "target-size");
  return sizeChecks.length > 0 && sizeChecks.every((c) =>
    typeof c.data === "object" && c.data !== null && (c.data as { messageKey?: unknown }).messageKey === "partiallyObscured");
}

async function withoutTargetsUnderStickyBars<V extends AxeViolation>(page: Page, violations: V[]): Promise<V[]> {
  const kept: V[] = [];
  for (const v of violations) {
    if (v.id !== "target-size") { kept.push(v); continue; }
    const nodes = [];
    for (const n of v.nodes) {
      const selector = n.target.length === 1 && typeof n.target[0] === "string" ? n.target[0] : null;
      const covered = selector !== null && onlyObscured(n) && await page.evaluate(([sel, bars]) => {
        const el = document.querySelector(sel);
        if (!el || el.closest(bars)) return false;
        const r = el.getBoundingClientRect();
        if (r.width < 24 || r.height < 24) return false;
        return [...document.querySelectorAll(bars)].some((bar) => {
          if (getComputedStyle(bar).position !== "sticky") return false;
          const b = bar.getBoundingClientRect();
          return b.width > 0 && r.left < b.right && r.right > b.left && r.top < b.bottom && r.bottom > b.top;
        });
      }, [selector, stickyBars] as const);
      if (!covered) nodes.push(n);
    }
    if (nodes.length) kept.push({ ...v, nodes });
  }
  return kept;
}

// Every page visited passes these (SPEC 17.1 and 17.2, DESIGN 10).
export async function checkPage(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(wcagTags).analyze();
  const violations = await withoutTargetsUnderStickyBars(page, results.violations);
  expect(violations, `axe on ${page.url()}`).toEqual([]);
  if ((page.viewportSize()?.width ?? 1000) > 320) return;

  const size = await page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    client: document.documentElement.clientWidth,
  }));
  expect(size.scroll, `horizontal scroll on ${page.url()}`).toBeLessThanOrEqual(size.client);

  // DESIGN 8.3: a date, a clock, a zone or a size never breaks inside itself. The
  // zone, such as "GMT+5:45", is its own .nowrap unit and may take a line to itself.
  const broken = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>(".nowrap, td.num")]
      .filter((el) => {
        const range = document.createRange();
        range.selectNodeContents(el);
        return new Set([...range.getClientRects()].filter((r) => r.width > 0).map((r) => Math.round(r.top))).size > 1;
      })
      .map((el) => el.textContent),
  );
  expect(broken, `text that wraps inside itself on ${page.url()}`).toEqual([]);

  // A button's label stays inside its box at large text and long translations (SPEC 17.2, 22.7).
  const spill = await page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>("button, a.button")]
      .filter((el) => {
        const box = el.getBoundingClientRect();
        if (box.width === 0) return false;
        const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          if (!n.textContent?.trim() || n.parentElement?.closest(".visually-hidden")) continue;
          const range = document.createRange();
          range.selectNodeContents(n);
          for (const r of range.getClientRects()) {
            if (r.width > 0 && (r.right > box.right + 0.5 || r.left < box.left - 0.5)) return true;
          }
        }
        return false;
      })
      .map((el) => `${el.textContent?.trim()} (${Math.round(el.getBoundingClientRect().width)} px wide, ${el.scrollWidth} px of text)`),
  );
  expect(spill, `button text that runs past its button on ${page.url()}`).toEqual([]);

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

// Moves focus to a control with the Tab key alone, so a test shows that the
// control can be reached from the keyboard. Gives up after `limit` presses.
export async function tabTo(page: Page, target: Locator, limit = 150) {
  const handle = await target.elementHandle();
  if (!handle) throw new Error("tabTo: no such element");
  for (let i = 0; i < limit; i++) {
    if (await handle.evaluate((el) => el === document.activeElement)) return;
    await page.keyboard.press("Tab");
  }
  throw new Error(`tabTo: not reached in ${limit} Tab presses`);
}

// On a phone Generate's planet rows start closed, so the preview sits near
// the top (DESIGN 8.6); this opens a planet's row, in every tab, where it is
// closed. It sends no preview for the planet already shown.
export async function openPlanetRow(page: Page, planet = "nauvis") {
  const row = page.locator(`#mg-resources-${planet}`);
  if (!(await row.evaluate((d) => (d as HTMLDetailsElement).open))) await row.locator("summary").click();
  await expect(row).toHaveJSProperty("open", true);
}

// The rarer Mods tools (match to the save, upload, mod settings) sit in a
// disclosure that is closed on a phone and always open on a wide window.
export async function openModTools(page: Page) {
  const tools = page.locator("#mod-tools");
  // A wide window opens it by script, so wait for that instead of clicking.
  if ((page.viewportSize()?.width ?? 1280) >= 1024) return expect(tools).toHaveJSProperty("open", true);
  if (await tools.evaluate((el) => (el as HTMLDetailsElement).open)) return;
  await page.locator("#mod-tools > summary").click();
  await expect(tools).toHaveJSProperty("open", true);
}
