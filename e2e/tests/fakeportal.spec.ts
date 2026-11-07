import { test, expect, createAdmin, type FakePortal } from "./foreman.js";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { connect as tlsConnect } from "node:tls";

test.use({ fakePortalOn: true });

interface Reply {
  status: number;
  location?: string;
  body: Buffer;
}

// Asks for https://host/path as Foreman's portal client does: a CONNECT to the
// proxy, then TLS to the host, trusting only the fake's test authority. Rejects
// if the proxy refuses the tunnel.
function viaProxy(portal: FakePortal, host: string, path: string): Promise<Reply> {
  const proxy = new URL(portal.proxy);
  return new Promise((resolve, reject) => {
    const connect = httpRequest({ host: proxy.hostname, port: proxy.port, method: "CONNECT", path: `${host}:443` });
    connect.on("error", reject);
    connect.on("connect", (res, socket) => {
      if (res.statusCode !== 200) {
        socket.destroy();
        return reject(new Error(`proxy answered CONNECT ${host} with ${res.statusCode}`));
      }
      const req = httpsRequest(
        {
          host,
          path,
          createConnection: () => tlsConnect({ socket, servername: host, ca: readFileSync(portal.caFile) }),
        },
        (resp) => {
          const chunks: Buffer[] = [];
          resp.on("data", (c) => chunks.push(c));
          resp.on("end", () => {
            socket.destroy();
            resolve({ status: resp.statusCode ?? 0, location: resp.headers.location, body: Buffer.concat(chunks) });
          });
        },
      );
      req.on("error", reject);
      req.end();
    });
    connect.end();
  });
}

const creds = (p: FakePortal) => `username=${p.username}&token=${p.token}`;

test("the fake portal serves the portal's API and counts what it is asked", async ({ fakePortal }) => {
  const portal = fakePortal!;
  const list = await viaProxy(portal, "mods.factorio.com", "/api/mods?page_size=max&version=2.0");
  expect(list.status).toBe(200);
  expect(list.body.toString()).toContain("visible-planets");

  const full = await viaProxy(portal, "mods.factorio.com", "/api/mods/visible-planets/full");
  expect(full.status).toBe(200);
  const versions = (JSON.parse(full.body.toString()).releases as { version: string }[]).map((r) => r.version);
  // Issue 131: a world can have 1.5.5 installed that the portal does not have.
  expect(versions).not.toContain("1.5.5");

  expect(await portal.count("mods.factorio.com", "/api/mods")).toBe(2);
  expect((await viaProxy(portal, "mods.factorio.com", "/api/mods/no-such-mod-here/full")).status).toBe(404);
});

test("a download needs credentials, then redirects to a download host with a matching SHA-1", async ({ fakePortal }) => {
  const portal = fakePortal!;
  const rel = await portal.add("fp-smoke", "1.2.3", ["base >= 2.0"]);

  // With no credentials the real portal sends the client to its login page.
  const anon = await viaProxy(portal, "mods.factorio.com", rel.download_url);
  expect(anon.status).toBe(302);
  expect(anon.location).toMatch(/^\/login/);

  const signed = await viaProxy(portal, "mods.factorio.com", `${rel.download_url}?${creds(portal)}`);
  expect(signed.status).toBe(302);
  const to = new URL(signed.location!);
  expect(to.protocol).toBe("https:");
  expect(to.hostname).toBe("dl-mod.factorio.com");

  const file = await viaProxy(portal, to.hostname, to.pathname);
  expect(file.status).toBe(200);
  expect(createHash("sha1").update(file.body).digest("hex")).toBe(rel.sha1);
  expect(await portal.count("dl-mod.factorio.com", "/cdn/")).toBe(1);

  // The added mod is in the list a search reads.
  const list = await viaProxy(portal, "mods.factorio.com", "/api/mods?page_size=max&version=2.0");
  expect(list.body.toString()).toContain("fp-smoke");
});

test("the control channel switches failure modes", async ({ fakePortal }) => {
  const portal = fakePortal!;
  const rel = await portal.add("fp-modes", "1.0.0");
  const dl = `${rel.download_url}?${creds(portal)}`;

  await portal.setMode("fp-modes", "1.0.0", "wrong-sha1");
  const bad = await viaProxy(portal, "mods.factorio.com", dl);
  expect(bad.status).toBe(200);
  expect(createHash("sha1").update(bad.body).digest("hex")).not.toBe(rel.sha1);

  await portal.setMode("fp-modes", "1.0.0", "unauthorized");
  expect((await viaProxy(portal, "mods.factorio.com", dl)).status).toBe(401);

  await portal.failApi(500);
  expect((await viaProxy(portal, "mods.factorio.com", "/api/mods/fp-modes/full")).status).toBe(500);
  await portal.failApi(0);
  expect((await viaProxy(portal, "mods.factorio.com", "/api/mods/fp-modes/full")).status).toBe(200);

  await portal.setDown(true);
  await expect(viaProxy(portal, "mods.factorio.com", "/api/mods/fp-modes/full")).rejects.toThrow(/503/);
  await portal.setDown(false);
  expect((await viaProxy(portal, "mods.factorio.com", "/api/mods/fp-modes/full")).status).toBe(200);
});

// The fake is the only way out, and it must lead nowhere but the portal's own hosts.
test("the proxy refuses every host that is not the portal", async ({ fakePortal }) => {
  const portal = fakePortal!;
  for (const host of ["example.com", "factorio.com", "registry-1.docker.io", "mods.factorio.com.evil.example"]) {
    await expect(viaProxy(portal, host, "/"), host).rejects.toThrow(/403/);
  }
  const state = await portal.state();
  expect(state.refused).toEqual(expect.arrayContaining(["example.com:443", "registry-1.docker.io:443"]));
  expect(state.requests).toEqual([]);
});

test("a client that does not trust the test authority rejects the fake's certificate", async ({ fakePortal }) => {
  const proxy = new URL(fakePortal!.proxy);
  const rejected = new Promise<string>((resolve, reject) => {
    const connect = httpRequest({ host: proxy.hostname, port: proxy.port, method: "CONNECT", path: "mods.factorio.com:443" });
    connect.on("error", reject);
    connect.on("connect", (_res, socket) => {
      const tls = tlsConnect({ socket, servername: "mods.factorio.com" }); // the system roots
      tls.on("secureConnect", () => reject(new Error("the handshake succeeded without the test authority")));
      tls.on("error", (e) => resolve(e.message));
    });
    connect.end();
  });
  expect(await rejected).toMatch(/certificate|issuer/i);
});

// Whether Foreman's own portal client reaches the fake is covered by the Go
// test in cmd/fakeportal (the real client, driven by the same two variables)
// and, once the Mods screens exist, by the mods specs.
test("Foreman starts and runs with the fake portal's environment", async ({ page, foreman, fakePortal }) => {
  await createAdmin(page);
  await page.goto("/worlds");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Worlds");
  expect(foreman.logs()).not.toContain('"level":"ERROR"');
  // No page needs the portal yet, so nothing was asked of it.
  expect((await fakePortal!.state()).refused).toEqual([]);
});
