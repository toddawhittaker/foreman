# 0014. One snapshot per run of mod changes on a stopped world

- **Status**: Accepted
- **Date**: 2026-11-07
- **References**: SPEC.md sections 6.2, 9.4 and 9.13

## Context

Every mod change took its own "Before mod change" snapshot. An admin who
sets up a stopped world often makes many changes in a row: install a few
mods, turn one off, upload settings. Each one added a snapshot, so ten
changes filled the automatic retention (6.6) and pushed out older, more
useful snapshots. Undo after the last change only went back one step,
not to the world as it was before the admin began.

## Decision

On a stopped world, the first mod change since the game last launched
takes the snapshot. Later changes reuse it while the run holds, and undo
restores it. A running world still takes one snapshot per change.

Foreman keeps a small marker, `snapshots/<id>/mod-run.json`, outside the
world and outside every snapshot. It names the run's snapshot and lists
the world's files (path, size, modification time) as the last change left
them. A change reuses the snapshot only when the marker reads cleanly,
the snapshot still exists as a mod snapshot, and the world's files match
the list exactly. Anything else takes a new snapshot: a missing or
damaged marker, a snapshot removed by retention or by hand, a game that
ran, a save added, a settings change, a restore, or an import.

A start that launches the game ends the run, and the marker is removed
before the game starts, whether or not it then reaches the game. A start
refused by the pre-start check does not end the run, because the world's
files did not change.

## Consequences

Undo after a run of changes returns the world to before the first, and
the run costs one snapshot. The snapshot's time and file list are enough
for the page to say "Undo the mod changes since" a time and to list which
mods undo takes away or puts back.

The check is fail safe: when in doubt Foreman snapshots again, which costs
disk, never a lost state. The cost is one file listing per mod change on
a stopped world, and one more file Foreman owns. Retention does not
protect the run's snapshot; if it removes it, the next change snapshots
afresh.
