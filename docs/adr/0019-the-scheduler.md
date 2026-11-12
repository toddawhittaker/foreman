# 0019. The restart scheduler: a leaf package, a wait, then a countdown

- **Status**: Accepted
- **Date**: 2026-11-12
- **References**: SPEC.md sections 7.6, 7.8 and 7.12; DESIGN.md section 8.15

## Context

An admin wants a nightly restart that never kicks players off mid-game and
never turns a stopped server on. The controller already has a cancellable
stop countdown (7.6) and a restart (7.8).

## Decision

- **A leaf package.** `internal/schedule` uses only the controller's public
  methods, through its own `Game` interface, plus the notifier, so tests use
  a fake clock and game.
- **Wait, then countdown.** At the set time, if the active world's game is
  running, it polls `/players online` until nobody is on, then restarts with
  a fixed 60-second countdown. A player who joins during it triggers the
  existing Cancel stop, which tells them in chat, and the wait begins again.
  The limit (0 to 12 hours) counts from the set time; at the limit the day
  is skipped.
- **Two stop options**, set only by the scheduler. `WarnEmpty` runs the
  countdown even with nobody online. `OnlyRunning` refuses with
  `ErrNotRunning` and starts nothing when no game is in game; without it,
  restarting a game that crashed before the monitor saw it would start it.
  The schedule never uses "start anyway", and an RCON failure counts as
  someone online.
- **No catch-up.** A restart missed while Foreman or its host was down is
  not made up.
- **Daylight saving.** A time inside a gap runs at the next valid minute; a
  repeated hour runs once. One schedule serves the active world, and only
  its settings are stored.

## Consequences

A server that crashed overnight stays down until an admin starts it. A
restart can slip by hours while players stay on, then is skipped. A
scheduled restart sends both the scheduled-restart and start webhooks.
