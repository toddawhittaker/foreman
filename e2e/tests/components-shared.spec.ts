import { test, expect, createAdmin, seedWorld, seedSecond, image21, type FakeGame, type Foreman } from "./foreman.js";
import { chromium, firefox, webkit, type Browser, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The shared components (DESIGN 7) have one look and one size on every page
// that uses them: one height per button role, the title band on every plate,
// one gap between stacked fields, one foot bar, table rows padded inside
// their highlight, empty states centred in their plate, and one working bar
// drawn the same in every browser. Layout checks are exact, in x and y.

// The webhook form needs the secret key that seals its address (SPEC 8.10).
test.use({ fakeGameOn: true, foremanEnv: { FOREMAN_SECRET_KEY: "componentscomponentscomponents00" } });

// The working bar's markup, as the working partial renders it (components_test.go checks the same file).
const workingBar = readFileSync(join(import.meta.dirname, "..", "testdata", "working-bar.html"), "utf8");

// The raised face and the strong hairline, read from the page's own tokens.
async function tokens(page: Page) {
  return page.evaluate(() => {
    const probe = document.createElement("div");
    document.body.append(probe);
    const read = (prop: string, value: string) => {
      probe.style.setProperty(prop, value);
      return getComputedStyle(probe).getPropertyValue(prop);
    };
    const out = { raised: read("background-color", "var(--color-raised)"), lineStrong: read("border-top-color", "var(--color-line-strong)") };
    probe.remove();
    return out;
  });
}

async function seed(page: Page, foreman: Foreman, fakeGame: FakeGame | undefined) {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
}

test("a button has one height per role: 44 px on a page, 36 px in a row", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  const heights: Record<string, Set<number>> = { page: new Set(), row: new Set() };
  for (const path of ["/", "/worlds", "/worlds/base/saves", "/settings/users", "/settings/api-tokens", "/settings/general", "/import"]) {
    await page.goto(path);
    const found = await page.evaluate(() =>
      [...document.querySelectorAll("main .button")]
        .filter((b) => b.getBoundingClientRect().height > 0 && getComputedStyle(b).visibility !== "hidden")
        .map((b) => ({
          row: !!b.closest("td, .row-actions, .worlds > li, .favorite-list > li, .token-card, .token-list, .webhook-list"),
          height: b.getBoundingClientRect().height,
        })),
    );
    expect(found.length, path).toBeGreaterThan(0);
    for (const b of found) heights[b.row ? "row" : "page"].add(b.height);
  }
  expect([...heights.page]).toEqual([44]);
  expect([...heights.row]).toEqual([36]);
});

test("every plate's title is the band", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  const { raised, lineStrong } = await tokens(page);
  let checked = 0;
  for (const path of ["/", "/worlds/base/saves", "/settings/users", "/import", "/favorites"]) {
    await page.goto(path);
    const titles = await page.evaluate(() => {
      const shown = (e: Element) => !e.matches('input[type="hidden"], [hidden]');
      return [...document.querySelectorAll("main .panel, main .card")]
        .filter((p) => getComputedStyle(p).borderTopStyle === "solid" && p.getBoundingClientRect().height > 0)
        .map((p) => [...p.children].find(shown))
        .filter((t): t is Element => !!t && t.matches("h2:not(.visually-hidden), .section-head, .group__head"))
        .map((t) => {
          const cs = getComputedStyle(t);
          const plate = t.parentElement!.getBoundingClientRect();
          const box = t.getBoundingClientRect();
          return { text: t.textContent!.trim().slice(0, 30), bg: cs.backgroundColor, rule: `${cs.borderBottomWidth} ${cs.borderBottomStyle} ${cs.borderBottomColor}`, dx: box.x - plate.x - 1, dy: box.y - plate.y - 1, dw: plate.width - 2 - box.width };
        });
    });
    for (const t of titles) {
      expect(t, `${path} ${t.text}`).toEqual({ ...t, bg: raised, rule: `1px solid ${lineStrong}`, dx: 0, dy: 0, dw: 0 });
      checked++;
    }
  }
  expect(checked).toBeGreaterThan(4);
});

test("stacked fields have one gap", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  const gaps: string[] = [];
  for (const path of ["/settings/users", "/worlds/base/settings"]) {
    await page.goto(path);
    const found = await page.evaluate(() => {
      const out: string[] = [];
      for (const f of document.querySelectorAll("main .field")) {
        const next = f.nextElementSibling;
        if (!next?.matches(".field") || f.getBoundingClientRect().height === 0 || next.getBoundingClientRect().height === 0) continue;
        const a = f.getBoundingClientRect();
        const b = next.getBoundingClientRect();
        if (b.x === a.x) out.push(`${f.id || f.querySelector("[id]")?.id} ${b.y - a.bottom}`);
      }
      return out;
    });
    expect(found.length, path).toBeGreaterThan(0);
    for (const g of found) gaps.push(`${path} ${g}`);
  }
  expect(gaps.filter((g) => !g.endsWith(" 16"))).toEqual([]);
});

test("every form ends in one foot bar: raised, last, Cancel then the primary at the end", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  const { raised } = await tokens(page);
  for (const path of ["/settings/general", "/settings/restart-schedule", "/settings/notifications/new", "/worlds/base/settings", "/import"]) {
    await page.goto(path);
    const foots = await page.evaluate(() =>
      [...document.querySelectorAll("main .form-foot")]
        .filter((f) => f.getBoundingClientRect().height > 0)
        .map((f) => {
          const kids = [...f.children].filter((k) => k.getBoundingClientRect().width > 0);
          const primary = f.querySelector(".button--primary");
          const cancel = f.querySelector(":scope > a");
          const end = f.getBoundingClientRect().right - parseFloat(getComputedStyle(f).paddingRight);
          return {
            bg: getComputedStyle(f).backgroundColor,
            last: f.parentElement!.lastElementChild === f,
            primaryLast: kids[kids.length - 1] === primary,
            primaryAtEnd: primary ? primary.getBoundingClientRect().right === end : false,
            cancelBefore: cancel ? cancel.nextElementSibling === primary : true,
          };
        }),
    );
    expect(foots.length, path).toBe(1);
    expect(foots[0], path).toEqual({ bg: raised, last: true, primaryLast: true, primaryAtEnd: true, cancelBefore: true });
  }
});

test("a highlighted row keeps its content inside the highlight, and the table lines up with its title", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  for (const path of ["/worlds/base/saves", "/settings/audit-log", "/worlds/base"]) {
    await page.goto(path);
    const row = page.locator("main table tbody tr").first();
    await row.hover();
    const m = await row.evaluate((tr) => {
      const cells = [...tr.children].filter((c) => c.getBoundingClientRect().width > 0);
      const box = tr.getBoundingClientRect();
      const inner = (c: Element) => {
        const r = c.getBoundingClientRect();
        const cs = getComputedStyle(c);
        return { left: r.left + parseFloat(cs.paddingLeft), right: r.right - parseFloat(cs.paddingRight) };
      };
      const plate = tr.closest(".panel, .card")!;
      const head = [...plate.children].find((c) => !c.matches('input[type="hidden"], [hidden]'))!;
      const titleText = document.createRange();
      titleText.selectNodeContents(head.matches("h2") ? head : head.querySelector("h2, h3")!);
      return {
        bg: getComputedStyle(tr).backgroundColor,
        startPad: inner(cells[0]).left - box.left,
        endPad: box.right - inner(cells[cells.length - 1]).right,
        textToTitle: inner(cells[0]).left - titleText.getBoundingClientRect().left,
      };
    });
    const { raised } = await tokens(page);
    expect(m, path).toEqual({ bg: raised, startPad: 12, endPad: 12, textToTitle: 0 });
  }
});

test("an empty state is centred across its plate", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  for (const path of ["/deleted", "/chat"]) {
    await page.goto(path);
    const m = await page.locator("main .empty--art").first().evaluate((e) => {
      const box = e.getBoundingClientRect();
      const parent = e.parentElement!;
      const cs = getComputedStyle(parent);
      const p = parent.getBoundingClientRect();
      const left = p.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft);
      const right = p.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight);
      const art = getComputedStyle(e, "::before");
      return { left: box.left - left, right: right - box.right, art: `${art.width} ${art.height}` };
    });
    expect(m, path).toEqual({ left: 0, right: 0, art: "160px 96px" });
  }
});

// The working bar's box and motion, measured in one browser on the Server page.
async function measureWorkingBar(page: Page) {
  return page.evaluate((markup) => {
    const plate = document.querySelector("main .panel")!;
    const host = document.createElement("div");
    host.id = "working-host";
    host.innerHTML = markup;
    plate.prepend(host);
    const after = document.createElement("button");
    after.className = "button";
    after.textContent = "Next";
    host.after(after);
    const bar = host.querySelector(".working__bar")!;
    const seg = getComputedStyle(bar, "::before");
    const shown = seg.visibility;
    const words = host.querySelector(".working__text")!.getBoundingClientRect();
    const b = bar.getBoundingClientRect();
    const before = after.getBoundingClientRect();
    host.firstElementChild!.classList.add("working--idle");
    const idle = after.getBoundingClientRect();
    return {
      bar: { width: b.width, height: b.height },
      // Browsers lay out in different fractions of a pixel, so the segment's 30% is read to a tenth.
      segment: { width: Math.round(parseFloat(seg.width) * 10) / 10, height: seg.height, background: seg.backgroundColor, shown },
      motion: `${seg.animationName} ${seg.animationDuration} ${seg.animationIterationCount} ${seg.animationDirection}`,
      wordsBeside: Math.round(words.top + words.height / 2) === Math.round(b.top + b.height / 2) && words.left > b.right,
      ariaHidden: bar.getAttribute("aria-hidden"),
      idleKeepsRoom: idle.x === before.x && idle.y === before.y,
      idleHidden: getComputedStyle(host.firstElementChild!).visibility,
    };
  }, workingBar);
}

test("the working bar is the same box and motion in Chromium, Firefox and WebKit", async ({ page, foreman, fakeGame, browserName }) => {
  test.skip(browserName !== "chromium", "this test drives the other two browsers itself");
  await seed(page, foreman, fakeGame);
  const state = await page.context().storageState();
  const results: Record<string, unknown> = {};
  for (const [name, type] of [["chromium", chromium], ["firefox", firefox], ["webkit", webkit]] as const) {
    let browser: Browser | undefined;
    try {
      browser = await type.launch();
      const context = await browser.newContext({ storageState: state, colorScheme: "dark", viewport: { width: 1280, height: 720 }, locale: "en-US" });
      const p = await context.newPage();
      await p.goto(foreman.url + "/");
      results[name] = await measureWorkingBar(p);
    } finally {
      await browser?.close();
    }
  }
  const want = {
    bar: { width: 320, height: 8 },
    segment: { width: 95.4, height: "6px", background: "rgb(243, 154, 42)", shown: "visible" },
    motion: "working-slide 1.2s infinite alternate",
    wordsBeside: true,
    ariaHidden: "true",
    idleKeepsRoom: true,
    idleHidden: "hidden",
  };
  expect(results).toEqual({ chromium: want, firefox: want, webkit: want });
});

test("under reduced motion the working bar stands still and stays in view", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  const m = await measureWorkingBar(page);
  expect(m.motion).toMatch(/^none /);
  expect(m.bar).toEqual({ width: 320, height: 8 });
  expect(m.segment.shown).toBe("visible");
  // It stands in the middle of its track.
  const middle = await page.locator("#working-host .working__bar").evaluate((b) => getComputedStyle(b, "::before").transform);
  expect(middle).toMatch(/^matrix\(1, 0, 0, 1, 111\.\d+, 0\)$/);
});
