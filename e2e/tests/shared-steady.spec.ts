import { test, expect, createAdmin, seedWorld, image21, type FakeGame, type Foreman } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { statSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Shared components keep one look and one place (DESIGN 6, "Controls stay
// put"; DESIGN 7): a tab keeps its box on every sibling page and when a
// sub-page gains its unsaved dot, the first part under a page header is the
// same distance from it on every page, every disclosure draws one chevron,
// every column head has one padding and line, and a wrapped checkbox label
// starts where a one-line one does. Every layout check is 0 px.

test.use({ fakeGameOn: true });

type Box = { x: number; y: number; width: number; height: number };

// An element's box on the page, not the window, so a scroll does not count.
const pageBox = (l: Locator) =>
  l.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const n = (v: number) => Math.round(v * 100) / 100;
    return { x: n(r.x + scrollX), y: n(r.y + scrollY), width: n(r.width), height: n(r.height) };
  });

// Each top-level tab of the row named label, by its words, with its box.
async function tabBoxes(page: Page, label: string) {
  const links = page.getByRole("navigation", { name: label }).locator(":scope > ul > li > a");
  const out: Record<string, Box> = {};
  for (const a of await links.all()) out[(await a.innerText()).trim()] = await pageBox(a);
  return out;
}

// Visits every page of the row named label, starting from start, and
// expects every tab to keep its box on each.
async function expectTabsStill(page: Page, start: string, label: string) {
  await page.goto(start);
  const hrefs = await page
    .getByRole("navigation", { name: label })
    .locator(":scope > ul > li > a")
    .evaluateAll((as) => as.map((a) => (a as HTMLAnchorElement).getAttribute("href")!));
  expect(hrefs.length).toBeGreaterThan(1);
  const first = await tabBoxes(page, label);
  for (const href of hrefs) {
    await page.goto(href);
    expect(await tabBoxes(page, label), href).toEqual(first);
  }
}

function writePlayers(f: Foreman) {
  const dir = join(f.dataDir, "worlds", "base");
  const st = statSync(join(dir, "saves", "base.zip"), { bigint: true });
  const ms = Number(st.mtimeNs / 1_000_000n);
  const frac = String(st.mtimeNs % 1_000_000_000n).padStart(9, "0").replace(/0+$/, "");
  const modified = new Date(ms).toISOString().replace(/\.\d+Z$/, frac ? `.${frac}Z` : "Z");
  const now = new Date().toISOString();
  writeFileSync(
    join(dir, "players.json"),
    JSON.stringify({
      format_version: 1,
      basis: { save: "base.zip", size: Number(st.size), modified },
      as_of: now,
      seed: { at: now, save: "base.zip", game_version: "2.1.24", result: "ok" },
      tracked_since: now,
      inexact: false,
      game_ticks: 3600 * 60,
      players: [{ name: "carol", seeded_ticks: 3600 * 60, tracked_ms: 0, last_seen: "2026-11-21T12:00:00Z" }],
    }),
  );
}

async function setUp(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  await g.call("/image", { ref: image21, on: true });
}

test("every tab keeps its box on each of its sibling pages", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await expectTabsStill(page, "/settings/general", "Foreman settings");
  await expectTabsStill(page, "/worlds", "Worlds pages");
  await expectTabsStill(page, "/worlds/base", "World pages");
  await expectTabsStill(page, "/", "Server pages");
});

test("the first change on Game settings moves no sub-page link", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/worlds/base/settings");
  const subs = page.locator(".subnav__sub a");
  const boxes = async () => Promise.all((await subs.all()).map(pageBox));
  const before = await boxes();
  expect(before.length).toBeGreaterThan(1);
  await page.getByLabel("Description").fill("Bring snacks");
  await expect(page.locator(".subnav__sub a[data-unsaved]")).toHaveCount(1);
  expect(await boxes()).toEqual(before);
});

// The distance from the page header's foot to the first part drawn under it.
async function headGap(page: Page, path: string) {
  await page.goto(path);
  return page.locator("main > .pagehead").evaluate((head) => {
    let el = head.nextElementSibling;
    while (el) {
      const s = getComputedStyle(el);
      const r = el.getBoundingClientRect();
      if (s.display !== "none" && s.position !== "absolute" && r.height > 0) break;
      el = el.nextElementSibling;
    }
    return Math.round((el!.getBoundingClientRect().top - head.getBoundingClientRect().bottom) * 100) / 100;
  });
}

test("the first part under a page header is 16 px under it on every page", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  for (const path of ["/settings/general", "/worlds", "/worlds/base", "/worlds/base/mods", "/worlds/base/map", "/worlds/base/saves", "/about", "/design", "/no-such-page"]) {
    expect(await headGap(page, path), path).toBe(16);
  }
});

// What draws each disclosure's arrow.
async function arrows(page: Page) {
  return page.locator("details > summary").evaluateAll((sums) =>
    sums
      .filter((s) => s.parentElement!.matches(".disclosure, .menu, .menu__langs, .plan-panel__list"))
      .map((s) => {
        const a = getComputedStyle(s, "::after");
        return {
          what: `${s.parentElement!.className}: ${s.textContent!.trim().slice(0, 30)}`,
          marker: getComputedStyle(s).display === "list-item" && getComputedStyle(s).listStyleType !== "none",
          chevron: `${a.content} ${a.maskImage.slice(0, 60)} ${a.width} ${a.height}`,
        };
      }),
  );
}

test("every disclosure draws the one chevron and no browser marker", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  const all = [];
  for (const path of ["/design", "/"]) {
    await page.goto(path);
    all.push(...(await arrows(page)));
  }
  expect(all.length).toBeGreaterThan(3);
  const chevron = all.find((a) => a.what.includes("emergency"))!.chevron;
  expect(chevron).toContain("url(");
  expect(all.filter((a) => a.marker || a.chevron !== chevron)).toEqual([]);
});

// The padding and line of each column head.
async function headLooks(page: Page, selector: string) {
  return page.locator(selector).first().evaluate((h) => {
    const s = getComputedStyle(h);
    return { padding: `${s.paddingTop} ${s.paddingBottom}`, line: s.lineHeight };
  });
}

test("every column head has one padding and one line", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  writePlayers(foreman);
  await page.goto("/worlds/base/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.locator("main tbody tr")).toHaveCount(1);

  await page.goto("/");
  await expect(page.locator(".server-players thead th").first()).toBeVisible();
  const players = await headLooks(page, ".server-players thead th");
  await page.goto("/snapshots");
  const snapshots = await headLooks(page, "main thead th");
  await page.goto("/settings/audit-log");
  const audit = await headLooks(page, ".audit-table thead th");
  expect({ snapshots, audit }).toEqual({ snapshots: players, audit: players });

  for (const path of ["/settings/api-tokens", "/settings/users"]) {
    await page.goto(path);
    const head = page.locator(".token-head > span").first();
    if (await head.isVisible()) expect((await headLooks(page, ".token-head > span")).line, path).toBe(players.line);
  }
});

test("a checkbox label that wraps starts its words where a one-line label does", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/settings/general");
  // The box and the words' first line, each from the label's top.
  const place = (id: string) =>
    page.locator(`label.check:has(#${id})`).evaluate((label) => {
      const top = label.getBoundingClientRect().top;
      const box = label.querySelector("input")!.getBoundingClientRect();
      const lines: DOMRect[] = [];
      for (const node of label.childNodes) {
        if (node.nodeType !== Node.TEXT_NODE || !node.textContent!.trim()) continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        lines.push(...[...range.getClientRects()].filter((r) => r.width > 0));
      }
      const first = Math.min(...lines.map((r) => r.top));
      return { lines: new Set(lines.map((r) => Math.round(r.top))).size, box: Math.round((box.top - top) * 100) / 100, words: Math.round((first - top) * 100) / 100 };
    });
  const one = await place("general-update-check");
  const two = await place("general-wiki-icons");
  expect(one.lines).toBe(1);
  expect(two.lines).toBeGreaterThan(1);
  expect({ box: two.box, words: two.words }).toEqual({ box: one.box, words: one.words });
});
