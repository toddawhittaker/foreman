# 0003. The game container: shape, identity and network

- **Status**: Accepted
- **Date**: 2026-11-03
- **References**: STACK.md sections 1, 5; SPEC.md sections 4.2 to 4.6, 8.7, 14.3, 14.4

## Context

The `factoriotools/factorio` entrypoint fails as any user but its own and
changes between versions. The game is the part Foreman trusts least (SPEC 14.1).

## Decision

- **Run the game binary directly** as the configured uid and gid. A shell
  wrapper reads the RCON password from the world's file, so Docker's
  settings never hold it. All capabilities are dropped.
- **Name and labels.** `foreman-<world-id>`, with `world` and `install`
  labels. Foreman finds its containers by label and refuses a container
  of that name it did not make.
- **A fresh container per start**; Docker's own restarts never recreate it.
- **Poll, do not subscribe.** A monitor inspects the container every few
  seconds and when the log stream ends. The exit code is not used.
- **`--start-server-load-latest`**, never a fixed save name.
- **Network.** `FOREMAN_GAME_NETWORK` is required inside a container. A
  game is refused when other containers share the network, a `tcp://`
  Docker host is not on another of Foreman's networks, the range overlaps
  `FOREMAN_TRUSTED_PROXIES`, or Foreman cannot find itself (SPEC 4.4).

## Consequences

- Rejected: **the image entrypoint**. Less code, but it needs its own uid
  and its behaviour moves between images.
- Rejected: **reusing a container**. Settings drift unseen. Recreating
  loses the game's write folder and log, which Foreman does not need.
- Rejected: **Docker's `GET /events`**. Faster, but a missed event needs a
  catch-up path anyway. Polling costs seconds and has one path.
- Rejected: **a fixed save name**. After a crash Docker would reload it and
  skip newer autosaves. Choosing a save sets its time instead.
- Rejected: **picking the network automatically**. It can put a game beside
  the socket proxy. Naming it costs the operator one setting.
