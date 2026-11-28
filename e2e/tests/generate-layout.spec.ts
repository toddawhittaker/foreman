import { test, expect, createAdmin, seedWorld, image21, live } from "./foreman.js";
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

// The boxes of every selector, and the page's scroll, at one moment.
async function snap(page: Page, selectors: string[]) {
  const out: Record<string, Box | number> = { scrollY: await page.evaluate(() => window.scrollY) };
  for (const s of selectors) out[s] = await box(page, s);
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

function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: ["base", "elevated-rails", "quality", "space-age"].map((name) => ({ name, enabled: true })) }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
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

test("#984: steps 1, 3 and 4 put the crumb's row, the title and the step bar in one place", async ({ page, foreman }) => {
  const id = await openNewMap(page, { name: "Steps" });
  addFakePlanet(foreman.dataDir, id);
  const paths = ["/generate", `${newMapPath(id)}/mod-settings`, newMapPath(id)];
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
    expect(seen[1], `${label}: step 3 against step 1`).toEqual(seen[0]);
    expect(seen[2], `${label}: step 4 against step 1`).toEqual(seen[0]);
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

test("#975: in the panel, each box has its working bar under it, and ticking one moves neither", async ({ page }) => {
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
    await expect(page.locator("#preview-amounts-work .working")).toBeHidden(live);
    expect(await snap(page, parts), `${label}: counted`).toEqual(at);
    // Territories, held: the same bar under its box, not the old line of words.
    const terr = hold(page, "/generate/preview/territories");
    await terr.start();
    await page.locator("#preview-territories").check();
    await expect(page.locator("#preview-territories-work .working")).toBeVisible();
    await expect(page.locator("#preview-territories-work .working__bar")).toBeVisible();
    await expect(page.locator("#territory-result")).not.toContainText("Finding the demolisher territories");
    expect(await snap(page, parts), `${label}: finding`).toEqual(at);
    await terr.release();
    await expect(page.locator("#preview-territories-work .working")).toBeHidden(live);
    // The amounts are counted again for the new picture; once both are done nothing has moved.
    await expect(page.locator("#preview-amounts-work .working")).toBeHidden(live);
    expect(await snap(page, parts), `${label}: found`).toEqual(at);
    // Five times off and on with the amounts already counted.
    for (let i = 0; i < 5; i++) {
      await page.locator("#preview-amounts").setChecked(i % 2 === 1);
      expect(await snap(page, parts), `${label}: toggle ${i}`).toEqual(at);
    }
  });
});

test("#975: in the large view of Vulcanus, ticking either box never moves the other", async ({ page }) => {
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
    await expect(page.locator("#large-amounts-work .working")).toBeHidden(live);
    expect(await snap(page, parts), `${label}: counted`).toEqual(at);
    const terr = hold(page, "/generate/preview/territories");
    await terr.start();
    await page.locator("#large-territories").check();
    await expect(page.locator("#large-territories-work .working")).toBeVisible();
    expect(await snap(page, parts), `${label}: finding`).toEqual(at);
    await terr.release();
    await expect(page.locator("#large-territories-work .working")).toBeHidden(live);
    await expect(page.locator("#large-amounts-work .working")).toBeHidden(live);
    expect(await snap(page, parts), `${label}: found`).toEqual(at);
    for (let i = 0; i < 5; i++) {
      await page.locator("#large-amounts").setChecked(i % 2 === 1);
      expect(await snap(page, parts), `${label}: toggle ${i}`).toEqual(at);
    }
    await page.keyboard.press("Escape");
    await expect(page.locator("#large-view")).toBeHidden();
    // Leave both off for the next round.
    await page.locator("#preview-territories").uncheck();
  });
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
  await page.locator("#large-starmap").click();
  await expect(page.locator("#large-view")).toHaveAttribute("data-starmap", "");
  const frame = await box(page, ".large-view__stage");
  const prev = await box(page, "#large-prev");
  const next = await box(page, "#large-next");
  // Within the frame's start padding, no further in than Next is from the end.
  expect(prev.x).toBeGreaterThanOrEqual(frame.x);
  expect(prev.x - frame.x).toBeLessThanOrEqual(frame.x + frame.width - (next.x + next.width));
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

test("#966: every star map label fits inside its picture, on the page and in the large view", async ({ page, foreman }) => {
  const id = await openNewMap(page, { name: "Modded" });
  addFakePlanet(foreman.dataDir, id);
  await each(page, async (label) => {
    await page.goto(newMapPath(id));
    await page.locator("label:has(> #preview-view-all)").click();
    await expect(page.locator("#preview-view-all")).toBeChecked();
    const card = section(page).locator(".starmap-card svg").first();
    await expect(card).toBeVisible({ timeout: 60_000 });
    const inside = (svg: string) => page.locator(svg).first().evaluate((s) => {
      const frame = s.getBoundingClientRect();
      return [...s.querySelectorAll("text")].filter((t) => {
        const b = t.getBoundingClientRect();
        return b.left < frame.left - 0.5 || b.right > frame.right + 0.5 || b.top < frame.top - 0.5 || b.bottom > frame.bottom + 0.5;
      }).map((t) => t.textContent);
    });
    expect(await inside("#preview .starmap-card svg"), `${label}: page`).toEqual([]);
    await section(page).locator(".starmap-card a.picture").click();
    await expect(page.locator("#large-view")).toHaveAttribute("data-starmap", "");
    await expect(page.locator("#large-frame svg.starmap")).toBeVisible();
    expect(await inside("#large-frame svg.starmap"), `${label}: large`).toEqual([]);
    await page.keyboard.press("Escape");
  });
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
    // Using a slider on a phone moves nothing sideways.
    await page.goto(newMapPath(id));
    await expect(page.locator("#generate-form")).toBeVisible(live);
    await page.locator("#mg-resources-nauvis summary").click();
    const head = ["main h1", "#flow-steps", ".pagehead__crumb"];
    const before = await snap(page, head);
    const range = page.locator("#mg-resources-nauvis .step__range").first();
    if (await range.isVisible()) {
      await range.focus();
      await page.keyboard.press("ArrowRight");
      expect(await scrollsSideways(page), `${label}: after a slider`).toBe(false);
      await page.evaluate((y) => window.scrollTo(0, y as number), before.scrollY);
      expect(await snap(page, head), `${label}: header after a slider`).toEqual(before);
    }
  }, [{ width: 1920, height: 1080 }, { width: 375, height: 812 }]);
});
