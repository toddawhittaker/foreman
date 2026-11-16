# 0023. A stop takes the missed look at a restarted game before it plans

- **Status**: Accepted
- **Date**: 2026-11-16
- **References**: SPEC.md section 8.7; ADR 0016; issue #646

## Context

Docker can restart a crashed game before Foreman's next poll. A Stop or
Restart sent in that gap saw a restart count above the one Foreman last
looked at, concluded "crashed", and stopped the game without warning players
or saving, even when the new run was already in game. The aim was two
guarantees: a game in game is always warned and saved, and a game not in game
is never refused a Stop.

Two ways were open.

## Decision

The operation takes in the restart itself, before it plans. When Docker's
restart count is above the one Foreman last saw, it takes the monitor's own
look (`observe`, with the log read of ADR 0016). That look counts the crash,
starts following the new run's log and drops the RCON connection to the run
that ended. The step runs in `beginOpOn`, which every operation passes through
before it locks its worlds, so Switch, Restore, Rollback, mod changes and
Update get it too, and a new action cannot forget it. The Docker call runs
before any world lock is held. Because the follower reads the in-game line
asynchronously, the stop also asks the game `/players online` when the log has
not said "in game" yet: Factorio opens RCON only once in game, so an answer
means in game.

Rejected: reading the log since the run's start time and treating the game as
running only once RCON reaches it. It would tell us the run is in game, but
the stop would have no log follower: the save waits for the save-done line
that the follower reads, and the follower of the crashed run has ended, so the
save would time out. It would also duplicate what the monitor's look already
does.

## Consequences

- One code path (the monitor's look) decides what a restart means, so the
  stop and the poll cannot disagree.
- A Start now costs one extra Docker inspect when it passes through
  `beginOpOn`.
- Two small windows remain. A crash in the instant between the catch-up and
  the plan still takes the plain-stop path, now microseconds wide instead of a
  poll interval. A stale monitor look applied after the catch-up can wind the
  run's start time back until the next poll; the RCON check still warns and
  saves.
