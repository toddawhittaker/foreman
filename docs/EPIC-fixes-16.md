# Fixes batch 16

Branch `epic/fixes-16`, cut on 2026-11-21 from main `5b718314`. Six issues:
three small fixes (#781, #784, #785) and three features: the players of a
save with their play times (#769), the factory map (#773) and the world
file export and import (#774). The fold task (FD) deletes this file and `docs/epic-fixes-16/`.

Designs this plan is built from (in `docs/epic-fixes-16/`, deleted with this plan by FD):
`docs/epic-fixes-16/design-769-773.md` (#769, #773), `docs/epic-fixes-16/design-774.md` (#774),
`docs/epic-fixes-16/scope-small.md` (#781, #784, #785), and the feasibility report
`docs/epic-fixes-16/feasibility.md` (benchmark run, `/time`, memory). Section numbers
below are the SPEC numbers the doc task D writes.

No task changes how the app looks or behaves except where its issue says
so.

## Rulings

### #769 players of a save, and total play time

1. **Reading the save.** One way for both #769 and #773: a one-off game
   container runs the world's own image with `--benchmark <save>
   --benchmark-ticks 8 --benchmark-runs 1`, no network, the world's mods
   plus a control-only helper mod that writes JSON (SPEC 8.16, ADR 0030).
   It is the fifth and sixth kind of one-off container in SPEC 14.3, with no
   new Docker endpoint. Rejected: a throwaway server with a Lua command over
   RCON (needs a network, contacts auth.factorio.com, puts an RCON password
   in an untrusted container); a throwaway server that is killed when the
   mod's file appears (kept only as the fallback if a game version breaks
   benchmark mode); reading `level.dat` in Go (undocumented, shifts between
   patches). The method lives in one function, `saveCopyCommand` in
   `internal/game/savecopy.go`.
2. **The save is never written.** It is hard-linked into the work folder and
   mounted read-only over the link (a copy when the link fails, after the
   6.11 disk check). `mod-list.json` and `mod-settings.dat` are copies,
   because the game rewrites both. `config/`, `rconpw` and
   `server-settings.json` never enter the work folder.
3. **Mods are checked twice**, because the game drops a missing mod
   silently and exits 0: the pre-start check (`PreStartCheckAt`) runs first,
   and the helper's `active_mods` must equal the world's enabled mods after.
   Either failure gives `mods-differ`, and no figures are kept.
4. **D1: the record is `worlds/<id>/players.json`** (Todd). Snapshots,
   restore and clone carry it with no new code. Format in section "Contracts".
   It is not in a world file (#774): the new host reads its own.
5. **D2: when a read runs** (Todd): automatically while the world is
   stopped and no game runs on this Foreman (confirmed item 2); while it runs, only when an admin clicks "Read players from the
   newest save". The button says the read needs about as much memory as the
   running game uses now, and shows that figure.
6. **D3: where it shows** (Todd): the full list on the world's Overview
   page; the Server page shows total play time and an "N players in this
   save" link to that list.
7. **D7: drift.** A crash, or Foreman being down during a run, marks the
   record inexact; it is read again at the next view while the world is
   stopped. A "Read again" button is always there.
8. **D8: limits.** 12 GiB of memory with no swap, 5 minutes, the one
   preview slot. Measured: 6.9 GB and 44 s for a 187 MB save with 44 mods.
9. **D9: priority.** 8.14, 8.15 and 8.16 are P1.
10. **A read starts from a page view**, never from a scheduler: the world
    page (or the Server page's link count) asks, and the result arrives
    live. Imports of every kind, uploads, clones and restores are covered
    because the record no longer matches the save that loads
    (`LoadTarget`). An upload removes the record.
11. **Total play time is `game.tick`**, which `/time` reports (tested:
    "276 hours and 40 minutes" equals tick 59,760,005). While running it
    comes from plain `/time` over RCON (never Lua, so achievements stay),
    at most once a minute and at each finished save; otherwise from the
    record. The save's `connected` and `last_online` are never used
    (wrong for players online at save time).
12. **A start cancels** any save read of that world.
13. **Not audited.** A read changes no world file the game uses.

### #773 factory map (the full issue, Todd)

14. **What it draws.** One PNG per surface (planets, space platforms and
    modded surfaces) from the game's own chart
    (`force.get_chunk_chart`), plus an optional overlay of building
    outlines by category, from the same run. Games from 2.0.61; a 1.1 or
    older 2.0 world says "not supported". **SPEC 5.16, P1** (Todd raised it
    from P2; 5.13 to 5.15 are #774's).
15. **Categories** are fixed and come from the engine's prototype type, so
    modded entities land in one too: belts and inserters, production, power,
    fluids, rail, defence, logistic network and storage, other. One table in
    `internal/factorymap/categories.go`.
16. **Pictures are drawn on the server.** The base picture and one
    transparent PNG per category, on the same pixel grid, at most 4096
    pixels a side; a larger charted area is scaled down and the page says
    how many tiles a pixel is. Kept in `factorymaps/<world-id>/` under the
    data directory, never in a world or snapshot; one drawing per world;
    removed when the world no longer exists.
17. **The viewer** is its own page, `/worlds/{id}/map`, a world tab after
    Snapshots. It works like the Generate page's large view (SPEC 5.6):
    zoom by wheel, keys, buttons or pinch in steps of 1.15, pan by drag,
    keys or a tap, a surface list, and a checkbox per category that stacks
    its layer. Downloads: the base PNG, or the base with the ticked layers
    combined on the server. The zoom code is written new in
    `static/factorymap.js`, not shared with `preview.js`; FD files an issue
    to share them once both are stable.
18. **Modes**, per world in `world.json` as `factory_map: {"mode":
    "automatic" | "manual" | "off", "skip_online": true}`, set on the map
    page. The default is manual. Not audited, no snapshot (Foreman's own
    setting, like the display name).
19. **When automatic fires** (fitted to D2): when the world has been stopped
    for 2 minutes, no game is running on this Foreman, the world is not
    archived, and the save that loads differs from the last picture's.
    A 1-minute loop in the controller checks; it also covers a Foreman
    restart. A start cancels the run.
20. **Skip while players are online.** Under D2 an automatic run never
    happens beside a running game, so nobody can be online during one. The
    option therefore governs the click to draw a running world's newest
    save: when on (the default), that click is refused while players are
    online. Todd confirmed this meaning.
21. **Manual** is a "Draw factory map" button, beside a running game only on
    a click with the same memory note as rule 5. Off hides the button and
    stops automatic runs; the last picture stays until the save changes.
22. **Not in MCP or the REST API**, like map previews (21.7's "Not
    available" list gains factory map pictures and the players of a save).

### #774 world file export and import (from its design; Todd ruled D1, D2, D3, D7)

23. **D1:** no snapshots in a world file now; 5.15 records format 2 as P2.
24. **D2:** upload, preview, confirm.
25. **D3:** a choice on the export card, "All saves" (the default) or "Only
    the save that loads next" (using `LoadTarget`).
26. **D4:** a fixed 64 GiB cap plus the 6.11 disk check; no new setting.
27. **D5:** archive and export stay separate.
28. **D6:** audited as `world-export` and `world-import`.
29. **D7:** a running world exports the last save on disk, and the card
    says so.
30. **D8:** no MCP and no REST API.
31. **D9:** a world file only ever makes a new world.
32. The format is section 1.1 of its design; its writer uses the lowest
    format version that holds what it writes. ADR 0031 records the format.

### Small fixes

33. **#781:** `serverPage` reads the status again when a result is stored
    while the status still shows an operation (scope, #781).
34. **#784:** `gameRuns.snapshotsChanged()` in `live.go`, called after an
    inline take and every delete.
35. **#785:** "A mod stopped the game." with the seven translations in the
    scope; a second agent reviews them (SPEC 22.6).

## Contracts (C0 commits them as stubs)

All signatures, JSON shapes and element ids are in `docs/epic-fixes-16/design-769-773.md`
section 3 and `docs/epic-fixes-16/design-774.md` section 2.1; C0 copies them exactly.
The additions for the full #773:

```go
// internal/factorymap/types.go
type Category string // "belts", "production", "power", "fluids", "rail", "defence", "logistics", "other"
var Categories = []Category{...}               // fixed order, the page's order
type Surface struct { Index int; Name, Kind string; Width, Height, TilesPerPixel int }
type Drawing struct { Basis saveplayers.Basis; DrawnAt time.Time; GameVersion string; Surfaces []Surface; Failure string }
func CategoryOf(entityType string) Category      // F3
func DrawChart(manifest, chunks io.Reader, ...) (*image.NRGBA, Surface, error)            // F2
func DrawOverlay(entities io.Reader, s Surface) (map[Category]*image.Paletted, error)      // F3
// store.go, F4: Open(dataDir, id) (*Store, error); Replace(d Drawing, pngs map[string][]byte) error;
// Read() (Drawing, bool, error); OpenPNG(surface int, layer Category /* "" = base */) (*os.File, error);
// Composite(surface int, layers []Category) (image.Image, error); Remove(dataDir, id) error

// internal/world/factorymapmode.go, F6
type FactoryMapMode struct { Mode string; SkipOnline bool } // in Meta as FactoryMap
func (m *Manager) SetFactoryMapMode(id string, mode FactoryMapMode) error

// internal/game/factorymap.go, F5
type FactoryMapView struct { State, Failure string; Mode world.FactoryMapMode; Drawing factorymap.Drawing; Running, PlayersOnline bool }
func (c *Controller) FactoryMap(id string) (FactoryMapView, error)
func (c *Controller) DrawFactoryMap(ctx context.Context, id string) error // refuses per rules 20, 21
func (c *Controller) factoryMapLoop()                                    // rule 19
```

Helper mod output for the map (F1 writes, F2 to F4 read), in the work
folder: `factorymap/manifest.json` with `format` 1, `game_version`, `tick`
and `surfaces` (`index`, `name`, `kind`, `chunks`, chunk bounds, `chart`
and `entities` file names, `entities_total`, `entities_truncated`); one
`surface-<i>.jsonl` of `{"x","y","data"}` chart chunks; one
`surface-<i>-entities.jsonl` whose first line is the list of entity types
and each later line `[type_index, x1, y1, x2, y2]`. Limits (14.6): 512 MiB
in all, 300 surfaces, 65,536 charted chunks a surface, 2,000,000 entities
in all, each decoded chunk exactly 32 by 32 pixels.

C0 also adds, so no later task edits these shared files:
`internal/web/server.go` (the route calls for `savePlayersRoutes`,
`factoryMapRoutes`, `worldExportRoutes`, `worldImportRoutes`),
`internal/web/render.go` (partials `saveplayers` and `worldexportcard`),
`templates/world.html` (`{{template "world-players" .}}` and
`{{template "world-export" .}}`, with empty defines in the two partials),
`templates/layout.html` (the Factory map world tab, label key
`factorymap.tab`), `internal/game/controller.go` (fields and the
`go c.factoryMapLoop()` line), `internal/game/broadcast.go`
(`UpdateSavePlayers`, `UpdateFactoryMap`), `internal/game/actions.go`
(`c.cancelSaveCopies(id)` at the top of Start), `internal/fakegame/territories.go`
(the two kind cases), `internal/world/meta.go` (`factory_map` in
`knownKeys` and `Meta`), `internal/world/precheck.go` (`LoadTarget`
extracted), and the English key `factorymap.tab` in a new
`locales/en/factorymap.json`.

## Tasks

Lanes: **L** linux-dev (game images cached; real-game and Opus work),
**D** Todd-Desktop-2 (browser tests and UI), **P** pve1-dev (Go-only and
real-game work; too slow for browser tests).

### Wave 0 (start at once from `5b718314`)

| Task | Issue | Owner, model, effort | Files it alone owns | Needs | Lane |
|---|---|---|---|---|---|
| S1 | #781 | builder, Sonnet, high | `internal/web/game.go`, `serverpage_hub_test.go` | none | P |
| S2 | #784 | builder, Sonnet, high | `internal/web/live.go`, `snapshots.go`, `snapshots_all.go`, `game_test.go`, `e2e/tests/server-hub.spec.ts` | none | D |
| S3 | #785 | builder, Haiku, low; reviewer Sonnet | `locales/*/crash.json`, `translated/crash.json`, `render_test.go`, `serverpage_messages_test.go` | none | P |
| C0 | #769 #773 #774 | builder, Sonnet, high | every stub file in Contracts, and the shared lines listed there; the `internal/world/worldfile.go` contract and its classifier test (#774 T0) | none | L |
| D | all | builder, Opus, low | `docs/SPEC.md` (5.1, 5.5, 5.13 to 5.16, 6.1, 8.14 to 8.16, 12.9, 14.3, 14.5, 14.6, 21.7), `docs/adr/0030-reading-a-save-in-a-benchmark-run.md`, `docs/adr/0031-world-file-format.md`, `docs/OPERATOR.md` (moving a world; memory for reading a large save) | the rulings | L |

### Wave 1 (start from C0's verified head; S-tasks need not have landed)

#769:

| Task | Owner, model, effort | Files it alone owns | Needs | Lane |
|---|---|---|---|---|
| A1 copy run | builder, Opus, medium | `internal/game/savecopy.go`, `savecopy_test.go`; `readOnlyZipMounts` in `modsettings.go` | C0 | L |
| A2 players mod | builder, Sonnet, high | `internal/game/saveplayers/` | C0 | P |
| A3 record | builder, Sonnet, high | `internal/saveplayers/` | C0 | P |
| A4 reading | builder, Opus, low | `internal/game/saveplayers_seed.go`, `_test.go` | C0; green on A1, A3 | L |
| A5 tracking | builder, Opus, medium | `internal/game/saveplayers_track.go`, `_test.go`, edits in `internal/game/monitor.go` | C0; green on A3 | L |
| A6 `/time` | builder, Haiku, low | `internal/game/gametime.go`, `_test.go`, `/time` in `internal/fakegame/commands.go` | C0 | P |
| A7 world hooks | builder, Sonnet, high | `internal/world/modrun.go`, `saves.go` (`AddSave`), `clone.go` | C0 | P |
| A8 fake reads | builder, Sonnet, high | `internal/fakegame/saveplayers.go`, `_test.go` | C0 | P |
| A9 world page list | ui-designer, Sonnet, high | `internal/web/saveplayers.go`, `templates/saveplayers.html`, `locales/*/saveplayers.json`, `translated/saveplayers.json`; existing CSS classes only | C0 | D |
| A11 e2e | tester, Sonnet, high | `e2e/tests/save-players.spec.ts` | A8, A9, A10 green | D |
| A12 real game | tester, Sonnet, high | `internal/game/realgame3_saveplayers_game_test.go` | A1 to A4 green | L |

#773:

| Task | Owner, model, effort | Files it alone owns | Needs | Lane |
|---|---|---|---|---|
| F1 map mod | builder, Sonnet, high | `internal/game/factorymap/` | C0 | P |
| F2 chart | builder, Sonnet, high | `internal/factorymap/chart.go`, `_test.go` | C0 | P |
| F3 overlay | builder, Sonnet, high | `internal/factorymap/overlay.go`, `categories.go`, their tests | C0 | P |
| F4 store | builder, Sonnet, high | `internal/factorymap/store.go`, `manifest.go`, their tests | C0 | P |
| F5 map runs | builder, Opus, medium | `internal/game/factorymap.go`, `factorymap_auto.go`, their tests | C0; green on A1, F2 to F4 | L |
| F6 mode setting | builder, Haiku, low | `internal/world/factorymapmode.go`, `_test.go` | C0 | P |
| F7 map handlers | builder, Sonnet, high | `internal/web/factorymap.go`, `_test.go` | C0; green on F5 | D |
| F8 viewer | ui-designer, Opus, low | `templates/factorymap.html`, `static/factorymap.js`, `static/factorymap.css` (linked from the page's own scripts block), `locales/*/factorymap.json` (C0 seeds the English tab key), `translated/factorymap.json` | C0; renders on F7's stubs | D |
| F9 fake map | builder, Sonnet, high | `internal/fakegame/factorymap.go`, `_test.go` | C0 | P |
| F10 e2e | tester, Sonnet, high | `e2e/tests/factory-map.spec.ts` | F7, F8, F9 green | D |
| F11 real game | tester, Sonnet, high | `internal/game/realgame3_factorymap_game_test.go` | F1, F5 green | L |
| F12 hostile output | tester, Opus, low | `internal/factorymap/hostile_test.go` (oversized files, lying counts, wrong chunk sizes, boxes outside the chart, named pipes, unknown fields) | C0 (test-first; red until F2 to F4) | P |

#774 (task names from its design; T9 is folded into D):

| Task | Owner, model, effort | Files it alone owns | Needs | Lane |
|---|---|---|---|---|
| T1 export core | builder, Sonnet, high | `internal/world/worldfile_export.go`, `_test.go` | C0 | P |
| T2 import core | builder, Opus, low | `internal/world/worldfile_import.go`, `_test.go`, `createOptions` in `internal/world/world.go` | C0 | L |
| T3 export web | builder, Sonnet, high | `internal/web/worldexport.go`, `_test.go`, `locales/*/worldexport.json`, `translated/worldexport.json`, `internal/web/auditlog.go`, `locales/*/audit.json`, `translated/audit.json` | C0; green on T1 | P |
| T4 import web | builder, Sonnet, high | `internal/web/worldimport.go`, `_test.go`, `templates/worldfile.html`, `locales/*/worldimport.json`, `translated/worldimport.json` | C0; green on T2 | P |
| T5 attack tests | tester, Opus, low | `internal/world/worldfile_security_test.go` | C0 (test-first; T2 does not merge before it) | P |
| T6 world file UI | ui-designer, Sonnet, high | `templates/worldexportcard.html`, the third form in `templates/import.html`, `static/foreman.css` (the batch's only editor), `locales/*/worldfile.json`, `translated/worldfile.json` | C0 | D |
| T7 e2e | tester, Sonnet, high | `e2e/tests/world-file.spec.ts` | T3, T4, T6 green | D |
| T8 real game | tester, Sonnet, high | `internal/web/realgame6_worldfile_game_test.go` | T1, T2 green | L |

### Wave 2 (after S1 and S2 land)

| Task | Owner, model, effort | Files it alone owns | Needs | Lane |
|---|---|---|---|---|
| A10 Server page play time | ui-designer, Sonnet, high | `internal/web/serverpage.go`, `templates/server.html`, `locales/*/server.json`, `translated/server.json`, and, after S1 and S2 land, `serverPage` in `game.go` and the facts region in `live.go` | S1, S2 merged; C0 stubs | D |

### Wave 3

| Task | Owner, model, effort | Files it alone owns | Needs | Lane |
|---|---|---|---|---|
| FD fold | builder, Sonnet, high | `docs/STATUS.md`, `docs/DESIGN.md` (sections for the players list, the factory map and world files, from the UI tasks' PR bodies), `docs/SPEC.md` where a task changed a stated rule, `docs/I18N.md` if it lists areas; deletes this plan; files the viewer-sharing issue (rule 17) | every task merged | L |

**38 tasks** (S1 to S3, C0, D, A1 to A12, F1 to F12, T1
to T8, FD). Twenty-five start the moment C0 lands; the six test tasks in wave 1 can be
written then too and finish when their inputs land.

## Shared files and who owns them

| File | Owner (only it edits) | Others get |
|---|---|---|
| `internal/web/server.go`, `render.go` | C0 | nothing |
| `templates/world.html` | C0 (two includes) | A9 and T6 own the partials |
| `templates/layout.html` | C0 (the map tab) | nothing |
| `static/foreman.css` | T6 | A9 uses existing classes; F8 ships `factorymap.css` |
| `internal/web/auditlog.go`, `audit.json` | T3 | #769 and #773 add no audit action (rules 13, 18) |
| `internal/world/world.go` (`createOptions`) | T2 | #769 needs no hook there (rule 10) |
| `internal/world/meta.go`, `precheck.go` | C0 | F6 and T1 use `factory_map` and `LoadTarget` |
| `internal/web/game.go`, `live.go` | S1, then S2, then A10 (chain) | nothing else |
| `internal/game/monitor.go` | A5 | nothing |
| `internal/game/actions.go`, `controller.go`, `broadcast.go` | C0 | A1 implements `cancelSaveCopies` in its own file |
| `internal/fakegame/territories.go` | C0 | A8, F9 own their files; `commands.go` is A6's |
| Locale files | one area per task (new areas `saveplayers`, `factorymap`, `worldexport`, `worldimport`, `worldfile`; `crash` S3; `server` A10; `audit` T3) | `make i18n-hash` must rewrite only the owner's `translated/<area>.json`; S3 checks that first, and if it does not, hash runs are chained through the orchestrator |
| `docs/SPEC.md`, ADRs, `OPERATOR.md` | D (then FD) | nothing |
| `docs/DESIGN.md`, `STATUS.md` | FD | UI tasks put proposed text in PR bodies |
| `e2e/tests/server-hub.spec.ts` | S2 | A11 writes its own spec |

## Waves and chains

1. Wave 0: S1, S2, S3, C0 and D together.
2. Wave 1 from C0's head: the 31 tasks above (25 build tasks and 6 test
   tasks). Tasks marked "green on"
   build against the stubs and finish on the named head.
3. Wave 2: A10 once S1 and S2 are merged (it then owns `game.go` and
   `live.go`).
4. Tests: A11, A12, F10, F11, T7, T8 as their inputs land. T5 and F12
   are written first and must pass before T2 and F2 to F4 merge.
5. Real-game check (below), then FD, then the epic PR with `Fixes #769`,
   `#773`, `#774`, `#781`, `#784`, `#785`.

**Longest chain:** C0, A1, F5, F7, F10, FD (six steps). The #769 chain
is one step shorter (C0, A1, A4, A12, FD), and the Server page's is S1,
S2, A10, A11, FD.

## Checks

- Each task: the orchestrator runs `make verify` (one at a time,
  `TEST_SHARDS=8`) and the e2e specs for every file it touched. T6 edits
  `foreman.css`, so its PR gets the full e2e suite.
- Reviews: code-reviewer on every task. security-reviewer on A1, A4, F1,
  F4, F5, T1, T2, T4 (one-off containers, paths, hostile ZIPs and game
  output, secrets). a11y-reviewer on A9, A10, F8 and T6, and on T4's
  preview page.
- **The original save never changes** (rule 2), pinned three ways: A1's
  unit test of the container spec (read-only mount on the link; no bind
  under `worlds/`; copies, not links, of `mod-list.json` and
  `mod-settings.dat`); A4's test that every world file's SHA-256, size,
  modified time to the nanosecond and inode are unchanged after a read,
  except `players.json`; A12 and F11 repeat that against the real game,
  and A12 adds a hostile run of the same container spec that tries to
  append to the save and must fail.
- **Real-game check (epic-run step 11).** This batch changes
  `internal/game` and `internal/world`, so the full `make test-game` runs
  (about an hour), on linux-dev, gated on load. Its new tests are A12 and
  F11 (2.0.77 and 2.1.24; 1.1.110 shows "not supported" for the map) and
  T8. Also by hand on linux-dev, against copies only: read and draw the
  three trial saves the feasibility test used (32 MB, 94 MB, 187 MB with 44
  mods), and record time and peak memory in the epic PR; and export,
  import and start a world on a second data folder until it reaches
  InGame with its save and mods.

## Left out on purpose

- MCP and REST access to the players of a save, the factory map and world
  files (rules 22, 30). Adding a field to `list_players` later is allowed
  within `/api/v1/`.
- A per-player session history: only totals and last seen are kept.
- A scheduler for reads: they start from a page view (rule 10); only the
  factory map has a loop, because automatic mode asks for one.
- Sharing the zoom code with `preview.js` (rule 17); an issue is filed.
- Map tiles for very large factories: pictures stop at 4096 pixels a side
  and scale down.
- From #774: snapshots in a world file, replacing a world from a file,
  resumable downloads, signing, carrying database state, scheduled
  exports (its design, section 5).
- Several worlds at once: nothing here assumes one. Records, pictures and
  reads are keyed by world id.

## Confirmed by Todd (2026-11-21)

1. **Rule 20:** "skip while players are online" governs the click to draw
   a running world's newest save; it is on by default.
2. **Rules 5 and 19:** automatic player reads and automatic map drawings
   run only when no game is running on this Foreman.
3. **Rule 18:** the default factory map mode is manual for every world,
   new or existing.
