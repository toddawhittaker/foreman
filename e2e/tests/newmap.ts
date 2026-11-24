import { expect } from "./foreman.js";
import type { Page } from "@playwright/test";

// Step 3 of the guided Generate (SPEC 5.6): the map settings of a world
// whose map is not made yet.
export const newMapPath = (id: string) => `/worlds/${id}/new-map`;

// The planets' pictures in the Map preview; with every planet shown, a plain
// Space Age world's star map (an image too, named "Star map: ...") follows them.
export const planetPictures = (page: Page) => page.getByRole("region", { name: "Map preview" }).getByRole("img", { name: /^Map preview of / });

// Makes a world on /generate and opens its step 3, returning the world id.
// Generate lands on the world page, or on its Mods tab once step 1 is
// split off; the id is read from either address. step1 fills step 1's own
// fields (name, version, Space Age) before the world is made.
export async function openNewMap(page: Page, opts: { name?: string; step1?: (page: Page) => Promise<void> } = {}): Promise<string> {
  await page.goto("/generate");
  await page.locator("#generate-name").fill(opts.name ?? "New world");
  if (opts.step1) await opts.step1(page);
  await page.locator("#generate-button").click();
  await expect(page).toHaveURL(/\/worlds\/[^/?#]+(\/mods)?(\?|$)/);
  const id = new URL(page.url()).pathname.split("/")[2];
  await page.goto(newMapPath(id));
  await expect(page.locator("#generate-form")).toBeVisible({ timeout: 60_000 });
  return id;
}

// Step 1 fields of a 1.1 world: its typed version, no Space Age (SPEC 5.6) and no
// image download. Pass it as openNewMap's step1.
export async function oldGameStep1(page: Page) {
  await page.locator(".version-fields > details.disclosure > summary").click();
  await page.getByLabel("Type a version").fill("1.1.110");
  await page.getByLabel("Type a version").press("Tab");
  await page.getByLabel("Download the game image", { exact: false }).uncheck();
  await page.getByLabel("Space Age is on").uncheck();
}
