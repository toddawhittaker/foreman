import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, live, stateBadge, type FakeGame, type Foreman } from "./foreman.js";
import { chromium, firefox, webkit, type Browser, type Page } from "@playwright/test";
import { copyFileSync, readFileSync, utimesSync } from "node:fs";
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

// The empty partial's markup, as components_test.go renders it.
const emptyState = readFileSync(join(import.meta.dirname, "..", "testdata", "empty-state.html"), "utf8");

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

// The active world "base" with a second, older save (so its saves list has a
// Load button and two rows), the world "Second", and the game image.
async function seed(page: Page, foreman: Foreman, fakeGame: FakeGame | undefined, opts: Parameters<typeof seedWorld>[1] = {}) {
  await createAdmin(page);
  seedWorld(foreman, opts);
  const saves = join(foreman.dataDir, "worlds", "base", "saves");
  const older = new Date(Date.now() - 86_400_000);
  copyFileSync(join(saves, "base.zip"), join(saves, "older.zip"));
  utimesSync(join(saves, "older.zip"), older, older);
  seedSecond(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
}

// The pages whose buttons are measured, each with the buttons that sit in a
// row of a list or table, named by their own ids or selectors.
const buttonPages: [string, string[]][] = [
  ["/", []],
  ["/worlds", ['main a.button[aria-describedby^="world-"]', 'main a.button[href$="/switch"]']],
  ["/worlds/base/saves", ['main button[name="load"]']],
  ["/settings/users", []],
  ["/settings/api-tokens", []],
  ["/settings/general", []],
  ["/import", []],
];

async function buttonHeights(page: Page) {
  const heights: Record<string, Set<number>> = { page: new Set(), row: new Set() };
  let rows = 0;
  for (const [path, rowButtons] of buttonPages) {
    await page.goto(path);
    const found = await page.evaluate((rowSel) => {
      const row = new Set(rowSel.flatMap((s) => [...document.querySelectorAll(s)]));
      return [...document.querySelectorAll("main .button")]
        .filter((b) => b.getBoundingClientRect().height > 0 && getComputedStyle(b).visibility !== "hidden")
        .map((b) => ({ row: row.has(b), height: b.getBoundingClientRect().height }));
    }, rowButtons);
    expect(found.length, path).toBeGreaterThan(0);
    expect(found.filter((b) => b.row).length > 0, `${path} has its row buttons`).toBe(rowButtons.length > 0);
    for (const b of found) heights[b.row ? "row" : "page"].add(b.height);
    rows += found.filter((b) => b.row).length;
  }
  expect(rows).toBeGreaterThan(1);
  return { page: [...heights.page], row: [...heights.row] };
}

test("a button has one height per role: 44 px on a page, 36 px in a row", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  expect(await buttonHeights(page)).toEqual({ page: [44], row: [36] });
});

test.describe("on a touch screen", () => {
  test.use({ hasTouch: true, isMobile: true, viewport: { width: 390, height: 844 } });

  test("every button is 44 px, in a row as on a page", async ({ page, foreman, fakeGame }) => {
    await seed(page, foreman, fakeGame);
    expect(await buttonHeights(page)).toEqual({ page: [44], row: [44] });
  });
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

// Each row of the table: its padding inside the highlight at both ends, the
// first cell's text against the plate's title, and in y the cells filling
// the row's box with even space above and below. A cell that lends its box to
// the row (display: contents) is measured by its children.
async function measureRows(page: Page, table: string) {
  const rows = page.locator(`${table} tbody tr`);
  await expect(rows.first()).toBeVisible(live);
  const count = await rows.count();
  expect(count, table).toBeGreaterThan(0);
  const out = [];
  for (let i = 0; i < count; i++) {
    const row = rows.nth(i);
    await row.hover();
    out.push(
      await row.evaluate((tr) => {
        const boxes = (c: Element): Element[] => (getComputedStyle(c).display === "contents" ? [...c.children].flatMap(boxes) : [c]);
        const cells = [...tr.children].flatMap(boxes).filter((c) => c.getBoundingClientRect().width > 0);
        const box = tr.getBoundingClientRect();
        const trs = getComputedStyle(tr);
        const inner = (c: Element) => {
          const r = c.getBoundingClientRect();
          const cs = getComputedStyle(c);
          return { left: r.left + parseFloat(cs.paddingLeft), right: r.right - parseFloat(cs.paddingRight) };
        };
        // The plate's title, or where its text would start when the title is
        // only for screen readers: the plate's content edge.
        const plate = tr.closest(".panel, .card")!;
        const head = [...plate.children].find((c) => !c.matches('input[type="hidden"], [hidden]'))!;
        const heading = head.matches("h2") ? head : head.querySelector("h2, h3")!;
        const titleText = document.createRange();
        titleText.selectNodeContents(heading);
        const ps = getComputedStyle(plate);
        const titleLeft = heading.matches(".visually-hidden")
          ? plate.getBoundingClientRect().left + parseFloat(ps.borderLeftWidth) + parseFloat(ps.paddingLeft)
          : titleText.getBoundingClientRect().left;
        // The first column's name, when the column names are shown.
        const thead = tr.closest("table")!.tHead!;
        const th = thead.querySelector("th")!;
        const thShown = getComputedStyle(thead).position !== "absolute" && th.getBoundingClientRect().width > 1;
        const top = Math.min(...cells.map((c) => c.getBoundingClientRect().top));
        const bottom = Math.max(...cells.map((c) => c.getBoundingClientRect().bottom));
        const probe = document.createElement("span");
        probe.style.paddingLeft = "var(--cell-pad)";
        tr.lastElementChild!.append(probe);
        const pad = parseFloat(getComputedStyle(probe).paddingLeft);
        probe.remove();
        // Layout is in 1/64 px units; a plate's padding in rem leaves float noise below that.
        const px = (n: number) => Math.round(n * 64) / 64 + 0;
        return {
          bg: getComputedStyle(tr).backgroundColor,
          startPad: px(Math.min(...cells.map((c) => inner(c).left)) - box.left - pad),
          endPad: px(box.right - Math.max(...cells.map((c) => inner(c).right)) - pad),
          textToTitle: px(inner(cells[0]).left - titleLeft),
          textToHeader: thShown ? px(inner(cells[0]).left - inner(th).left) : 0,
          cellsFillRow:
            top - box.top - parseFloat(trs.borderTopWidth) - parseFloat(trs.paddingTop) === 0 &&
            box.bottom - bottom - parseFloat(trs.borderBottomWidth) - parseFloat(trs.paddingBottom) === 0,
          evenAboveBelow: trs.paddingTop === trs.paddingBottom,
          padSet: pad > 0,
        };
      }),
    );
  }
  return out;
}

const rowPages: [string, string][] = [
  ["/worlds/base/saves", "main table"],
  ["/settings/audit-log", "main table"],
  ["/worlds/base", "main #save-players-table"],
  ["/worlds/base/mods", "main .mod-list"],
];

for (const width of [1920, 390]) {
  // Every page lays its rows out with the shared even padding at both widths.
  const phoneOwnLayout: string[] = [];
  const pages = width === 1920 ? rowPages : rowPages.filter(([path]) => !phoneOwnLayout.includes(path));
  test.describe(`at ${width} px`, () => {
    test.use({ viewport: { width, height: 1000 } });

    test("every highlighted row keeps its content inside the highlight, and the table lines up with its title", async ({ page, foreman, fakeGame }) => {
      await seed(page, foreman, fakeGame, { mods: { "fp-one": "2.1", "fp-two": "2.1", "fp-three": "2.1" } });
      let rows = 0;
      for (const [path, table] of pages) {
        await page.goto(path);
        const { raised } = await tokens(page);
        const want = { bg: raised, startPad: 0, endPad: 0, textToTitle: 0, textToHeader: 0, cellsFillRow: true, evenAboveBelow: true, padSet: true };
        const measured = await measureRows(page, table);
        rows += measured.length;
        for (const [i, m] of measured.entries()) expect(m, `${path} row ${i + 1}`).toEqual(want);
      }
      expect(rows).toBeGreaterThan(pages.length);
    });
  });
}

test.describe("below 360 px, where plates lie flat", () => {
  test.use({ viewport: { width: 340, height: 800 } });

  test("a table's text lines up with the plate's title", async ({ page, foreman, fakeGame }) => {
    await seed(page, foreman, fakeGame);
    await page.goto("/worlds/base/saves");
    const m = await page.locator("main table").first().evaluate((table) => {
      const plate = table.closest(".panel, .card")!;
      const head = [...plate.children].find((c) => !c.matches('input[type="hidden"], [hidden]'))!;
      const titleText = document.createRange();
      titleText.selectNodeContents(head.matches("h2") ? head : head.querySelector("h2, h3")!);
      const title = titleText.getBoundingClientRect().left;
      const cells = [...table.querySelectorAll("tr > :first-child")].filter((c) => c.getBoundingClientRect().width > 0);
      return {
        toTitle: [...new Set(cells.map((c) => c.getBoundingClientRect().left + parseFloat(getComputedStyle(c).paddingLeft) - title))],
        noScroll: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      };
    });
    expect(m).toEqual({ toTitle: [0], noScroll: true });
  });
});

// A table may be pulled into its plate's padding, through wrapper boxes that
// draw nothing, but it must not make any box scroll or clip it, nor the page
// scroll sideways, nor reach past the inside of its plate's frame.
async function noSidewaysScroll(page: Page, table: string) {
  return page.locator(table).evaluate((t) => {
    const box = t.getBoundingClientRect();
    const name = (e: Element) => `${e.tagName.toLowerCase()}${e.id ? "#" + e.id : ""}${e.classList.length ? "." + [...e.classList].join(".") : ""}`;
    const clipped: string[] = [];
    for (let e: Element | null = t.parentElement; e && e !== document.documentElement; e = e.parentElement) {
      if (getComputedStyle(e).overflowX !== "visible" && e.scrollWidth > e.clientWidth) clipped.push(`${name(e)} ${e.scrollWidth} > ${e.clientWidth}`);
    }
    const plate = t.closest(".panel, .card")!;
    const p = plate.getBoundingClientRect();
    const ps = getComputedStyle(plate);
    const inner = { left: p.left + parseFloat(ps.borderLeftWidth), right: p.right - parseFloat(ps.borderRightWidth) };
    return {
      page: document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      clipped,
      inPlate: box.left >= inner.left && box.right <= inner.right,
    };
  });
}

test.describe("at 360 px", () => {
  test.use({ viewport: { width: 360, height: 800 } });

  test("the save's players table and the Server page's players table stay inside their boxes", async ({ page, foreman, fakeGame }) => {
    await seed(page, foreman, fakeGame);
    await page.goto("/worlds/base");
    await expect(page.locator("#save-players-table")).toBeVisible();
    expect(await noSidewaysScroll(page, "#save-players-table")).toEqual({ page: true, clipped: [], inPlate: true });

    await page.goto("/");
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(stateBadge(page)).toHaveText("Running", live);
    await fakeGame!.call("/join", { name: "foreman-base", player: "alice" });
    await expect(page.locator("#server-players-table")).toBeVisible(live);
    expect(await noSidewaysScroll(page, "#server-players-table")).toEqual({ page: true, clipped: [], inPlate: true });
  });
});

test("an empty state is centred across its plate", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  for (const path of ["/deleted", "/chat"]) {
    await page.goto(path);
    // The pages still draw their own empty states, so the partial's markup takes the place of the first.
    await page.locator("main .empty--art").first().evaluate((old, markup) => (old.outerHTML = markup), emptyState);
    const m = await page.locator("main .empty--art:has(.empty__text)").first().evaluate((e) => {
      const box = e.getBoundingClientRect();
      const cs = getComputedStyle(e);
      const plate = e.parentElement!;
      const ps = getComputedStyle(plate);
      const p = plate.getBoundingClientRect();
      const left = p.left + parseFloat(ps.borderLeftWidth) + parseFloat(ps.paddingLeft);
      const right = p.right - parseFloat(ps.borderRightWidth) - parseFloat(ps.paddingRight);
      const centre = (left + right) / 2;
      // The drawing is the first grid item: it starts at the top of the
      // content box, centred in its one track.
      const art = getComputedStyle(e, "::before");
      const contentLeft = box.left + parseFloat(cs.paddingLeft);
      const track = box.width - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
      const artWidth = parseFloat(art.width);
      const artLeft = contentLeft + (cs.justifyItems === "center" && art.justifySelf === "auto" ? (track - artWidth) / 2 : 0);
      const artTop = box.top + parseFloat(cs.paddingTop);
      const text = e.querySelector(".empty__text")!.getBoundingClientRect();
      return {
        box: { left: box.left - left, right: right - box.right },
        artCentre: artLeft + artWidth / 2 - centre,
        textCentre: text.left + text.width / 2 - centre,
        artTop: artTop - box.top,
        textTop: text.top - (artTop + parseFloat(art.height)),
        art: `${art.width} ${art.height}`,
      };
    });
    // The drawing sits 24 px below the empty state's top, and the sentence 8 px below the drawing.
    expect(m, path).toEqual({ box: { left: 0, right: 0 }, artCentre: 0, textCentre: 0, artTop: 24, textTop: 8, art: "160px 96px" });
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

test("under reduced motion the working bar stands still, fills its track in a muted tone, and stays in view", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/");
  const m = await measureWorkingBar(page);
  expect(m.motion).toMatch(/^none /);
  expect(m.bar).toEqual({ width: 320, height: 8 });
  expect(m.segment.shown).toBe("visible");
  // A still segment part way along would read as a share done, so it fills the track.
  const still = await page.locator("#working-host .working__bar").evaluate((b) => {
    const seg = getComputedStyle(b, "::before");
    const probe = document.createElement("div");
    document.body.append(probe);
    probe.style.backgroundColor = "var(--color-text-faint)";
    const faint = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return { width: parseFloat(seg.width), track: b.clientWidth, transform: seg.transform, muted: seg.backgroundColor === faint };
  });
  expect(still).toEqual({ width: still.track, track: 318, transform: "none", muted: true });
});

for (const colorScheme of ["dark", "light"] as const) {
  test(`the working bar passes the accessibility scan in the ${colorScheme} theme`, async ({ page, foreman, fakeGame }) => {
    await seed(page, foreman, fakeGame);
    await page.emulateMedia({ colorScheme });
    await page.goto("/");
    await measureWorkingBar(page);
    await page.locator("#working-host .working").evaluate((w) => w.classList.remove("working--idle"));
    await expect(page.locator("#working-host .working__text")).toBeVisible();
    await checkPage(page);
  });
}

test("a checkbox in the mixed state shows a dash on the heading colour", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  for (const colorScheme of ["dark", "light"] as const) {
    await page.emulateMedia({ colorScheme, forcedColors: "none" });
    await page.goto("/");
    const m = await page.evaluate(() => {
      const box = document.createElement("input");
      box.type = "checkbox";
      box.indeterminate = true;
      document.querySelector("main")!.prepend(box);
      const probe = document.createElement("div");
      document.body.append(probe);
      probe.style.backgroundColor = "var(--color-heading)";
      const heading = getComputedStyle(probe).backgroundColor;
      probe.remove();
      const cs = getComputedStyle(box);
      const mark = getComputedStyle(box, "::before");
      return { fill: cs.backgroundColor === heading, edge: cs.borderTopColor === heading, shown: mark.transform, dash: mark.maskImage.includes("M5 12h14") };
    });
    expect(m, colorScheme).toEqual({ fill: true, edge: true, shown: "matrix(1, 0, 0, 1, 0, 0)", dash: true });
  }
  // In forced colours the box is the system's own control, which draws the mixed state itself.
  await page.emulateMedia({ forcedColors: "active" });
  await page.goto("/");
  const forced = await page.evaluate(() => {
    const box = document.createElement("input");
    box.type = "checkbox";
    box.indeterminate = true;
    document.querySelector("main")!.prepend(box);
    return { appearance: getComputedStyle(box).appearance, mark: getComputedStyle(box, "::before").content };
  });
  expect(forced).toEqual({ appearance: "auto", mark: "none" });
});

test("a tab's radio stays hidden at 1 px, not drawn as a checkbox", async ({ page, foreman, fakeGame }) => {
  await seed(page, foreman, fakeGame);
  await page.goto("/");
  const m = await page.evaluate(() => {
    const label = document.createElement("label");
    label.className = "tab";
    label.innerHTML = '<input type="radio" name="probe-tab"> Probe';
    document.querySelector("main")!.prepend(label);
    const r = label.querySelector("input")!.getBoundingClientRect();
    return { width: r.width, height: r.height };
  });
  expect(m).toEqual({ width: 1, height: 1 });
});
