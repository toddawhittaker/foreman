# 0016. Keeping run state right when Docker's view lags an operation

- **Status**: Accepted
- **Date**: 2026-11-09
- **References**: SPEC.md sections 7.3, 8.7 and 14.3; ADR 0003

## Context

Two races gave the wrong state while a start was in flight.

- The monitor reads Docker (the container list, then each inspect) before
  it takes each world's lock. A start that began and finished between the
  read and the lock was then overwritten by the older look, so a running
  game showed as stopped until the next poll, and a switch that read that
  state could leave the new world stopped.
- A start notices a failure two ways: the log follower reads the game's
  failed-load line, or a container poll sees the game stop. The game writes
  the line and exits at once, so the poll could win, or Docker's restart
  could begin a new run that cleared the mark, and a save that failed to
  load was reported as a plain stop. Under load this failed 53 of 400 runs.

## Decision

- Each world counts the operations it has begun. The monitor notes the
  count before it reads Docker and drops a look if an operation is running
  or one began since the look was taken; a dropped inspect reads no usage
  either. Rejected: holding the lock across the Docker read (it would block
  every operation on a slow daemon), and a version number on every state
  write (more to keep right for the same effect).
- When Start's container check sees the game stop, it reads the start's
  container log before it says why. Every start makes a fresh container
  (ADR 0003), so that log holds every run of this start and nothing older.
  If a run logged a failed load, the start fails as a failed load with the
  game's reason, masked then cut, and the run is marked as the follower
  would mark it. The read asks only for the log's tail (the existing
  allowed logs call, SPEC 14.3) and keeps the newest bytes. Rejected:
  waiting for the follower to catch up (no bound on how long), and trusting
  the exit code (SPEC 8.7: it differs between versions).

## Consequences

A look is lost now and then, and the next poll (5 s in production) takes
its place. A failed load reads the same whichever check sees it first. If
Docker's newest run stops for another reason, Start still reports the
earlier run's failed load. Both races are tested directly, through hooks in
the fake Docker daemon, because loops of the old tests hit them only under
heavy load.
