# 0002. World and snapshot formats

- **Status**: Accepted
- **Date**: 2026-11-03
- **References**: SPEC.md sections 3.4, 5.1, 5.2, 5.5, 6.5, 6.7, 6.8, 6.11

## Context

These files outlive any Foreman release, and a person must be able to read
and restore them without Foreman. Never losing a save, cheap snapshots
and not trusting the game container (SPEC 14.1) pull against each other.

## Decision

- **Active world**: a row in the table `active_worlds(world_id, since)`.
  A set of rows leaves room for several active worlds; one is used.
- **World id**: made from the name (lowercase letters, digits, hyphens, at
  most 40 characters, `-2` when taken). It never changes on rename and is
  never reused; a world or snapshot folder holding an id keeps it taken.
- **`world.json`**: `format_version` (1), `name`, `game_version`,
  `space_age`, `load_save`, `created_at`, in snake_case. Fields are only
  added; a rewrite keeps unknown ones. A newer `format_version` is listed
  but not changed.
- **Snapshots**: folders named by UTC time (`20261103T142501Z`). The
  `snapshot.json` manifest holds label, reason, time, who, each file's
  path, size and nanosecond time, and a SHA-256 per zip. Disk use counts
  each inode once.
- **Restore** builds `.staging-*`, renames the live folder to
  `.restore-old-<id>-*`, then renames the staged one into place. Startup
  recovery finishes or reverses a half-done swap.
- **Hardlinks are kept** for zips (SPEC 6.8), with hashes to catch change.

## Consequences

- Snapshots cost almost no space, and `cp` can restore one.
- SPEC 6.8 trusts the game to write saves as new files; SPEC 14.1 does
  not. A rewritten save would change its hardlinked snapshot too. Todd
  chose hardlinks plus hashes over copying every zip.
- Accepted limits of the hashes: they are checked before restore stages
  files, not again while linking; and a hash reused because inode, size and
  time are unchanged is fooled by a same-size rewrite with the time set
  back. Both need write access to the world.
- Restore needs startup recovery, so `worlds/<id>` never holds a
  half-restored world.
