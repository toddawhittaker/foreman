# 0006. Running the game inside a VPN sidecar's network

- **Status**: Accepted. Split out of Epic 9 into Epic 9b (SPEC 18.9b) and not yet built; until then Foreman refuses to start when `FOREMAN_GAME_NETWORK_CONTAINER` is set.
- **Date**: 2026-11-04
- **References**: SPEC.md sections 3.2, 4.4, 4.6, 4.13, 12.6, 14.1, 14.16, 18.9b. Amends ADR 0003.

## Context

Some operators want players to join through Tailscale, Netbird or another
VPN sidecar container. It is optional: by default the game port is published
on the host and no VPN is needed. Foreman must not prevent a sidecar.

## Decision

- Optionally, the setting `FOREMAN_GAME_NETWORK_CONTAINER` names a running
  sidecar. When set, the game container uses Docker network mode
  `container:<verified sidecar ID>` and Foreman publishes no port. The game listens on
  UDP 34197 on every interface, so VPN players join through the VPN and LAN
  players through the port the sidecar publishes on the host, set in the
  sidecar's own definition. When that port is not published, the Server
  page warns that LAN players cannot reach the game; the start goes ahead.
- The sidecar must be on `FOREMAN_GAME_NETWORK` and is the one extra
  container allowed there. Foreman reaches RCON at the sidecar's address on
  that network, and RCON is bound only to that address
  (`--rcon-bind <address>:27015`). This keeps RCON off the VPN interface,
  but operator-defined routes or forwarding can still reach that address.
- Additional Docker networks, including shared networks, are allowed.
  Their unrelated services are not refusal reasons. Check every attached
  Docker range for trusted-proxy overlap and configured Docker API/socket
  proxy exposure; unknown ranges fail closed.
- Every Docker range of the sidecar counts as a game range for sign-in,
  learned before first start and restored on Foreman restart. Docker does
  not reveal VPN ranges: never trust the sidecar's VPN address or a broad
  VPN range as a sign-in proxy.
- A start is refused when the sidecar is missing, not running, on the host
  network, in another container's namespace, or off the game network. It
  is looked up by name at every start; creation joins the inspected ID.

## Consequences

- Accepted risk: the sidecar sees all game traffic and usually holds
  `NET_ADMIN`; Foreman does not police it (SPEC 14.16). The game inherits
  access to additional networks, VPN peers and loopback services. Operators
  own those access rules and must not expose Docker through the sidecar.
- Rejected: requiring a dedicated sidecar on only the game Docker network.
  Shared sidecars preserve self-hosters' network choices (Todd, 2026-11-14).
- A sidecar restart may cut the game off until Foreman starts it again;
  the operator restarts the game after restarting the sidecar.
- Rejected: **a VPN on the host only, with normal port publishing**. No
  change needed and LAN play works, but Todd wants sidecars allowed.
- Not possible: **Foreman publishing the port on the sidecar**; Docker
  lets only a network's owner publish ports.
- Rejected: **a bind-address setting only**. Simpler, but the game could
  not share the sidecar's network, which is what makes the VPN carry it.
