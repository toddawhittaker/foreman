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
  // The group takes focus, so a screen reader's reading position follows the jump.
  await expect(target).toBeFocused();
  await checkPage(page);
});

// DESIGN 8.11: the parent names look like links in both themes, and several of
// them wrapped on a phone keep a target of 24 px each that do not overlap.
test("the parent links are underlined and keep their tap targets at 320 px", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, ["fmtest"]);
  await page.setViewportSize({ width: 320, height: 800 });
  await page.goto("/worlds/base/mods/settings");
  await expect(page.getByLabel("Startup integer")).toBeVisible(live);
  const origin = group(page, "FM test").locator(".mod-origin");
  const link = origin.getByRole("link", { name: "Fake planet" });
  await expect(link).toHaveCSS("text-decoration-line", "underline");
  // Three linked names in one line, as an origin with three parents reads.
  await origin.evaluate((el) => {
    const a = el.querySelector("a")!;
    for (const n of ["Second parent mod", "Third parent mod"]) {
      const c = a.cloneNode(true) as HTMLAnchorElement;
      c.textContent = n;
      el.append(", ", c);
    }
  });
  const boxes = await origin.locator("a").evaluateAll((as) => as.flatMap((a) => [...a.getClientRects()].map((r) => ({ top: r.top, bottom: r.bottom, left: r.left, right: r.right }))));
  expect(boxes.length).toBeGreaterThanOrEqual(3);
  for (const b of boxes) expect(b.bottom - b.top).toBeGreaterThanOrEqual(24);
  for (const [i, a] of boxes.entries()) {
    for (const b of boxes.slice(i + 1)) {
      const overlap = a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
      expect(overlap, `two link targets overlap: ${JSON.stringify(boxes)}`).toBe(false);
    }
  }
  await checkPage(page);
});

// SPEC 9.6: the loading words show while the script's request runs, and are gone after.
test("the loading words show while the form loads, and not after", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!, ["fmtest"]);
  let release!: () => void;
  const held = new Promise<void>((r) => (release = r));
  await page.route("**/mods/settings/form", async (route) => {
    await held;
    await route.continue();
  });
  await page.goto("/worlds/base/mods/settings");
  await expect(page.getByRole("status")).toContainText("Reading the mods' settings");
  release();
  await expect(page.getByLabel("Startup integer")).toBeVisible(live);
  await expect(page.getByText("Reading the mods' settings")).toHaveCount(0);
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
    // No script, no request: the page does not say it is loading, and the link is there.
    await expect(page.getByText("Reading the mods' settings")).toBeHidden();
    await expect(page.getByRole("link", { name: "Show the settings when they are ready" })).toBeVisible();
    await page.getByRole("link", { name: "Show the settings when they are ready" }).click();
    await expect(page.getByLabel("Startup integer")).toBeVisible(live);
    await expect(group(page, "FM test").locator(".mod-origin")).toHaveText("Required by Fake planet");
  });
});
