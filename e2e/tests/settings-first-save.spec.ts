import { test, expect, createAdmin, seedWorld } from "./foreman.js";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

// SPEC 11.1, 11.2: a world with no server-settings.json (as a generated or
// imported one has) starts without --server-settings. The first save through
// the form must write the three keys the game needs, or the next start is
// refused.

test.use({ fakeGameOn: true });

const typed = "newpass-typed";

test("the first save of a name, a password and max players writes the keys the game needs", async ({ page, foreman }) => {
  await createAdmin(page);
  seedWorld(foreman);
  const file = join(foreman.dataDir, "worlds", "base", "config", "server-settings.json");
  expect(existsSync(file), "the seeded world has no server-settings.json").toBe(false);

  await page.goto("/worlds/base/settings");
  await expect(page.getByLabel("Server name")).toHaveValue("Base");
  await expect(page.getByRole("radio", { name: "Hidden" })).toBeChecked();

  await page.getByLabel("New password").fill(typed);
  await page.getByLabel("Maximum players").fill("8");
  await page.getByRole("button", { name: "Preview changes" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Save these changes to Base?");
  await page.getByRole("button", { name: "Save settings" }).click();
  await expect(page).toHaveURL(/\/worlds\/base\/settings\?saved=1/);

  const saved = JSON.parse(readFileSync(file, "utf8"));
  expect(saved.name).toBe("Base");
  expect(saved.description).toBe("");
  expect(saved.visibility).toEqual({ public: false, lan: false });
  expect(saved.max_players).toBe(8);
  expect(saved.game_password).toBe(typed);

  // The saved file is what the form shows next time, and the password does not show.
  await expect(page.getByLabel("Server name")).toHaveValue("Base");
  expect(await page.content()).not.toContain(typed);
});
