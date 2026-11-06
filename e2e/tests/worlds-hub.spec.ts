import { test, expect, checkPage, createAdmin, seedWorld, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, mkdirSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Switching from the Worlds list and the world pages, what a running switch
// shows there, and the delete refusal (SPEC 7.9, 17.7, 17.8).

test.use({ fakeGameOn: true });

const live = { timeout: 20_000 };
const fixtures = join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata");

// A world written as a folder (SPEC 5.1). seedWorld made "base".
function seedExtra(f: Foreman, id: string, name: string) {
  const dir = join(f.dataDir, "worlds", id);
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name, game_version: "2.1.24", space_age: false, created_at: "2026-11-02T10:00:00Z" }),
  );
  const save = join(dir, "saves", "base.zip");
  copyFileSync(join(fixtures, "2.1.24-no-space-age.zip"), save);
  const at = new Date(Date.now() - 3_600_000);
  utimesSync(save, at, at);
}

async function setUp(page: Page, f: Foreman, g: FakeGame) {
  await createAdmin(page);
  seedWorld(f);
  seedExtra(f, "second", "Second");
  seedExtra(f, "third", "Third");
  await g.call("/image", { ref: image21, on: true });
}

const row = (page: Page, name: string) => page.locator("ul.worlds > li").filter({ has: page.getByRole("heading", { name }) });

test("each world but the active one has a Switch button on the list", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/worlds");

  await expect(row(page, "Base").getByRole("link", { name: /Switch/ })).toHaveCount(0);
  // The screen shows "Switch" beside the world's name; a screen reader hears which world.
  const button = row(page, "Second").getByRole("link", { name: "Switch to Second", exact: true });
  await expect(button).toHaveClass(/button/);
  await expect(button.locator(".visually-hidden")).toHaveText("Switch to Second");
  await expect(row(page, "Third").getByRole("link", { name: "Switch to Third", exact: true })).toBeVisible();
  await expect(page.getByText("Switching is unavailable")).toHaveCount(0);
  await checkPage(page);

  await button.click();
  await expect(page).toHaveURL("/worlds/second/switch");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Switch to Second");
});

// DESIGN 7.1: one primary action per page, and the destructive one last.
test("on a world page Switch is the main action, Rename is plain and Delete comes last", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/worlds/second");

  await expect(page.getByRole("link", { name: "Switch to this world" })).toHaveClass(/button--primary/);
  await expect(page.getByRole("button", { name: "Rename world" })).not.toHaveClass(/button--primary/);
  await expect(page.locator("main .button--primary")).toHaveCount(1);
  const headings = await page.locator("main h2").allTextContents();
  expect(headings[0]).toBe("Switch");
  expect(headings.at(-1)).toBe("Delete");
  await checkPage(page);
});

test("a rename says Done", async ({ page, foreman, fakeGame }) => {
  await setUp(page, foreman, fakeGame!);
  await page.goto("/worlds/second");
  await expect(page.locator(".message--success")).toHaveCount(0);

  await page.getByLabel("Name", { exact: true }).fill("  Second base ");
  await page.getByRole("button", { name: "Rename world" }).click();
  await expect(page).toHaveURL(/\/worlds\/second\?renamed=/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Second base");
  await expect(page.locator(".message--success p")).toHaveText("Done: Renamed to Second base.");
  await checkPage(page);

  // A link that names something else is not believed.
  await page.goto("/worlds/second?renamed=Something+else");
  await expect(page.locator(".message--success")).toHaveCount(0);
});

test("a running switch is shown on both of its worlds and not on others", async ({ page, foreman, fakeGame }) => {
  const g = fakeGame!;
  await setUp(page, foreman, g);
  await page.goto("/");
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.getByRole("region", { name: "Status" }).locator(".state")).toHaveText("Running", live);
  await g.call("/join", { name: "foreman-base", player: "alice" });

  // The countdown holds the switch open on its first step.
  await page.goto("/worlds/second/switch");
  await page.getByLabel("Countdown in seconds").fill("30");
  await page.getByRole("button", { name: "Switch to Second" }).click();
  await expect(page.getByRole("heading", { name: "Switching from Base to Second" })).toBeVisible(live);

  const busy = "Switching from Base to Second. Now: Warn players.";
  await page.goto("/worlds");
  await expect(row(page, "Base")).toContainText(busy);
  await expect(row(page, "Second")).toContainText(busy);
  await expect(row(page, "Third")).not.toContainText("Switching");
  await expect(page.locator("ul.worlds").getByRole("link", { name: /^Switch to / })).toHaveCount(0);
  await expect(page.locator(".message--note")).toContainText("Switching is unavailable while an operation runs on the server");
  await checkPage(page);

  for (const id of ["base", "second"]) {
    await page.goto(`/worlds/${id}`);
    await expect(page.locator(".message--note")).toContainText(busy);
  }
  await page.goto("/worlds/third");
  await expect(page.getByText("Switching from Base to Second")).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Switch" })).toContainText("An operation is running on the server, such as a switch.");
  await expect(page.getByRole("link", { name: "Switch to this world" })).toHaveCount(0);
  // Third is not part of the switch, so it can still be deleted.
  await expect(page.getByRole("link", { name: "Delete world" })).toBeVisible();

  // The target cannot be deleted while the switch runs, and the page says
  // so before any name is typed, on the world page and the confirmation.
  const reason = /Switching from Base to Second\. Now: Warn players\. This world cannot be deleted until it ends\./;
  await page.goto("/worlds/second");
  await expect(page.getByRole("region", { name: "Delete" })).toContainText(reason);
  await expect(page.getByRole("link", { name: "Delete world" })).toHaveCount(0);
  await checkPage(page);
  await page.goto("/worlds/second/delete");
  await expect(page.getByText(reason)).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete world" })).toHaveCount(0);
  await expect(page.getByLabel("Type the world's name to confirm")).toHaveCount(0);
  await checkPage(page);
});
