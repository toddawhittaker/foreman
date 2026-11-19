import { test, expect, checkPage, createAdmin, adminPassword, mainNav } from "./foreman.js";
import type { Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import type { Foreman } from "./foreman.js";

// SPEC 12.12: the Users tab lists every user, deletes another user through a
// confirm page, and changes your own password.

function sql(f: Foreman, query: string, ...args: (string | number)[]) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return db.prepare(query).all(...args) as Record<string, unknown>[];
  } finally {
    db.close();
  }
}

// Adds a user with the admin's password hash, so it signs in with adminPassword.
function addPasswordUser(f: Foreman, name: string) {
  sql(f, "INSERT INTO users (username, password_hash, role, created_at) SELECT ?, password_hash, 'admin', created_at FROM users WHERE username = 'admin'", name);
}

async function openUsers(page: Page) {
  await page.goto("/");
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "Users" }).click();
  await expect(page).toHaveURL("/settings/users");
}

async function signIn(page: Page, name: string, password: string) {
  await page.goto("/signin");
  await page.getByLabel("Username").fill(name);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("the tab lists users and deletes another one, signing them out", async ({ page, foreman, browser }) => {
  await createAdmin(page);
  addPasswordUser(foreman, "bob");
  sql(foreman, "INSERT INTO users (username, password_hash, role, created_at) SELECT 'carol', '', 'admin', created_at + 1 FROM users WHERE username = 'admin'");
  sql(foreman, "INSERT INTO api_tokens (user_id, name, scope, secret_hash, created_at, period_start) SELECT id, 'Claude', 'read', 'x', 0, 0 FROM users WHERE username = 'bob'");

  const bob = await browser.newContext({ baseURL: foreman.url });
  const bobPage = await bob.newPage();
  await signIn(bobPage, "bob", adminPassword);
  await expect(bobPage).toHaveURL("/");

  await openUsers(page);
  const list = page.locator("#user-list");
  await expect(list.getByRole("heading", { level: 3 })).toHaveText([/admin\s+You/, "bob", "carol"]);
  await expect(list.locator("li").nth(1)).toContainText("Password");
  await expect(list.locator("li").nth(2)).toContainText("Proxy");
  // No Delete for yourself.
  await expect(list.locator("li").first().getByRole("link")).toHaveCount(0);
  await checkPage(page);

  await page.getByRole("link", { name: "Delete user bob" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Delete user bob?");
  await expect(page.locator("#user-delete-tokens")).toHaveText("Delete their 1 API token.");
  await expect(page.locator("#user-delete-returns")).toHaveCount(0);
  await checkPage(page);
  await page.getByRole("button", { name: "Delete user" }).click();
  await expect(page).toHaveURL(/\/settings\/users\?done=deleted/);
  await expect(page.locator("#done-message")).toHaveText("Done: The user was deleted.");
  await expect(page.locator("#user-list")).not.toContainText("bob");
  expect(sql(foreman, "SELECT count(*) AS n FROM api_tokens")[0].n).toBe(0);
  expect(sql(foreman, "SELECT username, detail FROM audit_log WHERE action = 'user-delete'")).toEqual([{ username: "admin", detail: "bob: requested; API tokens: 1" }]);
  await checkPage(page);

  // Bob's next request is signed out.
  await bobPage.goto("/");
  await expect(bobPage).toHaveURL(/\/signin/);
  await bob.close();

  // A proxy user is warned they come back.
  await page.getByRole("link", { name: "Delete user carol" }).click();
  await expect(page.locator("#user-delete-returns")).toContainText("remove them at the proxy");
  await checkPage(page);
});

test("change your own password: refusals, then success signs out other browsers", async ({ page, foreman, browser }) => {
  await createAdmin(page);
  const other = await browser.newContext({ baseURL: foreman.url });
  const otherPage = await other.newPage();
  await signIn(otherPage, "admin", adminPassword);
  await expect(otherPage).toHaveURL("/");

  await openUsers(page);
  const fresh = "a brand new passphrase";
  await page.getByLabel("Current password").fill("not the password");
  await page.getByLabel("New password", { exact: true }).fill(fresh);
  await page.getByLabel("New password again").fill(fresh);
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.locator("#password-current-error")).toHaveText("Error: The current password is wrong.");
  await expect(page.getByLabel("New password", { exact: true })).toHaveValue("");
  await checkPage(page);

  await page.getByLabel("Current password").fill(adminPassword);
  await page.getByLabel("New password", { exact: true }).fill("short");
  await page.getByLabel("New password again").fill("other");
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.locator("#password-new-error")).toContainText("too short");
  await expect(page.locator("#password-confirm-error")).toContainText("different");
  await checkPage(page);

  await page.getByLabel("Current password").fill(adminPassword);
  await page.getByLabel("New password", { exact: true }).fill(fresh);
  await page.getByLabel("New password again").fill(fresh);
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.locator("#done-message")).toContainText("Your password is changed.");
  await checkPage(page);
  expect(sql(foreman, "SELECT username, detail FROM audit_log WHERE action = 'password-change'")).toEqual([{ username: "admin", detail: "" }]);

  // This browser stays signed in; the other one is signed out.
  await page.goto("/settings/users");
  await expect(page).toHaveURL("/settings/users");
  await otherPage.goto("/");
  await expect(otherPage).toHaveURL(/\/signin/);
  await signIn(otherPage, "admin", fresh);
  await expect(otherPage).toHaveURL("/");
  await other.close();
});

test.describe("a proxy user", () => {
  test.use({
    fakeGameOn: true,
    foremanEnv: {
      FOREMAN_AUTH_METHODS: "headers",
      FOREMAN_AUTH_USER_HEADER: "X-Test-User",
      FOREMAN_AUTH_GROUPS_HEADER: "X-Test-Groups",
      FOREMAN_AUTH_REQUIRED_GROUP: "factorio",
      FOREMAN_TRUSTED_PROXIES: "127.0.0.1/32",
    },
  });

  test("sees no password form, and cannot delete themselves", async ({ page }) => {
    await page.setExtraHTTPHeaders({ "X-Test-User": "alice", "X-Test-Groups": "factorio" });
    await page.goto("/settings/users");
    await expect(page.locator("#password-managed")).toHaveText("You sign in through the proxy, so your sign-in is managed there. You have no password here.");
    await expect(page.getByLabel("Current password")).toHaveCount(0);
    await expect(page.locator("#user-list").getByRole("link")).toHaveCount(0);
    await checkPage(page);
  });
});
