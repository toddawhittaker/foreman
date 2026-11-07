import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, signOut } from "./foreman.js";
import type { Page } from "@playwright/test";

// DESIGN 7.2: every field the server refuses when empty carries the required
// attribute, an asterisk after its label (drawn by the style sheet with empty
// alt text, so screen readers hear "required" once, from the field), and its
// form explains the asterisk once with "* Required". The table lists, per
// form, the fields its handler refuses when empty, so a field added to a
// handler without its mark fails here.
const required: Record<string, string[]> = {
  "/worlds/base": ["name"],
  "/worlds/base/clone": ["clone-name"],
  "/worlds/second/delete": ["confirm-name"],
  "/worlds/base/saves": ["save-file"],
  "/import": ["upload-name", "upload-source"],
  "/console": ["command"],
  "/generate": ["generate-name"],
  "/worlds/base/settings": ["name"],
};

async function expectMarked(page: Page, ids: string[]) {
  for (const id of ids) {
    const field = page.locator(`#${id}`);
    await expect(field, `#${id} on ${page.url()}`).toHaveAttribute("required", "");
    const label = page.locator(`label[for="${id}"]`);
    await expect(label, `the mark of #${id}`).toHaveClass(/\brequired\b/);
    expect(await label.evaluate((el) => getComputedStyle(el, "::after").content)).toContain("*");
    // The label's name is its words alone.
    await expect(field).toHaveAccessibleName(((await label.textContent()) ?? "").trim());
    // The form says once what the asterisk means, before the field.
    const note = field.locator("xpath=ancestor::form[1]").locator(".required-note");
    await expect(note, `the note in #${id}'s form`).toHaveCount(1);
    await expect(note).toHaveText("* Required");
    expect((await note.boundingBox())!.y).toBeLessThan((await field.boundingBox())!.y);
  }
  // No field is marked required without the mark, and no mark is on an optional field.
  const unmarked = await page.locator("main [required]").evaluateAll((els) =>
    els.filter((el) => !document.querySelector(`label.required[for="${el.id}"]`)).map((el) => el.id),
  );
  expect(unmarked, `required fields without a mark on ${page.url()}`).toEqual([]);
  const stray = await page.locator("main label.required").evaluateAll((labels) =>
    labels.map((l) => l.getAttribute("for")).filter((id) => id && !document.getElementById(id)?.hasAttribute("required")),
  );
  expect(stray, `marks on optional fields on ${page.url()}`).toEqual([]);
}

test("every required field is marked, on every form", async ({ page, foreman }) => {
  // Generate draws its preview on load, and this test runs with no Docker (DESIGN 8.8).
  foreman.expectError(/"msg":"map preview".*cannot reach Docker/);
  await page.goto("/setup");
  await expectMarked(page, ["username", "password", "confirm"]);
  await checkPage(page);
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  for (const [path, ids] of Object.entries(required)) {
    await page.goto(path);
    await expectMarked(page, ids);
    await checkPage(page);
  }
  await signOut(page);
  await page.goto("/signin");
  await expectMarked(page, ["username", "password"]);
  await checkPage(page);
});

// A placeholder is an example, never the label (DESIGN 7.2).
test("a world name field gives an example, and its label stays", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/clone");
  const field = page.getByLabel("Name of the clone");
  await expect(field).toHaveAttribute("placeholder", "Family base");
  await expect(page.locator('label[for="clone-name"]')).toBeVisible();
});
