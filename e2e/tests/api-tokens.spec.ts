import { test, expect, checkPage, createAdmin, mainNav } from "./foreman.js";
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

test("create a token, see the secret once, renew and revoke it", async ({ page, foreman }) => {
  await createAdmin(page);
  await openTokens(page);
  await expect(page.locator("#my-tokens-empty")).toBeVisible();
  await expect(page.getByRole("radio", { name: "Read" })).toBeChecked();
  await expect(page.locator("#scope-admin-warning")).toContainText("guards against mistakes");
  await checkPage(page);

  const secret = await create(page, "Claude");
  expect(secret).toMatch(/^fmn_[A-Za-z0-9_-]{43}$/);
  await expect(page.locator("#token-endpoint")).toHaveValue(/\/mcp$/);
  await expect(page.locator("#mcp-off-note")).toBeVisible();
  await checkPage(page);

  await page.getByRole("link", { name: "Back to API tokens" }).click();
  await expect(page.locator("#my-tokens")).toContainText("Claude");
  expect(await page.content()).not.toContain(secret);

  const id = Number(sql(foreman, "SELECT id FROM api_tokens WHERE name = 'Claude'")[0].id);
  const before = Number(sql(foreman, "SELECT period_start FROM api_tokens WHERE id = ?", id)[0].period_start);
  sql(foreman, "UPDATE api_tokens SET period_start = ? WHERE id = ?", before - 10 * 86400, id);
  await page.reload();
  await page.locator("#my-tokens").getByRole("button", { name: "Renew Claude" }).click();
  await expect(page.locator("#renewed-message")).toHaveText("Done: Renewed Claude. It works for another 30 days.");
  await expect(page.locator("#renewed-message")).toBeFocused();
  expect(Number(sql(foreman, "SELECT period_start FROM api_tokens WHERE id = ?", id)[0].period_start)).toBeGreaterThanOrEqual(before);

  await page.locator("#my-tokens").getByRole("link", { name: /^Revoke Claude/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Revoke Claude of admin?");
  await checkPage(page);
  await page.getByRole("button", { name: "Revoke token" }).click();
  await expect(page.locator("#done-message")).toContainText("Revoked Claude.");
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
  await expect(page).toHaveURL("/worlds?done=renewed&name=Claude");
  await expect(page.locator("#renewed-message")).toHaveText("Done: Renewed Claude. It works for another 30 days.");
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
  await expect(page.locator("#done-message")).toHaveText("Done: Turned MCP access on.");
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
