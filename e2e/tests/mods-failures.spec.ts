import { test, expect, checkPage, setUpModsWorld, doneBox, stateBadge, creates, live, makeZip, version20, type FakeGame, type FakePortal, type Foreman } from "./foreman.js";
import { existsSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.2, 9.11, 9.12, 17.8, 17.5: what a mod change does when something is
// wrong: the plan moves, there is no account, a download fails. In every case
// nothing is changed and the error is said in words.
test.use({ fakeGameOn: true, fakePortalOn: true });

const world = { version: version20, fixture: "2.0.77-no-space-age.zip" };
const modsDir = (f: Foreman) => join(f.dataDir, "worlds", "base", "mods");

test.describe("the plan changes between the page and the post", () => {
  test.use({ portalAccountOn: true });

  test("the mods folder changes: the plan changed, nothing is applied, and the new plan can be confirmed", async ({ page, foreman, fakeGame, fakePortal }) => {
    await fakePortal!.add("fp-late", "1.0.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-late");
    await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
    const listing = readdirSync(modsDir(foreman)).sort();

    // Someone puts another mod into the folder after the page opened.
    const info = JSON.stringify({ name: "fp-else", version: "1.0.0", factorio_version: "2.0" });
    writeFileSync(join(modsDir(foreman), "fp-else_1.0.0.zip"), makeZip({ "fp-else_1.0.0/info.json": info }));
    await page.getByRole("button", { name: "Install 1 mod" }).click();

    const changed = page.locator("#plan-changed");
    await expect(changed).toContainText("The plan changed since you saw it");
    await expect(changed).toContainText("Nothing was changed.");
    await expect(page).toHaveURL("/worlds/base/mods/plan");
    expect(existsSync(join(modsDir(foreman), "fp-late_1.0.0.zip"))).toBe(false);
    expect(readdirSync(modsDir(foreman)).sort()).toEqual([...listing, "fp-else_1.0.0.zip"].sort());
    expect(await fakePortal!.count("dl-mod.factorio.com", "/cdn/")).toBe(0);
    await checkPage(page);

    // The page shows the new plan, and confirming that one applies it.
    await page.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    expect(existsSync(join(modsDir(foreman), "fp-late_1.0.0.zip"))).toBe(true);
  });

  test("another install lands in between: the plan changed, nothing is applied", async ({ page, foreman, fakeGame, fakePortal }) => {
    const portal = fakePortal!;
    await portal.add("fp-late", "1.0.0", ["base >= 2.0"]);
    await portal.add("fp-first", "1.0.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, fakeGame!, world);
    await page.goto("/worlds/base/mods/plan?kind=install&name=fp-late");
    await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();

    // A second window installs another mod first.
    const other = await page.context().newPage();
    await other.goto(`${foreman.url}/worlds/base/mods/plan?kind=install&name=fp-first`);
    await other.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(doneBox(other)).toContainText("The mods of Base are changed.", live);
    await other.close();

    await page.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(page.locator("#plan-changed")).toContainText("Nothing was changed.");
    expect(existsSync(join(modsDir(foreman), "fp-late_1.0.0.zip"))).toBe(false);
    expect(existsSync(join(modsDir(foreman), "fp-first_1.0.0.zip"))).toBe(true);
  });
});

test.describe("no factorio.com account", () => {
  const key = "keykeykeykeykeykeykeykeykeykeykey";
  test.use({ foremanEnv: { FOREMAN_SECRET_KEY: key } });

  test("the plan says downloads are off and links to the account page, which returns to the plan", async ({ page, foreman, fakeGame, fakePortal }) => {
    const portal = fakePortal!;
    await portal.add("fp-nodl", "1.0.0", ["base >= 2.0"]);
    await setUpModsWorld(page, foreman, fakeGame!, world);

    // Search needs no account, but says downloads are off.
    await page.goto("/worlds/base/mods/search?q=fp-nodl");
    await expect(page.locator(".message--note")).toContainText("Mod downloads are off because Foreman has no factorio.com account. Searching still works.");
    await expect(page.getByRole("heading", { level: 3, name: "fp-nodl" })).toBeVisible();
    await checkPage(page);

    await page.getByRole("link", { name: "Add to plan fp-nodl", exact: true }).click();
    await expect(page.locator(".message--note")).toContainText("This plan downloads 1 mod, and downloads are off because Foreman has no factorio.com account.");
    await expect(page.getByRole("button", { name: /^Install/ })).toHaveCount(0);
    await checkPage(page);
    const planUrl = page.url();

    await page.getByRole("link", { name: "Enter the factorio.com account" }).click();
    await expect(page).toHaveURL(/\/settings\/factorio-account\?return=/);
    await page.getByLabel("factorio.com username").fill(portal.username);
    await page.getByLabel("factorio.com token").fill(portal.token);
    await page.getByRole("button", { name: "Save account" }).click();

    // Back on the plan, which now offers the button.
    await expect(page).toHaveURL(planUrl);
    await expect(page.getByRole("heading", { level: 1, name: "Install fp-nodl in Base?" })).toBeVisible();
    const html = await page.content();
    expect(html).not.toContain(portal.token);
    expect(html).not.toContain(portal.username);
    await page.getByRole("button", { name: "Install 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
    expect(existsSync(join(modsDir(foreman), "fp-nodl_1.0.0.zip"))).toBe(true);
    expect(foreman.logs()).not.toContain(portal.token);
    expect((await portal.state()).refused).toEqual([]);
  });

  test("the Mods tab says so, and a change that downloads nothing needs no account", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { ...world, mods: { "fp-local": "2.0" } });
    await page.goto("/worlds/base/mods");
    await expect(page.locator(".message--note").filter({ hasText: "Mod downloads are off" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Enter the factorio.com account" })).toBeVisible();
    await checkPage(page);

    await page.getByRole("table", { name: /Mods of Base/ }).getByRole("row", { name: /fp-local/ }).getByRole("link", { name: "Disable" }).click();
    await page.getByRole("button", { name: "Disable 1 mod" }).click();
    await expect(doneBox(page)).toContainText("The mods of Base are changed.", live);
  });
});

test.describe("a download fails", () => {
  test.use({ portalAccountOn: true });

  const cases: { name: string; prepare: (portal: FakePortal) => Promise<void>; message: RegExp }[] = [
    {
      name: "the checksum is wrong",
      prepare: (portal) => portal.setMode("fp-bad", "1.0.0", "wrong-sha1"),
      message: /did not match the checksum the mod portal publishes/,
    },
    {
      name: "the portal answers 401",
      prepare: (portal) => portal.setMode("fp-bad", "1.0.0", "unauthorized"),
      message: /factorio\.com refused the username or token/,
    },
    {
      name: "the portal goes down after the plan",
      prepare: (portal) => portal.setDown(true),
      // The plan was just made, so the post reads the portal's answers from the cache and the download is what fails.
      message: /A mod could not be downloaded from the mod portal\. Nothing was changed\./,
    },
  ];

  for (const c of cases) {
    test(`${c.name}: the game keeps running, nothing changes, and no token is shown`, async ({ page, foreman, fakeGame, fakePortal }) => {
      const g: FakeGame = fakeGame!;
      const portal = fakePortal!;
      await portal.add("fp-bad", "1.0.0", ["base >= 2.0"]);
      await setUpModsWorld(page, foreman, g, { ...world, start: true });
      const before = { creates: await creates(g), list: readFileSync(join(modsDir(foreman), "mod-list.json"), "utf8"), files: readdirSync(modsDir(foreman)).sort() };

      await page.goto("/worlds/base/mods/plan?kind=install&name=fp-bad");
      await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible();
      await c.prepare(portal);
      await page.getByRole("button", { name: "Install 1 mod" }).click();

      // The failure is in words, on the Server page or the plan page.
      await expect(page.locator("main")).toContainText(c.message, live);
      await checkPage(page);
      expect(await creates(g)).toBe(before.creates);
      expect(readFileSync(join(modsDir(foreman), "mod-list.json"), "utf8")).toBe(before.list);
      expect(readdirSync(modsDir(foreman)).sort()).toEqual(before.files);
      await page.goto("/");
      await expect(stateBadge(page)).toHaveText("Running");
      // The page and the log never hold the token or the username.
      const html = await page.content();
      expect(html).not.toContain(portal.token);
      expect(html).not.toContain(portal.username);
      expect(foreman.logs()).not.toContain(portal.token);
      expect((await portal.state()).refused).toEqual([]);
    });
  }
});
