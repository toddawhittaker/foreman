import { test, expect, createAdmin, seedWorld, image21 } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

// SPEC 22.3: the text the scripts make comes from the catalogue, in the
// page's language. The admin is created in English, then the language cookie
// switches the pages; each test checks the scripts' own text only, so it
// does not depend on how far the pages around them are translated.

test.use({ fakeGameOn: true });

const live = { timeout: 20_000 };
const fixtures = join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata");

async function speak(page: Page, tag: string) {
  await page.context().addCookies([{ name: "foreman_lang", value: tag, url: page.url() }]);
}

// Adds a countdown and a time, which the scripts pick up as they do on the
// Server page: the countdown on its next tick, the time on a settled swap.
async function addCountdownAndTime(page: Page, seconds: number, daysAgo: number) {
  await page.evaluate(
    ([sec, days]) => {
      const until = document.createElement("span");
      until.id = "test-countdown";
      until.dataset.until = new Date(Date.now() + sec * 1000).toISOString();
      const time = document.createElement("time");
      time.id = "test-time";
      time.setAttribute("datetime", new Date(Date.now() - days * 86_400_000).toISOString());
      time.textContent = "UTC";
      document.body.append(until, time);
      document.dispatchEvent(new Event("htmx:afterSettle"));
    },
    [seconds, daysAgo],
  );
}

test.describe("in German", () => {
  test("the countdown, the age of a time and the live note speak German", async ({ page, foreman, fakeGame }) => {
    await createAdmin(page);
    seedWorld(foreman);
    await fakeGame!.call("/image", { ref: image21, on: true });
    await speak(page, "de");

    await page.goto("/");
    await expect(page.locator("html")).toHaveAttribute("lang", "de");
    await addCountdownAndTime(page, 30.5, 3);
    await expect(page.locator("#test-countdown")).toHaveText(/^noch \d+ Sekunden$/, live);
    await expect(page.locator("#test-time")).toContainText("(vor 3 Tagen)");

    // The note shows when the log stream cannot connect.
    await page.route("**/worlds/base/console/events", (route) => route.fulfill({ status: 503, body: "" }));
    await page.goto("/console");
    await expect(page.locator("#live-lost")).toHaveText(
      "Hinweis: Live-Aktualisierungen wurden beendet. Lade die Seite neu, um den aktuellen Stand zu sehen.",
      live,
    );
    await expect(page.locator("#announcer")).toHaveText("Live-Aktualisierungen wurden beendet. Lade die Seite neu, um den aktuellen Stand zu sehen.");
  });

  test("choosing an import folder says the suggestion in German", async ({ page, foreman }) => {
    await createAdmin(page);
    mkdirSync(join(foreman.dataDir, "import", "one"), { recursive: true });
    copyFileSync(join(fixtures, "2.1.24-space-age.zip"), join(foreman.dataDir, "import", "one", "a.zip"));
    mkdirSync(join(foreman.dataDir, "import", "two"), { recursive: true });
    copyFileSync(join(fixtures, "2.0.77-no-space-age.zip"), join(foreman.dataDir, "import", "two", "a.zip"));

    await speak(page, "de");
    await page.goto("/import");
    const first = page.locator('input[name="folder"]').nth(0);
    const second = page.locator('input[name="folder"]').nth(1);
    const same = page.locator("#folder-version-select option").first();

    await first.check();
    await expect(same).toHaveText("Wie im Speicherstand (2.1.24)");
    await expect(page.locator("#announcer")).toHaveText("Weltname vorgeschlagen: one.");

    // Two ticked: the several-folders sentence is read out.
    await second.check();
    await expect(same).toHaveText("Wie im Speicherstand");
    await expect(page.locator("#announcer")).toHaveText(/^Mehrere Ordner ausgewählt: /);
  });
});

test.describe("in Russian", () => {
  // 25 and 26 seconds both take the "many" form.
  test("the countdown picks the Russian plural form", async ({ page }) => {
    await createAdmin(page);
    await speak(page, "ru");
    await page.goto("/");
    await expect(page.locator("html")).toHaveAttribute("lang", "ru");
    await addCountdownAndTime(page, 25.5, 3);
    await expect(page.locator("#test-countdown")).toHaveText(/^осталось \d+ секунд$/, live);
    await expect(page.locator("#test-time")).toContainText("(3 дня назад)");
  });
});
