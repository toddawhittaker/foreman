# Epic 9c: Public mods and autosave tidy

Plan, 2026-11-16. Epic branch: `epic/public-mods`. Base:
`9c0edb6a` (`origin/main`). Serves SPEC 9.16, 11.6 and 14.18, with the
edits to 5.11, 6.7, 12.1, 12.9, 13.1 and 14.1, and the VISION Auth
amendment. Closes #132 and #671. Follow WORKFLOW and the epic-run skill.

## Rulings (Todd, 2026-11-16)

1. The public page offers every enabled, non-built-in mod of the running
   world, one by one and as one zip, and marks the ones the portal cannot
   supply. Turned-off mods are left out. Folder mods are listed with no
   download.
2. No licence handling: no display, flag or exclude list.
3. Off by default; one admin setting turns it on.
4. The setting lives in a new key-value table, `app_settings`
   (migration `0009_app_settings.sql`), which Epic 11's settings reuse.
5. Autosave tidy: K is the newest autosave by time, skipping the file the
   last run loaded; if Foreman restarted since, the newest by time.
   Snapshot first.

## Tasks

Paths are under `internal/` unless they start with `docs/`, `e2e/` or
`store/`.

| Task | Owner, model | Files (nobody else edits them) | Issue | Order |
|---|---|---|---|---|
| T0 docs | orchestrator | docs/VISION.md, docs/SPEC.md, docs/I18N.md, this plan | #132 #671 | Plan PR, done first |
| T1 public mods backend | builder, Sonnet high | `store/migrations/0009_app_settings.sql`; `store/appsettings.go` + test; `world/modfiles.go` + test (`OpenModFile`); `web/publicmods.go` (off-means-404 gate, list with marks, kept portal answers, 4 download slots, both download routes); `web/server.go` (routes, slots field); `web/server_test.go`; `web/publicmods_test.go`; `web/publicmods_security_test.go`; docs/OPERATOR.md (new subsection in section 6) | #132 | Wave 1 |
| T2 public mods pages | ui-designer, Opus low | `web/publicmodspage.go` (page, `/public/static/`, `/public/language`); `web/publicmodsettings.go` (settings page, audit `public-mods-set`); `web/render.go`; `templates/publicmods.html`; `templates/publicmodssettings.html`; `templates/layout.html` (settings nav item); `static/foreman.css`; `i18n/locales/*/publicmods.json`; `i18n/translated/publicmods.json`; docs/DESIGN.md; `e2e/tests/public-mods.spec.ts` | #132 | Starts from T1's verified head |
| T3 autosave tidy | builder, Opus low | `world/autosaves.go` + test (mapping, preview counts); `gamesettings/autosaveslots.go`; `game/autosaves.go` (audit constant, call, remembering the loaded file); `game/launch.go`; `game/monitor.go` (keep the loaded file name after the run ends); `web/settings.go`; `templates/settingspreview.html`; `web/snapshotlabel.go`; `i18n/locales/*/worlds.json` and `settings.json` + their `translated/` files; `e2e/tests/autosave-tidy.spec.ts`; a new `game/realgame*_autosaves_game_test.go` | #671 | Wave 1, parallel with T1 |
| T4 fold | builder, Sonnet high | docs/STATUS.md, docs/SPEC.md, docs/adr/, this plan (deleted) | #132 #671 | Last |

No task edits a shared e2e helper; a task that needs one stops and
reports.

## Reviews

- T1: code-reviewer and security-reviewer (new unauthenticated routes,
  file paths).
- T2: code-reviewer, a11y-reviewer, and security-reviewer for the public
  language POST and static path.
- T3: code-reviewer and security-reviewer (renames and deletes in a
  world's folder).
- Integration review on the epic head after T3 and T2 land.

## Checks before the epic PR

- `make verify` and the full `make e2e` on the epic head.
- Real-game check (T3): lower a world's slots from 20 to 10 after a
  graceful restart, start it, confirm the snapshot, the renamed files, and
  that the game's next autosave overwrites the oldest kept save.
- Real-game check (T1/T2): download a running world's mods from the
  public page with the proxy rule in place, and join with them.
