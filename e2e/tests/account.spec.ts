import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";
import { test, expect, checkPage, createAdmin, openMenu, seedWorld, tabTo } from "./foreman.js";

// SPEC 9.11, 11.4: the factorio.com account page. The username and token are
// written once and never shown back. Every project runs these, so each holds
// at 320 px and at desktop width, in both themes.

const key = "keykeykeykeykeykeykeykeykeykeykey";
const username = "portal-user-7731";
const token = "portal-token-5509";

// Reaches the page the way a person does: the user menu, then the link.
async function openAccountPage(page: import("@playwright/test").Page) {
  await openMenu(page);
  await page.locator("details.menu").getByRole("link", { name: "factorio.com account" }).click();
  await expect(page).toHaveURL("/settings/factorio-account");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("factorio.com account");
}

// Neither value is anywhere on the page: not in a field, not in the text.
async function expectNoValues(page: import("@playwright/test").Page, ...values: string[]) {
  for (const field of await page.locator("main input:not([type=hidden])").all()) {
    await expect(field).toHaveValue("");
  }
  const html = await page.content();
  for (const v of values) expect(html, `the page shows ${v}`).not.toContain(v);
}

test.describe("with a secret key", () => {
  test.use({ foremanEnv: { FOREMAN_SECRET_KEY: key } });

  test("save, see the stored line, replace and remove", async ({ page, foreman }) => {
    await createAdmin(page);
    await openAccountPage(page);
    // The state leads the page in one sentence, before the intro (SPEC 9.11).
    await expect(page.locator("#account-none")).toHaveText("Mod downloads are off: no account is set.");
    await expect(page.locator("main")).not.toContainText("Searching for mods still works");
    await expect(page.locator("#account-none + .account-intro > p").first()).toContainText("Foreman uses this account");
    // With no account the form is open and its button is the primary action.
    await expect(page.getByRole("button", { name: "Save account" })).toHaveClass(/button--primary/);
    await expect(page.locator("main details")).toHaveCount(0);
    await expect(page.locator("#account-shared")).toContainText("Every admin’s mod downloads use this one account");
    await expect(page.getByLabel("factorio.com token")).toHaveAttribute("type", "password");
    await checkPage(page);

    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page).toHaveURL("/settings/factorio-account?done=saved");
    await expect(page.locator(".message--success")).toHaveText("Done: The account is saved.");
    await expect(page.locator("#account-stored")).toContainText(/^Mod downloads are on\. The account was stored on .+ by admin\.$/);
    await expect(page.locator("#account-stored time")).toHaveAttribute("datetime", /^\d{4}-\d{2}-\d{2}T/);
    await expectNoValues(page, username, token);
    await checkPage(page);

    // Replace is folded away and secondary; opening it takes one click.
    const replace = page.locator("details#account-replace");
    await expect(replace).not.toHaveAttribute("open", "");
    await expect(page.getByLabel("factorio.com username")).toBeHidden();
    await expect(page.getByRole("button", { name: "Remove account" })).not.toHaveClass(/button--primary/);
    await replace.getByText("Replace the account").click();
    await expect(page.getByRole("button", { name: "Replace account" })).not.toHaveClass(/button--primary/);
    await checkPage(page);
    await page.getByLabel("factorio.com username").fill("second-user");
    await page.getByLabel("factorio.com token").fill("second-token");
    await page.getByRole("button", { name: "Replace account" }).click();
    await expect(page.locator(".message--success")).toHaveText("Done: The account is saved.");
    await expectNoValues(page, username, token, "second-user", "second-token");

    await page.getByRole("button", { name: "Remove account" }).click();
    await expect(page).toHaveURL("/settings/factorio-account?done=removed");
    await expect(page.locator(".message--success")).toHaveText("Done: The account is removed.");
    await expect(page.locator("#account-none")).toBeVisible();
    await expect(page.getByRole("button", { name: "Remove account" })).toHaveCount(0);
    await checkPage(page);

    // The log never held a value.
    for (const v of [username, token, "second-user", "second-token"]) expect(foreman.logs()).not.toContain(v);
  });

  test("a refused form lists the problems and shows nothing back", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/settings/factorio-account");
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page.locator("#error-summary")).toContainText("Enter the factorio.com username");
    await expect(page.locator("#account-username-error")).toBeVisible();
    await expect(page.locator("main")).toContainText("Type both again.");
    await expectNoValues(page, token);
    await checkPage(page);
  });

  test("works by keyboard alone, from the menu to the saved line", async ({ page }) => {
    await createAdmin(page);
    await page.locator("details.menu > summary").focus();
    await page.keyboard.press("Enter");
    await tabTo(page, page.locator("details.menu").getByRole("link", { name: "factorio.com account" }));
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL("/settings/factorio-account");

    await tabTo(page, page.getByLabel("factorio.com username"));
    await page.keyboard.type(username);
    // The token's hint links to the profile page, where the token is; then the field.
    await page.keyboard.press("Tab");
    await expect(page.getByRole("link", { name: /your factorio\.com profile page/ })).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(page.getByLabel("factorio.com token")).toBeFocused();
    await page.keyboard.type(token);
    // Enter in a field submits the form.
    await page.keyboard.press("Enter");
    await expect(page.locator(".message--success")).toHaveText("Done: The account is saved.");

    // Replace by keyboard: the summary opens the form, and Enter in a field submits it.
    await tabTo(page, page.getByText("Replace the account"));
    await page.keyboard.press("Enter");
    await expect(page.locator("details#account-replace")).toHaveAttribute("open", "");
    await tabTo(page, page.getByLabel("factorio.com username"));
    await page.keyboard.type("second-user");
    await tabTo(page, page.getByLabel("factorio.com token"));
    await page.keyboard.type("second-token");
    await page.keyboard.press("Enter");
    await expect(page.locator(".message--success")).toHaveText("Done: The account is saved.");

    await tabTo(page, page.getByRole("button", { name: "Remove account" }));
    await page.keyboard.press("Enter");
    await expect(page.locator(".message--success")).toHaveText("Done: The account is removed.");
  });

  test("a refused replace opens the form again and asks for both fields", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/settings/factorio-account");
    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page.locator("#account-stored")).toBeVisible();

    await page.getByText("Replace the account").click();
    await page.getByLabel("factorio.com username").fill("second-user");
    await page.getByRole("button", { name: "Replace account" }).click();
    await expect(page.locator("#error-summary")).toContainText("Enter the factorio.com token");
    await expect(page.locator("details#account-replace")).toHaveAttribute("open", "");
    await expect(page.locator("#account-token-error")).toBeVisible();
    await expect(page.getByLabel("factorio.com username")).toHaveValue("");
    await checkPage(page);
  });

  test("goes back to the page that sent the admin, and only to a local path", async ({ page }) => {
    await createAdmin(page);
    await page.goto("/settings/factorio-account?return=%2Fworlds");
    await expect(page.getByRole("link", { name: "Cancel" })).toHaveAttribute("href", "/worlds");
    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page).toHaveURL("/worlds");

    // An outside address is dropped: there is no way back, and saving stays here.
    await page.goto("/settings/factorio-account?return=https%3A%2F%2Fevil.example%2F");
    await page.getByText("Replace the account").click();
    await expect(page.getByRole("link", { name: "Cancel" })).toHaveCount(0);
    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Replace account" }).click();
    await expect(page).toHaveURL("/settings/factorio-account?done=saved");
  });

  test("an unreadable stored account asks for it again", async ({ page, foreman }) => {
    await createAdmin(page);
    await page.goto("/settings/factorio-account");
    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page.locator("#account-stored")).toBeVisible();

    // A row the key cannot open, as after a key change.
    const db = new DatabaseSync(join(foreman.dataDir, "foreman.db"));
    db.prepare("UPDATE stored_secrets SET value = ? WHERE name = 'factorio-account'").run(Buffer.from("not sealed by this key"));
    db.close();

    await page.goto("/settings/factorio-account");
    await expect(page.locator("#account-unreadable")).toContainText("could not be read");
    await expect(page.locator("#account-unreadable")).toContainText("Enter the account again to replace it.");
    await checkPage(page);
    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page.locator("#account-stored")).toBeVisible();
  });
});

test.describe("without a secret key", () => {
  test("the page says how to turn it on, with no form to fill", async ({ page }) => {
    await createAdmin(page);
    await openAccountPage(page);
    const note = page.locator("#account-nokey");
    await expect(note).toContainText("FOREMAN_SECRET_KEY");
    await expect(note).toContainText("FOREMAN_FACTORIO_USERNAME");
    // A form that cannot be used is not shown at all, disabled or otherwise.
    await expect(page.locator("main form")).toHaveCount(0);
    await expect(page.getByLabel("factorio.com username")).toHaveCount(0);
    await checkPage(page);
  });
});

test.describe("with an account in the environment", () => {
  test.use({ foremanEnv: { FOREMAN_FACTORIO_USERNAME: username, FOREMAN_FACTORIO_TOKEN: token } });

  test("the page names the variables and has no form", async ({ page }) => {
    await createAdmin(page);
    await openAccountPage(page);
    await expect(page.getByRole("heading", { level: 2, name: "Set by environment" })).toBeVisible();
    await expect(page.locator("main")).toContainText("FOREMAN_FACTORIO_USERNAME");
    await expect(page.locator("main form")).toHaveCount(0);
    await expectNoValues(page, username, token);
    await checkPage(page);
  });
});

// SPEC 9.11, 14.17: the token's hint links to where the token is, and the
// Mods tab's link to this page comes back to the Mods tab once the account is saved.
test.describe("from the Mods tab", () => {
  test.use({ foremanEnv: { FOREMAN_SECRET_KEY: key }, fakeGameOn: true });

  test("the profile link, then saving returns to the Mods tab", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/worlds/base/mods");
    await page.getByRole("link", { name: "Enter the factorio.com account" }).click();
    await expect(page).toHaveURL("/settings/factorio-account?return=%2Fworlds%2Fbase%2Fmods");
    const profile = page.getByRole("link", { name: "your factorio.com profile page (opens in a new tab)" });
    await expect(profile).toHaveAttribute("href", "https://factorio.com/profile");
    await expect(profile).toHaveAttribute("target", "_blank");
    await expect(profile).toHaveAttribute("rel", "noopener noreferrer");
    await checkPage(page);
    await page.getByLabel("factorio.com username").fill(username);
    await page.getByLabel("factorio.com token").fill(token);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page).toHaveURL("/worlds/base/mods");
    await expect(page.getByText("Mod downloads are off")).toHaveCount(0);
  });
});
