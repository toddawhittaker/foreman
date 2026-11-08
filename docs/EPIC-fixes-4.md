# Fixes batch 4 plan

Branch `epic/fixes-4`, cut from main d33b3fb (fixes batch 3 merged). Todd
approved the scope on 2026-11-08. The batch pull request into main is
opened and left for Todd to merge. Gate B (SPEC 19.2) runs after this
batch, by Todd's ruling.

## Scope

- Generate (SPEC 5.6): bugs #315, #316, #321, #322; enhancements #317,
  #318, #319, #320, #323.
- Saves tab: #398. Mods: #340. App-wide headings: #378.
- STATUS gaps: the Undo button's time is UTC, not local (SPEC 17.11); the
  Deleted worlds page's Restore link names show UTC.
- Stretch: #399 (settings beside the large map view on desktop), only if
  everything above has landed cleanly.
- Left out on purpose: #400 (epic-sized: a headless game per preview),
  #353 (needs a vision change), #339 (P2), #132, #92, #206, #207.

## Rulings

- R1. The Generate tasks all edit `generate.html`, `mapgenform.html`,
  `generatepreview.html`, `generate.js`, `preview.js`, `generate.go` and
  `mapgen.json`, so they run one after another (lane G), each cut from the
  batch head after the one before it lands.
- R2. #319 and #322 are one task: both are about choosing between all
  planets and one planet in the preview.
- R3. `foreman.css` is the one shared file. A wave-one task may only add
  or change rules for its own page's components, in its own block; the
  orchestrator resolves any conflict when landing. #378 changes the shared
  heading and section rules, so it runs last, after every other screen
  change, and re-takes the screenshots once.
- R4. #315 uses the browser's own leave-page warning (`beforeunload`),
  shown only when the form differs from what was loaded or last saved. It
  is the only warning a browser allows for closing a tab or following a
  link out.
- R5. The Undo time and the Deleted worlds names are one task (T), because
  both need local time from `time.js`. It runs after the mods task (M),
  which owns the Undo label's code.
- R6. Machine load: every heavy run waits for the load gate (load under
  20, 12 GB free), uses at most 4 browser workers, and at most two full
  `make e2e` runs go at once.

## Tasks

| Task | Issues | Owner, model | Files (main ones) | Wave |
|---|---|---|---|---|
| G1 | #315, #316, #321, #320, #323 | ui-designer, Opus medium | generate.html, mapgenform.html, generatepreview.html, generate.js, preview.js, genterritories.go labels, mapgen.json (8), generate and large-view specs | 1 (lane G) |
| S | #398 | ui-designer, Opus medium | saves.html, saves.go, worlds.json saves keys (8), saves and click-budget specs, SPEC 17.10 click count | 1 |
| M | #340 | ui-designer, Opus medium | modplan.html, mods.html, modplanrows.go, mods.go, the resolver's exact-version use in internal/mods, mods.json (8), mods-plan and mods-tab specs, SPEC 9.2, 9.3 | 1 |
| G2 | #319, #322, #317, #318 | ui-designer, Opus medium | lane G files, mapgen size cap, SPEC 5.6 | 2 (lane G, after G1) |
| T | STATUS gaps (UTC) | builder, Opus low | time.js, deleted.html, the Undo label in mods.go and server.html, their locale keys, times spec | 2 (after M and S) |
| G3 | #399 (stretch) | ui-designer, Opus high | lane G files | 3 (lane G, after G2) |
| H | #378 | ui-designer, Sonnet high | foreman.css heading and section rules, DESIGN 2 and 4, screenshots of every page | 4 (last screen change) |
| Fold | all | builder, Sonnet high | docs/STATUS.md, SPEC, DESIGN, ADRs, delete this plan | last |

Each task's prompt names its exact file list; a task that must touch a file
outside it says so in its report.

## Checks

- Each task: `make verify` and the browser specs for every file it touched
  (the whole suite for a file every page uses, such as `time.js` and
  `foreman.css`), checked again by the orchestrator before the squash
  merge.
- After each wave: `make verify` and full `make e2e` on the batch head.
- Reviews at the end: code-reviewer, a11y-reviewer, security-reviewer (S
  sets which save file loads; M chooses which portal release downloads),
  and a second-agent translation review of the new strings (SPEC 22.6).
- Real-game check: S and M change which save and which mod versions the
  game loads, so a tester runs them against a real
  `factoriotools/factorio` container before the batch pull request. It
  needs UDP 34197, so Todd stops the trial game first.
