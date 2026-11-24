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

test("routes arc about the sun, a moon's route to its planet is straight, and the fake moon has its orbit ring about its planet", async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! }, { moon: true });
  // Nauvis and Vulcanus share only the sun, so the route between them curves:
  // more than one straight segment, and not all on the one line.
  const curved = await routeBetween(page, "Nauvis", "Vulcanus");
  expect(curved.length).toBeGreaterThan(2);
  const [a, b] = [curved[0], curved.at(-1)!];
  const away = curved.map((p) => Math.abs((b[0] - a[0]) * (a[1] - p[1]) - (a[0] - p[0]) * (b[1] - a[1])) / Math.hypot(b[0] - a[0], b[1] - a[1]));
  expect(Math.max(...away)).toBeGreaterThan(1);
  // Every curve is smooth: the spiral out from Aquilo turns at most a few degrees from piece to piece.
  const spiral = await routeBetween(page, "Aquilo", "Solar system edge");
  const heading = (i: number) => Math.atan2(spiral[i][1] - spiral[i - 1][1], spiral[i][0] - spiral[i - 1][0]);
  for (let i = 2; i < spiral.length; i++) {
    const bend = Math.abs(Math.atan2(Math.sin(heading(i) - heading(i - 1)), Math.cos(heading(i) - heading(i - 1))));
    expect(bend * 180 / Math.PI).toBeLessThan(3);
  }
  // Vulcanus's label leaves the game's side (right), where its routes leave, for the clear left.
  await expect(starmap(page).locator("g.starmap__location", { has: page.locator("title", { hasText: /^Vulcanus$/ }) }).locator("text")).toHaveAttribute("text-anchor", "end");

  // A moon's route to its own planet is straight: two points, one segment.
  expect(await routeBetween(page, "Fake planet", "fake-moon")).toHaveLength(2);

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
  // Base places have wiki pictures: the seven of Space Age, none for the sun; the failed one draws nothing.
  await expect.poll(() => asked.filter((u) => /\/(Nauvis|Gleba|Fulgora|Aquilo|Solar_system_edge|Shattered_planet)\.png$/.test(u)).length, live).toBe(6);
  await expect(starmap(page).locator('image[href$="/Nauvis.png"]')).toHaveAttribute("href", `${wikiBase}Nauvis.png`);
  expect(asked.some((u) => /Sun|Star/i.test(u))).toBe(false);
  // The failed picture draws nothing, and its circle stays, with its name.
  await expect(starmap(page).locator('image[href$="/Vulcanus.png"]')).toHaveCount(1);
  await expect(starmap(page).locator("circle.starmap__planet")).toHaveCount(visible.length);
  expect(await titles(page).allTextContents()).toContain("Vulcanus");
  // The mod's own picture is still Foreman's.
  await expect(starmap(page).locator('image[href$="/icons/starmap/fake-planet.png"]')).toHaveCount(1);
  const res = await page.request.get(newMapPath(id));
  expect(res.headers()["referrer-policy"]).toBe("no-referrer");
  await checkPage(page);
});

test("a plain Space Age world with no other mods shows the star map with Space Age's seven places", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "Plain" });
  await section(page).locator("label.tab", { hasText: "All planets" }).click();
  await expect(starmap(page)).toBeVisible({ timeout: 60_000 });
  const names = await titles(page).allTextContents();
  expect([...names].sort()).toEqual(visible.filter((n) => n !== "Fake planet").sort());
  await expect(starmap(page).locator("circle.starmap__planet")).toHaveCount(7);
  // The game's own routes are drawn, the Nauvis to Vulcanus one curved about the sun.
  expect((await routeBetween(page, "Nauvis", "Vulcanus")).length).toBeGreaterThan(2);
  await checkPage(page);
});

test("the star map opens in the large view with zoom and pan", async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! });
  await section(page).getByRole("link", { name: /Star map.*opens the large view/ }).click();
  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  await expect(page.locator("#large-title")).toHaveText("Star map");
  const big = dialog(page).locator("svg.starmap");
  await expect(big).toBeVisible();
  // The large view has no planet picture.
  await expect(page.locator("#large-img")).toBeHidden();

  const width = () => big.evaluate((s) => s.getBoundingClientRect().width);
  const fitted = await width();
  // The sun's rings carry on past the square picture to the frame's edge, which clips them;
  // what is drawn past the picture never makes the frame scroll.
  expect(await big.evaluate((s) => getComputedStyle(s).overflow)).toBe("visible");
  const past = await big.evaluate((s) => {
    const box = s.getBoundingClientRect();
    return Math.max(...[...s.querySelectorAll("g.starmap__rings circle:not([cx])")].map((c) => box.left - c.getBoundingClientRect().left));
  });
  expect(past).toBeGreaterThan(0);
  const frameEl = page.locator("#large-frame");
  expect(await frameEl.evaluate((f) => f.scrollWidth <= f.clientWidth + 1 && f.scrollHeight <= f.clientHeight + 1)).toBe(true);
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

test("the star map is the large view's first step: Next goes to the first planet, Previous comes back", async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! });
  const first = await page.locator("#preview-planets option").first().evaluate((o) => (o as HTMLOptionElement).text);
  const last = await page.locator("#preview-planets option").last().evaluate((o) => (o as HTMLOptionElement).text);
  await section(page).getByRole("link", { name: /Star map.*opens the large view/ }).click();
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  // The steps lead round: the first planet after the star map, the last planet before it.
  await expect(dialog(page).getByRole("button", { name: `Next planet: ${first}` })).toBeVisible();
  await expect(dialog(page).getByRole("button", { name: `Previous planet: ${last}` })).toBeVisible();
  // On the star map the planet-only controls are hidden, the download is its file, and the arrows step.
  for (const id of ["large-amounts-bar", "large-territories-box", "large-settings"]) await expect(page.locator(`#${id}`)).toBeHidden();
  await expect(page.locator("#large-download")).toHaveAttribute("href", /starmap\.svg/);
  await expect(page.locator("#large-keys")).toContainText("planet");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator("#large-title")).toHaveText(first);
  await expect(dialog(page)).not.toHaveAttribute("data-starmap", "");
  await expect(dialog(page).locator("svg.starmap")).toHaveCount(0);
  await expect(page.locator("#large-img")).toBeVisible();
  await expect(page.locator("#large-amounts-bar")).toBeVisible();
  await expect(page.locator("#large-download")).not.toHaveAttribute("href", /starmap\.svg/);
  await expect(dialog(page).getByRole("button", { name: "Previous planet: Star map" })).toBeVisible();
  // Previous from the first planet is the star map again.
  await dialog(page).getByRole("button", { name: "Previous planet: Star map" }).click();
  await expect(page.locator("#large-title")).toHaveText("Star map");
  await expect(dialog(page).locator("svg.starmap")).toBeVisible();
  await expect(page.locator("#large-download")).toHaveAttribute("href", /starmap\.svg/);
  // Previous from the star map is the last planet; closing then returns focus to that planet's picture.
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator("#large-title")).toHaveText(last);
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  await expect.poll(() => page.evaluate(() => document.activeElement?.closest("#preview figure")?.querySelector("figcaption")?.textContent?.trim())).toBe(last);
});

test("in the One planet view, with nothing else drawn, Previous from the planet shows the star map", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "One" });
  const picture = section(page).locator("a.picture[data-planet]");
  await expect(picture).toHaveCount(1, { timeout: 60_000 });
  // The star map is not seen on the page in this view, yet the large view reaches it.
  await expect(starmap(page)).toHaveCount(0);
  const planet = (await picture.locator("xpath=..").locator("figcaption").textContent())!.trim();
  await picture.click();
  await expect(page.locator("#large-title")).toHaveText(planet);
  await dialog(page).getByRole("button", { name: "Previous planet: Star map" }).click();
  await expect(page.locator("#large-title")).toHaveText("Star map");
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  await expect(dialog(page).locator("svg.starmap")).toBeVisible();
  await expect(page.locator("#large-download")).toHaveAttribute("href", /starmap\.svg/);
  await page.keyboard.press("ArrowRight");
  await expect(page.locator("#large-title")).toHaveText(planet);
  await expect(page.locator("#large-img")).toBeVisible();
  // Back to the star map, then on past the first planet to one not drawn yet, which is drawn.
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator("#large-title")).toHaveText("Star map");
  await page.keyboard.press("ArrowLeft");
  await expect(page.locator("#large-title")).not.toHaveText("Star map");
  await expect(page.locator("#large-img")).toBeVisible({ timeout: 20_000 });
  await expect(page.locator("#large-frame")).not.toHaveAttribute("data-missing", "true", { timeout: 20_000 });
  // Closing from the star map, unseen on the page, puts focus on the planet's picture.
  await page.keyboard.press("ArrowRight");
  await expect(page.locator("#large-title")).toHaveText("Star map");
  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  await expect(section(page).locator("a.picture[data-planet]").first()).toBeFocused();
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
