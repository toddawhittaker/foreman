import { test, expect, createAdmin, seedWorld, seedSecond, image21 } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";

// A bar stuck to the window's foot never hides the keyboard focus: the page's
// scroll-padding scrolls a focused control clear of it (WCAG 2.4.11, DESIGN 6
// rule 7). Each test puts a control behind its bar, Tabs onto it, and checks
// that it ends fully above the bar's top edge.

test.use({ fakeGameOn: true });

const live = { timeout: 20_000 };

// tallBox: the control before the bar is taller than the bar (the Console's
// game log), so the page stays at its top, where the bar lies over its foot.
async function tabsClearOf(page: Page, bar: Locator, tallBox = false) {
  await expect(bar).toBeVisible();
  const placed = await bar.evaluate((barEl, tallBox) => {
    const tabbable = [...document.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex="0"]',
    )].filter((el) => el.checkVisibility({ visibilityProperty: true }) && !barEl.contains(el));
    // A control in another sticky box (Generate's preview column) never passes under the bar.
    const inSticky = (el: Element) => {
      for (let a = el.parentElement; a; a = a.parentElement) if (getComputedStyle(a).position === "sticky") return true;
      return false;
    };
    const room = barEl.getBoundingClientRect().height - 8;
    // The controls drawn above the bar's own place, in the order they are
    // drawn. The Snapshots bar comes first in the code but is drawn last.
    const top = (el: Element) => el.getBoundingClientRect().top + window.scrollY;
    const stuck = (barEl as HTMLElement).style.position;
    (barEl as HTMLElement).style.position = "static";
    const barTop = top(barEl);
    (barEl as HTMLElement).style.position = stuck;
    const before = tabbable.filter((el) => top(el) < barTop && !inSticky(el)).sort((a, b) => top(a) - top(b));
    const fits = before.filter((el) => el.getBoundingClientRect().height <= room);
    const target = tallBox ? before[before.length - 1] : fits[fits.length - 1];
    if (!target) return { sticky: false, under: false, found: false };
    if (!tallBox) {
      // The last control that fits behind the bar, drawn 4 px under its top.
      // The bar's own place is further down, so the bar is stuck over it.
      window.scrollBy(0, target.getBoundingClientRect().top - (window.innerHeight - barEl.getBoundingClientRect().height + 4));
      window.scrollBy(0, target.getBoundingClientRect().top - (barEl.getBoundingClientRect().top + 4));
    } else {
      window.scrollTo(0, 0);
    }
    const b = barEl.getBoundingClientRect();
    const r = target.getBoundingClientRect();
    target.setAttribute("data-under-bar", "");
    // Focus the control before it without scrolling, so Tab is what moves the page.
    tabbable[tabbable.indexOf(target) - 1].focus({ preventScroll: true });
    return { sticky: getComputedStyle(barEl).position === "sticky", under: r.top < b.bottom && r.bottom > b.top, found: true };
  }, tallBox);
  expect(placed).toEqual({ sticky: true, under: true, found: true });
  await page.keyboard.press("Tab");
  const target = page.locator("[data-under-bar]");
  await expect(target).toBeFocused();
  const gap = await target.evaluate((el) => {
    const bar = document.querySelector("[data-bar-under-test]")!;
    return bar.getBoundingClientRect().top - el.getBoundingClientRect().bottom;
  });
  expect(gap, "the focused control ends above the bar's top edge").toBeGreaterThanOrEqual(-0.5);
}

async function markBar(bar: Locator) {
  await bar.evaluate((el) => el.setAttribute("data-bar-under-test", ""));
  return bar;
}

test("Generate's action bar never covers the focused control", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/generate");
  await tabsClearOf(page, await markBar(page.locator(".generate > .gen-actions")));
});

test("a settings save bar never covers the focused control", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/settings");
  await tabsClearOf(page, await markBar(page.locator(".form-foot")));
});

test("the Snapshots selection bar never covers the focused control", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await page.goto("/worlds/second/snapshots");
  for (let i = 1; i <= 8; i++) {
    await page.getByLabel("Label (optional)").fill(`Snapshot ${i}`);
    await page.getByRole("button", { name: "Take snapshot" }).click();
    await expect(page.getByText(`Done: Foreman took the snapshot "Snapshot ${i}".`)).toBeVisible();
  }
  await page.goto("/snapshots");
  await page.locator('input[name="snap"]').first().check();
  await tabsClearOf(page, await markBar(page.locator(".select-bar")));
});

test("the phone Console command bar never covers the focused control", async ({ page, foreman, fakeGame }) => {
  test.skip((page.viewportSize()?.width ?? 1000) > 320, "the command bar sticks only on a phone (DESIGN 8.5)");
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main h1 .state")).toHaveText("Running", live);
  await page.goto("/console");
  await tabsClearOf(page, await markBar(page.locator(".terminal .console-form")), true);
});
