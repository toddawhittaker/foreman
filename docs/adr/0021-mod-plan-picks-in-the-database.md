# 0021. The mod plan's picks are saved in the database, not the world folder

- **Status**: Accepted
- **Date**: 2026-11-15
- **References**: SPEC.md sections 3.4, 6 and 9.14

## Context

The Mods tab keeps a plan: the mods a person picked with "+" and has not yet
installed. It must survive a page reload and a visit to another page. Two
places could hold it. The world folder is the natural home for per-world
data. The app database already holds small per-world records.

## Decision

- **The picks are rows in the app database.** The table `mod_plan_mods` has
  one row per world and mod name, with who added it and when. Only the names
  of the mods picked are saved, at most 500 per world (50 until fixes batch 11). The dependencies are
  worked out again each time the plan is drawn.
- **Not in the world folder.** Every write to a world's files needs the
  snapshot SPEC 6 requires first, and a pick is not worth a snapshot. A
  plan in the folder would also travel with snapshots, clones and restores,
  so a restored or cloned world would open with picks nobody made there.
- **Deleting a world clears its picks.** World ids are never reused, so a
  deleted world's picks cannot reach a new one.

## Consequences

- Adding or removing a pick is cheap and takes no snapshot.
- A snapshot restore, a clone or an import never carries a plan, and an
  undo does not bring picks back.
- Moving the app database without the data directory loses the picks, which
  is acceptable for an unfinished plan.
- Versions chosen on the review page are not saved, since only names are.
