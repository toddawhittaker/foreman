import { test, expect } from "./foreman.js";

test("the built binary answers its health path", async ({ request }) => {
  const resp = await request.get("/healthz");
  expect(resp.status()).toBe(200);
  expect(await resp.text()).toBe("ok\n");
});
