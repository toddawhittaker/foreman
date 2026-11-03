import { test, expect, checkPage } from "./foreman.js";

// SPEC 12.5: a trusted proxy's identity header signs the user in. The
// browser connects from 127.0.0.1, which is the trusted range here; the
// untrusted case cannot be produced from a browser and is covered in Go
// (internal/web/security_test.go).
test.use({
  foremanEnv: {
    FOREMAN_AUTH_METHODS: "headers",
    FOREMAN_AUTH_USER_HEADER: "X-Test-User",
    FOREMAN_TRUSTED_PROXIES: "127.0.0.1/32",
  },
});

test("a trusted header signs the user in", async ({ page }) => {
  await page.setExtraHTTPHeaders({ "X-Test-User": "alice" });
  await page.goto("/");
  await expect(page).toHaveURL("/");
  await expect(page.getByText("Signed in as alice")).toBeVisible();
  await checkPage(page);
});

test("without the header, home is refused and there is no setup page", async ({ page }) => {
  const home = await page.goto("/");
  expect(home?.status()).toBe(403);
  await checkPage(page);
  const setup = await page.goto("/setup");
  expect(setup?.status()).toBe(404);
});
