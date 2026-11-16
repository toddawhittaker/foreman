import { test, expect, checkPage, createAdmin } from "./foreman.js";
import type { Page } from "@playwright/test";
import { readdirSync } from "node:fs";
import { join } from "node:path";

// The shared upload control (DESIGN 7.10), tried on the Import page's
// "Upload a save" form. The server's own checks (SPEC 14.6) are unchanged
// and covered in import.spec.ts.
test.use({ fakeGameOn: true });

const emptyZip = Buffer.concat([Buffer.from("PK\x05\x06"), Buffer.alloc(18)]);

const uploadForm = (page: Page) => page.locator("form", { has: page.getByRole("heading", { name: "Upload a save" }) });

// Drops files on the zone the way a browser does: dragenter on the page,
// then dragover and drop on the zone, carrying a DataTransfer of files.
async function drop(page: Page, files: { name: string; body: string }[]) {
  const dt = await page.evaluateHandle((files) => {
    const dt = new DataTransfer();
    for (const f of files) dt.items.add(new File([f.body], f.name, { type: "application/zip" }));
    return dt;
  }, files);
  const area = page.locator("#upload-source").locator("..");
  await page.dispatchEvent("body", "dragenter", { dataTransfer: dt });
  await expect(page.locator("[data-dropzone]")).toHaveClass(/dropzone--page/);
  await expect(area.getByText("Drop the file to choose it")).toBeVisible();
  await area.dispatchEvent("dragover", { dataTransfer: dt });
  await expect(page.locator("[data-dropzone]")).toHaveClass(/dropzone--over/);
  await area.dispatchEvent("drop", { dataTransfer: dt });
}

test("a dropped save fills the field, names the file, and imports", async ({ page, foreman }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = uploadForm(page);
  await expect(form.getByText("A .zip file of up to 1 GB.")).toBeVisible();
  await expect(form.getByText("Drop the file here, or choose a file")).toBeVisible();
  await checkPage(page);

  await drop(page, [{ name: "dropped.zip", body: emptyZip.toString("latin1") }]);
  await expect(page.locator("[data-dropzone]")).not.toHaveClass(/dropzone--page|dropzone--over/);
  await expect(form.locator(".dropzone__chosen")).toHaveText("Chosen: dropped.zip, 22 B Clear");
  // The page's own listener still runs: the name is suggested from the file.
  await expect(form.getByLabel("World name")).toHaveValue("dropped");
  await expect(form.getByLabel("Save file")).toBeFocused();
  await checkPage(page);

  await form.getByLabel("Or type a version").fill("1.1.110");
  await form.getByRole("button", { name: "Upload and import" }).click();
  await expect(page).toHaveURL("/worlds/dropped?imported=1&unchecked=1");
  expect(readdirSync(join(foreman.dataDir, "worlds", "dropped", "saves"))).toEqual(["dropped.zip"]);
});

test("a click anywhere on the zone, or Space on it, opens the picker", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = uploadForm(page);
  // The prompt's words lie under the transparent input, so a click on them
  // lands on the input.
  let chooser = page.waitForEvent("filechooser");
  await form.locator(".dropzone__area").click({ position: { x: 10, y: 10 } });
  await (await chooser).setFiles({ name: "clicked.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(form.locator(".dropzone__file")).toHaveText("Chosen: clicked.zip, 22 B");

  // Keyboard: Shift+Tab from the button passes Clear and reaches the field, with a visible focus ring on the zone.
  await form.getByRole("button", { name: "Upload and import" }).focus();
  await page.keyboard.press("Shift+Tab");
  await expect(form.getByRole("button", { name: "Clear" })).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  const input = form.getByLabel("Save file");
  await expect(input).toBeFocused();
  const outline = await form.locator(".dropzone__area").evaluate((el) => getComputedStyle(el).outlineStyle);
  expect(outline).toBe("solid");
  chooser = page.waitForEvent("filechooser");
  await page.keyboard.press("Space");
  await (await chooser).setFiles({ name: "typed.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(form.locator(".dropzone__file")).toHaveText("Chosen: typed.zip, 22 B");

  // The zone is at least a 44 px target, even at phone width.
  await page.setViewportSize({ width: 320, height: 700 });
  const box = await form.locator(".dropzone__area").boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(44);
  expect(box!.width).toBeLessThanOrEqual(320);
  await checkPage(page);
});

test("a wrong type, several files or a file over the limit is refused before uploading", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = uploadForm(page);
  const input = form.getByLabel("Save file");

  await drop(page, [{ name: "notes.txt", body: "hello" }]);
  const err = form.locator("#upload-source-client-error");
  await expect(err).toHaveText("Error: notes.txt is not a .zip file. Choose a file that ends in .zip.");
  await expect(input).toHaveAttribute("aria-invalid", "true");
  await expect(input).toHaveAttribute("aria-describedby", "upload-source-hint upload-source-client-error");
  await expect(page.locator("#announcer")).toHaveText("notes.txt is not a .zip file. Choose a file that ends in .zip.");
  expect(await input.evaluate((el: HTMLInputElement) => el.files!.length)).toBe(0);
  await checkPage(page);

  await drop(page, [{ name: "a.zip", body: "x" }, { name: "b.zip", body: "y" }]);
  await expect(err).toHaveText("Error: Choose one file at a time.");

  // A picked file of the wrong type is emptied too, and the page's own
  // listener never sees it: no world name is suggested from it.
  await input.setInputFiles({ name: "picked.dat", mimeType: "application/octet-stream", buffer: Buffer.from("x") });
  await expect(err).toHaveText("Error: picked.dat is not a .zip file. Choose a file that ends in .zip.");
  await expect(form.getByLabel("World name")).toHaveValue("");
  expect(await input.evaluate((el: HTMLInputElement) => el.files!.length)).toBe(0);

  // The real limit is 1 GiB; the test lowers the zone's copy of it, since
  // the limit text comes from the server and only the number is compared.
  await form.locator("[data-dropzone]").evaluate((el: HTMLElement) => { el.dataset.maxBytes = "10"; });
  await drop(page, [{ name: "big.zip", body: "eleven bytes" }]);
  await expect(err).toHaveText("Error: big.zip is larger than 1 GB. Choose a smaller file.");

  // A good file then clears the error.
  await drop(page, [{ name: "ok.zip", body: "1" }]);
  await expect(err).toBeHidden();
  await expect(input).not.toHaveAttribute("aria-invalid");
  await expect(input).toHaveAttribute("aria-describedby", "upload-source-hint");
});

test("a refused drop empties an earlier good choice", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = uploadForm(page);
  const input = form.getByLabel("Save file");
  await input.setInputFiles({ name: "good.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(form.locator(".dropzone__chosen")).toBeVisible();
  await drop(page, [{ name: "bad.txt", body: "x" }]);
  await expect(form.locator("#upload-source-client-error")).toHaveText("Error: bad.txt is not a .zip file. Choose a file that ends in .zip.");
  await expect(form.locator(".dropzone__chosen")).toBeHidden();
  expect(await input.evaluate((el: HTMLInputElement) => el.files!.length)).toBe(0);
});

test("a new good choice clears the server's error", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = uploadForm(page);
  await form.getByLabel("World name").fill("Nofile");
  await form.getByLabel("Or type a version").fill("1.1.110");
  await form.getByRole("button", { name: "Upload and import" }).click();
  const serverError = form.locator("#upload-source-error");
  await expect(serverError).toBeVisible();
  const input = form.getByLabel("Save file");
  await expect(input).toHaveAttribute("aria-invalid", "true");
  await input.setInputFiles({ name: "fixed.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(serverError).toBeHidden();
  await expect(input).not.toHaveAttribute("aria-invalid");
  await expect(input).toHaveAttribute("aria-describedby", "upload-source-hint");
});

test("Clear empties the field and returns focus to it", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/import");
  const form = uploadForm(page);
  const input = form.getByLabel("Save file");
  await input.setInputFiles({ name: "first.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(form.locator(".dropzone__chosen")).toBeVisible();
  await page.setViewportSize({ width: 320, height: 700 });
  const clearBox = await form.getByRole("button", { name: "Clear" }).boundingBox();
  expect(clearBox!.height).toBeGreaterThanOrEqual(44);
  expect(clearBox!.width).toBeGreaterThanOrEqual(44);
  await form.getByRole("button", { name: "Clear" }).click();
  await expect(form.locator(".dropzone__chosen")).toBeHidden();
  await expect(input).toBeFocused();
  await expect(page.locator("#announcer")).toHaveText("No file chosen.");
  expect(await input.evaluate((el: HTMLInputElement) => el.files!.length)).toBe(0);

  // Choosing again replaces the file.
  await input.setInputFiles({ name: "second.zip", mimeType: "application/zip", buffer: emptyZip });
  await expect(form.locator(".dropzone__file")).toHaveText("Chosen: second.zip, 22 B");
});

test.describe("without script", () => {
  test.use({ javaScriptEnabled: false });

  test("the zone is a plain labelled file field that still uploads", async ({ page, foreman }) => {
    await createAdmin(page);
    await page.goto("/import");
    const form = uploadForm(page);
    await expect(form.getByText("Drop the file here, or choose a file")).toBeHidden();
    await expect(form.locator(".dropzone__chosen")).toBeHidden();
    const input = form.getByLabel("Save file");
    await expect(input).toBeVisible();
    expect(await input.evaluate((el) => getComputedStyle(el).opacity)).toBe("1");
    await expect(input).toHaveAttribute("accept", ".zip");

    await form.getByLabel("World name").fill("Plain");
    await form.getByLabel("Or type a version").fill("1.1.110");
    await input.setInputFiles({ name: "plain.zip", mimeType: "application/zip", buffer: emptyZip });
    await form.getByRole("button", { name: "Upload and import" }).click();
    await expect(page).toHaveURL("/worlds/plain?imported=1&unchecked=1");
    expect(readdirSync(join(foreman.dataDir, "worlds", "plain", "saves"))).toEqual(["plain.zip"]);
  });
});
