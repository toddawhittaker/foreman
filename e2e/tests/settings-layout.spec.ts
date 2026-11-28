import { test, expect, createAdmin, adminPassword, checkPage } from "./foreman.js";
import type { Page } from "@playwright/test";

// The Settings area holds still (DESIGN 6, "The page header" and "Toasts";
// SPEC 17): the crumb, title and tab row sit in the same boxes on every tab,
// a page's own button sits under the tab row on a phone, and a save or a
// refusal says so in a toast, so nothing on the page moves. Boxes are checked
// in x and y at 0 px, at 1920 and 390 wide, in both themes; each test walks
// them itself, so it runs once, in the first project.

const widths = [
  { width: 1920, height: 1080 },
  { width: 390, height: 844 },
];
const themes = ["light", "dark"] as const;

type Box = { x: number; y: number; width: number; height: number };

// A part's box in the page, not the window, so a scroll does not read as a move.
async function box(page: Page, selector: string): Promise<Box> {
  const b = await page.locator(selector).first().boundingBox();
  expect(b, selector).not.toBeNull();
  const scrollY = await page.evaluate(() => window.scrollY);
  return { ...b!, y: b!.y + scrollY };
}

// Boxes of several parts, read at once.
async function boxes(page: Page, selectors: string[]) {
  const out: Record<string, Box> = {};
  for (const s of selectors) out[s] = await box(page, s);
  return out;
}

async function each(page: Page, run: (label: string, phone: boolean) => Promise<void>) {
  for (const size of widths) {
    await page.setViewportSize(size);
    for (const theme of themes) {
      await page.emulateMedia({ colorScheme: theme });
      await run(`${size.width} ${theme}`, size.width < 640);
    }
  }
}

const tabs = [
  "/settings/general",
  "/settings/restart-schedule",
  "/settings/notifications",
  "/settings/public-mods",
  "/settings/api-tokens",
  "/settings/users",
  "/settings/audit-log",
  "/settings/factorio-account",
];

const tabRow = 'main .pagehead nav[aria-label="Foreman settings"]';

test("every Settings tab puts its crumb, title and tab row in the same boxes", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label, phone) => {
    let first: { crumb: Box; title: Box; tabs: Box } | undefined;
    for (const path of tabs) {
      await page.goto(path);
      const crumb = await box(page, "main .pagehead__crumb");
      const title = await box(page, "main .pagehead > h1");
      const row = await box(page, tabRow);
      // The title's words differ, so its width may; its place and height do not.
      const got = { crumb, title: { ...title, width: 0 }, tabs: row };
      if (!first) first = got;
      expect(got, `${label} ${path}`).toEqual(first);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${label} ${path} scrolls sideways`).toBe(true);
    }
  });
});

// The plates and the control used keep their boxes when the page comes back
// with its result; the toast floats over the page.
async function expectStill(page: Page, label: string, selectors: string[], act: () => Promise<void>) {
  const before = await boxes(page, selectors);
  await act();
  await expect(page.locator("#toasts > [data-toast]").first()).toBeVisible();
  expect(await boxes(page, selectors), label).toEqual(before);
}

test("saving General and Restart schedule moves nothing on the page", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    await page.goto("/settings/general");
    await expectStill(page, `${label} general`, [tabRow, "form.general-grid", "#act-general-save", "#general-snapshots-heading"], async () => {
      await page.locator("#act-general-save").click();
      await expect(page.locator("#toasts .toast--success")).toBeVisible();
    });
    await expect(page.locator("#toasts")).toContainText("Saved. The new settings apply from now on.");

    await page.goto("/settings/restart-schedule");
    await expectStill(page, `${label} schedule`, [tabRow, "#schedule-state", "form.setting-row", "#act-schedule-save"], async () => {
      await page.locator("#act-schedule-save").click();
      await expect(page.locator("#toasts .toast--success")).toBeVisible();
    });
  });
});

test("turning MCP access on moves neither the plates nor its button's row", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    await page.goto("/settings/api-tokens");
    const on = (await page.locator("#act-mcp").getAttribute("value")) === "on";
    // The state's words change, and so does the create form's note on
    // whether a token can work yet, so the plates keep their place and width
    // and the button its row.
    const place = async () => {
      const b = await boxes(page, [tabRow, "#mcp-access", "#api-access", "#create-token"]);
      for (const s of ["#mcp-access", "#api-access", "#create-token"]) b[s] = { ...b[s], height: 0 };
      return b;
    };
    const before = await place();
    const button = await box(page, "#act-mcp");
    await page.locator("#act-mcp").click();
    await expect(page.locator("#act-mcp")).toHaveAttribute("value", on ? "off" : "on");
    await expect(page.locator("#toasts > [data-toast]")).toHaveCount(1);
    expect(await place(), label).toEqual(before);
    expect((await box(page, "#act-mcp")).y, label).toBe(button.y);
  });
  await checkPage(page);
});

test("a wrong current password leaves the user list where it was", async ({ page }) => {
  await createAdmin(page);
  await each(page, async (label) => {
    await page.goto("/settings/users");
    const list = 'section[aria-labelledby="users-heading"]';
    const plate = 'section[aria-labelledby="password-heading"]';
    const before = await boxes(page, [tabRow, list]);
    const top = (await box(page, plate)).y;
    await page.getByLabel("Current password").fill("not the password");
    await page.getByLabel("New password", { exact: true }).fill("a brand new password");
    await page.getByLabel("New password again").fill("a brand new password");
    await page.locator("#act-password").click();
    // The error summary takes focus (DESIGN 7.2), so the window may scroll to it; boxes are read in the page.
    await expect(page.locator("#password-current-error")).toBeVisible();
    expect(await boxes(page, [tabRow, list]), label).toEqual(before);
    expect((await box(page, plate)).y, label).toBe(top);
    // The right password changes it, and the toast says so.
    await page.getByLabel("Current password").fill(adminPassword);
    await page.getByLabel("New password", { exact: true }).fill(adminPassword);
    await page.getByLabel("New password again").fill(adminPassword);
    await page.locator("#act-password").click();
    await expect(page.locator("#toasts")).toContainText("Done:");
    expect(await boxes(page, [tabRow, list]), label).toEqual(before);
  });
});

test.describe("with a secret key", () => {
  test.use({ foremanEnv: { FOREMAN_SECRET_KEY: "keykeykeykeykeykeykeykeykeykeykey" } });

  test("Add a webhook ends the title's row, or sits under the tab row on a phone, and the tab row stays put", async ({ page }) => {
    await createAdmin(page);
    await each(page, async (label, phone) => {
      await page.goto("/settings/restart-schedule");
      const without = await boxes(page, ["main .pagehead__crumb", tabRow]);
      await page.goto("/settings/notifications");
      const add = await box(page, "#act-webhook-add");
      const row = await box(page, tabRow);
      const title = await box(page, "main .pagehead > h1");
      expect(await boxes(page, ["main .pagehead__crumb", tabRow]), label).toEqual(without);
      if (phone) expect(add.y, label).toBeGreaterThanOrEqual(row.y + row.height);
      else {
        expect(add.y + add.height, label).toBeLessThanOrEqual(row.y);
        expect(add.y, label).toBeLessThanOrEqual(title.y + title.height);
      }
    });
  });

  test("saving the factorio.com account keeps the plate where it was", async ({ page }) => {
    await createAdmin(page);
    await each(page, async (label, phone) => {
      await page.goto("/settings/factorio-account");
      if (await page.getByRole("button", { name: "Remove account" }).count()) {
        await page.getByRole("button", { name: "Remove account" }).click();
        await expect(page.locator("#account-none")).toBeVisible();
      }
      const before = await boxes(page, [tabRow, ".setting-rows"]);
      await page.getByLabel("factorio.com username").fill("portal-user");
      await page.getByLabel("factorio.com token").fill("portal-token");
      await page.getByRole("button", { name: "Save account" }).click();
      await expect(page.locator("#toasts")).toContainText("The account is saved.");
      const after = await boxes(page, [tabRow, ".setting-rows"]);
      // The plate's content changes from the form to the stored state; its
      // place does not while the state sentence over it keeps one line.
      expect(after[tabRow], label).toEqual(before[tabRow]);
      expect(after[".setting-rows"].x, label).toBe(before[".setting-rows"].x);
      if (!phone) expect(after[".setting-rows"].y, label).toBe(before[".setting-rows"].y);
    });
  });
});
