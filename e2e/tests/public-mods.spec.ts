import { test, expect, checkPage, setUpModsWorld, mainNav, version20, live } from "./foreman.js";
import type { Browser, Page } from "@playwright/test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import type { Foreman } from "./foreman.js";

// SPEC 9.16 and 14.18: the public mods page and its setting.
test.use({ fakeGameOn: true, fakePortalOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
// chest-auto-sort 1.3.0 is deprecated on the fake portal; my-own-mod is not on it.
const mods = {
  "chest-auto-sort": { factorio: "2.0", version: "1.3.0", deps: ["base"] },
  "my-own-mod": { factorio: "2.0", version: "0.1.0", deps: ["base"] },
};

function audits(f: Foreman): string[] {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    const rows = db.prepare("SELECT username, detail FROM audit_log WHERE action = 'public-mods-set' ORDER BY id").all() as { username: string; detail: string }[];
    return rows.map((r) => `${r.username} ${r.detail}`);
  } finally {
    db.close();
  }
}

// Adds a mod installed as a folder, so the page asks players to get it from the admin.
function addFolderMod(f: Foreman) {
  const mods = join(f.dataDir, "worlds", "base", "mods");
  mkdirSync(join(mods, "folder-mod_2.0.0"), { recursive: true });
  writeFileSync(join(mods, "folder-mod_2.0.0", "info.json"), JSON.stringify({ name: "folder-mod", version: "2.0.0", title: "Folder mod", factorio_version: "2.0" }));
  const list = JSON.parse(readFileSync(join(mods, "mod-list.json"), "utf8"));
  list.mods.push({ name: "folder-mod", enabled: true });
  writeFileSync(join(mods, "mod-list.json"), JSON.stringify(list));
}

async function turnOn(page: Page) {
  // From the Server page: Settings, then Public mods, then the button.
  await page.goto("/");
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "Public mods" }).click();
  await expect(page).toHaveURL("/settings/public-mods");
  await expect(page.locator("#public-mods-state")).toContainText("The public mods page is off");
  await checkPage(page);
  await page.getByRole("button", { name: "Turn on the public mods page" }).click();
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Turned the public mods page on.");
  await expect(page.locator("#public-mods-state")).toContainText("The public mods page is on.");
  await checkPage(page);
}

// A browser with no sign-in, as a player has.
async function stranger(browser: Browser, baseURL: string | undefined) {
  const { viewport, colorScheme } = test.info().project.use;
  const ctx = await browser.newContext({ baseURL, viewport, colorScheme });
  return { ctx, page: await ctx.newPage() };
}

test("off by default, turned on in settings, then players download without signing in", async ({ page, foreman, fakeGame, browser, baseURL }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods, start: true });
  addFolderMod(foreman);
  const { ctx, page: player } = await stranger(browser, baseURL);
  try {
    expect((await player.request.get("/public/mods")).status()).toBe(404);

    await turnOn(page);
    expect(audits(foreman).map((a) => a.split(" ")[1])).toEqual(["on"]);

    await player.goto("/public/mods");
    await expect(player.getByRole("heading", { level: 1 })).toHaveText("Mods for this server");
    await expect(player.getByRole("heading", { level: 2, name: "Base" })).toBeVisible();
    await expect(player.locator("dl.facts")).toContainText("2.0.77");
    await expect(player.locator('meta[name="robots"]')).toHaveAttribute("content", "noindex");
    // The portal check may take a moment; the marks come with its answer.
    const own = player.locator(".public-mods > li", { hasText: "my-own-mod" });
    await expect(async () => {
      await player.reload();
      await expect(own).toContainText("Not on the mod portal", { timeout: 1000 });
    }).toPass(live);
    await expect(player.locator(".public-mods > li", { hasText: "chest-auto-sort" })).toContainText("Hidden on the mod portal");
    const folder = player.locator(".public-mods > li", { hasText: "Folder mod" });
    await expect(folder).toContainText("Ask the server admin for this mod");
    await expect(folder.getByRole("link")).toHaveCount(0);
    // The only link into Foreman is the product name.
    const hrefs = await player.locator("a[href]").evaluateAll((as) => as.map((a) => a.getAttribute("href")));
    expect(hrefs.filter((h) => h && !h.startsWith("/public/") && h !== "#main")).toEqual(["/"]);
    await checkPage(player);

    // One mod, by its link's name, and then all of them.
    const one = player.getByRole("link", { name: "my-own-mod 0.1.0" });
    const [file] = await Promise.all([player.waitForEvent("download"), one.click()]);
    expect(file.suggestedFilename()).toBe("my-own-mod_0.1.0.zip");
    const [all] = await Promise.all([player.waitForEvent("download"), player.getByRole("link", { name: "Download all" }).click()]);
    expect(all.suggestedFilename()).toMatch(/^Base-mods\.zip$/);
    const zip = readFileSync((await all.path())!);
    expect(zip.includes(Buffer.from("chest-auto-sort_1.3.0.zip"))).toBe(true);
    expect(zip.includes(Buffer.from("my-own-mod_0.1.0.zip"))).toBe(true);
    expect(zip.includes(Buffer.from("folder-mod"))).toBe(false);

    // The language menu works without a sign-in and comes back here.
    await player.locator("details.menu > summary").click();
    await player.locator("details.menu__langs > summary").click();
    await player.getByRole("button", { name: "Deutsch" }).click();
    await expect(player).toHaveURL("/public/mods");
    await expect(player.locator("html")).toHaveAttribute("lang", "de");
    await expect(player.getByRole("heading", { level: 1 })).toHaveText("Mods für diesen Server");
    const cookies = await ctx.cookies();
    expect(cookies.map((c) => c.name)).toEqual(["foreman_lang"]);

    // Turning it off is one click and takes effect at once.
    await page.getByRole("button", { name: "Turn off the public mods page" }).click();
    await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Turned the public mods page off.");
    expect((await player.request.get("/public/mods")).status()).toBe(404);
    expect(audits(foreman).map((a) => a.split(" ")[1])).toEqual(["on", "off"]);
  } finally {
    await ctx.close();
  }
});

test("with no game running the page says so and lists nothing", { tag: "@looks" }, async ({ page, foreman, fakeGame, browser, baseURL }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods });
  await turnOn(page);
  const { ctx, page: player } = await stranger(browser, baseURL);
  try {
    await player.goto("/public/mods");
    await expect(player.locator("#public-none")).toHaveText("No game is running now, so there are no mods to download. Come back when the server is up.");
    await expect(player.locator(".public-mods")).toHaveCount(0);
    await checkPage(player);
  } finally {
    await ctx.close();
  }
});
