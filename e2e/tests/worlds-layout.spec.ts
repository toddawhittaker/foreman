import { test, expect, checkPage, createAdmin, pickLanguage } from "./foreman.js";
import { mkdirSync, utimesSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Issue 759: on the world list, Last played wraps between its date, clock
// and age, and never runs into Disk use (DESIGN 8.2). The projects run this
// in both themes; it sets its own widths, so the phone projects skip it.
// The zone is UTC, so the text, and so its width, is the same on every machine.
test.use({ fakeGameOn: true, timezoneId: "UTC" });

// Where the visible Last played text ends, and where its cell and the Disk
// use text are, for every row of the list.
async function measure(page: import("@playwright/test").Page) {
  return page.locator("ul.worlds > li > .facts").evaluateAll((lists) =>
    lists.map((dl) => {
      const cells = dl.querySelectorAll(":scope > div");
      const time = cells[2].querySelector("time")!;
      const rects = [...time.getClientRects()].filter((r) => r.width > 0);
      const disk = cells[3].querySelector("dd")!;
      const diskText = document.createRange();
      diskText.selectNodeContents(disk);
      const diskRect = [...diskText.getClientRects()].filter((r) => r.width > 0);
      const cell = cells[2].getBoundingClientRect();
      return {
        text: time.innerText,
        age: time.querySelector(".time-short")?.textContent ?? "",
        // Lines the age takes in the full form, 0 while the short form shows.
        ageLines: (() => {
          const age = time.querySelector(".time-full > :last-child")!;
          const box = age.getBoundingClientRect();
          return box.width ? Math.round(box.height / parseFloat(getComputedStyle(age).lineHeight)) : 0;
        })(),
        timeRight: Math.max(...rects.map((r) => r.right)),
        timeTop: Math.min(...rects.map((r) => r.top)),
        timeBottom: Math.max(...rects.map((r) => r.bottom)),
        cellRight: cell.right,
        diskLeft: Math.min(...diskRect.map((r) => r.left)),
        diskTop: Math.min(...diskRect.map((r) => r.top)),
        diskBottom: Math.max(...diskRect.map((r) => r.bottom)),
      };
    }),
  );
}

// The phone, the stacked list's widths, either side of the table's 64rem
// start, where the full text returns, and wide.
const widths = [320, 390, 768, 1023, 1024, 1150, 1440, 1920];

// WCAG 1.4.12: text spaced out this far still fits.
const spacing = "* { letter-spacing: 0.12em !important; word-spacing: 0.16em !important; line-height: 1.5 !important; }";

const cases = [
  ...["en", "de", "ru", "zh-Hans", "fr", "es", "pl", "cs"].map((lang) => ({ lang, spaced: false, widths })),
  ...["de", "ru"].map((lang) => ({ lang, spaced: true, widths: [1024, 1100, 1150, 1280] })),
];

// All eight languages: German, Russian and Polish are the long ones.
for (const { lang, spaced, widths } of cases) {
  test(`Last played stays clear of Disk use at every width (${lang}${spaced ? ", text spacing" : ""})`, async ({ page, foreman }, info) => {
    test.skip(info.project.name.startsWith("phone"), "sets its own widths");
    await createAdmin(page);
    if (lang !== "en") await pickLanguage(page, lang);
    // Played 3 minutes ago, and last year, whose date also names the year.
    for (const [id, name, played] of [
      ["alpha", "Alpha", new Date(Date.now() - 3 * 60_000)],
      ["beta-base", "A rather long world name for the column", new Date(new Date().getFullYear() - 1, 10, 21, 16, 1)],
    ] as const) {
      const dir = join(foreman.dataDir, "worlds", id);
      for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
      writeFileSync(join(dir, "world.json"), JSON.stringify({ format_version: 1, name, game_version: "2.1.24", space_age: true, created_at: "2026-11-01T10:00:00Z" }));
      const save = join(dir, "saves", "base.zip");
      writeFileSync(save, Buffer.alloc(2_100_000));
      utimesSync(save, played, played);
    }
    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto("/worlds");
      await expect(page.locator("ul.worlds time[data-local]")).toHaveCount(2);
      if (spaced) await page.addStyleTag({ content: spacing });
      const wide = width >= 1024;
      for (const row of await measure(page)) {
        const where = `${lang} at ${width} px: "${row.text}"`;
        // A real age is shown, in the full or the short form.
        expect(row.age, where).not.toBe("");
        expect(row.text, where).toContain(row.age);
        // On the table the full form breaks only between date, clock and age.
        if (wide && !spaced) expect(row.ageLines, where).toBeLessThanOrEqual(1);
        // Where the row has room, the date and clock show as before.
        if (width >= 1440 && !spaced) expect(row.text, where).toContain("UTC");
        // The text never spills out of its own cell.
        expect(row.timeRight, where).toBeLessThanOrEqual(row.cellRight + 0.5);
        // Side by side, Disk use starts at least 24 px after it ends.
        const sideBySide = row.timeTop < row.diskBottom && row.diskTop < row.timeBottom;
        if (wide) expect(sideBySide, where).toBe(true);
        if (sideBySide && wide) expect(row.diskLeft - row.timeRight, where).toBeGreaterThanOrEqual(24);
      }
    }
    await checkPage(page);
  });
}
