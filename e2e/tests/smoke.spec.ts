import { test, expect } from "./foreman.js";

test("the built binary answers its health path", async ({ request }) => {
  const resp = await request.get("/healthz");
  expect(resp.status()).toBe(200);
  expect(await resp.text()).toBe("ok\n");
});

// The page's icon links resolve, and the root /favicon.ico answers, with no
// session.
test("the icon links and /favicon.ico are served", async ({ page, request }) => {
  await page.goto("/");
  const hrefs = await page.locator('head link[rel="icon"], head link[rel="apple-touch-icon"]').evaluateAll((links) => links.map((l) => (l as HTMLLinkElement).href));
  expect(hrefs).toHaveLength(3);
  for (const href of [...hrefs, new URL("/favicon.ico", page.url()).href]) {
    const resp = await request.get(href);
    expect(resp.status(), href).toBe(200);
    expect(resp.headers()["content-type"], href).toMatch(/^image\//);
  }
});
