import { test, expect, createAdmin, signOut } from "./foreman.js";
import type { Page } from "@playwright/test";

// The toast (DESIGN 6, "Toasts"): a result message over the page that takes
// no room, is heard through the polite announcer or as an alert, never takes
// focus, closes by keyboard, and stays put while the page scrolls. Without
// script the server's message sits in a slot that keeps its room.

const toasts = (page: Page) => page.locator("#toasts > .toast");
// A toast that comes after the page: the toast partial's markup, swapped into
// #toasts as an htmx answer does it (DESIGN 6, "Toasts").
const show = (page: Page, kind: "success" | "error", text: string) =>
  page.evaluate(
    ([k, t]) => {
      const toast = document.createElement("div");
      toast.className = `toast toast--${k}`;
      toast.setAttribute("data-toast", "");
      toast.innerHTML = `<p class="toast__text"><strong>${k === "error" ? "Error:" : "Done:"}</strong> <span data-toast-text></span></p><button type="button" class="toast__close" aria-label="Close this message" data-toast-close></button>`;
      toast.querySelector("[data-toast-text]")!.textContent = t;
      document.getElementById("toasts")!.append(toast);
    },
    [kind, text],
  );

// What the page's own parts occupy, to prove a toast moves none of them.
const layout = (page: Page) =>
  page.locator("main > :not(.toasts), header.site").evaluateAll((els) => els.map((el) => JSON.stringify(el.getBoundingClientRect())));

// Five wrong passwords, then one more: the sign-in page's error that sums up a
// refused attempt is the toast that proves the server's path (SPEC 12.4).
async function rateLimited(page: Page) {
  await createAdmin(page);
  await signOut(page);
  for (let i = 0; i < 6; i++) {
    await page.goto("/signin");
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password", { exact: true }).fill("not the password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
  }
  await expect(page.locator("main")).toContainText("Too many failed sign-ins");
}

test("a refused sign-in's error is a toast: an alert over the page, kept until closed by keyboard", async ({ page }) => {
  await page.clock.install();
  await rateLimited(page);
  const toast = toasts(page);
  await expect(toast).toHaveCount(1);
  await expect(toast).toHaveAttribute("role", "alert");
  await expect(toast).toContainText("Error:");
  await expect(page.locator("#toasts")).toHaveCSS("position", "fixed");
  // It takes no room: the form sits where it sits on a fresh sign-in page.
  const withToast = await page.locator("main form").boundingBox();
  // No field took focus from the toast, and the toast took none.
  const focused = await page.evaluate(() => document.activeElement?.closest("#toasts") !== null);
  expect(focused).toBe(false);
  // An error stays until it is closed.
  await page.clock.runFor(120_000);
  await expect(toast).toBeVisible();
  // Its close button is a 44 px target reached by keyboard.
  const close = toast.getByRole("button", { name: "Close this message" });
  const c = (await close.boundingBox())!;
  expect(Math.min(c.width, c.height)).toBeGreaterThanOrEqual(44);
  await page.locator("main").focus();
  let reached = false;
  for (let i = 0; i < 40 && !reached; i++) {
    await page.keyboard.press("Tab");
    reached = await close.evaluate((el) => el === document.activeElement);
  }
  expect(reached).toBe(true);
  await page.keyboard.press("Enter");
  await expect(toast).toHaveCount(0);
  // Focus goes back into the page, not to nowhere.
  expect(await page.evaluate(() => document.activeElement !== document.body)).toBe(true);
  expect(await page.locator("main form").boundingBox()).toEqual(withToast);
  await page.goto("/signin");
  expect(await page.locator("main form").boundingBox()).toEqual(withToast);
});

test("a success toast is announced politely, stays at least 6 s, and waits while hovered or focused", async ({ page }) => {
  await page.clock.install();
  await createAdmin(page);
  await page.goto("/worlds");
  const field = page.locator("header.site .menu > summary");
  await field.focus();
  const before = await layout(page);
  await show(page, "success", "Snapshot taken.");
  const toast = toasts(page);
  await expect(toast).toHaveCount(1);
  await expect(toast).toContainText("Done:");
  await expect(toast).not.toHaveAttribute("role", "alert");
  await expect(page.locator("#announcer")).toHaveText(/Snapshot taken\./);
  // Focus stays where it was and nothing moved.
  await expect(field).toBeFocused();
  expect(await layout(page)).toEqual(before);

  await page.clock.runFor(5_900);
  await expect(toast).toBeVisible();
  // Hovered, it waits.
  await toast.hover();
  await page.clock.runFor(30_000);
  await expect(toast).toBeVisible();
  await page.mouse.move(0, 0);
  // Focused, it waits too.
  await toast.getByRole("button", { name: "Close this message" }).focus();
  await page.clock.runFor(30_000);
  await expect(toast).toBeVisible();
  await field.focus();
  await page.clock.runFor(6_000);
  await expect(toast).toBeVisible();
  await page.clock.runFor(10_000);
  await expect(toast).toHaveCount(0);
});

test("toasts stack in one fixed place that stays put while the page scrolls", async ({ page }) => {
  await page.clock.install();
  await createAdmin(page);
  for (const size of [{ width: 1920, height: 1080 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(size);
    await page.goto("/design");
    await show(page, "success", "First.");
    await show(page, "error", "Second.");
    const [a, b] = await toasts(page).evaluateAll((els) => els.map((el) => el.getBoundingClientRect().toJSON()));
    // The newest sits under the one before it, and neither covers the other.
    expect(b.y).toBeGreaterThanOrEqual(a.y + a.height);
    // Whole, inside the window.
    expect(b.y + b.height).toBeLessThanOrEqual(size.height);
    expect(a.x).toBeGreaterThanOrEqual(0);
    expect(a.x + a.width).toBeLessThanOrEqual(size.width);
    await expect(toasts(page).nth(1)).toHaveAttribute("role", "alert");
    const at = await page.locator("#toasts").boundingBox();
    await page.mouse.wheel(0, 600);
    await expect.poll(() => page.evaluate(() => scrollY)).toBeGreaterThan(0);
    expect(await page.locator("#toasts").boundingBox()).toEqual(at);
  }
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("the server's message sits in a slot that keeps its room", async ({ page }) => {
    await rateLimited(page);
    const toast = toasts(page);
    await expect(toast).toHaveCount(1);
    await expect(toast).toContainText("Too many failed sign-ins");
    await expect(page.locator("#toasts")).not.toHaveCSS("position", "fixed");
    // In the page's flow: nothing after it starts above its foot.
    const t = (await toast.boundingBox())!;
    const next = (await page.locator("main > .toasts ~ :not(template)").first().boundingBox())!;
    expect(next.y).toBeGreaterThanOrEqual(t.y + t.height);
    // A close button needs script, so there is none to press in vain.
    await expect(toast.getByRole("button")).toHaveCount(0);
  });
});
