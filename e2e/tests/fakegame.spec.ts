import { test, expect } from "./foreman.js";
import { request } from "node:http";

test.use({ fakeGameOn: true });

// Node's fetch cannot dial a Unix socket, so the fake's Docker side is
// reached with http.request.
function dockerGet(socket: string, path: string): Promise<number> {
  return new Promise((resolve, reject) => {
    const req = request({ socketPath: socket, path, method: "GET" }, (res) => {
      res.resume();
      resolve(res.statusCode ?? 0);
    });
    req.on("error", reject);
    req.end();
  });
}

// Plumbing only: the Server page does not exist yet.
test("Foreman serves /worlds with the fake game as its Docker host", async ({ page, foreman, fakeGame }) => {
  expect(fakeGame).toBeDefined();
  await page.goto("/setup");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password", { exact: true }).fill("correct horse battery");
  await page.getByLabel("Confirm password").fill("correct horse battery");
  await page.getByRole("button", { name: "Create admin account" }).click();
  await expect(page).toHaveURL("/");
  await page.goto("/worlds");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Worlds");
  expect(foreman.logs()).not.toMatch(/level=ERROR/);
  // Reported, not asserted: with FOREMAN_HOST_DATA_DIR set Foreman makes no
  // Docker call today, and that changes once server control lands.
  const calls = (await fakeGame!.state()).calls;
  test.info().annotations.push({ type: "docker calls", description: calls.join(", ") || "none" });
});

test("a hook call changes what the fake Docker socket answers", async ({ fakeGame }) => {
  const g = fakeGame!;
  const ref = "factoriotools/factorio:2.1.24";
  const path = `/v1.52/images/${ref}/json`;

  expect(await dockerGet(g.socket, path)).toBe(404);
  await g.call("/image", { ref, on: true });
  expect(await dockerGet(g.socket, path)).toBe(200);
  await g.call("/image", { ref, on: false });
  expect(await dockerGet(g.socket, path)).toBe(404);

  expect((await g.state()).containers).toEqual([]);
  // A hook for a game that is not running is refused, with a reason.
  await expect(g.call("/join", { name: "foreman-none", player: "alice" })).rejects.toThrow(/no game is running/);
});
