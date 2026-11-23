import { test, expect, checkPage, setUpModsWorld, version20, live, makeZip } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";

// SPEC 9.6, DESIGN 8.11: the mod settings page says why a mod nobody chose is
// installed, and a browser without script still reaches the form.
test.use({ fakeGameOn: true, fakePortalOn: true });

const modsDir = (dataDir: string) => join(dataDir, "worlds", "base", "mods");
const fmtestDir = join(import.meta.dirname, "..", "..", "internal", "game", "testdata", "modsettings", "fmtest_1.0.0");

function fmtestZip(): Buffer {
  const entries: Record<string, string> = {};
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else entries[`fmtest_1.0.0/${relative(fmtestDir, p)}`] = readFileSync(p, "utf8");
    }
  };
  walk(fmtestDir);
  return makeZip(entries);
}

// A world whose planet-like mod needs the library mod fmtest. The fake game
// reports a setting for each of the two.
async function setUp(page: Page, foreman: { dataDir: string }, fakeGame: Parameters<typeof setUpModsWorld>[2], planetDeps: string[]) {
  await setUpModsWorld(page, foreman as Parameters<typeof setUpModsWorld>[1], fakeGame, {
    version: version20,
    fixture: "2.0.77-no-space-age.zip",
    mods: { fmtest: "2.0", "fake-planet": "2.0" },
  });
  writeFileSync(join(modsDir(foreman.dataDir), "fmtest_1.0.0.zip"), fmtestZip());
  const info = JSON.stringify({ name: "fake-planet", version: "1.0.0", title: "Fake planet", factorio_version: "2.0", dependencies: ["base", ...planetDeps] });
  writeFileSync(join(modsDir(foreman.dataDir), "fake-planet_1.0.0.zip"), makeZip({ "fake-planet_1.0.0/info.json": info }));
}

const group = (page: Page, title: string) => page.locator("fieldset.mod-settings-group").filter({ has: page.locator("legend", { hasText: title }) });

test("a library mod says who requires it, linked to that mod's group; the parent says nothing", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, ["fmtest"]);
  await page.goto("/worlds/base/mods/settings");
  await expect(page.getByLabel("Startup integer")).toBeVisible(live);

  const library = group(page, "FM test");
  const origin = library.locator(".mod-origin");
  await expect(origin).toHaveText("Required by Fake planet");
  await expect(group(page, "Fake planet").locator(".mod-origin")).toHaveCount(0);
  await checkPage(page);

  // The name is a link that jumps to that mod's group.
  const link = origin.getByRole("link", { name: "Fake planet" });
  const target = group(page, "Fake planet");
  await expect(link).toHaveAttribute("href", `#${await target.getAttribute("id")}`);
  await link.click();
  await expect(page).toHaveURL(new RegExp(`#${await target.getAttribute("id")}$`));
});

for (const [dep, text] of [["? fmtest", "Offered by Fake planet"], ["+ fmtest", "Recommended by Fake planet"], ["fmtest", "Required by Fake planet"]]) {
  test(`a dependency "${dep}" reads "${text}"`, async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, [dep]);
    await page.goto("/worlds/base/mods/settings");
    await expect(page.getByLabel("Startup integer")).toBeVisible(live);
    await expect(group(page, "FM test").locator(".mod-origin")).toHaveText(text);
    await checkPage(page);
  });
}

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  // SPEC 9.6: the form is loaded by script, so without it a link asks the server to read it first.
  test("a link reaches the form", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!, ["fmtest"]);
    await page.goto("/worlds/base/mods/settings");
    await expect(page.getByRole("status")).toContainText("Reading the mods' settings");
    await page.getByRole("link", { name: "Show the settings when they are ready" }).click();
    await expect(page.getByLabel("Startup integer")).toBeVisible(live);
    await expect(group(page, "FM test").locator(".mod-origin")).toHaveText("Required by Fake planet");
  });
});
