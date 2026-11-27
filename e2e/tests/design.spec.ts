import type { Locator, Page } from "@playwright/test";
import { darkBg, lightBg, test, expect, checkPage, createAdmin, setUpModsWorld, seedSecond, stateBadge, live, openMenu, signOut, adminPassword } from "./foreman.js";

// The component page draws every group once in each theme, whatever the
// system's, passes axe and fits 320 px (DESIGN 7, 10).
test("the component page shows each group in both themes", { tag: "@looks" }, async ({ page }) => {
  await createAdmin(page);
  await page.goto("/design");
  await expect(page.getByRole("heading", { level: 1, name: "Components" })).toBeVisible();
  const light = page.locator('.design-pane[data-theme="light"]');
  const dark = page.locator('.design-pane[data-theme="dark"]');
  await expect(light).toHaveCount(11);
  await expect(dark).toHaveCount(11);
  await expect(light.first()).toHaveCSS("background-color", lightBg);
  await expect(dark.first()).toHaveCSS("background-color", darkBg);
  // Inherited text takes the pane's theme, not the page's.
  await expect(light.locator(".confirm li").first()).toHaveCSS("color", "rgb(28, 25, 21)");
  await expect(dark.locator(".confirm li").first()).toHaveCSS("color", "rgb(236, 231, 223)");
  // DESIGN 7.5: the crash-looping tag is solid in both themes, on-danger ink
  // on the danger fill, not the other tags' tint of their own ink.
  for (const pane of [light, dark]) {
    const tag = pane.locator(".state--crash-looping").first();
    await expect(tag).toBeVisible();
    const [fill, ink] = await tag.evaluate((el) => {
      const probe = document.createElement("span");
      el.parentElement!.append(probe);
      probe.style.color = "var(--color-danger)";
      const danger = getComputedStyle(probe).color;
      probe.style.color = "var(--color-on-danger)";
      const onDanger = getComputedStyle(probe).color;
      probe.remove();
      return [getComputedStyle(el).backgroundColor === danger, getComputedStyle(el).color === onDanger];
    });
    expect(fill, "the fill is --color-danger").toBe(true);
    expect(ink, "the ink is --color-on-danger").toBe(true);
  }
  await checkPage(page);
});

// DESIGN 7, docs/EPIC-fixes-20.md ruling 2: a specimen on the component page is
// the real component, so its box and its key styles equal the same component on
// a page of the app, in both themes. Each pair names a page that draws the
// component and the specimen's selector inside a theme's pane.
// Specimens with no pair, since no page in this suite draws them as the
// component page does, are held only by the checks beside the pairs:
//  - the page header with a crumb, buttons and tabs (only the bare header is
//    on a page so far);
//  - the working bar and its idle state (no page uses the partial yet), the
//    success toast, the mod search's pager (it needs a second page of results),
//    the mixed checkbox (a property a script sets) and the copy field;
//  - the map settings' tab list and the guided Generate's step bar marks on
//    steps other than the current one;
//  - the Server plate in its Starting, Stopped and crash states, and the open
//    Emergency actions and More menus;
//  - the plain empty state and the other empty states' drawings;
//  - the form foot's Cancel: the partial draws a ghost button and the Restart
//    schedule page a plain link that the stylesheet makes alike, and the two
//    differ by 4 px of padding, which is a gap to close, not to hide here.
const styleProps = [
  "display", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft",
  "borderTopWidth", "borderBottomWidth", "borderLeftWidth", "borderTopColor", "borderLeftColor", "borderTopLeftRadius",
  "backgroundColor", "color", "fontFamily", "fontSize", "fontWeight", "lineHeight", "letterSpacing", "textTransform",
  "columnGap", "rowGap", "boxShadow", "minHeight",
] as const;
// Where a component sits among its neighbours is the page's rhythm, not the
// component's, so margins are compared only where they are part of it.
const marginProps = ["marginTop", "marginBottom", "marginLeft", "marginRight"] as const;

type Look = Record<string, string | number>;

// What one element looks like: its computed styles and its box. A specimen's
// width follows its pane, so width is compared only for a component that is as
// wide as its words.
async function look(el: Locator, box: "both" | "height" | "none", margins = false): Promise<Look> {
  const first = el.first();
  await expect(first, "the element is on the page").toBeVisible();
  const out = await first.evaluate((node, props) => {
    const cs = getComputedStyle(node);
    const r = node.getBoundingClientRect();
    const o: Record<string, string | number> = {};
    for (const p of props) o[p] = cs[p as keyof CSSStyleDeclaration] as string;
    o.width = r.width;
    o.height = r.height;
    return o;
  }, margins ? [...styleProps, ...marginProps] : styleProps);
  if (box !== "both") delete out.width;
  if (box === "none") delete out.height;
  return out;
}

interface Pair {
  name: string;
  // The selector on the app's page, and inside a theme's pane on /design.
  real: string;
  design: string;
  box?: "both" | "height" | "none";
  // At a width where the component's width follows its box (a tab or a panel
  // that fills the bar), the box to compare there instead.
  boxAt?: Partial<Record<number, "both" | "height" | "none">>;
  // Compare the margins too: they set this component apart from its neighbours.
  margins?: boolean;
  // The window widths to compare at; the default is a wide window. Shell rules
  // change at 40, 48 and 64 rem and on a phone, so they are held at 320 and 768 too.
  widths?: readonly number[];
  // Open the account menu on the real page first.
  menu?: boolean;
}

const wide = [1280] as const;
const shellWidths = [1280, 768, 320] as const;

const server: Pair[] = [
  { name: "top bar logo", real: "header.site > .product img", design: "header.site > .product img", box: "both", widths: shellWidths },
  { name: "top bar tab", real: "header.site .tabs li:nth-child(2) a", design: "header.site .tabs li:nth-child(2) a", box: "height", widths: shellWidths },
  { name: "top bar current tab", real: "header.site .tabs a[aria-current]", design: "header.site .tabs a[aria-current]", box: "height", widths: shellWidths },
  { name: "account menu", real: "header.site > .menu > summary", design: "header.site > .menu > summary", box: "both", widths: shellWidths },
  { name: "account menu panel", real: "header.site .menu__panel", design: ".design-menu-bar .menu__panel", box: "both", menu: true, widths: shellWidths, boxAt: { 320: "height" } },
  { name: "account menu hint", real: "header.site .menu__panel > .hint", design: ".design-menu-bar .menu__panel > .hint", box: "height", menu: true, widths: shellWidths },
  { name: "account menu language", real: "header.site .menu__panel > .menu__langs > summary", design: ".design-menu-bar .menu__panel > .menu__langs > summary", box: "height", menu: true, widths: shellWidths },
  { name: "account menu themes", real: "header.site .menu__themes", design: ".design-menu-bar .menu__themes", box: "height", menu: true, widths: shellWidths },
  { name: "account menu theme button", real: "header.site .menu__themes li:nth-child(2) button", design: ".design-menu-bar .menu__themes li:nth-child(2) button", box: "both", menu: true, widths: shellWidths },
  { name: "account menu about", real: "header.site .menu__panel > a.menu__item", design: ".design-menu-bar .menu__panel > a.menu__item", box: "height", menu: true, widths: shellWidths },
  { name: "account menu sign out", real: "header.site .menu__panel > form:last-child > button.menu__item", design: ".design-menu-bar .menu__panel > form:last-child > button.menu__item", box: "height", menu: true, widths: shellWidths },
  { name: "crumb", real: ".pagehead__crumb > nav.crumb", design: ".pagehead__crumb > nav.crumb", box: "height", widths: shellWidths },
  { name: "server tabs", real: "nav.subnav", design: "nav.subnav[aria-label='Server pages']", box: "height", widths: shellWidths },
  { name: "server tab", real: "nav.subnav li:nth-child(2) a", design: "nav.subnav[aria-label='Server pages'] li:nth-child(2) a", box: "both", widths: shellWidths, boxAt: { 320: "height" } },
  { name: "plate", real: ".plate", design: ".plate.status-card--running", box: "none" },
  { name: "plate band", real: ".plate__band", design: ".status-card--running .plate__band", box: "height" },
  { name: "plate state", real: ".plate__state", design: ".status-card--running .plate__state", box: "both" },
  { name: "plate fact", real: ".plate__fact", design: ".status-card--running .plate__fact", box: "height" },
  { name: "primary action", real: "#act-snapshot", design: ".status-card--running .server-actions .button--primary", box: "both" },
  { name: "restart", real: "#act-restart", design: ".status-card--running [data-design-icon=restart]", box: "both" },
  { name: "stop", real: "#act-stop", design: ".status-card--running [data-design-icon=stop]", box: "both", margins: true },
  { name: "emergency actions", real: ".emergency > summary", design: ".status-card--running .emergency > summary", box: "both" },
  { name: "stat", real: ".plate .stat", design: ".status-card--running .stat", box: "height" },
  { name: "meter", real: ".plate .meter", design: ".status-card--running .meter", box: "height" },
  { name: "play time", real: ".plate .play-time", design: ".status-card--running .play-time", box: "height" },
  { name: "shortcuts", real: ".shortcuts", design: ".shortcuts", box: "height" },
  { name: "shortcut", real: ".shortcuts > a", design: ".shortcuts > a", box: "height" },
  { name: "players card head", real: ".server-grid .section-head", design: ".server-grid .section-head", box: "height" },
  { name: "players table head", real: ".server-players th", design: ".server-players th", box: "height" },
  { name: "players table cell", real: ".server-players tbody tr:last-child td:first-child", design: ".server-players tbody tr:last-child td:first-child", box: "height" },
  { name: "players table time", real: ".server-players tbody tr:last-child td.num", design: ".server-players tbody tr:last-child td.num", box: "height" },
  { name: "snapshots card head", real: ".recent-snaps > h2", design: ".recent-snaps > h2", box: "height" },
  { name: "snapshots card link", real: ".recent-snaps .actions a", design: ".recent-snaps .actions a", box: "height" },
];

const worlds: Pair[] = [
  { name: "worlds area tabs", real: "nav.subnav--worlds", design: "nav.subnav--worlds", box: "height", widths: shellWidths },
  { name: "worlds area tab", real: "nav.subnav--worlds li:nth-child(2) a", design: "nav.subnav--worlds li:nth-child(2) a", box: "height", widths: shellWidths },
  { name: "live chip badge", real: ".live-chip .badge--active", design: ".live-chip .badge--active", box: "both" },
  { name: "state tag", real: ".live-chip .state", design: ".live-chip .state", box: "both" },
];

const settings: Pair[] = [
  { name: "short field input", real: ".field--short input", design: ".field--short input", box: "both" },
  { name: "short field label", real: ".field--short label", design: ".field--short label", box: "height" },
];

const general: Pair[] = [
  { name: "settings tabs", real: "nav.subnav--settings", design: "nav.subnav--settings", box: "height", widths: shellWidths },
  { name: "settings tab", real: "nav.subnav--settings li:nth-child(2) a", design: "nav.subnav--settings li:nth-child(2) a", box: "height", widths: shellWidths },
  { name: "field label", real: ".setting-row .field > label", design: ".field > label[for$='-name']", box: "height" },
  { name: "field input", real: ".setting-row .field input[type=text]", design: ".field > input[type=text]", box: "height" },
  { name: "field hint", real: ".setting-row .field .hint", design: ".field > .hint", box: "none" },
];

const mods: Pair[] = [
  { name: "world tabs", real: "nav.subnav[aria-label='World pages']", design: "nav.subnav[aria-label='World pages']", box: "height", widths: shellWidths },
  { name: "world tab", real: "nav.subnav[aria-label='World pages'] li:nth-child(1) a", design: "nav.subnav[aria-label='World pages'] li:nth-child(1) a", box: "both", widths: shellWidths, boxAt: { 320: "height" } },
  { name: "current world tab", real: "nav.subnav[aria-label='World pages'] a[aria-current]", design: "nav.subnav[aria-label='World pages'] a[aria-current]", box: "height", widths: shellWidths },
  { name: "mod list", real: "table.mod-list", design: "table.mod-list", box: "none" },
  { name: "mod row", real: "table.mod-list tbody tr", design: "table.mod-list tbody tr:nth-child(2)", box: "height" },
  { name: "mod name", real: ".mod-line .mod-name:has-text(\"flow-control\")", design: ".mod-line .mod-name:has-text(\"flow-control\")", box: "both" },
  { name: "mod version", real: ".mod-line .mod-ver", design: ".mod-line .mod-ver", box: "both" },
  { name: "mod enable button", real: "table.mod-list .mod-toggle.button", design: "table.mod-list a.mod-toggle.button", box: "both" },
  { name: "more menu toggle", real: ".row-menu__toggle", design: ".row-menu__toggle", box: "both" },
  { name: "message note", real: ".message--note", design: ".message--note", box: "none" },
  { name: "message warning", real: ".message--warning", design: ".message--warning", box: "none" },
  { name: "message action", real: ".message--warning .actions .button", design: ".message--warning .actions .button", box: "height" },
  { name: "mod table head", real: "table.mod-list th", design: "table.mod-list th", box: "height" },
  { name: "off badge", real: ".mod-line .badge", design: ".mod-line .badge", box: "both" },
  { name: "disclosure", real: "details.disclosure:not(#mod-tools) > summary", design: "details.disclosure:not(.emergency, .player-actions) > summary", box: "height" },
];

const tokens: Pair[] = [
  { name: "segmented", real: "fieldset.segmented", design: "fieldset.segmented", box: "height" },
  { name: "segment", real: "fieldset.segmented label:first-of-type", design: "fieldset.segmented label:first-of-type", box: "height" },
];

const confirm: Pair[] = [
  { name: "confirm box", real: ".confirm", design: ".confirm", box: "none" },
  { name: "confirm field label", real: ".confirm .field label", design: ".confirm .field label", box: "height" },
  { name: "confirm field input", real: ".confirm .field input", design: ".confirm .field input", box: "height" },
  { name: "danger button", real: ".confirm .button--danger", design: ".confirm .button--danger", box: "both" },
];

// A page that has no route of its own draws the header partial bare: the signed-in not-found page.
const pageHead: Pair[] = [
  { name: "page header", real: "main .pagehead", design: ".pagehead:not(:has(nav))", box: "height", widths: shellWidths },
  { name: "page header crumb row", real: "main .pagehead__crumb", design: ".pagehead:not(:has(nav)) .pagehead__crumb", box: "height", widths: shellWidths },
  { name: "page title", real: "main .pagehead > h1", design: ".pagehead:not(:has(nav)) > h2", box: "height", widths: shellWidths },
];

const deleted: Pair[] = [
  { name: "empty state", real: "main .empty--deleted", design: ".empty--deleted", box: "height" },
];

const generalFoot: Pair[] = [
  { name: "form foot with note", real: "main .form-foot", design: ".form-foot:has(.form-foot__note)", box: "height" },
  { name: "form foot note", real: "main .form-foot__note", design: ".form-foot__note", box: "height" },
  { name: "form foot save", real: "main .form-foot .button--primary", design: ".form-foot:has(.form-foot__note) .button--primary", box: "both" },
];

const scheduleFoot: Pair[] = [
  { name: "form foot with cancel", real: "main .form-foot", design: ".form-foot:has(> a)", box: "height" },
  { name: "form foot cancel save", real: "main .form-foot .button--primary", design: ".form-foot:has(> a) .button--primary", box: "both" },
];

const generate: Pair[] = [
  { name: "step bar", real: "nav.flow-steps", design: "nav.flow-steps", box: "height", widths: shellWidths },
  { name: "step mark", real: ".flow-step--current .flow-step__mark", design: ".flow-step--current .flow-step__mark", box: "both", widths: shellWidths },
  { name: "step name", real: ".flow-step--current .flow-step__name", design: ".flow-step--current .flow-step__name", box: "height", widths: shellWidths },
];

// A snapshot taken first gives the page its newest-snapshots list; its Done
// message is a toast, which no pair measures.
async function takeSnapshot(page: Page) {
  if ((await page.locator(".recent-snaps__list").count()) > 0) return;
  await page.locator("#act-snapshot").click();
  await expect(page.locator(".recent-snaps__list")).toBeVisible(live);
}

test.describe("specimens match the real components", () => {
  test.use({ fakeGameOn: true });

  // One test per width, so each stays well under the time cap.
  for (const width of shellWidths) test(`on the pages that draw them, in both themes, at ${width} px`, async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { mods: { "flow-control": "2.1", other: { factorio: "2.1", enabled: false } }, start: true });
    // A world that is not active can be deleted, so its confirmation page is the component's.
    seedSecond(foreman);
    await expect(stateBadge(page)).toHaveText("Running", live);

    const pages: [string, Pair[], ((p: Page) => Promise<void>)?][] = [
      ["/", server, takeSnapshot],
      ["/worlds", worlds],
      ["/worlds/base/settings?section=players", settings],
      ["/settings/general", [...general, ...generalFoot]],
      ["/settings/restart-schedule", scheduleFoot],
      ["/worlds/base/mods", mods],
      ["/generate", generate],
      ["/worlds/second/delete", confirm],
      ["/settings/api-tokens", tokens],
      ["/deleted", deleted],
      ["/no-such-page", pageHead],
    ];
    const names = pages.flatMap(([, pairs]) => pairs.map((p) => p.name));
    expect(names.length, "each pair has its own name").toBe(new Set(names).size);
    await page.setViewportSize({ width, height: 900 });
    for (const theme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme: theme });
      const real: Record<string, Look> = {};
      const here = pages.map(([path, pairs, prepare]) => ({ path, prepare, pairs: pairs.filter((p) => (p.widths ?? wide).includes(width)) })).filter((e) => e.pairs.length > 0);
      for (const { path, pairs, prepare } of here) {
        await page.goto(path);
        await prepare?.(page);
        for (const p of pairs) {
          if (p.menu) await openMenu(page);
          real[p.name] = await look(page.locator(p.real), p.boxAt?.[width] ?? p.box ?? "both", p.margins);
        }
      }
      await page.goto("/design");
      const pane = page.locator(`.design-pane[data-theme="${theme}"]`);
      for (const { pairs } of here) {
        for (const p of pairs) {
          expect.soft(await look(pane.locator(p.design), p.boxAt?.[width] ?? p.box ?? "both", p.margins), `${theme} at ${width}: ${p.name}`).toEqual(real[p.name]);
        }
      }
    }
  });
});

// The toast on the sign-in page is a rate-limit refusal, which takes six wrong
// passwords to reach. The specimens are read first, while signed in.
test("the error toast specimen matches the sign-in page's toast, in both themes", async ({ page }) => {
  await createAdmin(page);
  const parts: [string, "both" | "height" | "none"][] = [[".toast--error", "none"], [".toast--error .toast__text", "none"], [".toast--error strong", "both"], [".toast--error .toast__close", "both"]];
  const specimen: Record<string, Look> = {};
  await page.goto("/design");
  for (const theme of ["light", "dark"] as const) {
    for (const [sel, box] of parts) specimen[`${theme} ${sel}`] = await look(page.locator(`.design-pane[data-theme="${theme}"]`).locator(sel), box);
  }
  // The component page holds more than one menu, so the sign-out goes from a real page.
  await page.goto("/");
  await signOut(page);
  for (let i = 0; i < 6; i++) {
    await page.goto("/signin");
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password", { exact: true }).fill(adminPassword + " not");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
  }
  for (const theme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: theme });
    await page.goto("/signin");
    await page.getByLabel("Username").fill("admin");
    await page.getByLabel("Password", { exact: true }).fill(adminPassword + " not");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.locator("#toasts > .toast--error")).toBeVisible();
    for (const [sel, box] of parts) {
      expect.soft(await look(page.locator("#toasts").locator(sel), box), `${theme}: ${sel}`).toEqual(specimen[`${theme} ${sel}`]);
    }
  }
});

// The specimen is held to its own box: a 320 by 8 track with its words beside
// it, and the idle bar keeps that room. The bar in the starting plate's step
// list is the plate's, so it is left out here.
test("the working bar specimens are 320 by 8 with words beside, and the idle one keeps its room", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/design");
  for (const theme of ["light", "dark"] as const) {
    const bars = page.locator(`.design-pane[data-theme="${theme}"] .working:not(.steps .working)`);
    await expect(bars).toHaveCount(2);
    const box = async (i: number) => (await bars.nth(i).locator(".working__bar").boundingBox())!;
    expect(await box(0), `${theme}: the bar`).toMatchObject({ width: 320, height: 8 });
    expect(await box(1), `${theme}: the idle bar`).toMatchObject({ width: 320, height: 8 });
    await expect(bars.nth(1)).toHaveCSS("visibility", "hidden");
    const bar = await box(0);
    const words = (await bars.nth(0).locator(".working__text").boundingBox())!;
    expect(words.x, `${theme}: words beside the bar`).toBeGreaterThan(bar.x + bar.width);
    expect(Math.round(words.y + words.height / 2), `${theme}: words centred on the bar`).toBe(Math.round(bar.y + bar.height / 2));
  }
});

// A mixed checkbox has no attribute, so the page's script sets the property.
test("the mixed checkbox specimens are mixed", async ({ page }) => {
  await createAdmin(page);
  await page.goto("/design");
  for (const theme of ["light", "dark"] as const) {
    const box = page.locator(`.design-pane[data-theme="${theme}"] input[data-indeterminate]`);
    await expect(box).toHaveCount(1);
    expect(await box.evaluate((el: HTMLInputElement) => el.indeterminate), theme).toBe(true);
  }
});

// The open account menu floats under its bar, so the specimen keeps room for
// it: the panel ends inside that room, and nothing after it is covered.
test("the open account menu specimen keeps room for its panel", async ({ page }) => {
  await createAdmin(page);
  for (const width of [1280, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/design");
    for (const theme of ["light", "dark"] as const) {
      const room = page.locator(`.design-pane[data-theme="${theme}"] .design-menu-bar`);
      const r = (await room.boundingBox())!;
      const p = (await room.locator(".menu__panel").boundingBox())!;
      expect(p.y + p.height, `${theme} at ${width}: the panel ends inside its room`).toBeLessThanOrEqual(r.y + r.height);
    }
  }
});

// Under reduced motion the bar stands still: no animation, and its segment
// fills the track so the bar stays in view.
test("the working bar specimens stand still under reduced motion", async ({ page }) => {
  await createAdmin(page);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/design");
  for (const theme of ["light", "dark"] as const) {
    const bar = page.locator(`.design-pane[data-theme="${theme}"] .working`).first().locator(".working__bar");
    const seg = await bar.evaluate((b) => {
      const cs = getComputedStyle(b, "::before");
      return { animation: cs.animationName, visibility: cs.visibility, fills: parseFloat(cs.width) >= parseFloat(getComputedStyle(b).width) - 4 };
    });
    expect(seg, theme).toEqual({ animation: "none", visibility: "visible", fills: true });
  }
});
