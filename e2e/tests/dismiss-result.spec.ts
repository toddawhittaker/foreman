import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, doneBox, live, stateBadge } from "./foreman.js";

// SPEC 17.9, DESIGN 7.3: the Done box after a success has a close button. It
// removes the box, and with it the Switch back offer; the box stays gone on a
// reload; focus goes to the first control of the panel.

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  test("closing the Done box after a switch removes it and its Switch back", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/worlds/second");
    await page.getByRole("link", { name: "Switch to this world" }).click();
    await page.locator("section.confirm").getByRole("button", { name: "Switch to Second" }).click();
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(doneBox(page)).toContainText("The worlds are switched", live);
    await expect(page.getByRole("link", { name: "Switch back to Base" })).toBeVisible();

    const close = doneBox(page).getByRole("button", { name: "Close this message" });
    const size = await close.boundingBox();
    expect(size!.width).toBeGreaterThanOrEqual(44);
    expect(size!.height).toBeGreaterThanOrEqual(44);
    await close.click();

    await expect(doneBox(page)).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Switch back to Base" })).toHaveCount(0);
    await expect(page.locator("#act-snapshot")).toBeFocused();
    await checkPage(page);

    await page.reload();
    await expect(stateBadge(page)).toHaveText("Running", live);
    await expect(doneBox(page)).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Switch back to Base" })).toHaveCount(0);
  });
});

test.describe("with the fake game and no JavaScript", () => {
  test.use({ fakeGameOn: true, javaScriptEnabled: false });

  test("the close button posts and the page comes back without the box", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await page.goto("/");
    await page.getByRole("button", { name: "Take snapshot" }).click();
    // Without script the page does not follow the action, so it is loaded again.
    await expect(async () => {
      await page.goto("/");
      await expect(doneBox(page)).toBeVisible({ timeout: 1_000 });
    }).toPass(live);

    await doneBox(page).getByRole("button", { name: "Close this message" }).click();
    await expect(page).toHaveURL(/\/$/);
    await expect(doneBox(page)).toHaveCount(0);
    await page.reload();
    await expect(doneBox(page)).toHaveCount(0);
  });
});
