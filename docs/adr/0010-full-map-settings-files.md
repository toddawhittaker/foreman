# 0010. Generate writes complete map-settings files

- **Status**: Accepted
- **Date**: 2026-11-06
- **References**: SPEC.md sections 5.6 and 11.5

## Context

Generate (SPEC 5.6) lets the admin pick a preset and then change its
Enemy and Advanced settings, which live in `map-settings.json`. The game
reads that file only at `--create`. Two facts about the game shape what
Foreman can write:

- It refuses a `map-settings.json` that lacks any key its exact version
  knows, and the set of keys changed at 2.1.13 and again at 2.1.18. A
  partial file with only the changed keys is refused.
- When `--preset` and the files are both given, the preset wins every
  setting they share, so the admin's changes would be lost without any
  error.

## Decision

- An unchanged preset passes `--preset` and the seed, and writes no file.
- Any change writes both `map-gen-settings.json` and `map-settings.json`
  complete, and passes no `--preset`. The map-settings file starts from
  the example file in that exact version's image, takes the preset's
  values as the game applies them, then the form's changes.
- A version with no tested layout (2.1.13 to 2.1.17) offers no
  map-settings fields. A changed default preset writes the map-gen file
  alone; a changed other preset is refused.
- Rejected: **partial files**, because the game refuses them. Rejected:
  **`--preset` plus the files**, because the preset silently undoes the
  changes.

## Consequences

- Foreman must carry each layout's example file and each preset's values,
  read from the game images when the project is built. A preset's values
  differ from the example file in places (2.1's minimum expansion
  cooldown, 1.1's research queue), so both are pinned by tests against
  the real game.
- A new game version with a new layout needs its own data before its
  map settings can be changed.
- Editing the file after the map is made has no effect, so map settings
  are offered only at Generate (SPEC 11.5, issue #207).
