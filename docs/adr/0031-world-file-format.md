# 0031. The world file format

- **Status**: Accepted
- **Date**: 2026-11-21
- **References**: SPEC.md sections 5.1, 5.5, 5.13, 5.14, 5.15, 14.5, 14.6 and 14.8; ADR 0002

## Context

I want to move a whole world to another Foreman in one file (#774). Exported
files sit on people's disks for years, so the layout and the format file
are the part that is hard to undo. Everything else is code that can change
later.

## Decision

A world file is one ZIP, `<name>-world.zip`. Unpacked, it is a world folder
in the SPEC 5.1 layout plus one format file:

- `foreman-world.json`, always the first entry: `format_version` (1),
  `exported_at`, `foreman_version`, `saves` (`all` or the loaded save) and
  `game_password_set` (whether a password existed, never its value);
- `world.json`;
- `saves/<save>.zip`, stored, not compressed again;
- every regular file under `mods/`, zips stored and the rest deflated;
- the 5.1 config files.

Each entry's ZIP time is the file's modification time.

**The lowest version rule.** A writer uses the lowest format version that
can hold what it writes. If snapshots are added later as format 2 (SPEC
5.15), an export without snapshots stays format 1, so an older Foreman can
still read it. A reader refuses a format version newer than it knows.

**What is stripped.** `config/rconpw` is never exported; the new host makes
its own. The game password and the factorio.com username, password and
token are removed from `server-settings.json` on export and again on
import. `imported_from` and `archived` are dropped from `world.json`.
`players.json` is left out, because the new host reads its own. Hidden
files, `.tmp.zip` saves, stray files and work leftovers are left out. A
symlink or other special file refuses the export. Nothing from the app
database is read.

Rejected:

- **A new layout of Foreman's own.** Using the 5.1 layout means a world
  file is also an inbox folder for SPEC 5.5, readable without Foreman.
- **One format version for every export once snapshots exist.** It would
  lock older Foremans out of files they could read.
- **Snapshots in format 1.** Not needed now, and large.

## Consequences

Someone without Foreman can unzip a world file and read it, and an old
Foreman can import it by hand as a folder. `mod-settings.dat` and a save's
mod data can hold anything a mod chose to keep, a token included, and
cannot be stripped, so the export card warns about it. A file is untrusted
input on import and is checked under SPEC 14.5 and 14.6.
