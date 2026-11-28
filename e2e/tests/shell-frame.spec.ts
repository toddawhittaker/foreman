import { test, expect, createAdmin, openMenu, seedWorld, signOut, image21, checkPage } from "./foreman.js";
import type { Page } from "@playwright/test";

// The shell (DESIGN 6): the top bar and account menu follow the theme, the
// page frame lines up with the bar at one width, every page header sits in
// one place, and a signed-in 404 keeps the shell. Each test walks the two
// widths and both themes itself, so it runs once, in the first project.

// Real browsers show scroll bars, and the frame must line up with them shown.
test.use({ launchOptions: { ignoreDefaultArgs: ["--hide-scrollbars"] } });

const widths = [
  { width: 1920, height: 1080 },
  { width: 390, height: 844 },
];
const themes = ["light", "dark"] as const;

type Box = { x: number; y: number; width: number; height: number };

async function box(page: Page, selector: string): Promise<Box> {
  const b = await page.locator(selector).first().boundingBox();
  expect(b, selector).not.toBeNull();
  return b!;
}

// The relative luminance of a computed rgb() colour (WCAG 2.2).
async function luminance(page: Page, selector: string, prop: "backgroundColor" | "color" = "backgroundColor") {
  const value = await page.locator(selector).first().evaluate((el, p) => getComputedStyle(el)[p], prop);
  const [r, g, b] = value.match(/[\d.]+/g)!.slice(0, 3).map((c) => {
    const s = Number(c) / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

async function each(page: Page, run: (label: string) => Promise<void>) {
  for (const size of widths) {
    await page.setViewportSize(size);
    for (const theme of themes) {
      await page.emulateMedia({ colorScheme: theme });
      await run(`${size.width} ${theme}`);
    }
  }
}

test("the top bar and the account menu follow the theme", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    await page.goto("/worlds");
    const light = label.endsWith("light");
    const bar = await luminance(page, "header.site");
    expect(light ? bar > 0.8 : bar < 0.02, `${label}: bar ${bar}`).toBe(true);
    // The bar's words take the theme's text colour, dark on the light bar.
    const words = await luminance(page, "header.site .menu > summary", "color");
    expect(light ? words < 0.05 : words > 0.6, `${label}: menu words ${words}`).toBe(true);
    await checkPage(page);
    await openMenu(page);
    const panel = await luminance(page, ".menu__panel");
    expect(light ? panel > 0.8 : panel < 0.05, `${label}: menu panel ${panel}`).toBe(true);
    await page.locator("details.menu__langs > summary").click();
    const item = await luminance(page, ".menu__langs > form .menu__item", "color");
    expect(light ? item < 0.05 : item > 0.6, `${label}: language words ${item}`).toBe(true);
    await checkPage(page);
  });
});

test.describe("logs", () => {
  test.use({ fakeGameOn: true });

  test("the Console and Chat logs follow the theme and stay a sunken box", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    // Chat shows its log only while the game runs (DESIGN 8.14).
    expect((await page.request.post("/worlds/base/start", { form: {}, maxRedirects: 0 })).status()).toBe(303);
    await expect.poll(async () => (await page.request.get("/")).text(), { timeout: 20_000 }).toContain("state--running");
    for (const path of ["/console", "/chat"]) {
      await each(page, async (label) => {
        await page.goto(path);
        const log = page.locator(".log").first();
        await expect(log).toBeVisible();
        const face = await luminance(page, ".log");
        const light = label.endsWith("light");
        expect(light ? face > 0.7 : face < 0.02, `${path} ${label}: log ${face}`).toBe(true);
        // Still a well with an edge; the console's lines are in the mono face.
        const style = await log.evaluate((el) => ({ font: getComputedStyle(el).fontFamily, edge: getComputedStyle(el).borderBlockEndWidth }));
        if (path === "/console") expect(style.font).toMatch(/Mono/);
        expect(style.edge).not.toBe("0px");
        expect(face, `${path} ${label}: apart from the page`).not.toBe(await luminance(page, "body"));
      });
    }
  });
});

test("the account button keeps its place in the bar when the page scrolls, never clipped", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    // The component page is long at every width.
    await page.goto("/design");
    const at = async () => {
      const bar = await box(page, "header.site");
      const b = await box(page, "header.site > .menu > summary");
      return { bar, b };
    };
    const top = await at();
    // Inside the bar and the window, whole.
    expect(top.b.y, label).toBeGreaterThanOrEqual(top.bar.y);
    expect(top.b.y + top.b.height, label).toBeLessThanOrEqual(top.bar.y + top.bar.height);
    expect(top.b.x + top.b.width, label).toBeLessThanOrEqual(page.viewportSize()!.width);
    await page.mouse.wheel(0, 400);
    await expect.poll(() => page.evaluate(() => scrollY)).toBeGreaterThan(0);
    const down = await at();
    // The same box inside the bar, in x and y.
    expect({ x: down.b.x - down.bar.x, y: down.b.y - down.bar.y, w: down.b.width, h: down.b.height }, label).toEqual({
      x: top.b.x - top.bar.x,
      y: top.b.y - top.bar.y,
      w: top.b.width,
      h: top.b.height,
    });
  });
});

test("every page is one width, its frame lined up with the top bar", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    // Pages that once kept an 80rem working width, and one that never did.
    const seen: Record<string, number[]> = {};
    for (const path of ["/worlds", "/settings/general", "/settings/users", "/settings/audit-log", "/no-such-page"]) {
      await page.goto(path);
      const start = (await box(page, "header.site > .product")).x;
      const end = await box(page, "header.site > .menu > summary");
      const tabs = await box(page, "main .subnav, main h1");
      // The content starts under the wordmark and ends under the menu.
      expect(tabs.x, `${label} ${path} start`).toBe(start);
      const last = await page.locator("main > :not(.toasts)").last().boundingBox();
      expect(last!.x + last!.width, `${label} ${path} end`).toBe(end.x + end.width);
      seen[path] = [start, end.x + end.width];
    }
    expect(new Set(Object.values(seen).map((v) => v.join())).size, label).toBe(1);
  });
});

test("a signed-in unknown page keeps the shell, its header where the other error pages have it", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    const parts = async (path: string) => {
      const res = await page.goto(path);
      expect(res!.status(), path).toBe(404);
      await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
      await expect(page.locator("header.site .menu__user")).toHaveText("admin");
      return { bar: await box(page, "header.site"), head: await box(page, "main .pagehead"), h1: await box(page, "main h1"), panel: await box(page, "main .panel") };
    };
    const unknown = await parts("/no-such-page");
    await checkPage(page);
    // A missing page inside an area keeps that area's tabs, so
    // its header grows by the tab row; the bar and the title stay put.
    const inArea = await parts("/worlds/no-such-world");
    expect({ bar: inArea.bar, h1: inArea.h1 }, label).toEqual({ bar: unknown.bar, h1: unknown.h1 });
    expect(await page.locator("main .pagehead nav.subnav").count(), label).toBeGreaterThan(0);
  });
});

test("the gate pages share one header: the wordmark, then one centred title", async ({ page }) => {
  const parts = async (path: string, label: string) => {
    await page.goto(path);
    expect(new URL(page.url()).pathname, label).toBe(path);
    const logo = await box(page, "main > .gate-logo");
    const h1 = await box(page, "main h1");
    // The title is centred under the wordmark.
    expect(Math.abs(h1.x + h1.width / 2 - (logo.x + logo.width / 2)), `${path} ${label}`).toBeLessThanOrEqual(0.5);
    expect(await page.locator("main h1").evaluate((el) => getComputedStyle(el).textAlign), `${path} ${label}`).toBe("center");
    return { logo, h1y: h1.y };
  };
  // A fresh store shows only setup.
  const setup: Record<string, Awaited<ReturnType<typeof parts>>> = {};
  await each(page, async (label) => {
    setup[label] = await parts("/setup", label);
  });
  await createAdmin(page);
  await signOut(page);
  await each(page, async (label) => {
    expect(await parts("/signin", label), label).toEqual(setup[label]);
    expect(await parts("/signed-out", label), label).toEqual(setup[label]);
  });
  // On a wide screen each title is one line.
  await page.setViewportSize(widths[0]);
  for (const path of ["/signin", "/signed-out"]) {
    await page.goto(path);
    expect((await box(page, "main h1")).height, path).toBeLessThan(50);
  }
});

// The page header's slots, filled as a page with buttons and a tab row fills
// them (DESIGN 6, "The page header"): the 404 page's own header, given a crumb,
// buttons and tabs in the browser, so the rules are checked without a page
// that uses all of them yet.
async function fill(page: Page, opts: { crumb: boolean; actions: number; tabs: boolean }) {
  await page.goto("/no-such-page");
  await page.evaluate((o) => {
    const head = document.querySelector("main .pagehead")!;
    if (o.crumb) head.querySelector(".pagehead__crumb")!.innerHTML = `<nav class="crumb" aria-label="Breadcrumb"><ol role="list"><li><a href="/worlds">Worlds</a></li><li><span aria-current="page">Base</span></li></ol></nav>`;
    const actions = head.querySelector(".pagehead__actions")!;
    for (let i = 0; i < o.actions; i++) actions.insertAdjacentHTML("beforeend", `<a class="button" href="/worlds">Act ${i}</a>`);
    if (o.tabs)
      head.insertAdjacentHTML(
        "beforeend",
        `<nav class="subnav" aria-label="Pages"><ul role="list"><li><a href="/" aria-current="page">One</a></li><li><a href="/">Two</a></li><li><a href="/">Three</a></li></ul></nav>`,
      );
  }, opts);
  const get = async (s: string) => (await page.locator(s).count()) ? await box(page, s) : null;
  return { crumb: await get(".pagehead__crumb"), h1: await get(".pagehead h1"), actions: await get(".pagehead__actions .button"), tabs: await get(".pagehead .subnav"), after: await get("main .pagehead + *") };
}

test("the page header keeps its parts in fixed places, with or without a crumb or buttons", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    const bare = await fill(page, { crumb: false, actions: 0, tabs: true });
    const full = await fill(page, { crumb: true, actions: 2, tabs: true });
    const phone = page.viewportSize()!.width < 640;
    // The crumb keeps its row when a page has none, so the title and tab row stay put.
    expect(full.crumb, label).toEqual(bare.crumb);
    // The buttons take room beside the title, never above or below it.
    expect([full.h1!.x, full.h1!.y, full.h1!.height], label).toEqual([bare.h1!.x, bare.h1!.y, bare.h1!.height]);
    if (phone) {
      // On a phone the page's buttons go under the tab row (DESIGN 6).
      expect(full.actions!.y, label).toBeGreaterThanOrEqual(full.tabs!.y + full.tabs!.height);
      expect(full.tabs, label).toEqual(bare.tabs);
    } else {
      // On a wide screen the buttons end the title's row, which has one height.
      expect(full.tabs, label).toEqual(bare.tabs);
      expect(full.after, label).toEqual(bare.after);
      expect(full.actions!.y, label).toBeGreaterThanOrEqual(full.h1!.y - 10);
      expect(full.actions!.y + full.actions!.height, label).toBeLessThanOrEqual(full.tabs!.y);
    }
  });
});
