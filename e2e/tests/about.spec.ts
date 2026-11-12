import type { Page } from "@playwright/test";
import { test, expect, checkPage, createAdmin, image21, openMenu, seedWorld, pickLanguage } from "./foreman.js";

// DESIGN 6 ("User menu"), SPEC 17.1, 17.2, 22; epic fixes 7 ruling 7. The
// user menu's About item opens a modal dialog with the product, the build's
// version and commit, the Go version, the licence and the project's address.
// Without script the item is a link to /about with the same content.

const project = "https://github.com/toddawhittaker/foreman";
const menuButton = (page: Page) => page.locator("details.menu > summary");
const aboutItem = (page: Page) => page.locator("details.menu").getByRole("link", { name: "About" });
const dialog = (page: Page) => page.getByRole("dialog", { name: "About Foreman" });
const inDialog = (page: Page) => page.evaluate(() => Boolean(document.activeElement?.closest("#about-dialog")));

// Tabs until target has focus.
async function tabUntil(page: Page, target: ReturnType<Page["locator"]>, limit = 40) {
  for (let i = 0; i < limit && !(await target.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(target).toBeFocused();
}

// The facts a person reads: the label and value of each row.
async function facts(page: Page, root: ReturnType<Page["locator"]>) {
  const rows = await root.locator("dl.about__facts > div").evaluateAll((els) =>
    els.map((el) => [el.querySelector("dt")!.textContent!.trim(), el.querySelector("dd")!.textContent!.trim()]),
  );
  return Object.fromEntries(rows) as Record<string, string>;
}

test("About opens by keyboard, shows the build, and Escape closes it with focus returned", async ({ page }) => {
  await createAdmin(page);
  await menuButton(page).focus();
  await page.keyboard.press("Enter");
  await tabUntil(page, aboutItem(page));
  await page.keyboard.press("Enter");

  await expect(dialog(page)).toBeVisible();
  await expect(dialog(page).getByRole("heading", { name: "About Foreman" })).toBeVisible();
  // The browser moved focus in, to Close.
  await expect(dialog(page).getByRole("button", { name: "Close" })).toBeFocused();
  // The logo is decorative and the name stays English.
  await expect(dialog(page).locator("img")).toHaveAttribute("alt", "");

  const got = await facts(page, dialog(page));
  expect(Object.keys(got)).toEqual(expect.arrayContaining(["Version", "Go version", "Licence"]));
  expect(got["Version"]).toMatch(/\S/);
  expect(got["Go version"]).toMatch(/^go1\.\d+/);
  expect(got["Licence"]).toBe("MIT");
  // A build with a recorded commit shows its first twelve characters.
  if ("Commit" in got) expect(got["Commit"]).toMatch(/^[0-9a-f]{12}(-dirty)?$/);
  else expect(got["Version"]).toBe("development build");

  const link = dialog(page).getByRole("link", { name: /GitHub/ });
  await expect(link).toHaveAttribute("href", project);
  // The link says it opens in a new tab.
  await expect(link).toHaveAccessibleName("GitHub (opens in a new tab)");
  await expect(dialog(page)).not.toContainText(/game version/i);
  await checkPage(page);

  // Focus stays inside the dialog however long one tabs, both ways.
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Tab");
    expect(await inDialog(page), `Tab ${i + 1}`).toBe(true);
  }
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Shift+Tab");
    expect(await inDialog(page), `Shift+Tab ${i + 1}`).toBe(true);
  }

  await page.keyboard.press("Escape");
  await expect(dialog(page)).toBeHidden();
  // The menu closed as focus went into the dialog, so its button has focus.
  await expect(page.locator("details.menu")).not.toHaveAttribute("open", "");
  await expect(menuButton(page)).toBeFocused();
});

test("Close shuts the dialog by keyboard, and it opens again", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  await aboutItem(page).click();
  await expect(dialog(page)).toBeVisible();
  await page.keyboard.press("Space");
  await expect(dialog(page)).toBeHidden();
  await expect(menuButton(page)).toBeFocused();

  await page.keyboard.press("Enter");
  await tabUntil(page, aboutItem(page));
  await page.keyboard.press("Enter");
  await expect(dialog(page)).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(dialog(page)).toBeHidden();
  await expect(menuButton(page)).toBeFocused();
});

test("the dim area outside the box closes it", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  await aboutItem(page).click();
  await expect(dialog(page)).toBeVisible();
  await page.mouse.click(2, 2);
  await expect(dialog(page)).toBeHidden();
});

test("the About page has the same content, and the dialog is not repeated on it", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  await aboutItem(page).click();
  const inBox = await facts(page, dialog(page));
  await page.keyboard.press("Escape");
  await page.goto("/about");
  await expect(page.getByRole("heading", { level: 1, name: "About Foreman" })).toBeVisible();
  expect(await facts(page, page.locator("main"))).toEqual(inBox);
  await expect(page.locator("main").getByRole("link", { name: /GitHub/ })).toHaveAttribute("href", project);
  await expect(page.locator("dialog")).toHaveCount(0);
  await checkPage(page);
});

test("a signed-out visitor is sent to sign in, not shown the build", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  await page.getByRole("button", { name: "Sign out" }).click();
  await page.goto("/about");
  await expect(page).not.toHaveURL(/\/about$/);
  await expect(page.getByText(/^go1\./)).toHaveCount(0);
});

test("the words follow the language, the names do not", async ({ page }) => {
  await createAdmin(page);
  await pickLanguage(page, "de");
  await page.goto("/about");
  await expect(page.getByRole("heading", { level: 1, name: "Über Foreman" })).toBeVisible();
  await expect(page.locator("dl.about__facts")).toContainText("Lizenz");
  await expect(page.locator("dl.about__facts")).toContainText("MIT");
});

// SPEC 17.2: at 320 px and 200% text the dialog and the page stay inside the window.
for (const lang of ["en", "de", "ru"]) {
  test(`at 320 px and 200% text in ${lang}, the dialog and the page fit`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await createAdmin(page);
    if (lang !== "en") await pickLanguage(page, lang);
    await page.addStyleTag({ content: "html { font-size: 200% }" });
    await openMenu(page);
    await page.locator("details.menu a[data-about]").click();
    const box = page.locator("#about-dialog");
    await expect(box).toBeVisible();
    const b = (await box.boundingBox())!;
    expect(b.x).toBeGreaterThanOrEqual(0);
    expect(b.x + b.width).toBeLessThanOrEqual(320);
    // The close button can be reached without a scroll bar inside the dialog.
    const scroll = await box.evaluate((el) => ({ w: el.scrollWidth, c: el.clientWidth }));
    expect(scroll.w).toBeLessThanOrEqual(scroll.c);
    await checkPage(page);
    await page.keyboard.press("Escape");
    await page.goto("/about");
    await checkPage(page);
  });
}

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("the menu's About item is a link to the About page", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/worlds");
    await page.locator("details.menu > summary").click();
    await aboutItem(page).click();
    await expect(page).toHaveURL("/about");
    await expect(page.getByRole("heading", { level: 1, name: "About Foreman" })).toBeVisible();
    const got = await facts(page, page.locator("main"));
    expect(got["Version"]).toMatch(/\S/);
    expect(got["Go version"]).toMatch(/^go1\.\d+/);
    expect(got["Licence"]).toBe("MIT");
    await expect(page.locator("main").getByRole("link", { name: /GitHub/ })).toHaveAttribute("href", project);
    // axe needs script, so the page's checks are in the tests above.
  });
});

// Generate asks before leaving a changed form; opening About leaves nothing, so it does not ask.
test.describe("on Generate with a changed form", () => {
  test.use({ fakeGameOn: true });

  test("About opens its dialog and Foreman's leave question does not appear", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/generate");
    await page.getByLabel("World name").fill("Half done");
    await openMenu(page);
    await aboutItem(page).click();
    await expect(dialog(page)).toBeVisible();
    await expect(page.getByRole("dialog", { name: "Leave without saving these map settings?" })).toBeHidden();
    await page.keyboard.press("Escape");
    await expect(page).toHaveURL(/\/generate$/);
    await expect(page.getByLabel("World name")).toHaveValue("Half done");
  });
});
