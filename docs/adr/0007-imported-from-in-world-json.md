# 0007. Where a world was imported from is recorded in world.json and matched by time

- **Status**: Accepted
- **Date**: 2026-11-05
- **References**: SPEC.md sections 3.4, 5.1, 5.5. Builds on ADR 0002.

## Context

The Import page should say which inbox folders are already in a world, so
an admin does not import the same folder twice. Foreman needs a record of
what each import copied, and a way to tell that a folder still holds the
same save. The folder can be edited by hand, and Foreman never changes it.

## Decision

- The record is an `imported_from` object in the world's `world.json`: the
  folder name, the newest save's name, size and modification time (to the
  nanosecond), and the time of the import. It is not in the database,
  because SPEC 3.4 keeps game data out of it, and a world that is moved or
  backed up keeps its record. Fields are only added, so no format version
  changes (SPEC 5.1).
- A folder counts as already imported when its name and its newest save's
  name, size and modification time all match a record. Foreman does not
  hash the save: a hash reads gigabytes on every page load, and a changed
  save changes its size or time. Saves added later also change the newest
  save, so the folder becomes new again.
- The Import page reads only each world's `world.json` to find the
  records. It does not measure disk use.

## Consequences

- Worlds imported before this change, and uploaded saves, have no record
  and are never matched.
- A folder whose save was copied back with a new modification time looks
  new. The admin can import any folder anyway, so the cost is a duplicate
  world, never a loss.
- Deleting a world removes its record, so its folder shows as waiting
  again.
- Rejected: **hashing the save**. Slow, and no clearer for the admin.
- Rejected: **a database table**. It would split a world's facts between
  two stores (SPEC 3.4).
