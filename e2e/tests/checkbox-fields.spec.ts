import { test, expect, createAdmin, seedWorld, seedSecond, image21 } from "./foreman.js";
import { mkdirSync, writeFileSync, copyFileSync } from "node:fs";
import { join } from "node:path";
import type { Page } from "@playwright/test";

// DESIGN 7.2: a checkbox or radio is native, 1.25rem square, in a label that is
// the 44 px target. The text-box styling must not reach it, even inside a .field.
test.use({ fakeGameOn: true });

const box = 20; // 1.25rem at the 16 px root

async function expectTickSized(page: Page, where: string) {
  const controls = page.locator('input[type="checkbox"]:visible, input[type="radio"]:visible');
  for (const input of await controls.all()) {
    const name = (await input.evaluate((el) => el.id || el.getAttribute("name"))) ?? "";
    const b = (await input.boundingBox())!;
    expect(b.height, `${where} ${name} is no taller than its tick`).toBeLessThanOrEqual(box + 1);
    expect(b.width, `${where} ${name} is no wider than its tick`).toBeLessThanOrEqual(box + 1);
    const label = input.locator("xpath=ancestor::label[1]");
    expect((await label.boundingBox())!.height, `${where} ${name} keeps its 44 px label`).toBeGreaterThanOrEqual(44);
    const look = await input.evaluate((el) => {
      const s = getComputedStyle(el);
      return { shadow: s.boxShadow, padding: s.paddingLeft };
    });
    expect(look, `${where} ${name} has no text-box shadow or padding`).toEqual({ shadow: "none", padding: "0px" });
  }
}

async function expectSmallControls(page: Page, url: string, minimum: number) {
  await page.goto(url);
  const found = await page.locator('input[type="checkbox"]:visible, input[type="radio"]:visible').count();
  expect(found, `${url} has controls to measure`).toBeGreaterThanOrEqual(minimum);
  await expectTickSized(page, url);
}

test("checkboxes and radios are tick-sized on every page that has one", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  // The image is absent, so the pages offer the download checkbox.
  await fakeGame!.call("/image", { ref: image21, on: false });
  const inbox = join(foreman.dataDir, "import", "box");
  mkdirSync(inbox, { recursive: true });
  copyFileSync(join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata", "2.1.24-no-space-age.zip"), join(inbox, "a.zip"));
  writeFileSync(join(inbox, "note.txt"), "x");

  await expectSmallControls(page, "/import", 3); // folder, Space Age, download
  await expectSmallControls(page, "/generate", 3); // mode radios, Space Age
  await page.locator("label.tab:has(#tab-enemy)").click();
  await expect(page.getByRole("checkbox", { name: /Peaceful mode/ })).toBeVisible();
  await expectTickSized(page, "/generate (Enemy tab)");
  await expectSmallControls(page, "/worlds/second/switch", 1);
  await expectSmallControls(page, "/worlds/base/clone", 2);
  await expectSmallControls(page, "/worlds/base/settings", 4);
  await expectSmallControls(page, "/worlds/second/delete", 1);
});
