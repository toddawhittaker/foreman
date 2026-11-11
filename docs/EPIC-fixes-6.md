# Fixes batch 6 plan

Branch `epic/fixes-6`, cut from origin/main 322eb40. Serves SPEC 5.6
("Resource amounts", "Patches", "Downloads"), 14.3 and 17.15. Closes #460
and #461.

## Rulings (Todd, 2026-11-10)

- Fulgora scrap joins within 16 tiles with a bucketed scan; every other
  ore joins within 6 (#461). Wells stay at 24.
- A patch's label (not its list entry) is hidden when a patch of the same
  resource with at least 4 times its amount lies within 64 tiles, box edge
  to box edge.
- "Download all" counts every planet that has nothing held, with progress
  ("planet 2 of 5") and Cancel; on failure it names the planet and offers
  the zip without it or Try again. A single-planet download counts on
  demand too.
- SPEC 5.6 text changes are shown to Todd in the epic PR.

## Tasks (chained; each starts from the previous verified head)

| # | Task | Owner / model | Files | Closes |
|---|------|---------------|-------|--------|
| 1 | Per-planet counts: rebase `resource-amounts/per-planet-counts` (94e8e39) onto the epic head and re-verify | builder, Sonnet high | whatever the branch touches | |
| 2 | Fulgora scrap gap 16: rebase `resource-amounts/fulgora-scrap-gap` (1bc6fb4) onto task 1 | builder, Sonnet high | control.lua, realgame2_resources_game_test.go, ADR 0015, SPEC 5.6 | |
| 3 | Default ore gap 6 and label hiding; re-measure hiding counts and Fulgora test limits; cost check incl. 600% scrap on 2.0.77 | builder, Opus low | control.lua, internal/resource, internal/web/genresources.go, tests, ADR 0015 | #461 |
| 4 | Download all counts every planet (progress, cancel, failure handling, single-planet on demand) | builder, Opus low | internal/web, generate page JS/templates, e2e, locales | #460 |
| 5 | Fold: STATUS, SPEC 5.6, ADR, delete this plan | builder, Sonnet | docs | |
