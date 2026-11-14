# 0006. Running the game inside a VPN sidecar's network

- **Status**: Accepted and built in Epic 9b (SPEC 18.9b). Live Tailscale and host-reboot acceptance are still open (STATUS.md, Epic 9b).
- **Date**: 2026-11-04. Revised 2026-11-14 (shared sidecars, RCON and trust rules).
- **References**: SPEC.md sections 3.2, 4.4, 4.6, 4.13, 12.6, 14.1, 14.16, 18.9b. Amends ADR 0003.

## Context

Some operators want players to join through Tailscale, Netbird or another
VPN sidecar container. It is optional: by default the game port is published
on the host and no VPN is needed. Foreman must not prevent a sidecar.
The sidecar is the operator's: Foreman never installs, signs in, configures,
restarts or removes it, and no VPN credential ever enters Foreman.

## Decision

- Optionally, the setting `FOREMAN_GAME_NETWORK_CONTAINER` names a running
  sidecar. When set, the game container uses Docker network mode
  `container:<verified sidecar ID>` and Foreman publishes no port. The game
  declares neither exposed nor published ports in this mode, which Docker
  refuses anyway. It listens on UDP 34197 on every interface, so VPN players
  join through the VPN and LAN players through the port the sidecar
  publishes on the host, set in the sidecar's own definition. When that
  port has no host binding, the Server page warns that LAN players cannot
  reach the game; the start goes ahead. The warning is evidence from
  inspection, not a connectivity probe.
- The sidecar must be on `FOREMAN_GAME_NETWORK` and is the one extra
  container allowed there. RCON is bound only to the sidecar's IPv4 address
  on that network (`--rcon-bind <address>:27015`). The address reaches the
  game in the environment variable `FOREMAN_RCON_BIND`, which the start
  script passes as a single argument, so no environment content is ever
  interpreted as shell text. This keeps RCON off the VPN interface, but
  operator-defined routes or forwarding can still reach that address.
- One code path finds the RCON address for the first connection, each
  reconnect and a Foreman restart. It reads the address from the game
  container and trusts it only when the container the game's network mode
  names, inspected by that ID and never by the configured name, still has
  exactly that address on the game network. Each new RCON connection is
  checked again before the password is sent, so a replacement sidecar at
  the same address never receives it from a cached client. The check is
  Docker's word at one moment and cannot close the gap before the login.
- Additional Docker networks, including shared networks, are allowed.
  Their unrelated services are not refusal reasons. Check every attached
  Docker range for trusted-proxy overlap and for the address of a `tcp://`
  Docker API/socket proxy; unknown ranges fail closed.
- Every Docker range of the sidecar counts as a game range for sign-in,
  read before the first start, on every monitor look and again after a
  Foreman restart, and kept when a later read fails. Docker does not reveal
  VPN ranges: never trust the sidecar's VPN address or a broad VPN range as
  a sign-in proxy. The product hard-codes no Tailscale range.
- A start is refused when the sidecar is missing, not running, on the host
  network, in another container's namespace, or off the game network, and
  before any world file changes. It is looked up by name at every start;
  creation joins the inspected ID, and a failed create or start never
  falls back to another network. The same check guards restart, switch,
  version change and mod changes.
- The sidecar is the operator's to run, and so is its lifecycle. Foreman has
  no watcher, no warning for a sidecar restart and no automatic recovery. A
  game whose sidecar was restarted or replaced loses RCON; Foreman says it
  cannot reach the game, never claims a save it did not make, and the
  operator uses Force stop (no save) and starts the game again.
- The Tailscale reference setup uses kernel networking
  (`TS_USERSPACE=false`), a persistent state volume, `/dev/net/tun`,
  `NET_ADMIN` and `NET_RAW` on the sidecar only, an image pinned by tag and
  digest, and interactive sign-in or an auth key read from a file mounted
  into the sidecar alone. Userspace mode is untested and not promised.

## Consequences

- Accepted risk: the sidecar sees all game traffic and usually holds
  `NET_ADMIN`; Foreman does not police it (SPEC 14.16). The sidecar and
  every peer in its namespace are trusted. The game inherits access to
  additional networks, VPN peers and loopback services. Operators own those
  access rules and must not expose Docker through the sidecar. Foreman adds
  no scanner for namespace peers and no VPN firewall manager.
- Rejected: requiring a dedicated sidecar on only the game Docker network.
  Shared sidecars preserve self-hosters' network choices (Todd, 2026-11-14).
- Measured on Docker 29.1.3 with a game-image container as the namespace
  holder: restarting the holder keeps its ID and address, but the running
  game sees only loopback, with no RCON listener. Replacing the holder
  gives a new ID at the same address, and the game stays in the orphaned
  namespace with the old ID in its network mode. Docker accepts a
  `container:<ID>` create when the holder is stopped or removed and fails
  at start; Foreman refuses before creating anything and relies on neither.
- The game does not follow a sidecar restart by itself. The operator
  restarts the game after restarting the sidecar. Recovery after a host
  reboot, where Docker restarts the game and the sidecar in no guaranteed
  order, is not yet measured.
- Rejected: **a VPN on the host only, with normal port publishing**. No
  change needed and LAN play works, but Todd wants sidecars allowed.
- Not possible: **Foreman publishing the port on the sidecar**; Docker
  lets only a network's owner publish ports.
- Rejected: **a bind-address setting only**. Simpler, but the game could
  not share the sidecar's network, which is what makes the VPN carry it.
- Not built: **a sidecar watcher with automatic game restart**. SPEC 4.13
  keeps recovery manual and Epic 9b added none. A later epic may weigh it
  against the cost of Foreman restarting a game on its own.
