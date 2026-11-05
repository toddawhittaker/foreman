import { test, expect, createAdmin, seedWorld, image21 } from "./foreman.js";
import type { Page } from "@playwright/test";

// Moving between tabs must not move the page sideways (DESIGN 6). The
// projects run this at desktop width and at 320 CSS pixels.

// Playwright hides scrollbars by default; real browsers show them, and a page
// that scrolls then loses their width.
test.use({ fakeGameOn: true, launchOptions: { ignoreDefaultArgs: ["--hide-scrollbars"] } });

// Where the parts that stay the same on every tab sit.
async function frame(page: Page, path: string) {
  await page.goto(path);
  const box = async (selector: string) => {
    const b = await page.locator(selector).first().boundingBox();
    expect(b, selector).not.toBeNull();
    return { x: b!.x, width: b!.width };
  };
  return {
    heading: await box("h1"),
    nav: await box("nav[aria-label='Main']"),
    header: await box("header.site"),
    main: await box("main"),
  };
}

test("the Server, Console and Worlds pages line up", async ({ page, foreman, fakeGame }) => {
  await createAdmin(page);
  seedWorld(foreman);
  await fakeGame!.call("/image", { ref: image21, on: true });

  const server = await frame(page, "/");
  expect(await frame(page, "/console")).toEqual(server);
  expect(await frame(page, "/worlds")).toEqual(server);
});
