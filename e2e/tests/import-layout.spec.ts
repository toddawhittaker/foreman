import { test, expect, checkPage, createAdmin, makeZip } from "./foreman.js";
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.1 and 5.5: a folder in the Factorio server's own layout (saves/,
// mods/, config/) imports as it is.
test("imports a folder in the server layout: its save, mods and settings", async ({ page, foreman }) => {
  await createAdmin(page);
  const src = join(foreman.dataDir, "import", "server");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(src, sub), { recursive: true });
  writeFileSync(join(src, "saves", "campaign.zip"), makeZip({ "level/level.dat0": "the map" }));
  writeFileSync(join(src, "mods", "mod-list.json"), "{}");
  writeFileSync(join(src, "config", "server-settings.json"), '{"game_password":"from-import"}');
  writeFileSync(join(src, "config", "rconpw"), "not-ours");
  writeFileSync(join(src, "config", "config.ini"), "[path]");

  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  await expect(form.getByLabel(/server: 1 save and mods/)).toBeChecked();
  await checkPage(page);
  await form.getByLabel("World name").fill("Server layout");
  await form.getByLabel("Or type a version").fill("2.0.72");
  await form.getByRole("button", { name: "Import folder" }).click();
  await expect(page).toHaveURL("/worlds/server-layout");

  await page.goto("/worlds/server-layout/saves");
  await expect(page.locator("tbody tr").getByRole("link")).toHaveText("campaign.zip");
  await checkPage(page);

  const world = join(foreman.dataDir, "worlds", "server-layout");
  expect(readdirSync(join(world, "saves"))).toEqual(["campaign.zip"]);
  expect(readdirSync(join(world, "mods"))).toEqual(["mod-list.json"]);
  expect(readdirSync(join(world, "config"))).toEqual(["server-settings.json"]);
  expect(readFileSync(join(world, "config", "server-settings.json"), "utf8")).toBe('{"game_password":"from-import"}');
});

test("a save name in both places is listed as not importable and names the file", async ({ page, foreman }) => {
  await createAdmin(page);
  const src = join(foreman.dataDir, "import", "clash");
  mkdirSync(join(src, "saves"), { recursive: true });
  writeFileSync(join(src, "same.zip"), makeZip({ "level/level.dat0": "top" }));
  writeFileSync(join(src, "saves", "same.zip"), makeZip({ "level/level.dat0": "inner" }));

  await page.goto("/import");
  const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
  const row = form.getByLabel(/clash: /);
  await expect(row).toBeDisabled();
  await expect(form.getByText("the save same.zip is in both the folder and its saves folder")).toBeVisible();
  await expect(form.getByText("There is nothing to import")).toBeVisible();
  await checkPage(page);
  expect(readdirSync(join(foreman.dataDir, "worlds")).filter((n) => !n.startsWith("."))).toEqual([]);
});
