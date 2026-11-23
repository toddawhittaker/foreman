import { test, expect, checkPage, setUpModsWorld, live, stateBadge, type Foreman } from "./foreman.js";
import { readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 7.4: an Unfinished world whose saved map settings name a control
// its mods no longer have is refused at start, with no start anyway, and the
// fix is a link to the world's map settings.
test.use({ fakeGameOn: true });

const base = (f: Foreman, ...rel: string[]) => join(f.dataDir, "worlds", "base", ...rel);

// Turns the seeded world into one Generate made: new_map and no save.
function makeUnfinished(f: Foreman) {
  const meta = JSON.parse(readFileSync(base(f, "world.json"), "utf8"));
  writeFileSync(base(f, "world.json"), JSON.stringify({ ...meta, new_map: { preset: "" } }));
  rmSync(base(f, "saves", "base.zip"));
}

// Saves map settings as step 3 would, naming the given controls.
function saveMapSettings(f: Foreman, controls: string[]) {
  const autoplace = Object.fromEntries(controls.map((c) => [c, { frequency: 1, size: 1, richness: 1 }]));
  writeFileSync(base(f, "config", "map-gen-settings.json"), JSON.stringify({ autoplace_controls: autoplace }));
}

// Removes the fake-planet mod, as the Mods tab would.
function removeFakePlanet(f: Foreman) {
  rmSync(base(f, "mods", "fake-planet_1.0.0.zip"));
  const list = JSON.parse(readFileSync(base(f, "mods", "mod-list.json"), "utf8"));
  list.mods = list.mods.filter((m: { name: string }) => m.name !== "fake-planet");
  writeFileSync(base(f, "mods", "mod-list.json"), JSON.stringify(list));
}

test("a removed planet mod refuses the first start and links to the map settings", async ({ page, foreman, fakeGame }) => {
  // A second mod stays, so the start needs the planet run, not the built-in planets.
  await setUpModsWorld(page, foreman, fakeGame!, { mods: { "fake-planet": "2.1", "other-mod": "2.1" } });
  makeUnfinished(foreman);
  saveMapSettings(foreman, ["iron-ore", "fake-ore"]);
  removeFakePlanet(foreman);

  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  const box = page.locator(".message").filter({ hasText: "fake-ore" });
  await expect(box).toContainText("The world's map settings name controls its mods no longer have: fake-ore.", live);
  await expect(stateBadge(page)).toHaveText("Stopped");
  await expect(box.getByRole("link", { name: "Open the map settings" })).toHaveAttribute("href", "/worlds/base/new-map");
  await expect(box.getByText("Or start anyway")).toHaveCount(0);
  await expect(box.getByRole("button", { name: "Start anyway" })).toHaveCount(0);
  await checkPage(page);

  // Saving the map settings again drops the control, and the start works.
  saveMapSettings(foreman, ["iron-ore"]);
  await page.getByRole("button", { name: "Try to start again" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
});

test("a planet mod still installed starts", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { mods: { "fake-planet": "2.1" } });
  makeUnfinished(foreman);
  saveMapSettings(foreman, ["iron-ore", "fake-ore"]);

  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
});
