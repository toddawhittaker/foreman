import { test, expect, createAdmin, mainNav, seedWorld } from "./foreman.js";
import type { Page } from "@playwright/test";

// SPEC 23.1, 23.4, 23.8 and 12.10: a token made in the browser works on the
// REST API only while an admin has REST API access on, and is refused with 403
// as soon as it is turned off, from a request context with no cookie.

test.use({ fakeGameOn: true });

async function openTokens(page: Page) {
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "API tokens" }).click();
  await expect(page).toHaveURL("/settings/api-tokens");
}

test("a token reads the tool list and runs a tool while REST API access is on, and gets 403 once it is off", async ({ page, foreman, playwright }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await page.goto("/");
  await openTokens(page);

  // MCP stays off throughout: the two switches are separate.
  await page.getByRole("button", { name: "Turn on REST API access" }).click();
  await expect(page.locator("#toasts .toast--success")).toHaveText("Done: Turned REST API access on.");
  await expect(page.locator("#api-state")).toContainText("REST API access is on.");
  await expect(page.locator("#mcp-state")).toContainText("MCP access is off.");

  await page.getByLabel("Name").fill("Script");
  await page.getByRole("button", { name: "Create token" }).click();
  const secret = await page.locator("#token-secret").inputValue();
  const base = await page.locator("#token-api-base").inputValue();
  expect(base).toMatch(/\/api\/v1$/);

  const api = await playwright.request.newContext();
  try {
    const auth = { Authorization: `Bearer ${secret}` };

    const list = await api.get(`${base}/tools`, { headers: auth });
    expect(list.status()).toBe(200);
    const tools = (await list.json()).tools as { name: string }[];
    expect(tools.map((t) => t.name)).toContain("server_status");

    const call = await api.post(`${base}/tools/server_status`, { headers: auth, data: { world_id: "base" } });
    expect(call.status()).toBe(200);
    const answer = await call.json();
    expect(answer.result.world.id).toBe("base");
    expect(typeof answer.message).toBe("string");

    // Turned off, the same token gets 403 on both routes, at once.
    await page.goto("/settings/api-tokens");
    await page.getByRole("button", { name: "Turn off REST API access" }).click();
    await expect(page.locator("#api-state")).toContainText("REST API access is off.");
    expect((await api.get(`${base}/tools`, { headers: auth })).status()).toBe(403);
    expect((await api.post(`${base}/tools/server_status`, { headers: auth, data: { world_id: "base" } })).status()).toBe(403);
  } finally {
    await api.dispose();
  }
  expect(foreman.logs()).not.toContain(secret);
});
