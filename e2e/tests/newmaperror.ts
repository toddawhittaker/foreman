import type { Page } from "@playwright/test";

// Puts something that is no number in step 3's seed box without telling the
// page, so no preview starts; saving then sends the form back with its error
// (SPEC 5.6).
export function typeBadSeed(page: Page) {
  return page.locator("#generate-seed").evaluate((i: HTMLInputElement) => { i.value = "x"; });
}
