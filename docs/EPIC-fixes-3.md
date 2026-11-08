# Fixes batch 3 plan

Branch `epic/fixes-3`, cut from main 6130733 (Epic 8 merged). Todd asked
for this batch on 2026-11-07 and authorized running it through overnight
without stopping for plan approval. The batch pull request into main is
opened and left for Todd to merge.

## Scope

Todd's list, bugs first: #357, #358, #356, #337, then #354, #359, then
#355 with #345, #360 with #342 and #343, #344, #340 and #341 as time
allows. Issues filed by the step-one workflow review of Epic 8's screens
join the task whose files they touch. Left out on purpose: #353 (needs a
vision change), #339 and #132.

## Rulings

- R1. Tasks that touch the mods screens all edit `internal/web/mods*.go`,
  `internal/web/templates/mod*.html` and `internal/i18n/locales/*/mods.json`,
  so they run one after another (lane M below), each cut from the batch
  head after the one before it lands.
- R2. #357 and #358 are one task (A): both change `internal/mods/planner.go`.
  A starts from branch `epic8/f8-wip` (72acea5) with the batch branch merged
  in, and keeps or discards that work on its merits.
- R3. #356 (refusal layout) lands before #359, because #359 adds a refusal
  with a new first next step to the same screen.
- R4. #359 fails safe for availability: a portal that cannot be reached, or
  a check that runs out of time, never blocks a start; the result says the
  check could not be made.
- R5. Machine load: every heavy run waits for the load gate (load under 20,
  12 GB free) and uses at most 4 browser workers; another project builds on
  this machine.

## Tasks

| Task | Issues | Owner, model | Files (main ones) | Wave |
|---|---|---|---|---|
| A | #357, #358 | builder, Opus low | internal/mods, internal/web/mods.go, modplan.html, mods.json (8), mods-install and mods-save specs, SPEC 9.2, 9.4 | 1 (lane M) |
| B | #337 | builder, Opus low | internal/game (the subnet refresh and its test) | 1 |
| C | #356 | ui-designer, Opus medium | failure.html, the Server page step result template, foreman.css rules for those lists, server.json (8), their specs | 1 |
| D | #354 | builder, Sonnet high | account.html, account.json (8), README "Installing mods" | 1 |
| E | #359 | builder, Opus medium | the pre-start check (internal/game or internal/world), internal/modportal cache use, the refusal on failure.html, mods.json and server.json (8), SPEC 7.4, 9, 17.10 | 2 (lane M, after A and C) |
| G | #355, #345 | ui-designer, Opus medium | modsearch.html, the search handler, mods.json (8), search specs | 3 (lane M) |
| H | #360, #342, #343 | ui-designer, Opus medium | mods.html, the Mods tab handler, mods.json (8), Mods tab specs | 4 (lane M) |
| I | #344, #340 | ui-designer, Opus medium | modplan.html, the plan handler, resolver exact-version use, mods.json (8), plan specs, SPEC 9.2, 9.3 | 5 (lane M) |
| F | #341 | builder, Opus low | internal/world snapshot step for mod changes, undo wording, SPEC 6 and 9.4, ADR | after I, or alongside lane M when its files are shown to be disjoint |
| Fold | all | builder, Sonnet high | docs/STATUS.md, SPEC, ADRs, delete this plan | last |

Each task's prompt names its exact file list; a task that must touch a file
outside it says so in its report.

## Checks

- Each task: `make verify` and the browser specs for every file it touched
  (whole suite for a file every page uses), checked again by the
  orchestrator before the squash merge.
- After each wave: `make verify` and full `make e2e` on the batch head.
- Reviews at the end: code-reviewer, security-reviewer (E reads the portal;
  F changes snapshots), a11y-reviewer, and a second-agent translation
  review of the new strings (SPEC 22.6).
- Real-game check: A, E and F change mods and snapshots, so a tester runs
  them against a real `factoriotools/factorio` container before the batch
  pull request.
