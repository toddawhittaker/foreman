import { test, expect, checkPage, setUpModsWorld, openModTools } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 7.6, 14.6, 14.10: the General settings reach the pages they govern.
// The countdown default shows on the stop form, and the upload limits show in
// every drop zone's hint and in the zone's own data, which the script reads.
// A change applies to the next page load, with no restart. Every project runs
// these, so each holds at 320 px and at desktop width, in both themes.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const countdown = (page: Page) => page.getByLabel("Default stop countdown (seconds)");
const saveLimit = (page: Page) => page.getByLabel("Largest save upload (MB)");
const modLimit = (page: Page) => page.getByLabel("Largest mod upload (MB)");

async function saveGeneral(page: Page, values: { countdown?: string; save?: string; mod?: string }) {
  await page.goto("/settings/general");
  if (values.countdown !== undefined) await countdown(page).fill(values.countdown);
  if (values.save !== undefined) await saveLimit(page).fill(values.save);
  if (values.mod !== undefined) await modLimit(page).fill(values.mod);
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL("/settings/general?done=saved");
}

test("the stop and restart forms start from the countdown setting", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { start: true });
  await page.goto("/worlds/base/stop");
  await expect(page.locator("#countdown")).toHaveValue("60");
  await saveGeneral(page, { countdown: "25" });
  await page.goto("/worlds/base/stop");
  await expect(page.locator("#countdown")).toHaveValue("25");
  await checkPage(page);
  await page.goto("/worlds/base/restart");
  await expect(page.locator("#countdown")).toHaveValue("25");
  await saveGeneral(page, { countdown: "0" });
  await page.goto("/worlds/base/stop");
  await expect(page.locator("#countdown")).toHaveValue("0");
});

test("the Saves tab and the Import page say the save upload limit", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!);
  await page.goto("/worlds/base/saves");
  const zone = page.locator(".dropzone", { has: page.locator("#save-file") });
  await expect(zone.getByText("A .zip file of up to 1 GB.")).toBeVisible();

  await saveGeneral(page, { save: "500" });
  await page.goto("/worlds/base/saves");
  await expect(zone.getByText("A .zip file of up to 500 MB.")).toBeVisible();
  // The script reads the same figure from the zone, so its message agrees.
  await expect(page.locator("[data-dropzone]").first()).toHaveAttribute("data-limit", "500 MB");
  await expect(page.locator("[data-dropzone]").first()).toHaveAttribute("data-max-bytes", String(500 * 1024 * 1024));
  await checkPage(page);

  await page.goto("/import");
  const upload = page.locator("form", { has: page.getByRole("heading", { name: "Upload a save" }) });
  await expect(upload.getByText("A .zip file of up to 500 MB.")).toBeVisible();
  await checkPage(page);
});

test("the Mods tab says the mod upload limit and leaves the mod-settings.dat limit alone", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { mods: { alpha: "2.1" } });
  await saveGeneral(page, { mod: "50" });
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await page.locator("#mod-upload > summary").click();
  await page.locator("#mod-settings-replace > summary").click();
  const modZone = page.locator(".dropzone", { has: page.locator("#mod-file") });
  const settingsZone = page.locator(".dropzone", { has: page.locator("#settings-file") });
  await expect(modZone.getByText("A .zip file of up to 50 MB.")).toBeVisible();
  await expect(settingsZone.getByText("A .dat file of up to 4 MB.")).toBeVisible();
  await expect(modZone).toHaveAttribute("data-limit", "50 MB");
  await checkPage(page);

  await saveGeneral(page, { mod: "2048" });
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await page.locator("#mod-upload > summary").click();
  await expect(page.locator(".dropzone", { has: page.locator("#mod-file") }).getByText("A .zip file of up to 2 GB.")).toBeVisible();
});
