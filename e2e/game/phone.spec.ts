/// <reference types="node" />
import { test, expect, type Page } from "@playwright/test";
import { checkPage, listeningPort, stateBadge } from "../tests/foreman.js";
import { spawn } from "node:child_process";
import { copyFileSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, utimesSync, writeFileSync } from "node:fs";
import { request } from "node:http";
import { join } from "node:path";
import { randomBytes } from "node:crypto";
import { DatabaseSync } from "node:sqlite";

// The vision's phone scenario against the real 2.1 game: see who is on, warn
// them, restart, and switch to another world and back with no save lost
// (VISION "Success looks like", SPEC 7, 7.9, 8.1, 19.1). It runs through
// `make e2e-game`, inside the Playwright image with the host's Docker socket.
// No game client can join, so the list is only ever seen empty.

const password = "correct horse battery";
const repo = join(import.meta.dirname, "..", "..");
const dockerSocket = process.env.DOCKER_HOST?.replace(/^unix:\/\//, "") || "/var/run/docker.sock";

// The image Foreman runs for a 2.1.24 world: the exact tag. make e2e-game has checked that the
// tag on this host is the Makefile's pinned digest.
const image21 = /^GAME_IMAGE_2_1\s*:=\s*([^\s@]+)/m.exec(readFileSync(join(repo, "Makefile"), "utf8"))![1];

// Not a world id anyone would pick, so the container is foreman-e3game-<random>.
const worldID = "e3game-" + randomBytes(3).toString("hex");
const container = "foreman-" + worldID;
// A second world for the switch scenario; its container shares the prefix make e2e-game cleans up.
const worldBID = worldID + "-b";
const containerB = "foreman-" + worldBID;
// The path Foreman gives Docker for binds, so it must be the host's path:
// the Makefile mounts the repository at its own path.
const dataDir = join(repo, ".cache", "e2e-game", worldID);

// The game takes a while to load a map and to answer on RCON.
const gameTimeout = { timeout: 180_000 };

// One call to the Docker API over the socket.
function dockerCall(method: string, path: string): Promise<{ status: number; body: string }> {
  return new Promise((resolve, reject) => {
    const req = request({ socketPath: dockerSocket, method, path }, (res) => {
      let body = "";
      res.on("data", (d) => (body += d));
      res.on("end", () => resolve({ status: res.statusCode ?? 0, body }));
    });
    req.on("error", reject);
    req.end();
  });
}

// A container that is gone is not running.
async function isRunning(name: string): Promise<boolean> {
  const r = await dockerCall("GET", `/containers/${name}/json`);
  return r.status === 200 && JSON.parse(r.body).State.Running === true;
}

async function removeContainer(name = container) {
  // Not found is fine: the test may have failed before it was made.
  const r = await dockerCall("DELETE", `/containers/${name}?force=true&v=true`);
  if (r.status !== 204 && r.status !== 404) throw new Error(`could not remove ${name}: ${r.status} ${r.body}`);
}

async function startForeman() {
  mkdirSync(dataDir, { recursive: true });
  let out = "";
  const proc = spawn(join(repo, "bin", "foreman"), ["serve"], {
    env: {
      ...process.env,
      FOREMAN_DATA_DIR: dataDir,
      FOREMAN_HOST_DATA_DIR: dataDir,
      FOREMAN_GAME_UID: String(process.getuid?.() ?? 0),
      FOREMAN_GAME_GID: String(process.getgid?.() ?? 0),
      // The Playwright container is on this run's own network, which the game joins too.
      FOREMAN_GAME_NETWORK: process.env.FOREMAN_E2E_GAME_NETWORK ?? "",
      FOREMAN_LISTEN: "127.0.0.1:0",
      HTTPS_PROXY: "http://127.0.0.1:9",
      HTTP_PROXY: "http://127.0.0.1:9",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  proc.stdout.on("data", (d) => (out += d));
  proc.stderr.on("data", (d) => (out += d));
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
    logs: () => out,
    stop: async () => {
      if (proc.exitCode === null) {
        const exited = new Promise((r) => proc.once("exit", r));
        proc.kill("SIGTERM");
        await exited;
      }
    },
  };
}

// A world holding one real 2.1 save made without Space Age (SPEC 5.1). The
// active one is also recorded as active.
function seedWorld(id = worldID, name = "Phone", active = true) {
  const dir = join(dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name, game_version: "2.1.24", space_age: false, created_at: "2026-11-01T10:00:00Z" }),
  );
  const save = join(dir, "saves", id === worldID ? "phone.zip" : "other.zip");
  copyFileSync(join(repo, "internal", "savefile", "testdata", "2.1.24-no-space-age.zip"), save);
  const at = new Date(Date.now() - 3_600_000);
  utimesSync(save, at, at);
  if (!active) return;
  const db = new DatabaseSync(join(dataDir, "foreman.db"));
  try {
    db.prepare("INSERT INTO active_worlds (world_id, since) VALUES (?, ?)").run(id, 0);
  } finally {
    db.close();
  }
}

// The newest save's modification time, in milliseconds.
function newestSave(): number {
  const saves = join(dataDir, "worlds", worldID, "saves");
  return Math.max(...readdirSync(saves).filter((n) => n.endsWith(".zip")).map((n) => statSync(join(saves, n)).mtimeMs));
}

const badge = stateBadge;
// The facts list beside the heading: uptime, game version, save.
const facts = (page: Page) => page.locator("main [sse-swap=facts]");

// The real game loads a small map in about a second, so an operation may be
// over before a check can look at it. A page script notes every step the page
// ever showed, across reloads, and the test asserts on that.
const noteSteps = () => {
  const note = () => {
    const seen: string[] = JSON.parse(sessionStorage.getItem("stepsSeen") ?? "[]");
    for (const li of document.querySelectorAll(".steps li")) {
      const text = (li.textContent ?? "").replace(/\s+/g, " ").trim();
      if (text && !seen.includes(text)) seen.push(text);
    }
    sessionStorage.setItem("stepsSeen", JSON.stringify(seen));
  };
  new MutationObserver(note).observe(document, { childList: true, subtree: true, characterData: true });
  document.addEventListener("DOMContentLoaded", note);
};

const stepsSeen = (page: Page): Promise<string[]> => page.evaluate(() => JSON.parse(sessionStorage.getItem("stepsSeen") ?? "[]"));

// Checks the step list if the operation is still on the page.
async function checkMidOperation(page: Page) {
  if (await page.locator(".steps").isVisible()) await checkPage(page);
  else console.log("NOTE the operation ended before its step list could be checked");
}

// The names on a world's Saves page, as a person sees them.
async function savesOf(page: Page, base: string, id: string): Promise<string[]> {
  await page.goto(`${base}/worlds/${id}/saves`);
  await expect(page.getByRole("heading", { level: 2, name: "Saves" })).toBeVisible();
  return (await page.locator("table tbody tr td:first-child a").allTextContents()).map((t) => t.trim()).sort();
}

test("from a phone: start, see who is on, restart with a warning, stop", async ({ page }) => {
  // The image must already be here; nothing is downloaded (SPEC 3.7). make e2e-game checks first; this guards a direct run.
  const image = await dockerCall("GET", `/images/${encodeURIComponent(image21)}/json`);
  expect(image.status, `the game image ${image21} is not on this host. Pull it with docker pull, then run again`).toBe(200);

  const foreman = await startForeman();
  try {
    await page.addInitScript(noteSteps);
    // First run: make the admin, then sign out and back in.
    await page.goto(foreman.url + "/setup");
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByLabel("Confirm password").fill(password);
    await page.getByRole("button", { name: "Create admin account" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    seedWorld();
    await page.context().clearCookies();
    await page.goto(foreman.url + "/");
    await expect(page).toHaveURL(foreman.url + "/signin");
    await checkPage(page);
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();

    await page.goto(foreman.url + "/");
    // The page leads with the world name and its state (DESIGN 6).
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^Phone /);
    await expect(badge(page)).toHaveText("Stopped", { timeout: 20_000 });
    await expect(page.getByText("Nobody can be online while the game is not running.")).toBeVisible();
    await checkPage(page);

    // Start, and watch the step list run to Running without a reload.
    const t0 = Date.now();
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    await checkMidOperation(page);
    await page.evaluate(() => ((window as unknown as { marker: number }).marker = 1));
    await expect(badge(page)).toHaveText("Running", gameTimeout);
    expect(await stepsSeen(page)).toEqual(expect.arrayContaining([expect.stringContaining("Wait for the game to load"), expect.stringContaining("Check RCON answers")]));
    const startTiming = `start to Running: ${Math.round((Date.now() - t0) / 1000)} s`;
    test.info().annotations.push({ type: "timing", description: startTiming });
    console.log("TIMING " + startTiming);
    // The page followed the game without a reload (SPEC 16.3).
    expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
    await expect(facts(page)).toContainText("2.1.24");
    // The container is the pinned image, made by Foreman for this world.
    const inspected = JSON.parse((await dockerCall("GET", `/containers/${container}/json`)).body);
    expect(inspected.Config.Image).toBe(image21);
    expect(inspected.State.Running).toBe(true);

    // No client can join, so the real RCON answers with no players.
    await expect(page.getByText("Nobody is online.")).toBeVisible({ timeout: 30_000 });
    await checkPage(page);

    const beforeRestart = newestSave();

    // Restart with a short countdown. Nobody is online, so nobody is warned.
    await page.getByRole("link", { name: "Restart server" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Restart Phone");
    await checkPage(page);
    await page.getByLabel("Countdown in seconds").fill("5");
    await page.getByRole("button", { name: "Restart server" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    await checkMidOperation(page);
    const t1 = Date.now();
    await expect(badge(page)).toHaveText("Running", gameTimeout);
    expect(await stepsSeen(page)).toEqual(expect.arrayContaining([expect.stringContaining("Save the game")]));
    const restartTiming = `restart to Running: ${Math.round((Date.now() - t1) / 1000)} s`;
    test.info().annotations.push({ type: "timing", description: restartTiming });
    console.log("TIMING " + restartTiming);
    await expect(page.getByText("Nobody is online.")).toBeVisible({ timeout: 30_000 });
    await checkPage(page);

    // Stop, again with a short countdown.
    await page.getByRole("link", { name: "Stop server" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Stop Phone");
    await page.getByLabel("Countdown in seconds").fill("5");
    await page.getByRole("button", { name: "Stop server" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    await expect(badge(page)).toHaveText("Stopped", { timeout: 150_000 });
    await expect(page.getByText("Nobody can be online while the game is not running.")).toBeVisible();
    await checkPage(page);

    // The game saved before it stopped (SPEC 8.1): a save is newer than the one seen while it ran.
    expect(newestSave()).toBeGreaterThan(beforeRestart);
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  } catch (e) {
    await test.info().attach("foreman-log", { body: foreman.logs(), contentType: "text/plain" });
    throw e;
  } finally {
    // The game outlives Foreman (SPEC 3.8), so remove it through Docker.
    await foreman.stop();
    await removeContainer();
    rmSync(dataDir, { recursive: true, force: true });
  }
});

test("from a phone: switch to another world and back with no save lost", async ({ page }) => {
  const image = await dockerCall("GET", `/images/${encodeURIComponent(image21)}/json`);
  expect(image.status, `the game image ${image21} is not on this host. Pull it with docker pull, then run again`).toBe(200);

  const foreman = await startForeman();
  try {
    await page.addInitScript(noteSteps);
    await page.goto(foreman.url + "/setup");
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByLabel("Confirm password").fill(password);
    await page.getByRole("button", { name: "Create admin account" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    seedWorld();
    seedWorld(worldBID, "Other", false);

    // The Server page names the active world in its heading (DESIGN 6).
    const active = (name: string) => page.getByRole("heading", { level: 1 }).filter({ hasText: name });
    await page.goto(foreman.url + "/");
    await expect(badge(page)).toHaveText("Stopped", { timeout: 20_000 });
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", gameTimeout);
    await expect(page.getByText("Nobody is online.")).toBeVisible({ timeout: 30_000 });

    const aBefore = await savesOf(page, foreman.url, worldID);
    const bBefore = await savesOf(page, foreman.url, worldBID);
    expect(aBefore).toEqual(["phone.zip"]);
    expect(bBefore).toEqual(["other.zip"]);

    // Switch from A to B with a short countdown (SPEC 7.9).
    const t0 = Date.now();
    await page.goto(foreman.url + `/worlds/${worldBID}`);
    await page.getByRole("link", { name: "Switch to this world" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Switch from Phone to Other?");
    await expect(page.locator("section.confirm")).toBeVisible();
    await checkPage(page);
    await page.getByLabel("Countdown in seconds").fill("5");
    await page.getByRole("button", { name: "Switch to Other" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    await checkMidOperation(page);
    await expect(active("Other")).toBeVisible(gameTimeout);
    await expect(badge(page)).toHaveText("Running", gameTimeout);
    await expect(page.locator(".message--success").filter({ hasText: "The worlds are switched" })).toBeVisible(gameTimeout);
    expect(await stepsSeen(page)).toEqual(
      expect.arrayContaining([expect.stringContaining("Save the game"), expect.stringContaining("Take a snapshot of both worlds")]),
    );
    const toB = `switch A to B, to Running: ${Math.round((Date.now() - t0) / 1000)} s`;
    test.info().annotations.push({ type: "timing", description: toB });
    console.log("TIMING " + toB);
    await expect(page.getByText("Nobody is online.")).toBeVisible({ timeout: 30_000 });
    await checkPage(page);
    expect(await isRunning(containerB)).toBe(true);
    expect(await isRunning(container)).toBe(false);

    // And back again.
    const t1 = Date.now();
    await page.goto(foreman.url + `/worlds/${worldID}`);
    await page.getByRole("link", { name: "Switch to this world" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Switch from Other to Phone?");
    await checkPage(page);
    await page.getByLabel("Countdown in seconds").fill("5");
    await page.getByRole("button", { name: "Switch to Phone" }).click();
    await expect(page).toHaveURL(foreman.url + "/");
    await expect(active("Phone")).toBeVisible(gameTimeout);
    await expect(badge(page)).toHaveText("Running", gameTimeout);
    await expect(page.locator(".message--success").filter({ hasText: "The worlds are switched" })).toBeVisible(gameTimeout);
    const back = `switch B back to A, to Running: ${Math.round((Date.now() - t1) / 1000)} s`;
    test.info().annotations.push({ type: "timing", description: back });
    console.log("TIMING " + back);
    await expect(facts(page)).toContainText("2.1.24");
    await expect(page.getByText("Nobody is online.")).toBeVisible({ timeout: 30_000 });
    await checkPage(page);
    expect(await isRunning(container)).toBe(true);
    expect(await isRunning(containerB)).toBe(false);

    // No save was lost: every save from before is still listed (autosaves may be new).
    for (const [id, before] of [[worldID, aBefore], [worldBID, bBefore]] as const) {
      const after = await savesOf(page, foreman.url, id);
      expect(after, `saves of ${id} after the round trip`).toEqual(expect.arrayContaining(before));
    }
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  } catch (e) {
    await test.info().attach("foreman-log", { body: foreman.logs(), contentType: "text/plain" });
    throw e;
  } finally {
    await foreman.stop();
    await removeContainer(container);
    await removeContainer(containerB);
    rmSync(dataDir, { recursive: true, force: true });
  }
});
