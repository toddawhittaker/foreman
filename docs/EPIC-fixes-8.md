# Fixes batch 8

Branch `epic/fixes-8`, cut from main 442d5bc. Todd asked (2026-11-13) for
the batch to run through to a green pull request into main without a stop
for plan approval.

## Tasks

| Task | Owner, model | Issues | Files it may touch |
| --- | --- | --- | --- |
| 1. UI fixes | ui-designer, Opus medium | #531, #537, #536, #535, Server phone order | `internal/web/templates/` (layout, about, server, snapshots, allsnapshots, design), `internal/web/static/` (foreman.css, scripts, a new wordmark image), `internal/web/locales/`, their Go handlers and tests if needed, `e2e/tests/`, `docs/DESIGN.md` |
| 2. Flaky crash-loop test | builder, Opus low | #475 | `internal/game/` only |
| 3. Fold | builder, Sonnet | all | `docs/STATUS.md`, `docs/SPEC.md`, `docs/DESIGN.md` 7.4, this plan (deleted) |

Tasks 1 and 2 run in parallel; their files do not overlap. Task 3 starts
after both merge.

## Rulings

- **#531.** The world's sub-navigation tab keeps the visible word
  "Snapshots". Its accessible name becomes "Snapshots of this world" (or
  the same idea in each language, starting with the visible word, WCAG
  2.5.3), in all eight languages. Remove the exclusion in
  `e2e/tests/user-menu.spec.ts`.
- **#537.** The state lamp gets the state in words. The designer picks the
  form (a visible word where it fits, with an accessible name everywhere)
  and records it in DESIGN.md. No state may rely on shape alone.
- **#536.** The Snapshots selection bar moves before the list in the code,
  so it is one Shift+Tab from the first checkbox. It still floats at the
  window's foot on screen. The same on the all-snapshots page if it has
  the bar.
- **#535.** The wordmark replaces the small icon beside "About Foreman"
  in the dialog and on `/about`. The heading text stays, so the image is
  decorative (`alt=""`). Serve a right-sized WebP made from
  `docs/brand/foreman-wordmark.webp` (about twice the shown width).
- **Server phone order.** DESIGN.md (section 6, the Server page) already
  says the pages tiles come before the players on a phone. Today the CSS
  `order` does it, so the tab order does not match the screen. Put the
  pages card before the players card in the code and drop the `order`
  rule; on a wide screen the grid keeps players on the left.
- **DESIGN 7.4.** The text says the confirmation box has "a 4 px top
  edge"; the CSS draws the 6 px hazard stripe, with a 4 px top border
  only in forced colours. The fold makes the text match the CSS.
- The WORKFLOW.md auto-merge line already exists; nothing to do.
- Not in this batch: the Generate phone map disclosure (no issue yet;
  Todd to decide), and native-speaker checks of five translations.
