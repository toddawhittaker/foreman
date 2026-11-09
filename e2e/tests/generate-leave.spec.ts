import { test, expect, checkPage, createAdmin, seedWorld, image21, openMenu, tabTo } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 5.6: leaving Generate with settings that differ from what was loaded
// asks first. A link or form of the page asks in Foreman's own dialog, which
// says what is lost; reload, closing the tab, the address bar and Back keep
// the browser's own "leave this page?" question (beforeunload). Nothing
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

// The browser's own questions: Foreman's dialog replaces them for every
// link and form of the page, so a test that expects none still collects them.
function browserQuestions(page: Page, answer: "accept" | "dismiss" = "dismiss") {
  const asked: string[] = [];
  page.on("dialog", async (d) => {
    asked.push(d.type());
    await (answer === "accept" ? d.accept() : d.dismiss());
  });
  return asked;
}

const leaveDialog = (page: Page) => page.getByRole("dialog", { name: "Leave without saving these map settings?" });
const stay = (page: Page) => leaveDialog(page).getByRole("button", { name: "Stay on this page" });
const leave = (page: Page) => leaveDialog(page).getByRole("button", { name: "Leave without saving" });
const cancel = (page: Page) => page.getByRole("link", { name: "Cancel" });

async function edit(page: Page) {
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Half done");
}

test("an unchanged form leaves without asking, even after the preview filled the seed", async ({ page }) => {
  const asked = browserQuestions(page);
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await expect(page.locator("#generate-seed")).toHaveValue(/^\d+$/);
  // A tab and a planet row are views, not settings.
  await page.locator("label.tab", { hasText: /^\s*Terrain\s*$/ }).click();
  await cancel(page).click();
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual([]);
});

test("a changed setting asks in Foreman's dialog before leaving, and staying keeps it and the focus", async ({ page }) => {
  const asked = browserQuestions(page);
  await edit(page);
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await expect(leaveDialog(page).getByText("The new world's settings are not saved. If you leave, they are lost.")).toBeVisible();
  await expect(leaveDialog(page)).toHaveAccessibleDescription("The new world's settings are not saved. If you leave, they are lost.");
  await expect(stay(page)).toBeFocused();
  await stay(page).click();
  await expect(leaveDialog(page)).toBeHidden();
  await expect(page).toHaveURL("/generate");
  await expect(page.getByLabel("World name")).toHaveValue("Half done");
  await expect(cancel(page)).toBeFocused();
  expect(asked).toEqual([]);
});

test("Escape closes the dialog like Stay, and focus goes back to the link", async ({ page }) => {
  await edit(page);
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(leaveDialog(page)).toBeHidden();
  await expect(page).toHaveURL("/generate");
  await expect(page.getByLabel("World name")).toHaveValue("Half done");
  await expect(cancel(page)).toBeFocused();
});

test("leaving from the dialog goes on to the link, and the browser asks nothing more", async ({ page }) => {
  const asked = browserQuestions(page);
  await edit(page);
  await cancel(page).click();
  await leave(page).click();
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual([]);
});

test("a header link asks too, and staying returns focus to it", async ({ page }) => {
  const asked = browserQuestions(page);
  await edit(page);
  const link = page.getByRole("banner").getByRole("link", { name: "Snapshots" });
  await link.click();
  await expect(leaveDialog(page)).toBeVisible();
  await stay(page).click();
  await expect(page).toHaveURL("/generate");
  await expect(link).toBeFocused();
  await link.click();
  await leave(page).click();
  await expect(page).toHaveURL(/\/snapshots$/);
  expect(asked).toEqual([]);
});

test("the language menu and Sign out ask, and leaving carries them out", async ({ page }) => {
  const asked = browserQuestions(page);
  await edit(page);
  await openMenu(page);
  const german = page.locator('details.menu button[name="lang"][value="de"]');
  await german.click();
  await expect(leaveDialog(page)).toBeVisible();
  await stay(page).click();
  await expect(page).toHaveURL("/generate");
  await expect(page.getByLabel("World name")).toHaveValue("Half done");
  // Focus is back in the menu: on the button used, or on the menu's own button if the menu closed meanwhile.
  await expect.poll(() => page.evaluate(() => Boolean(document.activeElement?.closest("details.menu")))).toBe(true);
  await openMenu(page);
  await german.click();
  await leave(page).click();
  await expect(page.locator("html")).toHaveAttribute("lang", "de");
  expect(asked).toEqual([]);
});

test("Sign out asks, and leaving signs out", async ({ page }) => {
  const asked = browserQuestions(page);
  await edit(page);
  await openMenu(page);
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(leaveDialog(page)).toBeVisible();
  await leave(page).click();
  await expect(page).toHaveURL(/\/signed-out/);
  expect(asked).toEqual([]);
});

test("a click that is not leaving asks nothing: in-page links, the picture, and a link opened beside the page", async ({ page, context }) => {
  const asked = browserQuestions(page);
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await page.getByLabel("World name").fill("Half done");
  // The skip link stays on the page.
  await page.keyboard.press("Tab");
  await page.getByRole("link", { name: "Skip to" }).press("Enter");
  await expect(leaveDialog(page)).toBeHidden();
  await expect(page).toHaveURL(/#main$/);
  // A picture opens the large view, which is not leaving.
  await pictures(page).click();
  await expect(page.locator("#large-view")).toBeVisible();
  await expect(leaveDialog(page)).toBeHidden();
  await page.keyboard.press("Escape");
  await expect(page.locator("#large-view")).toBeHidden();
  // A modified click opens the link in another tab, and this page stays as it is.
  const opened = context.waitForEvent("page");
  await cancel(page).click({ modifiers: ["Control"] });
  await (await opened).close();
  await expect(leaveDialog(page)).toBeHidden();
  await expect(page).toHaveURL(/\/generate/);
  expect(asked).toEqual([]);
});

test("a preset change asks, even once it is changed back", async ({ page }) => {
  const asked = browserQuestions(page);
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption("rich-resources");
  await expect(page.locator("#mg-nauvis-iron-ore-richness")).not.toHaveValue("100");
  // The redraw put in fields that load with the edited values, so the mark is
  // kept: asking once too often fails safe (SPEC 5.6).
  await page.locator("#generate-preset").selectOption("default");
  await expect(page.locator("#mg-nauvis-iron-ore-richness")).toHaveValue("100");
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await leave(page).click();
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual([]);
});

test("an edit thrown away by Reset to defaults still asks, as Undo can bring it back", async ({ page }) => {
  await page.goto("/generate");
  await page.locator("#generate-preset").selectOption("rich-resources");
  await expect(page.locator("#mg-nauvis-iron-ore-richness")).not.toHaveValue("100");
  await page.getByRole("button", { name: "Reset to defaults" }).click();
  await expect(page.getByRole("button", { name: "Undo reset" })).toBeVisible();
  await expect(page.locator("#generate-preset")).toHaveValue("default");
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await stay(page).click();
  await expect(page).toHaveURL("/generate");
});

test("a typed seed still asks after Preview map rewrites the seed field", async ({ page }) => {
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await page.locator("#generate-seed").fill("4242");
  await page.getByRole("button", { name: "Preview map" }).click();
  await expect(pictures(page)).toHaveCount(5);
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await stay(page).click();
  await expect(page).toHaveURL("/generate");
});

test("an edited setting still asks after Space Age is ticked and unticked", async ({ page }) => {
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
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await stay(page).click();
  await expect(page).toHaveURL("/generate");
});

test("the preview size and the territory box are views: changing them leaves without asking", async ({ page }) => {
  const asked = browserQuestions(page);
  await page.goto("/generate");
  await expect(pictures(page)).toHaveCount(1);
  await page.locator("#generate-preview-size").selectOption("2048", { force: true });
  await expect(pictures(page)).toHaveAttribute("width", "2048");
  await page.locator("#mg-resources-vulcanus summary").click();
  await expect(pictures(page)).toHaveAccessibleName(/^Map preview of Vulcanus/);
  await page.getByLabel("Show demolisher territories").check();
  await cancel(page).click();
  await expect(page).toHaveURL("/worlds");
  expect(asked).toEqual([]);
});

test("a world made successfully leaves without asking", async ({ page }) => {
  const asked = browserQuestions(page);
  await page.goto("/generate");
  await page.getByLabel("World name").fill("Made here");
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page).toHaveURL("/worlds/made-here?generated=1");
  await expect(leaveDialog(page)).toBeHidden();
  expect(asked).toEqual([]);
});

test("a form sent back with errors asks before leaving", async ({ page }) => {
  await page.goto("/generate");
  await page.locator("#generate-seed").fill("777");
  // No name, so the form comes back with its error.
  await page.getByRole("button", { name: "Generate world" }).click();
  await expect(page.locator("#error-summary")).toBeVisible();
  await cancel(page).click();
  await expect(leaveDialog(page)).toBeVisible();
  await stay(page).click();
  await expect(page).toHaveURL(/\/generate$/);
  await expect(page.locator("#generate-seed")).toHaveValue("777");
});

test("a download link is not leaving, so it asks nothing", async ({ page }) => {
  const asked = browserQuestions(page);
  await edit(page);
  await page.evaluate(() => {
    const a = document.createElement("a");
    a.href = "/worlds";
    a.download = "worlds.html";
    a.textContent = "Save the list";
    document.querySelector("main")!.append(a);
  });
  const download = page.waitForEvent("download");
  await page.getByRole("link", { name: "Save the list" }).click();
  await download;
  await expect(leaveDialog(page)).toBeHidden();
  await expect(page).toHaveURL("/generate");
  expect(asked).toEqual([]);
});

test("the dialog opens from the keyboard and keeps Tab inside until it is closed", async ({ page }) => {
  await edit(page);
  await tabTo(page, cancel(page));
  await page.keyboard.press("Enter");
  await expect(leaveDialog(page)).toBeVisible();
  await expect(stay(page)).toBeFocused();
  // The page behind is inert, so Tab either stays in the dialog or leaves the page for the browser's own controls (nothing focused in the page); it never reaches the form behind.
  const behind = () => page.evaluate(() => {
    const active = document.activeElement;
    return Boolean(active && active !== document.body && !active.closest("#leave-dialog"));
  });
  for (const key of ["Tab", "Tab", "Tab", "Tab", "Shift+Tab", "Shift+Tab", "Shift+Tab"]) {
    await page.keyboard.press(key);
    expect(await behind(), `focus after ${key} is on the page behind the dialog`).toBe(false);
  }
  await stay(page).focus();
  await page.keyboard.press("Escape");
  await expect(leaveDialog(page)).toBeHidden();
  await expect(cancel(page)).toBeFocused();
});

// If the page does not go (a cancelled navigation, a link that is a file),
// Leave must not leave the next attempt unasked.
test("after Leave, a navigation that does not happen is asked about again", async ({ page }) => {
  // A file sent as a download: the page stays where it is.
  await page.route("**/snapshots", (route) => route.fulfill({ status: 200, headers: { "content-disposition": "attachment; filename=x.txt", "content-type": "text/plain" }, body: "x" }));
  const asked = browserQuestions(page, "dismiss");
  await edit(page);
  const link = page.getByRole("banner").getByRole("link", { name: "Snapshots" });
  await link.click();
  const download = page.waitForEvent("download");
  await leave(page).click();
  await download;
  await expect(page).toHaveURL("/generate");
  expect(asked).toEqual([]);
  await link.click();
  await expect(leaveDialog(page)).toBeVisible();
});

// Reload, closing the tab, the address bar and Back cannot be caught by a
// click, so the browser's own question stays for them.
test("a reload with unsaved settings still gets the browser's own question", async ({ page }) => {
  const asked = browserQuestions(page, "accept");
  await edit(page);
  await page.reload();
  expect(asked).toEqual(["beforeunload"]);
  await expect(leaveDialog(page)).toBeHidden();
});

for (const width of [1280, 320]) {
  test(`the open dialog passes the accessibility scan at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 800 });
    await edit(page);
    await cancel(page).click();
    await expect(leaveDialog(page)).toBeVisible();
    // Inside the window with room at the sides, and its buttons whole.
    const box = (await leaveDialog(page).boundingBox())!;
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(width);
    await page.screenshot({ path: `../screenshots/leave-dialog-${width}-${test.info().project.name}.png` });
    await checkPage(page);
  });
}
