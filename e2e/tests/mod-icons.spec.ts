import { test, expect, checkPage, createAdmin, seedWorld, image21, live } from "./foreman.js";
import { openNewMap, newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 14.6, 14.10, 17: an object tag in a mod's setting text shows the
// mod's own icon, named by its alt text, whether the wiki setting is off or
// on; Foreman serves the picture itself; a picture that fails shows the name.
test.use({ fakeGameOn: true });

const wikiLabel = "Show game icons from the Factorio wiki (your browser contacts wiki.factorio.com)";
const iconPath = (id: string) => `/worlds/${id}/icons/item/fake-widget.png`;
// A 1 x 1 transparent PNG, the answer to a wiki request that succeeds.
const pixel = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");

// Adds the fake planet mod to a world and enables it, as a Mods install would.
function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: [{ name: "base", enabled: true }] }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

async function modded(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }): Promise<string> {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  return id;
}

async function wikiOn(page: Page) {
  await page.goto("/settings/general");
  await page.getByLabel(wikiLabel).check();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
}

const stepPath = (id: string) => `${newMapPath(id)}/mod-settings`;
const settingsPath = (id: string) => `/worlds/${id}/mods/settings`;
const icon = (page: Page) => page.locator("img.rt-obj-icon[src*='/icons/item/fake-widget.png']");

// Both pages that show a mod's settings, with the text of fp-sta-bool.
const pages = [
  { name: "the mod settings page", open: (id: string) => settingsPath(id) },
  { name: "Generate's Mod settings step", open: (id: string) => stepPath(id) },
];

for (const p of pages) {
  test(`${p.name} shows the mod's own icon with the item's name as alt, wiki setting off`, { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    const id = await modded(page, foreman, fakeGame!);
    await page.goto(p.open(id));
    await expect(icon(page)).toHaveCount(1, live);
    await expect(icon(page)).toHaveAttribute("alt", "Fake widget");
    await expect(icon(page)).toHaveAttribute("src", iconPath(id));
    await expect.poll(() => icon(page).evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    await checkPage(page);
  });

  test(`${p.name}: with the wiki setting on, the mod's icon still wins`, async ({ page, foreman, fakeGame }) => {
    const id = await modded(page, foreman, fakeGame!);
    // The wiki would take the object only if the mod's icon were missing, so it is never asked for it.
    const asked: string[] = [];
    await page.route("https://wiki.factorio.com/**", (route) => {
      asked.push(route.request().url());
      return route.fulfill({ status: 200, contentType: "image/png", body: pixel });
    });
    await wikiOn(page);
    await page.goto(p.open(id));
    await expect(icon(page)).toHaveCount(1, live);
    await expect(icon(page)).toHaveAttribute("alt", "Fake widget");
    await expect(icon(page)).toHaveAttribute("src", iconPath(id));
    await expect(page.locator("img.rt-obj-icon[src*='wiki.factorio.com']")).toHaveCount(0);
    expect(asked).toEqual([]);
    await checkPage(page);
  });

  test(`${p.name}: a picture that fails to load shows the name, never a broken image`, async ({ page, foreman, fakeGame }) => {
    const id = await modded(page, foreman, fakeGame!);
    let asked = 0;
    await page.route(`**${iconPath(id)}`, (route) => {
      asked++;
      return route.abort();
    });
    await page.goto(p.open(id));
    await expect.poll(() => asked, live).toBeGreaterThan(0);
    await expect(icon(page)).toHaveCount(0);
    await expect(page.getByText("Fake widget").first()).toBeAttached();
    await checkPage(page);
  });
}

test("the icon address answers a PNG that is not guessed, and is private", async ({ page, foreman, fakeGame }) => {
  const id = await modded(page, foreman, fakeGame!);
  // The icon index is made by the mod settings run; open the page that starts it.
  await page.goto(settingsPath(id));
  await expect(icon(page)).toHaveCount(1, live);

  const res = await page.request.get(iconPath(id));
  expect(res.status()).toBe(200);
  const headers = res.headers();
  expect(headers["content-type"]).toBe("image/png");
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(headers["cache-control"]).toMatch(/\bprivate\b/);
  expect((await res.body()).subarray(0, 8)).toEqual(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));

  // A name no mod has an icon for, a kind the game does not have, and a path that climbs out are all 404.
  for (const bad of [`/worlds/${id}/icons/item/no-such-thing.png`, `/worlds/${id}/icons/bogus/fake-widget.png`, `/worlds/${id}/icons/item/..%2f..%2fworld.json.png`, `/worlds/${id}/icons/item/fake-widget`]) {
    expect((await page.request.get(bad)).status(), bad).toBe(404);
  }
  // A world that does not exist.
  expect((await page.request.get("/worlds/no-such-world/icons/item/fake-widget.png")).status()).toBe(404);
});

test("a signed-out request for an icon is refused", async ({ page, foreman, fakeGame, playwright, baseURL }) => {
  const id = await modded(page, foreman, fakeGame!);
  await page.goto(settingsPath(id));
  await expect(icon(page)).toHaveCount(1, live);
  const anon = await playwright.request.newContext({ baseURL });
  const res = await anon.get(iconPath(id), { maxRedirects: 0 });
  expect([401, 403, 303]).toContain(res.status());
  expect(res.headers()["content-type"] ?? "").not.toContain("image/png");
  await anon.dispose();
});
