import { test, expect, createAdmin, mainNav } from "./foreman.js";

// SPEC 21.1, 21.2, 12.10, 14.19: a token made in the browser works at /mcp,
// its use shows on the page, a revoke ends it at once (401 naming revoked),
// and MCP off refuses every call with 403. The calls come from a request
// context of their own: no cookie and no Origin, as an MCP client sends.

const initialize = {
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "e2e", version: "1" } },
};

test("a token made on the page calls /mcp until it is revoked or MCP is off", async ({ page, foreman, playwright }) => {
  await createAdmin(page);
  await page.goto("/");
  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "API tokens" }).click();
  await page.getByRole("button", { name: "Turn on MCP access" }).click();
  await expect(page.locator("#mcp-state")).toContainText("MCP access is on.");

  await page.getByLabel("Name").fill("Claude");
  await page.getByRole("radio", { name: "Operate" }).check();
  await page.getByRole("button", { name: "Create token" }).click();
  const secret = await page.locator("#token-secret").inputValue();
  expect(secret).toMatch(/^fmn_[A-Za-z0-9_-]{43}$/);
  const endpoint = await page.locator("#token-endpoint").inputValue();
  expect(endpoint).toBe(foreman.url + "/mcp");

  const api = await playwright.request.newContext();
  try {
    const mcp = (token: string) =>
      api.post(endpoint, {
        headers: { Authorization: `Bearer ${token}`, Accept: "application/json, text/event-stream" },
        data: initialize,
      });

    const lastUsed = () =>
      page.locator("#my-tokens .token-card", { hasText: "Claude" }).locator("dt", { hasText: "Last used" }).locator("xpath=following-sibling::dd");
    await page.getByRole("link", { name: "Back to API tokens" }).click();
    await expect(lastUsed()).toHaveText("Never");

    const ok = await mcp(secret);
    expect(ok.status()).toBe(200);
    expect((await ok.json()).result.serverInfo.name).toBe("foreman");
    await page.reload();
    await expect(lastUsed()).not.toHaveText("Never");

    await page.locator("#my-tokens").getByRole("link", { name: /^Revoke Claude/ }).click();
    await page.getByRole("button", { name: "Revoke token" }).click();
    await expect(page.locator("#toasts .toast--success")).toContainText("Revoked Claude.");
    const revoked = await mcp(secret);
    expect(revoked.status()).toBe(401);
    expect(await revoked.text()).toContain("revoked");

    // A new token may take the revoked one's name, and works.
    await page.getByLabel("Name").fill("Claude");
    await page.getByRole("button", { name: "Create token" }).click();
    const fresh = await page.locator("#token-secret").inputValue();
    expect((await mcp(fresh)).status()).toBe(200);

    await page.getByRole("link", { name: "Back to API tokens" }).click();
    await page.getByRole("button", { name: "Turn off MCP access" }).click();
    await expect(page.locator("#mcp-state")).toContainText("MCP access is off.");
    for (const token of [fresh, secret, "fmn_nonsense"]) {
      const off = await mcp(token);
      expect(off.status()).toBe(403);
      expect(await off.text()).toContain("MCP access is off");
    }
  } finally {
    await api.dispose();
  }
  expect(foreman.logs()).not.toContain(secret);
});
