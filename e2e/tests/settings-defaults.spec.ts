import { test, expect, checkPage, createAdmin, seedWorld, pickLanguage, type Foreman } from "./foreman.js";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { Page } from "@playwright/test";

// Game settings defaults (SPEC 11.1, 17): every unset field shows the value
// that applies, in its placeholder and its hint, and the form offers the
// Players and access, Gameplay, Saving and Network groups.

const settingsFile = (dir: string) => join(dir, "worlds", "base", "config", "server-settings.json");
const read = (dir: string) => JSON.parse(readFileSync(settingsFile(dir), "utf8"));
test.use({ fakeGameOn: true });

// The keys the game refuses to start without, and nothing else: an older world's file.
const bare = { name: "Base", description: "", visibility: { public: false, lan: false } };

async function setUp(page: Page, f: Foreman, version = "2.1.24") {
  await createAdmin(page);
  seedWorld(f, { version });
  writeFileSync(settingsFile(f.dataDir), JSON.stringify(bare, null, 2));
  await page.goto("/worlds/base/settings");
}

test("every unset field shows the game's default, and an untouched form changes nothing", async ({ page, foreman }) => {
  await setUp(page, foreman);
  for (const [label, value] of [
    ["Maximum players", "0"],
    ["Kick idle players after this many minutes", "0"],
    ["Autosave interval in minutes", "10"],
    ["Autosave slots", "5"],
    ["Upload limit in KB per second", "0"],
    ["Upload slots", "5"],
  ] as const) {
    const field = page.getByLabel(label);
    await expect(field).toHaveValue("");
    await expect(field).toHaveAttribute("placeholder", value);
  }
  await expect(page.locator("#autosave-slots-hint")).toContainText("Game default: 5");
  await expect(page.locator("#autosave-slots-hint")).toContainText("Foreman gives new worlds 20 slots.");
  await expect(page.getByLabel("Require user verification")).toBeChecked();
  await expect(page.getByLabel("Only admins can pause the game")).toBeChecked();
  await expect(page.getByLabel("Pause when no one is online")).toBeChecked();
  await expect(page.getByLabel("Pause the game while a player is joining")).not.toBeChecked();
  await expect(page.getByLabel("Save without pausing the game")).not.toBeChecked();
  await expect(page.getByLabel("Who can use console commands")).toHaveValue("admins-only");
  await expect(page.locator("#settings-gameplay")).toContainText("Game default: on");
  await expect(page.locator("#settings-gameplay")).toContainText("Game default: off");
  await checkPage(page);

  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByText("Nothing changed.")).toBeVisible();
});

test("a new world's form shows Foreman's 20 autosave slots", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/settings");
  await expect(page.getByLabel("Autosave slots")).toHaveValue("20");
});

test("each new field writes its own key, and the Network group opens when it holds a value", async ({ page, foreman }) => {
  await setUp(page, foreman);
  const network = page.locator("#settings-network details");
  await expect(network).not.toHaveAttribute("open");
  await page.getByLabel("Returning players can join when the server is full").check();
  await page.getByLabel("Kick idle players after this many minutes").fill("15");
  await page.getByLabel("Pause the game while a player is joining").check();
  await page.getByLabel("Only admins can pause the game").uncheck();
  await page.getByLabel("Who can use console commands").selectOption("false");
  await page.getByLabel("Save without pausing the game").check();
  await network.locator("summary").click();
  await page.getByLabel("Upload limit in KB per second").fill("512");
  await page.getByLabel("Upload slots").fill("3");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL(/\/worlds\/base\/settings\?saved=1/);
  expect(read(foreman.dataDir)).toMatchObject({
    ignore_player_limit_for_returning_players: true,
    afk_autokick_interval: 15,
    auto_pause_when_players_connect: true,
    only_admins_can_pause_the_game: false,
    allow_commands: false,
    non_blocking_saving: true,
    max_upload_in_kilobytes_per_second: 512,
    max_upload_slots: 3,
  });
  await expect(page.locator("#settings-network details")).toHaveAttribute("open", "");
  await expect(page.getByLabel("Upload slots")).toHaveValue("3");
  await checkPage(page);
});

test("fork saving carries its note, and the pause-while-joining setting is hidden on 1.1", async ({ page, foreman }) => {
  await setUp(page, foreman, "1.1.110");
  await expect(page.getByLabel("Pause the game while a player is joining")).toHaveCount(0);
  const note = page.locator("#non-blocking-saving-note");
  await expect(note).toContainText("highly experimental");
  await expect(note).toContainText("close to double");
  await expect(page.getByLabel("Save without pausing the game")).toBeEnabled();
  await checkPage(page);
});

// Below the rail's width the jump disclosure holds plain links, so the
// sections are reachable without script.
test.describe("without script", () => {
  test.use({ javaScriptEnabled: false, viewport: { width: 320, height: 640 } });

  test("the jump links reach the new sections", async ({ page, foreman }) => {
    await setUp(page, foreman);
    const jump = page.locator("details.settings-jump");
    await jump.locator("summary").click();
    await expect(jump.getByRole("link")).toHaveText(["Listing", "Players and access", "Gameplay", "Saving", "Network", "Admins", "Whitelist", "Ban list"]);
    await jump.getByRole("link", { name: "Network" }).click();
    await expect(page).toHaveURL(/#settings-network$/);
  });
});

for (const lang of ["de", "ru"]) {
  test.describe(`${lang} at 320 px and 200% text`, () => {
    test.use({ viewport: { width: 320, height: 640 } });

    test("the form fits and passes axe", async ({ page, foreman }) => {
      await setUp(page, foreman);
      await pickLanguage(page, lang);
      await page.goto("/worlds/base/settings");
      await expect(page.locator("html")).toHaveAttribute("lang", lang);
      await page.evaluate(() => {
        document.documentElement.style.fontSize = "200%";
      });
      await page.locator("#settings-network details summary").click();
      await checkPage(page);
    });
  });
}
