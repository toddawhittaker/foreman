import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live, makeZip, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.5 (upload a mod zip) and 9.6 (mod-settings.dat). Neither needs a
// factorio.com account, so none is set; the fake portal is on only so the Mods
// tab's status block has a portal to ask.
test.use({ fakeGameOn: true, fakePortalOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const modsFile = (dataDir: string, file: string) => join(dataDir, "worlds", "base", "mods", file);
const modsTable = (page: Page) => page.getByRole("table", { name: /Mods of Base/ });

const upZip = makeZip({ "up-mod_2.3.4/info.json": JSON.stringify({ name: "up-mod", version: "2.3.4", factorio_version: "2.0", dependencies: ["base >= 2.0"] }) });

test.describe("upload a mod zip", () => {
  test("the zip, then its plan, then installed, on a running game", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUpModsWorld(page, foreman, g, { ...world, start: true });
    const before = await creates(g);
    await page.goto("/worlds/base/mods");
    await page.locator("#mod-file").setInputFiles({ name: "up-mod_2.3.4.zip", mimeType: "application/zip", buffer: upZip });
    await page.getByRole("button", { name: "Upload mod", exact: true }).click();

    await expect(page.getByRole("heading", { level: 1, name: "Install the uploaded up-mod 2.3.4 in Base?" })).toBeVisible();
    await expect(page.locator("#mod-plan")).toContainText("up-mod 2.3.4, from the uploaded zip");
    // Nothing is downloaded, so there is no download line, and nothing is in the world yet.
    await expect(page.locator("#mod-plan")).not.toContainText("from the mod portal");
    await expect(page.locator("#countdown")).toBeVisible();
    await checkPage(page);
    await expect(async () => expect(readFileSync(modsFile(foreman.dataDir, "mod-list.json"), "utf8")).not.toContain("up-mod")).toPass();

    await page.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    await expect(stateBadge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    expect(readFileSync(modsFile(foreman.dataDir, "up-mod_2.3.4.zip")).equals(upZip)).toBe(true);

    await page.goto("/worlds/base/mods");
    const row = modsTable(page).getByRole("row", { name: /up-mod/ });
    await expect(row).toContainText("2.3.4");
    await expect(row).toContainText("Enabled");
  });

  test("a file that is not a mod zip is refused on the Mods tab, with the error summary", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods");
    await page.locator("#mod-file").setInputFiles({ name: "junk.zip", mimeType: "application/zip", buffer: Buffer.from("not a zip") });
    await page.getByRole("button", { name: "Upload mod", exact: true }).click();
    await expect(page.locator("#error-summary")).toBeFocused();
    await expect(page.locator("#mod-file-error")).toContainText("This file is not a mod zip");
    await expect(page.locator("#mod-file")).toHaveAttribute("aria-invalid", "true");
    await checkPage(page);
  });
});

test.describe("mod-settings.dat", () => {
  const old = Buffer.from("OLD-SETTINGS-\x00\x01\x02");
  const fresh = Buffer.from("NEW-SETTINGS-\x00\x03\x04\x05");

  test("a world with none says so and offers no download", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods");
    await expect(page.getByRole("link", { name: "Download mod-settings.dat" })).toHaveCount(0);
    await expect(page.locator("#settings-heading + p + p, #settings-heading ~ p.empty")).toContainText("has no mod-settings.dat yet");
    await checkPage(page);
  });

  test("download gives the world's file", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, world);
    writeFileSync(modsFile(foreman.dataDir, "mod-settings.dat"), old);
    await page.goto("/worlds/base/mods");
    await checkPage(page);
    const download = page.waitForEvent("download");
    await page.getByRole("link", { name: "Download mod-settings.dat" }).click();
    const d = await download;
    expect(d.suggestedFilename()).toBe("mod-settings.dat");
    expect(readFileSync((await d.path())!).equals(old)).toBe(true);
  });

  test("upload while the game runs: the countdown is offered, the file is replaced, the game restarts", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUpModsWorld(page, foreman, g, { ...world, start: true });
    writeFileSync(modsFile(foreman.dataDir, "mod-settings.dat"), old);
    const before = await creates(g);
    await page.goto("/worlds/base/mods");
    const form = page.locator("form", { has: page.locator("#settings-file") });
    await expect(form.locator("#countdown")).toBeVisible();
    await expect(form).toContainText("Warn the players, then save and stop the server.");
    await checkPage(page);

    await page.locator("#settings-file").setInputFiles({ name: "mod-settings.dat", mimeType: "application/octet-stream", buffer: fresh });
    await page.getByRole("button", { name: "Upload mod settings" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    await expect(stateBadge(page)).toHaveText("Running", live);
    expect(await creates(g)).toBe(before + 1);
    expect(readFileSync(modsFile(foreman.dataDir, "mod-settings.dat")).equals(fresh)).toBe(true);
    // The undo offered is the snapshot from before the change.
    await expect(page.getByRole("link", { name: "Undo this mod change" })).toBeVisible();
  });

  test("an empty file is refused with a field error and changes nothing", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, world);
    writeFileSync(modsFile(foreman.dataDir, "mod-settings.dat"), old);
    await page.goto("/worlds/base/mods");
    await page.locator("#settings-file").setInputFiles({ name: "mod-settings.dat", mimeType: "application/octet-stream", buffer: Buffer.alloc(0) });
    await page.getByRole("button", { name: "Upload mod settings" }).click();
    await expect(page.locator("#error-summary")).toBeFocused();
    await expect(page.locator("#settings-file-error")).toContainText("This file is empty.");
    await checkPage(page);
    expect(readFileSync(modsFile(foreman.dataDir, "mod-settings.dat")).equals(old)).toBe(true);
  });
});
