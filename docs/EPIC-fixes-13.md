# Fixes batch 13

Branch `epic/fixes-13`, cut from main `d6fe6205` on 2026-11-19. Three
issues Todd filed while checking Epic 11a on the trial. The fold task (FD)
deletes this file.

## Tasks

| Task | Issue | Owner, model | Files it may touch |
|------|-------|--------------|--------------------|
| S1 | #726 settings tab order; "Settings" opens the first tab | builder, Sonnet high | `internal/web/templates/layout.html` (the `settings-tabs` block and the Settings link at line 39), any handler or test that names the Settings link's target, `e2e/tests/settings-tabs.spec.ts`, `schedule.spec.ts`, `click-budgets.spec.ts`, `live-ops-scaffold.spec.ts`, `server-live-ops.spec.ts` (tab-order and Settings-link assertions only) |
| A1 | #724 audit log as a filter bar plus a paginated table | ui-designer, Sonnet high | `internal/web/auditlog.go` and its tests, `internal/web/templates/auditlog.html`, the audit-log rules in `internal/web/static/foreman.css`, `internal/store` audit query (offset or cursor for Newer, only if needed), `e2e/tests/auditlog.spec.ts`, `long-words.spec.ts` and `i18n.spec.ts` (add the audit page at 320 px), locale files (new strings only) and `internal/i18n/translated` |
| N1 | #725 Pushover preset | builder, Opus low | `internal/notify/*`, `internal/web/notify.go` and its tests, `templates/notificationform.html`, `notifications.html`, `internal/web/mcpadmin_foreman.go` (webhook tools), `internal/store/webhooks.go` only if needed, `e2e/tests/notifications.spec.ts`, locale files (new strings only) and `internal/i18n/translated`, `docs/OPERATOR.md` notifications section |
| FD | fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md` 8.11 and 12.9 and the Settings-link line, `docs/DESIGN.md` (line 566 and 8.15), `docs/VISION.md` notifications line, an ADR only if a "why" needs one; deletes this plan |

No two tasks share a file except the locale files and `translated/`,
which A1 and N1 both add keys to. Whichever merges second merges the epic
branch in and regenerates the i18n hash.

## Waves

1. S1, A1 and N1 together, all from the plan's merge commit.
2. FD from the epic head after all three merge, carrying `Fixes #724`,
   `Fixes #725`, `Fixes #726`.

## Rulings in this plan

1. **N1 stores the two Pushover keys in the existing `url` column**, with
   no migration. That column already holds a secret (a Discord or ntfy
   address) and is never shown, logged or returned, so the keys inherit
   that handling. The page and MCP show the host `api.pushover.net` only.
   Rejected: two new columns, which need a migration and a second copy of
   the secret rules.
2. **Pushover priority** is 1 for a crash, a crash loop and low disk, 0
   otherwise, never 2 (as the issue proposes). No device field. No
   test-notification button (out of scope; it would serve every preset).
3. **A1 chooses the 320 px layout** (scroll inside the table, or a card
   per row) and records why in its PR body; the fold moves it into DESIGN.
4. **A1 also checks the Generate button's contrast in dark forced-colours
   mode** (STATUS records 1.11:1; SPEC 17.1) and fixes it with one
   stylesheet rule if it is still there (Todd's ruling, 2026-11-19). The
   Generate page's e2e spec is an allowed extra file for A1.
5. **S1: "Settings" opens General**, and is marked current on every
   settings tab. Direct links to a tab (for example the mods page's link
   to the factorio.com account tab) are unchanged.

## Checks

Each task: the orchestrator runs `make verify` and the e2e specs for every
touched file. S1 touches `layout.html` and A1 touches `foreman.css`, so
each runs the whole e2e suite. N1 gets a security review (secrets and an
outbound webhook); A1 and S1 get an accessibility review; all get a code
review. No real-game check: nothing touches server control, worlds,
snapshots, mods or versions.
