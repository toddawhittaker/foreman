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
// fake moon on through the mod's startup setting; bare sets it to its largest value, which
// also adds fake-bare, a planet with no map controls at all.
async function modded({ page, foreman, fakeGame }: Env, opts: { moon?: boolean; bare?: boolean; wiki?: boolean } = {}): Promise<string> {
  await createAdmin(page);
  if (opts.wiki) await wikiOn(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  if (opts.moon || opts.bare) {
    await page.goto(`${newMapPath(id)}/mod-settings`);
    await page.getByLabel("fm-sta-int").fill(opts.bare ? "10" : "9");
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
  // Vulcanus's label leaves the game's side (right), where its routes leave, for a clear spot.
  await expect(starmap(page).locator("g.starmap__location", { has: page.locator("title", { hasText: /^Vulcanus$/ }) }).locator("text")).not.toHaveAttribute("text-anchor", "start");

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

// DESIGN 8.23: a planet's picture shows only once it has loaded. Slow, or failed and asked
// again, its circle shows plain meanwhile, never the browser's broken picture; the large view's
// copy shows it too.
test("a slow or failed planet picture leaves the plain circle until a later try loads it", async ({ page, foreman, fakeGame }) => {
  const icon = "**/icons/starmap/fake-planet.png";
  let asked = 0;
  let release!: () => void;
  const held = new Promise<void>((r) => { release = r; });
  await page.route(icon, async (route) => {
    asked += 1;
    // The first try is refused as busy; the next waits until the test lets it go.
    if (asked === 1) return route.fulfill({ status: 503, headers: { "Retry-After": "2" }, body: "" });
    await held;
    return route.continue();
  });
  await modded({ page, foreman, fakeGame: fakeGame! });
  const own = starmap(page).locator("g.starmap__location", { has: page.locator("title", { hasText: /^Fake planet$/ }) });
  await expect.poll(() => asked, live).toBe(2);
  await expect(own.locator("image")).toBeHidden();
  await expect(own.locator("image")).not.toHaveAttribute("data-loaded");
  await expect(own.locator("circle.starmap__planet")).toBeVisible();
  release();
  await expect(own.locator("image")).toHaveAttribute("data-loaded", "", live);
  await expect(own.locator("image")).toBeVisible();
  // The large view's copy shows the picture too.
  await section(page).getByRole("link", { name: /Star map.*opens the large view/ }).click();
  await expect(dialog(page).locator('g[data-location="fake-planet"] image')).toBeVisible();
});

// @slow: the tries are 1, 2 and 4 s apart, and showing that no fifth follows waits past the next 8 s.
test("a planet picture that keeps failing is asked for four times in all, then the plain circle stays", { tag: "@slow" }, async ({ page, foreman, fakeGame }) => {
  test.slow();
  let asked = 0;
  await page.route("**/icons/starmap/fake-planet.png", (route) => {
    asked += 1;
    return route.fulfill({ status: 500, body: "" });
  });
  await modded({ page, foreman, fakeGame: fakeGame! });
  const own = starmap(page).locator('g[data-location="fake-planet"]');
  // The first try, then 1, 2 and 4 seconds later.
  await expect.poll(() => asked, { timeout: 15_000 }).toBe(4);
  await page.waitForTimeout(9_000);
  expect(asked).toBe(4);
  await expect(own.locator("image")).toBeHidden();
  await expect(own.locator("circle.starmap__planet")).toBeVisible();
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

// One planet's view of a plain Space Age world, its large view open on the star map.
async function starMapStep(page: Page, foreman: Env["foreman"], fakeGame: Env["fakeGame"]) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "One" });
  const picture = section(page).locator("a.picture[data-planet]");
  await expect(picture).toHaveCount(1, { timeout: 60_000 });
  await picture.click();
  await dialog(page).getByRole("button", { name: "Previous planet: Star map" }).click();
  await expect(page.locator("#large-title")).toHaveText("Star map");
}

const bigMap = (page: Page) => dialog(page).locator("svg.starmap");
const place = (page: Page, title: string) => bigMap(page).locator("g.starmap__location", { has: page.locator(`title:text-is("${title}")`) });

test("a planet on the large view's star map steps to that planet, drawing it first; other places and a pan do not step", async ({ page, foreman, fakeGame }) => {
  await starMapStep(page, foreman, fakeGame!);
  // The picture is a group here, keeping its text alternative, so its planets can be found.
  await expect(bigMap(page)).toHaveAttribute("role", "group");
  await expect(bigMap(page)).toHaveAccessibleName(/^Star map:/);
  const vulcanus = bigMap(page).getByRole("button", { name: "Vulcanus" });
  await expect(vulcanus).toBeVisible();
  // The edge and the shattered planet are drawn but are no controls, and a click on them does nothing.
  for (const name of ["Solar system edge", "Shattered planet"]) {
    await expect(place(page, name)).toHaveCount(1);
    await expect(place(page, name)).not.toHaveAttribute("role", "button");
    await expect(place(page, name)).not.toHaveAttribute("tabindex", /./);
    await place(page, name).locator("circle").click({ force: true });
    await expect(page.locator("#large-title")).toHaveText("Star map");
  }
  // A drag that starts on Vulcanus pans and does not step.
  const box = (await vulcanus.locator("circle").boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 40, box.y + box.height / 2 + 30, { steps: 5 });
  await page.mouse.up();
  await expect(page.locator("#large-title")).toHaveText("Star map");
  // A click on Vulcanus, not drawn yet in this view, draws it and shows its controls.
  await vulcanus.locator("circle").click();
  await expect(page.locator("#large-title")).toHaveText("Vulcanus");
  await expect(dialog(page)).not.toHaveAttribute("data-starmap", "");
  await expect(page.locator("#large-img")).toBeVisible({ timeout: 20_000 });
  await expect(page.locator("#large-frame")).not.toHaveAttribute("data-missing", "true", { timeout: 20_000 });
  await expect(page.locator("#large-amounts-bar")).toBeVisible();
  await expect(page.locator("#large-territories-box")).toBeVisible();
  await expect(page.locator("#large-frame")).toBeFocused();
});

test("a planet on the large view's star map is reached with Tab and stepped to with Enter", async ({ page, foreman, fakeGame }) => {
  await starMapStep(page, foreman, fakeGame!);
  const gleba = bigMap(page).getByRole("button", { name: "Gleba" });
  await page.locator("#large-frame").focus();
  for (let i = 0; i < 12 && !(await gleba.evaluate((g) => g === document.activeElement)); i++) await page.keyboard.press("Tab");
  await expect(gleba).toBeFocused();
  // The focused planet has a visible ring.
  expect(await gleba.evaluate((g) => getComputedStyle(g).outlineStyle)).not.toBe("none");
  await page.keyboard.press("Enter");
  await expect(page.locator("#large-title")).toHaveText("Gleba");
  await expect(page.locator("#large-amounts-bar")).toBeVisible();
});

// DESIGN 8.8: Previous and Next sit in one place for every picture, with or without settings,
// Vulcanus's box or the star map, so clicks in one spot keep stepping. fake-bare has no settings.
test("the planet arrows stay in one place for every planet and the star map", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! }, { bare: true });
  await section(page).getByRole("link", { name: /Nauvis.*opens the large view/ }).click();
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
  const boxes = async () => ({ prev: await page.locator("#large-prev").boundingBox(), next: await page.locator("#large-next").boundingBox() });
  const first = await boxes();
  const names = await page.locator("#preview-planets option").allTextContents();
  expect(names.length).toBeGreaterThan(2);
  // Clicks on Next in the same spot go round every planet, through the star map, and back to
  // Nauvis; the star map shows the arrows too, in the same places (SPEC 5.6: it is the first step).
  const at = (b: typeof first.next) => ({ x: b!.x + b!.width / 2, y: b!.y + b!.height / 2 });
  const round = [...names.slice(1), "Star map", names[0]];
  for (const name of round) {
    await page.mouse.click(at(first.next).x, at(first.next).y);
    await expect(page.locator("#large-title")).toHaveText(name);
    await expect(page.locator("#large-next")).toBeVisible();
    expect(await boxes()).toEqual(first);
  }
  // And back again with Previous, clicked in its one spot.
  for (const name of ["Star map", ...[...names].reverse()]) {
    await page.mouse.click(at(first.prev).x, at(first.prev).y);
    await expect(page.locator("#large-title")).toHaveText(name);
    expect(await boxes()).toEqual(first);
  }
  // The header's Star map button reaches the same step, arrows unmoved.
  await dialog(page).getByRole("button", { name: "Star map", exact: true }).click();
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  await expect(page.locator("#large-prev")).toBeVisible();
  expect(await boxes()).toEqual(first);
  await page.screenshot({ path: `../screenshots/starmap-large-${test.info().project.name}.png` });

  // A count's note or error can take several lines beside the box and grow the bar; the arrows stay.
  await fakeGame!.call("/preview-mode", { mode: "no-patches" });
  for (const name of names.slice(0, names.indexOf("Fake planet") + 1)) {
    await page.mouse.click(at(first.next).x, at(first.next).y);
    await expect(page.locator("#large-title")).toHaveText(name);
  }
  const amounts = dialog(page).getByRole("checkbox", { name: "Show resource amounts" });
  const result = page.locator("#large-amounts-result");
  await amounts.check();
  await expect(result.locator(".message--note")).toContainText("This planet's resources may be tiles, which can't be counted.");
  await expect(page.locator("#large-prev")).toBeVisible();
  expect(await boxes()).toEqual(first);
  await page.screenshot({ path: `../screenshots/large-view-note-${test.info().project.name}.png` });
  await amounts.uncheck();
  await expect(result).toBeEmpty();
  expect(await boxes()).toEqual(first);
  await page.route((u) => u.pathname.endsWith("/generate/preview/resources"), (r) => r.fulfill({ status: 500, body: "" }));
  await amounts.check();
  await expect(result.locator(".message--error")).toContainText("The request for the resource amounts failed.");
  await expect(result.getByRole("button", { name: "Try again" })).toBeVisible();
  expect(await boxes()).toEqual(first);
  await page.screenshot({ path: `../screenshots/large-view-error-${test.info().project.name}.png` });
  // Stepping on with the error shown keeps them too.
  await page.mouse.click(at(first.next).x, at(first.next).y);
  await expect(page.locator("#large-title")).not.toHaveText("Fake planet");
  expect(await boxes()).toEqual(first);
});

// SPEC 5.6: a planet whose mod gives it no resource or terrain controls says so where its
// settings would be, on the page in both tabs and once in the large view's settings column.
test("a planet with no map controls says so in its settings row and in the large view", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await modded({ page, foreman, fakeGame: fakeGame! }, { bare: true });
  const sentence = /^fake-bare has no resource or terrain settings\. Its mod places them without controls, so the game offers none either\.$/;
  const row = page.locator('#panel-resources > details[data-planet="fake-bare"]');
  await row.locator("summary").click();
  await expect(row.locator(".mapgen-none")).toHaveText(sentence);
  await expect(row.locator("select, input")).toHaveCount(0);
  await expect(page.locator('#panel-terrain > details[data-planet="fake-bare"] .mapgen-none')).toHaveText(sentence);
  // Fake planet has a resource control and no terrain one: no sentence for it.
  await expect(page.locator('details[data-planet="fake-planet"] .mapgen-none')).toHaveCount(0);
  await checkPage(page);

  await section(page).getByRole("link", { name: /fake-bare.*opens the large view/ }).click();
  await expect(dialog(page)).toBeVisible();
  const wide = (page.viewportSize()?.width ?? 0) >= 1240;
  // The settings column shows only on a wide view (DESIGN 8.8); there it says it once.
  if (wide) {
    await expect(dialog(page).locator(".mapgen-none:visible")).toHaveCount(1);
    await expect(dialog(page).locator(".mapgen-none:visible")).toHaveText(sentence);
    await page.screenshot({ path: `../screenshots/no-controls-large-${test.info().project.name}.png` });
  }
  await page.keyboard.press("Escape");
  // The rows go back to the form, each tab's with its sentence.
  await expect(page.locator('#panel-resources > details[data-planet="fake-bare"] .mapgen-none')).toHaveText(sentence);
  await expect(page.locator('#panel-terrain > details[data-planet="fake-bare"] .mapgen-none')).toHaveText(sentence);
});

// A plain Space Age world with every planet shown, so the preview ends with the star map.
async function plainAll({ page, foreman, fakeGame }: Env) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
  await openNewMap(page, { name: "Plain" });
  await section(page).locator("label.tab", { hasText: "All planets" }).click();
  await expect(starmap(page)).toBeVisible({ timeout: 60_000 });
}

// DESIGN 8.8: from a planet, the large view's header goes to the star map in one click.
test("the large view's Star map button shows the star map in one click from a planet, in every preview view", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await plainAll({ page, foreman, fakeGame: fakeGame! });
  await section(page).getByRole("link", { name: /Nauvis.*opens the large view/ }).click();
  await expect(page.getByRole("dialog", { name: "Nauvis" })).toBeVisible();
  const button = dialog(page).getByRole("button", { name: "Star map", exact: true });
  await expect(button).toBeVisible();
  // Its touch target is at least the download icon's beside it.
  const [b, d] = [(await button.boundingBox())!, (await page.locator("#large-download").boundingBox())!];
  expect(b.height).toBeGreaterThanOrEqual(d.height - 0.5);
  await page.screenshot({ path: `../screenshots/starmap-button-${test.info().project.name}.png` });
  await page.keyboard.press("+");
  await button.click();
  await expect(page.getByRole("dialog", { name: "Star map" })).toBeVisible();
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  await expect(dialog(page).locator("svg.starmap")).toBeVisible();
  await expect(page.locator("#large-img")).toBeHidden();
  // As opening it from the page: fitted, the map focused, the download the star map's, and its name said.
  await expect(page.locator("#large-frame")).toBeFocused();
  await expect(page.locator("#large-frame")).not.toHaveAttribute("data-zoomed", "");
  await expect(page.locator("#large-download")).toHaveAttribute("href", /starmap/);
  await expect(page.locator("#large-status")).toHaveText("Star map");
  // Hidden while the star map is shown.
  await expect(button).toBeHidden();
  await checkPage(page);
  // Closing goes back to the page's star map picture.
  await page.keyboard.press("Escape");
  await expect(section(page).getByRole("link", { name: /Star map.*opens the large view/ })).toBeFocused();

  // With One planet the page shows no star map, yet it is still the view's first step (SPEC 5.6),
  // so the button offers it, and Next from it is the first planet.
  await section(page).locator("label.tab", { hasText: "One planet" }).click();
  await expect(starmap(page)).toHaveCount(0, { timeout: 60_000 });
  await section(page).getByRole("link", { name: /opens the large view/ }).click();
  await expect(dialog(page)).toBeVisible();
  const planet = await page.locator("#large-title").textContent();
  await button.click();
  await expect(page.locator("#large-title")).toHaveText("Star map");
  await expect(button).toBeHidden();
  await dialog(page).getByRole("button", { name: /^Next planet: / }).click();
  await expect(page.locator("#large-title")).toHaveText(await page.locator("#preview-planets option").first().textContent() ?? planet!);
});

// DESIGN 8.8: zoomed in, any edge of the picture can pan to the frame's middle; fitted, nothing scrolls.
test("zoomed in, the star map pans until any edge reaches the frame's middle, and Zoom in keeps the middle in place", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await plainAll({ page, foreman, fakeGame: fakeGame! });
  await section(page).getByRole("link", { name: /Star map.*opens the large view/ }).click();
  await expect(dialog(page)).toHaveAttribute("data-starmap", "");
  const frame = page.locator("#large-frame");
  for (let i = 0; i < 3; i++) await page.keyboard.press("+");
  await expect(page.locator("#large-status")).toHaveText("Zoom 152%");
  // Where the picture's edges are, against the middle of the frame's visible area.
  const edges = () => frame.evaluate((f) => {
    const box = f.getBoundingClientRect();
    const mid = { x: box.left + f.clientLeft + f.clientWidth / 2, y: box.top + f.clientTop + f.clientHeight / 2 };
    const pic = f.querySelector("svg.starmap")!.getBoundingClientRect();
    return { left: pic.left - mid.x, right: pic.right - mid.x, top: pic.top - mid.y, bottom: pic.bottom - mid.y };
  });
  const scrollTo = (end: boolean) => frame.evaluate((f, e) => {
    f.scrollLeft = e ? f.scrollWidth : 0;
    f.scrollTop = e ? f.scrollHeight : 0;
  }, end);
  await scrollTo(false);
  let e = await edges();
  expect(Math.abs(e.left)).toBeLessThan(1.5);
  expect(Math.abs(e.top)).toBeLessThan(1.5);
  await scrollTo(true);
  e = await edges();
  expect(Math.abs(e.right)).toBeLessThan(1.5);
  expect(Math.abs(e.bottom)).toBeLessThan(1.5);

  // From near an edge, Zoom in keeps the spot at the middle where it was.
  await scrollTo(false);
  await frame.evaluate((f) => f.scrollBy(40, 30));
  const spot = async () => {
    const at = await edges();
    return { u: -at.left / (at.right - at.left), v: -at.top / (at.bottom - at.top), w: at.right - at.left };
  };
  const before = await spot();
  await dialog(page).getByRole("button", { name: "Zoom in" }).click();
  await expect(page.locator("#large-status")).toHaveText("Zoom 175%");
  const after = await spot();
  expect(after.w).toBeCloseTo(before.w * 1.15, 0);
  expect(Math.abs(after.u - before.u) * after.w).toBeLessThan(1.5);
  expect(Math.abs(after.v - before.v) * after.w).toBeLessThan(1.5);

  // Fitted again, the picture is centred and nothing scrolls.
  await page.keyboard.press("0");
  expect(await frame.evaluate((f) => f.scrollWidth <= f.clientWidth + 1 && f.scrollHeight <= f.clientHeight + 1)).toBe(true);
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
