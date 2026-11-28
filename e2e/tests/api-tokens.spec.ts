import { test, expect, checkPage, createAdmin, mainNav } from "./foreman.js";
import { languages } from "./i18n.js";
import type { Page } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import type { Foreman } from "./foreman.js";

// SPEC 12.10, 14.19 and 21.2: the API tokens page, the secret shown once,
// renew, revoke through a confirm page, the expiry banner and the MCP switch.

function sql(f: Foreman, query: string, ...args: (string | number)[]) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    return db.prepare(query).all(...args) as Record<string, unknown>[];
  } finally {
    db.close();
  }
}

async function openTokens(page: Page) {
  await page.goto("/");
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "API tokens" }).click();
  await expect(page).toHaveURL("/settings/api-tokens");
}

async function create(page: Page, name: string) {
  await page.getByLabel("Name").fill(name);
  await page.getByRole("button", { name: "Create token" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(`Token ${name} created`);
  return page.locator("#token-secret").inputValue();
}

test("create a token, see the secret once, renew and revoke it", { tag: "@looks" }, async ({ page, foreman }) => {
  await createAdmin(page);
  await openTokens(page);
  await expect(page.locator("#my-tokens-empty")).toBeVisible();
  await expect(page.getByRole("radio", { name: "Read" })).toBeChecked();
  await expect(page.locator("#scope-admin-warning")).toContainText("guards against mistakes");
  await checkPage(page);

  const secret = await create(page, "Claude");
  expect(secret).toMatch(/^fmn_[A-Za-z0-9_-]{43}$/);
  await expect(page.locator("#token-endpoint")).toHaveValue(/\/mcp$/);
  await expect(page.locator("#access-off-note")).toBeVisible();
  await checkPage(page);

  await page.getByRole("link", { name: "Back to API tokens" }).click();
  await expect(page.locator("#my-tokens")).toContainText("Claude");
  expect(await page.content()).not.toContain(secret);

  const id = Number(sql(foreman, "SELECT id FROM api_tokens WHERE name = 'Claude'")[0].id);
  const before = Number(sql(foreman, "SELECT period_start FROM api_tokens WHERE id = ?", id)[0].period_start);
  sql(foreman, "UPDATE api_tokens SET period_start = ? WHERE id = ?", before - 10 * 86400, id);
  await page.reload();
  await page.locator("#my-tokens").getByRole("button", { name: "Renew Claude" }).click();
  // The result is a toast, which takes no focus (DESIGN 6, "Toasts").
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Renewed Claude. It works for another 30 days.");
  expect(Number(sql(foreman, "SELECT period_start FROM api_tokens WHERE id = ?", id)[0].period_start)).toBeGreaterThanOrEqual(before);

  await page.locator("#my-tokens").getByRole("link", { name: /^Revoke Claude/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Revoke Claude of admin?");
  await checkPage(page);
  await page.getByRole("button", { name: "Revoke token" }).click();
  await expect(page.locator("#toasts .toast--success")).toContainText("Revoked Claude.");
  await expect(page.locator("#token-" + id)).toContainText("Revoked");
  await expect(page.locator("#token-" + id).getByRole("button")).toHaveCount(0);
  await checkPage(page);
});

test("the banner appears near expiry and Renew comes back to the same page", async ({ page, foreman }) => {
  await createAdmin(page);
  await openTokens(page);
  await create(page, "Claude");
  await page.goto("/worlds");
  await expect(page.locator("#token-banner")).toHaveCount(0);

  const now = Math.floor(Date.now() / 1000);
  sql(foreman, "UPDATE api_tokens SET period_start = ? WHERE name = 'Claude'", now - 26 * 86400);
  await page.reload();
  const banner = page.locator("#token-banner");
  await expect(banner).toContainText("Your API token Claude expires at");
  await checkPage(page);
  await banner.getByRole("button", { name: "Renew Claude" }).click();
  // Once its toast shows, the address drops the result (DESIGN 6, "Toasts").
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Renewed Claude. It works for another 30 days.");
  await expect(page).toHaveURL("/worlds");
  await expect(page.locator("#token-banner")).toHaveCount(0);

  sql(foreman, "UPDATE api_tokens SET period_start = ? WHERE name = 'Claude'", now - 40 * 86400);
  await page.reload();
  await expect(page.locator("#token-banner")).toContainText("Your API token Claude expired at");
  await checkPage(page);
  await page.goto("/settings/api-tokens");
  await expect(page.locator("#my-tokens")).toContainText("Expired");
  await checkPage(page);
});

test("the MCP switch turns access on and off", async ({ page, foreman }) => {
  await createAdmin(page);
  await openTokens(page);
  await expect(page.locator("#mcp-state")).toContainText("MCP access is off.");
  await page.getByRole("button", { name: "Turn on MCP access" }).click();
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Turned MCP access on.");
  await expect(page.locator("#mcp-state")).toContainText("MCP access is on.");
  await checkPage(page);
  await page.getByRole("button", { name: "Turn off MCP access" }).click();
  await expect(page.locator("#mcp-state")).toContainText("MCP access is off.");
  const rows = sql(foreman, "SELECT detail FROM audit_log WHERE action = 'mcp-set' ORDER BY id");
  expect(rows.map((r) => r.detail)).toEqual(["on", "off"]);
});

test.describe("with FOREMAN_MCP=off", () => {
  test.use({ foremanEnv: { FOREMAN_MCP: "off" } });
  test("the page says so and offers no switch", async ({ page }) => {
    await createAdmin(page);
    await openTokens(page);
    await expect(page.locator("#mcp-state")).toContainText("FOREMAN_MCP=off");
    await expect(page.getByRole("button", { name: /MCP access/ })).toHaveCount(0);
    await checkPage(page);
  });
});

test("the REST API switch is its own control and turns access on and off", async ({ page, foreman }) => {
  await createAdmin(page);
  await openTokens(page);
  await expect(page.locator("#api-state")).toContainText("REST API access is off.");
  await page.getByRole("button", { name: "Turn on REST API access" }).click();
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Turned REST API access on.");
  await expect(page.locator("#api-state")).toContainText("REST API access is on.");
  await expect(page.locator("#mcp-state")).toContainText("MCP access is off.");
  await checkPage(page);
  await page.getByRole("button", { name: "Turn off REST API access" }).click();
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Turned REST API access off.");
  await expect(page.locator("#api-state")).toContainText("REST API access is off.");
  const rows = sql(foreman, "SELECT detail FROM audit_log WHERE action = 'api-set' ORDER BY id");
  expect(rows.map((r) => r.detail)).toEqual(["on", "off"]);
  expect(sql(foreman, "SELECT detail FROM audit_log WHERE action = 'mcp-set'")).toEqual([]);
});

test("the warning that a token will not work shows only while MCP and REST API access are both off", async ({ page }) => {
  const words = "Note: MCP and REST API access are both off, so this token will not work until an admin turns one on.";
  await createAdmin(page);
  await openTokens(page);
  await expect(page.locator("#token-access-off")).toHaveText(words);

  await page.getByRole("button", { name: "Turn on MCP access" }).click();
  await expect(page.locator("#mcp-state")).toContainText("MCP access is on.");
  await expect(page.locator("#token-access-off")).toHaveCount(0);
  await page.getByRole("button", { name: "Turn off MCP access" }).click();
  await expect(page.locator("#token-access-off")).toHaveText(words);

  await page.getByRole("button", { name: "Turn on REST API access" }).click();
  await expect(page.locator("#api-state")).toContainText("REST API access is on.");
  await expect(page.locator("#token-access-off")).toHaveCount(0);
  await create(page, "Script");
  await expect(page.locator("#access-off-note")).toHaveCount(0);

  await page.getByRole("link", { name: "Back to API tokens" }).click();
  await page.getByRole("button", { name: "Turn off REST API access" }).click();
  await expect(page.locator("#token-access-off")).toHaveText(words);
  await create(page, "Second");
  await expect(page.locator("#access-off-note")).toContainText(words);
  await expect(page.locator("#access-off-note").getByRole("link", { name: "Turn on MCP access" })).toBeVisible();
  await expect(page.locator("#access-off-note").getByRole("link", { name: "Turn on REST API access" })).toBeVisible();
  await checkPage(page);
});

test("the REST API switch has a visible name and a visible focus ring", { tag: "@looks" }, async ({ page }) => {
  await createAdmin(page);
  await openTokens(page);
  const button = page.getByRole("button", { name: "Turn on REST API access" });
  await expect(page.locator("#api-heading")).toContainText("REST API access");
  await button.focus();
  await expect(button).toBeFocused();
  expect(await button.evaluate((el) => getComputedStyle(el).outlineStyle)).not.toBe("none");
});

test("a new token's page shows both addresses, each with its own copy button", { tag: "@looks" }, async ({ page, context }) => {
  await createAdmin(page);
  await openTokens(page);
  await create(page, "Claude");
  await expect(page.locator("#token-endpoint")).toHaveValue(/^http:\/\/[^/]+\/mcp$/);
  await expect(page.locator("#token-api-base")).toHaveValue(/^http:\/\/[^/]+\/api\/v1$/);
  await expect(page.getByLabel("MCP endpoint")).toBeVisible();
  await expect(page.getByLabel("REST API base address")).toBeVisible();
  await checkPage(page);
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.getByRole("button", { name: "Copy REST API address" }).click();
  await expect(page.locator("#token-api-base-done")).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toMatch(/\/api\/v1$/);
  await page.getByRole("button", { name: "Copy MCP endpoint" }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toMatch(/\/mcp$/);
});

// The language sweep of i18n.spec.ts does not visit this page, so its two screens
// are checked here: the longest translations still fit at 320 px and pass axe.
for (const lang of languages) {
  test(`${lang}: the page and the created page fit and pass axe`, { tag: "@looks" }, async ({ page, foreman }) => {
    await page.setViewportSize({ width: 320, height: 640 });
    await createAdmin(page);
    await page.context().addCookies([{ name: "foreman_lang", value: lang, url: foreman.url }]);
    await page.goto("/settings/api-tokens");
    await checkPage(page);
    await page.locator("#token-name").fill("T-" + lang);
    await page.locator("#act-token-create").click();
    await expect(page.locator("#token-api-base")).toBeVisible();
    await checkPage(page);
  });
}

test.describe("with FOREMAN_API=off", () => {
  test.use({ foremanEnv: { FOREMAN_API: "off" } });
  test("the page says so and offers no REST API switch", async ({ page }) => {
    await createAdmin(page);
    await openTokens(page);
    await expect(page.locator("#api-state")).toContainText("FOREMAN_API=off");
    await expect(page.getByRole("button", { name: /REST API access/ })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Turn on MCP access" })).toBeVisible();
    await checkPage(page);
  });
});

// A done=revoked link shows a toast only for a token that really is revoked,
// so a crafted link cannot put its own words on the page.
test("a crafted revoked link shows no made-up name", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/settings/api-tokens?done=revoked&name=Your%20password%20was%20reset");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("API tokens");
  await expect(page.locator("#toasts > [data-toast]")).toHaveCount(0);
  await expect(page.locator("main")).not.toContainText("Your password was reset");
});
