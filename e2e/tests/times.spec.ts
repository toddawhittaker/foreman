import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, setUpModsWorld, doneBox, live, image21, stateBadge } from "./foreman.js";

// Taking a snapshot asks Docker whether the game is running (SPEC 6.10).
test.use({ fakeGameOn: true, timezoneId: "America/New_York", locale: "en-US" });

async function takeSnapshot(page: import("@playwright/test").Page, foreman: import("./foreman.js").Foreman) {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/worlds/base/snapshots");
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.getByText(/Done: Foreman took the snapshot/)).toBeVisible();
}

// DESIGN 9: times show in the browser's zone, with how long ago they were.
test("times show in the browser's time zone with a relative time", async ({ page, foreman }) => {
  await takeSnapshot(page, foreman);
  const time = page.locator("tbody tr").first().locator("td").nth(1).locator("time");
  const iso = await time.getAttribute("datetime");
  expect(iso).toMatch(/Z$/);
  const clock = new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit",
    timeZone: "America/New_York",
  }).format(new Date(iso!));
  await expect(time).toContainText(clock);
  await expect(time).toContainText(/\b(EST|EDT)\b/);
  await expect(time).not.toContainText("UTC");
  await expect(time).toHaveText(/\((now|\d+ seconds? ago)\)$/);
  // The date, the clock and the zone are three units that each stay whole (DESIGN 8.3).
  const units = time.locator(":scope > .nowrap");
  await expect(units).toHaveCount(3);
  await expect(units.nth(1)).toHaveText(clock);
  await expect(units.nth(2)).toHaveText(/^(EST|EDT)$/);
  await checkPage(page);

  // The Snapshots tab uses the same element.
  await page.goto("/snapshots");
  await expect(page.locator("tbody tr").first().locator("td").nth(1).locator("time")).toContainText(/\b(EST|EDT)\b/);
  await checkPage(page);
});

// DESIGN 9: without JavaScript the server's UTC text stays.
test("without JavaScript times stay in UTC", async ({ page, foreman, browser }) => {
  await takeSnapshot(page, foreman);
  const plain = await browser.newContext({
    baseURL: foreman.url,
    javaScriptEnabled: false,
    storageState: await page.context().storageState(),
  });
  const p = await plain.newPage();
  await p.goto("/worlds/base/snapshots");
  await expect(p.locator("tbody tr").first().locator("td").nth(1).locator("time")).toHaveText(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC$/);
  await plain.close();
});

// DESIGN 9: a time inside a control's spoken name has the local time only, so
// the name does not go stale or grow.
test("a time in an accessible name has no relative text", async ({ page, foreman }) => {
  await takeSnapshot(page, foreman);
  const name = page.getByRole("checkbox");
  await expect(name).toHaveAccessibleName(/^Manual snapshot \(taken .*\b(EST|EDT)\)$/);
  await expect(name).not.toHaveAccessibleName(/ago|now/);
  await expect(name).not.toHaveAccessibleName(/ ,/);
  await expect(page.getByRole("link", { name: /^Restore Manual snapshot, taken / })).not.toHaveAccessibleName(/ago|now/);
});

// DESIGN 9: the relative text of a visible time is refreshed once a minute.
test("the relative time does not sit stale", async ({ page, foreman }) => {
  await takeSnapshot(page, foreman);
  await page.clock.install();
  await page.reload();
  const time = page.locator("tbody tr").first().locator("td").nth(1).locator("time");
  await expect(time).toHaveText(/\((now|\d+ seconds? ago)\)$/);
  await page.clock.fastForward(125_000);
  await expect(time).toHaveText(/\(2 minutes ago\)$/);
});

// SPEC 17.11: the Restore links on the Deleted worlds page name the time in
// the browser's zone, with no relative text in the name.
test("a Restore link on the Deleted worlds page names the local time", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await page.goto("/worlds/second/delete");
  await page.getByLabel("Type the world's name to confirm").fill("Second");
  await page.getByRole("button", { name: "Delete world" }).click();
  await page.goto("/deleted");
  const link = page.getByRole("link", { name: /^Restore Before delete, taken / });
  await expect(link).toHaveAccessibleName(/\b(EST|EDT)$/);
  await expect(link).not.toHaveAccessibleName(/UTC|ago|now/);
  await checkPage(page);
});

test.describe("the Undo button", () => {
  test.use({ fakePortalOn: true, portalAccountOn: true });

  // SPEC 17.11: the Undo button names when the run of mod changes began in
  // the browser's zone; a button's label has no relative text.
  test("names the local time", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { mods: { "fp-a": "2.1", "fp-b": "2.1" } });
    for (const name of ["fp-a", "fp-b"]) {
      await page.goto(`/worlds/base/mods/plan?kind=disable&name=${name}`);
      await page.getByRole("button", { name: "Disable 1 mod" }).click();
      await expect(page).toHaveURL("/");
      await expect(doneBox(page)).toBeVisible(live);
    }
    const undo = page.locator("#act-undo");
    await expect(undo).toHaveText(/^Undo the mod changes since .*\d:\d\d.*\b(EST|EDT)$/);
    await expect(undo).not.toHaveText(/UTC|ago|now/);
    await checkPage(page);
  });
});

// SPEC 17.11: the crash-loop box's Restore offer names the snapshot's time
// in the browser's zone, with no relative text in a button.
test("the crash-loop Restore offer names the local time", async ({ page, foreman, fakeGame }) => {
  test.setTimeout(90_000);
  await createAdmin(page);
  seedWorld(foreman, { fixture: "2.1.24-space-age-testmod.zip", spaceAge: true });
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  await page.getByRole("button", { name: "Start server" }).click();
  await page.locator("summary", { hasText: "Or start anyway" }).click();
  await page.getByRole("button", { name: "Start anyway" }).click();
  await expect(stateBadge(page)).toHaveText("Running", live);
  await fakeGame!.call("/crash-loop", { name: "foreman-base", on: true });
  await expect(stateBadge(page)).toHaveText("Crash-looping", { timeout: 40_000 });
  const restore = page.locator("#act-recover-restore");
  await expect(restore).toHaveText(/^Restore "Before start anyway" \(.*\d:\d\d.*\b(EST|EDT)\)$/);
  await expect(restore).not.toHaveText(/UTC|ago|now/);
});
