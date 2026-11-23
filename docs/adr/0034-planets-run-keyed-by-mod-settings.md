# 0034. A second planets run, keyed by the world's mod settings

- **Status**: Accepted
- **Date**: 2026-11-23
- **References**: SPEC.md sections 5.6 and 9.6; ADRs 0017 and 0033; issue #878

## Context

Generate step 4 lists the world's planets, and ADR 0033 takes that list
from the mod-settings run. That run uses each setting's default value, so
that the setting definitions it also returns never depend on what a person
saved. A startup setting can add or remove a planet, though, so once step 3
lets a person change startup settings, the planets must follow the saved
values. Running the one run with the saved values would make the
definitions follow them too.

## Decision

- The definitions run stays on the mods' default values and keeps its
  cache, keyed by the mods fingerprint.
- A second "planets" run copies the world's `mod-settings.dat` into its
  work folder (a copy, because the game writes the file back) and writes
  the planet list. Its cache key is the definitions key plus the hash of
  that file. A world with no `mod-settings.dat` uses the definitions run, so
  the game runs once.
- Both runs are one function that takes the run kind (`modSettingsRunKind`),
  so the work folder, limits and checks are not copied.

## Consequences

Step 4's planets follow step 3 and the mod settings page, and the settings
form still shows each setting's default. A world that has saved settings
costs a second game run the first time its planets are read, and a second
cache. Rejected: one run on the saved values (the definitions would follow
the saved values, and a change would flush both); a run on every page view
(too slow); and reading planets from the saved values in Go (the game's own
data stage decides the planets, and Foreman cannot repeat it).
