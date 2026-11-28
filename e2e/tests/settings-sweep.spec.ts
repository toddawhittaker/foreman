import { test, expect, createAdmin } from "./foreman.js";
import type { Foreman } from "./foreman.js";
import type { Page, Locator } from "@playwright/test";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";

// Fixes 20 wave 4 (#986 #987 #989), DESIGN 6 "Controls stay put" and DESIGN 7
// "One completion footer": the Settings pages' re-sweep. Layout holds to 0 px.

const key = "keykeykeykeykeykeykeykeykeykeykey";
test.use({ foremanEnv: { FOREMAN_SECRET_KEY: key } });

const r2 = (n: number) => Math.round(n * 100) / 100;

async function rect(l: Locator) {
  const b = await l.boundingBox();
  expect(b, "the element has a box").not.toBeNull();
  return { x: r2(b!.x), y: r2(b!.y), w: r2(b!.width), h: r2(b!.height), right: r2(b!.x + b!.width), bottom: r2(b!.y + b!.height) };
}

const style = (l: Locator, prop: string) => l.evaluate((e, p) => getComputedStyle(e).getPropertyValue(p), prop);

// DESIGN 7.1 "One completion footer": the password form's Change password ends
// its foot bar, as every other form's primary action does, at every width.
for (const width of [1920, 390]) {
  test(`Change password ends its foot bar at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await createAdmin(page);
    await page.goto("/settings/users");
    const foot = page.locator("#act-password").locator("xpath=..");
    const bar = (await foot.boundingBox())!;
    const button = (await page.locator("#act-password").boundingBox())!;
    // Rounded to the pixel: the engine places the button to 1/64 px.
    expect(Math.round(bar.x + bar.width - parseFloat(await style(foot, "padding-right")) - (button.x + button.width)) + 0).toBe(0);
  });
}

// DESIGN 7: one radio-card look. The webhook Service cards and the API token
// Scope cards place the radio alike and draw rest and chosen alike, in both themes.
async function cards(page: Page, card: string) {
  return page.locator(card).evaluateAll((els) =>
    els.map((el) => {
      const c = el.getBoundingClientRect();
      const r = el.querySelector("input")!.getBoundingClientRect();
      const s = getComputedStyle(el);
      return {
        checked: el.querySelector("input")!.checked,
        dx: Math.round((r.x - c.x) * 100) / 100,
        dy: Math.round((r.y - c.y) * 100) / 100,
        top: Math.round(r.y * 100) / 100,
        look: [s.backgroundColor, s.borderTopColor, s.borderBottomColor, s.boxShadow, s.paddingTop, s.paddingLeft, s.borderRadius].join(" | "),
      };
    }),
  );
}

test("the Service cards and the Scope cards are one radio card", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1000 });
  await createAdmin(page);
  for (const scheme of ["dark", "light"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/settings/notifications/new");
    const service = await cards(page, "#webhook-form .webhook-form__presets > *, .webhook-form__presets > *");
    await page.goto("/settings/api-tokens");
    const scope = await cards(page, ".scope-cards > :not(legend, .hint)");
    expect(service).toHaveLength(4);
    expect(scope).toHaveLength(3);
    const all = [...service, ...scope];
    // The radio sits at one inset in every card.
    for (const c of all) expect([c.dx, c.dy], scheme).toEqual([all[0].dx, all[0].dy]);
    // The cards sharing a row put their radios on one line (the ntfy hint holds a code).
    expect(service[1].top, `${scheme}: ntfy's radio`).toBe(service[0].top);
    expect(service[3].top, `${scheme}: Generic JSON's radio`).toBe(service[2].top);
    // One rest look and one chosen look.
    const chosen = all.filter((c) => c.checked).map((c) => c.look);
    const rest = all.filter((c) => !c.checked).map((c) => c.look);
    expect(chosen).toHaveLength(2);
    expect(new Set(chosen).size, `${scheme}: chosen looks ${chosen.join(" // ")}`).toBe(1);
    expect(new Set(rest).size, `${scheme}: rest looks ${[...new Set(rest)].join(" // ")}`).toBe(1);
  }
});

// A phone lays every webhook row out alike, as the Users list does: the facts
// at the top, then the row's actions under them.
test("every webhook row on a phone has the facts on top and the actions under them", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await createAdmin(page);
  const add = async (preset: string, fill: () => Promise<void>) => {
    await page.goto("/settings/notifications/new");
    await page.locator(`#preset-${preset}`).check();
    await fill();
    await page.getByRole("button", { name: "Add webhook" }).click();
    await expect(page).toHaveURL(/\/settings\/notifications$/);
  };
  await add("discord", () => page.getByLabel("Webhook address").fill("https://discord.com/api/webhooks/42/tok-1"));
  await add("ntfy", () => page.getByLabel("Webhook address").fill("https://ntfy.sh/alerts"));
  await add("pushover", async () => {
    await page.getByLabel("Application token").fill("e2eTOKENsecret6612aaaaaaaaaaaa");
    await page.getByLabel("User key").fill("e2eUSERsecret6613bbbbbbbbbbbbb");
  });
  await add("generic", () => page.getByLabel("Webhook address").fill("https://hooks.example.com/in/abc"));
  const rows = page.locator(".webhook-list > li");
  await expect(rows).toHaveCount(4);
  const shapes = [];
  for (const row of await rows.all()) {
    const li = await rect(row);
    const title = await rect(row.locator("h2"));
    const facts = await rect(row.locator(".facts"));
    const actions = await rect(row.locator(".actions"));
    const edit = await rect(row.locator(".actions .button").first());
    // The title starts the row at its top edge, the facts follow, the actions come last.
    expect(title.x, "title at the row's start").toBe(li.x);
    expect(actions.y, "actions under the facts").toBeGreaterThanOrEqual(facts.bottom);
    expect(actions.x, "actions at the row's start").toBe(li.x);
    expect(actions.right, "actions as wide as the row").toBe(li.right);
    // The first row has no top padding (the list's edge), so measure from the content box.
    const padTop = parseFloat(await style(row, "padding-top"));
    shapes.push({ titleTop: r2(title.y - li.y - padTop), titleW: title.w, editTop: r2(edit.y - actions.y), editX: r2(edit.x - li.x), editW: edit.w });
  }
  for (const s of shapes) expect(s).toEqual(shapes[0]);
});

// The audit log: filtering adds nothing that moves the filter row (DESIGN 6,
// "Controls stay put"), and Newer and Older keep their places on every page.
function seed(f: Foreman) {
  const db = new DatabaseSync(join(f.dataDir, "foreman.db"));
  try {
    const add = db.prepare("INSERT INTO audit_log (time, username, action, world_id, detail, token_name) VALUES (?, ?, ?, ?, ?, ?)");
    const base = Math.floor(Date.now() / 1000) + 3600;
    for (let i = 0; i < 120; i++) add.run(base + i, "admin", "start", null, `entry-${i}`, "");
  } finally {
    db.close();
  }
}

test("filtering the audit log moves no filter control", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1000 });
  await createAdmin(page);
  const controls = ["#audit-user", "#audit-action", "#audit-world", "#audit-size", "#act-audit-filter"];
  const measure = async () => Promise.all(controls.map((c) => rect(page.locator(c))));
  await page.goto("/settings/audit-log");
  const before = await measure();
  await page.locator("#audit-action").selectOption({ index: 1 });
  await page.locator("#act-audit-filter").click();
  await expect(page.locator("#act-audit-clear")).toBeVisible();
  expect(await measure()).toEqual(before);
});

for (const width of [1920, 390]) {
  test(`Newer and Older keep their places on every audit page at ${width} px`, async ({ page, foreman }) => {
    await page.setViewportSize({ width, height: 900 });
    await createAdmin(page);
    seed(foreman);
    await page.goto("/settings/audit-log?size=25");
    const older1 = await rect(page.locator("#act-audit-older"));
    const nav1 = await rect(page.getByRole("navigation", { name: "Log pages" }).last());
    await page.locator("#act-audit-older").click();
    await expect(page.locator("#act-audit-newer")).toBeVisible();
    const older2 = await rect(page.locator("#act-audit-older"));
    const newer2 = await rect(page.locator("#act-audit-newer"));
    const nav2 = await rect(page.getByRole("navigation", { name: "Log pages" }).last());
    // Older's place relative to its bar is the same with and without Newer.
    expect([r2(older2.x - nav2.x), r2(older2.y - nav2.y), older2.w]).toEqual([r2(older1.x - nav1.x), r2(older1.y - nav1.y), older1.w]);
    // Newer comes first, on the same line, at every width.
    expect(newer2.y).toBe(older2.y);
    expect(newer2.right).toBeLessThanOrEqual(older2.x);
  });
}

// DESIGN 7.2: fields in a form stand 16 px apart; the Token created page's
// two addresses are no exception.
test("the Token created page's addresses stand 16 px apart", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1000 });
  await createAdmin(page);
  await page.goto("/settings/api-tokens");
  await page.locator("#token-name").fill("sweep");
  await page.locator("#act-token-create").click();
  const endpoint = await rect(page.locator("#token-endpoint").locator("xpath=ancestor::div[contains(concat(' ', @class, ' '), ' field ')][1]"));
  const base = await rect(page.locator("#token-api-base").locator("xpath=ancestor::div[contains(concat(' ', @class, ' '), ' field ')][1]"));
  expect(r2(base.y - endpoint.bottom)).toBe(16);
});

// DESIGN 7.2: the "* Required" note carries no bottom margin of its own, on
// General as on every other form.
test("General's required note has no bottom margin", async ({ page }) => {
  await createAdmin(page);
  for (const path of ["/settings/general", "/settings/api-tokens"]) {
    await page.goto(path);
    expect(await style(page.locator(".required-note").first(), "margin-bottom"), path).toBe("0px");
  }
});
