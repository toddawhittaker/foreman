import { test, expect, createAdmin, signOut, seedWorld, checkPage } from "./foreman.js";
import type { Page } from "@playwright/test";

// The toast (DESIGN 6, "Toasts"): a result message over the page that takes
// no room, is heard through the polite announcer or as an alert, never takes
// focus, closes by keyboard, and stays put while the page scrolls. Without
// script the server's message sits in a slot that keeps its room.

const toasts = (page: Page) => page.locator("#toasts > .toast");
// The "toast-oob" partial's markup, as an htmx answer carries it (DESIGN 6,
// "Toasts"; TestToastOutOfBand pins the server's side).
const toastOOB = (kind: "success" | "error", text: string) =>
  `<div hx-swap-oob="afterbegin:#toasts"><div class="toast toast--${kind}" data-toast><p class="toast__text"><strong>${kind === "error" ? "Error:" : "Done:"}</strong> <span data-toast-text>${text}</span></p><button type="button" class="toast__close" aria-label="Close this message" data-toast-close></button></div></div>`;

// A toast that comes after the page, put where htmx puts it: newest first.
const show = (page: Page, kind: "success" | "error", text: string) =>
  page.evaluate((html) => {
    const t = document.createElement("template");
    t.innerHTML = html;
    document.getElementById("toasts")!.prepend(t.content.querySelector("[data-toast]")!);
  }, toastOOB(kind, text));

// clock.install leaves fake time flowing with real time, so a slow step such
// as an axe run would spend a toast's time unseen. Stopped here, only runFor
// moves it, and "stays 6 s" means 6 s from when the toast lands. The announcer
// fills on the next frame, so the tests run 100 ms (frames) after a toast
// lands and count those 100 ms in its 6 s.
async function stopClock(page: Page) {
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
}

// An htmx request to a path the test answers itself, swapped into a probe
// that must keep its words unless htmx swaps the answer.
async function ask(page: Page, status: number, body: string) {
  await page.route("**/toast-test", (route) => route.fulfill({ status, contentType: "text/html", body }));
  await page.evaluate(() => {
    const probe = document.createElement("p");
    probe.id = "probe";
    probe.textContent = "untouched";
    document.querySelector("main")!.append(probe);
    void (window as unknown as { htmx: { ajax: (v: string, p: string, o: object) => Promise<void> } }).htmx.ajax("POST", "/toast-test", { target: "#probe", swap: "innerHTML" });
  });
}

// axe in both themes while the current state shows.
async function checkThemes(page: Page) {
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme });
    await checkPage(page);
  }
}

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
  // Read out once, through the layout's one alert region.
  await expect(page.locator("#alerts")).toHaveText(/Error: Too many failed sign-ins/);
  await expect(page.locator("[role=alert]")).toHaveCount(1);
  await expect(toast).toContainText("Error:");
  await checkThemes(page);
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
  // axe runs on timers of its own, so it runs while time flows, on this
  // toast; the time is measured on a fresh one.
  await checkThemes(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await toast.evaluate((el) => el.remove());
  await field.focus();

  await stopClock(page);
  await show(page, "success", "Snapshot taken.");
  await expect(toast).toHaveCount(1);
  await page.clock.runFor(100);
  await expect(page.locator("#announcer")).toHaveText(/Snapshot taken\./);
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
    // Through htmx, as the toast-oob partial lands.
    await ask(page, 200, toastOOB("success", "First."));
    await expect(toasts(page)).toHaveCount(1);
    const a = await toasts(page).first().evaluate((el) => el.getBoundingClientRect().toJSON());
    await ask(page, 200, toastOOB("error", "Second."));
    await expect(toasts(page)).toHaveCount(2);
    // The toast already shown keeps its exact box; the newest goes on top.
    const [b, a2] = await toasts(page).evaluateAll((els) => els.map((el) => el.getBoundingClientRect().toJSON()));
    expect(a2, `${size.width}: the first toast moved`).toEqual(a);
    expect(b.y + b.height).toBeLessThanOrEqual(a.y);
    // Whole, inside the window.
    expect(a.y + a.height).toBeLessThanOrEqual(size.height);
    expect(b.y).toBeGreaterThanOrEqual(0);
    expect(a.x).toBeGreaterThanOrEqual(0);
    expect(a.x + a.width).toBeLessThanOrEqual(size.width);
    const at = await page.locator("#toasts").boundingBox();
    await page.mouse.wheel(0, 600);
    await expect.poll(() => page.evaluate(() => scrollY)).toBeGreaterThan(0);
    expect(await page.locator("#toasts").boundingBox()).toEqual(at);
  }
});

test("a toast in an htmx answer lands whole and closes after its time", async ({ page }) => {
  await page.clock.install();
  await createAdmin(page);
  await page.goto("/worlds");
  await stopClock(page);
  await ask(page, 200, `<b>swapped</b>${toastOOB("success", "Saved.")}`);
  await expect(page.locator("#probe")).toHaveText("swapped");
  const toast = toasts(page);
  await expect(toast).toHaveCount(1);
  await expect(toast.locator(".toast__text")).toHaveText("Done: Saved.");
  await expect(toast.getByRole("button", { name: "Close this message" })).toBeVisible();
  await page.clock.runFor(100);
  await expect(page.locator("#announcer")).toHaveText("Done: Saved.");
  await page.clock.runFor(5_900);
  await expect(toast).toBeVisible();
  await page.clock.runFor(4_000);
  await expect(toast).toHaveCount(0);
});

test("an error answer's toast is shown, and nothing else of it is swapped", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/worlds");
  await ask(page, 422, `<b>swapped</b>${toastOOB("error", "The name is taken.")}`);
  const toast = toasts(page);
  await expect(toast).toHaveCount(1);
  await expect(toast).toHaveClass(/toast--error/);
  await expect(page.locator("#alerts")).toHaveText("Error: The name is taken.");
  await expect(page.locator("#probe")).toHaveText("untouched");
  await checkThemes(page);
});

test("toasts still arm after a swap replaces their region", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/worlds");
  await page.evaluate(() => {
    const fresh = document.createElement("div");
    fresh.id = "toasts";
    fresh.className = "toasts";
    document.getElementById("toasts")!.replaceWith(fresh);
  });
  await show(page, "success", "After the swap.");
  await expect(page.locator("#announcer")).toHaveText("Done: After the swap.");
  await expect(toasts(page)).toHaveAttribute("data-armed", "");
});

test("Escape closes the newest toast, unless a menu or a text field has the key", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/general");
  await show(page, "success", "Older.");
  await show(page, "error", "Newest.");
  // In a text field Escape is the field's.
  await page.locator("main input[type=text], main input:not([type])").first().focus();
  await page.keyboard.press("Escape");
  await expect(toasts(page)).toHaveCount(2);
  // An open menu takes Escape first, and only the menu closes.
  await page.locator("header.site .menu > summary").click();
  await page.keyboard.press("Escape");
  await expect(page.locator("details.menu")).not.toHaveAttribute("open");
  await expect(toasts(page)).toHaveCount(2);
  await page.locator("main").focus();
  await page.keyboard.press("Escape");
  await expect(toasts(page)).toHaveCount(1);
  await expect(toasts(page)).toContainText("Older.");
});

test("on a phone the toast sits above the foot bar and never covers the focused control", async ({ page, foreman }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/settings?section=players");
  await show(page, "error", "The save failed because the server could not be reached. Try again in a moment.");
  const bar = page.locator(".form-foot");
  const stack = page.locator("#toasts");
  // Over the bar, not on it.
  const [s, b] = [(await stack.boundingBox())!, (await bar.boundingBox())!];
  expect(s.y + s.height).toBeLessThanOrEqual(b.y);
  // A field drawn 4 px under the toast's top edge, reached by Tab.
  const placed = await page.evaluate(() => {
    const tabbable = [...document.querySelectorAll<HTMLElement>("main a[href], main button:not([disabled]), main input:not([disabled]):not([type=hidden]), main select, main textarea, main summary")].filter(
      (el) => el.checkVisibility({ visibilityProperty: true }) && !el.closest(".form-foot, #toasts"),
    );
    const top = document.getElementById("toasts")!.getBoundingClientRect().top;
    const fields = tabbable.filter((el) => el.matches("input, select, textarea") && el.getBoundingClientRect().height < 60);
    const target = fields[fields.length - 1];
    window.scrollBy(0, target.getBoundingClientRect().top - (top + 4));
    const r = target.getBoundingClientRect();
    target.setAttribute("data-under-toast", "");
    tabbable[tabbable.indexOf(target) - 1].focus({ preventScroll: true });
    return r.top > top && r.top < document.getElementById("toasts")!.getBoundingClientRect().bottom;
  });
  expect(placed, "the field starts under the toast").toBe(true);
  await page.keyboard.press("Tab");
  const target = page.locator("[data-under-toast]");
  await expect(target).toBeFocused();
  const t = (await target.boundingBox())!;
  const s2 = (await stack.boundingBox())!;
  expect(t.y + t.height, "the focused field ends above the toast").toBeLessThanOrEqual(s2.y);
  expect(t.y).toBeGreaterThanOrEqual(0);
});

test("at 320 px a tall stack stays inside the window and scrolls", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 640 });
  await createAdmin(page);
  await page.goto("/worlds");
  const long =
    "The world could not be started because the game image is missing, the save is locked by another process, the disk is nearly full, and the mod portal did not answer. Check the image, close the other process, free some space and wait for the portal, then try again.";
  for (let i = 0; i < 3; i++) await show(page, "error", `${i + 1}. ${long}`);
  const s = (await page.locator("#toasts").boundingBox())!;
  expect(s.y).toBeGreaterThanOrEqual(0);
  expect(s.y + s.height).toBeLessThanOrEqual(640);
  expect(s.x).toBeGreaterThanOrEqual(0);
  expect(s.x + s.width).toBeLessThanOrEqual(320);
  const scrolls = await page.locator("#toasts").evaluate((el) => el.scrollHeight > el.clientHeight && getComputedStyle(el).overflowY === "auto");
  expect(scrolls, "the stack scrolls inside itself").toBe(true);
  await checkPage(page);
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("the server's message sits in a slot that keeps its room", async ({ page }) => {
    await rateLimited(page);
    const toast = toasts(page);
    await expect(toast).toHaveCount(1);
    await expect(toast).toContainText("Too many failed sign-ins");
    await expect(page.locator("#toasts")).not.toHaveCSS("position", "fixed");
    // In the page's flow, at its end: what comes before it ends above it.
    const t = (await toast.boundingBox())!;
    const before = (await page.locator("main > :not(.toasts, template)").last().boundingBox())!;
    expect(t.y).toBeGreaterThanOrEqual(before.y + before.height);
    // A close button needs script, so there is none to press in vain.
    await expect(toast.getByRole("button")).toHaveCount(0);
  });
});
