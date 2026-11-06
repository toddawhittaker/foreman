# UI batch plan

Branch `epic/ui-batch`, cut from main 9f0a100 on 2026-11-06. This plan lives
only on the batch branch; the fold moves its lasting rules into SPEC.md and
deletes it (WORKFLOW.md, "Epic plans").

Issues in scope: #220, #221, #232, #233, #234, #235, #236, #237, #238, #239,
#240, #241, #242, #244, and auto-pause on by default (Todd, 2026-11-06).
Left out: #207 (the game reads `map-settings.json` only when it creates the
map, so editing it afterwards changes nothing; the fold says so in 11.5),
#93, #92, #131, #132, #206.

## Rulings (Todd, 2026-11-06)

1. **Map settings at Generate (#238).** Generate offers the Enemy and
   Advanced settings that live in `map-settings.json`. The file is built from
   the example file in the world's own game image, plus the form's changes,
   and is always written complete, because the game refuses a file missing
   any key its exact version knows. An unchanged preset passes `--preset` and
   writes no file. Any change writes both files in full and does not pass
   `--preset`, because a preset silently wins over the files. Versions with
   no tested layout (2.1.13 to 2.1.17) show Enemy and Advanced read-only.
2. **Layout (#238).** One form: pick a preset, it fills the tabs, "[modified]"
   shows once anything changes, with a reset. Tabs Resources, Terrain, Enemy,
   Advanced, stacked when script is off. Sliders that snap to the game's own
   steps where the container is wide; selects of the same steps where it is
   narrow (phones). The preview sits under the settings when narrow, beside
   them and sticky when wide. A hidden `filled_from` field stops a page
   without script from writing the wrong preset's values.
3. **Achievement warnings (#239).** A Go table per game line in
   `internal/mapgen`, built from the game's own text (1.1: enemy base
   frequency or size below 100%, peaceful mode; 2.x adds no enemies, a bigger
   starting area, more trees, and pollution, evolution or expansion turned off
   or eased; 2.0 and 2.1 differ slightly). A short warning shows beside each
   setting as soon as it crosses the line, and one summary sits by the
   Generate button. The server works out the same answer.
4. **Preview size (#236).** 1280 by 1280 tiles. On 2.x the game draws one
   tile per pixel and has no closer scale, so zoom is the browser scaling the
   picture with sharp pixels.
5. **Preview flow (#233, #234).** Previews draw on page load and about 0.8 s
   after the last change; the newest request wins. Only the planet being
   viewed is drawn; others when picked. Previews still run one at a time, but
   a request waits up to about 20 s for its turn instead of being refused. The
   large view opens in the page with Previous and Next, so nothing is lost,
   and the seed shown is the seed used. Download one picture or all as a zip,
   named after the world, seed and planet (#237). Random seed button (#235)
   fills 0 to 4294967295 in the browser.
6. **No warning button (#241).** The countdown field on every stop, restart,
   switch, restore and update page gets a one-tap "No warning" button that
   sets it to 0. SPEC 7.6's rule that there is no separate checkbox stays.
   An e2e test checks that the Restart link after a settings save opens the
   same stop page as the Server page's Restart.
7. **Auto-pause.** A missing `auto_pause` key shows as on in the settings
   form (the game's own default). The first save on a world with no
   `server-settings.json` writes `auto_pause: true` with the three required
   keys. An explicit `false` already in a file is kept.
8. **Ideas from the two other managers** (design review of Factorio Stack
   Manager and FactorioHQ, 2026-11-06). Added to this batch:
   - The Server page heading is the active world's name with its state
     beside it; a "Join at host:port" line with a Copy address button; the
     page's links on one row; each world's Switch button beside its name.
   - The host comes from a new `FOREMAN_PUBLIC_ADDRESS` (SPEC 3.2); the
     line is hidden when it is blank. Foreman never guesses the address.
   - The game log colours the game's own Error and Warning lines and has a
     Download log link served by the server.
   - The world page gets a menu of links (Overview, Saves, Snapshots,
     Settings) marking the current page, in place of the "Back to" link and
     the buttons inside About; the empty "this is the active world" Switch
     panel goes.
   - The world list shows the active world's state, and the "0 B" that
     wraps alone at 320 px is fixed (DESIGN 8.3).
   - Number fields are about 10 characters wide, not full width.
   - On Generate: the way to make the map is chosen first, as labelled
     radios with one-line descriptions; each slider shows its value as a
     number and a word; the preview says it shows unsaved settings; the
     seed button reads "Random seed".
9. Mine, not disputed: Make admin (#240) sits beside each connected player on
   the settings page and goes through the normal edit, preview and confirm;
   the one list of game lines (#220) lives in `internal/mapgen`; the
   "* Required" note shows on every form with a required field (#242).

Found while designing: the server's write timeout (30 s,
`internal/web/server.go`) is shorter than a preview's limit (60 s), so a
slow preview loses its reply. W1-D fixes it with a per-request deadline.

## Shared files, owner per wave

| Wave | `foreman.css` | `layout.html` | `jsstrings.go` | `DESIGN.md` | Locale files | e2e helpers |
|---|---|---|---|---|---|---|
| 1 | W1-E | W1-E | none | W1-E (6, 7.2) | W1-E: common, accounts, server, worlds. W1-F: settings. | W1-E |
| 2 | W2-A | none | W2-A | W2-A (8.6, 8.7) | W2-A: worlds, mapgen, js | W2-A |
| 3 | W3-A | none | W3-A | W3-A (8.8) | W3-A: worlds, js | W3-A |

A task that needs a shared file it does not own stops and reports.

## Wave 1, from 9f0a100, six tasks in parallel

| Task | Owner, model | Files | Issues | SPEC |
|---|---|---|---|---|
| W1-A: a coverage file per run | builder, Haiku | `Makefile` | #232 | STACK 13 |
| W1-B: one list of game lines | builder, Sonnet high | `internal/mapgen/mapgen.go`, `internal/gamesettings/schema.go`, `internal/mapexchange/decode.go`, `internal/world/builtin.go`, their tests | Refs #220 | 10.8, 5.6 |
| W1-C: map-settings and achievement data | builder, Opus low | `internal/mapgen/extract.go`, `internal/mapgen/data/**`, new `mapsettings.go` and `achievements.go` with tests, `presets.go`, `internal/mapexchange/layout.go` | Refs #238, #239 | 5.6 |
| W1-D: preview backend | builder, Opus low | `internal/game/preview.go`, `internal/fakegame/create.go` and `instance.go`, `internal/gameenv`, `internal/web/generate.go`, new `internal/web/genpreview.go`, new `templates/generatepreview.html`, their tests | Refs #233, #234, #236, #237 | 5.6, 14.3, 14.6 |
| W1-E: shell and hub, two PRs in order (E1 shell, E2 hub) | ui-designer, Opus low | `layout.html` (header, `countdown` partial), `foreman.css`, new `static/icons/logo-*.png`, the templates except Generate and settings (incl. `server.html`, `world.html`, `worlds.html`, `console.html`), `internal/web/serverpage.go`, `console.go`, `live.go` and the world page handlers for their view models and the log download route, `internal/config` (`FOREMAN_PUBLIC_ADDRESS`), README's variable table, the locale files above, new `e2e/tests/header-logo.spec.ts`, `required-fields.spec.ts` and `hub.spec.ts`, the e2e helpers, `click-budgets.spec.ts`, `DESIGN.md` 6, 7.2 and 8.1 to 8.5 | E1: #244, #241 (button), #242 except Generate and settings, number widths. E2: ruling 8's hub items | 3.2, 7.6, 8.6, 17, 22 |
| W1-F: settings | builder, Sonnet high | `internal/web/settings.go`, `settingsform.go`, `settingsraw.go`, `internal/web/game.go` (stop page return path), the settings templates, new `static/settingsraw.js`, settings locale file, `e2e/tests/settings.spec.ts`, their tests | #240, #241 (e2e and return path), #221 (raw editor), #242 on settings, auto-pause | 11.1, 11.3, 17.9 |

What each must do:

- **W1-A:** a `mktemp` profile removed by a `trap`; the total read from it;
  the floor check unchanged; a failed run still fails.
- **W1-C:** default map-settings per layout; each preset's map-gen and
  map-settings values; `mapgen.MapSettings(version, preset, overrides)`
  returning the complete file or refusing an untested layout;
  `mapgen.AchievementBlockers(line, mapGen, mapSettings)`.
- **W1-D:** the lock that waits; `planet=` on the preview route (without it,
  all planets, as today); one current preview per user keyed by a hash of
  the settings, with per-planet entries; download routes (one PNG, a zip);
  the per-request write deadline; size 1280; the second version check in
  `checkPreview` removed.
- **W1-E:** logo 32 to 48 CSS px with a 2x image, empty `alt`, link named
  "Foreman"; icon only at 320 px; the `required-note` partial and `.req`
  mark; the No warning button in the `countdown` partial; the e2e check
  fails when a field the server requires is not marked; a number-field
  width class W1-F also uses. E2, from E1's head: the hub items in ruling 8;
  click budgets from the Server page do not rise; the address never shows
  when `FOREMAN_PUBLIC_ADDRESS` is blank.
- **W1-F:** "Connected now" with Make admin; caret on the named line in the
  raw editor; auto-pause ruling 7; the #241 e2e test; Cancel on the stop page
  returns where the person came from; required marks on the settings pages
  with W1-E's partial and class.

## Wave 2, from the epic head after wave 1

| Task | Owner, model | Files | Issues | SPEC |
|---|---|---|---|---|
| W2-A: the game-like Generate form | ui-designer, Opus medium | `internal/web/generate.go`, `mapgenform.go`, `mapgenlabels.go`, `mapgenpreset.go`, new `mapsettingsform.go`, their tests, `generate.html`, `mapgenform.html`, `static/generate.js`, `foreman.css`, `jsstrings.go`, locale files worlds, mapgen and js, `e2e/tests/generate.spec.ts`, `click-budgets.spec.ts`, e2e helpers, `DESIGN.md` 8.6, 8.7 | #238, #239, #235, #220, #221 (tab stops, legends), #242 on Generate | 5.6, 11.5, 17 |

## Wave 3, from the epic head after wave 2

| Task | Owner, model | Files | Issues | SPEC |
|---|---|---|---|---|
| W3-A: the preview UI | ui-designer, Opus low | `templates/generatepreview.html`, `generate.html` (triggers and slot only), `static/generate.js` or new `static/preview.js`, `foreman.css`, `jsstrings.go`, locale files worlds and js, `internal/web/genpreview.go` (view model only), new `e2e/tests/generate-preview.spec.ts`, `DESIGN.md` 8.8 | #233, #234, #236, #237, #221 (picture link) | 5.6, 17 |

The e2e test covers requests while typing, a dropped request, and the large
view by keyboard alone at 320 px.

## Wave 4: checks and fold

- **Reviews**, read-only, in parallel on the epic head: code-reviewer
  (Opus); security-reviewer (Opus: the preview container and its waiting
  lock, the zip route, file names built from typed text, the token check,
  promote by RCON from settings); a11y-reviewer (tabs, sliders, the large
  view, required marks, logo, live warnings).
- **Second translation pass** (SPEC 22.6) on the locale files changed.
- **Fix round**, then each reviewer confirms its findings are closed.
- **Real-game check** (tester, Sonnet high) on 1.1.110, 2.0.77 and 2.1.24,
  with and without Space Age: previews at 1280 one planet at a time; a
  dropped preview leaves no container or folder behind; a modified death
  world reaches InGame with the preset's evolution kept; Enemy and Advanced
  values match what was chosen; auto-pause written on first save.
- **Fold:** SPEC 3.2 (`FOREMAN_PUBLIC_ADDRESS`), 7.1 and 8.6 (the Server
  page heading and join line, log colours and download), 5.6 (two ways to make the map, the tabs, `filled_from`,
  achievements, the preview rules), 7.6 (No warning button), 11.1 (Make
  admin, auto-pause), 11.3, 11.5 (#207 note), 14.3, 17 (a new 17.13 on
  required fields, the logo, a Generate row in 17.10 with a budget of 3);
  ADR 0010 on why Generate writes a complete map-settings file and drops
  `--preset` when changed; STATUS section; `Fixes #N` for every issue;
  delete this plan.
