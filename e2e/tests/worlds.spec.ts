import { test, expect, checkPage, type Foreman } from "./foreman.js";
import type { Page } from "@playwright/test";
import { mkdirSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// Taking a snapshot asks Docker whether the game is running first
// (SPEC 6.10), so these tests use the fake game as Docker.
test.use({ fakeGameOn: true });

const admin = "admin";
const password = "correct horse battery";

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill(admin);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
  await page.goto("/worlds");
}

// Worlds are folders a person can make by hand (SPEC 5.1), so the list is
// tested from folders written directly, not through Foreman.
function seedWorld(f: Foreman, id: string, meta: Record<string, unknown>, saveTime?: Date) {
  const dir = join(f.dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(join(dir, "world.json"), JSON.stringify({ format_version: 1, created_at: "2026-11-01T10:00:00Z", ...meta }));
  if (saveTime) {
    const save = join(dir, "saves", "base.zip");
    writeFileSync(save, Buffer.alloc(2_100_000));
    utimesSync(save, saveTime, saveTime);
  }
}

function setActive(f: Foreman, id: string) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    db.prepare("INSERT INTO active_worlds (world_id, since) VALUES (?, ?)").run(id, 0);
  } finally {
    db.close();
  }
}

// DESIGN 6: "/" is the Server page; the world list stays at /worlds.
test("/ is the Server page and the world list is at /worlds", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/");
  await expect(page).toHaveURL("/");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Server");
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Worlds" }).click();
  await expect(page).toHaveURL("/worlds");
});

test("empty world list names the next step", async ({ page }) => {
  await createAdmin(page);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Worlds");
  await expect(page.getByText(/No worlds yet\. Import a folder of saves/)).toBeVisible();
  await checkPage(page);
});

// SPEC 5.4: name, game version, Space Age, last played, disk use, and which is active.
test("world list shows each world, the active one first", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "alpha", { name: "Alpha", game_version: "1.1.110", space_age: false });
  seedWorld(foreman, "zeta-base", { name: "Zeta base", game_version: "2.0.72", space_age: true }, new Date("2026-11-03T14:25:00Z"));
  seedWorld(foreman, "future", { name: "Future", game_version: "3.0.0", space_age: false, format_version: 9 });
  mkdirSync(join(foreman.dataDir, "worlds", ".staging-half-made"), { recursive: true });
  setActive(foreman, "zeta-base");

  await page.goto("/worlds");
  const items = page.locator("ul.worlds > li");
  await expect(items).toHaveCount(3);

  const first = items.nth(0);
  await expect(first.getByRole("heading")).toHaveText("Zeta base Active");
  await expect(first.locator(".badge--active")).toHaveText("Active");
  await expect(first).toContainText("2.0.72");
  await expect(first.locator("dd").nth(1)).toHaveText("On");
  await expect(first.locator("time")).toHaveAttribute("datetime", "2026-11-03T14:25:00Z");
  await expect(first.locator("time")).toHaveText(/Nov 3.*\(.+ ago\)$/);
  await expect(first).toContainText("2.1 MB");

  await expect(items.nth(1).getByRole("heading")).toHaveText("Alpha");
  await expect(items.nth(1)).toContainText("Never");
  await expect(items.nth(2)).toContainText("A newer version of Foreman wrote this world");
  await expect(page.locator(".badge--active")).toHaveCount(1);
  await expect(page.getByText("No worlds yet")).toHaveCount(0);
  await checkPage(page);
});

// SPEC 5.2, 5.8: the display name changes, the id does not, and two
// worlds may not share a name ignoring case.
test("renames a world and refuses a duplicate name", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "base", { name: "Base", game_version: "2.0.72", space_age: false });
  seedWorld(foreman, "other", { name: "Other", game_version: "2.0.72", space_age: false });

  await page.goto("/worlds");
  await page.getByRole("link", { name: "Base", exact: true }).click();
  await expect(page).toHaveURL("/worlds/base");
  await expect(page.getByRole("link", { name: "Saves" })).toHaveAttribute("href", "/worlds/base/saves");
  await expect(page.locator("#main").getByRole("link", { name: "Snapshots" })).toHaveAttribute("href", "/worlds/base/snapshots");
  await checkPage(page);

  await page.getByLabel("Name", { exact: true }).fill("OTHER");
  await page.getByRole("button", { name: "Rename world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Another world already has this name");
  await expect(page.getByLabel("Name", { exact: true })).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);

  await page.getByLabel("Name", { exact: true }).fill("Main base");
  await page.getByRole("button", { name: "Rename world" }).click();
  await expect(page).toHaveURL(/\/worlds\/base\?renamed=Main\+base$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Main base");
  await expect(page.locator(".message--success p")).toHaveText("Done: Renamed to Main base.");
});

test("a world a newer Foreman wrote cannot be renamed", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "future", { name: "Future", game_version: "3.0.0", space_age: false, format_version: 9 });
  await page.goto("/worlds/future");
  await expect(page.getByText("A newer version of Foreman wrote this world")).toBeVisible();
  await expect(page.getByRole("button", { name: "Rename world" })).toHaveCount(0);
  await checkPage(page);
});

// DESIGN 6, "The 320 px rules": a long name wraps; checkPage fails on sideways scroll at 320 px.
test("a very long world name wraps on the list and the world page", async ({ page, foreman }) => {
  await createAdmin(page);
  const long = "Family-base-".repeat(8) + "x".repeat(40);
  seedWorld(foreman, "long", { name: long, game_version: "2.0.72", space_age: true });
  await page.goto("/worlds");
  await expect(page.getByRole("link", { name: long, exact: true })).toBeVisible();
  await checkPage(page);
  await page.goto("/worlds/long");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(long);
  await checkPage(page);
});

// DESIGN 6, Shell: the current tab is orange, a hovered tab is grey.
test("the Worlds tab marks the current page and hovers grey", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "alpha", { name: "Alpha", game_version: "2.0.72", space_age: false });
  const tab = page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Worlds" });
  await page.goto("/worlds/alpha/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  const restore = await page.getByRole("link", { name: /^Restore/ }).getAttribute("href");
  // Every page about worlds marks the tab (DESIGN 7.3).
  for (const path of ["/worlds", "/worlds/alpha", "/import", "/worlds/alpha/saves", "/worlds/alpha/snapshots",
    restore!, "/worlds/alpha/delete", "/deleted"]) {
    await page.goto(path);
    await expect(tab, path).toHaveAttribute("aria-current", "page");
  }
  const face = () => tab.evaluate((el) => getComputedStyle(el).backgroundColor);
  const current = await face();
  // Every signed-in page is about worlds, so drop the mark to see the idle tab.
  await tab.evaluate((el) => el.removeAttribute("aria-current"));
  const idle = await face();
  await tab.hover();
  const hover = await face();
  const button = await page.evaluate(() => {
    const probe = document.createElement("span");
    probe.style.background = "var(--color-button-hover)";
    document.body.append(probe);
    const c = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return c;
  });
  expect(hover).toBe(button);
  expect(hover).not.toBe(current);
  expect(idle).not.toBe(current);
});

// SPEC 5.4, 6.6: each world in the list says, in its Disk use, what its snapshots add, and
// that is the figure on the world's own Snapshots page.
test("the list shows what each world's snapshots add", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman, "alpha", { name: "Alpha", game_version: "2.0.72", space_age: false }, new Date("2026-11-03T14:25:00Z"));
  seedWorld(foreman, "beta", { name: "Beta", game_version: "2.0.72", space_age: false });
  await page.goto("/worlds/alpha/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.getByText("Done: Foreman took the snapshot")).toBeVisible();
  // The world moves on, so the snapshot alone holds the old save.
  rmSync(join(foreman.dataDir, "worlds", "alpha", "saves", "base.zip"));
  await page.reload();
  const added = (await page.locator("dl.facts div").filter({ hasText: "Snapshots add" }).locator("dd").textContent())!;
  expect(added).not.toBe("0 B");

  await page.goto("/worlds");
  const fact = (name: string) =>
    page.locator("ul.worlds > li").filter({ has: page.getByRole("heading", { name }) }).locator("div").filter({ hasText: /^Disk use/ }).locator("dd");
  await expect(fact("Alpha")).toHaveText(new RegExp(`snapshots add ${added}$`));
  await expect(fact("Beta")).toHaveText(/snapshots add 0 B$/);
  await checkPage(page);
});
