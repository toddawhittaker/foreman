import { test, expect, createAdmin, seedWorld, image21 } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 5.6: leaving Generate with settings that differ from what was loaded
// asks the browser's own "leave this page?" question (beforeunload). Nothing
// changed, a seed the preview filled in, and a world made successfully ask
// nothing. A form the server sent back with errors holds settings that are
// not saved, so leaving it asks too.
test.use({ fakeGameOn: true });

test.beforeEach(async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
});

const pictures = (page: Page) => page.getByRole("region", { name: "Map preview" }).getByRole("img");

// Counts the leave questions; each one is answered with accept or dismiss.
function leaveQuestions(page: Page, answer: "accept" | "dismiss") {
  const asked: string[] = [];
  page.on("dialog", async (d) => {
    asked.push(d.type());
    await (answer === "accept" ? d.accept() : d.dismiss());
  });
  return asked;
}

async function cancel(page: Page) {
  await page.getByRole("link", { name: "Cancel" }).click();
}

test("an unchanged form leaves without asking, even after the preview filled the seed", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await expect(page.locator("#generate-seed")).toHaveValue(/^\d+$/);
  // A tab and a planet row are views, not settings.
  await page.locator("label.tab", { hasText: /^\s*Terrain\s*$/ }).click();
  await cancel(page);
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual([]);
});

test("a changed setting asks before leaving, and staying keeps it", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Half done");
  await cancel(page);
  await expect.poll(() => asked).toEqual(["beforeunload"]);
  await expect(page).toHaveURL("/generate");
  await expect(page.getByLabel("World name")).toHaveValue("Half done");
});

test("a preset change asks, even once it is changed back", async ({ page }) => {
  const asked = leaveQuestions(page, "accept");
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption("rich-resources");
  await expect(page.locator("#mg-nauvis-iron-ore-richness")).not.toHaveValue("100");
  // The redraw put in fields that load with the edited values, so the mark is
  // kept: asking once too often fails safe (SPEC 5.6).
  await page.locator("#generate-preset").selectOption("default");
  await expect(page.locator("#mg-nauvis-iron-ore-richness")).toHaveValue("100");
  await cancel(page);
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual(["beforeunload"]);
});

test("a typed seed still asks after Preview map rewrites the seed field", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await page.locator("#generate-seed").fill("4242");
  await page.getByRole("button", { name: "Preview map" }).click();
  await expect(pictures(page)).toHaveCount(5);
  await cancel(page);
  await expect.poll(() => asked).toEqual(["beforeunload"]);
  await expect(page).toHaveURL("/generate");
});

test("an edited setting still asks after Space Age is ticked and unticked", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await page.locator("#mg-nauvis-iron-ore-size").evaluate((s) => {
    (s as HTMLSelectElement).value = "200";
    s.dispatchEvent(new Event("change", { bubbles: true }));
  });
  const spaceAge = page.getByLabel("Space Age is on");
  await spaceAge.uncheck();
  await expect(page.locator("#mg-resources-vulcanus")).toHaveCount(0);
  await spaceAge.check();
  await expect(page.locator("#mg-resources-vulcanus")).toHaveCount(1);
  await cancel(page);
  await expect.poll(() => asked).toEqual(["beforeunload"]);
  await expect(page).toHaveURL("/generate");
});

test("the preview size and the territory box are views: changing them leaves without asking", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await page.locator("#generate-preview-size").selectOption("2048", { force: true });
  await expect(pictures(page)).toHaveAttribute("width", "2048");
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Vulcanus/);
  await page.getByLabel("Show demolisher territories").check();
  await cancel(page);
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual([]);
});

test("a world made successfully leaves without asking", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Made here");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/made-here?generated=1");
  expect(asked).toEqual([]);
});

test("a form sent back with errors asks before leaving", async ({ page }) => {
  const asked = leaveQuestions(page, "dismiss");
  await page.goto("/generate");
  await page.locator("#generate-seed").fill("777");
  // No name, so the form comes back with its error.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toBeVisible();
  await cancel(page);
  await expect.poll(() => asked).toEqual(["beforeunload"]);
  await expect(page).toHaveURL(/\/generate$/);
  await expect(page.locator("#generate-seed")).toHaveValue("777");
});
