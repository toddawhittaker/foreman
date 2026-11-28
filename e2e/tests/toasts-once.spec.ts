import { test, expect, createAdmin, seedWorld, seedSecond, image21, live } from "./foreman.js";
import type { Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import type { Foreman } from "./foreman.js";

// DESIGN 6, "Toasts", and SPEC 17.21: a result shows as a toast, once per
// open page. The toast takes no room and never takes focus, the address
// parameters it came from leave the address, and a reload shows it no more.

function sql(f: Foreman, query: string, ...args: (string | number)[]) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return db.prepare(query).all(...args) as Record<string, unknown>[];
  } finally {
    db.close();
  }
}

// The boxes of each selector's first element, as whole numbers of CSS px
// from the page's top left, read in one call.
async function boxes(page: Page, selectors: string[]) {
  return page.evaluate((sels) => sels.map((s) => {
    const el = document.querySelector(s);
    if (!el) throw new Error(`no element for ${s}`);
    const r = el.getBoundingClientRect();
    return { s, x: r.x + window.scrollX, y: r.y + window.scrollY };
  }), selectors);
}

const toast = (page: Page, text: string | RegExp) => page.locator("#toasts [data-toast]").filter({ hasText: text });
const header = [".crumb", "main h1", ".subnav--settings"];

async function noFocusInToasts(page: Page) {
  expect(await page.evaluate(() => !!document.activeElement?.closest("#toasts"))).toBe(false);
}

async function tokenAged(page: Page, f: Foreman, days: number) {
  await createAdmin(page);
  await page.goto("/settings/api-tokens");
  await page.getByLabel("Name").fill("A1");
  await page.getByRole("button", { name: "Create token" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Token A1 created");
  sql(f, "UPDATE api_tokens SET period_start = ? WHERE name = 'A1'", Math.floor(Date.now() / 1000) - days * 86400);
}

const renewed = "Renewed A1. It works for another 30 days.";

test("Renew from a row shows a toast; the header stays put and a reload shows it no more", async ({ page, foreman }) => {
  // Ten days in, the token is not near expiry, so no banner comes and goes.
  await tokenAged(page, foreman, 10);
  await page.goto("/settings/api-tokens");
  await expect(page.locator("#token-banner")).toHaveCount(0);
  const before = await boxes(page, header);
  await page.locator("#my-tokens").getByRole("button", { name: "Renew A1" }).click();
  await expect(toast(page, renewed)).toBeVisible();
  await expect(page.locator("#renewed-message")).toHaveCount(0);
  expect(await boxes(page, header)).toEqual(before);
  await noFocusInToasts(page);
  await expect(page).toHaveURL("/settings/api-tokens");
  await page.reload();
  await expect(page.locator("main h1")).toBeVisible();
  await expect(toast(page, renewed)).toHaveCount(0);
});

test("Renew from the banner shows a toast; the header is where the page without the banner has it", async ({ page, foreman }) => {
  await tokenAged(page, foreman, 26);
  await page.goto("/worlds");
  await expect(page.locator("#token-banner")).toContainText("Your API token A1 expires at");
  await page.locator("#token-banner").getByRole("button", { name: "Renew A1" }).click();
  await expect(toast(page, renewed)).toBeVisible();
  await expect(page.locator("#renewed-message")).toHaveCount(0);
  await noFocusInToasts(page);
  await expect(page).toHaveURL("/worlds");
  const after = await boxes(page, [".crumb, main h1", "main h1"]);
  await page.reload();
  await expect(page.locator("main h1")).toBeVisible();
  await expect(toast(page, renewed)).toHaveCount(0);
  expect(await boxes(page, [".crumb, main h1", "main h1"])).toEqual(after);
});

test("a crafted Renewed link shows no toast", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/worlds?done=renewed&name=Made%20up");
  await expect(page.locator("main h1")).toBeVisible();
  await expect(page.locator("#toasts [data-toast]")).toHaveCount(0);
});

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("Take snapshot's Done toast drops the result parameter, and a reload shows it no more", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await expect(page.locator("main h1 .state")).toHaveText("Stopped", live);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(toast(page, "Done:")).toBeVisible(live);
    await expect(page).toHaveURL("/");
    await noFocusInToasts(page);
    await page.reload();
    await expect(page.locator("main h1 .state")).toHaveText("Stopped", live);
    // Long enough for the live stream's first send.
    await page.waitForTimeout(1500);
    await expect(toast(page, "Done:")).toHaveCount(0);
  });

  test("a switch shows its Done toast once on the acting page and once on another open Server page", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await expect(page.locator("main h1 .state")).toHaveText("Stopped", live);
    const other = await page.context().newPage();
    await other.goto("/");
    await expect(other.locator("main h1 .state")).toHaveText("Stopped", live);

    await page.getByRole("link", { name: "Switch world" }).click();
    await page.getByRole("link", { name: "Switch to Second" }).click();
    await page.getByRole("button", { name: "Switch to Second" }).click();
    const switched = "The worlds are switched, from Base to Second.";
    for (const p of [page, other]) {
      await expect(p.locator("#server-title", { hasText: "Second" })).toBeVisible(live);
      await expect(toast(p, switched)).toHaveCount(1, live);
    }
    for (const p of [page, other]) {
      await expect(p).toHaveURL("/");
      await p.reload();
      await expect(p.locator("#server-title", { hasText: "Second" })).toBeVisible(live);
      await p.waitForTimeout(1500);
      await expect(toast(p, switched)).toHaveCount(0);
    }
    await other.close();
  });

  test("Select all automatic with none says so in a toast, and nothing on the page moves", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/worlds/base/snapshots");
    await page.getByLabel("Label (optional)").fill("Only a manual one");
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(toast(page, 'Done: Foreman took the snapshot "Only a manual one".')).toBeVisible();

    await page.goto("/snapshots");
    const parts = ["main h1", "#list-heading", ".group__head button[name=select]", "main table", ".disk-use"];
    const before = await boxes(page, parts);
    await page.getByRole("button", { name: "Select all automatic", exact: true }).click();
    await expect(toast(page, "There are no automatic snapshots to tick.")).toBeVisible();
    await expect(page.locator("#tick-note")).toHaveCount(0);
    expect(await boxes(page, parts)).toEqual(before);
    await noFocusInToasts(page);
  });
});
