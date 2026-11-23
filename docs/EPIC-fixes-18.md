# Fixes batch 18

Branch `epic/fixes-18`, cut on 2026-11-23 from main `667762a6`. The last two
open issues: #914 (mod icons from the world's mod zips) and #920 (the star
map). The fold task (G) deletes this file.

## Design facts (orchestrator, from the architect's scope)

- The game's runtime API gives planet positions and routes, but no icon
  paths. Foreman's control mod gains a small `data-final-fixes.lua` that
  copies icon paths from `data.raw` into one `mod-data` prototype, which
  `control.lua` writes out. The control mod takes an optional dependency on
  each world mod so it loads last. ADR 0036 records this.
- Both outputs (`icons.json`, `starmap.json`) come from the mod settings
  runs that already exist (definitions run and planets run), cached with
  their results. No new game run.
- The icon request never carries a file path: the page asks for
  `/worlds/{id}/icons/{kind}/{name}.png` and the server finds the path in
  the cached run output, checks it twice, and reads it from the installed
  zip without extracting. The answer is a freshly encoded PNG. ADR 0037.

## Rulings (Todd, 2026-11-23, unless marked "orchestrator")

1. **Hidden locations:** left out of the star map, with every route that
   touches one.
2. **Moons:** drawn where the game places them, with a faint orbit ring
   around the parent when the data names one (PlanetsLib).
3. **Planet look:** every location is a theme-coloured circle sized by
   `magnitude`. A modded planet's own starmap icon goes on top; a base-game
   planet, and the sun, get the wiki's pictures on top while the wiki setting
   is on (D confirms which wiki files exist). A
   failed image leaves the circle. Pages carry `Referrer-Policy:
   no-referrer` while the wiki setting is on (SPEC 14.10).
4. **Where it shows:** one more picture in Generate's map preview, beside
   the planet pictures ("Star map"); it opens in the large map view like a
   planet picture, with the same zoom and pan; and it is one more file in
   the "Download all drawn planets" zip. No table, no separate page, no
   world-page map.
5. **Labels:** drawn on the map at `label_orientation`. The picture's
   accessible name names it as the star map; each planet has a `<title>`
   (orchestrator: the smallest text alternative SPEC 17 allows).
6. **Curved routes:** routes are drawn as the game draws them. Community
   reports give the rule: a route arcs around the two locations' nearest
   common parent (the sun for planets), and is straight when that parent is
   one of the two ends or there is no parent but the universe origin. The
   arc is taken as a spiral around the parent, the radius and angle each
   changing evenly from one end to the other (orchestrator; D checks it
   against Todd's in-game screenshot and the real-game test). To match that
   screenshot, the map also draws a plain sun circle and a faint ring at
   each planet's distance (orchestrator).
7. **Layered mod icons:** an icon shows only for a plain `icon`, or `icons`
   with one untinted layer. Everything else shows its name.
8. **Zip file format** (orchestrator): the star map goes into the zip as
   `starmap.svg`, the same SVG the page draws, so no rasteriser is added.
9. **Mod icons show** only on per-world pages whose run is cached: the mod
   settings page and Generate's Mod settings step. Order: mod icon, then
   wiki icon, then the name (orchestrator).

## Tasks

| Id | Wave | Owner, model | Files it may touch | Issue |
|---|---|---|---|---|
| A | 1 | builder, Sonnet high | new `internal/mapgen/starmap.go` (+test), new `internal/modsettings/iconindex.go` (+test), `internal/game/modsettings.go` (two new `ModSettingDefs` fields only), `internal/game/modplanets.go` (exported cache wrapper only) | both |
| B | 1 | builder, Sonnet high | new `internal/world/modicon.go` (+test) | #914 |
| C | 2 (after A) | builder, Opus | `internal/game/modsettings/control.lua`, new `internal/game/modsettings/data-final-fixes.lua`, `internal/game/modsettings/modsettings.go`, `internal/game/controlmod.go` (+test), `internal/game/modsettings.go`, `internal/gameenv/modsettings.go`, `internal/fakegame/modsettings.go` (+tests), `e2e/testdata/fake-planet_1.0.0.zip`, new `internal/game/realgame2_starmap_icons_game_test.go` | both |
| D | 2 (after A) | ui-designer, Opus | new `internal/web/starmap.go` (+test), new `internal/web/templates/starmap.html`, `internal/web/genpreview.go` (+tests), `internal/web/templates/generatepreview.html`, `internal/web/static/preview.js`, `internal/web/static/viewer.js` (large view), `internal/web/static/foreman.css`, `internal/i18n/locales/*`, `internal/i18n/translated/*`, `docs/DESIGN.md` | #920 |
| E | 2 (after A, B) | builder, Sonnet high | `internal/modsettings/richtext.go`, `internal/modsettings/wikiicon.go` (+tests), new `internal/web/modicons.go` (+test), `internal/web/server.go` (one routes line), `internal/web/render.go`, `internal/web/htmx.go`, `internal/web/modsettings.go`, `internal/web/flowmodsettings.go`, `e2e/tests/wiki-icons.spec.ts` (only if the marker change breaks it) | #914 |
| F | 3 (after C, D, E) | tester, Sonnet high | new `e2e/tests/mod-icons.spec.ts`, new `e2e/tests/starmap.spec.ts`, `e2e/tests/download-all.spec.ts` (star map file in the zip), `e2e/tests/large-view.spec.ts` (star map in the large view) | both |
| G | 4 | builder, Sonnet high | `docs/SPEC.md` (5.6, 9.6, 14.5, 14.6, 14.10, 17), `docs/STATUS.md`, new ADRs 0036 and 0037, delete this file | both |

Reviews: security-reviewer on A, B, C, E; a11y-reviewer on D, E; code-reviewer
on all. The orchestrator runs the real-game check (epic-run step 11): a world
with a real planet mod, its mod settings page, the star map and one icon.

## Done looks like

- #914: an object tag such as `[item=fake-widget]` in a mod setting shows
  the mod's own icon, with the object's name as `alt`, whether the wiki
  setting is on or off. Built-in game paths are never served.
- #920: Generate's map preview has a Star map picture with the game's
  planet positions, curved routes as in the game, moon orbits, hidden locations left out; it
  opens in the large view with zoom and pan; the zip holds `starmap.svg`.
- Unit tests for every path check, limit and crop; e2e tests with the fake
  game and fake mod zip; real-game tests on the oldest and newest 2.x images.

## Left out

Folder (unzipped) mods' icons; 1.1 worlds (no planets, no `mod-data`); which
planets a save has discovered; icons on the
portal search, mod plan and public mods page; star map in the REST API and
MCP; 2.1 route `shape` overrides, if any; layered icons (ruling 7).
