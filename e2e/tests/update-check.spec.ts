import { test, expect, createAdmin } from "./foreman.js";

// SPEC 13.1, 14.10, DESIGN 6 (About): when GitHub reports a newer Foreman
// release, About names it with a link to its page; turning the switch off on
// the General tab hides it, and turning it on checks at once. Needs a build
// with a release version, which `make e2e` gives bin/foreman.

test.use({ fakePortalOn: true });

const updateSwitch = (page: import("@playwright/test").Page) => page.getByLabel("Check GitHub for new Foreman versions");

test("About shows a newer release, and the General switch hides it", async ({ page, fakePortal }) => {
  await fakePortal!.setGitHubTag("v99.0.0");
  await createAdmin(page);
  await page.goto("/settings/general");
  await expect(updateSwitch(page)).toBeChecked();
  // Off, then on: turning it on asks GitHub at once, rather than a minute after start.
  await updateSwitch(page).uncheck();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator("#done-message")).toBeVisible();
  await updateSwitch(page).check();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator("#done-message")).toBeVisible();

  const notice = page.locator("#about-newer");
  await expect(async () => {
    await page.goto("/about");
    await expect(notice).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 15_000 });
  await expect(notice).toContainText("Foreman 99.0.0 is available.");
  await expect(notice.getByRole("link", { name: /See what's new/ })).toHaveAttribute(
    "href",
    "https://github.com/toddawhittaker/foreman/releases/tag/v99.0.0",
  );

  await page.goto("/settings/general");
  await updateSwitch(page).uncheck();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page.locator("#done-message")).toBeVisible();
  await page.goto("/about");
  await expect(page.locator("#about-facts")).toBeVisible();
  await expect(notice).toHaveCount(0);
});
