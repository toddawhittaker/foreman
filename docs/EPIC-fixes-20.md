# Fixes batch 20: UI consistency

Branch `epic/fixes-20`, cut on 2026-11-27 from main `6d242662` (the merge of #969). It covers 30 issues: #962 to #966 and #970 to #991. Most came from Todd's hand test of #969 and a three-machine UI sweep. The fold task (H) deletes this file.

## Rulings (Todd, 2026-11-27, unless marked "orchestrator")

1. **Nothing moves because of a change elsewhere on the page.** A control keeps its exact box when something else appears, hides, changes length or redraws. Content may grow only below the control that caused it. Every fix proves this with layout tests that check x **and** y (the existing `layout-shift.spec.ts` checks x only).
2. **One thing, one look, one place, one size.** A component is defined once and used everywhere, including on `/design`.
3. **CSS restructure first (#983, option 2 with the guard).** About a dozen files by component under `internal/web/static/css/`, joined by Go in a fixed order and served as one `foreman.css`. It uses `@layer tokens, base, components, pages`, and a Go guard test in `make verify` fails on conflicting duplicate rules and on raw values where a token exists. Task A changes no pixels; unifying the variants is for the later tasks.
4. **One page width (#978).** Every page's frame spans the full width. Fields fill their column; short fixed-format inputs (a number, a time) are sized to their content.
5. **Light theme is light (#979):** the top bar, the account menu and the Console and Chat logs follow the theme.
6. **Result messages are a toast overlay (#985).** They take no layout room. They're announced through the polite announcer (errors through `role=alert`). A success toast stays at least 6 s and pauses on hover and focus; an error stays until dismissed. Each has a close button reachable by keyboard, doesn't steal focus, sits in one fixed place and stacks. Field errors stay inline at their field. Without JavaScript, a fixed slot that keeps its room.
7. **Phone page buttons go below the tab row (#981, #984).** On desktop the title row has one fixed height.
8. **Centre empty states in their plate (#982).** One empty-state component.
9. **One working bar drawn in CSS (#976)**, used everywhere; busy buttons keep their ring.
10. **Table rows: even padding inside the hover highlight (#980),** using option (a): the end cells are padded and the table is pulled out by the same amount.
11. **The batch ends with a re-sweep (orchestrator):** the three sweep slices run again on the batch head, and nothing new may appear. The sweep worktrees on linux-dev, Todd-Desktop-2 and pve1-dev are kept until then.

## CSS files after task A (the owner of each in later tasks is in brackets)

Task A's split, in the order they're joined: `css/tokens.css` [F1] · `css/base.css` (element selectors) [F2] · `css/buttons.css` [F2] · `css/tables.css` [F2] · `css/plates.css` (plates, title band, gaps, facts, the Danger zone) [F2] · `css/forms.css` (fields, hints, Required, form footer) [F2] · `css/messages.css` (messages, notes, empty states, working bar) [F2] · `css/controls.css` (segmented, step bar, radios, checkboxes) [F2] · `css/shell.css` (top bar, page header, crumb, tab row, account menu, toast) [F1] · `css/pages/server.css` [P1] · `css/pages/settings.css` [P2] · `css/pages/worlds.css` (worlds, saves, snapshots, import, favorites) [P3] · `css/pages/generate.css` (generate, preview, large view, game and mod settings) [P4] · `css/pages/mods.css` (mods, plan, search) [P5] · `css/pages/design.css` [G]. `factorymap.css` stays a separate, unlayered file [P3].

Orchestrator rulings after task A:
- The game-settings rules that `settings.html` uses sit in `pages/generate.css`. P2's first step moves them to `pages/settings.css`, checked pixel for pixel; P4 doesn't touch them.
- The guard test `stylesheet_test.go` allowlists 10 known conflicts in `shorthandConflictsToFixInWave1`. Each owner fixes its own and deletes its own lines: the 5 `.field select` lines are F2's, the 2 `form.general-grid` lines P2's, the 3 `.plan-tree__deps` lines P5's. A stale entry fails the test.
- `/design` is G's: F1 and F2 don't edit `design.html`. Each lists in its report the `/design` specimens its new components need, and a short G2 task adds them after F1 and F2 land.
- Task checks are the repo-wide fast checks, `make test-pkgs` for the changed packages, and the touched e2e specs. The full `make verify` runs on the epic head before the epic PR.

## Tasks

| Task | Owner, model | Machine | Files it may touch | Closes |
| --- | --- | --- | --- | --- |
| **Wave 0** | | | | |
| A. CSS restructure, identical pixels, guard | builder, Opus, high | linux-dev | `internal/web/static/foreman.css` → `internal/web/static/css/**`, the Go that serves and joins it, `design.go`, `designtokens.go`, `designexport.go` and their tests, a new guard test, DESIGN 11 | #983 |
| B. Mod plan offers: version fit, no duplicates | builder, Opus, low | Todd-Desktop-2 | `internal/mods/**`, `internal/web/modplanrows.go`, `internal/web/mcpadmin_mods.go` and tests, `internal/fakeportal/**`, `e2e/tests/mods-plan.spec.ts`, locales for a new note | #970, #972 |
| C. Console reply race with the play-time ask | builder, Opus, medium | pve1-dev | the RCON and console code under `internal/game/` and `internal/web/console*.go` and their tests, `internal/fakegame/**` | #964 |
| D. Planets-read failure names the mod, cached | builder, Opus, low | Todd-Desktop-2 | the planets-read code in `internal/game/` and `internal/web/gen*.go` and tests, the warning in `newmap.html` (only that block), locales, `e2e/tests/modded-planets.spec.ts` | #965 |
| **Wave 1** (from the epic head after A) | | | | |
| F1. Shell components | builder, Opus, high | Todd-Desktop-2 | `css/tokens.css`, `css/shell.css`, `layout.html`, a new `pagehead.html` partial, a new toast partial and `static/toast.js`, `error.html`, `setup.html`, `signin.html`, `signedout.html`, `menu.js`, DESIGN 6 | #979, #963, #984 (partial and 404/gate pages), #985 (component) |
| F2. Shared components | builder, Opus, high | linux-dev | `css/base.css`, `buttons.css`, `forms.css`, `tables.css`, `plates.css`, `messages.css`, `controls.css`, new partials (working bar, empty state, form footer), DESIGN 7 | #976, #980, #982 (component), #987, #988, #989, #990 (components) |
| G. `/design` in step | builder, Sonnet, high | pve1-dev | `design.html`, `design.go` and its test, `e2e/tests/design.spec.ts` | (with #983 item 5) |
| **Wave 2** (pages; from the epic head after F1, F2 and G) | | | | |
| P1. Server area | builder, Opus, medium | linux-dev | `server.html`, `console.html`, `chat.html`, `stop.html`, `forcestop.html`, `switch.html`, `playeraction.html`, `saveplayers.html`, `css/pages/server.css`, `log.js`, `live.js`, their handlers and specs | #985, #986, #988, #990 and #991 (server parts) |
| P2. Settings area | builder, Opus, medium | pve1-dev | `settings.html`, `generalsettings.html`, `generalconfirm.html`, `schedule.html`, `notifications.html`, `notificationform.html`, `notificationdelete.html`, `publicmods*.html`, `apitoken*.html`, `users.html`, `userdelete.html`, `account.html`, `auditlog.html`, `about.html`, `css/pages/settings.css`, their handlers and specs | #962, #981 (settings), and the settings parts of #985, #987, #988, #990 |
| P3. Worlds and snapshots | builder, Opus, medium | Todd-Desktop-2 | `worlds.html`, `favorites.html`, `favoritedelete.html`, `deleted.html`, `delete.html`, `world.html`, `worldexportcard.html`, `worldfile.html`, `clone.html`, `saves.html`, `snapshots.html`, `allsnapshots.html`, `snapshotdelete.html`, `snapshotbulkdelete.html`, `restore.html`, `rollback.html`, `versions.html`, `update.html`, `import.html`, `dropzone.html`, `factorymap.html`, `factorymap.css`, `ticks.js`, `import.js`, `css/pages/worlds.css`, their handlers and specs | #991 (highlight), and the worlds parts of #985 to #990 |
| P4. Generate, preview and settings pages | builder, Opus, medium | Todd-Desktop-2 | `generate.html`, `genflowsteps.html`, `newmap.html`, `mapgenform.html`, `mapexchange.html`, `generatepreview.html`, `flowmodsettings.html`, `modsettings*.html`, `settingspreview.html`, `settingsraw.html`, `preview.js`, `generate.js`, `flowmodsettings.js`, `settingsdraft.js`, `settingsraw.js`, `subnav.js`, `css/pages/generate.css`, their handlers and specs | #973, #974, #975, #977, #966, and the generate parts of #984, #986, #987 |
| P5. Mods and the plan | builder, Opus, medium | linux-dev | `mods.html`, `modplan.html`, `modportal.html`, `modsearch.html`, `modsearch.js`, `css/pages/mods.css`, their handlers and specs | #971, and the mods parts of #986, #988, #989 |
| **Wave 3** | | | | |
| H. Fold | builder, Sonnet, high | linux-dev | `docs/STATUS.md`, `docs/SPEC.md`, `docs/DESIGN.md` (consistency only), an ADR for the CSS layout, this file (deleted) | all 30 issues by `Fixes #N` |

Waves 0 and 1 overlap only on the epic head: wave 1 starts when A has merged and passed step 7. B, C and D merge whenever they're ready. Wave 2 starts when F1, F2 and G have merged. P1 to P5 run in parallel on disjoint files; a handler or spec shared by two page tasks goes to the first one listed and the other reports what it needs.

## What each task proves

- **A:** full-page captures of every page in both themes at 1920 and 390, in the main states, are identical before and after (the climb's capture tool), and the report is attached to the PR. The guard test catches a planted conflict. `make verify` and the e2e specs pass with no test edits, apart from tests that read the stylesheet's layout.
- **F1 and F2:** each unified component on `/design`, and tests per component: one computed height per button role, the plate band on every plate title, one stacked-field gap, one footer, the working bar the same in Chromium, Firefox and WebKit, and toast accessibility (announced, dismissable, doesn't take focus, the no-JS fallback keeps its room).
- **P1 to P5:** layout-stability tests for every case listed in their issues. The boxes of the control used and its siblings stay identical (0 px), `scrollY` doesn't change unless the user scrolls, and sibling tabs and flow steps keep identical header boxes in x and y.
- **B, C, D:** a failing test first. B and D also get the real-game check for the mods and planets read (step 11, targeted tests only).

## Checks

- Orchestrator, per task: `make check-go fmt-check vet lint typecheck`, `make test-pkgs` for the changed packages, then the e2e specs for the touched files under the load gate. The full `make verify` runs once on the epic head.
- Reviews: code-reviewer on every task, a11y-reviewer on A, F1, F2 and P1 to P5, and security-reviewer on F1 (the toast prints server messages) and P2 (tokens and users).
- The full CI suite on each epic push. Then the re-sweep (ruling 11), the targeted real-game tests (B, D), a trial deploy for Todd's hand test, and the epic PR.
