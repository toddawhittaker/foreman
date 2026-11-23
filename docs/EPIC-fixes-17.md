# Fixes batch 17

Branch `epic/fixes-17`, cut on 2026-11-22 from main `cdbe9ba7`. All 17 open
issues: the test-drive findings from Epic 12 (#873 to #886) and three older
ones (#833, #838, #854). The fold task (FD) deletes this file.

No task changes how the app looks or behaves except where its issue says so.

## Rulings (Todd, 2026-11-22, unless marked "orchestrator")

1. **#880:** recommended ("+") dependencies stay ticked, as SPEC 9.14 says.
   The Install button names the count, for example "Install 3 mods and 10
   recommended", and updates when a box is unticked.
2. **#875:** the default mod size limit becomes 1,024 MB (the maximum stays
   2,048 MB). An over-limit download gets its own error naming the file's
   size and the limit, with a link to the setting for admins and "ask an
   admin" for others, and no Try again. Every failed mod download is logged
   with mod, version and reason, never the username or token.
3. **#874, #884 item 3:** measure the resource count with the heavy mod set at
   3072 tiles, then give runs that load a world's mods one fixed larger
   memory limit and time limit (measured peak times 1.5, worst time times 2;
   expected about 2 GiB and 120 s). Base-game runs keep 1 GiB and 60 s. One
   run at a time stays. An out-of-memory failure of the count says it was
   the resource count.
4. **#876:** a control gets the on/off box when the game says it
   `can_be_disabled` and its category is resource, or (on 2.x) cliff or
   enemy. By rule, never by planet name. Muluna's lunar rocks keep the mod's
   category (Terrain). This also covers #884 item 1 (enemy boxes).
5. **#884 item 2:** when a planet has resource controls and the count finds no
   patches, the page says "This planet's resources are not patches, so they
   can't be counted"; SPEC 5.6 records the limit. Detected in Go, no Lua
   change (orchestrator).
6. **#878:** a new Generate step 3, Mod settings, between Mods and Map
   settings, startup settings only. When no mod has startup settings the
   step is skipped and the step bar marks it as plain text "Mod settings
   (none)", not a link (orchestrator).
7. **#886:** the Server page panel becomes Players: online first (longest
   session first, orchestrator), then by play time, at most 10 rows, then
   "All N players" linking to the world's Overview list. Offline rows get no
   actions (orchestrator). `list_players` in MCP and REST stays online-now.
8. **#882:** wiki icons only, as agreed in the issue: an opt-in admin setting,
   images loaded by the browser from wiki.factorio.com with
   `referrerpolicy="no-referrer"`, names as the fallback. Mod icons from zips
   become a new issue (FD files it). HTML pages get
   `Content-Security-Policy: img-src 'self' data:`, plus
   `https://wiki.factorio.com` only while the setting is on (orchestrator).
9. **#854:** already met. Every spec runs in desktop-dark, desktop-light,
   phone-dark and phone-light (`e2e/playwright.config.ts`), so every
   `checkPage` already scans dark. FD comments the evidence; the epic PR
   closes it.
10. **#879:** origins are worked out from the installed mods' `info.json`
    dependencies, not stored. Shown on the mod settings page only (and so in
    #878's step), chosen mods' groups first (orchestrator).
11. **#877:** hex and `r,g,b` colours show text only. Object tags show the
    prototype name made readable ("Space science pack"), or the localised
    name where #881's lookup already finds it (orchestrator).
12. **#833:** one plain script `internal/web/static/viewer.js` exposing
    `window.ForemanViewer`, loaded before each page's script. No ES modules.
    The layer colours stay duplicated in Go and CSS (orchestrator).
13. **#885:** `padding-inline-end` on the closable message reserves the
    button's width; the button takes the quiet icon style if DESIGN 7.3 has
    one (orchestrator).

## Tasks

All start from `cdbe9ba7` unless "Starts from" says otherwise. Model: S =
Sonnet at high effort, O = Opus at low effort. Machine: L = linux-dev
(local), D = Todd-Desktop-2, P = pve1-dev.

| Task | Issues | Owner, model, machine | Files it owns (plus their tests) | Docs | Real-game check | Starts from |
|---|---|---|---|---|---|---|
| G1 | #883, then #881 | builder, S, P | `internal/game/modsettings.go`, new `internal/game/baselocale.go`, new test mods under `internal/game/testdata/` | SPEC 9.6 (label lookup order) | `go test -tags game ./internal/game/ -run 'ModSettingsNoSettings\|BaseLocale'` | main |
| P1 | #873 | ui-designer, S, D | `templates/generatepreview.html` (picture actions and zip link), `foreman.css` (`.picture-actions` block only) | DESIGN 8.8 if the layout rule changes | none | main |
| P2 | #874, #884 items 2 and 3 | builder, O, L | `internal/game/preview.go`, `controlmod.go`, `resources.go`, `internal/web/genresources.go`, `genpreview.go`, `locales/*/worlds.json` | SPEC 5.6 (limits, Vesta note, P3's cliff and enemy sentence) | measurement first, then a heavy-mods test at 3072 and `make test-game-resources` | main |
| P3 | #876, #884 item 1 | builder, O, P | `internal/web/mapgenform.go`, `internal/mapgen/cliffs.go`, `internal/mapgen/mapgen.go` (an enemy category if needed) | DESIGN "Cliffs off"; sends its SPEC 5.6 sentence to P2 | size 0 removes a test mod's cliffs and enemies on 2.1.24 | main |
| M1 | #875 | builder, S, D | `internal/modportal/download.go`, `internal/web/mods.go` (download error paths), `modfiles.go`, `gametext.go`, the result template that shows the error, `locales/*/mods.json` | SPEC 14.6, 14.10 | none | main |
| T1 | #877 | builder, O, L | `internal/modsettings/richtext.go`, the template function in `internal/web/render.go`, mod-text call sites in `mods.html`, `modplan.html`, `modportal.html`, `modsearch.html`, `publicmods*.html`, labels and descriptions in `modsettings.html`, `internal/mapgen/catalogue.go`, `foreman.css` (new `.rt-*` block), `locales/*/mods.json` | SPEC 14.6, DESIGN 8.11 | none | main |
| C1 | #838, #879 | builder, S, L | new `internal/mods/origins.go`, `internal/web/modsettings.go`, `modsettings.html` (group headings, no-script path), `locales/*/mods.json` | SPEC 9.6, DESIGN 8.11 | none | main |
| E1 | #880 | builder, S, D | `modplan.html` (Install button only), the button label in `internal/web/mods.go`, `locales/*/mods.json` | SPEC 9.14 | none | main |
| D1 | #878 | builder, O, L | `internal/web/newmap.go` (flow steps), the continue path in `mods.go`, a new step handler and template (reusing the `modsettings-form` define, not copying it), `get_new_map` in MCP and REST, `locales/*/newmap.json`, `e2e/tests/click-budgets.spec.ts` | SPEC 5.6, 7.4, 9.14, 21; DESIGN 8.6 | light: change a planet mod's startup setting at step 3; step 4's planets follow | main |
| S1 | #885 | ui-designer, S, D | `foreman.css` (closable message block only), new `e2e/tests/done-message.spec.ts` | DESIGN 7.3 | none | main |
| S2 | #886 | builder, O, D | `templates/server.html` (players panel), `internal/web/serverpage.go`, `saveplayers.go`, `live.go` (players part), `locales/*/server.json`, `foreman.css` (new block after the server players rules) | SPEC 8.1, 8.14, 8.15, 21.4; DESIGN Server page | none | main |
| V1 | #833 | builder, O, D | new `internal/web/static/viewer.js`, `factorymap.js`, `preview.js` (zoom and pan only), the script tags in `factorymap.html` and `newmap.html` | none | none | main |
| W1 | #882 (wiki part) | builder, S, L | `internal/web/appsettings.go` (setting, audit, MCP and REST parity), the image branch in `richtext.go`, the CSP header in `render.go`, `locales/*/settings.json` | SPEC 13.1, 14 (trust boundaries, outbound request), OPERATOR.md | none | T1's verified head |
| FD | fold | builder, S, L | `docs/STATUS.md`, SPEC and DESIGN leftovers, this file (deleted), issue for mod icons, #854 comment | as needed | none | last merge |

### Shared files

- **`mods.json`** (M1, T1, C1, E1) and **`mods.go`** (M1, E1, D1): each task
  adds its own keys and edits only its own functions. Landed one at a time
  with `git apply --3way`; the orchestrator resolves any conflict.
- **`modsettings.html`:** T1 edits labels and descriptions, C1 the group
  headings and the no-script path, D1 nothing (it reuses the define as is).
- **`modplan.html`:** T1 the mod text, E1 the Install button.
- **`foreman.css`:** P1, T1, S1 and S2 each edit only their named block and
  add new rules next to their component, never reorder.
- **`richtext.go`, `render.go`:** T1 first; W1 starts from T1's head.
- **`internal/i18n/translated/` hashes:** the orchestrator reruns the hash
  target once after merging if needed.

Every new user-visible string goes into all eight languages (docs/I18N.md).

## Order

- **Wave 1, at once:** G1, P1, P2, P3, M1, T1, C1, E1, D1, S1, S2, V1.
- **Wave 2:** W1 as soon as T1's head passes verify.
- **Land order:** G1 first (#883 is high severity), T1 before C1 and E1, M1
  before E1 and D1; others as they pass.
- **After the last task:** integration code review and accessibility review
  on the epic head, FD, then the epic PR into main.

## What done looks like

- Each task: its issue's tests (unit, and e2e for anything visible, with
  `checkPage` in the new states), `make verify` passed by the orchestrator,
  the e2e specs for its touched pages passed, and its real-game check where
  listed.
- Reviews per task as it lands: code-reviewer always; security-reviewer for
  M1 (downloads, logs) and W1 (outbound request, CSP); a11y-reviewer for
  every UI task (P1, T1, C1, E1, D1, S1, S2, W1).
- The epic PR into main is green on CI's full run, with `Fixes` lines for all
  17 issues (#854 closed as already met).

## Left out

- Mod icons from zips (#882's second half): a new issue.
- Moving resource-like terrain controls to Resources (ruling 4).
- Starting recommended mods unticked (ruling 1).
- Sharing the layer colours between Go and CSS (ruling 12).
- Counting tile resources such as Vesta's (ruling 5).
