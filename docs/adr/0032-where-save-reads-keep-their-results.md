# 0032. Where a save read keeps its results

- **Status**: Accepted
- **Date**: 2026-11-21
- **References**: SPEC.md sections 5.1, 5.16, 6.1, 8.14, 8.16 and 14.3; ADR 0030

## Context

ADR 0030 settled how a save is read. Three choices about what happens to the
output were left: where the players record lives, where the factory map
pictures live, and where the game writes while it runs. The first draft put
the map run's write folder on disk, in the work folder, because its output
can be hundreds of MiB.

## Decision

**The players record is a world file**, `worlds/<id>/players.json`.
Snapshots, restores and clones then carry it with no new code, and a
restore brings back the record that matches the restored save. It stays out
of a world file, because the new host reads its own.

**Factory map pictures live outside the world**, in
`factorymaps/<world-id>/` under the data folder. They are drawn from the
save and can always be drawn again, they can be large, and a snapshot or a
world file should not grow by a picture. One drawing per world is kept,
replaced atomically, and removed with the world.

**The game writes only to a 1 GiB in-memory folder.** The helper's output is
at most 512 MiB (SPEC 14.6), which fits. The folder counts against the run's
12 GiB limit, so a hostile or broken mod cannot fill the host's disk, and
only the helper's named output is moved into the work folder for Foreman to
read.

Rejected:

- **The players record in the app database.** Snapshots and restores would
  need code to keep it in step with the save.
- **Factory map pictures in the world folder.** Every snapshot would copy
  them, and a world file would carry them.
- **The map run's write folder on disk.** The game could then write as much
  as the disk holds before the output limit is checked.

## Consequences

A restore or a clone needs no players code. A deleted world's pictures need
a sweep at startup, since Foreman may be down when the world goes. A host
with little memory has 1 GiB less for the game during a read.
