# Fixes batch 7 plan

Branch `epic/fixes-7`, cut from origin/main b0d0e2e. Closes #523, #496,
#487, #495, #499, #497 and #524. Serves SPEC 5.6 (large map view), 6.11
(space reserve), 7.1 (Docker unreachable), 7.12 (restart schedule), 17.1,
17.2, 17.10 and 22, with DESIGN 6, 8.8, 8.15 and 8.16.

## Rulings

Todd, 2026-11-12:

1. **The user menu loses its settings links.** The new Settings tab is
   the one home for factorio.com account, Restart schedule and
   Notifications. The menu keeps the theme choice, About and Sign out.
2. **Language picker.** A button in the header, between the main
   navigation and the user menu, shows a globe, the current language's
   flag and its short code (EN, DE). It opens a list of all eight
   languages, each with its flag and its own name. Flags are decorative
   (`alt=""`); screen readers hear the language name, and the button's
   accessible name says the current language in full. It works without
   script, as the menu does today.
3. **Flags.** English shows the United Kingdom, Spanish shows Spain.
   German, Russian, Simplified Chinese, French, Polish and Czech show
   Germany, Russia, China, France, Poland and Czechia.
4. **Flag images** are SVG files copied from the MIT-licensed flag-icons
   project at an exact version (4x3 set), stored under
   `internal/web/static/flags/` with their checksums, and listed in
   STACK.md beside htmx. Nothing is fetched at build or start.

Orchestrator, from the issues and the spec:

5. **Settings tab** sits last in the main navigation, after Snapshots, and
   opens Restart schedule (#524). It is marked current on all three
   settings pages and their sub-pages.
6. **The world's "Settings" tab becomes "Game settings"** in all eight
   languages; the term joins the I18N.md glossary.
7. **About** shows the product name and logo, Foreman's version and
   commit, the Go version, the licence name (MIT) and a link to
   https://github.com/toddawhittaker/foreman. It leaves out the list of
   game versions. The version and commit are set at build time by the
   Makefile and Dockerfile (`-ldflags -X`, from `git describe`), fall back
   to Go's build info, and read "development build" when neither has
   them. The Go version comes from `runtime.Version()`. Without script,
   the menu item is a link to an `/about` page with the same content.
8. **#495** is diagnosed first: does the "cannot read the game container"
   reason stay while Docker keeps failing, or does the next check replace
   it with "cannot reach Docker"? If it is replaced, the test waits for
   the reason SPEC 7.1 names rather than only the state. If it stays, the
   monitor is fixed so a failing daemon always gives the SPEC 7.1 reason.
9. **#496 and #487** are the same flake. The test fakes the free-space
   reading through a test-only hook in the world package; production
   reads the disk as before.
10. **#523**: a never-saved schedule preselects the browser's time zone
    when it is in the list; a saved one keeps its zone; without script the
    page still offers UTC.
11. **Single owner for shared files.** Task 4 may edit `foreman.css` only
    in its map-preview rules and task 5 only in its header, navigation
    and menu rules. The SPEC 5.6 and DESIGN 8.8 text for #499 is written
    by the fold from task 4's pull request body, because task 5 owns the
    design and spec files in wave 1.

## Invariants every task tests

- Every page fits at 320 px with 200% text in German and Russian, with
  nothing pushed off the header bar (SPEC 17.2).
- Every new control is reachable by keyboard, shows focus, and has a 44 px
  target on a phone.
- Click budgets from the Server page: changing the restart schedule takes
  at most 2 clicks and opening Notifications at most 2 (SPEC 17.10).
- Every new string exists in all eight languages (I18N.md).
- No page shows two navigation links with the same name.

## Tasks

| # | Issues | Owner, model | Files | Done when |
|---|---|---|---|---|
| 1 | #496, #487 | builder, Sonnet high | `internal/world/fsutil.go` (test hook only), `internal/world/*_test.go`, `internal/web/upload_limits_test.go` | The test passes while free space changes underneath it; no production behaviour changes. |
| 2 | #495 | builder, Opus low | `internal/game/monitor.go`, `internal/game/*_test.go` | Diagnosis in the PR body; ruling 8 applied; the test passes 50 runs in a row under `-race -count=50`. |
| 3 | #523 | builder, Sonnet high | `internal/web/templates/schedule.html`, new `internal/web/static/schedule.js`, `internal/web/schedule.go` if a flag for "never saved" is needed, its unit test, an e2e spec for the schedule page | e2e with Playwright `timezoneId` shows the preselection; a saved zone is kept; no locale changes. |
| 4 | #499 | ui-designer, Sonnet high | `internal/web/templates/generatepreview.html`, `generate.html` if the large view lives there, `internal/web/static/preview.js`, `generate.js`, `foreman.css` (map-preview rules only), `e2e/tests/territories.spec.ts` | The issue's e2e cases pass; axe clean; proposed SPEC 5.6 and DESIGN 8.8 text in the PR body. No locale changes. |
| 5 | #524, #497 (picker) | ui-designer, Opus medium | `internal/web/templates/layout.html`, `internal/web/static/menu.js`, `foreman.css` (header, navigation, menu rules), new `internal/web/static/flags/`, the eight locale files (`common.*`, world tab name), `internal/web/` handlers that set the current tab, `docs/I18N.md` glossary, `docs/DESIGN.md` 6, 8.15, 8.16, `docs/SPEC.md` 17.1, 17.10, `docs/STACK.md` vendored files, e2e specs that name the Settings tab or the user menu, a new header spec | Rulings 1 to 6 built; 320 px checks in de and ru pass; click-budget spec updated. |
| 6 | #497 (About) | builder, Sonnet high | new `internal/version/` package and test, new `internal/web/about.go`, new `templates/about.html`, `layout.html` (menu item and dialog), a small dialog script if needed, `server.go` route, locale files (`about.*`), `Makefile`, `Dockerfile`, `docs/DESIGN.md` 6 user-menu text, a new e2e spec | Ruling 7 built; dialog opens and closes by keyboard and returns focus; `/about` works without script; unit test shows values come from build info. |
| 7 | fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md` 5.6, `docs/DESIGN.md` 8.8, this plan (deleted) | STATUS section written from the task PR bodies; `Fixes` line for every issue. |

## Waves

1. Tasks 1 to 5 in parallel, each from b0d0e2e.
2. Task 6 from task 5's verified head (both edit `layout.html`, the locale
   files and DESIGN 6).
3. Task 7 after every task has landed and its reviews are closed.

Reviews: code-reviewer on every task; a11y-reviewer on tasks 3 to 6;
security-reviewer on task 6 (new route that shows build details). Then
one integration review of the header and navigation across tasks 5 and 6.

## Real-game checks

None. No task touches server control, world switching, snapshots, mods or
game versions.
