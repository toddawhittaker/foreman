import { expect, type Page } from "@playwright/test";
import { test } from "./foreman.js";

// Counts the clicks an admin makes (SPEC 17.10): each activation of a
// button, link or choice. Typing in a field is not a click, and nor is a
// navigation the test makes with page.goto. The count lives in the page's
// session storage, so it survives the page loads a click causes.

const key = "foreman-e2e-clicks";

// Runs in the page. Only trusted clicks count, so the click a label passes
// on to its box, and a script's own click, do not count twice.
function listen(storeKey: string) {
  const w = window as unknown as { __clickCounter?: boolean };
  if (w.__clickCounter) return;
  w.__clickCounter = true;
  document.addEventListener(
    "click",
    (e) => {
      if (!e.isTrusted) return;
      const el = (e.target as Element).closest<HTMLElement>("a, button, input, select, label, summary, [role=button]");
      const name = el
        ? (el.getAttribute("aria-label") || (el as HTMLInputElement).value || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 60)
        : "";
      const what = `${el ? el.tagName.toLowerCase() : "other"} "${name}"`;
      try {
        const seen: string[] = JSON.parse(sessionStorage.getItem(storeKey) ?? "[]");
        seen.push(what);
        sessionStorage.setItem(storeKey, JSON.stringify(seen));
      } catch {
        // Storage is unavailable on a blank page; there is nothing to click there.
      }
    },
    true,
  );
}

export interface Clicks {
  // Forgets earlier clicks. Call it once the test is on the Server page.
  reset: () => Promise<void>;
  // Each click so far, by tag and name.
  list: () => Promise<string[]>;
}

export async function countClicks(page: Page): Promise<Clicks> {
  await page.addInitScript(listen, key);
  await page.evaluate(listen, key);
  const list = async () => page.evaluate((k) => JSON.parse(sessionStorage.getItem(k) ?? "[]") as string[], key);
  return {
    list,
    reset: () => page.evaluate((k) => sessionStorage.removeItem(k), key),
  };
}

// Fails when the task took more clicks than SPEC 17.10 allows, naming each
// click. The actual count goes into the report as an annotation.
export async function expectWithin(clicks: Clicks, budget: number, task: string) {
  const made = await clicks.list();
  test.info().annotations.push({ type: "clicks", description: `${task}: ${made.length} of ${budget}` });
  // A count of zero for a task that needs a click means the counter is not listening.
  if (budget > 0) expect(made.length, `${task}: no click was counted`).toBeGreaterThan(0);
  expect(made.length, `${task} took ${made.length} clicks, budget ${budget}: ${made.join(", ")}`).toBeLessThanOrEqual(budget);
}
