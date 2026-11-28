import { test, expect, createAdmin, seedWorld, image21, live, setUpModsWorld, version20, makeZip, openModTools } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";
import { openNewMap, newMapPath } from "./newmap.js";

// Fixes 20, wave 4 (#973 #975 #986 #987 #988 #989 #990): the Worlds and
// Generate pages draw each shared part one way. One completion footer
// (DESIGN 7.1), plates (DESIGN 4), fields that fill their column (DESIGN
// 7.2), the empty state (DESIGN 7.3), the row button (DESIGN 7.1) and
// controls that stay put (DESIGN 6). Layout checks are 0 px on x and y.
test.use({ fakeGameOn: true });

type Box = { x: number; y: number; width: number; height: number; right: number; bottom: number };
const wide = { width: 1920, height: 1080 };
const phone = { width: 390, height: 844 };

// A box on the page, measured from the document's start, so scrolling does not count as moving.
const at = (page: Page, selector: string) => page.locator(selector).first().evaluate((el) => {
  const r = el.getBoundingClientRect();
  return { x: r.x + scrollX, y: r.y + scrollY, width: r.width, height: r.height, right: r.right + scrollX, bottom: r.bottom + scrollY };
}) as Promise<Box>;

function addFakePlanet(dataDir: string, id: string) {
  const mods = join(dataDir, "worlds", id, "mods");
  copyFileSync(join(import.meta.dirname, "..", "testdata", "fake-planet_1.0.0.zip"), join(mods, "fake-planet_1.0.0.zip"));
  const listFile = join(mods, "mod-list.json");
  const list = (existsSync(listFile) ? JSON.parse(readFileSync(listFile, "utf8")) : { mods: [{ name: "base", enabled: true }] }) as { mods: { name: string; enabled: boolean }[] };
  list.mods = list.mods.filter((m) => m.name !== "fake-planet").concat([{ name: "fake-planet", enabled: true }]);
  writeFileSync(listFile, JSON.stringify(list));
}

async function generateWorld(page: Page, foreman: Parameters<typeof seedWorld>[0], fakeGame: { call: (p: string, b: Record<string, unknown>) => Promise<void> }) {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame.call("/image", { ref: image21, on: true });
}

test.describe("Generate", () => {
  test("every step's foot bar puts Back and the primary button at the same x", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    const id = await openNewMap(page, { name: "Feet" });
    addFakePlanet(foreman.dataDir, id);
    const steps = [`/worlds/${id}/mods`, `${newMapPath(id)}/mod-settings`, newMapPath(id)];
    for (const size of [wide, phone]) {
      await page.setViewportSize(size);
      const seen: { back: number; primaryEnd: number }[] = [];
      for (const path of steps) {
        await page.goto(path);
        const foot = page.locator(".form-foot").filter({ has: page.locator("#flow-back") });
        await expect(foot, `${size.width} ${path}`).toHaveCount(1, live);
        // The bar settles at its form's end, where it is measured.
        await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
        const back = await foot.locator("#flow-back").boundingBox();
        const primary = await foot.locator(".button--primary").last().boundingBox();
        seen.push({ back: back!.x, primaryEnd: primary!.x + primary!.width });
      }
      expect(seen[1], `${size.width}: step 3 against step 2`).toEqual(seen[0]);
      expect(seen[2], `${size.width}: step 4 against step 2`).toEqual(seen[0]);
    }
    await expect(page.locator(".flow-foot")).toHaveCount(0);
  });

  test("step 4's map settings are in a plate, as step 1's World form is", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    const look = (sel: string) => page.locator(sel).evaluate((f) => {
      const s = getComputedStyle(f);
      return { bg: s.backgroundColor, border: s.borderTopWidth + " " + s.borderLeftWidth, radius: s.borderTopLeftRadius, pad: s.paddingLeft };
    });
    await page.setViewportSize(wide);
    await page.goto("/generate");
    const step1 = await look("#generate-form");
    const id = await openNewMap(page);
    await page.goto(newMapPath(id));
    expect(await look("#generate-form")).toEqual(step1);
    // The settings sit inside the plate's padding, not on the bare page.
    const form = await at(page, "#generate-form");
    const settings = await at(page, "#generate-form .gen-settings");
    expect(settings.x).toBeGreaterThan(form.x);
  });

  test("the typed version fills its column, as the version select does", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    await page.setViewportSize(wide);
    await page.goto("/generate");
    await page.locator(".version-fields > .disclosure > summary").click();
    const select = await at(page, "#generate-version-select");
    expect((await at(page, "#generate-version")).width).toBe(select.width);
  });

  test("Start from a favorite with no favorites shows the shared empty state", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    await page.goto("/generate");
    await expect(page.locator("#favorite-chooser .empty.empty--art")).toHaveCount(1);
  });

  test("a resource's box label is the shared check label, on the page and in the large view", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    await page.setViewportSize(wide);
    const id = await openNewMap(page);
    await page.goto(newMapPath(id));
    const text = (sel: string) => page.locator(sel).first().evaluate((l) => {
      const s = getComputedStyle(l);
      return { size: s.fontSize, spacing: s.letterSpacing, weight: s.fontWeight === "400" ? "400" : "bold" };
    });
    const shared = await text("label.check:has(> #generate-space-age), label.check:has(> #preview-amounts)");
    expect(await text("#map-settings .autoplace > legend .check")).toEqual({ ...shared, weight: "bold" });
    await page.getByRole("region", { name: "Map preview" }).getByRole("link", { name: /opens the large view/ }).first().click();
    await expect(page.locator("#large-settings .autoplace > legend .check").first()).toBeVisible(live);
    expect(await text("#large-settings .autoplace > legend .check")).toEqual({ ...shared, weight: "bold" });
  });

  test("the Preset select fills its column, and marking it modified moves nothing below", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    const id = await openNewMap(page);
    for (const size of [wide, phone]) {
      await page.setViewportSize(size);
      await page.goto(newMapPath(id));
      await expect(page.locator("#preset-modified")).toHaveText("");
      const select = await at(page, "#generate-preset");
      const column = await at(page, "#preset-box > .field");
      expect({ x: select.x, right: select.right }, `${size.width}`).toEqual({ x: column.x, right: column.right });
      const below = ["#map-settings", "#preset-box + .field"];
      const before = await Promise.all(below.map((b) => at(page, b)));
      await page.locator("#ms-pollution-enabled").evaluate((b) => (b as HTMLInputElement).click());
      await expect(page.locator("#preset-modified")).toHaveText("[modified]");
      await expect(page.locator("#preset-reset")).toBeVisible();
      expect(await Promise.all(below.map((b) => at(page, b))), `${size.width}`).toEqual(before);
    }
  });

  test("on a phone, ticking Show resource amounts or territories moves nothing below the preview", async ({ page, foreman, fakeGame }) => {
    await generateWorld(page, foreman, fakeGame!);
    await page.setViewportSize(phone);
    const id = await openNewMap(page);
    await page.goto(newMapPath(id));
    const section = page.getByRole("region", { name: "Map preview" });
    await expect(section.getByRole("img")).toHaveCount(1, { timeout: 60_000 });
    const below = "#map-settings";
    const before = await at(page, below);
    await page.locator("#preview-amounts").evaluate((b) => (b as HTMLInputElement).click());
    await expect(page.locator("#preview-amounts-result details.resource-text")).toBeVisible(live);
    await expect(page.locator("#preview-amounts-work .working")).toBeHidden(live);
    expect(await at(page, below), "amounts on").toEqual(before);
    await page.locator("#preview-amounts").evaluate((b) => (b as HTMLInputElement).click());
    await expect(page.locator("#preview-amounts-result")).toBeEmpty(live);
    expect(await at(page, below), "amounts off").toEqual(before);
    // All planets: every card's territory line has its slot before the box is ticked.
    await page.locator("label:has(> #preview-view-all)").click();
    await expect(section.locator(".pictures > li").nth(1)).toBeVisible({ timeout: 60_000 });
    await expect(page.locator("#preview")).not.toHaveAttribute("aria-busy", "true", live);
    const cards = await page.locator("#preview .pictures > li").count();
    const parts = [below, ...Array.from({ length: cards }, (_, i) => `#preview .pictures > li:nth-child(${i + 1})`)];
    const all = await Promise.all(parts.map((p) => at(page, p)));
    await page.locator("#preview-territories").evaluate((b) => (b as HTMLInputElement).click());
    await expect(page.locator("#preview-territories-work .working")).toBeHidden(live);
    await expect(page.locator("#preview .map .territories").first()).toBeVisible(live);
    await expect(page.locator("#preview")).not.toHaveAttribute("aria-busy", "true");
    expect(await Promise.all(parts.map((p) => at(page, p))), "territories on").toEqual(all);
  });
});

test.describe("Favorites", () => {
  test("the empty state's link is centred with its drawing and sentence", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.setViewportSize(wide);
    await page.goto("/favorites");
    const text = await at(page, "#favorites-empty .empty__text");
    const link = await at(page, "#favorites-empty ~ .actions > a");
    expect(link.x + link.width / 2).toBe(text.x + text.width / 2);
  });
});

test.describe("Factory map", () => {
  test("the page is full width, Draw and the empty state in a plate", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.setViewportSize(wide);
    await page.goto("/worlds/base/map");
    const tabs = await at(page, "main .subnav");
    const plate = await at(page, "#factorymap-state");
    expect({ x: plate.x, right: plate.right }).toEqual({ x: tabs.x, right: tabs.right });
    expect(await page.locator("#factorymap-state").evaluate((s) => getComputedStyle(s).backgroundColor))
      .toBe(await page.locator(".factorymap-mode").evaluate((s) => getComputedStyle(s).backgroundColor));
    const mode = await at(page, ".factorymap-mode");
    expect({ x: mode.x, right: mode.right }).toEqual({ x: tabs.x, right: tabs.right });
    for (const inside of ["#factorymap-state .factorymap__draw .button", "#factorymap-state .factorymap__summary"]) {
      const b = await at(page, inside);
      expect(b.x, inside).toBeGreaterThan(plate.x);
      expect(b.bottom, inside).toBeLessThan(plate.bottom);
    }
  });
});

const fmtestDir = join(import.meta.dirname, "..", "..", "internal", "game", "testdata", "modsettings", "fmtest_1.0.0");
function fmtestZip(): Buffer {
  const entries: Record<string, string> = {};
  const walk = (dir: string) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else entries[`fmtest_1.0.0/${relative(fmtestDir, p)}`] = readFileSync(p, "utf8");
    }
  };
  walk(fmtestDir);
  return makeZip(entries);
}

test.describe("Mods", () => {
  test.use({ fakePortalOn: true });

  test("Mod settings' foot bar ends the Save plate, at its edges", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { fmtest: "2.0" } });
    writeFileSync(join(foreman.dataDir, "worlds", "base", "mods", "fmtest_1.0.0.zip"), fmtestZip());
    for (const size of [wide, phone]) {
      await page.setViewportSize(size);
      await page.goto("/worlds/base/mods");
      await openModTools(page);
      await page.getByRole("link", { name: "Edit mod settings" }).click();
      await expect(page.getByLabel("Startup integer")).toBeVisible(live);
      await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
      const save = await page.locator("section:has(> .setting-row__head #mod-settings-save-heading)").evaluate((s) => {
        const r = s.getBoundingClientRect();
        return { x: r.x, right: r.right, bottom: r.bottom };
      });
      const foot = await page.locator("#mod-settings-form .form-foot").evaluate((f) => {
        const r = f.getBoundingClientRect();
        return { x: r.x, right: r.right, bottom: r.top };
      });
      expect(foot, `${size.width}`).toEqual(save);
    }
  });

  test("a row's More is the shared row button: raised, with the row button's padding", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip", mods: { fmtest: "2.0" } });
    await page.setViewportSize(wide);
    await page.goto("/worlds/base/mods");
    const more = page.locator(".mod-list .row-menu__toggle").first();
    await expect(more).toBeVisible(live);
    const looks = await more.evaluate((m) => {
      // A plain secondary button in the same row, for the shared row-button look.
      const probe = document.createElement("button");
      probe.className = "button";
      probe.textContent = "More";
      m.closest(".row-menu")!.parentElement!.append(probe);
      const pick = (el: Element) => {
        const s = getComputedStyle(el);
        return { bg: s.backgroundColor, shadow: s.boxShadow, padBlock: s.paddingTop + " " + s.paddingBottom, height: el.getBoundingClientRect().height };
      };
      const out = { more: pick(m), shared: pick(probe) };
      probe.remove();
      return out;
    });
    expect(looks.more).toEqual(looks.shared);
    expect(looks.more.height).toBe(36);
  });

  // The plan's primary button needs downloads on, so the portal account is set.
  test.describe("with a portal account", () => {
    test.use({ portalAccountOn: true });

    test("ticking an optional mod leaves Cancel where it was", async ({ page, foreman, fakeGame, fakePortal }) => {
      await fakePortal!.add("fp-extra", "1.0.0", ["base >= 2.0"]);
      await fakePortal!.add("fp-core", "1.0.0", ["base >= 2.0", "? fp-extra"]);
      await setUpModsWorld(page, foreman, fakeGame!, { version: version20, fixture: "2.0.77-no-space-age.zip" });
      await page.setViewportSize(wide);
      await page.goto("/worlds/base/mods/plan?kind=install&name=fp-core");
      const cancel = page.getByRole("link", { name: "Cancel, change nothing" });
      await expect(page.getByRole("button", { name: "Install 1 mod" })).toBeVisible(live);
      await cancel.scrollIntoViewIfNeeded();
      const before = await at(page, "a:has(+ #act-apply)");
      await page.locator('input[type=checkbox][name="optional"][value="fp-extra"]').check();
      await expect(page.getByRole("button", { name: "Install 2 mods" })).toBeVisible(live);
      expect(await at(page, "a:has(+ #act-apply)")).toEqual(before);
    });
  });
});
