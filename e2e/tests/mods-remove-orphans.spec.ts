import { test, expect, checkPage, setUpModsWorld, doneBox, live, version20 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 9.4: removing a mod offers, ticked, each installed mod only it
// needed; an unticked one is kept, a ticked one goes in the same change.
test.use({ fakeGameOn: true });

const shot = (page: Page, name: string) => page.screenshot({ path: `../screenshots/${name}-${test.info().project.name}.png`, fullPage: true });

test("removing a mod offers its orphans, and an unticked one is kept", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, {
    version: version20,
    fixture: "2.0.77-no-space-age.zip",
    mods: {
      "fp-top": { factorio: "2.0", deps: ["base >= 2.0", "fp-dep-a", "fp-dep-b", "? fp-extra"] },
      "fp-dep-a": "2.0",
      "fp-dep-b": "2.0",
      "fp-extra": "2.0",
    },
  });
  await page.goto("/worlds/base/mods/plan?kind=remove&name=fp-top");
  const orphans = page.locator("#plan-orphans");
  await expect(orphans.getByRole("checkbox")).toHaveCount(2);
  const a = orphans.getByRole("checkbox", { name: "fp-dep-a, only needed by fp-top" });
  const b = orphans.getByRole("checkbox", { name: "fp-dep-b, only needed by fp-top" });
  await expect(a).toBeChecked();
  await expect(b).toBeChecked();
  await expect(page.getByRole("button", { name: "Remove 3 mods" })).toBeVisible();
  await checkPage(page);
  await shot(page, "mods-remove-orphans");

  await a.uncheck();
  await expect(page.getByRole("button", { name: "Remove 2 mods" })).toBeVisible();
  await expect(orphans.getByRole("checkbox", { name: "fp-dep-a, only needed by fp-top" })).not.toBeChecked();
  await page.getByRole("button", { name: "Remove 2 mods" }).click();
  await expect(doneBox(page)).toBeVisible(live);

  const zips = readdirSync(join(foreman.dataDir, "worlds", "base", "mods")).filter((n) => n.endsWith(".zip")).sort();
  expect(zips).toEqual(["fp-dep-a_1.0.0.zip", "fp-extra_1.0.0.zip"]);
});
