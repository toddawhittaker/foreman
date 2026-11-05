import { test, expect, checkPage, createAdmin, seedWorld, image21, type Foreman, type FakeGame } from "./foreman.js";
import type { Page } from "@playwright/test";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { request } from "node:http";
import { join } from "node:path";

// Game versions: the world page, the release notice, update and rollback
// (SPEC 10.2, 10.3, 10.4, 10.7) against the fake game. Docker Hub is never
// reachable here: the fixture sends outbound traffic to a refused port.

const live = { timeout: 20_000 };
const fixtures = join(import.meta.dirname, "..", "..", "internal", "savefile", "testdata");

const badge = (page: Page) => page.getByRole("region", { name: "Status" }).locator(".state");

// The version on disk, so the checks do not depend on how a page words it.
const diskVersion = (f: Foreman, id: string): string =>
  JSON.parse(readFileSync(join(f.dataDir, "worlds", id, "world.json"), "utf8")).game_version;

// A second world, written as a folder (SPEC 5.1). seedWorld made "base".
function seedSecond(f: Foreman) {
  const dir = join(f.dataDir, "worlds", "second");
  for (const sub of ["saves", "mods", "config"]) mkdirSync(join(dir, sub), { recursive: true });
  writeFileSync(
    join(dir, "world.json"),
    JSON.stringify({ format_version: 1, name: "Second", game_version: "2.1.24", space_age: false, created_at: "2026-11-02T10:00:00Z" }),
  );
  copyFileSync(join(fixtures, "2.1.24-no-space-age.zip"), join(dir, "saves", "base.zip"));
}

async function setUp(page: Page, f: Foreman, g: FakeGame, opts: Parameters<typeof seedWorld>[1] = {}) {
  await createAdmin(page);
  seedWorld(f, opts);
  await g.call("/image", { ref: image21, on: true });
  await page.goto("/");
  await expect(badge(page)).toHaveText("Stopped", live);
}

// The fake's own record of a container, including the image it was made from.
async function container(g: FakeGame, name: string) {
  const state = (await (await fetch(g.hook + "/state")).json()) as {
    containers: { name: string; image: string; status: string }[];
  };
  return state.containers.filter((c) => c.name === name).at(-1);
}

const creates = async (g: FakeGame) =>
  (await g.state()).calls.filter((c) => c.startsWith("POST ") && c.includes("/containers/create")).length;
const pulls = async (g: FakeGame) => (await g.state()).calls.filter((c) => c.includes("/images/create"));

// Asks the fake Docker host whether it holds an image; the call log keeps no query strings.
function hasImage(g: FakeGame, ref: string): Promise<boolean> {
  return new Promise((resolve, reject) => {
    const req = request({ socketPath: g.socket, path: `/images/${ref}/json` }, (res) => {
      res.resume();
      resolve(res.statusCode === 200);
    });
    req.on("error", reject);
    req.end();
  });
}

const image2125 = image21.replace(/:.*$/, ":2.1.25");

async function snapshotLabels(page: Page, id: string): Promise<string> {
  await page.goto(`/worlds/${id}/snapshots`);
  return (await page.locator("main").innerText()) ?? "";
}

test.describe("with the fake game", () => {
  test.use({ fakeGameOn: true });

  // SPEC 10.2, 10.3: the active world offers Update, another world says how to get it.
  test("the world page shows the version, and offers Update only for the active world", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    seedSecond(foreman);

    await page.goto("/worlds/base");
    await expect(page.locator("dl.facts")).toContainText("2.1.24");
    const panel = page.getByRole("region", { name: "Game version" });
    await expect(panel).toContainText("This world runs Factorio 2.1.24");
    await expect(panel.getByRole("link", { name: "Update game version" })).toHaveAttribute("href", "/worlds/base/update");
    await expect(panel.getByText("Switch to this world to update it.")).toHaveCount(0);
    // No update has happened yet, so there is nothing to roll back.
    await expect(page.getByRole("link", { name: /Roll back to/ })).toHaveCount(0);

    await page.goto("/worlds/second");
    await expect(page.locator("dl.facts")).toContainText("2.1.24");
    const other = page.getByRole("region", { name: "Game version" });
    await expect(other.getByText("Switch to this world to update it.")).toBeVisible();
    await expect(other.getByRole("link", { name: "Update game version" })).toHaveCount(0);
    await checkPage(page);
  });

  // SPEC 10.2, 14.10: Docker Hub down is said in words and never blocks the page.
  test("the release notice says Docker Hub cannot be reached", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/worlds/base");
    await expect(page.locator("#release-notice")).toContainText("Docker Hub cannot be reached", live);
    await expect(page.locator("#release-notice")).toContainText("You can still update by typing a version");
    await expect(page.locator("#release-notice")).not.toContainText("Checking for a newer");
    // Update is still offered.
    await expect(page.getByRole("link", { name: "Update game version" })).toBeVisible();
    await checkPage(page);
  });

  // SPEC 10.7: the list is a help, not a gate.
  test("the update page works with no version list", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    await page.goto("/worlds/base/update");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Update Base");
    await expect(page.getByRole("heading", { level: 2, name: /Update Base from Factorio 2\.1\.24\?/ })).toBeVisible();
    await expect(page.getByLabel("New game version")).toBeVisible();
    // The list loads (or fails) without blocking, and offers nothing while Docker Hub is down.
    await page.waitForLoadState("networkidle");
    await expect(page.locator("#update-version-select option")).toHaveText(["Choose a version", "Docker Hub cannot be reached; type a version below"]);
    await expect(page.getByLabel("Or type a version")).toBeVisible();
    await expect(page.getByText("A game version only moves forward.")).toContainText("Roll back");
    await expect(page.getByText("Leave the server stopped, because it is not running now.")).toBeVisible();
    await expect(page.getByLabel("Countdown in seconds")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Update game version" })).toHaveClass(/button--danger/);
    await checkPage(page);
  });

  // SPEC 10.3, 6.2: a stopped world updates to the typed version, snapshot first, and stays stopped.
  test("a stopped world updates to a typed version and stays stopped", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    const before = await creates(g);
    expect(await hasImage(g, image2125)).toBe(false);

    await page.goto("/worlds/base/update");
    await page.getByLabel("Or type a version").fill("2.1.25");
    await page.getByRole("button", { name: "Update game version" }).click();

    await expect.poll(() => diskVersion(foreman, "base"), live).toBe("2.1.25");
    await expect(page).toHaveURL("/");
    await expect(badge(page)).toHaveText("Stopped");
    // The image is fetched as a step, then nothing is started.
    expect(await hasImage(g, image2125)).toBe(true);
    expect(await creates(g)).toBe(before);

    expect(await snapshotLabels(page, "base")).toContain("Before update");

    await page.goto("/worlds/base");
    await expect(page.locator("dl.facts")).toContainText("2.1.25");
    await expect(page.getByRole("region", { name: "Game version" })).toContainText("This world runs Factorio 2.1.25");
    await expect(page.getByRole("link", { name: "Roll back to 2.1.24" })).toBeVisible();
    await checkPage(page);

    // A stopped world rolls back before the reply and lands on Snapshots with the undo snapshot.
    await page.getByRole("link", { name: "Roll back to 2.1.24" }).click();
    await expect(page.getByLabel("Countdown in seconds")).toHaveCount(0);
    await checkPage(page);
    await page.getByRole("button", { name: "Roll back to 2.1.24" }).click();
    await expect(page).toHaveURL(/\/worlds\/base\/snapshots\?undo=/);
    expect(diskVersion(foreman, "base")).toBe("2.1.24");
    await expect(page.locator("main")).toContainText("Before restore");
    expect(await creates(g)).toBe(before);
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  });

  // SPEC 10.3, 10.4: a running world is stopped, snapshotted, set and started on the new image;
  // rollback puts the old version and its image back.
  test("a running world updates with a step list, then rolls back", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    await page.getByRole("button", { name: "Start server" }).click();
    await expect(badge(page)).toHaveText("Running", live);
    await g.call("/join", { name: "foreman-base", player: "alice" });
    expect((await container(g, "foreman-base"))?.image).toContain(":2.1.24");

    await page.goto("/worlds/base/update");
    await expect(page.getByLabel("Countdown in seconds")).toHaveValue("60");
    await expect(page.getByText("Start the game again on the new version.")).toBeVisible();
    await page.getByLabel("Or type a version").fill("2.1.25");
    await page.getByLabel("Countdown in seconds").fill("3");
    await checkPage(page);
    await page.getByRole("button", { name: "Update game version" }).click();

    // The countdown holds the first step open, so the list can be seen.
    await expect(page).toHaveURL("/");
    await expect(page.getByRole("listitem").filter({ hasText: "Warn players" })).toContainText("In progress", live);
    await expect(page.getByRole("listitem").filter({ hasText: "Take the snapshot before the update" })).toContainText("Waiting");
    await checkPage(page);

    await expect.poll(async () => (await container(g, "foreman-base"))?.image, live).toContain(":2.1.25");
    await expect(badge(page)).toHaveText("Running", live);
    expect((await container(g, "foreman-base"))?.status).toBe("running");
    expect(diskVersion(foreman, "base")).toBe("2.1.25");
    expect(await hasImage(g, image2125)).toBe(true);
    expect(await snapshotLabels(page, "base")).toContain("Before update");

    // Rollback: the warning is on its page, before the button.
    await page.goto("/worlds/base");
    await page.getByRole("link", { name: "Roll back to 2.1.24" }).click();
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Roll back Base");
    const confirm = page.getByRole("region", { name: /Go back from Factorio 2\.1\.25 to 2\.1\.24/ });
    await expect(confirm).toContainText("everything played since the update is lost");
    await expect(confirm).toContainText('Restore the snapshot "Before update"');
    await expect(confirm).toContainText('labelled "Before restore"');
    await expect(confirm.getByRole("button", { name: "Roll back to 2.1.24" })).toHaveClass(/button--danger/);
    await checkPage(page);

    await page.getByLabel("Countdown in seconds").fill("0");
    await confirm.getByRole("button", { name: "Roll back to 2.1.24" }).click();
    await expect.poll(() => diskVersion(foreman, "base"), live).toBe("2.1.24");
    await expect.poll(async () => (await container(g, "foreman-base"))?.image, live).toContain(":2.1.24");
    await expect(badge(page)).toHaveText("Running", live);

    const labels = await snapshotLabels(page, "base");
    expect(labels).toContain("Before restore");
    await page.goto("/worlds/base");
    await expect(page.locator("dl.facts")).toContainText("2.1.24");
    // Back on the old version, there is no older one to roll back to.
    await expect(page.getByRole("link", { name: /Roll back to/ })).toHaveCount(0);
    expect(foreman.logs()).not.toMatch(/"level":"ERROR"/);
  });

  // SPEC 10.3: a version that is not newer, or that Foreman does not run, is a field error
  // and changes nothing.
  test("a version that is not newer or not supported is a field error", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    const before = await creates(g);
    const pulled = (await pulls(g)).length;

    for (const typed of ["2.1.23", "2.1.24", "2.0.77", "3.0.0", "2.1", "latest"]) {
      await page.goto("/worlds/base/update");
      await page.getByLabel("Or type a version").fill(typed);
      await page.getByRole("button", { name: "Update game version" }).click();

      await expect(page.getByRole("heading", { name: /Fix this problem/ }), typed).toBeVisible();
      const field = page.getByLabel("Or type a version");
      await expect(field, typed).toHaveValue(typed);
      await expect(field, typed).toHaveAttribute("aria-invalid", "true");
      await expect(page.locator("#update-version-error"), typed).toContainText("Error:");
      expect(diskVersion(foreman, "base"), typed).toBe("2.1.24");
    }

    // The wording for the common case says "newer".
    await page.goto("/worlds/base/update");
    await page.getByLabel("Or type a version").fill("2.1.23");
    await page.getByRole("button", { name: "Update game version" }).click();
    await expect(page.locator("#update-version-error")).toContainText(/newer/i);
    await checkPage(page);

    // Nothing was downloaded, started or snapshotted.
    expect((await pulls(g)).length).toBe(pulled);
    expect(await creates(g)).toBe(before);
    expect(await snapshotLabels(page, "base")).not.toContain("Before update");
  });

  // SPEC 10.3: only the active world updates; a form sent for another world changes nothing.
  test("a world that is not the active one cannot be updated", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    await setUp(page, foreman, g);
    seedSecond(foreman);

    const resp = await page.request.post("/worlds/second/update", { form: { game_version: "2.1.25" }, maxRedirects: 0 });
    expect(resp.status()).toBe(409);
    expect(await resp.text()).toContain("Switch to this world to update it.");
    expect(diskVersion(foreman, "second")).toBe("2.1.24");
    expect((await pulls(g)).length).toBe(0);
    expect(await snapshotLabels(page, "second")).not.toContain("Before update");
  });

  // SPEC 10.3, 7.4, 17.5: the refusal box is the confirmation for update anyway.
  test("a problem at the new version is refused, and Update anyway goes ahead after a snapshot", async ({ page, foreman, fakeGame }) => {
    const g = fakeGame!;
    // The save uses Space Age, and the world has it off.
    await setUp(page, foreman, g, { fixture: "2.1.24-space-age.zip", spaceAge: false });
    await page.goto("/worlds/base/update");
    await page.getByLabel("Or type a version").fill("2.1.25");
    await page.getByRole("button", { name: "Update game version" }).click();

    const refusal = page.locator(".message--warning").filter({ hasText: "Space Age" });
    await expect(refusal).toBeVisible(live);
    await expect(refusal).toContainText("base.zip uses Space Age");
    await expect(refusal).toContainText('labelled "Before update"');
    await expect(refusal.getByRole("button", { name: "Update anyway" })).toBeVisible();
    // Nothing changed while the box is up.
    expect(diskVersion(foreman, "base")).toBe("2.1.24");
    expect((await pulls(g)).length).toBe(0);
    await checkPage(page);

    await refusal.getByRole("button", { name: "Update anyway" }).click();
    await expect.poll(() => diskVersion(foreman, "base"), live).toBe("2.1.25");
    expect(await snapshotLabels(page, "base")).toContain("Before update");
  });

  // SPEC 10.4: a rollback is offered only after an update, and refused otherwise.
  test("a rollback with nothing to roll back to changes nothing", async ({ page, foreman, fakeGame }) => {
    await setUp(page, foreman, fakeGame!);
    const resp = await page.request.post("/worlds/base/rollback", { form: {}, maxRedirects: 0 });
    expect(resp.status()).toBe(409);
    expect(diskVersion(foreman, "base")).toBe("2.1.24");
    await page.goto("/worlds/base/rollback");
    await expect(page.locator("#action-error")).toContainText("Error:");
    await expect(page.getByRole("button", { name: /Roll back to/ })).toHaveCount(0);
    await checkPage(page);
    expect(diskVersion(foreman, "base")).toBe("2.1.24");
    expect(await snapshotLabels(page, "base")).not.toContain("Before restore");
  });
});

// SPEC 10.7, 5.5: an import takes a typed version with no list to pick from.
test.describe("the import form", () => {
  test("accepts a typed game version while Docker Hub is unreachable", async ({ page, foreman }) => {
    await createAdmin(page);
    mkdirSync(join(foreman.dataDir, "import", "old"), { recursive: true });
    copyFileSync(join(fixtures, "2.0.77-no-space-age.zip"), join(foreman.dataDir, "import", "old", "a.zip"));
    await page.goto("/import");
    const form = page.locator("form", { has: page.getByRole("heading", { name: "Import a folder" }) });
    await page.waitForLoadState("networkidle");
    await expect(form.locator("#folder-version-select option")).toHaveText(["Choose a version", "Docker Hub cannot be reached; type a version below"]);
    await form.getByLabel("World name").fill("Typed");
    await form.getByLabel("Or type a version").fill("2.0.78");
    await checkPage(page);
    await form.getByRole("button", { name: "Import folder" }).click();
    await expect(page).toHaveURL("/worlds/typed");
    await expect(page.locator("dl.facts")).toContainText("2.0.78");
    expect(diskVersion(foreman, "typed")).toBe("2.0.78");
  });
});
