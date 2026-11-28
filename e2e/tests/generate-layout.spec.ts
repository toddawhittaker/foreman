import { test, expect, checkPage, createAdmin, seedWorld, image21, live, makeZip } from "./foreman.js";
import type { Page, Route } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { openNewMap, newMapPath } from "./newmap.js";

// Fixes batch 20, rulings 1 and 2 on Generate's pages: nothing moves because
// of a change elsewhere on the page (0 px in x and y, scrollY unchanged), and
// each thing is drawn one way. The issues: #973 (the map keeps its size),
// #974 (Previous on the star map), #975 (one working bar in a fixed spot),
// #977 (one row per resource in the large view), #966 (the star map's labels
// fit), #984 (one page header on every step), #986 (a disclosure opens under
// its summary) and #987 (one footer, no sideways scroll). Each test walks the
// widths and themes itself, so it runs once, in the first project.
test.use({ fakeGameOn: true });

type Box = { x: number; y: number; width: number; height: number };
const widths = [
  { width: 1920, height: 1080 },
  { width: 390, height: 844 },
];
const themes = ["dark", "light"] as const;

async function each(page: Page, run: (label: string, wide: boolean) => Promise<void>, sizes = widths) {
  for (const size of sizes) {
    await page.setViewportSize(size);
    for (const theme of themes) {
      await page.emulateMedia({ colorScheme: theme });
      await run(`${size.width} ${theme}`, size.width >= 1240);
    }
  }
}

async function box(page: Page, selector: string): Promise<Box> {
  const b = await page.locator(selector).first().boundingBox();
  expect(b, selector).not.toBeNull();
  return b!;
}

// The boxes of every selector, and the page's scroll, at one moment, read
// in one call to the page so a slow runner pays one round trip, not one per
// selector.
async function snap(page: Page, selectors: string[]) {
  const read = await page.evaluate((sels) => ({
    scrollY: window.scrollY,
    boxes: sels.map((s) => {
      const el = document.querySelector(s);
      // As Playwright's boundingBox: an element with no box (display: none) has none.
      if (!el || el.getClientRects().length === 0) return null;
      const r = el.getBoundingClientRect();
      return { x: r.x, y: r.y, width: r.width, height: r.height };
    }),
  }), selectors);
  const out: Record<string, Box | number> = { scrollY: read.scrollY };
  selectors.forEach((s, i) => {
    expect(read.boxes[i], s).not.toBeNull();
    out[s] = read.boxes[i]!;
  });
  return out;
}

const scrollsSideways = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth);
// What reaches past the window's edge, named, so a failure says where.
const pastEdge = (page: Page) => page.evaluate(() => {
  const edge = document.documentElement.clientWidth;
  return [...document.querySelectorAll("main *")].filter((el) => {
    const r = el.getBoundingClientRect();
    return r.width > 0 && (r.right > edge + 0.5 || r.left < -0.5) && getComputedStyle(el).position !== "fixed" && !el.closest(".visually-hidden, dialog:not([open])");
  }).slice(0, 6).map((el) => `${el.tagName.toLowerCase()}#${el.id}.${[...el.classList].join(".")} ${Math.round(el.getBoundingClientRect().left)}..${Math.round(el.getBoundingClientRect().right)}`);
});
const section = (page: Page) => page.getByRole("region", { name: "Map preview" });

// Holds the requests to a path until released, so a test sees the working state.
function hold(page: Page, ends: string) {
  const held: Route[] = [];
  const match = (u: URL) => u.pathname.endsWith(ends);
  return {
    start: () => page.route(match, (r) => { held.push(r); }),
    waiting: () => held.length,
    release: async () => {
      for (const r of held.splice(0)) await r.continue().catch(() => {});
      await page.unroute(match);
    },
  };
}

// label: the fake planet's name in its mod's locale, else the test mod's own zip.
function addFakePlanet(dataDir: string, id: string, label?: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  const zip = join(mods, "fake-planet_1.0.0.zip");
  if (label === undefined) copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), zip);
  else writeFileSync(zip, makeZip({
    "fake-planet_1.0.0/info.json": JSON.stringify({ name: "fake-planet", version: "1.0.0", title: "Fake planet", author: "Foreman tests", factorio_version: "2.0", dependencies: ["base"] }),
    "fake-planet_1.0.0/locale/en/fake-planet.cfg": `[space-location-name]\nfake-planet=${label}\n`,
  }));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: ["base", "elevated-rails", "quality", "space-age"].map((name) => ({ name, enabled: true })) }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

// The end state of each step, so a box is read once what the step adds has landed.
async function amountsDone(page: Page, root: "#preview" | "#large-view", on: boolean) {
  const work = root === "#preview" ? "#preview-amounts-work" : "#large-amounts-work";
  await expect(page.locator(`${work} .working`)).toBeHidden(live);
  if (on) await expect(page.locator(`${root} .resources`).first()).toBeVisible(live);
  else await expect(page.locator(`${root} .resources`)).toHaveCount(0, live);
  if (on && root === "#preview") await expect(page.locator("#preview-amounts-result details.resource-text")).toBeVisible(live);
  if (!on && root === "#preview") await expect(page.locator("#preview-amounts-result")).toBeEmpty(live);
  await expect(page.locator("#preview")).not.toHaveAttribute("aria-busy", "true");
}
async function territoriesDone(page: Page, root: "#preview" | "#large-view") {
  await expect(page.locator(root === "#preview" ? "#preview-territories-work .working" : "#large-territories-work .working")).toBeHidden(live);
  await expect(page.locator(`${root} .map .territories`).first()).toBeVisible(live);
  await expect(page.locator("#territory-result")).not.toBeEmpty(live);
  await expect(page.locator("#preview")).not.toHaveAttribute("aria-busy", "true");
}

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

// Step 4 on Vulcanus, its One planet picture drawn.
async function vulcanus(page: Page, id: string) {
  await page.goto(newMapPath(id));
  await expect(section(page).getByRole("img")).toHaveCount(1, { timeout: 60_000 });
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(section(page).getByRole("img", { name: /^Map preview of Vulcanus/ })).toBeVisible();
}

async function openLarge(page: Page) {
  await section(page).getByRole("link", { name: /opens the large view/ }).first().click();
  await expect(page.locator("#large-view")).toBeVisible();
  await expect.poll(() => page.locator("#large-img").evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
}

test("#984: steps 1 to 4 put the crumb's row, the title and the step bar in one place", async ({ page, foreman }) => {
  const id = await openNewMap(page, { name: "Steps" });
  addFakePlanet(foreman.dataDir, id);
  const paths = ["/generate", `/worlds/${id}/mods`, `${newMapPath(id)}/mod-settings`, newMapPath(id)];
  await each(page, async (label) => {
    const seen: Record<string, Box>[] = [];
    for (const path of paths) {
      await page.goto(path);
      await expect(page.locator("#flow-steps")).toBeVisible(live);
      // The header partial, on every step.
      await expect(page.locator("main > header.pagehead #flow-steps")).toHaveCount(1);
      const h1 = await box(page, "main h1");
      seen.push({
        crumb: await box(page, ".pagehead__crumb"),
        // The title's words differ; its start, its line and its height do not.
        h1: { x: h1.x, y: h1.y, width: 0, height: h1.height },
        steps: await box(page, "#flow-steps"),
      });
    }
    expect(seen[1], `${label}: step 2 against step 1`).toEqual(seen[0]);
    expect(seen[2], `${label}: step 3 against step 1`).toEqual(seen[0]);
    expect(seen[3], `${label}: step 4 against step 1`).toEqual(seen[0]);
  });
});

// A box in the preview's pane, whatever the pane has scrolled; the pane may stick to the window.
const inPage = (page: Page, selector: string) => page.locator(selector).first().evaluate((el) => {
  const r = el.getBoundingClientRect();
  const pane = el.closest(".gen-preview")!;
  return { x: r.x, y: r.y - pane.getBoundingClientRect().y + pane.scrollTop, width: r.width, height: r.height };
});

test("#973: the map keeps its size as Show resource amounts is ticked and its list opens", async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    await page.goto(newMapPath(id));
    const map = section(page).locator("a.picture .map img").first();
    await expect(map).toBeVisible({ timeout: 60_000 });
    await expect.poll(() => map.evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    const parts = ["#preview a.picture .map img", "#preview-amounts"];
    const at = async () => Object.fromEntries(await Promise.all(parts.map(async (s) => [s, await inPage(page, s)])));
    await page.locator("#preview-amounts").scrollIntoViewIfNeeded();
    const off = await at();
    const y = await page.evaluate(() => window.scrollY);
    await page.locator("#preview-amounts").check();
    const list = page.locator("#preview-amounts-result details.resource-text");
    await expect(list).toBeVisible(live);
    expect(await at(), `${label}: ticked`).toEqual(off);
    expect(await page.evaluate(() => window.scrollY), `${label}: ticked, scrollY`).toBe(y);
    await list.locator("summary").scrollIntoViewIfNeeded();
    const y2 = await page.evaluate(() => window.scrollY);
    await list.locator("summary").click();
    await expect(list).toHaveAttribute("open", "");
    expect(await at(), `${label}: list open`).toEqual(off);
    expect(await page.evaluate(() => window.scrollY), `${label}: list open, scrollY`).toBe(y2);
  });
});

// One test per width, so each stays well inside the time limit on a slow runner.
for (const size of widths) test(`#975: in the panel, each box has its working bar under it, and ticking one moves neither, ${size.width} px`, async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    await vulcanus(page, id);
    // The hint is gone; nothing describes the box any more.
    await expect(page.getByText("Finding them takes a few seconds more.")).toHaveCount(0);
    await expect(page.locator("#preview-territories")).not.toHaveAttribute("aria-describedby");
    const parts = ["#preview-territories", "label:has(> #preview-territories)", "#preview-amounts", "label:has(> #preview-amounts)", "#preview-territories-work", "#preview-amounts-work"];
    await page.locator("#preview-amounts").scrollIntoViewIfNeeded();
    const at = await snap(page, parts);
    // Amounts, held: its bar shows under its box, and nothing moves.
    const amounts = hold(page, "/generate/preview/resources");
    await amounts.start();
    await page.locator("#preview-amounts").check();
    await expect(page.locator("#preview-amounts-work .working")).toBeVisible();
    await expect(page.locator("#preview-territories-work .working")).toBeHidden();
    expect(await snap(page, parts), `${label}: counting`).toEqual(at);
    await amounts.release();
    await amountsDone(page, "#preview", true);
    expect(await snap(page, parts), `${label}: counted`).toEqual(at);
    // Territories, held: the same bar under its box, not the old line of words.
    const terr = hold(page, "/generate/preview/territories");
    await terr.start();
    await page.locator("#preview-territories").check();
    await expect(page.locator("#preview-territories-work .working")).toBeVisible();
    await expect(page.locator("#preview-territories-work .working__bar")).toBeVisible();
    await expect(page.locator("#territory-result")).not.toContainText("Finding the demolisher territories");
    // The bar is hidden from screen readers, so the start is said (WCAG 4.1.3).
    await expect(page.locator("#preview-territories-work")).toHaveAttribute("aria-hidden", "true");
    await expect(page.locator("#announcer")).toHaveText("Finding the demolisher territories…");
    expect(await snap(page, parts), `${label}: finding`).toEqual(at);
    // A slot busy passes axe too.
    await checkPage(page);
    await terr.release();
    // The amounts are counted again for the new picture; once both are done nothing has moved.
    await territoriesDone(page, "#preview");
    await amountsDone(page, "#preview", true);
    expect(await snap(page, parts), `${label}: found`).toEqual(at);
    // Five times off and on with the amounts already counted.
    for (let i = 0; i < 5; i++) {
      await page.locator("#preview-amounts").setChecked(i % 2 === 1);
      await amountsDone(page, "#preview", i % 2 === 1);
      expect(await snap(page, parts), `${label}: toggle ${i}`).toEqual(at);
    }
  }, [size]);
});

for (const size of widths) test(`#975: in the large view of Vulcanus, ticking either box never moves the other, ${size.width} px`, async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    await vulcanus(page, id);
    await openLarge(page);
    await expect(page.locator("#large-view")).toHaveAccessibleName("Vulcanus");
    await expect(page.locator("#large-territories")).not.toHaveAttribute("aria-describedby");
    await expect(page.locator("#large-territories-hint")).toHaveCount(0);
    const parts = ["#large-territories", "#large-amounts", "#large-territories-work", "#large-amounts-work", "#large-frame", "#large-prev", "#large-next"];
    const at = await snap(page, parts);
    const amounts = hold(page, "/generate/preview/resources");
    await amounts.start();
    await page.locator("#large-amounts").check();
    await expect(page.locator("#large-amounts-work .working")).toBeVisible();
    expect(await snap(page, parts), `${label}: counting`).toEqual(at);
    await amounts.release();
    await amountsDone(page, "#large-view", true);
    expect(await snap(page, parts), `${label}: counted`).toEqual(at);
    const terr = hold(page, "/generate/preview/territories");
    await terr.start();
    await page.locator("#large-territories").check();
    await expect(page.locator("#large-territories-work .working")).toBeVisible();
    expect(await snap(page, parts), `${label}: finding`).toEqual(at);
    await terr.release();
    await territoriesDone(page, "#large-view");
    await amountsDone(page, "#large-view", true);
    expect(await snap(page, parts), `${label}: found`).toEqual(at);
    // The large view passes axe too.
    await checkPage(page);
    for (let i = 0; i < 5; i++) {
      await page.locator("#large-amounts").setChecked(i % 2 === 1);
      await amountsDone(page, "#large-view", i % 2 === 1);
      expect(await snap(page, parts), `${label}: toggle ${i}`).toEqual(at);
    }
    await page.keyboard.press("Escape");
    await expect(page.locator("#large-view")).toBeHidden();
    // Leave both off for the next round.
    await page.locator("#preview-territories").uncheck();
  }, [size]);
});

test("#984: the large view's header keeps its parts in place from planet to planet", async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    await page.goto(newMapPath(id));
    await expect(section(page).getByRole("img")).toHaveCount(1, { timeout: 60_000 });
    await openLarge(page);
    const parts = ["#large-title", "#large-download", "#large-starmap", "#large-amounts", "#large-frame", "#large-prev", "#large-next"];
    const first = await snap(page, parts);
    // The title's own width follows its words; its start does not.
    const title = first["#large-title"] as Box;
    first["#large-title"] = { ...title, width: 0 };
    const names = await page.locator("#preview-planets option").allTextContents();
    for (const name of names.slice(1)) {
      await page.locator("#large-next").click();
      await expect(page.locator("#large-title")).toHaveText(name);
      const now = await snap(page, parts);
      now["#large-title"] = { ...(now["#large-title"] as Box), width: 0 };
      expect(now, `${label}: ${name}`).toEqual(first);
    }
    await page.keyboard.press("Escape");
    await expect(page.locator("#large-view")).toBeHidden();
  });
});

test("#974: on the star map Previous sits at the frame's start edge, the mirror of Next", async ({ page }) => {
  await openNewMap(page);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await expect(section(page).getByRole("img")).toHaveCount(1, { timeout: 60_000 });
  await openLarge(page);
  // A planet has its settings column beside the frame; the star map has none, so the frame widens.
  await expect(page.locator("#large-settings")).toBeVisible();
  const planetNext = await box(page, "#large-next");
  await page.locator("#large-starmap").click();
  await expect(page.locator("#large-view")).toHaveAttribute("data-starmap", "");
  const frame = await box(page, ".large-view__stage");
  const prev = await box(page, "#large-prev");
  const next = await box(page, "#large-next");
  // Next keeps its place, so clicks in one spot keep stepping.
  expect(next).toEqual(planetNext);
  // Mirror images: the same gap to each edge, within 1 px, on one line.
  expect(prev.x).toBeGreaterThanOrEqual(frame.x);
  expect(Math.abs((prev.x - frame.x) - (frame.x + frame.width - (next.x + next.width)))).toBeLessThanOrEqual(1);
  expect(prev.y).toBe(next.y);
});

test("#977: the large view's settings put each resource's name and its three sliders on one row", async ({ page }) => {
  const id = await openNewMap(page);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await vulcanus(page, id);
  // The Map settings page's row of the same resource.
  const row = (root: string) => page.locator(`${root} fieldset.autoplace:not(.autoplace--all)`).filter({ has: page.locator(".step__range") }).first();
  const pageRow = row("#mg-resources-vulcanus");
  await pageRow.scrollIntoViewIfNeeded();
  const pageHeight = (await pageRow.boundingBox())!.height;
  await openLarge(page);
  const big = row("#large-settings");
  await expect(big).toBeVisible();
  const legend = await big.locator("> legend").boundingBox();
  const ranges = await big.locator(".step__range").evaluateAll((rs) => rs.map((r) => r.getBoundingClientRect().top));
  expect(ranges).toHaveLength(3);
  for (const top of ranges) expect(Math.abs(top - ranges[0])).toBeLessThanOrEqual(2);
  // The name is in the row's start column, beside the sliders, not over them.
  expect(legend!.y + legend!.height).toBeGreaterThan(ranges[0]);
  const height = (await big.boundingBox())!.height;
  expect(Math.abs(height - pageHeight) / pageHeight).toBeLessThanOrEqual(0.2);
  expect(await scrollsSideways(page)).toBe(false);
  // At 200% text the row may stack again, but nothing scrolls sideways.
  await page.evaluate(() => { document.documentElement.style.fontSize = "200%"; });
  expect(await page.locator("#large-settings").evaluate((s) => s.scrollWidth <= s.clientWidth)).toBe(true);
  expect(await scrollsSideways(page)).toBe(false);
});

// The server sizes the star map for its labels, so the page, the large view and the downloaded file
// all fit them, with or without script. A name of wide letters is wider than an average guess.
test("#966: every star map label fits inside its picture, on the page, in the large view and in the file", async ({ page, foreman }) => {
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id, "WM".repeat(50));
  const inside = (svg: string) => page.locator(svg).first().evaluate((s) => {
    const frame = s.getBoundingClientRect();
    return [...s.querySelectorAll("text")].filter((t) => {
      const b = t.getBoundingClientRect();
      return b.left < frame.left - 0.5 || b.right > frame.right + 0.5 || b.top < frame.top - 0.5 || b.bottom > frame.bottom + 0.5;
    }).map((t) => t.textContent);
  });
  await each(page, async (label) => {
    await page.goto(newMapPath(id));
    await page.locator("label:has(> #preview-view-all)").click();
    await expect(page.locator("#preview-view-all")).toBeChecked();
    const card = section(page).locator(".starmap-card svg").first();
    await expect(card).toBeVisible({ timeout: 60_000 });
    await expect(card.locator("text", { hasText: "WMWM" })).toHaveCount(1);
    expect(await inside("#preview .starmap-card svg"), `${label}: page`).toEqual([]);
    await section(page).locator(".starmap-card a.picture").click();
    await expect(page.locator("#large-view")).toHaveAttribute("data-starmap", "");
    await expect(page.locator("#large-frame svg.starmap")).toBeVisible();
    expect(await inside("#large-frame svg.starmap"), `${label}: large`).toEqual([]);
    await page.keyboard.press("Escape");
  });
  // The file, drawn on its own as a browser opens it.
  const href = await section(page).locator(".starmap-card a.download").first().getAttribute("href");
  const res = await page.request.get(href!);
  expect(res.ok()).toBe(true);
  await page.setContent(await res.text());
  expect(await inside("svg"), "file").toEqual([]);
});

test("#986: opening Fill from a map exchange string leaves its summary where it was", async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label, wide) => {
    await page.goto(newMapPath(id));
    const summary = page.locator("#exchange-fill > summary");
    await summary.scrollIntoViewIfNeeded();
    // On a phone the disclosure comes before Preset and Seed, which may move down as it opens.
    const parts = wide ? ["#exchange-fill > summary", "#generate-preset", "#seed-random"] : ["#exchange-fill > summary"];
    const at = await snap(page, parts);
    await summary.click();
    await expect(page.locator("#exchange-fill")).toHaveAttribute("open", "");
    expect(await snap(page, parts), `${label}: open`).toEqual(at);
    await summary.click();
    expect(await snap(page, parts), `${label}: closed`).toEqual(at);
  });
});

test("#987: every step ends in the one form foot, and step 4 never scrolls sideways on a phone", async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    for (const path of ["/generate", newMapPath(id)]) {
      await page.goto(path);
      await expect(page.locator("#generate-form")).toBeVisible(live);
      const foot = page.locator("#generate-form > .form-foot");
      await expect(foot, `${label} ${path}`).toHaveCount(1);
      expect(await foot.evaluate((f) => f === f.parentElement!.lastElementChild), `${label} ${path}: last`).toBe(true);
      await expect(foot.locator("> *").last()).toHaveClass(/button--primary/);
      await expect(page.locator(".gen-actions, .flow-foot")).toHaveCount(0);
      expect(await scrollsSideways(page), `${label} ${path}: ${(await pastEdge(page)).join(", ")}`).toBe(false);
    }
    // Changing a step, by slider or select, moves nothing sideways.
    await page.goto(newMapPath(id));
    await expect(page.locator("#generate-form")).toBeVisible(live);
    // Nauvis's settings open, whichever planet the form last had open; never toggled shut.
    const nauvis = page.locator("#mg-resources-nauvis");
    if (!(await nauvis.evaluate((d) => (d as HTMLDetailsElement).open))) await nauvis.locator("> summary").click();
    const head = ["main h1", "#flow-steps", ".pagehead__crumb"];
    const before = await snap(page, head);
    // A slider on a wide panel, the select it stands for on a narrow one (DESIGN 7.2): one shows.
    const step = page.locator("#mg-nauvis-iron-ore-size-range, #mg-nauvis-iron-ore-size").locator("visible=true");
    await expect(step).toHaveCount(1);
    await step.focus();
    await page.keyboard.press("ArrowDown");
    expect(await scrollsSideways(page), `${label}: after a slider`).toBe(false);
    await page.evaluate((y) => window.scrollTo(0, y as number), before.scrollY);
    expect(await snap(page, head), `${label}: header after a slider`).toEqual(before);
  }, [{ width: 1920, height: 1080 }, { width: 375, height: 812 }]);
});

// A new seed typed in without scrolling to its field, so only the redraw it starts may move anything.
const typeSeed = (page: Page, seed: string) => page.locator("#generate-seed").evaluate((f, v) => {
  (f as HTMLInputElement).value = v;
  f.dispatchEvent(new Event("input", { bubbles: true }));
  f.dispatchEvent(new Event("change", { bubbles: true }));
}, seed);

test("#975: Preview map redraws without moving its button, the preview's size field or the page", async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    await page.goto(newMapPath(id));
    await expect(section(page).getByRole("img")).toHaveCount(1, { timeout: 60_000 });
    await expect(page.locator("#preview-amounts")).toBeVisible();
    // In the window's middle, clear of the bars at its foot, so the click itself scrolls nothing.
    await page.locator("#preview-button").evaluate((b) => b.scrollIntoView({ block: "center" }));
    const parts = ["#preview-button", "#newmap-save", "#preview-size-field", "#preview-status", "label:has(> #preview-amounts)"];
    const at = await snap(page, parts);
    const draw = hold(page, "/generate/preview");
    await draw.start();
    // A pointer's click where the button is: Playwright's own click scrolls it clear of the foot bar's room first.
    const button = at["#preview-button"] as Box;
    await page.mouse.click(button.x + button.width / 2, button.y + button.height / 2);
    await expect.poll(draw.waiting).toBeGreaterThan(0);
    await expect(page.locator("#preview-status .working")).toBeVisible();
    expect(await snap(page, parts), `${label}: drawing`).toEqual(at);
    await draw.release();
    await expect(page.locator("#preview-status .working")).toBeHidden(live);
    await expect(page.locator("#preview")).not.toHaveAttribute("aria-busy", "true");
    await expect.poll(() => section(page).locator("a.picture .map img").first().evaluate((i) => (i as HTMLImageElement).naturalWidth)).toBeGreaterThan(0);
    // Preview map chooses All planets, which has no amounts box (DESIGN 8.8); what sits above it stays.
    const drawn = parts.filter((p) => !p.includes("#preview-amounts"));
    expect(await snap(page, drawn), `${label}: drawn`).toEqual(Object.fromEntries(Object.entries(at).filter(([k]) => !k.includes("#preview-amounts"))));
  });
});

test("#975: Show resource amounts moves neither Draw from a real game nor the picture's Download link", async ({ page }) => {
  const id = await openNewMap(page);
  await each(page, async (label) => {
    await page.goto(newMapPath(id));
    await expect(section(page).getByRole("img")).toHaveCount(1, { timeout: 60_000 });
    const draw = page.locator("#preview-chart-button");
    await expect(draw).toBeVisible();
    await draw.scrollIntoViewIfNeeded();
    const link = "#preview .picture-actions > a.download";
    // The link's words grow with the amounts, so its start stays and its width may not.
    const read = async () => {
      const out = await snap(page, ["#preview-chart-button", "#preview-amounts", link]);
      out[link] = { ...(out[link] as Box), width: 0 };
      return out;
    };
    const at = await read();
    for (const on of [true, false, true, false]) {
      // Ticked where it is, without scrolling it into view: only the change itself may move anything.
      await page.locator("#preview-amounts").evaluate((b) => (b as HTMLInputElement).click());
      await expect(page.locator("#preview-amounts")).toBeChecked({ checked: on });
      await amountsDone(page, "#preview", on);
      expect(await read(), `${label}: amounts ${on ? "on" : "off"}`).toEqual(at);
    }
  });
});

test("#975: under the preview the redraw's words wrap whole at 320 px, and showing them moves nothing", async ({ page }) => {
  const id = await openNewMap(page);
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto(newMapPath(id));
  await expect(section(page).getByRole("img")).toHaveCount(1, { timeout: 60_000 });
  const text = page.locator("#preview-status .working__text");
  await page.locator("#preview-status").scrollIntoViewIfNeeded();
  const parts = ["#preview-status", "#preview-size-field"];
  const at = await snap(page, parts);
  const draw = hold(page, "/generate/preview");
  await draw.start();
  await typeSeed(page, "4242");
  await expect(page.locator("#preview-status .working")).toBeVisible();
  // Whole: nothing is cut off with an ellipsis.
  expect(await text.evaluate((t) => t.scrollWidth <= t.clientWidth && getComputedStyle(t).textOverflow !== "ellipsis")).toBe(true);
  await expect(text).toHaveText("Drawing the map preview for the current settings…");
  expect(await snap(page, parts), "drawing").toEqual(at);
  await draw.release();
  await expect(page.locator("#preview-status .working")).toBeHidden(live);
  // At 200% text it wraps rather than cuts, and still moves nothing as it shows.
  await page.evaluate(() => { document.documentElement.style.fontSize = "200%"; });
  await page.locator("#preview-status").scrollIntoViewIfNeeded();
  const big = await snap(page, parts);
  const again = hold(page, "/generate/preview");
  await again.start();
  await typeSeed(page, "4343");
  await expect(page.locator("#preview-status .working")).toBeVisible();
  expect(await text.evaluate((t) => t.scrollWidth <= t.clientWidth)).toBe(true);
  expect(await snap(page, parts), "drawing at 200%").toEqual(big);
  await again.release();
});
