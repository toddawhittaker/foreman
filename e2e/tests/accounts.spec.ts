import { test, expect, checkPage } from "./foreman.js";
import type { Page } from "@playwright/test";

const admin = "admin";
const password = "correct horse battery";

async function createAdmin(page: Page) {
  await page.goto("/setup");
  await page.getByLabel("Username").fill(admin);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
}

async function signIn(page: Page, user: string, pw: string) {
  await page.getByLabel("Username").fill(user);
  await page.getByLabel("Password").fill(pw);
  await page.getByRole("button", { name: "Sign in" }).click();
}

// SPEC 12.3, 12.4 and 12.8: the first-run journey, checked on every page it visits.
test("first run: setup, sign out, sign in again", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL("/setup");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Create the first admin account");
  await expect(page.getByText("Use at least 12 characters.")).toBeVisible();
  await checkPage(page);

  // The password rule is enforced, and the error summary takes focus (DESIGN 7.2).
  await page.getByLabel("Username").fill(admin);
  await page.getByLabel("Password", { exact: true }).fill("short");
  await page.getByLabel("Confirm password").fill("short");
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/setup");
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page.locator("#error-summary")).toContainText("Use at least 12 characters");
  await expect(page).toHaveTitle(/^Error: Create the first admin account/);
  await checkPage(page);

  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByLabel("Confirm password").fill(password);
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
  await expect(page.getByText(`Signed in as ${admin}`)).toBeVisible();
  await checkPage(page);

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL("/signed-out");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("You are signed out");
  await checkPage(page);

  // The session really ended: home now asks for sign-in, not setup.
  await page.getByRole("link", { name: "Sign in again" }).click();
  await expect(page).toHaveURL("/signin");
  await checkPage(page);

  // A wrong password does not say which field was wrong (SPEC 12.4).
  await signIn(page, admin, "not the right password");
  await expect(page.locator("#error-summary")).toBeFocused();
  const wrongPassword = await page.locator("#error-summary").innerText();
  await checkPage(page);
  await signIn(page, "nobody", "not the right password");
  await expect(page.locator("#error-summary")).toHaveText(wrongPassword);
  await expect(page.locator("main")).not.toContainText(/username is (wrong|unknown|not found)|no such user/i);
  await expect(page.locator("main")).not.toContainText(/password is (wrong|incorrect)/i);

  await signIn(page, admin, password);
  await expect(page).toHaveURL("/");
  await expect(page.getByText(`Signed in as ${admin}`)).toBeVisible();
});

// SPEC 12.3: setup is open only until an admin exists.
test("setup after an admin exists goes to sign-in", async ({ page, context }) => {
  await createAdmin(page);
  await context.clearCookies();
  await page.goto("/setup");
  await expect(page).toHaveURL("/signin");
  // Signed in or not, the setup page stays closed.
  await signIn(page, admin, password);
  await page.goto("/setup");
  await expect(page).toHaveURL("/signin");
});

// SPEC 12.4: repeated failures for one username reach the limit.
test("repeated failures reach the rate-limit message", async ({ page }) => {
  await createAdmin(page);
  await page.getByRole("button", { name: "Sign out" }).click();
  await page.goto("/signin");
  for (let i = 0; i < 5; i++) {
    await signIn(page, admin, `wrong password ${i}`);
    await expect(page.locator("#error-summary")).toContainText("not right");
  }
  // Even the right password is refused while the limit holds.
  await signIn(page, admin, password);
  await expect(page.locator("#error-summary")).toContainText("Too many failed sign-ins");
  await expect(page.locator("#error-summary")).toBeFocused();
  await expect(page).toHaveURL("/signin");
  await checkPage(page);
});

// SPEC 17.1 and DESIGN 10: the skip link is the first tab stop and moves focus to main.
test("the skip link is the first tab stop", async ({ page }) => {
  for (const path of ["/setup", "/signed-out"]) {
    await page.goto(path);
    await page.keyboard.press("Tab");
    const skip = page.getByRole("link", { name: "Skip to content" });
    await expect(skip).toBeFocused();
    const box = await skip.boundingBox();
    expect(box && Math.min(box.width, box.height), "focused skip link size").toBeGreaterThanOrEqual(24);
    await page.keyboard.press("Enter");
    await expect(page.locator("main")).toBeFocused();
  }
});

// SPEC 17.2 and DESIGN 7: the error summary's links stand alone in a list,
// so they are full-size touch targets, not inline text.
test("error summary links are 44 px tall", async ({ page }) => {
  await page.goto("/setup");
  await page.getByRole("button", { name: "Create admin account" }).click();
  const links = await page.locator("#error-summary a").all();
  expect(links.length).toBeGreaterThan(0);
  for (const link of links) {
    const box = await link.boundingBox();
    expect(box && box.height, await link.innerText()).toBeGreaterThanOrEqual(44);
  }
});
