# Fixes batch 15

Branch `epic/fixes-15`, cut on 2026-11-21 from `80650b80`, the head of
the audit follow-up branch `fix/audit-decisions`, with main `e70c651e`
(the docs pass) merged in, giving `55dd8156`. Todd chose this base so the
two batches do not conflict. Until the audit's pull request merges, the
epic pull request also shows the audit's commits, and it merges after
that one; main is then merged into the epic. Three
issues found while taking the documentation screenshots: #759, #760 and
the seven items of #761. The fold task (FD) deletes this file.

None of these is touched by the audit follow-up (settings confirm, mod
search length, failed-start cleanup, helper tidy-ups); checked against
its diff on 2026-11-21.

No task changes how the app looks or behaves except where its issue says
so.

## Tasks

| Task | Issue | Owner, model | Files it may touch |
|------|-------|--------------|--------------------|
| R1 | #760 the crash-loop warning names the mod from "The mod X (version) caused a non-recoverable error" | builder, Sonnet high | `internal/game/crashreasons/` (catalogue, code, tests, a new fixture log), the crash-reason message keys in `internal/i18n/locales/*/server.json` and `internal/i18n/translated` only if a new message is needed |
| S1 | #759 Last played no longer runs into Disk use at any width; #761 item 1 the Mods page's sticky Plan card stays above the "Match mods to the save" card | ui-designer, Opus low | `internal/web/static/foreman.css`, `templates/worlds.html`, the Mods templates that hold the two cards (layout markup only), `static/time.js` only if the age text needs it, new or changed e2e specs for these two layouts |
| M1 | #761 item 2 a shared dependency is listed once in the plan panel, and the review page names every mod that needs it; item 5 the Server page shows the snapshot a mod install just took, with no reload | builder, Opus low | `internal/web/modplanpanel.go`, `mods.go`, `modfiles.go`, `templates/modplan.html`, `templates/mods.html` (not the two cards S1 owns), `serverpage.go`, `templates/server.html`, their Go tests, the mods and server e2e specs |
| G1 | #761 item 3 Default preset with nothing changed shows no "[modified]" and no achievements warning; item 6 Ribbon world with Space Age off generates on the first try; item 7 an older preview reply never replaces a newer one | builder, Opus medium | `internal/web/generate.go`, `genpreview.go`, `mapsettingsform.go`, `mapgenform.go`, `mapgenlabels.go`, `templates/generate.html`, `generatepreview.html`, `mapgenform.html`, `static/generate.js`, `static/preview.js`, the map generation code under `internal/` that item 6 leads to, their tests, the generate e2e specs |
| U1 | #761 item 4 the world page's Update link selects the target version in the dropdown as well as the typed box | builder, Sonnet high | `internal/web/update.go`, `templates/update.html`, `templates/versions.html`, their Go tests, the update e2e spec |
| FD | fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md` where a task changed a stated rule, `docs/screenshots/worlds.webp` (retaken after S1); deletes this plan |

No two tasks share a file: only S1 edits `foreman.css`, only R1 edits
locale files, only M1 edits `mods.go`, only G1 edits `generate.js`.

## Waves

1. R1, S1, M1, G1 and U1 together, from the plan's merge commit.
2. FD from the epic head after all five merge, carrying `Fixes #759`,
   `Fixes #760` and `Fixes #761`.

## Rulings in this plan

1. **#760 reuses the "a mod failed" message** when it can name the mod
   from the new line; a new message, in all eight languages
   (docs/I18N.md), only if the existing one cannot fit. The fixture is the
   real 2.1.24 log line.
2. **#761 item 2**: one row for a shared dependency, naming every mod
   that needs it, the same in the plan panel and on the review page.
3. **#761 item 6** is reproduced first against a real
   `factoriotools/factorio` 2.1.24 container the task creates. If it does
   not reproduce, G1 says so and the item becomes a STATUS gap.
4. **#761 item 7** is fixed from the mechanism without a reproduction: a
   preview reply older than the newest request is dropped, pinned by an
   e2e test that delays the first reply.
5. **#761 item 4**: if the target version is not in the dropdown's list,
   the dropdown stays on "Choose a version" and the typed box keeps it,
   as today.

## Checks

Each task: the orchestrator runs `make verify` and the e2e specs for every
file it touched; S1 touches `foreman.css`, so its PR gets the full suite.
Every task gets a code review; S1, M1, G1 and U1 also get an
accessibility review. Real-game check before the epic PR (it touches mods
and world generation): `make test-game`, plus a real start of a 2.1.24
Space Age world with even-distribution 2.1.0 (#760) and a Ribbon world
with Space Age off (#761 item 6).
