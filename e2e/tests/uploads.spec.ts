import { test, expect, checkPage, createAdmin, setUpModsWorld, makeZip, openModTools, live, dropFile } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readdirSync, rmSync } from "node:fs";
import { join } from "node:path";

// DESIGN 7.10: every upload uses the drop zone, and a file another tab owns
// is reached by a button, never a link. The zone's own behavior is tried in
// dropzone.spec.ts; the server's checks (SPEC 14.6) in the page specs.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const save = "2.1.24-space-age-testmod.zip";
const testModZip = () =>
  makeZip({ "foreman-test_0.1.0/info.json": JSON.stringify({ name: "foreman-test", version: "0.1.0", factorio_version: "2.1", dependencies: ["base"] }) });

const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/uploads-${name}-${test.info().project.name}.png`, fullPage: true });

test("the Saves tab upload is a drop zone that uploads the dropped save", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!);
  await page.goto("/worlds/base/saves");
  const zone = page.locator(".dropzone", { has: page.locator("#save-file") });
  await expect(zone).toHaveClass(/dropzone--on/);
  await expect(zone.getByText("A save never replaces one with the same name.")).toHaveCount(0);
  await expect(page.getByText("A save never replaces one with the same name.")).toBeVisible();
  await expect(zone.getByText("A .zip file of up to 1 GB.")).toBeVisible();
  await checkPage(page);
  await shot(page, "saves");

  // A wrong type is refused in the zone, before uploading.
  await dropFile(page, "save-file", [{ name: "notes.txt", body: "hello" }]);
  await expect(page.locator("#save-file-client-error")).toContainText("notes.txt is not a .zip file");
  await expect(page.locator("#save-file")).toHaveAttribute("aria-invalid", "true");
  await checkPage(page);

  await dropFile(page, "save-file", [{ name: "dropped.zip", body: makeZip({ "level/level.dat0": "from elsewhere" }) }]);
  await expect(zone.locator(".dropzone__file")).toContainText("Chosen: dropped.zip");
  await page.getByRole("button", { name: "Upload save" }).click();
  await expect(page.getByText("Done: Uploaded dropped.zip.")).toBeVisible();
  expect(readdirSync(join(foreman.dataDir, "worlds", "base", "saves")).sort()).toEqual(["base.zip", "dropped.zip"]);
});

test("a world with no save offers the Saves tab as a button from the Mods tab", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!);
  rmSync(join(foreman.dataDir, "worlds", "base", "saves", "base.zip"));
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await expect(page.getByText("This world has no save yet, so there is nothing to match.")).toBeVisible();
  const button = page.locator("#act-upload-save");
  await expect(button).toHaveText("Upload a save");
  await expect(button).toHaveClass(/button/);
  // On a phone every button is a 44 px target.
  if ((page.viewportSize()?.width ?? 1280) < 600) expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await checkPage(page);
  await shot(page, "sync");
  await button.click();
  await expect(page).toHaveURL("/worlds/base/saves");
  await expect(page.locator("#save-file")).toBeVisible();
});

test("the empty Worlds list offers the upload as a button to the Import page", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/worlds");
  const button = page.locator("#act-upload-save");
  await expect(button).toHaveText("Upload a save");
  await expect(button).toHaveClass(/button/);
  if ((page.viewportSize()?.width ?? 1280) < 600) expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await checkPage(page);
  await button.click();
  await expect(page).toHaveURL("/import#upload-heading");
  await expect(page.locator("#upload-source")).toBeVisible();
});

test("the Mods tab's mod zip and mod-settings.dat uploads are drop zones", async ({ page, foreman, fakeGame }) => {
  // The settings upload is offered once the world has a mod of its own.
  await setUpModsWorld(page, foreman, fakeGame!, { mods: { alpha: "2.1" } });
  await page.goto("/worlds/base/mods");
  await openModTools(page);
  await page.locator("#mod-upload > summary").click();
  const modZone = page.locator(".dropzone", { has: page.locator("#mod-file") });
  await expect(modZone).toHaveClass(/dropzone--on/);
  await expect(modZone.getByText(/A \.zip file of up to/)).toBeVisible();
  await page.locator("#mod-settings-replace > summary").click();
  const settingsZone = page.locator(".dropzone", { has: page.locator("#settings-file") });
  // One figure everywhere: the hint says what the server says, in whole binary units.
  await expect(modZone.getByText("A .zip file of up to 200 MB.")).toBeVisible();
  await expect(settingsZone.getByText("A .dat file of up to 4 MB.")).toBeVisible();
  await expect(settingsZone.locator("label code")).toHaveText("mod-settings.dat");
  // The help paragraph above the field still describes it, after the limits hint.
  await expect(page.getByLabel("New mod-settings.dat")).toHaveAccessibleDescription(/A \.dat file of up to 4 MB\..*Your Factorio client keeps it in its mods folder/);
  await checkPage(page);
  await shot(page, "mods");

  // The settings zone takes only a .dat; a zip is refused in place.
  await dropFile(page, "settings-file", [{ name: "mods.zip", body: "x" }]);
  await expect(page.locator("#settings-file-client-error")).toContainText("mods.zip is not a .dat file");
  await dropFile(page, "settings-file", [{ name: "big.dat", body: Buffer.alloc(4 * 2 ** 20 + 1) }]);
  await expect(page.locator("#settings-file-client-error")).toContainText("big.dat is larger than 4 MB");
  await dropFile(page, "settings-file", [{ name: "mod-settings.dat", body: "\x02\x00settings" }]);
  await expect(settingsZone.locator(".dropzone__file")).toContainText("Chosen: mod-settings.dat");
  await expect(page.locator("#settings-file-client-error")).toBeHidden();

  // A dropped mod zip opens its plan, as the form always did.
  await dropFile(page, "mod-file", [{ name: "foreman-test_0.1.0.zip", body: testModZip() }]);
  await expect(modZone.locator(".dropzone__file")).toContainText("Chosen: foreman-test_0.1.0.zip");
  await page.locator("#mod-upload").getByRole("button", { name: "Upload mod" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Install the uploaded foreman-test 0.1.0 in Base?" })).toBeVisible();
});

test.describe("an unresolved mod's zip on the review page", () => {
  const syncPlan = "/worlds/base/mods/plan?kind=sync";

  test("is a drop zone, and still one after the plan is redrawn", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
    await page.goto(syncPlan);
    const zone = page.locator(".dropzone", { has: page.locator("#mod-foreman-test-zip") });
    await expect(zone).toHaveClass(/dropzone--on/);
    // Away and back: the redraw brings a new zone, which the script takes over again.
    await page.getByLabel("Continue without foreman-test").check();
    await expect(zone).toHaveCount(0, live);
    await page.getByLabel("Upload the zip of foreman-test 0.1.0").check();
    await expect(zone).toHaveClass(/dropzone--on/, live);
    await checkPage(page);
    await shot(page, "plan");

    await dropFile(page, "mod-foreman-test-zip", [{ name: "foreman-test_0.1.0.zip", body: testModZip() }]);
    await expect(zone.locator(".dropzone__file")).toContainText("Chosen: foreman-test_0.1.0.zip");
    await page.getByRole("button", { name: "Upload zip" }).click();
    await expect(page.getByLabel("Use the uploaded zip of foreman-test 0.1.0")).toBeChecked();
  });

  test("refuses a wrong type in the zone, in the mod's own group", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
    await page.goto(syncPlan);
    await dropFile(page, "mod-foreman-test-zip", [{ name: "readme.txt", body: "x" }]);
    await expect(page.locator("#mod-foreman-test-zip-client-error")).toContainText("readme.txt is not a .zip file");
    await expect(page.getByRole("group", { name: "The mod portal does not have foreman-test 0.1.0" }).locator("#mod-foreman-test-zip-client-error")).toBeVisible();
  });
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("each upload is the plain labelled file field and posts as before", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
    await page.goto("/worlds/base/mods/plan?kind=sync");
    await expect(page.locator(".dropzone")).not.toHaveClass(/dropzone--on/);
    await page.getByLabel("Zip of foreman-test 0.1.0", { exact: true }).setInputFiles({ name: "foreman-test_0.1.0.zip", mimeType: "application/zip", buffer: testModZip() });
    await page.getByRole("button", { name: "Upload zip" }).click();
    await expect(page.getByLabel("Use the uploaded zip of foreman-test 0.1.0")).toBeChecked();

    await page.goto("/worlds/base/saves");
    await expect(page.locator(".dropzone")).not.toHaveClass(/dropzone--on/);
    await page.getByLabel("Save file").setInputFiles({ name: "plain.zip", mimeType: "application/zip", buffer: makeZip({ "level/level.dat0": "x" }) });
    await page.getByRole("button", { name: "Upload save" }).click();
    await expect(page.getByText("Done: Uploaded plain.zip.")).toBeVisible();
  });
});
