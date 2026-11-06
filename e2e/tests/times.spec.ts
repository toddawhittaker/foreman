import { test, expect, checkPage, createAdmin, seedWorld } from "./foreman.js";

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
