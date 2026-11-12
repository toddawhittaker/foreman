import { test, expect, checkPage, createAdmin, openMenu, pickLanguage } from "./foreman.js";
import { countClicks, expectWithin } from "./clicks.js";

// SPEC 7.12, DESIGN 8.15: the restart schedule page, the Foreman settings
// sub-navigation it shares with the account and notifications pages, and
// the user menu's order. Every project runs these, so each holds at 320 px
// and at desktop width, in both themes.

const settingsNav = (page: import("@playwright/test").Page) => page.getByRole("navigation", { name: "Foreman settings" });

test("set a nightly restart from the Server page in three clicks", async ({ page }) => {
  await createAdmin(page);
  const clicks = await countClicks(page);
  await clicks.reset();

  await openMenu(page);
  await page.locator("details.menu").getByRole("link", { name: "Restart schedule" }).click();
  await expect(page).toHaveURL("/settings/restart-schedule");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Restart schedule");
  await expect(page.locator("#schedule-state")).toHaveText("No restart is scheduled.");
  // How it works comes before the form, as a numbered list.
  await expect(page.getByRole("heading", { name: "How it works" })).toBeVisible();
  await expect(page.locator("main ol > li")).toHaveCount(5);
  // A new schedule is offered ticked, at 04:00 UTC with a one-hour limit.
  await expect(page.getByLabel("Restart the active world every day")).toBeChecked();
  await expect(page.getByLabel("Time", { exact: true })).toHaveValue("04:00");
  await expect(page.getByLabel("Time zone")).toHaveValue("UTC");
  await expect(page.getByLabel("Wait for players to leave for up to")).toHaveValue("1");
  await checkPage(page);

  await page.getByRole("button", { name: "Save schedule" }).click();
  await expect(page).toHaveURL("/settings/restart-schedule?done=saved");
  await expect(page.locator("#done-message")).toContainText("Saved. The next restart is");
  await expect(page.locator("#schedule-state")).toContainText("Foreman restarts the active world every day at 04:00 (UTC). Next restart:");
  await expect(page.locator("#schedule-state time")).toHaveAttribute("datetime", /T04:00:00Z$/);
  await expectWithin(clicks, 3, "Set a nightly restart");
  await checkPage(page);
});

test("change the time, zone and limit, then turn it off", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/restart-schedule");
  await page.getByLabel("Time", { exact: true }).fill("02:30");
  await page.getByLabel("Time zone").selectOption("Europe/London");
  await page.getByLabel("Wait for players to leave for up to").fill("3");
  await page.getByRole("button", { name: "Save schedule" }).click();
  await expect(page.locator("#schedule-state")).toContainText("every day at 02:30 (Europe/London)");
  await expect(page.getByLabel("Time zone")).toHaveValue("Europe/London");
  await expect(page.getByLabel("Wait for players to leave for up to")).toHaveValue("3");

  // Turning it off keeps the fields, so turning it on again is one tick.
  await page.getByLabel("Restart the active world every day").uncheck();
  await page.getByRole("button", { name: "Save schedule" }).click();
  await expect(page.locator("#done-message")).toContainText("Saved. No restart is scheduled.");
  await expect(page.locator("#schedule-state")).toHaveText("No restart is scheduled.");
  await expect(page.getByLabel("Restart the active world every day")).not.toBeChecked();
  await expect(page.getByLabel("Time", { exact: true })).toHaveValue("02:30");
  await expect(page.getByLabel("Time zone")).toHaveValue("Europe/London");
  await checkPage(page);
});

test("a wrong limit is refused with its error, and nothing is saved", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/restart-schedule");
  await page.getByLabel("Wait for players to leave for up to").fill("13");
  await page.getByRole("button", { name: "Save schedule" }).click();
  const summary = page.locator("#error-summary");
  await expect(summary).toBeFocused();
  await expect(summary.getByRole("link")).toHaveText(["Enter a whole number of hours from 0 to 12."]);
  const limit = page.getByLabel("Wait for players to leave for up to");
  await expect(limit).toHaveAttribute("aria-invalid", "true");
  await expect(limit).toHaveValue("13");
  await expect(page.locator("#schedule-limit-error")).toContainText("Enter a whole number of hours from 0 to 12.");
  await checkPage(page);
  // The summary's link takes focus to the field.
  await summary.getByRole("link").click();
  await expect(limit).toBeFocused();

  await page.goto("/settings/restart-schedule");
  await expect(page.locator("#schedule-state")).toHaveText("No restart is scheduled.");
});

test("the three Foreman settings pages share one sub-navigation", async ({ page }) => {
  await createAdmin(page);
  for (const [path, current] of [
    ["/settings/factorio-account", "factorio.com account"],
    ["/settings/restart-schedule", "Restart schedule"],
    ["/settings/notifications", "Notifications"],
  ]) {
    await page.goto(path);
    const nav = settingsNav(page);
    await expect(nav.getByRole("link")).toHaveText(["factorio.com account", "Restart schedule", "Notifications"]);
    await expect(nav.getByRole("link", { name: current })).toHaveAttribute("aria-current", "page");
    await expect(nav.locator("[aria-current]")).toHaveCount(1);
    // It sits under the page's heading.
    await expect(page.locator("main h1 + nav.subnav")).toHaveCount(1);
  }
  // Moving between them is one click.
  await settingsNav(page).getByRole("link", { name: "Restart schedule" }).click();
  await expect(page).toHaveURL("/settings/restart-schedule");
  await checkPage(page);
});

test("the user menu lists the settings pages in the sub-navigation's order", async ({ page }) => {
  await createAdmin(page);
  await openMenu(page);
  const links = page.locator("details.menu .menu__panel > a.menu__item");
  await expect(links).toHaveText(["factorio.com account", "Restart schedule", "Notifications"]);
});

test("the page is translated", async ({ page }) => {
  await createAdmin(page);
  await pickLanguage(page, "de");
  await page.goto("/settings/restart-schedule");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Neustart-Zeitplan");
  await expect(page.getByRole("navigation", { name: "Foreman-Einstellungen" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Zeitplan speichern" })).toBeVisible();
});

// DESIGN 8.15: a never-saved schedule preselects the viewer's own zone; a
// saved one keeps its zone; without script the page offers UTC.
test.describe("in the viewer's time zone", () => {
  test.use({ timezoneId: "America/New_York" });

  test("a never-saved schedule preselects the browser's zone", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/settings/restart-schedule");
    await expect(page.getByLabel("Time zone")).toHaveValue("America/New_York");
    await page.getByRole("button", { name: "Save schedule" }).click();
    await expect(page.locator("#schedule-state")).toContainText("every day at 04:00 (America/New_York)");
  });

  test("a saved schedule keeps its saved zone", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/settings/restart-schedule");
    await page.getByLabel("Time zone").selectOption("Europe/London");
    await page.getByRole("button", { name: "Save schedule" }).click();
    await expect(page.locator("#schedule-state")).toContainText("(Europe/London)");
    await page.goto("/settings/restart-schedule");
    await expect(page.getByLabel("Time zone")).toHaveValue("Europe/London");
  });

  test("a refused form keeps the zone that was posted", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/settings/restart-schedule");
    await page.getByLabel("Time zone").selectOption("Asia/Tokyo");
    await page.getByLabel("Wait for players to leave for up to").fill("13");
    await page.getByRole("button", { name: "Save schedule" }).click();
    await expect(page.locator("#error-summary")).toBeVisible();
    await expect(page.getByLabel("Time zone")).toHaveValue("Asia/Tokyo");
  });

  test.describe("without script", () => {
    test.use({ javaScriptEnabled: false });

    test("the zone select offers UTC and the form saves", async ({ page }) => {
      await createAdmin(page);
      await page.goto("/settings/restart-schedule");
      await expect(page.getByLabel("Time zone")).toHaveValue("UTC");
      await page.getByRole("button", { name: "Save schedule" }).click();
      await expect(page.locator("#schedule-state")).toContainText("every day at 04:00 (UTC)");
    });
  });
});
