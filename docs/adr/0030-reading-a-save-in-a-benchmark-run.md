# 0030. Reading a save in a one-off benchmark run

- **Status**: Accepted
- **Date**: 2026-11-21
- **References**: SPEC.md sections 8.14, 8.15, 8.16, 5.16, 14.3 and 14.6; ADR 0011, ADR 0014

## Context

Two features need facts that only the game can read from a save: the
players of a save and their play times (#769), and the factory map (#773).
The save format is not documented. Foreman already runs four kinds of
locked-down one-off game container (SPEC 14.3): no network, a read-only
root, no RCON, and only a fresh work folder mounted. I wanted the new reads
to fit that shape, and I wanted the world's save never to change.

A feasibility test on 2026-11-21 ran the game on copies of five saves on
2.0.77 and 2.1.24:

| Save | Mods | Time | Peak memory |
|---|---|---|---|
| 174 KB | Space Age only | 1.0 s | 353 MB |
| 32 MB | Space Age and 2 mods | 5.0 s | 1.50 GB |
| 94 MB | Space Age only | 12.3 s | 2.83 GB |
| 187 MB | Space Age and 44 mods | 43.6 s | 6.87 GB |

The test also found that a helper mod's `on_init` runs on a save that never
had the mod, that benchmark mode never writes the save (every copy hashed
the same before and after), that the game rewrites `mod-list.json` and
`mod-settings.dat`, that a missing mod is dropped silently with a
successful exit, and that the save's `connected` and `last_online` are wrong
for players who were online when it was saved.

## Decision

The world's own image runs `--benchmark <save> --benchmark-ticks 8
--benchmark-runs 1` in a one-off container with no network. The work folder
holds the save as a hard link mounted read-only over the link (a copy when
the link fails), the world's mods, copies of `mod-list.json` and
`mod-settings.dat`, and a control-only helper mod that writes JSON. The game
exits by itself after 8 ticks. `config/`, `rconpw` and
`server-settings.json` never enter the work folder.

Mods are checked twice: the pre-start check before the run, and the helper's
list of active mods against the world's enabled mods after it. Either
failure keeps no figures. The run's limits are 12 GiB with no swap and 5
minutes, in the one preview slot. The two kinds are
`foreman-saveplayers-<folder>` and `foreman-factorymap-<folder>`.

The method lives in one function, `saveCopyCommand` in
`internal/game/savecopy.go`, so a change of method touches nothing that
reads the output.

Total play time is `game.tick`. While the game runs, Foreman reads it with
plain `/time` over RCON, never Lua, so achievements stay on. The test showed
"276 hours and 40 minutes" for tick 59,760,005.

Rejected:

- **A throwaway server with a Lua command over RCON.** It needs a network,
  contacts auth.factorio.com at start, puts an RCON password inside an
  untrusted container, and needs a readiness wait and a stop.
- **A throwaway server that is killed when the mod's file appears.** No
  RCON, but still a server's lifecycle and a reach for the network. It is
  kept only as the fallback if a game version breaks benchmark mode.
- **Reading `level.dat` in Go.** It is undocumented and shifts between
  patches.

## Consequences

The reads add no Docker endpoint, no network and no secret to a one-off
container. The save cannot be written, by the game or by anything else in
the container. A read needs about as much memory as running the game, so
it runs by itself only while no game is running on this Foreman, and beside
a running game only when an admin clicks and has been shown the figure. A
host with less memory than a large save needs fails the read, which is safe
while the world is stopped. A future game version that breaks benchmark
mode means rewriting `saveCopyCommand` for the fallback.
