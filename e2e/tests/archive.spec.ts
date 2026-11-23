import { test, expect, checkPage, createAdmin, seedWorld, seedSecond, image21, live, stateBadge } from "./foreman.js";
import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Archive a world (SPEC 5.9): it leaves the main list for a closed foldout,
// cannot be switched to or started, and comes back with Unarchive.
test.use({ fakeGameOn: true });

test("archive from the world page, find it in the foldout, then unarchive", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);

  await page.goto("/worlds/second");
  await expect(page.locator("#act-switch")).toBeVisible();
  await checkPage(page);

  await page.getByRole("button", { name: "Archive world", exact: true }).click();
  await expect(page).toHaveURL("/worlds/second?archived=on");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Second");
  await expect(page.locator("h1 .badge")).toHaveText("Archived");
  await expect(page.locator(".message--success")).toContainText("Second is archived.");
  await expect(page.locator("#world-done")).toBeFocused();
  await expect(page.locator("#act-switch")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Archive world", exact: true })).toHaveCount(0);
  await checkPage(page);

  // The main list holds only Base; Second waits in a closed foldout.
  await page.goto("/worlds");
  await expect(page.locator("ul.worlds").first().locator("> li")).toHaveCount(1);
  await expect(page.getByRole("link", { name: "Second" })).toBeHidden();
  const foldout = page.locator("#archived-worlds");
  await expect(foldout.locator("summary")).toHaveText("Archived worlds (1)");
  expect(await foldout.evaluate((d) => (d as HTMLDetailsElement).open)).toBe(false);
  await checkPage(page);

  await foldout.locator("summary").click();
  await expect(foldout.getByRole("link", { name: "Second" })).toBeVisible();
  await checkPage(page);
  await foldout.getByRole("link", { name: "Second" }).click();
  await expect(page).toHaveURL("/worlds/second");

  await page.getByRole("button", { name: "Unarchive world" }).click();
  await expect(page).toHaveURL("/worlds/second?archived=off");
  await expect(page.locator(".message--success")).toContainText("Second is unarchived.");
  await expect(page.locator("h1 .badge")).toHaveCount(0);
  await expect(page.locator("#act-switch")).toBeVisible();

  await page.goto("/worlds");
  await expect(page.locator("#archived-worlds")).toHaveCount(0);
  await expect(page.locator("ul.worlds > li")).toHaveCount(2);
});

test("the active world cannot be archived", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);

  await page.goto("/worlds/base");
  await expect(page.getByRole("button", { name: "Archive world", exact: true })).toHaveCount(0);
  await expect(page.getByText("the active world cannot be archived")).toBeVisible();
  await checkPage(page);

  // A hand-made request is refused too.
  const res = await page.request.post("/worlds/base/archive");
  expect(res.status()).toBe(409);
  await page.goto("/worlds");
  await expect(page.locator("#archived-worlds")).toHaveCount(0);
});

test("a switch to an archived world is refused with its reason", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });

  await page.goto("/worlds/second");
  await page.getByRole("button", { name: "Archive world", exact: true }).click();
  await expect(page).toHaveURL("/worlds/second?archived=on");

  await page.goto("/worlds/second/switch");
  await expect(page.locator(".message--error")).toContainText("This world is archived, so it cannot start. Unarchive it first.");
  await checkPage(page);
});

test("an archived world that is active will not start", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  // An older Foreman or a hand edit can leave the active world archived.
  const meta = join(foreman.dataDir, "worlds", "base", "world.json");
  writeFileSync(meta, JSON.stringify({ ...JSON.parse(readFileSync(meta, "utf8")), archived: true }));

  await page.goto("/");
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  await page.getByRole("button", { name: "Start server" }).click();
  await expect(page.locator("main")).toContainText("This world is archived, so it cannot start. Unarchive it first.", live);
  await expect(stateBadge(page)).toHaveText("Stopped");
  await checkPage(page);
});

test("when every world is archived the list says so and keeps the way to add one", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  // The active world is archived by hand: the page refuses to archive it.
  const meta = join(foreman.dataDir, "worlds", "base", "world.json");
  writeFileSync(meta, JSON.stringify({ ...JSON.parse(readFileSync(meta, "utf8")), archived: true }));
  await page.goto("/worlds/second");
  await page.getByRole("button", { name: "Archive world", exact: true }).click();

  await page.goto("/worlds");
  await expect(page.getByText("All your worlds are archived.")).toBeVisible();
  await expect(page.getByRole("link", { name: "Upload a save" })).toBeVisible();
  const foldout = page.locator("#archived-worlds");
  await expect(foldout.locator("summary")).toHaveText("Archived worlds (2)");
  await foldout.locator("summary").click();
  await expect(foldout.getByRole("heading", { level: 2 })).toHaveCount(2);
  await checkPage(page);
});
