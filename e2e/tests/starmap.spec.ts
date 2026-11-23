import { test, expect, checkPage, createAdmin, seedWorld, image21, live } from "./foreman.js";
import { openNewMap, newMapPath } from "./newmap.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { inflateRawSync } from "node:zlib";

// SPEC 5.6, 14.6, 14.10, 17: a world's star map is one more picture in the
// map preview, drawn as the game places its space locations; it opens in the
// large view like a planet picture and is one more file in the zip.
test.use({ fakeGameOn: true });

const wikiLabel = "Show game icons from the Factorio wiki (your browser contacts wiki.factorio.com)";
const wikiBase = "https://wiki.factorio.com/images/";
// A 1 x 1 transparent PNG, the answer to a wiki request that succeeds.
const pixel = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==", "base64");
// Space Age's seven places, the fake planet, and nothing else: the game's hidden placeholder is left out.
const visible = ["Nauvis", "Vulcanus", "Gleba", "Fulgora", "Aquilo", "Solar system edge", "Shattered planet", "Fake planet"];

const section = (page: Page) => page.getByRole("region", { name: "Map preview" });
const starmap = (page: Page) => section(page).getByRole("img", { name: /^Star map:/ });
const titles = (page: Page) => starmap(page).locator("g.starmap__location > title");
const dialog = (page: Page) => page.locator("#large-view");

// Adds the fake planet mod to a world and enables it, as a Mods install
// would, on a Space Age world, so the star map has the game's own places.
function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: ["base", "elevated-rails", "quality", "space-age"].map((name) => ({ name, enabled: true })) }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

type Env = { page: Page; foreman: Parameters<typeof seedWorld>[0]; fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> } };

// A Space Age world with the fake planet mod, its step 4 open, and the
// pictures of every planet drawn, which brings the star map. moon turns the
// fake moon on through the mod's startup setting.
async function modded({ page, foreman, fakeGame }: Env, opts: { moon?: boolean; wiki?: boolean } = {}): Promise<string> {
  await createAdmin(page);
  if (opts.wiki) await wikiOn(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  if (opts.moon) {
    await page.goto(`${newMapPath(id)}/mod-settings`);
    await page.getByLabel("fm-sta-int").fill("9");
    await page.getByRole("button", { name: "Next: Map settings" }).click();
    await expect(page).toHaveURL(newMapPath(id), live);
  } else {
    await page.goto(newMapPath(id));
  }
  await drawAll(page);
  return id;
}

async function drawAll(page: Page) {
  await expect(page.locator('#generate-form details[data-planet="fake-planet"]').first()).toBeAttached({ timeout: 60_000 });
  await section(page).locator("label.tab", { hasText: "All planets" }).click();
  await expect(starmap(page)).toBeVisible({ timeout: 60_000 });
}

async function wikiOn(page: Page) {
  await page.goto("/settings/general");
  await page.getByLabel(wikiLabel).check();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
}

// The centre of the circle drawn for the location with this title.
async function centre(page: Page, title: string): Promise<[number, number]> {
  const found = await starmap(page).evaluate((svg, t) => {
    const g = [...svg.querySelectorAll("g.starmap__location")].find((l) => l.querySelector("title")?.textContent === t);
    const c = g?.querySelector("circle.starmap__planet");
    return c ? [Number(c.getAttribute("cx")), Number(c.getAttribute("cy"))] : null;
  }, title);
  expect(found, `a circle for ${title}`).not.toBeNull();
  return found as [number, number];
}

// The path data of every route, as the numbers it passes through.
const routes = (page: Page) =>
  starmap(page).locator("g.starmap__routes path").evaluateAll((ps) =>
    ps.map((p) => (p.getAttribute("d") ?? "").split(/[ML]/).filter(Boolean).map((pt) => pt.trim().split(" ").map(Number))));

// The route between two titles, whichever end it starts at.
async function routeBetween(page: Page, a: string, b: string) {
  const [pa, pb] = [await centre(page, a), await centre(page, b)];
  const same = (p: number[], q: number[]) => p[0] === q[0] && p[1] === q[1];
  const found = (await routes(page)).filter((r) => (same(r[0], pa) && same(r.at(-1)!, pb)) || (same(r[0], pb) && same(r.at(-1)!, pa)));
  expect(found, `a route between ${a} and ${b}`).toHaveLength(1);
  return found[0];
}

test("the map preview has a star map picture that names every visible location and leaves out the hidden one", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! });
  await expect(starmap(page)).toHaveAccessibleName("Star map: the planets around the sun and the routes between them.");
  // The picture is one more card, captioned "Star map", beside the planets'.
  await expect(section(page).locator("figcaption", { hasText: /^Star map$/ })).toBeVisible();
  const names = await titles(page).allTextContents();
  expect([...names].sort()).toEqual([...visible].sort());
  // The game's hidden placeholder is nowhere in the picture, neither as a name nor a circle.
  expect(names.join(" ")).not.toMatch(/unknown/i);
  await expect(starmap(page).locator("circle.starmap__planet")).toHaveCount(visible.length);
  // No moon while the setting is off.
  expect((await titles(page).allTextContents()).join(" ")).not.toMatch(/moon/i);
  // Nothing makes the page scroll sideways (SPEC 17.2).
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
  await checkPage(page);
});

test("routes arc about the sun, and the fake moon has its orbit ring about its planet", async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! }, { moon: true });
  // Nauvis and Vulcanus share only the sun, so the route between them curves:
  // more than one straight segment, and not all on the one line.
  const curved = await routeBetween(page, "Nauvis", "Vulcanus");
  expect(curved.length).toBeGreaterThan(2);
  const [a, b] = [curved[0], curved.at(-1)!];
  const away = curved.map((p) => Math.abs((b[0] - a[0]) * (a[1] - p[1]) - (a[0] - p[0]) * (b[1] - a[1])) / Math.hypot(b[0] - a[0], b[1] - a[1]));
  expect(Math.max(...away)).toBeGreaterThan(1);

  // The moon (the mod names it no better than its prototype name) is drawn, and a faint ring is centred on its planet, passing through the moon.
  expect(await titles(page).allTextContents()).toContain("fake-moon");
  const planet = await centre(page, "Fake planet");
  const moon = await centre(page, "fake-moon");
  const rings = await starmap(page).locator("g.starmap__rings circle[cx]").evaluateAll((cs) => cs.map((c) => [Number(c.getAttribute("cx")), Number(c.getAttribute("cy")), Number(c.getAttribute("r"))]));
  expect(rings).toHaveLength(1);
  expect([rings[0][0], rings[0][1]]).toEqual(planet);
  expect(Math.hypot(moon[0] - planet[0], moon[1] - planet[1])).toBeCloseTo(rings[0][2], 0);
});

test("wiki setting off: the star map asks the wiki for nothing, and the fake planet has its own picture", async ({ page, foreman, fakeGame }) => {
  const asked: string[] = [];
  page.on("request", (r) => {
    if (r.url().includes("wiki.factorio.com")) asked.push(r.url());
  });
  const id = await modded({ page, foreman, fakeGame: fakeGame! });
  await expect(starmap(page).locator("image")).toHaveCount(1);
  const own = starmap(page).locator("image");
  await expect(own).toHaveAttribute("href", `/worlds/${id}/icons/starmap/fake-planet.png`);
  await expect.poll(() => page.request.get(`/worlds/${id}/icons/starmap/fake-planet.png`).then((r) => r.headers()["content-type"])).toBe("image/png");
  await expect(page.locator('img[src*="wiki.factorio.com"], image[href*="wiki.factorio.com"]')).toHaveCount(0);
  const res = await page.request.get(newMapPath(id));
  expect(res.headers()["referrer-policy"]).not.toBe("no-referrer");
  expect(asked).toEqual([]);
});

test("wiki setting on: base places get the wiki's pictures, the page sends no referrer, and a failed picture leaves the circle", async ({ page, foreman, fakeGame }) => {
  const asked: string[] = [];
  await page.route(`${wikiBase}**`, (route) => {
    asked.push(route.request().url());
    return route.request().url().endsWith("/Vulcanus.png") ? route.abort() : route.fulfill({ status: 200, contentType: "image/png", body: pixel });
  });
  const id = await modded({ page, foreman, fakeGame: fakeGame! }, { wiki: true });
  // Base places have wiki pictures: the seven of Space Age, none for the sun; the failed one is dropped by the browser's error.
  await expect.poll(() => asked.filter((u) => /\/(Nauvis|Gleba|Fulgora|Aquilo|Solar_system_edge|Shattered_planet)\.png$/.test(u)).length, live).toBe(6);
  await expect(starmap(page).locator('image[href$="/Nauvis.png"]')).toHaveAttribute("href", `${wikiBase}Nauvis.png`);
  expect(asked.some((u) => /Sun|Star/i.test(u))).toBe(false);
  // The failed picture is gone and its circle stays, with its name.
  await expect(starmap(page).locator('image[href$="/Vulcanus.png"]')).toHaveCount(0);
  await expect(starmap(page).locator("circle.starmap__planet")).toHaveCount(visible.length);
  expect(await titles(page).allTextContents()).toContain("Vulcanus");
  // The mod's own picture is still Foreman's.
  await expect(starmap(page).locator('image[href$="/icons/starmap/fake-planet.png"]')).toHaveCount(1);
  const res = await page.request.get(newMapPath(id));
  expect(res.headers()["referrer-policy"]).toBe("no-referrer");
  await checkPage(page);
});

test("the star map opens in the large view, alone, with zoom and pan", async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! });
  await section(page).getByRole("link", { name: /Star map.*opens the large view/ }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  await expect(page.locator("#large-title")).toHaveText("Star map");
  const big = dialog(page).locator("svg.starmap");
  await expect(big).toBeVisible();
  // The large view has no planet picture, nor planet steps.
  await expect(page.locator("#large-img")).toBeHidden();
  await expect(dialog(page).getByRole("button", { name: /next planet|previous planet/i })).toHaveCount(0);

  const width = () => big.evaluate((s) => s.getBoundingClientRect().width);
  const fitted = await width();
  await page.keyboard.press("+");
  await expect(page.locator("#large-status")).toHaveText("Zoom 115%");
  expect(await width()).toBeCloseTo(fitted * 1.15, 0);
  for (let i = 0; i < 8; i++) await page.keyboard.press("+");
  const frame = page.locator("#large-frame");
  const before = await frame.evaluate((f) => ({ x: f.scrollLeft, y: f.scrollTop }));
  const box = (await frame.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 - 80, box.y + box.height / 2 - 60, { steps: 5 });
  await page.mouse.up();
  const after = await frame.evaluate((f) => ({ x: f.scrollLeft, y: f.scrollTop }));
  expect(after.x).toBeGreaterThan(before.x);
  expect(after.y).toBeGreaterThan(before.y);
  await page.keyboard.press("0");
  expect(await width()).toBeCloseTo(fitted, 0);
  await checkPage(page);
  // Closing puts the planets back: the next open of a planet shows its picture, not the star map.
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  await section(page).getByRole("link", { name: /Nauvis.*opens the large view/ }).click();
  await expect(dialog(page)).not.toHaveAttribute("data-starmap", "");
  await expect(dialog(page).locator("svg.starmap")).toHaveCount(0);
});

// The stored entries of a zip, read through its central directory, as the
// writer streams entries whose sizes are not in their local headers.
function readZip(buf: Buffer): Map<string, Buffer> {
  let end = buf.length - 22;
  while (end >= 0 && buf.readUInt32LE(end) !== 0x06054b50) end--;
  expect(end, "a complete zip").toBeGreaterThanOrEqual(0);
  const out = new Map<string, Buffer>();
  let p = buf.readUInt32LE(end + 16);
  for (let i = buf.readUInt16LE(end + 10); i > 0; i--) {
    const method = buf.readUInt16LE(p + 10);
    const csize = buf.readUInt32LE(p + 20);
    const nameLen = buf.readUInt16LE(p + 28);
    const local = buf.readUInt32LE(p + 42);
    const start = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const raw = buf.subarray(start, start + csize);
    out.set(buf.subarray(p + 46, p + 46 + nameLen).toString(), method === 0 ? Buffer.from(raw) : inflateRawSync(raw));
    p += 46 + nameLen + buf.readUInt16LE(p + 30) + buf.readUInt16LE(p + 32);
  }
  return out;
}

for (const wiki of [false, true]) {
  test(`the zip holds starmap.svg, a standalone SVG with no wiki address (wiki setting ${wiki ? "on" : "off"})`, async ({ page, foreman, fakeGame }) => {
    if (wiki) await page.route(`${wikiBase}**`, (route) => route.fulfill({ status: 200, contentType: "image/png", body: pixel }));
    await modded({ page, foreman, fakeGame: fakeGame! }, { wiki });
    const href = await section(page).getByRole("link", { name: /^Download all drawn planets \(zip\)/ }).getAttribute("href");
    const res = await page.request.get(href!);
    expect(res.status()).toBe(200);
    const files = readZip(await res.body());
    const name = [...files.keys()].find((n) => n.endsWith("starmap.svg"));
    expect(name, `starmap.svg among ${[...files.keys()].join(", ")}`).toBeDefined();
    // The planets' pictures are still there beside it.
    expect([...files.keys()].filter((n) => n.endsWith(".png")).length).toBeGreaterThan(1);
    const svg = files.get(name!)!.toString("utf8");
    expect(svg).toMatch(/^(<\?xml [^>]*\?>\s*)?<svg xmlns="http:\/\/www\.w3\.org\/2000\/svg"/);
    expect(svg).not.toMatch(/wiki\.factorio\.com/);
    expect(svg).not.toMatch(/<image|<script|onerror|href=/i);
    // It is well-formed XML with every visible location's name.
    const parsed = await page.evaluate((s) => {
      const doc = new DOMParser().parseFromString(s, "image/svg+xml");
      return { error: doc.querySelector("parsererror")?.textContent ?? "", root: doc.documentElement.localName, titles: [...doc.querySelectorAll("g > title")].map((t) => t.textContent) };
    }, svg);
    expect(parsed.error).toBe("");
    expect(parsed.root).toBe("svg");
    expect([...parsed.titles].sort()).toEqual([...visible].sort());
  });
}
