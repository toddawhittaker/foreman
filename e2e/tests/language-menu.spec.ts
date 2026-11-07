import { test, expect, checkPage, createAdmin, openMenu, pickLanguage, signOut } from "./foreman.js";

// SPEC 22.2: the menu on the sign-in page switches the language, the html
// lang follows, and the choice holds on the next pages.
test("the language menu switches to German and the choice persists", async ({ page }) => {
  await createAdmin(page);
  await signOut(page);
  await page.goto("/signin");
  await expect(page.locator("html")).toHaveAttribute("lang", "en");

  await pickLanguage(page, "de");

  await expect(page).toHaveURL("/signin");
  await expect(page.locator("html")).toHaveAttribute("lang", "de");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Anmelden");
  // The menu's summary names the language shown, and its list marks it.
  await expect(page.locator("details.menu > summary .menu__lang")).toHaveText("Deutsch");
  await openMenu(page);
  await expect(page.getByRole("group", { name: "Sprache (Language)" }).getByRole("button", { name: "Deutsch" })).toHaveAttribute("aria-current", "true");
  await checkPage(page);

  await page.goto("/no-such-page");
  await expect(page.locator("html")).toHaveAttribute("lang", "de");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Seite nicht gefunden");

  await page.goto("/signed-out");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Du bist abgemeldet");
});

// The menu is a details element and plain buttons: it works by keyboard alone.
test("the language menu works from the keyboard", async ({ page }) => {
  await createAdmin(page);
  await signOut(page);
  await page.goto("/signin");
  await page.locator("details.menu > summary").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("details.menu")).toHaveAttribute("open", "");
  for (let i = 0; i < 8; i++) await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Čeština" })).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("html")).toHaveAttribute("lang", "cs");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Přihlášení");
});

test.describe("with a German browser", () => {
  test.use({ locale: "de-DE" });

  // SPEC 22.2: with no cookie, Accept-Language picks the language.
  test("Accept-Language picks German", async ({ page }) => {
    await page.goto("/setup");
    await expect(page.locator("html")).toHaveAttribute("lang", "de");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Das erste Admin-Konto anlegen");
    await checkPage(page);
    expect((await page.context().cookies()).some((c) => c.name === "foreman_lang")).toBe(false);
  });
});
