import { test, expect, checkPage, createAdmin, openMenu, pickLanguage } from "./foreman.js";

// The Chat tab and the links to the Notifications and Restart schedule pages.
// This checks they are reachable the way a person reaches them and load.

test("the Chat tab sits beside Overview and Console and opens the Chat page", async ({ page }) => {
  await createAdmin(page);
  const tabs = page.getByRole("navigation", { name: "Server pages" });
  await expect(tabs.getByRole("link")).toHaveText(["Overview", "Console", "Chat"]);
  await tabs.getByRole("link", { name: "Chat" }).click();
  await expect(page).toHaveURL("/chat");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Chat");
  await expect(page.getByRole("navigation", { name: "Server pages" }).getByRole("link", { name: "Chat" })).toHaveAttribute("aria-current", "page");
  await checkPage(page);
});

test("the user menu links to the Notifications and Restart schedule pages", async ({ page }) => {
  await createAdmin(page);
  const menu = page.locator("details.menu");
  for (const [name, path] of [
    ["Restart schedule", "/settings/restart-schedule"],
    ["Notifications", "/settings/notifications"],
  ]) {
    await openMenu(page);
    // Beside the factorio.com account link.
    await expect(menu.getByRole("link", { name: "factorio.com account" })).toBeVisible();
    await menu.getByRole("link", { name }).click();
    await expect(page).toHaveURL(path);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(name);
    await openMenu(page);
    await expect(menu.getByRole("link", { name })).toHaveAttribute("aria-current", "page");
    await checkPage(page);
  }
});

test("the tab and links are translated", async ({ page }) => {
  await createAdmin(page);
  await pickLanguage(page, "de");
  await page.goto("/chat");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Chat");
  await openMenu(page);
  await expect(page.locator("details.menu").getByRole("link", { name: "Benachrichtigungen" })).toBeVisible();
  await expect(page.locator("details.menu").getByRole("link", { name: "Neustart-Zeitplan" })).toBeVisible();
});

test("the new pages need a signed-in user", async ({ page }) => {
  for (const path of ["/chat", "/settings/notifications", "/settings/restart-schedule"]) {
    const res = await page.request.get(path, { maxRedirects: 0 });
    expect(res.status(), path).not.toBe(200);
  }
});
