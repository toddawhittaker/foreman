import { test, expect, createAdmin, seedWorld, image21, openPlanetRow } from "./foreman.js";
import type { Locator, Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 5.6, 11.5; DESIGN 8.7: a planet with two or more resources has an "All
// resources" row that sets one setting on every resource that is on. It goes
// through each resource's own select, and it sends nothing itself.
test.use({ fakeGameOn: true });

const configFile = (dataDir: string, id: string, file: string) => join(dataDir, "worlds", id, "config", file);
// A slider stands in for the select on a wide panel (DESIGN 8.7); a phone shows selects.
const sliderShown = (page: Page) => page.locator("#mg-nauvis-all-richness-range").isVisible();

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/generate");
  await openPlanetRow(page, "nauvis");
});

const all = (page: Page, key: string) => page.locator(`#mg-nauvis-all-${key}`);
const resource = (page: Page, control: string, key: string) => page.locator(`#mg-nauvis-${control}-${key}`);
const iron = (page: Page, key: string) => resource(page, "iron-ore", key);

// A person's own choice of the last step (600) on the All row: End on the
// slider where one is shown, the select on a phone.
async function chooseAllLast(page: Page, key: string) {
  if (await sliderShown(page)) {
    await page.locator(`#mg-nauvis-all-${key}-range`).focus();
    await page.keyboard.press("End");
  } else {
    await all(page, key).selectOption("600");
  }
}

// Sets a select, as a person's choice does, whether the page shows it or its slider.
async function setSelect(select: Locator, code: string) {
  await select.evaluate((s, v) => {
    (s as HTMLSelectElement).value = v;
    s.dispatchEvent(new Event("input", { bubbles: true }));
    s.dispatchEvent(new Event("change", { bubbles: true }));
  }, code);
}
const setResource = (page: Page, control: string, key: string, code: string) => setSelect(resource(page, control, key), code);

// What the row shows: its select's chosen words.
const shown = (page: Page, key: string) => all(page, key).evaluate((s) => (s as HTMLSelectElement).selectedOptions[0].text);

// The resources of Nauvis that have this setting, by control name.
const nauvisResources = (page: Page, key: string) =>
  page.locator(`#mg-resources-nauvis fieldset.autoplace:not(.autoplace--all) select[name$=".${key}"]`).evaluateAll((ss) => ss.map((s) => (s as HTMLSelectElement).name.split(".")[2]));

test("the row sits at the top of Nauvis's resources, apart from them, and holds the three settings", async ({ page }) => {
  const row = page.locator("#mg-nauvis-all");
  await expect(row).toBeVisible();
  await expect(row.locator("legend")).toHaveText("All resources");
  await expect(row).toContainText("Sets every resource of this planet that is on.");
  expect(await page.locator("#mg-resources-nauvis .mapgen-body > :first-child").getAttribute("id")).toBe("mg-nauvis-all");
  await expect(row.locator("label")).toHaveText(["Frequency", "Size", "Richness"]);
  // Richness offers "None" as the resources do; frequency does not.
  await expect(all(page, "richness").locator("option:not([data-mixed])").first()).toHaveText("None");
  await expect(all(page, "frequency").locator("option:not([data-mixed])").first()).not.toHaveText("None");
  // It starts on what the resources hold, not on "Mixed".
  expect(await shown(page, "richness")).toBe("100% (default)");
});

test("planets with one resource have no row", async ({ page }) => {
  await expect(page.locator("#mg-nauvis-all")).toHaveCount(1);
  await expect(page.locator("#mg-vulcanus-all")).toHaveCount(1);
  await expect(page.locator("#mg-gleba-all")).toHaveCount(0);
  await expect(page.locator("#mg-fulgora-all")).toHaveCount(0);
  // The Terrain tab has none, whatever the planet.
  await expect(page.locator("#panel-terrain [data-all-groups]")).toHaveCount(0);
});

test("moving Richness sets every ticked resource, leaves an unticked one and the cliffs alone, and sends nothing of its own", async ({ page, foreman }) => {
  const cliff = page.locator("#mg-nauvis-nauvis_cliff-size");
  const cliffBefore = await cliff.inputValue();
  const keys = () => page.locator("#generate-form").evaluate((f) => [...new Set([...new FormData(f as HTMLFormElement).keys()])].sort());
  const keysBefore = await keys();

  await page.locator("#mg-nauvis-stone-on").uncheck();
  await chooseAllLast(page, "richness");

  const controls = await nauvisResources(page, "richness");
  expect(controls).toContain("iron-ore");
  expect(controls).toContain("stone");
  for (const c of controls) {
    await expect(resource(page, c, "richness")).toHaveValue(c === "stone" ? "100" : "600");
  }
  // Other settings of the resources and the cliffs did not move.
  await expect(iron(page, "frequency")).toHaveValue("100");
  await expect(iron(page, "size")).toHaveValue("100");
  await expect(cliff).toHaveValue(cliffBefore);
  // The resources' own sliders followed, and the row shows the value they share.
  if (await sliderShown(page)) await expect(page.locator("#mg-nauvis-iron-ore-richness-value")).toHaveText("600%");
  // Stone is off, so the row shows what the others share until stone is ticked again.
  expect(await shown(page, "richness")).toBe("600%");
  await page.locator("#mg-nauvis-stone-on").check();
  expect(await shown(page, "richness")).toBe("Mixed");

  // The form's fields are the same ones as before: the row adds none.
  expect(await keys()).toEqual(keysBefore);
  expect(keysBefore.some((k) => k.includes("all"))).toBe(false);

  // What is generated reflects the values, and stone's box that was ticked again sends its own.
  await page.locator("#mg-nauvis-stone-on").uncheck();
  await page.getByLabel("World name").fill("All rich");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/all-rich?generated=1");
  const mg = JSON.parse(readFileSync(configFile(foreman.dataDir, "all-rich", "map-gen-settings.json"), "utf8"));
  expect(mg.autoplace_controls["iron-ore"].richness).toBe(6);
  expect(mg.autoplace_controls["copper-ore"].richness).toBe(6);
  expect(mg.autoplace_controls["stone"].size).toBe(0);
  expect(Object.keys(mg.autoplace_controls).some((k) => k.includes("all"))).toBe(false);
});

test("Mixed shows while the resources differ, and the shown value follows each change", async ({ page }) => {
  expect(await shown(page, "richness")).toBe("100% (default)");
  await setResource(page, "iron-ore", "richness", "200");
  expect(await shown(page, "richness")).toBe("Mixed");
  if (await sliderShown(page)) {
    await expect(page.locator("#mg-nauvis-all-richness-value")).toHaveText("Mixed");
    await expect(page.locator("#mg-nauvis-all-richness-range")).toHaveAttribute("aria-valuetext", "Mixed");
  }
  // Other settings of the row are not affected by richness differing.
  expect(await shown(page, "frequency")).toBe("100% (default)");

  // Changing it back brings the shared value back.
  await setResource(page, "iron-ore", "richness", "100");
  expect(await shown(page, "richness")).toBe("100% (default)");

  // A resource that differs is out of the row's reach while it is off.
  await setResource(page, "iron-ore", "richness", "200");
  await page.locator("#mg-nauvis-iron-ore-on").uncheck();
  expect(await shown(page, "richness")).toBe("100% (default)");
  await page.locator("#mg-nauvis-iron-ore-on").check();
  expect(await shown(page, "richness")).toBe("Mixed");

  // Choosing a value for all makes them the same again, and Mixed cannot be chosen.
  await setSelect(all(page, "richness"), "300");
  await expect(iron(page, "richness")).toHaveValue("300");
  await expect(resource(page, "copper-ore", "richness")).toHaveValue("300");
  expect(await shown(page, "richness")).toBe("300%");
  await expect(all(page, "richness").locator("option[data-mixed]")).toBeDisabled();
});

test("a resource's warnings and the modified mark follow the row", async ({ page }) => {
  // Richness above the game's default turns achievements off for the resources that have the line; whatever it
  // is, the row's change reaches the same checks as a resource's own, so the mark shows and Reset restores.
  await expect(page.locator("#preset-modified")).toHaveText("");
  await chooseAllLast(page, "richness");
  await expect(page.locator("#preset-modified")).not.toHaveText("");
  await expect(page.locator("#preset-reset")).toBeVisible();
  await page.locator("#preset-reset").click();
  await expect(iron(page, "richness")).toHaveValue("100");
  await expect.poll(() => shown(page, "richness")).toBe("100% (default)");
});

test("a preset that sets resources redraws the row to what they hold", async ({ page }) => {
  await page.locator("#generate-preset").selectOption({ label: "Rich resources" });
  await expect(page.locator("#preset-modified")).toHaveText("");
  // The row shows the shared value, or Mixed, never a stale one.
  await expect
    .poll(async () => {
      const values = await page.locator("#mg-resources-nauvis fieldset.autoplace:not(.autoplace--all) select[name$='.richness']").evaluateAll((ss) => (ss as HTMLSelectElement[]).map((s) => s.value));
      const shownValue = await all(page, "richness").inputValue();
      return new Set(values).size === 1 ? shownValue === values[0] : shownValue === "";
    })
    .toBe(true);
});
