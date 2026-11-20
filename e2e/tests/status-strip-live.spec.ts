import { test, expect, createAdmin, seedWorld, image21, live, stateBadge } from "./foreman.js";

// SPEC 16.3, DESIGN 6 (Shell) and 7.5: the status strip follows the server's
// state live on every signed-in page, not only on Server, Console and Chat
// (#736). Signed-out pages open no stream.

test.use({ fakeGameOn: true });

test("the status strip follows the server's state on pages that are not Server", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  const strip = page.locator("header.site #site-state");
  const tab = page.locator("#nav-lamp");

  // A Settings page follows a start made from another tab.
  await page.goto("/settings/general");
  await expect(strip).toContainText("Stopped");
  await expect(page.locator("[sse-connect]")).toHaveCount(1);
  const other = await page.context().newPage();
  await other.goto("/");
  // The Server page keeps its own stream and opens no second one.
  await expect(other.locator("[sse-connect]")).toHaveCount(1);
  await other.getByRole("button", { name: "Start server" }).click();
  await expect(stateBadge(other)).toHaveText("Running", live);
  await other.close();
  await expect(strip).toContainText("Running", live);
  await expect(tab).toContainText("Running", live);

  // Worlds follows a crash.
  await page.goto("/worlds");
  await expect(strip).toContainText("Running");
  await fakeGame!.call("/crash", { name: "foreman-base" });
  await expect(strip).toContainText("Crashed", live);
  await expect(tab).toContainText("Crashed", live);
  await expect(page.locator("main h1")).toBeVisible();
  await expect(page.locator("#live-lost")).toHaveCount(0);
});

test("signed-out pages open no stream", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("[sse-connect]")).toHaveCount(0);
});
