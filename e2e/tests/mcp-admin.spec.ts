import { test, expect, createAdmin, mainNav, seedWorld, seedSecond, image21, live, stateBadge, doneBox } from "./foreman.js";
import type { APIRequestContext } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// SPEC 18.10b, 21.7, 21.8: an admin token made in the browser drives the
// two-step admin tools at /mcp, from a request context with no cookie. After
// a preview call the pages show nothing changed; after the confirm call they
// show the effect: a restore on the Server page and its "Before restore"
// snapshot on the Snapshots tab, and a deleted world gone from Worlds.

test.use({ fakeGameOn: true });

type ToolResult = { isError?: boolean; structuredContent?: Record<string, unknown>; content?: { text?: string }[] };

let nextID = 1;

async function callTool(api: APIRequestContext, endpoint: string, token: string, name: string, args: Record<string, unknown>): Promise<ToolResult> {
  const res = await api.post(endpoint, {
    headers: { Authorization: `Bearer ${token}`, Accept: "application/json, text/event-stream" },
    data: { jsonrpc: "2.0", id: nextID++, method: "tools/call", params: { name, arguments: args } },
  });
  expect(res.status()).toBe(200);
  const body = await res.json();
  expect(body.error, JSON.stringify(body.error)).toBeUndefined();
  return body.result as ToolResult;
}

const text = (r: ToolResult) => (r.content ?? []).map((c) => c.text ?? "").join("");

test("admin tools change nothing on the preview and show their effect after the confirm", async ({ page, foreman, fakeGame, playwright }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(stateBadge(page)).toHaveText("Stopped", live);
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(doneBox(page)).toContainText("Manual snapshot", live);

  await mainNav(page).getByRole("link", { name: "Settings" }).click();
  await page.getByRole("navigation", { name: "Foreman settings" }).getByRole("link", { name: "API tokens" }).click();
  await page.getByRole("button", { name: "Turn on MCP access" }).click();
  await expect(page.locator("#mcp-state")).toContainText("MCP access is on.");
  await page.getByLabel("Name").fill("Claude");
  await page.getByRole("radio", { name: "Admin" }).check();
  await page.getByRole("button", { name: "Create token" }).click();
  const secret = await page.locator("#token-secret").inputValue();
  const endpoint = await page.locator("#token-endpoint").inputValue();

  const api = await playwright.request.newContext();
  try {
    const call = (name: string, args: Record<string, unknown>) => callTool(api, endpoint, secret, name, args);
    const snapshots = (await call("list_snapshots", { world_id: "base" })).structuredContent!.snapshots as { name: string; untrusted_label: string }[];
    const manual = snapshots.find((s) => s.untrusted_label === "Manual snapshot");
    expect(manual, JSON.stringify(snapshots)).toBeTruthy();

    // Restore: the preview leaves the Snapshots tab as it was.
    const restoreArgs = { world_id: "base", snapshot: manual!.name };
    const preview = await call("restore_snapshot", restoreArgs);
    expect(preview.isError, text(preview)).toBeFalsy();
    expect(preview.structuredContent!.done).toBe(false);
    expect(preview.structuredContent!.snapshot).toBe("Before restore");
    await page.goto("/worlds/base/snapshots");
    await expect(page.getByText("Manual snapshot").first()).toBeVisible();
    await expect(page.getByText("Before restore")).toHaveCount(0);

    // The confirm restores; the Server page shows it, the tab its snapshot.
    const done = await call("restore_snapshot", { ...restoreArgs, confirm_code: preview.structuredContent!.confirm_code });
    expect(done.isError, text(done)).toBeFalsy();
    expect(done.structuredContent!.done).toBe(true);
    await page.goto("/");
    await expect(doneBox(page)).toContainText("The world is restored.", live);
    await page.goto("/worlds/base/snapshots");
    await expect(page.getByText("Before restore").first()).toBeVisible();

    // Delete: the preview leaves Second on the Worlds list.
    const delPreview = await call("delete_world", { world_id: "second" });
    expect(delPreview.isError, text(delPreview)).toBeFalsy();
    expect(delPreview.structuredContent!.snapshot).toBe("Before delete");
    await page.goto("/worlds");
    await expect(page.getByRole("link", { name: "Second", exact: true })).toBeVisible();

    const deleted = await call("delete_world", { world_id: "second", confirm_code: delPreview.structuredContent!.confirm_code });
    expect(deleted.isError, text(deleted)).toBeFalsy();
    expect(deleted.structuredContent!.done).toBe(true);
    await page.goto("/worlds");
    await expect(page.getByRole("link", { name: "Second", exact: true })).toHaveCount(0);

    // No page shows the audit log, so the row is read from the database.
    const db = new DatabaseSync(join(foreman.dataDir, "foreman.db"));
    try {
      const rows = db.prepare("SELECT world_id, token_name FROM audit_log WHERE action = 'world-delete'").all() as { world_id: string; token_name: string }[];
      expect(rows).toEqual([{ world_id: "second", token_name: "Claude" }]);
    } finally {
      db.close();
    }
  } finally {
    await api.dispose();
  }
  expect(foreman.logs()).not.toContain(secret);
});
