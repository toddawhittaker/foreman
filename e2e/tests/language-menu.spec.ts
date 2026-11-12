import { test, expect, checkPage, createAdmin, openLanguages, pickLanguage, signOut } from "./foreman.js";

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
  // The menu's summary names the language shown, and so does its language
  // row, which lists the seven others.
  await expect(page.locator("details.menu > summary .menu__lang")).toHaveText("Deutsch");
  await openLanguages(page);
  await expect(page.locator("details.menu__langs > summary")).toHaveAccessibleName("Sprache (Language) Deutsch");
  await expect(page.locator("details.menu__langs").getByRole("button")).toHaveCount(7);
  await expect(page.getByRole("button", { name: "Deutsch" })).toHaveCount(0);
  await checkPage(page);

  await page.goto("/no-such-page");
  await expect(page.locator("html")).toHaveAttribute("lang", "de");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Seite nicht gefunden");

  await page.goto("/signed-out");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Du bist abgemeldet");
});

// The menu and its language row are details elements and the languages
// plain buttons: it works by keyboard alone.
test("the language menu works from the keyboard", async ({ page }) => {
  await createAdmin(page);
  await signOut(page);
  await page.goto("/signin");
  await page.locator("details.menu > summary").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("details.menu")).toHaveAttribute("open", "");
  await page.keyboard.press("Tab");
  await expect(page.locator("details.menu__langs > summary")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("details.menu__langs")).toHaveAttribute("open", "");
  for (let i = 0; i < 7; i++) await page.keyboard.press("Tab");
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
