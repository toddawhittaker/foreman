import { test, expect, checkPage, setUpModsWorld, live, version20, type SeedMod } from "./foreman.js";
import type { Locator } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// The Mods tab with a world of 45 mods, as people run them (issues 342, 343,
// 360, 365, 371, 372, 373; SPEC 9.3, 9.7, 9.8, 17.4, 17.12). The projects run
// it in both themes, at desktop and at 320 px.
test.use({ fakeGameOn: true, fakePortalOn: true, portalAccountOn: true });

const many = Array.from({ length: 40 }, (_, i) => `fp-mod-${String(i + 1).padStart(2, "0")}`);
const updated = ["fp-mod-02", "fp-mod-05", "fp-mod-09"];

test("a world of 45 mods reads as even rows with each mod's news in its own row", async ({ page, foreman, fakeGame, fakePortal }) => {
  const portal = fakePortal!;
  const mods: Record<string, SeedMod> = {};
  for (const name of many) {
    await portal.add(name, "1.0.0", ["base >= 2.0"]);
    mods[name] = { factorio: "2.0", enabled: name !== "fp-mod-12" };
  }
  for (const name of updated) await portal.add(name, "1.1.0", ["base >= 2.0"]);
  // 1.5.5 is gone from the portal, which has 1.7.2.
  await portal.add("visible-planets", "1.7.2", ["base >= 2.0"]);
  mods["visible-planets"] = { factorio: "2.0", version: "1.5.5" };
  // The portal has none of these.
  mods["zz-private-tweaks"] = { factorio: "2.0" };
  mods["a-mod-with-a-rather-long-name-for-a-phone-screen"] = { factorio: "2.0" };
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods });
  // A mod installed as a folder.
  const folder = join(foreman.dataDir, "worlds", "base", "mods", "my-local-tweaks");
  mkdirSync(folder);
  writeFileSync(join(folder, "info.json"), JSON.stringify({ name: "my-local-tweaks", version: "0.3.0", factorio_version: "2.0" }));

  await page.goto("/worlds/base/mods");
  const status = page.locator("#mod-status");
  // One summary line: red only because players cannot join, with Update all beside it.
  await expect(status.locator(".message--error")).toContainText("1 mod stops players joining. Its row says how to fix it.", live);
  await expect(status).toContainText("2 mods are not on the mod portal.");
  await expect(status).toContainText("4 mods have an update for this game version.");
  await expect(status.getByRole("link", { name: "Update all" })).toBeVisible();
  await expect(status.locator(".message")).toHaveCount(1);

  const table = page.getByRole("table", { name: /Mods of Base: 44 mods\. 43 are enabled\./ });
  const row = (name: string) => table.getByRole("row", { name: new RegExp(`^${name}\\b`) });
  // Each flag is a line in its mod's row, with its fix there.
  await expect(row("visible-planets")).toContainText("Error: Players cannot join");
  await expect(row("visible-planets").getByRole("link", { name: "Update to 1.7.2 visible-planets" })).toBeVisible();
  await expect(row("zz-private-tweaks")).toContainText("Note: Not on the mod portal");
  // The portal's link goes from a mod it does not have; the name stays.
  await expect(row("zz-private-tweaks").getByRole("link", { name: /on the mod portal/ })).toHaveCount(0);
  await expect(row("fp-mod-01").getByRole("link", { name: "fp-mod-01 on the mod portal, opens in a new tab" })).toHaveAttribute("href", "https://mods.factorio.com/mod/fp-mod-01");
  // The state is a word only when a mod is off (SPEC 17.4).
  await expect(row("fp-mod-12")).toContainText("Disabled");
  await expect(row("fp-mod-12").getByRole("link", { name: "Enable fp-mod-12" })).toBeVisible();
  await expect(table).not.toContainText("Enabled");
  // A folder mod: its badge, no portal link, no Remove.
  await expect(row("my-local-tweaks")).toContainText("Local folder");
  await expect(row("my-local-tweaks").getByRole("link", { name: /Remove|on the mod portal/ })).toHaveCount(0);
  // Remove is the only red control (DESIGN 7.1).
  const red = await page.locator("main a, main button").evaluateAll((els) => {
    const probe = document.createElement("span");
    probe.style.color = "var(--color-danger-text)";
    document.body.append(probe);
    const danger = getComputedStyle(probe).color;
    probe.remove();
    return els
      .filter((el) => getComputedStyle(el).color === danger || el.classList.contains("button--danger"))
      .map((el) => el.textContent?.trim().split(/\s/)[0]);
  });
  expect(new Set(red)).toEqual(new Set(["Remove"]));

  const phone = (page.viewportSize()?.width ?? 1280) < 600;
  const box = async (l: Locator) => (await l.boundingBox())!;
  // Rows with nothing to say are one even height, and Disable and Remove keep their place.
  const plain = ["fp-mod-01", "fp-mod-03", "fp-mod-20", "fp-mod-40"];
  const heights = await Promise.all(plain.map(async (n) => Math.round((await box(row(n))).height)));
  expect(new Set(heights).size).toBe(1);
  const removeEnds = await Promise.all([...plain, ...updated].map(async (n) => {
    const b = await box(row(n).getByRole("link", { name: `Remove ${n}` }));
    return Math.round(phone ? b.x : b.x + b.width);
  }));
  if (!phone) expect(new Set(removeEnds).size).toBe(1);
  // Change version only where the portal has another release on the line
  // (SPEC 9.3): the updated mods and the gone version, not a mod with one.
  for (const n of [...updated, "visible-planets"]) await expect(row(n).getByRole("link", { name: `Change version ${n}`, exact: true })).toBeVisible();
  for (const n of [...plain, "zz-private-tweaks", "my-local-tweaks"]) await expect(row(n).getByRole("link", { name: /^Change version/ })).toHaveCount(0);
  // With Update and Change version, Remove still shares Disable's line, and
  // a folder mod's Disable sits where the other rows' Disable sits (DESIGN 8.11).
  for (const n of updated) {
    const dis = await box(row(n).getByRole("link", { name: `Disable ${n}` }));
    const rem = await box(row(n).getByRole("link", { name: `Remove ${n}` }));
    expect(Math.abs(rem.y - dis.y)).toBeLessThan(2);
  }
  const disableAt = async (n: string) => {
    const b = await box(row(n).getByRole("link", { name: `Disable ${n}` }));
    return Math.round(phone ? b.x : b.x + b.width);
  };
  expect(await disableAt("my-local-tweaks")).toBe(await disableAt("fp-mod-01"));
  // On a phone the actions drop under the name, so the name keeps the width.
  const name = await box(row("a-mod-with-a-rather-long-name-for-a-phone-screen").locator(".mod-name"));
  const disable = await box(row("a-mod-with-a-rather-long-name-for-a-phone-screen").getByRole("link", { name: /^Disable/ }));
  if (phone) {
    expect(disable.y).toBeGreaterThan(name.y + name.height - 1);
    expect(name.height).toBeLessThan(80);
  } else {
    expect(Math.abs(disable.y - name.y)).toBeLessThan(10);
  }

  // Issue 372: the built-in line says which are off and why.
  await expect(page.locator("#mods-built-in")).toContainText("Built in, version 2.0.77: base. Space Age is off, so elevated-rails, quality, space-age are off.");
  await expect(page.getByRole("link", { name: "Turn on Space Age" })).toHaveAttribute("href", "/worlds/base/settings");
  // Issue 373: the save uses none of these mods, so the tab says so before any click.
  await expect(page.locator("#sync-state")).toContainText("The mods differ from the save base.zip:");
  await expect(page.locator(".sync-diff")).toContainText("43 enabled mods are not in the save");
  // Issue 371: the rare forms fold away, below everything else.
  await expect(page.locator("#mod-file")).toBeHidden();
  await expect(page.locator("#settings-file")).toBeHidden();
  await checkPage(page);
  await page.screenshot({ path: `../screenshots/mods-tab-many-${test.info().project.name}.png`, fullPage: true });

  // The folded forms open by keyboard and pass the checks open.
  await page.locator("#mod-upload > summary").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#mod-file")).toBeVisible();
  await page.locator("#mod-settings-replace > summary").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#settings-file")).toBeVisible();
  await checkPage(page);
});

// Issue 373: a world whose mods match its save says so, and offers no match.
test("a world whose mods match the save says so", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip" });
  await page.goto("/worlds/base/mods");
  await expect(page.locator("#sync-state")).toHaveText("The mods match the save base.zip.");
  await expect(page.getByRole("link", { name: "Match mods to the save" })).toHaveCount(0);
  await checkPage(page);
});
