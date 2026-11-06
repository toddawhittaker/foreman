import { test, expect, checkPage, createAdmin, seedWorld, image21 } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";
import { existsSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.7: Clone copies a world's mods and settings into a new world, then
// either keeps the save or starts a fresh map.
test.use({ fakeGameOn: true });

const worldJSON = (dataDir: string, id: string) => JSON.parse(readFileSync(join(dataDir, "worlds", id, "world.json"), "utf8"));
const savesOf = (dataDir: string, id: string) => readdirSync(join(dataDir, "worlds", id, "saves"));

test("clones the active world and keeps its save, in three clicks from the Server page", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman, { spaceAge: true });
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  const clicks = await countClicks(page);
  await clicks.reset();

  await page.getByRole("link", { name: "Active world: Base" }).click();
  await page.getByRole("link", { name: "Clone world" }).click();
  await expect(page).toHaveURL("/worlds/base/clone");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Clone Base");
  await expect(page.getByLabel("Name of the clone")).toHaveValue("Base (copy)");
  // The label names the save and when it was made (SPEC 5.7).
  const keep = page.getByRole("radio", { name: /^Keep base\.zip, saved / });
  await expect(keep).toBeChecked();
  await expect(page.locator("main")).toContainText("Game version 2.1.24, with Space Age.");
  await checkPage(page);

  await page.getByRole("button", { name: "Clone world" }).click();
  await expect(page).toHaveURL("/worlds/base-copy?cloned=base");
  await expect(page.locator(".message--success")).toHaveText("Done: Cloned Base as Base (copy).");
  await expectWithin(clicks, 3, "clone a world, keeping the save");
  await checkPage(page);

  expect(savesOf(foreman.dataDir, "base-copy")).toEqual(["base.zip"]);
  expect(worldJSON(foreman.dataDir, "base-copy")).toMatchObject({ name: "Base (copy)", game_version: "2.1.24", space_age: true, load_save: "base.zip" });
  expect(worldJSON(foreman.dataDir, "base-copy").new_map).toBeUndefined();
  // The source is as it was, and the copy never takes the RCON password.
  expect(savesOf(foreman.dataDir, "base")).toEqual(["base.zip"]);
  expect(existsSync(join(foreman.dataDir, "worlds", "base-copy", "config", "rconpw"))).toBe(false);
});

test("a fresh map has no save and a random seed", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/worlds/base/clone");
  await page.getByLabel("Name of the clone").fill("Clean slate");
  await page.getByRole("radio", { name: "Start a fresh map" }).check();
  await page.getByRole("button", { name: "Clone world" }).click();

  await expect(page).toHaveURL("/worlds/clean-slate?cloned=base");
  await expect(page.locator(".message--success")).toHaveText("Done: Cloned Base as Clean slate.");
  expect(savesOf(foreman.dataDir, "clean-slate")).toEqual([]);
  const meta = worldJSON(foreman.dataDir, "clean-slate");
  expect(meta.load_save).toBeUndefined();
  expect(Number.isInteger(meta.new_map.seed)).toBe(true);
});

test("a name that is taken or blank is refused with the typed name kept", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/clone");
  await page.getByLabel("Name of the clone").fill("BASE");
  await page.getByRole("button", { name: "Clone world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Another world already has this name");
  await expect(page).toHaveTitle(/^Error: /);
  await expect(page.getByLabel("Name of the clone")).toHaveValue("BASE");
  await checkPage(page);

  await page.getByLabel("Name of the clone").fill("");
  await page.getByRole("button", { name: "Clone world" }).click();
  await expect(page.locator("#error-summary")).toContainText("Enter a name for the world");
  expect(readdirSync(join(foreman.dataDir, "worlds")).filter((n) => !n.startsWith("."))).toEqual(["base"]);
});

test("a world without a save offers only a fresh map, as text", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  // The save is gone: the world has none to keep.
  rmSync(join(foreman.dataDir, "worlds", "base", "saves", "base.zip"));
  await page.goto("/worlds/base/clone");
  await expect(page.getByRole("radio")).toHaveCount(0);
  await expect(page.locator("#clone-fresh")).toContainText("This world has no save to keep.");
  await checkPage(page);
  await page.getByRole("button", { name: "Clone world" }).click();
  await expect(page).toHaveURL("/worlds/base-copy?cloned=base");
  expect(worldJSON(foreman.dataDir, "base-copy").new_map.seed).toBeGreaterThanOrEqual(0);
});

test("a long world name is cut to fit the copy's name and the page fits at any width", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  const long = "Family base with a very long name ".repeat(3).trim().slice(0, 98);
  const meta = worldJSON(foreman.dataDir, "base");
  meta.name = long;
  writeFileSync(join(foreman.dataDir, "worlds", "base", "world.json"), JSON.stringify(meta));
  await page.goto("/worlds/base/clone");
  const value = await page.getByLabel("Name of the clone").inputValue();
  expect(value.length).toBeLessThanOrEqual(100);
  expect(value.endsWith(" (copy)")).toBe(true);
  await checkPage(page);
});
