import { test, expect, checkPage, setUpModsWorld, stateBadge, live } from "./foreman.js";
import type { Locator } from "@playwright/test";

// SPEC 7.4, 17.5, 17.9, DESIGN 7.3 and 7.4: a refusal before a start reads
// calmly. It states the problem, offers the main fix first, then Restore,
// and keeps "start anyway", with all it will do, behind one disclosure.
test.use({ fakeGameOn: true, fakePortalOn: true });

const save = "2.1.24-space-age-testmod.zip";

// Opens the start-anyway disclosure, if the box has one. The start runs in
// the background, so the refusal can reach the page by a live update after
// it loads; the box must be there before it is looked in.
async function openAnyway(box: Locator) {
  await expect(box).toBeVisible(live);
  const summary = box.locator("details > summary");
  if ((await summary.count()) > 0 && !(await box.locator("details").getAttribute("open"))) await summary.click();
}

// Every list item's text fills the box's width when it wraps: no item is
// squeezed into a narrow column beside empty space (DESIGN 7.3).
async function expectFullWidthItems(box: Locator) {
  const narrow = await box.locator("li").evaluateAll((items) =>
    items
      .filter((li) => (li as HTMLElement).offsetParent !== null)
      .map((li) => {
        const range = document.createRange();
        range.selectNodeContents(li);
        const rects = [...range.getClientRects()].filter((r) => r.width > 0);
        const lines = new Set(rects.map((r) => Math.round(r.top))).size;
        const box = li.getBoundingClientRect();
        const used = Math.max(...rects.map((r) => r.right)) - box.left;
        return { text: li.textContent?.trim().slice(0, 40), lines, used: Math.round(used), width: Math.round(box.width) };
      })
      .filter((i) => i.lines > 1 && i.used < i.width * 0.75),
  );
  expect(narrow, "list items squeezed into a narrow column").toEqual([]);
}

test("a refused start states the problem, offers the fix first, and keeps Start anyway behind one choice", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
  await page.getByRole("button", { name: "Start server" }).click();
  const box = page.locator(".message--warning").filter({ hasText: "does not match the world" });
  // One problem is one sentence, after the lead, not a one-item list.
  await expect(box.locator("p").first()).toContainText(
    "Foreman did not start the game, because the save that would load does not match the world: base.zip uses foreman-test, but this world has no file for it.",
    live,
  );
  // The main fix is the first control in the box.
  await expect(box.locator("a, button, summary").first()).toHaveText("Match the mods to the save");
  // The stale advice to copy zip files by hand is gone; the fix does it.
  await expect(box).not.toContainText("cannot add mods to a world yet");
  // Start anyway, and the list of what it does, wait behind one disclosure.
  const anyway = box.locator("details");
  await expect(anyway.locator("summary")).toHaveText("Or start anyway");
  await expect(box.getByRole("button", { name: "Start anyway" })).toBeHidden();
  await expect(box.getByText("If you choose Start anyway, Foreman will:")).toBeHidden();
  await checkPage(page);

  // The keyboard opens it; the steps and the snapshot are then shown.
  await anyway.locator("summary").focus();
  await page.keyboard.press("Enter");
  await expect(box.getByText("If you choose Start anyway, Foreman will:")).toBeVisible();
  await expect(box).toContainText('Take a snapshot of Base, labelled "Before start anyway".');
  await expectFullWidthItems(box);
  await checkPage(page);

  await box.getByRole("button", { name: "Start anyway" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
});

// A refusal inside a step list, as after a start or a mod change: every list
// in the box fills its width, at any screen size.
test("a refusal in the step list keeps its lists at the box's full width", async ({ page, foreman, fakeGame }) => {
  await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
  await page.getByRole("button", { name: "Start server" }).click();
  const box = page.locator(".steps .message--warning");
  await expect(box).toContainText("base.zip uses foreman-test", live);
  await openAnyway(box);
  await expect(box).toContainText("If you choose Start anyway, Foreman will:");
  await expectFullWidthItems(box);
});

// A refused restart is the same box, with the warning to players first.
test("a refused restart offers Restart anyway the same way", async ({ page, foreman, fakeGame }) => {
  test.setTimeout(90_000);
  await setUpModsWorld(page, foreman, fakeGame!, { fixture: save, spaceAge: true });
  await page.getByRole("button", { name: "Start server" }).click();
  const first = page.locator(".message--warning").filter({ hasText: "does not match the world" });
  await openAnyway(first);
  await first.getByRole("button", { name: "Start anyway" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);

  await page.getByRole("link", { name: "Restart server" }).click();
  await page.getByRole("button", { name: "Restart server" }).click();
  const box = page.locator(".message--warning").filter({ hasText: "does not match the world" });
  await expect(box.locator("summary")).toHaveText("Or restart anyway", live);
  await openAnyway(box);
  await expect(box).toContainText("Warn players online with a 60-second countdown, save the game and stop it.");
  await expectFullWidthItems(box);
  await checkPage(page);
});
