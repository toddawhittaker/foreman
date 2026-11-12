import { test, expect, checkPage, createAdmin, seedSecond, seedWorld } from "./foreman.js";
import type { Page } from "@playwright/test";

// Taking and restoring a snapshot asks Docker whether the game is running.
test.use({ fakeGameOn: true });

async function takeSnapshot(page: Page, label: string) {
  await page.goto("/worlds/second/snapshots");
  await page.getByLabel("Label (optional)").fill(label);
  await page.getByRole("button", { name: "Take snapshot" }).click();
  await expect(page.getByText(`Done: Foreman took the snapshot "${label}".`)).toBeVisible();
}

// SPEC 17.4: Select all automatic reloads the page, so it says what it ticked
// and puts focus on that line.
test("Select all automatic says what it ticked and moves focus to it", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  seedSecond(foreman); // not active, so a restore ends on its Snapshots page
  await takeSnapshot(page, "Only a manual one");

  await page.goto("/snapshots");
  await page.getByRole("button", { name: "Select all automatic", exact: true }).click();
  await expect(page.locator("#tick-note")).toHaveText("There are no automatic snapshots to tick.");
  await expect(page.locator("#tick-note")).toBeFocused();

  // A restore takes a "Before restore" snapshot, which is automatic.
  await page.goto("/worlds/second/snapshots");
  await page.getByRole("link", { name: /^Restore Only a manual one/ }).click();
  await page.getByRole("button", { name: "Restore snapshot" }).click();
  await expect(page).toHaveURL(/\/worlds\/second\/snapshots\?(restored|undo)=/);

  await page.getByRole("button", { name: "Select all automatic", exact: true }).click();
  await expect(page.locator("#tick-note")).toHaveText("Select all automatic ticked 1 snapshot.");
  await expect(page.locator("#tick-note")).toBeFocused();
  await expect(page.locator('input[name="snap"]:checked')).toHaveCount(1);
  await checkPage(page);

  await page.goto("/snapshots");
  await expect(page.locator("#tick-note")).toHaveCount(0);
  await page.getByRole("button", { name: "Select all automatic of Second" }).click();
  await expect(page.locator("#tick-note")).toHaveText("Select all automatic ticked 1 snapshot.");
  await expect(page.locator("#tick-note")).toBeFocused();
  await checkPage(page);
});

// WCAG 1.4.4: at 200% text on a phone the tables stay inside their panel and
// a button label breaks only between words.
test.describe("at 320 px and 200% text", () => {
  test.use({ viewport: { width: 320, height: 640 } });

  async function expectFits(page: Page) {
    await page.evaluate(() => {
      document.documentElement.style.fontSize = "200%";
    });
    const problems = await page.evaluate(() => {
      const out: string[] = [];
      const root = document.documentElement;
      if (root.scrollWidth > root.clientWidth) out.push(`page scrolls sideways: ${root.scrollWidth} > ${root.clientWidth}`);
      for (const t of document.querySelectorAll("table")) {
        const panel = t.closest<HTMLElement>(".panel")!.getBoundingClientRect();
        const r = t.getBoundingClientRect();
        if (r.left < panel.left - 0.5 || r.right > panel.right + 0.5) out.push(`table ${t.querySelector("caption")?.textContent} is ${r.width}px wide in a ${panel.width}px panel`);
      }
      // A word that splits across lines shows as two boxes.
      for (const b of document.querySelectorAll<HTMLElement>("main button, main .button")) {
        const walker = document.createTreeWalker(b, NodeFilter.SHOW_TEXT);
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          const text = n.textContent ?? "";
          for (const m of text.matchAll(/\S+/g)) {
            const range = document.createRange();
            range.setStart(n, m.index!);
            range.setEnd(n, m.index! + m[0].length);
            const lines = new Set([...range.getClientRects()].map((q) => Math.round(q.top)));
            if (lines.size > 1) out.push(`"${m[0]}" splits across lines in the ${b.textContent?.trim()} button`);
          }
        }
      }
      return out;
    });
    expect(problems).toEqual([]);
    // Stacked rows are still a table to a screen reader: Safari drops the
    // native roles when the cells are display:block, so the markup states them.
    await expect(page.locator("table.snapshot-list")).not.toHaveCount(0);
    await expect(page.locator("table.snapshot-list:not([role=table])")).toHaveCount(0);
    await expect(page.getByRole("table").first()).toBeVisible();
    await expect(page.getByRole("columnheader")).toHaveCount(3 * (await page.getByRole("table").count()));
    await expect(page.getByRole("row")).not.toHaveCount(0);
    await expect(page.getByRole("cell")).not.toHaveCount(0);
  }

  test("the Snapshots tab and a world's snapshots page fit", async ({ page, foreman }) => {
    await createAdmin(page);
    seedWorld(foreman);
    seedSecond(foreman);
    await takeSnapshot(page, "A label long enough to wrap");
    await page.goto("/snapshots");
    await expectFits(page);
    await page.goto("/worlds/second/snapshots");
    await expectFits(page);
  });
});
