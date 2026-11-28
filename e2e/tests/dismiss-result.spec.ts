import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, doneBox, live, stateBadge } from "./foreman.js";

// SPEC 17.9, DESIGN 6, 7.3: after a success with an undo, the Done words are a
// toast and the plate keeps the undo in a row under its actions, with a close
// button. Closing removes the row, and with it the Switch back offer, and the
// server forgets the result, so neither comes back on a reload; focus goes to
// the first control of the plate.

const undoRow = (page: import("@playwright/test").Page) => page.locator("#result-undo");

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("closing the Done box after a switch removes it and its Switch back", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/worlds/second");
    await page.getByRole("link", { name: "Switch to this world" }).click();
    await page.locator("section.confirm").getByRole("button", { name: "Switch to Second" }).click();
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(doneBox(page)).toContainText("The worlds are switched", live);
    await expect(undoRow(page).getByRole("link", { name: "Switch back to Base" })).toBeVisible();

    const close = undoRow(page).getByRole("button", { name: "Dismiss the undo offer" });
    const size = await close.boundingBox();
    expect(size!.width).toBeGreaterThanOrEqual(44);
    expect(size!.height).toBeGreaterThanOrEqual(44);
    await close.click();

    await expect(undoRow(page)).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Switch back to Base" })).toHaveCount(0);
    await expect(page.locator("#act-snapshot")).toBeFocused();
    await checkPage(page);

    await page.reload();
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(doneBox(page)).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Switch back to Base" })).toHaveCount(0);
  });

  // The live update can reach the page before the close's own answer does. It
  // redraws the panel, so the form that sent the close is gone when the answer
  // comes; focus must still land on the first control, not on the heading.
  test("focus goes to the first control when the live update comes before the answer", { tag: "@looks" }, async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/worlds/second");
    await page.getByRole("link", { name: "Switch to this world" }).click();
    await page.locator("section.confirm").getByRole("button", { name: "Switch to Second" }).click();
    await expect(doneBox(page)).toContainText("The worlds are switched", live);

    let release!: () => void;
    const held = new Promise<void>((r) => (release = r));
    await page.route("**/dismiss-result", async (route) => {
      const answer = await route.fetch();
      await held;
      await route.fulfill({ response: answer });
    });
    await undoRow(page).getByRole("button", { name: "Dismiss the undo offer" }).click();
    // The server has forgotten the result; its live update removes the row.
    await expect(undoRow(page)).toHaveCount(0, live);
    release();
    await expect(page.locator("#act-snapshot")).toBeFocused();
  });
});

test.describe("with the fake game and no JavaScript", () => {
  test.use({ fakeGameOn: true, javaScriptEnabled: false });

  test("the close button posts and the page comes back without the row or the toast", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/worlds/second/switch");
    await page.locator("section.confirm").getByRole("button", { name: "Switch to Second" }).click();
    // Without script the page does not follow the action, so it is loaded again.
    await expect(async () => {
      await page.goto("/");
      await expect(undoRow(page)).toBeVisible({ timeout: 1_000 });
    }).toPass(live);
    await expect(doneBox(page)).toContainText("The worlds are switched");

    await undoRow(page).getByRole("button", { name: "Dismiss the undo offer" }).click();
    await expect(page).toHaveURL(/\/$/);
    await expect(undoRow(page)).toHaveCount(0);
    await expect(doneBox(page)).toHaveCount(0);
    await page.reload();
    await expect(undoRow(page)).toHaveCount(0);
    await expect(doneBox(page)).toHaveCount(0);
  });
});
