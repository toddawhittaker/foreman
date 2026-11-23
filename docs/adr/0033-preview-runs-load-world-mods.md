# 0033. Preview runs load the world's mods

- **Status**: Accepted
- **Date**: 2026-11-22
- **References**: SPEC.md sections 5.6, 9.6, 14.3 and 14.6; ADRs 0011, 0015 and 0017; issue #775

## Context

The preview, territories and resource counts (ADRs 0011, 0015) ran with
the base game and Space Age only, because Generate ran before a world had
mods. Planet mods add planets and resources and change the built-in ones,
so the pictures did not match the map players get. Generate now makes the
world, then its mods, then its map settings (SPEC 5.6). Tests on #775: the
headless game draws modded planets on 2.0.77 and 2.1.24 in 2 to 4 seconds
each, under 750 MiB; script-made terrain (Cerys) looks right only in a
real game, through `--test-save` on 2.1.

## Decision

- The preview, territory and resource runs of an Unfinished world load
  that world's mods: hard links (copies as the fallback) and copies of
  `mod-list.json` and `mod-settings.dat` in the work folder, as the
  mod-settings run does (ADR 0017). The limits stay 1 GiB, with 60 seconds
  for each planet drawn.
- The planet list comes from the mod-settings run, which also writes
  `planets.json`: one run, one cache.
- On 2.1, an opt-in real-game picture runs `--create`, then `--test-save`
  with a control-only helper mod that charts 1024 by 1024 tiles (1 GiB,
  90 seconds). The fast preview stays the default. A user draws one at a
  time, and the picture is kept in memory, keyed by the settings, seed,
  mods, game version and planet, until the preview store's size limit
  evicts it or the world's map is made. A restart loses it. There is no
  disk store, because a picture is cheap to ask for again.
- The planets a run may draw come from the stored output of the world's
  mod-settings run, never from the form. A run with a world's mods takes
  at most 10 minutes in all.

## Consequences

Pictures, counts and planet tabs match the world's mods. Each run links
the mods first and runs slower, so the limit is per planet and the store
grows to 160 MiB. A mod's broken data can now fail a preview. Rejected: `--dump-data` for the planet list (14 to 44 MB, which the
tmpfs cannot hold, ADR 0017); staging mods before a world exists (a second
download path and cleanup of abandoned staging); always the real-game
picture on 2.1 (13 to 40 seconds a redraw, no live redraw); and the fast
preview only (Cerys and other script-made terrain would look wrong).
