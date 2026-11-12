import { test, expect, checkPage, createAdmin, mainNav, pickLanguage } from "./foreman.js";

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

test("the Settings tab leads to the Restart schedule and Notifications pages", async ({ page }) => {
  await createAdmin(page);
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await expect(page).toHaveURL("/settings/restart-schedule");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Restart schedule");
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "Notifications" }).click();
  await expect(page).toHaveURL("/settings/notifications");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Notifications");
  await expect(mainNav(page).getByRole("link", { name: "Settings" })).toHaveAttribute("aria-current", "true");
  await checkPage(page);
});

test("the tab and links are translated", async ({ page }) => {
  await createAdmin(page);
  await pickLanguage(page, "de");
  await page.goto("/chat");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Chat");
  await expect(page.getByRole("navigation", { name: "Hauptmenü" }).getByRole("link", { name: "Einstellungen" })).toBeVisible();
  await page.goto("/settings/restart-schedule");
  const nav = page.getByRole("navigation", { name: "Foreman-Einstellungen" });
  await expect(nav.getByRole("link", { name: "Benachrichtigungen" })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Neustart-Zeitplan" })).toBeVisible();
});

test("the new pages need a signed-in user", async ({ page }) => {
  for (const path of ["/chat", "/settings/notifications", "/settings/restart-schedule"]) {
    const res = await page.request.get(path, { maxRedirects: 0 });
    expect(res.status(), path).not.toBe(200);
  }
});
