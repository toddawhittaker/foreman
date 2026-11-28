import { test, expect, checkPage, createAdmin, seedSecond, seedWorld } from "./foreman.js";
import type { Page } from "@playwright/test";

// Nothing on the worlds and snapshots pages moves because of a change
// elsewhere on the page: a result is a toast over the page, the selection bar
// keeps its room, and sibling tabs draw one page header in one place (DESIGN 6,
// "Toasts" and "The page header"; DESIGN 8.3). Boxes are compared exactly, in
// x and y. Each test walks both widths and both themes itself.

// Taking a snapshot asks Docker whether the game is running.
test.use({ fakeGameOn: true });

const widths = [
  { width: 1920, height: 1080 },
  { width: 390, height: 844 },
];
const themes = ["light", "dark"] as const;

async function each(page: Page, run: (label: string) => Promise<void>) {
  for (const size of widths) {
    await page.setViewportSize(size);
    for (const theme of themes) {
      await page.emulateMedia({ colorScheme: theme });
      await run(`${size.width} ${theme}`);
    }
  }
}

type Box = { x: number; y: number; width: number; height: number };

// The page-relative boxes of the selectors' first matches, so a scroll does
// not count as a move.
async function boxes(page: Page, selectors: string[]): Promise<Record<string, Box>> {
  const out: Record<string, Box> = {};
  for (const s of selectors) {
    const el = page.locator(s).first();
    await expect(el, s).toBeAttached();
    out[s] = await el.evaluate((e) => {
      const r = e.getBoundingClientRect();
      return { x: r.x + window.scrollX, y: r.y + window.scrollY, width: r.width, height: r.height };
    });
  }
  return out;
}

// Opens path once its own content is in: the release notice loads after the
// page, and the save's players are read on the first visit and listed from
// the next, so the page is reloaded until the players plate stops changing.
async function settled(page: Page, path: string) {
  let last = -1;
  for (let i = 0; i < 10; i++) {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    const height = await page.locator("#save-players").evaluate((e) => e.getBoundingClientRect().height);
    if (height === last) return;
    last = height;
    await page.waitForTimeout(250);
  }
  throw new Error(`${path} never settled`);
}

const scrollY = (page: Page) => page.evaluate(() => window.scrollY);

async function takeSnapshot(page: Page, world: string, label: string) {
  await page.goto(`/worlds/${world}/snapshots`);
  await page.getByLabel("Label (optional)").fill(label);
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.locator("#toasts")).toContainText(`Done: Foreman took the snapshot "${label}".`);
}

test("ticking a snapshot moves nothing: the selection bar keeps its room", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await takeSnapshot(page, "second", "First");
  await takeSnapshot(page, "second", "Second");
  for (const path of ["/worlds/second/snapshots", "/snapshots"]) {
    await each(page, async (label) => {
      await page.goto(path);
      const box = page.locator('main input[name="snap"]').first();
      await box.scrollIntoViewIfNeeded();
      const watched = ['main input[name="snap"]', "main tbody tr:nth-child(2)", "main .group__head .button", "main .disk-use", "main .select-bar"];
      const before = await boxes(page, watched);
      const y = await scrollY(page);
      await expect(page.locator(".select-bar")).toBeHidden();
      await box.check();
      await expect(page.locator(".select-bar")).toBeVisible();
      expect(await boxes(page, watched), `${path} ${label}`).toEqual(before);
      expect(await scrollY(page), `${path} ${label}: scroll`).toBe(y);
      await box.uncheck();
      await expect(page.locator(".select-bar")).toBeHidden();
      expect(await boxes(page, watched), `${path} ${label}: unticked`).toEqual(before);
    });
  }
  await checkPage(page);
});

test("a result is a toast: the control just used and the page around it keep their boxes", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await each(page, async (label) => {
    // The rename keeps the name, so the page after it is the page before it plus a toast.
    await settled(page, "/worlds/second");
    const watched = ["main .pagehead", "main .pagehead > nav", "#rename-heading", "#name", 'form[action$="/rename"] button[type="submit"]', "main .world-manage"];
    const button = page.locator('form[action$="/rename"] button[type="submit"]');
    await button.scrollIntoViewIfNeeded();
    const before = await boxes(page, watched);
    await button.click();
    await expect(page.locator("#toasts .toast--success")).toBeVisible();
    await page.waitForLoadState("networkidle");
    expect(await boxes(page, watched), label).toEqual(before);
    expect(await page.locator("main .message--success").count(), `${label}: no message in the page`).toBe(0);
  });
  await checkPage(page);
});

// Known gap: the rename posts and redirects, so the new page loads at the top.
test.fixme("a rename keeps the page's scroll", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await each(page, async (label) => {
    await settled(page, "/worlds/second");
    const button = page.locator('form[action$="/rename"] button[type="submit"]');
    await button.scrollIntoViewIfNeeded();
    const y = await scrollY(page);
    await button.click();
    await expect(page.locator("#toasts .toast--success")).toBeVisible();
    await page.waitForLoadState("networkidle");
    expect(await scrollY(page), `${label}: scroll`).toBe(y);
  });
});

test("sibling tabs draw the page header in one place", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  const parts = ["main .pagehead__crumb", "main .pagehead > h1", "main .pagehead > nav"];
  // The title's words differ between pages, so its width may; its place and height may not.
  const place = (b: Record<string, Box>) =>
    Object.fromEntries(Object.entries(b).map(([k, v]) => [k, k.endsWith("nav") ? v : { x: v.x, y: v.y, height: v.height }]));
  for (const group of [
    ["/worlds/base", "/worlds/base/saves", "/worlds/base/snapshots", "/worlds/base/map"],
    ["/worlds", "/favorites", "/deleted"],
  ]) {
    await each(page, async (label) => {
      let first: Record<string, unknown> | undefined;
      for (const path of group) {
        await page.goto(path);
        const got = place(await boxes(page, parts));
        if (!first) first = got;
        else expect(got, `${label}: ${path} against ${group[0]}`).toEqual(first);
      }
    });
  }
});

test("Restore opened from the Snapshots tab keeps Snapshots highlighted, and from a world keeps Worlds", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await takeSnapshot(page, "second", "Keep me");
  const nav = page.getByRole("navigation", { name: "Main" });

  await page.goto("/snapshots");
  await page.getByRole("link", { name: /^Restore/ }).first().click();
  await expect(page).toHaveURL(/\/restore\?from=all$/);
  await expect(nav.getByRole("link", { name: "Snapshots" })).toHaveAttribute("aria-current", "true");
  await expect(nav.getByRole("link", { name: "Worlds" })).not.toHaveAttribute("aria-current");
  await expect(page.locator("main section.confirm")).toBeVisible();
  await checkPage(page);
  await page.getByRole("link", { name: /^Cancel/ }).click();
  await expect(page).toHaveURL("/snapshots");

  await page.goto("/worlds/second/snapshots");
  await page.getByRole("link", { name: /^Restore/ }).first().click();
  await expect(nav.getByRole("link", { name: "Worlds" })).toHaveAttribute("aria-current", "true");
  await expect(nav.getByRole("link", { name: "Snapshots" })).not.toHaveAttribute("aria-current");
});

test("the factory map with no picture starts at the tab row's edge", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/worlds/base/map");
  const tabs = (await page.locator("main .pagehead > nav").boundingBox())!;
  // The drawing's plate holds the summary, so the plate starts at the edge (DESIGN 8.22).
  for (const s of ["main #factorymap-state", "main .factorymap-mode"]) {
    const b = page.locator(s);
    await expect(b, s).toHaveCount(1);
    expect((await b.boundingBox())!.x, s).toBe(tabs.x);
  }
});

test("side-by-side plates on Saves have title bands of one height, and the export card's intro is under its band", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/worlds/base/saves");
  const bands = await page.locator(".split--saves .panel").evaluateAll((plates) => plates.map((p) => p.firstElementChild!.getBoundingClientRect().height));
  expect(bands).toHaveLength(2);
  expect(bands[0]).toBe(bands[1]);
  await page.goto("/worlds/base");
  expect(await page.locator("#export-hint").evaluate((e) => e.closest(".world-share__head") === null && e.closest(".world-share__body") !== null)).toBe(true);
});

// DESIGN 6, "Toasts", "Shown once": a toast built from a result parameter
// drops it from the address once shown, keeping the other parameters and the
// hash, so a reload or Back shows and announces nothing again.
test("a result toast shows once: the address loses its parameter and a reload shows none", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await takeSnapshot(page, "second", "Kept");
  // The snapshot's name, from its Restore button's id.
  const taken = (await page.locator('main a[id^="restore-"]').first().getAttribute("id"))!.slice("restore-".length);
  for (const c of [
    { path: "/worlds/second", param: "renamed", value: "Second", text: "Renamed to Second." },
    { path: "/worlds/second", param: "imported", value: "1", text: "Imported Second" },
    { path: "/worlds/second/snapshots", param: "taken", value: taken, text: 'Foreman took the snapshot "Kept".' },
    { path: "/snapshots", param: "removed", value: "2", text: "Foreman deleted 2 snapshots." },
    { path: "/worlds/second/saves", param: "chosen", value: "1", text: "" },
  ]) {
    const what = `${c.path}?${c.param}`;
    await page.goto(`${c.path}?keep=1&${c.param}=${encodeURIComponent(c.value)}#main`);
    const toast = page.locator("#toasts [data-toast]");
    await expect(toast, what).toHaveCount(1);
    if (c.text) await expect(toast, what).toContainText(c.text);
    await expect.poll(() => new URL(page.url()).searchParams.has(c.param), what).toBe(false);
    const url = new URL(page.url());
    expect(url.searchParams.get("keep"), `${what}: other parameters stay`).toBe("1");
    expect(url.hash, `${what}: the hash stays`).toBe("#main");
    await page.reload();
    await expect(page.locator("main h1"), what).toBeVisible();
    await expect(page.locator("#toasts [data-toast]"), `${what}: after a reload`).toHaveCount(0);
  }
});

// The toast leaves after a while; the way to the snapshot it named stays.
test("after a snapshot is taken the page links to it", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await takeSnapshot(page, "second", "Linked");
  const link = page.getByRole("link", { name: "Go to the snapshot" });
  await expect(link).toBeVisible();
  const target = (await link.getAttribute("href"))!;
  await expect(page.locator(target)).toHaveCount(1);
  await checkPage(page);
});

// The worlds list's Generate and Import buttons in the page header draw their icons.
test("the worlds list's header buttons draw their icons", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/worlds");
  for (const name of ["Generate", "Import"]) {
    const button = page.locator("main .pagehead__actions").getByRole("link", { name: new RegExp(`^${name}`) });
    await expect(button, name).toHaveCount(1);
    const icon = await button.evaluate((e) => {
      const s = getComputedStyle(e, "::before");
      return { content: s.content, mask: s.maskImage || s.webkitMaskImage, width: s.width };
    });
    expect(icon.content, name).not.toBe("none");
    expect(icon.mask, name).toMatch(/url\(/);
    expect(icon.width, name).toBe("18px");
  }
});
