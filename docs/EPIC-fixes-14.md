# Fixes batch 14

Branch `epic/fixes-14`, cut from main `e73b8a26` on 2026-11-20. Four
issues left by the whole-app UI climb (#741). The fold task (FD) deletes
this file.

The overriding rule: the UI was redesigned and hand-tested on 2026-11-20.
No task changes how the app looks or behaves except where its issue says
so, and each proves it (below).

## Tasks

| Task | Issue | Owner, model | Files it may touch |
|------|-------|--------------|--------------------|
| L1 | #736 status strip live on every signed-in page | builder, Opus low | `internal/web/live.go`, `render.go`, `server.go` (routes), the page types' `liveURL` methods, `templates/layout.html` (the `<main>` stream attribute only), `templates/server.html` (only if the strip's update moves), `static/live.js`, their Go tests, a new `e2e/tests/status-strip-live.spec.ts`. Not `foreman.css`. |
| C1 | #740 part 1: fold the override blocks from "The shell" (about line 11883) to just before "The status strip" (about line 12563) into their original rules | builder, Opus medium | `internal/web/static/foreman.css` only |
| C2 | #740 part 2: fold the rest of the override blocks (from "The status strip" to the end) | builder, Opus medium | `internal/web/static/foreman.css` only |
| B1 | #737 breadcrumb as `nav` + `ol` with a faint link underline; #738 column heads, fact names and plate labels in 13 px sentence case, capitals only for the overline | ui-designer, Opus low | the templates that hold `.crumb` (13 files) or a shared crumb block in `layout.html`, `foreman.css` (the crumb and label rules), locale files and `internal/i18n/translated` (the breadcrumb's accessible name only), the e2e specs that assert the crumb markup or the label case |
| FD | fold | builder, Sonnet high | `docs/STATUS.md`, `docs/DESIGN.md` (6 Shell, the crumb, 2 Type labels, 11 CSS file layout), `docs/SPEC.md` 16.3 if L1 changes what it says; deletes this plan |

C1 and C2 run one after the other, never together: they share
`foreman.css`. B1 starts from the epic head after C2 merges. L1 shares no
file with C1 or C2 and runs beside them.

## Waves

1. L1 and C1 together, from the plan's merge commit.
2. C2 from the epic head after C1 merges.
3. B1 prototype from the epic head after C2 merges. Before its PR, Todd
   approves before-and-after screenshots (below).
4. FD from the epic head after B1 merges, carrying `Fixes #736`,
   `Fixes #737`, `Fixes #738`, `Fixes #740`.

## Rulings in this plan

1. **L1 reuses the existing stream**: the same server-sent state update
   and out-of-band swap of `#site-state` that the Server page uses, sent
   to every signed-in page. Pages that already stream (Server, Console,
   Chat) keep theirs and are not connected twice. No visual change: the
   "live updates stopped" note may appear on any page that now streams,
   which is the existing behaviour of a live page (SPEC 16.3).
2. **C1 and C2 are pure refactors.** Each override is folded into the
   earliest rule for the same selector and condition, and the override is
   deleted. Where a fold would change which rule wins (specificity, a
   media query, or a later rule in between), the task keeps the cascade
   result the same and says how in its report. A rule it cannot fold
   safely stays where it is, listed in the report.
3. **Proof of no change** for L1, C1 and C2: the orchestrator captures all
   28 pages in light and dark at 1920 and 390 px with the climb's capture
   tool, before and after, and diffs each pair pixel by pixel. Every
   difference beyond the capture's own noise (two captures of the base)
   is explained and fixed before the PR merges. The full e2e suite also
   passes.
4. **B1 needs Todd's approval of screenshots before its PR**: four or five
   affected pages, desktop dark and light, before and after, side by
   side. Todd decides whether the breadcrumb also goes on the Settings and
   Server pages, and approves the sentence-case change.

## Checks

Each task: the orchestrator runs `make verify` and the full e2e suite
(every task touches a file every page loads). L1 gets a code review and a
security review (a new stream to every signed-in page). C1 and C2 get a
code review. B1 gets code and accessibility reviews. No real-game check:
nothing touches server control, worlds, snapshots, mods or versions.
