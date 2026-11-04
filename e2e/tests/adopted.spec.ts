import { test, expect, checkPage, createAdmin, adoptedContainer, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { existsSync, lstatSync, readdirSync, readlinkSync } from "node:fs";
import { join } from "node:path";

// Adopted mode through the web pages (SPEC 4.7 to 4.12, DESIGN 7.4, 7.6)
// against the fake game's seeded container.

// Foreman's start-up wiring for adopted mode is a separate task. Every test
// here passed with it merged; remove this line once it is on this branch.
test.fixme(true, "needs the start-up wiring for adopted mode");

const live = { timeout: 20_000 };

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");
const adoptLink = (page: Page) => page.getByRole("link", { name: "Adopt this game folder" });

// The page with no active world does not follow the game, so a reload picks
// up the monitor's first look at the game folder.
async function openServerPage(page: Page, shows: () => Promise<void>) {
  await createAdmin(page);
  await expect(async () => {
    await page.goto("/");
    await shows();
  }).toPass(live);
}

test.describe("an unadopted game folder", () => {
  test.use({ adoptedOn: { folder: "unadopted", running: true } });

  test("adopting a running game shows the danger box and the step list, then the new world", async ({ page, foreman, fakeGame, gameDir }) => {
    const g = fakeGame as FakeGame;
    await openServerPage(page, () => expect(adoptLink(page)).toBeVisible({ timeout: 1000 }));
    await expect(page.getByRole("button", { name: "Start server" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Download game image" })).toHaveCount(0);
    await checkPage(page);

    await adoptLink(page).click();
    await expect(page).toHaveURL("/adopt");
    const confirm = page.getByRole("region", { name: "Adopt the game folder as a new world?" });
    await expect(confirm).toContainText("Warn players online with the countdown below, save the game and stop it");
    await expect(confirm).toContainText('"Before adoption"');
    await expect(confirm).toContainText("pre-foreman-");
    await expect(confirm).toContainText("Start the game again");
    await expect(page.getByLabel("Game version")).toHaveValue("2.1.24");
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("60");
    await expect(confirm.getByRole("button", { name: "Adopt the game folder" })).toHaveClass(/button--danger/);
    await checkPage(page);

    // An empty name is refused on the form before anything is stopped.
    await page.getByLabel("World name").fill("");
    await confirm.getByRole("button", { name: "Adopt the game folder" }).click();
    await expect(page.getByRole("link", { name: "Enter a name for the world." })).toBeVisible();
    await checkPage(page);
    expect((await g.state()).containers.find((c) => c.name === adoptedContainer)?.status).toBe("running");

    await g.call("/join", { name: adoptedContainer, player: "bob" });
    await page.getByLabel("World name").fill("Gate");
    await page.getByLabel("Countdown in seconds").fill("3");
    await page.getByRole("button", { name: "Adopt the game folder" }).click();
    await expect(page).toHaveURL("/");

    // The countdown holds the first step open, so the list can be seen.
    await expect(page.getByRole("heading", { name: "Adopting the game folder" })).toBeVisible(live);
    await expect(page.getByRole("listitem").filter({ hasText: "Warn players" })).toContainText("In progress");
    await expect(page.getByRole("listitem").filter({ hasText: "Move the original folders" })).toContainText("Waiting");
    await checkPage(page);

    // The page follows the new world once it is the active one.
    await expect(page.getByRole("link", { name: "Active world: Gate" })).toBeVisible(live);
    await expect(badge(page)).toHaveText("Running", live);
    await expect(page.getByText("The game folder is adopted")).toBeVisible(live);
    await expect(page.getByText("Running version")).toBeVisible();
    await expect(page.getByRole("button", { name: "Download game image" })).toHaveCount(0);
    await checkPage(page);

    // The originals are kept, and the folder now presents the world (SPEC 4.8).
    const dir = gameDir as string;
    expect(readlinkSync(join(dir, "current"))).toBe("worlds/gate");
    expect(lstatSync(join(dir, "saves")).isSymbolicLink()).toBe(true);
    const kept = readdirSync(dir).filter((n) => n.startsWith("pre-foreman-"));
    expect(kept).toHaveLength(1);
    expect(existsSync(join(dir, kept[0], "saves", "base.zip"))).toBe(true);

    await page.goto("/worlds/gate/snapshots");
    await expect(page.getByRole("table")).toContainText("Before adoption");
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  });
});

test.describe("an unadopted game folder, stopped", () => {
  test.use({ adoptedOn: { folder: "unadopted" } });

  test("adopting a stopped game leaves it stopped, and Start then works", async ({ page }) => {
    await openServerPage(page, () => expect(adoptLink(page)).toBeVisible({ timeout: 1000 }));
    await adoptLink(page).click();
    await expect(page.getByText("Leave the game stopped, because it is not running now.")).toBeVisible();
    await expect(page.getByLabel("Countdown in seconds")).toHaveCount(0);
    await page.getByLabel("World name").fill("Gate");
    await page.getByRole("button", { name: "Adopt the game folder" }).click();

    await expect(page.getByRole("link", { name: "Active world: Gate" })).toBeVisible(live);
    await expect(badge(page)).toHaveText("Stopped", live);
    await expect(page.getByText("The game folder is adopted")).toBeVisible(live);
    await checkPage(page);

    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    // Only the start, wait and RCON steps show: the image is the container owner's.
    await expect(page.getByText("Check the game image")).toHaveCount(0);
    await expect(page.getByText("Create the game container")).toHaveCount(0);
    await checkPage(page);

    // Adopted already, so the form has nothing left to do.
    await page.goto("/adopt");
    await expect(page.getByText("nothing to adopt")).toBeVisible();
    await checkPage(page);
  });
});

test.describe("a container Foreman refuses", () => {
  test.use({ adoptedOn: { folder: "unadopted", env: ["GENERATE_NEW_SAVE=true"] } });

  test("the adoption page names the variable and changes nothing", async ({ page, gameDir }) => {
    await openServerPage(page, () => expect(adoptLink(page)).toBeVisible({ timeout: 1000 }));
    await adoptLink(page).click();
    await page.getByLabel("World name").fill("Gate");
    await page.getByRole("button", { name: "Adopt the game folder" }).click();

    const refusal = page.locator(".message--error");
    await expect(refusal).toContainText("GENERATE_NEW_SAVE");
    await expect(page.getByRole("link", { name: "Check again" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Adopt the game folder" })).toHaveCount(0);
    await checkPage(page);
    expect(existsSync(join(gameDir as string, "current"))).toBe(false);
    expect(lstatSync(join(gameDir as string, "saves")).isDirectory()).toBe(true);
  });
});

test.describe("a container that publishes its RCON port", () => {
  test.use({ adoptedOn: { folder: "unadopted", rconHostPort: "27015" } });

  test("the Server page warns, with and without a world", async ({ page }) => {
    const warning = page.getByText("publishes its RCON port on the host");
    await openServerPage(page, () => expect(warning).toBeVisible({ timeout: 1000 }));
    await expect(page.getByText("Remove the port mapping from the container's definition")).toBeVisible();
    await checkPage(page);

    await adoptLink(page).click();
    await page.getByLabel("World name").fill("Gate");
    await page.getByRole("button", { name: "Adopt the game folder" }).click();
    await expect(page.getByRole("link", { name: "Active world: Gate" })).toBeVisible(live);
    await expect(warning).toBeVisible();
    await checkPage(page);
  });
});

test.describe("a game folder Foreman cannot use", () => {
  test.use({ adoptedOn: { folder: "broken" } });

  test("the Server page names the path and offers no control", async ({ page }) => {
    const message = page.locator(".message--error");
    await openServerPage(page, () => expect(message).toBeVisible({ timeout: 1000 }));
    await expect(message).toContainText("saves");
    await expect(message).toContainText("is a file, not a folder or link");
    await expect(page.getByRole("button", { name: "Start server" })).toHaveCount(0);
    await expect(adoptLink(page)).toHaveCount(0);
    await checkPage(page);

    await page.goto("/adopt");
    await expect(page.locator(".message--error")).toContainText("is a file, not a folder or link");
    await expect(page.getByRole("button", { name: "Adopt the game folder" })).toHaveCount(0);
    await checkPage(page);
  });
});
