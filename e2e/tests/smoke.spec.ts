import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("the built binary answers its health path", async ({ request }) => {
  const resp = await request.get("/healthz");
  expect(resp.status()).toBe(200);
  expect(await resp.text()).toBe("ok\n");
});

// The health path is plain text, which axe cannot judge, so this proves the
// scanner runs in the pinned image. Every page Foreman serves gets its own scan (SPEC 17.1).
test("the accessibility scanner runs", async ({ page }) => {
  await page.setContent(
    '<!doctype html><html lang="en"><head><title>Foreman</title></head><body><main><h1>Foreman</h1></main></body></html>',
  );
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag22aa"]).analyze();
  expect(results.passes.length).toBeGreaterThan(0);
  expect(results.violations).toEqual([]);
});
