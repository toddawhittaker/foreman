import { test, expect, checkPage, setUpModsWorld, version20, openModTools, live, makeZip } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";

// SPEC 13.1, 14.10: game icons from the Factorio wiki are opt-in. Off, an
// object tag in mod text shows its name and the page loads nothing from the
// wiki. On, the browser loads the image itself, and a failed load shows the
// name, never a broken image.
test.use({ fakeGameOn: true, fakePortalOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { fmtest: "2.0" } };
const fmtestDir = join(import.meta.dirname, "..", "..", "internal", "game", "testdata", "modsettings", "fmtest_1.0.0");
const wikiAddress = "https://wiki.factorio.com/images/Space_science_pack.png";
const wikiLabel = "Show game icons from the Factorio wiki (your browser contacts wiki.factorio.com)";
// A 1 x 1 transparent PNG, the answer to a wiki request that succeeds.
const pixel = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");

const rich = [
  "[mod-setting-name]",
  "fm-sta-bool=Restrict Crusher recipe",
  "[mod-setting-description]",
  "fm-sta-bool=Logistic system requires [item=space-science-pack] and [item=Bad Name]",
  "",
].join("\n");

function fmtestZip(): Buffer {
  const entries: Record<string, string> = { "fmtest_1.0.0/locale/en/rich.cfg": rich };
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

async function setUp(page: Page, foreman: { dataDir: string }, fakeGame: Parameters<typeof setUpModsWorld>[2]) {
  await setUpModsWorld(page, foreman as Parameters<typeof setUpModsWorld>[1], fakeGame, { ...world, start: false });
  writeFileSync(join(foreman.dataDir, "worlds", "base", "mods", "fmtest_1.0.0.zip"), fmtestZip());
}

async function openEditor(page: Page) {
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await page.getByRole("link", { name: "Edit mod settings" }).click();
  await expect(page.getByLabel("Startup integer")).toBeVisible(live);
}

async function turnOn(page: Page) {
  await page.goto("/settings/general");
  await page.getByLabel(wikiLabel).check();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
}

const icons = (page: Page) => page.locator("#mod-settings-form form img.rt-obj-icon");

test("off by default: names, no image, no request to the wiki", async ({ page, foreman, fakeGame }) => {
  const asked: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("wiki.factorio.com")) asked.push(r.url());
  });
  await setUp(page, foreman, fakeGame!);
  await page.goto("/settings/general");
  await expect(page.getByLabel(wikiLabel)).not.toBeChecked();
  await checkPage(page);
  await openEditor(page);
  const box = page.getByLabel("Restrict Crusher recipe");
  await expect(box).toHaveAccessibleDescription(/Logistic system requires Space science pack and Bad Name/);
  await expect(icons(page)).toHaveCount(0);
  const res = await page.request.get("/settings/general");
  expect(res.headers()["content-security-policy"]).toBe("frame-ancestors 'none'; img-src 'self' data:");
  await checkPage(page);
  expect(asked).toEqual([]);
});

test("on: the browser loads the wiki image with the right address, alt text and referrer policy", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.route("https://wiki.factorio.com/**", (route) => route.fulfill({ status: 200, contentType: "image/png", body: pixel }));
  await turnOn(page);
  const res = await page.request.get("/settings/general");
  expect(res.headers()["content-security-policy"]).toBe("frame-ancestors 'none'; img-src 'self' data: https://wiki.factorio.com");
  await openEditor(page);
  // One image: a name that is not a plain prototype name stays text.
  await expect(icons(page)).toHaveCount(1);
  const icon = icons(page).first();
  await expect(icon).toHaveAttribute("src", wikiAddress);
  await expect(icon).toHaveAttribute("alt", "Space science pack");
  await expect(icon).toHaveAttribute("referrerpolicy", "no-referrer");
  await expect(icon).toHaveAttribute("loading", "lazy");
  await expect(page.getByLabel("Restrict Crusher recipe")).toHaveAccessibleDescription(/Logistic system requires Space science pack and Bad Name/);
  await checkPage(page);
});

test("on, but the wiki cannot be reached: the name shows, never a broken image", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  let asked = 0;
  await page.route("https://wiki.factorio.com/**", (route) => {
    asked++;
    return route.abort();
  });
  await turnOn(page);
  await openEditor(page);
  await expect.poll(() => asked).toBeGreaterThan(0);
  await expect(icons(page)).toHaveCount(0);
  await expect(page.locator("#mod-settings-form form").getByText("Space science pack", { exact: false }).first()).toBeVisible();
  await expect(page.getByLabel("Restrict Crusher recipe")).toHaveAccessibleDescription(/Logistic system requires Space science pack and Bad Name/);
  await checkPage(page);
});
