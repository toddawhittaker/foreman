import { test, expect, checkPage, createAdmin, openMenu, signOut, idpUser, idpSubject } from "./foreman.js";
import { DatabaseSync } from "node:sqlite";
import { join } from "node:path";

// SPEC 12.7: single sign-on, against the fake identity provider over https (cmd/fakeidp). The browser
// accepts the fake's test certificate; Foreman is given it through SSL_CERT_FILE (tests/foreman.ts).
// Every page visited is checked in both themes, at desktop width and at 320 px, by the four projects.
const button = "Sign in with single sign-on";

test.describe("single sign-on alone", () => {
  test.use({ fakeIdPOn: true, ignoreHTTPSErrors: true, foremanEnv: { FOREMAN_AUTH_METHODS: "oidc" } });

  test("the sign-in page is one button, and signing in lands on the page asked for", async ({ page, fakeIdP }) => {
    await page.goto("/worlds");
    await expect(page).toHaveURL("/signin?next=%2Fworlds");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Sign in");
    await expect(page.getByRole("button", { name: button })).toBeVisible();
    // No password form, no setup link, no second heading.
    await expect(page.getByLabel("Password")).toHaveCount(0);
    await expect(page.getByLabel("Username")).toHaveCount(0);
    await expect(page.locator('a[href="/setup"]')).toHaveCount(0);
    await expect(page.getByRole("heading", { level: 2 })).toHaveCount(0);
    await checkPage(page);

    await page.getByRole("button", { name: button }).click();
    await expect(page).toHaveURL("/worlds");
    await openMenu(page);
    await expect(page.getByText(`Signed in as ${idpUser}`)).toBeVisible();
    await checkPage(page);

    const seen = (await fakeIdP!.state()).authorizes;
    expect(seen).toHaveLength(1);
    expect(seen[0].scope).toBe("openid profile email groups");
    expect(seen[0].code_challenge_method).toBe("S256");
    expect(seen[0].has_nonce).toBe(true);
  });

  test("the signed-out page signs in again with one click", async ({ page }) => {
    await page.goto("/signin");
    await page.getByRole("button", { name: button }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Server");
    await signOut(page);
    await expect(page).toHaveURL("/signed-out");
    await expect(page.getByRole("link", { name: "Sign in again" })).toHaveCount(0);
    await checkPage(page);
    await page.getByRole("link", { name: button }).click();
    await expect(page).toHaveURL("/");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Server");
  });

  test("a user outside the required group is refused, with a way back", async ({ page, fakeIdP }) => {
    await fakeIdP!.setNext({ groups: ["staff"] });
    await page.goto("/signin");
    const answer = page.waitForResponse((r) => r.url().includes("/signin/oidc/callback"));
    await page.getByRole("button", { name: button }).click();
    expect((await answer).status()).toBe(403);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Single sign-on did not work");
    await expect(page.getByText("Your account is not allowed to use Foreman. Ask the person who runs Foreman to give you access.")).toBeVisible();
    await expect(page.getByText("Signed in as")).toHaveCount(0);
    await checkPage(page);
    // The next step is a control: it leads back to sign-in, not to a dead end.
    await page.getByRole("link", { name: "Back to sign in" }).click();
    await expect(page.getByRole("button", { name: button })).toBeVisible();
  });

  test("a username Foreman cannot use says trying again will not help", async ({ page, fakeIdP }) => {
    await fakeIdP!.setNext({ preferred_username: "x".repeat(65) });
    await page.goto("/signin");
    await page.getByRole("button", { name: button }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Single sign-on did not work");
    await expect(page.getByText("trying again will not help")).toBeVisible();
    await checkPage(page);
  });

  test("a provider that is down gives the unavailable page", async ({ page, fakeIdP }) => {
    await fakeIdP!.kill();
    await page.goto("/signin");
    const answer = page.waitForResponse((r) => new URL(r.url()).pathname === "/signin/oidc");
    await page.getByRole("button", { name: button }).click();
    expect((await answer).status()).toBe(503);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Single sign-on did not work");
    await expect(page.getByText("Foreman cannot reach the single sign-on provider right now. Try again in a minute.")).toBeVisible();
    await expect(page.getByRole("link", { name: "Try single sign-on again" })).toHaveAttribute("href", "/signin/oidc");
    await checkPage(page);
  });

  test("too many starts from one address give a wait page", async ({ page }) => {
    // Each address may start 20 sign-ins a minute (SPEC 12.7).
    for (let i = 0; i < 20; i++) {
      const r = await page.request.get("/signin/oidc", { maxRedirects: 0 });
      expect(r.status()).toBe(303);
    }
    await page.goto("/signin");
    const answer = page.waitForResponse((r) => new URL(r.url()).pathname === "/signin/oidc");
    await page.getByRole("button", { name: button }).click();
    expect((await answer).status()).toBe(429);
    await expect(page.getByText(/Wait \d+ minutes?, then try again\./)).toBeVisible();
    await checkPage(page);
  });
});

test.describe("single sign-on beside a password", () => {
  test.use({ fakeIdPOn: true, ignoreHTTPSErrors: true, foremanEnv: { FOREMAN_AUTH_METHODS: "local,oidc" } });

  test("the sign-in page has both, clearly separated, and each works", async ({ page }) => {
    await createAdmin(page);
    await signOut(page);
    await page.goto("/signin?next=%2Fworlds");
    await expect(page.getByRole("heading", { level: 2 })).toHaveText(["Single sign-on", "Foreman account"]);
    await expect(page.getByRole("button", { name: button })).toBeVisible();
    await expect(page.getByLabel("Username")).toBeVisible();
    await checkPage(page);

    // A wrong password keeps single sign-on on offer.
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password").fill("not the right password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("button", { name: button })).toBeVisible();
    await checkPage(page);

    await page.getByRole("button", { name: button }).click();
    await expect(page).toHaveURL("/worlds");
    await openMenu(page);
    await expect(page.getByText(`Signed in as ${idpUser}`)).toBeVisible();
  });

  test("the signed-out page offers both ways", async ({ page }) => {
    await createAdmin(page);
    await signOut(page);
    await expect(page).toHaveURL("/signed-out");
    await expect(page.getByRole("link", { name: button })).toBeVisible();
    await expect(page.getByRole("link", { name: "Sign in again" })).toBeVisible();
    await checkPage(page);
  });

  test("a name that belongs to a password user is refused, and says what to do", async ({ page, fakeIdP }) => {
    await createAdmin(page);
    await signOut(page);
    await fakeIdP!.setNext({ preferred_username: "admin" });
    await page.goto("/signin");
    const answer = page.waitForResponse((r) => r.url().includes("/signin/oidc/callback"));
    await page.getByRole("button", { name: button }).click();
    expect((await answer).status()).toBe(403);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Single sign-on did not work");
    await expect(page.getByText("Ask the person who runs Foreman to rename or remove that user.")).toBeVisible();
    await expect(page.getByText("Signed in as")).toHaveCount(0);
    await checkPage(page);
  });
});

test.describe("single sign-on beside trusted headers", () => {
  test.use({
    fakeGameOn: true,
    fakeIdPOn: true,
    ignoreHTTPSErrors: true,
    foremanEnv: {
      FOREMAN_AUTH_METHODS: "headers,oidc",
      FOREMAN_AUTH_USER_HEADER: "X-Test-User",
      FOREMAN_AUTH_GROUPS_HEADER: "X-Test-Groups",
      FOREMAN_AUTH_REQUIRED_GROUP: "factorio",
      FOREMAN_TRUSTED_PROXIES: "127.0.0.1/32",
    },
  });

  // SPEC 12.7: a user made by trusted headers is linked once, on the first sign-in by name; after that the
  // link is by subject, so another subject with the same name is a clash.
  test("a header user is linked once", async ({ page, request, foreman, fakeIdP }) => {
    const headers = { "X-Test-User": idpUser, "X-Test-Groups": "factorio" };
    expect((await request.get("/worlds", { headers })).status()).toBe(200);
    const users = () => {
      const db = new DatabaseSync(join(foreman.dataDir, "foreman.db"), { readOnly: true });
      try {
        return db.prepare("SELECT username, oidc_subject AS sub FROM users ORDER BY id").all() as { username: string; sub: string | null }[];
      } finally {
        db.close();
      }
    };
    expect(users()).toEqual([{ username: idpUser, sub: null }]);

    // Without the header the visitor goes to the sign-in page, which offers only single sign-on.
    await page.goto("/worlds");
    await expect(page).toHaveURL("/signin?next=%2Fworlds");
    await expect(page.getByLabel("Password")).toHaveCount(0);
    await checkPage(page);
    await page.getByRole("button", { name: button }).click();
    await expect(page).toHaveURL("/worlds");
    await openMenu(page);
    await expect(page.getByText(`Signed in as ${idpUser}`)).toBeVisible();
    expect(users()).toEqual([{ username: idpUser, sub: idpSubject }]);

    // Signing in again finds the same user.
    await signOut(page);
    await page.getByRole("link", { name: button }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Server");
    expect(users()).toHaveLength(1);

    // The name is now linked to one subject; a different person with it is refused.
    await signOut(page);
    await fakeIdP!.setNext({ sub: "another-subject" });
    await page.getByRole("link", { name: button }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Single sign-on did not work");
    expect(users()).toEqual([{ username: idpUser, sub: idpSubject }]);
    await checkPage(page);
  });
});
