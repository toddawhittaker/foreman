import { test, expect, checkPage, createAdmin, adminPassword, seedWorld, seedSecond, image21 } from "./foreman.js";
import type { APIRequestContext, BrowserContext } from "@playwright/test";
import { statSync } from "node:fs";
import { join } from "node:path";
import { catalogue, languages, pluralCategory, t, type Lang } from "./i18n.js";

// SPEC 22: the language follows a cookie, else Accept-Language, else English;
// counted text uses each language's plural forms; sizes use the language's
// decimal mark; every page holds at 320 px in the languages with long text.
// Text is chosen by catalogue key (tests/i18n.ts), never by wording.

const langCookie = "foreman_lang";

async function pageLang(request: APIRequestContext, headers: Record<string, string>, path = "/signed-out") {
  const resp = await request.get(path, { headers });
  expect(resp.status()).toBe(200);
  const body = await resp.text();
  return { lang: /<html lang="([^"]*)"/.exec(body)?.[1], body };
}

async function chooseLanguage(context: BrowserContext, lang: Lang, url: string) {
  await context.addCookies([{ name: langCookie, value: lang, url }]);
}

// SPEC 22.2: with no cookie, the best match for Accept-Language wins, else English.
test.describe("Accept-Language without a cookie", () => {
  const cases: [string, string, Lang][] = [
    ["a plain tag", "de", "de"],
    ["a region of a supported language", "de-AT,de;q=0.9,en;q=0.8", "de"],
    ["Russian", "ru-RU", "ru"],
    ["Chinese for mainland China is Simplified", "zh-CN", "zh-Hans"],
    ["Canadian French", "fr-CA", "fr"],
    ["Latin American Spanish", "es-419", "es"],
    ["the higher quality wins over the earlier tag", "pl;q=0.3,cs;q=0.8", "cs"],
    ["an unknown tag does not hide a later match", "xx, de;q=0.5", "de"],
    ["a malformed entry does not hide a valid one", "ru, !!!", "ru"],
    ["only unsupported tags", "pt-BR, ja", "en"],
    ["a wildcard", "*", "en"],
    ["an empty header", "", "en"],
  ];
  for (const [what, header, want] of cases) {
    test(`${what}: ${JSON.stringify(header)} shows ${want}`, async ({ request }) => {
      const { lang, body } = await pageLang(request, { "Accept-Language": header });
      expect(lang).toBe(want);
      expect(body).toContain(`<h1>${t("accounts.signedout.heading", {}, want)}</h1>`);
    });
  }
});

// SPEC 22.2: the menu's cookie overrides Accept-Language, including to English.
test("the cookie overrides Accept-Language for every language", async ({ request }) => {
  for (const want of languages) {
    const { lang, body } = await pageLang(request, { "Accept-Language": "de", Cookie: `${langCookie}=${want}` });
    expect(lang, `cookie ${want}`).toBe(want);
    expect(body, `cookie ${want}`).toContain(`<h1>${t("accounts.signedout.heading", {}, want)}</h1>`);
    expect(body, `cookie ${want}`).toMatch(new RegExp(`<option value="${want}"[^>]*selected`));
  }
});

// SPEC 22.2: a value that is not a supported language is ignored, not trusted.
test("a bad cookie value falls back to the Accept-Language match", async ({ request }) => {
  for (const bad of ["xx", "klingon", "zh-Hant", "%3Cscript%3Ealert(1)%3C%2Fscript%3E", "..%2F..%2Fetc%2Fpasswd", "", "de%00", "d".repeat(500)]) {
    const { lang, body } = await pageLang(request, { "Accept-Language": "fr", Cookie: `${langCookie}=${bad}` });
    expect(lang, `cookie ${JSON.stringify(bad)}`).toBe("fr");
    expect(body, `cookie ${JSON.stringify(bad)}`).not.toContain("<script>alert");
    expect(body, `cookie ${JSON.stringify(bad)}`).not.toContain("etc/passwd");
  }
  // With nothing else to go on, the page is English.
  const { lang } = await pageLang(request, { Cookie: `${langCookie}=klingon` });
  expect(lang).toBe("en");
});

// SPEC 22.2: the lang attribute names the language shown, in each language.
test("html lang, the menu and the page text agree in every language", async ({ page, foreman }) => {
  for (const lang of languages) {
    await chooseLanguage(page.context(), lang, foreman.url);
    await page.goto("/signed-out");
    await expect(page.locator("html")).toHaveAttribute("lang", lang);
    await expect(page.locator("#language")).toHaveValue(lang);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(t("accounts.signedout.heading", {}, lang));
    await expect(page.locator("label[for=language]")).toHaveText(t("common.language.label", {}, lang));
    expect(await page.title()).toContain(t("accounts.signedout.title", {}, lang));
  }
});

// SPEC 22.2: the menu returns to the page it was used on, signed in or not.
test.describe("the menu returns to the same page", () => {
  test("signed in, on a world page with a query string", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await page.goto("/worlds/base?x=1&y=two");
    await page.getByLabel(t("common.language.label")).selectOption("ru");
    await page.getByRole("button", { name: t("common.language.submit") }).click();

    await expect(page).toHaveURL("/worlds/base?x=1&y=two");
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
    // Still signed in, and the page is the world's page.
    await expect(page.getByRole("button", { name: t("common.signout", {}, "ru") })).toBeVisible();
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Base");

    // Back to English from the Russian page, by position since the words changed.
    await page.locator("#language").selectOption("en");
    await page.locator("form.language button").click();
    await expect(page).toHaveURL("/worlds/base?x=1&y=two");
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    await expect(page.getByRole("button", { name: t("common.signout") })).toBeVisible();
  });

  test("signed out, on the sign-in page with a destination, and the destination survives", async ({ page, foreman }) => {
    await createAdmin(page);
    await page.context().clearCookies();
    await page.goto("/signin?next=%2Fworlds");
    await page.getByLabel(t("common.language.label")).selectOption("cs");
    await page.getByRole("button", { name: t("common.language.submit") }).click();

    await expect(page).toHaveURL("/signin?next=%2Fworlds");
    await expect(page.locator("html")).toHaveAttribute("lang", "cs");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(t("accounts.signin.title", {}, "cs"));

    await page.getByLabel(t("accounts.username.label", {}, "cs")).fill("admin");
    await page.getByLabel(t("accounts.password.label", {}, "cs")).fill(adminPassword);
    await page.getByRole("button", { name: t("accounts.signin.submit", {}, "cs") }).click();
    await expect(page).toHaveURL("/worlds");
    await expect(page.locator("html")).toHaveAttribute("lang", "cs");
    expect((await page.context().cookies(foreman.url)).find((c) => c.name === langCookie)?.value).toBe("cs");
  });

  test("on the first-run setup page", async ({ page }) => {
    await page.goto("/setup");
    await page.getByLabel(t("common.language.label")).selectOption("pl");
    await page.getByRole("button", { name: t("common.language.submit") }).click();
    await expect(page).toHaveURL("/setup");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(t("accounts.setup.title", {}, "pl"));
  });

  // SPEC 22.2, 14.17: the menu never sends the browser to another site, and a
  // value that is not a language changes nothing.
  test("the menu refuses a foreign destination and an unknown language", async ({ request }) => {
    for (const dest of ["https://evil.example/x", "//evil.example/x", "/\\evil.example"]) {
      const resp = await request.post("/language", { form: { lang: "de", return: dest }, maxRedirects: 0 });
      expect(resp.status(), dest).toBe(303);
      const to = resp.headers()["location"] ?? "";
      expect(to.startsWith("/") && !to.startsWith("//") && !to.startsWith("/\\"), `${dest} went to ${to}`).toBe(true);
    }
    const bad = await request.post("/language", { form: { lang: "tlh", return: "/" }, maxRedirects: 0 });
    expect(bad.status()).toBe(400);
    expect(bad.headers()["set-cookie"] ?? "").not.toContain(langCookie);
  });
});

// SPEC 22.4: a message that counts has every plural form its language needs.
// The check is against the platform's CLDR, not Foreman's own plural code.
test.describe("plural forms", () => {
  test("the platform's rules say what the tests expect", () => {
    const want: Record<string, Record<number, string>> = {
      en: { 1: "one", 2: "other", 5: "other", 21: "other" },
      de: { 1: "one", 2: "other", 5: "other", 21: "other" },
      fr: { 0: "one", 1: "one", 2: "other", 5: "other", 21: "other" },
      es: { 1: "one", 2: "other", 5: "other", 21: "other" },
      ru: { 1: "one", 2: "few", 5: "many", 11: "many", 21: "one", 22: "few" },
      pl: { 1: "one", 2: "few", 5: "many", 12: "many", 21: "many", 22: "few" },
      cs: { 1: "one", 2: "few", 5: "other", 21: "other" },
      "zh-Hans": { 1: "other", 2: "other", 5: "other", 21: "other" },
    };
    for (const [lang, counts] of Object.entries(want)) {
      for (const [n, cat] of Object.entries(counts)) expect(pluralCategory(lang as Lang, Number(n)), `${lang} ${n}`).toBe(cat);
    }
  });

  // Covers every counting message in the catalogue, now and as pages are converted.
  for (const lang of languages) {
    test(`every counted message in ${lang} has exactly the forms whole numbers need`, () => {
      const needed = new Set<string>();
      for (let n = 0; n < 1000; n++) needed.add(pluralCategory(lang, n));
      let counted = 0;
      for (const [key, entry] of Object.entries(catalogue(lang))) {
        if (typeof entry === "string") continue;
        counted++;
        expect(Object.keys(entry).sort(), `${lang} ${key}`).toEqual([...needed].sort());
        // Only 1 may drop the number from its form ("a problem"); 2, 5 and 21 always show it.
        for (const n of [2, 5, 21]) {
          expect(entry[pluralCategory(lang, n)], `${lang} ${key} for ${n}`).toContain("{count}");
        }
      }
      expect(counted, "counted messages").toBeGreaterThan(0);
    });
  }

  const slavic: Lang[] = ["ru", "pl", "cs"];

  // The setup page's error summary counts the problems: 1, 2 and 3.
  for (const lang of slavic) {
    test(`${lang}: the error summary heading counts 1, 2 and 3 problems`, async ({ page, foreman }) => {
      await chooseLanguage(page.context(), lang, foreman.url);
      const short = "short";
      const attempts: [number, { username: string; password: string; confirm: string }][] = [
        [1, { username: "", password: adminPassword, confirm: adminPassword }],
        [2, { username: "", password: short, confirm: short }],
        [3, { username: "", password: short, confirm: "other" }],
      ];
      for (const [count, f] of attempts) {
        await page.goto("/setup");
        await page.locator("#username").fill(f.username);
        await page.locator("#password").fill(f.password);
        await page.locator("#confirm").fill(f.confirm);
        await page.locator("form.panel button[type=submit]").click();
        await expect(page.locator("#error-summary li")).toHaveCount(count);
        await expect(page.locator("#error-summary h2")).toHaveText(
          `${t("common.error.label", {}, lang)} ${t("common.summary.heading", { count }, lang)}`,
        );
        await checkPage(page);
      }
    });

    // The password hint counts characters; the number comes from the page.
    test(`${lang}: the password hint uses the plural form of the number it shows`, async ({ page, foreman }) => {
      await chooseLanguage(page.context(), lang, foreman.url);
      await page.goto("/setup");
      const shown = await page.locator("#password-hint").innerText();
      const count = Number(/\d+/.exec(shown)?.[0]);
      expect(count).toBeGreaterThan(1);
      expect(shown).toBe(t("accounts.setup.password.hint", { count }, lang));
    });

    // The rate-limit message counts minutes: about fifteen, the teens case.
    test(`${lang}: the rate-limit message uses the plural form of its minutes`, async ({ page, foreman }) => {
      await createAdmin(page);
      await page.context().clearCookies();
      await chooseLanguage(page.context(), lang, foreman.url);
      await page.goto("/signin");
      const attempt = async (password: string) => {
        await page.locator("#username").fill("admin");
        await page.locator("#password").fill(password);
        await page.locator("form.panel button[type=submit]").click();
      };
      for (let i = 0; i < 5; i++) await attempt(`wrong password ${i}`);
      await attempt(adminPassword);
      const shown = await page.locator("#error-summary").innerText();
      const count = Number(/\d+/.exec(shown)?.[0]);
      expect(count).toBeGreaterThan(0);
      expect(count).toBeLessThanOrEqual(15);
      expect(shown).toBe(`${t("common.error.label", {}, lang)} ${t("accounts.signin.ratelimited.minutes", { count }, lang)}`);
      await checkPage(page);
    });
  }
});

// SPEC 22.4: a size uses the language's decimal mark and its own unit text.
test.describe("sizes", () => {
  for (const lang of ["de", "fr"] as const) {
    test(`${lang}: a save's size has a decimal comma`, async ({ page, foreman }) => {
      await createAdmin(page);
      seedWorld(foreman);
      const bytes = statSync(join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata", "2.1.24-no-space-age.zip")).size;
      expect(bytes, "the fixture is in the kilobyte range").toBeGreaterThanOrEqual(10_000);
      expect(bytes).toBeLessThan(1_000_000);
      const kilo = (bytes / 1000).toFixed(1);

      await page.goto("/worlds/base/saves");
      await expect(page.locator("td.num").first()).toHaveText(t("common.bytes.kb", { n: kilo }));

      await chooseLanguage(page.context(), lang, foreman.url);
      await page.goto("/worlds/base/saves");
      const comma = kilo.replace(".", ",");
      await expect(page.locator("td.num").first()).toHaveText(t("common.bytes.kb", { n: comma }, lang));
      expect(comma).toContain(",");

      // The world page shows the whole folder's size the same way.
      await page.goto("/worlds/base");
      const unit = t("common.bytes.kb", { n: "NUM" }, lang).replace("NUM", "").trim();
      await expect(page.locator("dl.facts dd").last()).toHaveText(new RegExp(`^\\d+,\\d\\s*${unit}$`));
    });
  }
});

// SPEC 22.7: every page meets 17.1 and 17.2 in the languages with the longest
// text. checkPage scans with axe and, at 320 px, fails on horizontal scroll,
// a wrapped date or size, and a target under 24 px.
test.describe("layout at 320 px in the longest languages", () => {
  test.use({ fakeGameOn: true });

  for (const lang of ["de", "ru"] as const) {
    test(`${lang}: the main pages and the sign-in page fit and pass axe`, async ({ page, foreman, fakeGame }) => {
      test.setTimeout(90_000);
      const live = { timeout: 20_000 };
      await page.setViewportSize({ width: 320, height: 640 });

      // Set up in English with the real controls, then read the pages in lang.
      await createAdmin(page);
      seedWorld(foreman);
      seedSecond(foreman);
      await fakeGame!.call("/image", { ref: image21, on: true });
      await page.goto("/");
      await page.getByRole("button", { name: "Start server" }).click();
      await expect(page.getByRole("region", { name: "Status" }).locator(".state")).toHaveText("Running", live);
      await fakeGame!.call("/join", { name: "foreman-base", player: "alice" });
      await expect(page.getByRole("cell", { name: "alice" })).toBeVisible(live);
      await page.goto("/worlds/base/snapshots");
      await page.getByLabel("Label (optional)").fill("Before the big rocket launch rebuild");
      await page.getByRole("button", { name: "Take snapshot" }).click();
      await expect(page.getByText(/^Done: Foreman took the snapshot/)).toBeVisible();

      await chooseLanguage(page.context(), lang, foreman.url);
      for (const path of ["/", "/worlds", "/worlds/base", "/worlds/base/saves", "/snapshots", "/worlds/base/snapshots"]) {
        await page.goto(path);
        await expect(page.locator("html"), path).toHaveAttribute("lang", lang);
        await checkPage(page);
      }

      // The sign-in page, plain and at its longest (the rate-limit message).
      await page.context().clearCookies();
      await chooseLanguage(page.context(), lang, foreman.url);
      await page.goto("/signin");
      await expect(page.locator("html")).toHaveAttribute("lang", lang);
      await checkPage(page);
      for (let i = 0; i < 6; i++) {
        await page.locator("#username").fill("admin");
        await page.locator("#password").fill(i < 5 ? `wrong password ${i}` : adminPassword);
        await page.locator("form.panel button[type=submit]").click();
      }
      await expect(page.locator("#error-summary")).toContainText(t("common.error.label", {}, lang));
      await checkPage(page);
    });
  }
});
