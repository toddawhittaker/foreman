# 0036. The control mod reads icon paths in the data stage

- **Status**: Accepted
- **Date**: 2026-11-23
- **References**: SPEC.md sections 9.6 and 14.6; issues #914 and #920

## Context

Foreman wants a mod's own icons: for the objects named in a mod setting's
text, and for each planet on the star map. The game's runtime API, which
the control mod already uses to list settings and planets, gives no icon
paths. A prototype's `icon`, `icons` and `starmap_icon` exist only in the
data stage, and a mod can change them as late as its own
`data-final-fixes`. Foreman's mod settings run already starts the world's
own game image with its mods, twice, once on default values and once on the
world's saved values, and caches each result.

## Decision

- The control mod gains a `data-final-fixes.lua`. It copies the icon paths
  out of `data.raw` into one `mod-data` prototype, `foreman-icon-export`.
  `control.lua` reads that prototype at runtime and writes `icons.json`.
- It keeps only an icon of one picture: a plain `icon`, or `icons` with one
  layer that has no tint, shift, scale or floating. Paths under the game's
  own mods are never exported, because the art is Wube's.
- The control mod's `info.json` names every mod the run loads as an optional
  dependency, so it loads after them and its data stage sees their final
  icons.
- The data stage runs under `pcall` and only when the `mod-data` type
  exists, so 1.1 and an image without it write no icon file, and a failure
  there never fails the run. The star map is written from the runtime API
  in the same run, in its own `pcall`.
- No new run. Both existing runs write both files, and the results are
  cached with them.

## Consequences

The icon paths come from the game's own loaded data, exactly as the game
would use them, with no parsing of mod files in Foreman. The control mod is
no longer control-only: it adds one invisible prototype to a throwaway run.
A mod whose name is not a plain portal name cannot be listed as a
dependency, so it could change an icon after the export and leave a wrong
or missing icon, never a failed run. The export is limited in size, so a
huge pack with unusual names could have its icon file refused whole, and
the page then shows names.

Rejected: parsing the mods' Lua and `info.json` in Go, which would have to
copy the game's data stage, its load order and its fixes and would still be
wrong for a mod that builds icons by code; and a second game run just for
icons, which costs another container, up to 4 GiB and several seconds, to
read what the first run can carry out.
