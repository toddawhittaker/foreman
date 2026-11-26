# Fixes batch 19

Branch `epic/fixes-19`, cut on 2026-11-26 from main `a2061ef2`. One issue:
#955, counting spots of tiles that carry a fluid (Vesta's methane and
hydrogen sulfide pools). The fold task (B) deletes this file.

## Rulings (Todd, unless marked "orchestrator")

1. **Count fluid-tile spots, on any planet** (2026-11-25, in #955). A spot
   is a connected group of tiles that carry a fluid, joined by the same gap
   rule as ore patches. Each spot's label gives its size in tiles: pumping
   never runs out, so there is no amount. The text list groups spots by
   fluid, for example "Methane gas: 14 spots".
2. **Only tiles tied to a resource control count** (2026-11-26). A tile
   counts when it carries a fluid and its autoplace names a control whose
   category is `resource`, as Vesta's `tritium` and `deuterium` tiles do.
   The game's own oceans (Nauvis water, Aquilo ammonia, Fulgora oil) follow
   terrain settings, so they drop out with no rule for base planets. A mod
   that places fluid pools with no slider gets no count; that is a STATUS
   gap.
3. **Real-game check (orchestrator).** The real-game resource test gains a
   case that runs only when `FOREMAN_FLUID_TILE_MODS` names a folder of mod
   zips, and is skipped otherwise. Mod zips are never committed. The
   orchestrator runs it with the Vesta zips and their dependencies from the
   trial world `foo-2`, and the test asks for at least one methane and one
   hydrogen sulfide spot on Vesta.
4. **The zero-patch sentence (orchestrator).** It stays for a planet whose
   count still finds nothing, but no longer suggests tiles when the planet
   has a fluid tile tied to a resource control. SPEC 5.6's "Known limit"
   changes to say which resources still cannot be counted.

## Tasks

| Task | Owner, model | Files it may touch | Closes |
| --- | --- | --- | --- |
| A. Count fluid-tile spots, end to end | builder, Opus, medium effort | `internal/game/resources/` (`control.lua`, `resources.go` and their tests), `internal/game/resources.go`, `internal/game/*_game_test.go` for the resource case, `internal/resource/` (`resource.go`, `labels.go` and their tests), `internal/web/genresources.go` and its tests, the template and script that draw the labels and the text list, `internal/fakegame/resources.go` and its fake planet data, `internal/i18n/locales/*`, `e2e/tests/resources.spec.ts`, SPEC 5.6 | #955 |
| B. Fold | builder, Sonnet, high effort | `docs/STATUS.md`, `docs/SPEC.md`, this file (deleted) | |

One wave: A, then B. Task A is one task because the result format, its
decoder, the labels and the list change together, and splitting them would
chain every file.

## What A delivers

- `control.lua` finds the fluid tiles tied to a resource control, groups
  them by fluid with the ore gap, and writes each spot as a patch with its
  tile count and no amount. Older result files still decode.
- Labels give the spot's size ("312 tiles") in the picture and in the
  downloaded picture. The text list gives each fluid's spot count.
- The fake game gains a planet with fluid tiles, so the browser test shows
  the labels and the list.
- New strings in all eight languages, each reviewed per docs/I18N.md.

## Checks

- Orchestrator: `make verify`, `make e2e` for `resources.spec.ts`,
  `generate-preview.spec.ts` and `large-view.spec.ts`, then the real-game
  resource test with the Vesta mods (ruling 3) and with Space Age alone,
  to show base planets count as before.
- Reviews: code-reviewer and a11y-reviewer on A.
