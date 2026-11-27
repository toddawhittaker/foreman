import type { Locator, Page } from "@playwright/test";
import { darkBg, lightBg, test, expect, checkPage, createAdmin, setUpModsWorld, seedSecond, stateBadge, live } from "./foreman.js";

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
// component and the specimen's selector inside a theme's pane. A specimen with
// no pair here has no page in this suite that shows it (the report lists them).
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
  // Compare the margins too: they set this component apart from its neighbours.
  margins?: boolean;
}

const server: Pair[] = [
  { name: "top bar logo", real: "header.site > .product img", design: "header.site > .product img", box: "both" },
  { name: "top bar tab", real: "header.site .tabs li:nth-child(2) a", design: "header.site .tabs li:nth-child(2) a", box: "height" },
  { name: "top bar current tab", real: "header.site .tabs a[aria-current]", design: "header.site .tabs a[aria-current]", box: "height" },
  { name: "account menu", real: "header.site > .menu > summary", design: "header.site > .menu > summary", box: "both" },
  { name: "crumb", real: "nav.crumb", design: "nav.crumb", box: "height" },
  { name: "server tabs", real: "nav.subnav", design: "nav.subnav[aria-label='Server pages']", box: "height" },
  { name: "server tab", real: "nav.subnav li:nth-child(2) a", design: "nav.subnav[aria-label='Server pages'] li:nth-child(2) a", box: "both" },
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
  { name: "worlds area tabs", real: "nav.subnav--worlds", design: "nav.subnav--worlds", box: "height" },
  { name: "worlds area tab", real: "nav.subnav--worlds li:nth-child(2) a", design: "nav.subnav--worlds li:nth-child(2) a", box: "height" },
  { name: "live chip badge", real: ".live-chip .badge--active", design: ".live-chip .badge--active", box: "both" },
  { name: "state tag", real: ".live-chip .state", design: ".live-chip .state", box: "both" },
];

const settings: Pair[] = [
  { name: "short field input", real: ".field--short input", design: ".field--short input", box: "both" },
  { name: "short field label", real: ".field--short label", design: ".field--short label", box: "height" },
];

const general: Pair[] = [
  { name: "settings tabs", real: "nav.subnav--settings", design: "nav.subnav--settings", box: "height" },
  { name: "settings tab", real: "nav.subnav--settings li:nth-child(2) a", design: "nav.subnav--settings li:nth-child(2) a", box: "height" },
  { name: "field label", real: ".setting-row .field > label", design: ".field > label[for$='-name']", box: "height" },
  { name: "field input", real: ".setting-row .field input[type=text]", design: ".field > input[type=text]", box: "height" },
  { name: "field hint", real: ".setting-row .field .hint", design: ".field > .hint", box: "none" },
];

const mods: Pair[] = [
  { name: "world tabs", real: "nav.subnav[aria-label='World pages']", design: "nav.subnav[aria-label='World pages']", box: "height" },
  { name: "world tab", real: "nav.subnav[aria-label='World pages'] li:nth-child(1) a", design: "nav.subnav[aria-label='World pages'] li:nth-child(1) a", box: "both" },
  { name: "current world tab", real: "nav.subnav[aria-label='World pages'] a[aria-current]", design: "nav.subnav[aria-label='World pages'] a[aria-current]", box: "height" },
  { name: "mod list", real: "table.mod-list", design: "table.mod-list", box: "none" },
  { name: "mod row", real: "table.mod-list tbody tr", design: "table.mod-list tbody tr:nth-child(2)", box: "height" },
  { name: "mod name", real: ".mod-line .mod-name:has-text(\"flow-control\")", design: ".mod-line .mod-name:has-text(\"flow-control\")", box: "both" },
  { name: "mod version", real: ".mod-line .mod-ver", design: ".mod-line .mod-ver", box: "both" },
  { name: "mod enable button", real: "table.mod-list .mod-toggle.button", design: "table.mod-list a.mod-toggle.button", box: "both" },
  { name: "more menu toggle", real: ".row-menu__toggle", design: ".row-menu__toggle", box: "both" },
  { name: "message note", real: ".message--note", design: ".message--note", box: "none" },
  { name: "message warning", real: ".message--warning", design: ".message--warning:not(.message--closable)", box: "none" },
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

const generate: Pair[] = [
  { name: "step bar", real: "nav.flow-steps", design: "nav.flow-steps", box: "height" },
  { name: "step mark", real: ".flow-step--current .flow-step__mark", design: ".flow-step--current .flow-step__mark", box: "both" },
  { name: "step name", real: ".flow-step--current .flow-step__name", design: ".flow-step--current .flow-step__name", box: "height" },
];

test.describe("specimens match the real components", () => {
  test.use({ fakeGameOn: true });

  test("on the pages that draw them, in both themes", async ({ page, foreman, fakeGame }) => {
    await setUpModsWorld(page, foreman, fakeGame!, { mods: { "flow-control": "2.1", other: { factorio: "2.1", enabled: false } }, start: true });
    // A world that is not active can be deleted, so its confirmation page is the component's.
    seedSecond(foreman);
    await page.setViewportSize({ width: 1280, height: 900 });
    await expect(stateBadge(page)).toHaveText("Running", live);

    const pages: [string, Pair[]][] = [
      ["/", server],
      ["/worlds", worlds],
      ["/worlds/base/settings?section=players", settings],
      ["/settings/general", general],
      ["/worlds/base/mods", mods],
      ["/generate", generate],
      ["/worlds/second/delete", confirm],
      ["/settings/api-tokens", tokens],
    ];
    const names = pages.flatMap(([, pairs]) => pairs.map((p) => p.name));
    expect(names.length, "each pair has its own name").toBe(new Set(names).size);
    for (const theme of ["light", "dark"] as const) {
      await page.emulateMedia({ colorScheme: theme });
      const real: Record<string, Look> = {};
      for (const [path, pairs] of pages) {
        await page.goto(path);
        for (const p of pairs) real[p.name] = await look(page.locator(p.real), p.box ?? "both", p.margins);
      }
      await page.goto("/design");
      const pane = page.locator(`.design-pane[data-theme="${theme}"]`);
      for (const [, pairs] of pages) {
        for (const p of pairs) {
          const section = p.design.startsWith(".design-pane >") ? page.locator(p.design.replace(".design-pane", `.design-pane[data-theme="${theme}"]`)) : pane.locator(p.design);
          expect.soft(await look(section, p.box ?? "both", p.margins), `${theme}: ${p.name}`).toEqual(real[p.name]);
        }
      }
    }
  });
});
