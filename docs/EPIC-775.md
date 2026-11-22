# Epic 12: modded planets and guided Generate (#775)

Plan, 2026-11-22. Epic branch `epic/775-modded-planets`, cut from main
`1a7c4343`. Serves SPEC 5.6, 5.7, 5.12, 7.4, 9.6, 11.5, 14.3, 14.6, 17.10,
17.19, 18.12, 21.4, 21.7, 21.8 and 23.2, as this plan's own spec change
writes them, and ADR 0033. Follow docs/WORKFLOW.md and the `epic-run`
skill. Code comments and tests cite SPEC sections and ADRs, never this
plan, which F deletes.

Generate becomes a guided flow. Step 1 makes the empty world, step 2 is
its mods, and step 3 is the map settings and preview with every planet the
world's mods add. The world's own planet list, exported by the mod-settings
run, replaces the embedded one. Previews, territories and resource counts
load the world's mods. On 2.1 an opt-in "real-game picture" draws a planet
from a real game. The evidence is on #775 (feasibility runs, test 2 and
check set A1).

## Rulings

Todd ruled R1 to R7 on 2026-11-22. The orchestrator adopted R8 to R16
from the architect. None is reopened here.

- **R1** The opt-in real-game picture (2.1 only, `--test-save`) is in this epic.
- **R2** An abandoned flow leaves the world marked "Unfinished" (derived: `new_map` and no save) on the Worlds list and world page, with a Continue link; it is never deleted automatically.
- **R3** A map exchange string is typed in step 3, which is still shown; step 1 is version, Space Age and favorite only; a string from another game line is refused in step 3 with a message to change the world's version.
- **R4** A real-game picture is kept in memory in the preview store, keyed by map settings, seed, mods key, game version and planet, evicted only by the store's size limit and dropped when the map is created; a restart loses it.
- **R5** The existing mod-settings run also writes the planet list: one run, one memory cache under `ModSettingsKey`.
- **R6** Step 1 always lands on step 2 (Mods), which has a Skip to map settings; the 3-click budget stays.
- **R7** MCP adds `get_new_map` (read scope; refused once the world has a save) and `set_new_map` (scope and annotations of `generate_world`; refused with a save or busy; unknown planets and controls refused, nothing dropped silently); REST gets both through the MCP-REST parity.
- **R8** Without script each step is a plain page, the step bar is a list of links, and step 3 waits on the server for the planet run within its limit; with script the form part loads after the page.
- **R9** One line under the preview heading names what the fast preview cannot show; 2.1 worlds get "Draw from a real game (about 15 to 40 s)", captioned "From a real game, 1024 tiles", with "Back to the fast preview" and a toggletip; 2.0 and 1.1 get the fast preview only.
- **R10** The window stays on the origin; a picture about 99% one colour gets a note that a larger size may show more; offset panning is left out.
- **R11** Planet file limits: 1 MiB, 64 planets, 512 controls, 128 per planet, 512 resources, else refused; bad names left out with a count; names resolved, rich text removed, cut to 100 characters, `translate="no"`; on refusal or a failed run the built-in planets are used and the page says why.
- **R12** Step 3 always works from the current mods: gone planets and controls are listed as "no longer in the mods" and dropped at the next save, new ones get defaults; the pre-start check refuses an Unfinished world whose saved map settings name a control not in its planet list, with a link to step 3.
- **R13** Fast preview with a world's mods: 1 GiB and 60 s for each planet drawn; the preview store grows from 96 to 160 MiB; the real-game picture: 1 GiB, radius 16, 90 s.
- **R14** A control shared by two planets is shown under each planet as one value, with "Also changes *planet*".
- **R15** Saving step 3 takes no snapshot (the world has no save), and the spec says so.
- **R16** The scope includes 1.1 (no planets; modded resource controls on the one surface) and modded resources on built-in planets.

## Design (architect, 2026-11-22)

- **Planet list.** New `internal/mapgen/catalogue.go`: a `Catalogue` of
  planets, controls (category, richness, can be disabled, order, label)
  and resources. `Builtin(line, spaceAge)` wraps today's embedded data.
  `ParseWorldPlanets` checks a world's export under R11, modelled on
  `ParseDefinitions` (`internal/modsettings/definitions.go`).
- **Rich text.** New `internal/modsettings/richtext.go`, applied after
  `ResolveLocalised`.
- **Export.** `internal/game/modsettings/control.lua` also writes
  `planets.json`: on 2.x it walks `prototypes.space_location` for type
  `planet` and reads `prototypes.autoplace_control`; on 1.1 it reads
  `game.autoplace_control_prototypes`. `ModSettingDefs` carries the parsed
  catalogue, so one run and one cache serve both (R5).
- **Previews with mods.** `PreviewRequest` gains `World` (a world id, or
  empty for today's no-mods run) and `Planets` checked against a known
  list the caller passes. The work folder links the world's mods (as
  `makeModSettingsDir` does, through `LinkModsInto` and
  `readOnlyZipMounts`) and copies `mod-list.json` and `mod-settings.dat`.
- **Step 3 save.** New `internal/world/newmap.go`: `SetNewMap` writes the
  map files under ADR 0010 for a world with `new_map` and no save, refuses
  a world with a save or a busy one, and shares its file writing with
  `Generate` (`internal/world/generate.go`). No snapshot (R15).
- **Unfinished** is derived (`new_map` and no save), as `makesNewMap`
  does (`internal/web/modsettings.go`); `world.Summary` gains
  `Unfinished`. No new `world.json` field, no database migration.
- **Form split.** `GenerateForm` splits into the step-1 form and a
  `MapForm` built from a `Catalogue`. `mgPlanets` and every other caller
  of `mapgen.Planets` and `mapgen.Controls` in `internal/web` take the
  catalogue. The map exchange string moves with the map form.
- **Step 3 page** is `/worlds/{id}/new-map` (`internal/web/newmap.go`,
  `templates/newmap.html`). It reuses `mapgenform.html`,
  `generatepreview.html`, `generate.js` and `preview.js`; the form id stays
  `generate-form`. The preview endpoints keep `/generate/preview*` with a
  posted `world` field.
- **Real-game picture.** New `internal/game/chart.go` and
  `internal/game/chart/control.lua`, ported from
  `/home/todd/factorio-mod-planet-test-775/chart/mod-template/control.lua`
  on linux-dev: `--create`, then `--test-save` on that map, then
  `factorymap.DrawChart`.
- **Reused as they are:** `previewRunIn`, `controlMod`, `oneOffHostConfig`,
  `readOnlyZipMounts`, the mod-settings cache and its single shared run,
  `LinkModsInto`, `ModSettingsKey`, `ModsFingerprint`, `DefaultModList`,
  `ResolveLocalised`, `fillFromFiles`, `mapexchange.Decode`,
  `factorymap.DrawChart`, `resource.HiddenLabels`, and the fake game's
  `modsettings.go` and `resources.go` patterns.

## Tasks

Lanes: **L** linux-dev (game images cached; real-game and Opus work), **D**
Todd-Desktop-2 (browser tests and UI), **P** pve1-dev (Go-only work; too
slow for browser tests). "Files" means the files a task alone owns, plus
their `_test.go` files. A task that adds strings adds them in all eight
languages (docs/I18N.md, section 7) and runs `make i18n-hash` for its own
areas only. Every task stops and reports any choice its prompt leaves
open. Agents run only checks under 3 minutes, in the foreground; the
orchestrator runs `make verify` and the e2e specs a task touched.

| Task | Wave | Owner, model, effort | Files | Needs | Refs | Real game | Lane |
|---|---|---|---|---|---|---|---|
| S0 plan | 0 | orchestrator | `docs/EPIC-775.md`, `docs/SPEC.md`, `docs/adr/0033-preview-runs-load-world-mods.md`, `docs/adr/README.md` | none | #775 | no | L |
| B1 catalogue | 1 | builder, Opus, low | new `internal/mapgen/catalogue.go`, new `internal/modsettings/richtext.go` | S0 | #775 | no | P |
| B2 step 3 save | 1 | builder, Sonnet, high | new `internal/world/newmap.go`; `internal/world/generate.go`; `internal/world/list.go` (`Summary.Unfinished`) | S0 | #775 | in R | P |
| B3 previews with mods | 1 | builder, Opus, medium | `internal/game/preview.go`, `territories.go`, `resources.go`, `controlmod.go` | S0 | #775 | in R | L |
| C1 planet export | 2 | builder, Opus, low | `internal/game/modsettings.go`, `internal/game/modsettings/control.lua`, `internal/gameenv/modsettings.go`, `internal/fakegame/modsettings.go` | B1 | #775 | in R | L |
| C2 form split | 2 | builder, Opus, medium | `internal/web/generate.go`, `mapgenform.go`, `mapgenpreset.go`, `mapgenlabels.go`, `favorites.go`, `genpreview.go`, `genpreviewkey.go`, `genresources.go`, `genterritories.go`, `mcpadmin_worlds.go`; new `internal/web/newmappath.go` | B1 | #775 | no | P |
| E1 chart run | 2 | builder, Opus, medium | new `internal/game/chart.go`, new `internal/game/chart/` (`chart.go`, `control.lua`), new `internal/gameenv/chart.go`, new `internal/fakegame/chart.go`; one `case` line in `internal/fakegame/territories.go` | B3 | #775 | E3 | L |
| C3 pre-start check | 3 | builder, Opus, low | new `internal/game/newmapcheck.go`; `internal/game/actions.go` (`precheck`, `precheckReadOnly`); `internal/world/precheck.go` (one `ProblemKind`); `internal/web/gametext.go`; `locales/*/server.json`, `translated/server.json`; new `e2e/tests/newmap-stale.spec.ts` | C1, C2 | #775 | in R | L |
| D2a step 3 page | 3 | ui-designer, Opus, low | new `internal/web/newmap.go`, new `templates/newmap.html`, new `templates/genflowsteps.html`, `static/generate.js`, `static/preview.js`, one route line in `internal/web/server.go`, new area `locales/*/newmap.json` and `translated/newmap.json`, the area list in `docs/I18N.md`, new `e2e/tests/newmap.ts` (helper), new `e2e/tests/newmap.spec.ts` | B2, C1, C2 | #775 | no | D |
| D2b preview endpoints | 3 | builder, Opus, low | `internal/web/genpreview.go`, `genpreviewkey.go`, `genresources.go`, `genterritories.go` | B3, C1, C2 | #775 | in R | L |
| M1 MCP tools | 3 | builder, Sonnet, high | new `internal/web/mcpnewmap.go`; one line in `addReadTools` (`internal/web/mcptools.go`); one line in `internal/web/mcpadmin.go`; the name lists in `mcp_test.go`; rows in `mcpadmin_security_test.go`; `api_parity_test.go` | B2, C1, C2 | #775 | in R | P |
| E3 chart real game | 3 | tester, Sonnet, high | new `internal/game/realgame4_chart_game_test.go` | E1 | #775 | yes | L |
| D3 step 2 | 4 | ui-designer, Sonnet, high | `internal/web/mods.go`, `templates/mods.html`, `templates/modplan.html`, `locales/*/mods.json`, `translated/mods.json`, new `e2e/tests/generate-mods-step.spec.ts` | D2a | #775 | no | D |
| D4 labels and notes | 4 | ui-designer, Opus, low | `internal/web/mapgenform.go`, `mapgenlabels.go`, `genpreview.go` (R10 note only), `templates/mapgenform.html`, `templates/generatepreview.html`, `locales/*/mapgen.json`, `translated/mapgen.json`, new `e2e/tests/modded-planets.spec.ts` | D2a, D2b | #775 | no | D |
| T1 specs to step 3 (preview) | 4 | tester, Sonnet, high | `e2e/tests/` `generate-preview`, `large-view`, `large-view-settings`, `download-all`, `resources`, `territories`, `all-resources`, `planet-rows`, `sliders`, `generate-leave` (`.spec.ts`) | D2a, D2b | #775 | no | D |
| T2 specs to step 3 (form) | 4 | tester, Sonnet, high | `e2e/tests/` `generate`, `ui-fixes`, `i18n`, `foldouts`, `tips`, `checkbox-fields`, `long-words`, `required-fields`, `sticky-bars`, `about` (`.spec.ts`) | D2a, D2b | #775 | no | D |
| D1 step 1 | 5 | ui-designer, Opus, low | `internal/web/generate.go`, `mcpadmin_worlds.go`, `favorites.go`, `worlds.go`, `templates/generate.html`, `templates/worlds.html`, `templates/world.html`, `locales/*/worlds.json`, `translated/worlds.json`, `e2e/tests/generate.spec.ts`, `favorites.spec.ts`, `worlds.spec.ts`, `click-budgets.spec.ts`, new `e2e/tests/generate-flow.spec.ts` | D2a, D3, T1, T2 | #775 | no | D |
| E2 real-game picture UI | 5 | ui-designer, Opus, low | new `internal/web/genchart.go`; `internal/web/genpreview.go` (route line, store); `templates/generatepreview.html`; `static/preview.js`; `locales/*/mapgen.json`, `translated/mapgen.json`, `locales/*/js.json`, `translated/js.json`; new `e2e/tests/real-game-picture.spec.ts` | D4, E1 | #775 | E3 | D |
| L translation review | 6 | builder, Sonnet, high | every key added or changed in this epic, in the locale files of `server`, `newmap`, `mods`, `mapgen`, `worlds`, `js` and their `translated/` files | D1, E2, C3 | #775 | no | L |
| R real-game pass | 6 | tester, Opus, low | new `internal/game/realgame4_planets_game_test.go`, new `internal/web/realgame7_newmap_game_test.go` | every build task | #775 | yes | L |
| F fold | 7 | builder, Sonnet, high | `docs/STATUS.md`, `docs/SPEC.md` (only where a task changed a stated rule), `docs/DESIGN.md` (the step bar and the two pictures, from the UI tasks' PR bodies), `docs/OPERATOR.md` (planet mods and memory); deletes this plan | L, R | #775 | no | L |

### Waves and chains

1. Wave 1: B1, B2, B3 in parallel from S0's merged head.
2. Wave 2: C1 and C2 from B1's head; E1 from B3's head.
3. Wave 3: C3, D2a, D2b, M1 from the head with B1 to C2 merged (D2b and
   M1 also need B2 and B3); E3 from E1's head.
4. Wave 4: D3, D4, T1, T2. T1 and T2 start once D2a and D2b are merged.
5. Wave 5: D1 once D2a, D3, T1 and T2 are merged; E2 once D4 and E1 are.
6. Wave 6: L and R. Wave 7: F, then the epic PR with `Fixes #775`.

Chains forced by shared files, each in a later wave than the one before:

- `internal/web/generate.go`, `favorites.go` and `mcpadmin_worlds.go`: C2, then D1.
- `internal/web/genpreview.go`: C2, then D2b, then D4, then E2.
- `genpreviewkey.go`, `genresources.go`, `genterritories.go`: C2, then D2b.
- `mapgenform.go`, `mapgenlabels.go`: C2, then D4.
- `static/preview.js`: D2a, then E2. `static/generate.js`: D2a only.
- `templates/generatepreview.html`: D4, then E2.
- `locales/*/mapgen.json`: D4, then E2, then L.
- `e2e/tests/generate.spec.ts`: T2 (map-form tests), then D1 (step-1 tests).
- `internal/game/preview.go`: B3 only. E1 calls B3's mod-linking helper
  and does not edit `preview.go`.
- `internal/game/modsettings.go`: C1 only. B3 and E1 call
  `readOnlyZipMounts` as it is.

**Longest chain:** S0, B1, C2, D2b, D4, E2, L, F (eight steps).

**Why T1 and T2 come before D1.** D1 takes the map form off `/generate`,
which breaks about 20 specs. T1 and T2 first move each spec onto step 3
through D2a's helper `e2e/tests/newmap.ts`. The helper makes a world on
`/generate`, reads the world id from the address it lands on (the world
page before D1, the Mods tab after it), and opens `/worlds/{id}/new-map`,
so it works on both sides of D1. The full e2e suite then stays green on
every push to the epic branch.

### What done looks like

- **B1.** `Builtin(line, spaceAge)` returns exactly what `mapgen.Planets`
  and `mapgen.Controls` return today, for every line and both Space Age
  values. `ParseWorldPlanets` refuses a file over any R11 limit, keeps
  unknown fields out, leaves out a planet or control whose name fails
  `^[a-z0-9_-]{1,64}$` and counts them, and records which planets share
  each control (R14). On 1.1 the catalogue has one surface and no
  planets. `StripRichText` removes `[entity=iron-ore]`-style tags, keeps
  the text, and cuts to 100 characters. A hostile-input test covers
  oversized files, too many planets, controls and resources, bad names,
  deep nesting and wrong types. No caller changes yet.
- **B2.** `SetNewMap(ctx, id, files)` writes `map-gen-settings.json` and
  `map-settings.json` complete (ADR 0010) or removes them for an
  unchanged preset, and updates `new_map` (preset, seed). It refuses a
  world with a save, a busy world, a world a newer Foreman wrote and an
  archived world, under the world lock, and takes no snapshot (R15).
  `Generate` and `SetNewMap` share one writer. `Summary.Unfinished` is
  true exactly when `new_map` is set and no save exists.
- **B3.** With `World` set, the preview, territory and resource runs
  link that world's mods into the work folder (hard links, copies as the
  fallback, the 14.3 file and byte caps), copy `mod-list.json` and
  `mod-settings.dat`, and never bind the world's folder. A world mod with
  the control mod's name refuses the run. `Planets` is checked against the
  known list the caller passes. The time limit is 60 s for each planet
  drawn (R13). Without `World`, runs are byte-for-byte today's (a test
  compares the container specs).
- **C1.** One mod-settings run writes both outputs. `ModSettingDefs`
  carries the parsed `Catalogue`, with names resolved through
  `ResolveLocalised` in the page language, then rich text stripped. A
  refused or missing `planets.json` gives the built-in catalogue plus the
  reason, never an error page. The fake game writes a canned
  `planets.json`, with one extra planet and one extra resource control
  when the world holds the fake mod `fake-planet`, so e2e tests can see a
  modded planet. C1 adds a `fake-planet` zip to the e2e test data and
  says in its PR how a spec installs it (upload or the fake portal).
- **C2.** A pure refactor: the Generate page looks and behaves as
  before. `MapForm` is built from a `Catalogue`. No `internal/web` file
  calls `mapgen.Planets` or `mapgen.Controls` any more, except through
  `Builtin`. The map exchange string is part of `MapForm`.
  `newmappath.go` holds `PathNewMap` and `newMapPath(id)` only, so C3 and
  D2a share it. All existing unit tests and e2e specs pass unchanged.
- **E1.** `Controller.Chart(ctx, req, planet)` runs a seventh kind of
  one-off container, `foreman-chart-<folder>` (14.3): the world's mods,
  `--create`, then `--test-save` with the helper mod, radius 16, 1 GiB,
  90 s in all, the preview slot. It refuses lines other than 2.1. Output
  is checked under the 14.6 limits and drawn with `factorymap.DrawChart`.
  The fake game draws a canned chart.
- **C3.** Before a start, restart or switch of an Unfinished world, the
  check reads `config/map-gen-settings.json` and refuses, with the new
  problem kind and no start anyway, when an autoplace control it names is
  not in the world's current catalogue. The fix is a link to step 3. A
  world with a save, or without the file, is not affected. The e2e spec
  installs `fake-planet`, saves step 3 with its control, removes the mod,
  and sees the refusal and the link.
- **D2a.** Step 3 at `/worlds/{id}/new-map`, for an Unfinished world
  only; any other world gets a page that says why. With script the page
  shows at once and the form part loads after it; without script the
  server waits for the planet run within its limit (R8). The form starts
  from the world's saved files, else the default preset. "Fill from a map
  exchange string" fills the form (R3); a string from another game line
  is refused with the R3 message. Planets and controls that are no longer
  in the mods are listed and dropped at the next save; new ones get
  defaults (R12). When the built-in planets are used, the page says why
  (R11). "Save map settings" calls `SetNewMap` and lands on a page whose
  next step is a control (17.9). The step bar partial
  (`genflowsteps.html`) is a list of three links with `aria-current="step"`
  on the current one. The leave dialog of 5.6 works on step 3.
- **D2b.** Every preview request carries `world`. The server takes the
  planet list from that world's catalogue, refuses a planet outside it,
  and keys previews, counts and territories by the world's mods key too.
  The store holds 160 MiB (R13). Modded resources are labelled with their
  resolved names and map colours.
- **M1.** `get_new_map` (every scope, read-only) returns the planets,
  their controls, what each supports, and current values; it is refused
  once the world has a save. `set_new_map` (admin, the annotations of
  `generate_world`) takes a preset, a seed, per-planet control values or
  an exchange string. It refuses a world with a save or a busy one, and an
  unknown planet or control with an error naming it. It writes through
  `SetNewMap`. MCP-REST parity tests cover both tools.
- **E3.** On 2.1.24 with the planet-mod set: Cerys's real-game picture
  shows the dry-ice disc (not the preview's blue-grey), within 90 s and
  1 GiB. On 2.0.77 the call is refused before any container starts.
- **D3.** For an Unfinished world, the Mods tab and the review show the
  step bar, "Skip to map settings" and, after an install, "Next: map
  settings". A favorite's mods fill the review as today (5.12).
- **D4.** The one line under the preview heading (R9), "Also changes
  *planet*" under a shared control (R14), only the settings each control
  supports, names marked `translate="no"`, and the one-colour note (R10).
  The e2e spec uses the fake `fake-planet` mod.
- **T1, T2.** Each listed spec reaches its form through the helper and
  passes; no assertion is weakened (CLAUDE.md, "Testing rules").
- **D1.** Step 1 holds name, version, Space Age, favorite and the image
  download, and always lands on step 2 (R6). "Unfinished" and Continue
  show on the Worlds list and the world page (R2). `generate_world` keeps
  its arguments and results (SPEC 23.7). The click-budget test for
  "Generate a world with the defaults" counts 3.
- **E2.** "Draw from a real game" on 2.1 worlds only, the caption, "Back
  to the fast preview", and a toggletip (R9). Pictures are kept by R4's
  inputs and dropped when the world's map is created. Busy, timeout and
  out of memory each have their own sentence, as the resource count does.
- **L.** A second agent reviews every new or changed translation against
  docs/I18N.md (SPEC 22.6) and fixes it in place.
- **R.** See "Real-game pass".
- **F.** STATUS.md gets the epic's section from the PR bodies; SPEC is
  corrected where a task changed a rule; this plan is deleted.

## Shared files and who owns them

| File | Owner, in order | Others get |
|---|---|---|
| `internal/web/server.go` | D2a (one route line) | E2 registers in `genpreview.go`'s `previewRoutes` |
| `internal/web/generate.go`, `favorites.go`, `mcpadmin_worlds.go` | C2, then D1 | nothing |
| `internal/web/genpreview.go` | C2, D2b, D4, E2 | nothing |
| `internal/web/mcptools.go`, `mcpadmin.go` | M1 (one line each) | nothing |
| `internal/game/actions.go` | C3 | nothing |
| `internal/world/precheck.go` | C3 (one `ProblemKind`) | nothing |
| `internal/fakegame/territories.go` | E1 (one `case` line) | nothing |
| `static/foreman.css` | nobody | UI tasks use existing classes; a new class needs the orchestrator's ruling first |
| `docs/I18N.md` | D2a (the `newmap` area) | nothing |
| `docs/SPEC.md`, ADRs | S0, then F | nothing |
| `docs/STATUS.md`, `DESIGN.md`, `OPERATOR.md` | F | tasks put proposed text in PR bodies |

## Checks

- Each task: `make verify` (one at a time, `TEST_SHARDS=8`) and the e2e
  specs for every file it touched, run by the orchestrator.
- Reviews: code-reviewer on every task. security-reviewer on B1, B3, C1,
  E1 and M1 (untrusted game output, work folders, mods linked into a
  container, tool arguments). a11y-reviewer on D1, D2a, D3, D4 and E2.
- **Real-game check (epic-run step 11).** The epic changes
  `internal/game` and `internal/world`, so the full `make test-game`
  runs on linux-dev, gated on load (under 20, 12 GiB free).

### Real-game pass (R)

On 1.1.110, 2.0.77 and 2.1.24. The planet-mod set is the five mods of the
feasibility test (maraxsis, Cerys-Moon-of-Fulgora, Moshine, planet-muluna,
corrundum, with their dependencies), read from a local folder named by
`FOREMAN_TEST_PLANET_MODS`; the test skips with that name when it is
unset. Nothing is downloaded.

1. The mod-settings run on a world with the set gives 11 planets, 46
   controls and 28 resources on 2.0.77 and 2.1.24. Without mods: 5
   planets and 28 controls; on 1.1.110, 8 controls and no planets.
2. A fast preview of all 11 planets at 1024 tiles finishes within 60 s
   per planet and 1 GiB.
3. A resource count on Moshine and on Nauvis with the set; territories on
   Vulcanus with the set.
4. Step 3 saves changed controls on a modded planet and on a shared
   control (calcite); the first start's `--create` exits 0, and the new
   save's map exchange string carries those values.
5. Remove one planet mod after step 3 is saved: the start is refused
   (C3). Save step 3 again: the start works.
6. `set_new_map` through MCP, then a start, gives the same save as the UI
   path.
7. On 1.1.110: a world's step 3 shows the one surface, and a start works.

## Open questions

These are not settled by R1 to R16. Each task that meets one stops and
reports; the orchestrator brings it to Todd.

1. **Continue's step.** R2 says Continue "resumes at the right step".
   Which step: step 3 always, or step 2 while the world has saved mod
   picks not yet installed?
2. **Step 2 without a favorite.** The plan reads step 2 as the world's
   Mods tab, and the review when a favorite has mods. Confirm.
3. **Pre-start check when the planet run fails.** Proposed: the check
   does not refuse, because `--create` then fails without writing a save,
   so nothing is lost.
4. **Audit entry for a step 3 save** (and `set_new_map`). Proposed: a new
   action `new-map`, naming the preset and "modified", like `generate`.
5. **Real-game picture container.** One container running `--create`
   then `--test-save`, or two in a row within the 90 s?
6. **Real-game picture output limits** (14.6). Proposed: one planet per
   run, at most 1,024 chunks of exactly 32 by 32 pixels, the file at most
   4 MiB.
7. **`get_new_map`'s scope.** R7 says "read scope", so the plan puts it in
   every scope, while `list_mod_settings` and `get_game_settings` are
   admin only. Confirm.

## Left out

- Map presets that mods add.
- Achievement warnings for modded enemy controls.
- Changing map settings after the first start (#207).
- Real-game pictures on 2.0 (no `--test-save`) and the tile-reading
  fallback.
- Radius 32 for the real-game picture (75 s and 1.5 GiB).
- Sharing the viewer code (#833).
- Offset panning of the preview window (R10).
- The 9.6 mod settings page's missing no-script path, filed as #838.
- A disk store for real-game pictures (R4).
- Several worlds at once: nothing here assumes one; caches and pictures
  are keyed by world id.
