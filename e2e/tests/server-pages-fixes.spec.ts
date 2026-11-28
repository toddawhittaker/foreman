import { test, expect, createAdmin, seedWorld, image21, live, stateBadge, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Server area's re-sweep (fixes batch 20): plain confirmations for what
// destroys nothing (DESIGN 7.4), every plate's title in its band (DESIGN 7,
// "The title band"), the Console's find as a normal field (DESIGN 6, 7.2),
// the Server plate's buttons in one place across states (DESIGN 6, "Controls
// stay put"), the About page's header and plate, and an error page that keeps
// its area (DESIGN 6, "Sub-navigation"). Each test walks the widths it needs
// itself, so it runs once, in the first project. Layout checks are exact.

test.use({ fakeGameOn: true });

const container = "foreman-base";
const widths = [
  { width: 1920, height: 1080 },
  { width: 390, height: 844 },
];

async function setUp(page: Page, foreman: Foreman, fakeGame: FakeGame) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

async function start(page: Page) {
  await page.goto("/");
  await page.locator("#act-start").click();
  await expect(stateBadge(page)).toHaveText("Running", live);
}

type Box = { x: number; y: number; width: number; height: number };

// A box in page coordinates, so a scroll would show too.
async function box(page: Page, selector: string): Promise<Box> {
  return page.locator(selector).first().evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x + window.scrollX, y: r.y + window.scrollY, width: r.width, height: r.height };
  });
}

test("Stop and Restart confirm in the plain box, not the danger box", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await start(page);
  for (const path of ["/worlds/base/stop", "/worlds/base/restart"]) {
    await page.goto(path);
    const confirm = page.locator("main section.confirm");
    await expect(confirm, `${path}: the plain box`).toHaveClass(/\bconfirm--plain\b/);
    await expect(confirm, `${path}: no landmark name`).not.toHaveAttribute("aria-labelledby");
    // The hazard stripe is a gradient; the plain box's top rule is flat.
    const stripe = await confirm.evaluate((el) => getComputedStyle(el, "::before").backgroundImage);
    expect(stripe, `${path}: no hazard stripe`).toBe("none");
  }
});

// The band runs edge to edge of its plate from the plate's top, inside its
// border, in the raised face every plate's title has.
async function expectBand(page: Page, heading: string, raised: string, where: string) {
  const got = await page.locator(heading).evaluate((h) => {
    const plate = h.closest(".card, .panel.chat-plate")!;
    const band = h.parentElement === plate ? h : h.parentElement!;
    const p = plate.getBoundingClientRect();
    const b = band.getBoundingClientRect();
    const edge = parseFloat(getComputedStyle(plate).borderTopWidth);
    return { first: plate.firstElementChild === band, left: b.left - p.left - edge, right: p.right - b.right - edge, top: b.top - p.top - edge, height: b.height, bg: getComputedStyle(band).backgroundColor };
  });
  expect(got.first, `${where}: the title is the plate's first part`).toBe(true);
  expect({ left: got.left, right: got.right, top: got.top }, `${where}: the band runs edge to edge from the top`).toEqual({ left: 0, right: 0, top: 0 });
  expect(got.height, `${where}: the band is at least 3rem`).toBeGreaterThanOrEqual(48);
  expect(got.bg, `${where}: the raised face`).toBe(raised);
}

test("the Console's game log and Chat's plate have their title bands", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await start(page);
  for (const size of widths) {
    await page.setViewportSize(size);
    await page.goto("/");
    const raised = await page.locator(".server-players-panel > .section-head").evaluate((el) => getComputedStyle(el).backgroundColor);
    await page.goto("/console");
    await expectBand(page, "#log-heading", raised, `${size.width} console`);
    await page.goto("/chat");
    await expect(page.locator("#chat-heading")).toBeVisible();
    await expectBand(page, "#chat-heading", raised, `${size.width} chat`);
  }
});

test("the Console's find is a normal field: 44 px, its label above it in the body colour", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await start(page);
  for (const size of widths) {
    await page.setViewportSize(size);
    await page.goto("/console");
    await expect(page.locator(".log-tools")).toBeVisible();
    const field = await box(page, "#log-find");
    const label = await box(page, 'label[for="log-find"]');
    const command = await box(page, "#command");
    expect(field.height, `${size.width}: the field's height`).toBe(command.height);
    expect(field.height, `${size.width}: at least 44 px`).toBeGreaterThanOrEqual(44);
    expect(label.x, `${size.width}: the label starts where the field starts`).toBe(field.x);
    expect(label.y + label.height, `${size.width}: the label sits above the field`).toBeLessThanOrEqual(field.y);
    const colour = (sel: string) => page.locator(sel).evaluate((el) => getComputedStyle(el).color);
    expect(await colour('label[for="log-find"]'), `${size.width}: the label's colour`).toBe(await colour('label[for="command"]'));
  }
});

// @slow: it walks four server states at two widths, with a forced stop between.
test("the Server plate's buttons keep their place: stopped, running, stopping and restarting", { tag: "@slow" }, async ({ page, foreman, fakeGame }) => {
  test.slow();
  await setUp(page, foreman, fakeGame!);
  const seen: Record<string, Record<string, Box>> = {};
  const measure = async (state: string, parts: string[]) => {
    for (const size of widths) {
      await page.setViewportSize(size);
      await page.goto("/");
      await expect(page.locator("#act-snapshot")).toBeVisible(live);
      for (const part of parts) seen[`${size.width} ${part}`] = { ...seen[`${size.width} ${part}`], [state]: await box(page, part) };
    }
  };
  await page.goto("/");
  await measure("stopped", ["#act-snapshot"]);
  await start(page);
  const running = ["#act-snapshot", "#act-restart", "#act-stop", ".emergency > summary"];
  await measure("running", running);

  // A stop whose save hangs stays in progress.
  await fakeGame!.call("/save-hangs", { name: container, on: true });
  await page.setViewportSize(widths[0]);
  await page.goto("/worlds/base/stop");
  await page.getByLabel("Countdown in seconds").fill("0");
  await page.getByRole("button", { name: "Stop server" }).click();
  await expect(page.locator("#act-stop")).toBeDisabled(live);
  await expect(page.locator(".steps [aria-current=step]")).toContainText("Save the game", live);
  await measure("stopping", running);

  // Force stop ends it; then a restart whose save hangs.
  await page.goto("/worlds/base/force-stop");
  await page.locator("section.confirm").getByRole("button", { name: "Force stop" }).click();
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  await fakeGame!.call("/save-hangs", { name: container, on: false });
  await start(page);
  await fakeGame!.call("/save-hangs", { name: container, on: true });
  await page.goto("/worlds/base/restart");
  await page.getByLabel("Countdown in seconds").fill("0");
  await page.getByRole("button", { name: "Restart server" }).click();
  await expect(page.locator("#act-restart")).toBeDisabled(live);
  await expect(page.locator(".steps [aria-current=step]")).toContainText("Save the game", live);
  await measure("restarting", running);

  for (const [part, states] of Object.entries(seen)) {
    const [first, ...rest] = Object.entries(states);
    for (const [state, b] of rest) expect({ x: b.x, y: b.y }, `${part}: ${state} against ${first[0]}`).toEqual({ x: first[1].x, y: first[1].y });
  }
});

test("the About page has the shared header and its content in a plate", async ({ page }) => {
  await createAdmin(page);
  await page.setViewportSize(widths[0]);
  await page.goto("/snapshots");
  const sibling = await box(page, "main .pagehead > h1");
  await page.goto("/about");
  const h1 = await box(page, "main .pagehead > h1");
  expect({ y: h1.y, height: h1.height }, "the title's box is its siblings'").toEqual({ y: sibling.y, height: sibling.height });
  await expect(page.locator("main > .panel #about-facts")).toBeVisible();
  await expect(page.locator("main > .panel .about__source")).toBeVisible();
});

test("an error page under an area keeps the area's highlight and tabs", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/users/999/delete");
  await expect(page.locator("main h1")).toHaveText("Page not found");
  await expect(page.locator('header.site a[href="/settings/general"]')).toHaveAttribute("aria-current", "true");
  await expect(page.locator("main nav.subnav--settings")).toBeVisible();
  await expect(page.locator('main nav.subnav--settings a[href="/settings/users"]')).toHaveAttribute("aria-current", "true");
});
