import { expect } from "./foreman.js";
import type { Locator } from "@playwright/test";

// On Import a version not in the list is typed in a disclosure under the
// select. The disclosure opens itself once the list has loaded and says Docker
// Hub cannot be reached, so wait for the list before opening it as a person would.
export async function typedVersion(form: Locator) {
  await expect(form.locator("select[name=game_version] option")).not.toHaveCount(1);
  const d = form.locator("details.disclosure:has(input[name=game_version_typed])");
  if (!(await d.evaluate((e) => (e as HTMLDetailsElement).open))) await d.locator("summary").click();
  return form.getByLabel("Or type a version");
}
