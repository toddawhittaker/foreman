# Fixes batch 5 plan

Branch `epic/fixes-5`, cut from main bf7aef0 (Resource amounts merged).
Todd chose this batch before Gate B on 2026-11-09. The epic pull request
into main is opened and left for Todd to merge. Gate B (SPEC 19.2) runs
after this batch.

## Scope

- #441: a "Show resource amounts" box on the Generate page's "One planet"
  tab, and amount labels drawn into downloads from counts already held
  (SPEC 5.6, 17.15).
- #424: on the Mods tab, "Change version" becomes one of the row's
  actions, shown only when the portal has another release for the world's
  game line (SPEC 9.3, 17; DESIGN 8.11).
- #419: the Server page says which save loads at the next start while an
  older save is chosen (SPEC 7.11, 17.7).
- Enter in a field of the large view's settings column no longer creates
  the world (SPEC 5.6; a gap left by the Resource amounts epic).
- Gaps from earlier batches, chosen by Todd on 2026-11-09 (docs/STATUS.md,
  fixes batches 3 and 4):
  - Mods tab rows: at 320 px a row with Update wraps Remove; a folder
    mod's Disable sits at the row's end; flagged rows are not sorted
    first; no click-budget test for "Change a mod's version" (SPEC 17.10);
    the search page's unused `.mod-results*` and `.mod-summary` CSS.
  - Saves: the newest row's button is named after its file though it
    loads "the newest save"; the Load next buttons do not line up.
  - Large view: the settings column's `h3` headings look like body text;
    the key help does not say that keys inside a setting work the setting;
    no test presses Tab through the dialog or Escape on an info tip inside
    its settings column.
  - Mod-change wording: the refusal and the Server page notice can show
    two "Update *mod*" buttons for one plan;
    `server.problem.modlist_space_age_on`/`_off` say "turned off/on";
    `settings.preview.undo` keeps the old "restore from the Snapshots
    page" wording.
  - Cleanup: `restoremods.go` re-reads mod lists the world package already
    parses; code comments and tests that cite issue numbers or "ruling N"
    (WORKFLOW.md, "Code style").
- Left out: the larger open issues (#353, #339, #207, #206, #132, #92), and
  Enemy and Advanced settings in the large view (a feature).

## Rulings

- R1. Todd: lithium brine stays an amount, not a yield, because the game
  marks it finite. SPEC 5.6 already says so; the fold removes the
  provisional gap from STATUS.
- R2. Todd: Enter in a text or decimal field inside `#large-settings` does
  nothing. It does not create the world. Enter on a button there still
  presses it. Enter in the main Generate form's fields outside the large
  view keeps today's behaviour.
- R3. #441: a download never starts a count. It draws labels only for a
  planet whose counts the preview store already holds, using the same
  label look as the page. The zip does the same per counted planet. The
  One planet box is off by default.
- R4. #424: "a different version exists" means the portal lists at least
  one release of that mod for the world's game line other than the
  installed one. Folder mods and mods the portal lacks show none. The row
  must stay tidy at 320 px and 200% text, keep the accessible name
  "Change version <mod>", and keep the focus rule in `live.js` when the
  status removes the action. SPEC 17.10's click count must not get worse.
- R5. #419: with no choice set, or the newest save chosen, the Server page
  is unchanged. Otherwise it shows a fact line naming the save, with a
  link to the Saves tab, whether the world is stopped or running.
- R6. Two "Update *mod*" buttons: when the pre-start refusal shows, it
  carries the fix, and the Server page notice drops its own Update button
  for the same plan (the notice's text stays).
- R7. Comment cleanup replaces an issue number with the SPEC section it
  serves, or drops it when the sentence stands without it. Behaviour does
  not change. It runs last, alone, so it may touch any file.
- R8. Machine load: every heavy run waits for the load gate (load under
  20, 12 GB free), uses at most 4 browser workers, and at most two full
  `make e2e` runs go at once.

## Tasks

Shared files and their one owner at a time: `foreman.css` belongs to T2,
then T5 (chained). T1 reuses the existing label styles and reports rather
than edits if it needs CSS. `preview.js` and `generatepreview.html`
belong to T1, then T5. `server.html` and `server.json` belong to T3, then
T6. `mods.go` and `mods.html` belong to T2, then T6. Each locale file has
one owner at a time: `worlds.json` and `js.json` (eight languages plus
`translated/`) belong to T1, then T5; `mods.json` to T2; `server.json` to
T3, then T6; `settings.json` to T6. T3 names the newest save's button
with the existing `worlds.saves.*` strings and reports, rather than edits
`worlds.json`, if it needs a new one. `generate.js` belongs to T4. T7 runs
alone after every other task has merged. There are no migrations.

| Task | Issues | Owner, model | Files it may touch | Order |
|---|---|---|---|---|
| T1 Amounts on One planet and in downloads | #441 | ui-designer, Opus medium | generatepreview.html, preview.js, genresources.go, genterritories.go, genpreview.go and their tests, locales/*/worlds.json, locales/*/js.json, translated/worlds.json, translated/js.json, e2e/tests/resources.spec.ts, territories.spec.ts, generate-preview.spec.ts | Wave 1 |
| T2 Mods tab rows | #424, Mods tab row gaps | ui-designer, Opus medium | mods.html, mods.go, modversion.go and the portal-status code that decides the action, their tests, live.js (the version focus rule only), foreman.css (mods and mod-search blocks only), locales/*/mods.json, translated/mods.json, e2e/tests/mods-version.spec.ts, mods-tab.spec.ts, click-budgets.spec.ts | Wave 1 |
| T3 Loads next and Saves buttons | #419, Saves gaps | ui-designer, Sonnet high | server.html, serverpage.go, saves.html, saves.go and their tests, locales/*/server.json, translated/server.json, e2e/tests/server-hub.spec.ts, saves.spec.ts | Wave 1 |
| T4 Enter in the large view's settings | R2 | builder, Sonnet high | generate.js, e2e/tests/large-view.spec.ts | Wave 1 |
| T5 Large view polish | large view gaps | ui-designer, Sonnet high | preview.js, generatepreview.html, foreman.css (large-view block only), the `worlds.generate.large.*` keys in locales/*/worlds.json and translated/worlds.json, e2e/tests/large-view.spec.ts | Wave 2, from T1's, T2's and T4's verified heads |
| T6 Mod-change wording and restore reads | R6, wording and `restoremods.go` gaps | builder, Sonnet high | server.html and serverpage.go (the notice's button only), the refusal template, restoremods.go and its tests, locales/*/server.json, locales/*/settings.json, translated/server.json, translated/settings.json, the e2e specs for the Server page notice and refusal | Wave 2, from T2's and T3's verified heads |
| T7 Comment cleanup | R7 | builder, Sonnet high | code comments and test names anywhere; no behaviour change | Wave 3, after T1 to T6 merge |
| Fold | all | builder, Sonnet high | docs/STATUS.md, SPEC.md (5.6, 7.11, 9.3, 17.7, 17.10, 17.15), DESIGN.md (8.11 and any page section touched), delete this plan | Last |

Each task's prompt names its exact file list; a task that must touch a file
outside it says so in its report.

## Checks

- Each task: the orchestrator runs `make verify` and the browser specs for
  every file the task touched before the squash merge. T2 touches
  `foreman.css` and `live.js`, so it runs the full `make e2e`.
- After each wave: `make verify` and the full `make e2e` on the epic head.
- Reviews per task, started as each task PR merges: code-reviewer always;
  security-reviewer on T1 (the download routes); a11y-reviewer on T1, T2,
  T3, T5 and T6.
- At the end: one integration review (code-reviewer across tasks and dead
  code, a11y-reviewer for flows that cross tasks).
- Real-game check: none needed. T2 reads portal data already fetched and
  changes no mod install path; T1 reads held counts only.
