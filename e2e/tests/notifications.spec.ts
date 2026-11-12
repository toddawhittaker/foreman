import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";
import { test, expect, checkPage, createAdmin, openMenu, pickLanguage } from "./foreman.js";

// SPEC 8.10 to 8.12, 14.8, DESIGN 8.16: the notifications page. A webhook's
// address is written once and never shown back: only its preset and host.
// Every project runs these, so each holds at 320 px and at desktop width.

const key = "keykeykeykeykeykeykeykeykeykeykey";
const token = "hook-token-e2e-6612";
const discordURL = `https://discord.com/api/webhooks/42/${token}`;

async function openNotifications(page: import("@playwright/test").Page) {
  await openMenu(page);
  await page.locator("details.menu").getByRole("link", { name: "Notifications" }).click();
  await expect(page).toHaveURL("/settings/notifications");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Notifications");
}

// The address is nowhere: not in a field, not in the markup.
async function expectNoAddress(page: import("@playwright/test").Page) {
  const url = page.locator("#webhook-url");
  if (await url.count()) await expect(url).toHaveValue("");
  expect(await page.content(), "the page shows the address").not.toContain(token);
}

test.describe("with a secret key", () => {
  test.use({ foremanEnv: { FOREMAN_SECRET_KEY: key } });

  test("add, edit and delete a Discord webhook", async ({ page, foreman }) => {
    await createAdmin(page);
    await openNotifications(page);
    await expect(page.locator("#notify-empty")).toContainText("No webhooks yet.");
    await expect(page.locator("#act-webhook-add")).toHaveClass(/button--primary/);
    await checkPage(page);

    await page.locator("#act-webhook-add").click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Add a webhook");
    // Discord, every event and the admin's language are the defaults.
    await expect(page.getByRole("radio", { name: "Discord" })).toBeChecked();
    for (const box of await page.getByRole("checkbox").all()) await expect(box).toBeChecked();
    await expect(page.getByLabel("Language of the messages")).toHaveValue("en");
    await checkPage(page);

    await page.getByLabel("Webhook address").fill(discordURL);
    await page.getByRole("button", { name: "Add webhook" }).click();
    await expect(page).toHaveURL(/\/settings\/notifications\?done=added/);
    await expect(page.locator("#done-message")).toHaveText("Done: Added the Discord webhook to discord.com.");
    await expect(page.locator("#notify-state")).toHaveText("1 webhook gets messages.");
    const row = page.locator("#webhook-1");
    await expect(row.getByRole("heading", { level: 2 })).toHaveText("Discord discord.com");
    await expect(row).toContainText("All events");
    await expectNoAddress(page);
    await checkPage(page);

    // Edit: the address stays unless a new one is typed.
    await page.getByRole("link", { name: "Edit the Discord webhook to discord.com" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Edit the Discord webhook to discord.com");
    await expect(page.locator("#webhook-url-hint")).toHaveText("The address is kept: Discord, discord.com. Type a new address to change it.");
    await expectNoAddress(page);
    await checkPage(page);
    await page.getByRole("checkbox", { name: "A player joins" }).uncheck();
    await page.getByRole("checkbox", { name: "A player leaves" }).uncheck();
    await page.getByLabel("Language of the messages").selectOption("de");
    await page.getByRole("button", { name: "Save webhook" }).click();
    await expect(page.locator("#done-message")).toHaveText("Done: Saved the Discord webhook to discord.com.");
    await expect(row).toContainText("Start, stop, crash, crash loop, scheduled restart, low disk");
    await expect(row.locator('dd[lang="de"]')).toHaveText("Deutsch");

    // Delete asks first; Cancel keeps it.
    await page.getByRole("link", { name: "Delete the Discord webhook to discord.com" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete the Discord webhook to discord.com?");
    await checkPage(page);
    await page.getByRole("link", { name: "Cancel, keep the webhook" }).click();
    await expect(row).toBeVisible();
    await page.getByRole("link", { name: "Delete the Discord webhook to discord.com" }).click();
    await page.getByRole("button", { name: "Delete webhook" }).click();
    await expect(page.locator("#done-message")).toHaveText("Done: Deleted the Discord webhook to discord.com.");
    await expect(page.locator("#notify-empty")).toBeVisible();

    expect(foreman.logs()).not.toContain(token);
  });

  test("a refused address is not shown back and must be typed again", async ({ page, foreman }) => {
    await createAdmin(page);
    await page.goto("/settings/notifications/new");
    await page.getByLabel("Webhook address").fill(`ftp://discord.com/api/webhooks/42/${token}`);
    await page.getByRole("button", { name: "Add webhook" }).click();
    await expect(page.locator("#error-summary")).toBeFocused();
    await expect(page.locator("#webhook-url-error")).toHaveText("Error: Enter an address that starts with https:// or http://.");
    await expect(page.locator("#webhook-url")).toHaveAttribute("aria-invalid", "true");
    await expectNoAddress(page);
    await checkPage(page);

    // A good address with no event ticked: the event is the fault, and the address must be typed again.
    await page.getByLabel("Webhook address").fill(discordURL);
    for (const box of await page.getByRole("checkbox").all()) await box.uncheck();
    await page.getByRole("button", { name: "Add webhook" }).click();
    await expect(page.locator("#webhook-events-error")).toHaveText("Error: Choose at least one event.");
    await expect(page.locator("#webhook-url-error")).toHaveText("Error: Type the address again. Foreman does not keep an address it refused.");
    await expectNoAddress(page);

    // The ntfy preset wants a topic.
    await page.getByRole("radio", { name: "ntfy" }).check();
    await page.getByRole("checkbox", { name: "The server crashes" }).check();
    await page.getByLabel("Webhook address").fill("https://ntfy.sh/");
    await page.getByRole("button", { name: "Add webhook" }).click();
    await expect(page.locator("#webhook-url-error")).toContainText("Enter the address of an ntfy topic");
    await expect(page.getByRole("radio", { name: "ntfy" })).toBeChecked();
    expect(foreman.logs()).not.toContain(token);
  });

  test("an http:// address is accepted with a note that it is unencrypted", async ({ page, foreman }) => {
    await createAdmin(page);
    await page.goto("/settings/notifications/new");
    const field = page.getByLabel("Webhook address");
    const note = page.locator("#webhook-url-plain");
    await expect(note).toBeHidden();
    await page.getByRole("radio", { name: "ntfy" }).check();
    await field.fill(`http://ntfy.lan:8080/${token}`);
    await expect(note).toHaveText("Messages to an http:// address are sent unencrypted.");
    await expect(field).toHaveAccessibleDescription(/sent unencrypted/);
    await checkPage(page);
    await field.fill(`https://ntfy.lan/${token}`);
    await expect(note).toBeHidden();
    await expect(field).not.toHaveAccessibleDescription(/sent unencrypted/);
    await field.fill(`http://ntfy.lan:8080/${token}`);
    await page.getByRole("button", { name: "Add webhook" }).click();
    await expect(page.locator("#done-message")).toHaveText("Done: Added the ntfy webhook to ntfy.lan.");

    // The edit page of an http:// webhook shows the note from the start.
    await page.getByRole("link", { name: "Edit the ntfy webhook to ntfy.lan" }).click();
    await expect(note).toBeVisible();
    await expectNoAddress(page);
    await checkPage(page);
    expect(foreman.logs()).not.toContain(token);
  });

  test("the language defaults to the admin's and the page is translated", async ({ page }) => {
    await createAdmin(page);
    await pickLanguage(page, "fr");
    await page.goto("/settings/notifications/new");
    await expect(page.getByLabel("Langue des messages")).toHaveValue("fr");
    await page.getByLabel("Adresse du webhook").fill("https://ntfy.sh/alerts");
    await page.getByRole("radio", { name: "ntfy" }).check();
    await page.getByRole("button", { name: "Ajouter le webhook" }).click();
    await expect(page.locator("#done-message")).toContainText("Webhook ntfy vers ntfy.sh ajouté.");
    await expect(page.locator("#webhook-1 dd[lang]")).toHaveText("Français");
  });
});

test("without a secret key the page says how to turn webhooks on", async ({ page }) => {
  await createAdmin(page);
  await openNotifications(page);
  await expect(page.locator("#notify-nokey")).toContainText("Notifications need FOREMAN_SECRET_KEY");
  await expect(page.locator("main form")).toHaveCount(0);
  await expect(page.locator("#act-webhook-add")).toHaveCount(0);
  await checkPage(page);
  await page.goto("/settings/notifications/new");
  await expect(page).toHaveURL("/settings/notifications");
});

test.describe("after the secret key changes", () => {
  test.use({ foremanEnv: { FOREMAN_SECRET_KEY: "otherotherotherotherotherotherother" } });

  test("a webhook sealed with the old key says its address must be typed again", async ({ page, foreman }) => {
    await createAdmin(page);
    // A row sealed with another key, as after FOREMAN_SECRET_KEY changes.
    const db = new DatabaseSync(join(foreman.dataDir, "foreman.db"));
    db.prepare(
      "INSERT INTO webhooks (preset, host, url, events_off, lang, created_at, created_by, updated_at, updated_by) VALUES ('discord', 'discord.com', x'01000000000000000000000000000000000000000000000000000000', '', 'en', 0, 'admin', 0, 'admin')",
    ).run();
    db.close();
    await openNotifications(page);
    await expect(page.locator("#notify-state")).toHaveText("0 webhooks get messages.");
    await expect(page.locator("#webhook-1-unreadable")).toContainText("Foreman cannot read this webhook’s address");
    await checkPage(page);
    await page.getByRole("link", { name: "Edit the Discord webhook to discord.com" }).click();
    await expect(page.locator("#webhook-url-hint")).toHaveText("The stored address cannot be read. Type the address again.");
    await page.getByRole("button", { name: "Save webhook" }).click();
    await expect(page.locator("#webhook-url-error")).toHaveText("Error: Enter the webhook address.");
    await page.getByLabel("Webhook address").fill(discordURL);
    await page.getByRole("button", { name: "Save webhook" }).click();
    await expect(page.locator("#notify-state")).toHaveText("1 webhook gets messages.");
    await expect(page.locator("#webhook-1-unreadable")).toHaveCount(0);
  });
});
