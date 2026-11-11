# 0017. Mod settings: definitions from a one-off control-mod run; map settings on a saved world are changed in the game

- **Status**: Accepted
- **Date**: 2026-11-11
- **References**: SPEC.md sections 9.6, 9.13, 9.15, 14.3 and 14.6; ADR 0011 and ADR 0015

## Context

Editing mod settings in the UI needs each setting's type, default, limits,
allowed values, owner and visibility, which only the game knows. The
preview container has no network, RCON client or exec (14.3).

Startup settings live in `mod-settings.dat`. Map settings
(`runtime-global`) live in the save once the map exists. A real test on
1.1.110, 2.0.77 and 2.1.24 showed: the save's map values win over the file
(file 42, save 4, the mod saw 4); the game rewrites the file when the server
starts, not when it exits; and the only ways to change a map value in a save
are a Lua command in the owning mod's context, which ends the save's
achievements for good, or an admin in the game's Map tab.

## Decision

- Definitions come from a one-off control-mod run of the world's own image:
  a fourth kind of preview container, copying ADR 0011 and ADR 0015. The
  work folder gets the world's mods by hard link and never the world's own
  folder. The run also leaves a defaults `mod-settings.dat` written by the
  world's image.
- The result is cached in memory per world, keyed by the game version and
  the mods fingerprint without `mod-settings.dat`, and kept only when the
  key is the same before and after the run. A restart loses it.
- Foreman edits startup settings, and the map settings of a world with no
  save yet, as file writes through the mod change (9.13), changing only the
  edited entries.
- Foreman does not change the map settings of a world with a save. The page
  shows them read-only and says an admin changes them in the game under
  Settings, Mod settings, Map.

Rejected:

- **`--dump-data`**: it gives no owning mod for a setting, and its output is
  a 14 to 44 MB file that the 64 MB tmpfs cannot hold.
- **Reading definitions over RCON**: definitions are prototype data, not
  reachable from a running save without Lua, which has the achievement cost
  below.
- **Live map edits over RCON with Lua**: ends the save's local achievements
  for good, and the game refuses unless the command runs as the owning mod.
- **Stopping and editing the file**: the save wins; tested, nothing changed.
- **Patching the save**: the format is undocumented and changes by version.
- **Joining as a game client**: the Map tab sends a player input action over
  the lockstep multiplayer protocol, which Foreman cannot speak without
  running the whole game.
- **A disk cache**: new files to save one to two seconds on a miss.

## Consequences

- Editing a saved world's map settings takes a trip into the game. The page
  says so in one line.
- A cache miss runs the game (up to 5 minutes and 4 GiB at the limits), so
  the form loads after the page.
- Foreman checks every changed value itself, because the game silently
  resets a bad one.
- The definitions container can write the hard-linked mod zips, as the main
  game container can; editing inside a folder mod does not change the cache
  key; copying has no disk-space reservation beyond the 4 GiB cap.
- Per-player settings stay out of the page and keep their bytes.
