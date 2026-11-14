# 0016. Keeping run state right when Docker's view lags an operation

- **Status**: Accepted
- **Date**: 2026-11-09
- **References**: SPEC.md sections 7.3, 8.7, 8.9, 8.10 and 14.3; ADR 0003
- **Amended 2026-11-14**: crash reasons in a fast crash loop (below).

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

## Amendment 2026-11-14: crash reasons in a fast crash loop

### Context

The real game in a crash loop ends each run within half a second, and
Docker restarts it within a few hundred milliseconds. One poll then saw the
restart count rise by three at once, after Docker had cleared the
out-of-memory flag, while no follower had read the runs in between: a loop
of out-of-memory kills or of a save that cannot be read showed as unknown.
The looks that could fill the gap race each other: the poll inspects, then
may spend up to five seconds reading the log, while the follower's look at
an exit can be newer.

### Decision

- When the log stream ends, the follower inspects the container at once
  and takes in the out-of-memory flag of the exit it finds.
- Each look at an exit carries Docker's `FinishedAt`. The world keeps that
  of the last exit taken in and refuses a look at an exit no newer, in
  either direction (a stale "not out of memory" or a stale "out of
  memory"). A running or paused container's flag and `FinishedAt` describe
  no new exit and are ignored. Foreman's own stop of a crash loop is passed
  over. A new container starts afresh; the game getting back in game clears
  the flag and moves the kept time to that log line's time. Rejected:
  ordering by the operation count alone (it does not change between two
  exits of one container), and the restart count (Docker raises it on the
  start, after the exit's flag was cleared).
- When a look finds the restart count risen, the poll reads the log once:
  the start's read, with the same tail, byte and five-second bounds and
  Docker's line times. It is skipped when the world's secrets are not known
  to mask it, and dropped unless it reached the log's end. Lines the
  follower adds after it are read on top.
- The flag outranks the log's words unless the run that said them began,
  by Docker's time of its first line, after the exit the flag names. Both
  times come from Docker's clock. A run whose time is not known (the
  follower kept only the newest 64 run starts) never overrules the flag.
- A crash's reason is that of the newest run that said how its load ended,
  since the newest is often still loading. A crash or crash-loop event is
  sent at once only when that reason is final: the newest run's own, or an
  earlier run's from a read that reached the log's end after the restart.
  An earlier run's reason seen only by the follower may yet give way to the
  crashed run's words, so the event waits, within the settle bound, and
  then names the last reason seen. The Server page shows that reason
  meanwhile.

### Consequences

A loop of out-of-memory kills, or of a bad save, is named on the look that
finds it. A host kill, a flag both looks missed, and runs whose words
neither the follower nor the read saw stay unknown. When the follower lags
and the read is refused, a crash event can still name the newest run the
follower read, which may be older than the run that crashed. The races are
tested by taking the looks by hand in a set order.
