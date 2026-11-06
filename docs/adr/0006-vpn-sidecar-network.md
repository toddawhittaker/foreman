# 0006. Running the game inside a VPN sidecar's network

- **Status**: Accepted. Scheduled for Epic 9 (SPEC 18.9) and not yet built; until then Foreman refuses to start when `FOREMAN_GAME_NETWORK_CONTAINER` is set.
- **Date**: 2026-11-04
- **References**: SPEC.md sections 3.2, 4.4, 4.6, 4.13, 12.6, 14.1, 14.16, 18.9. Amends ADR 0003.

## Context

Some operators want players to join through Tailscale, Netbird or another
VPN sidecar container. It is optional: by default the game port is published
on the host and no VPN is needed. Foreman must not prevent a sidecar.

## Decision

- Optionally, the setting `FOREMAN_GAME_NETWORK_CONTAINER` names a running
  sidecar. When set, the game container uses Docker network mode
  `container:<sidecar>` and Foreman publishes no port. The game listens on
  UDP 34197 on every interface, so VPN players join through the VPN and LAN
  players through the port the sidecar publishes on the host, set in the
  sidecar's own definition. When that port is not published, the Server
  page warns that LAN players cannot reach the game; the start goes ahead.
- The sidecar must be on `FOREMAN_GAME_NETWORK` and is the one extra
  container allowed there. Foreman reaches RCON at the sidecar's address on
  that network, and RCON is bound only to that address
  (`--rcon-bind <address>:27015`), so VPN peers cannot reach it.
- Every range of the sidecar's networks counts as a game range (sign-in).
- A start is refused when the sidecar is missing, not running, on the host
  network, or off the game network. It is looked up by name at every start.

## Consequences

- Accepted risk: the sidecar sees all game traffic and usually holds
  `NET_ADMIN`; Foreman does not police it (SPEC 14.16).
- A sidecar restart may cut the game off until Foreman starts it again;
  the operator restarts the game after restarting the sidecar.
- Rejected: **a VPN on the host only, with normal port publishing**. No
  change needed and LAN play works, but Todd wants sidecars allowed.
- Not possible: **Foreman publishing the port on the sidecar**; Docker
  lets only a network's owner publish ports.
- Rejected: **a bind-address setting only**. Simpler, but the game could
  not share the sidecar's network, which is what makes the VPN carry it.
