# Foreman: specification

*Draft, 2026-11-02. Not yet approved. Grows out of docs/VISION.md.*

## 1. How to read this

1.1. This document says **what** Foreman must do. docs/VISION.md says
**why**, and wins on questions of intent. This document wins on questions
of detail. docs/STACK.md, once written, says **with what**.

1.2. Every requirement has a priority:

- **P0**: must have. Without it there is no first usable version.
- **P1**: should have. Foreman works without it, but not well. Part of v1.
- **P2**: wanted, not scheduled. Not part of v1 unless promoted.

1.3. Section numbers are addresses. Prompts, commits, tests and code
comments cite them ("SPEC 7.4"). Once this document is approved, a
section is never renumbered; a removed section keeps its number with the
word "Removed".

1.4. A line marked **Decision D*n*** depends on a choice not yet made.
Section 20 lists them. The requirement around it still stands; only the
mechanism is open.

## 2. Words used here

- **World**: a named directory holding one map's saves, mods, settings
  and metadata (section 5).
- **Active world**: the world the game container is currently running.
- **Game container**: the Docker container that runs the Factorio server.
  Foreman creates and owns it (section 4).
- **Game network**: the Docker network on which Foreman reaches the game's
  RCON port (4.4).
- **VPN sidecar**: an optional container the operator runs, such as
  Tailscale or Netbird, whose network the game shares so that players can
  join through a VPN (virtual private network) (4.13).
- **Snapshot**: a point-in-time copy of a world that can be restored
  (section 6).
- **Risky operation**: any operation that can break a world: a mod
  change, a game version change, a world switch, a settings change, a
  restore, or a delete.
- **RCON** (remote console): Factorio's network protocol for sending
  console commands to a running server.
- **Mod portal**: mods.factorio.com, where mods are downloaded from.
- **Built-in mods**: `base` plus the Space Age DLC (downloadable content)
  mods, which ship with the game and are never downloaded. For game 2.1
  the DLC mods are `space-age`, `quality`, `elevated-rails` and
  `recycler`; the list follows the game version (section 9.7).
- **Pre-start check**: the checks Foreman runs before every start
  (section 7.4).
- **Data directory**: the one directory where Foreman keeps everything:
  worlds, snapshots and its own database.
- **API token**: a secret that lets a program, such as an AI assistant,
  act as the user who made it, within a fixed scope (12.10).

## 3. Deployment

3.1. **One container.** Foreman ships as one container image, published
with an exact version tag. It needs no other service to run. The
image is published to GHCR (GitHub's container registry) as
`ghcr.io/toddawhittaker/foreman`. The image is built for `linux/amd64` and
`linux/arm64`, on the build machine's own architecture and cross-compiled,
so no emulation is needed. A release workflow builds both and, until Epic
11b (18.11b), publishes nothing. The game image for arm64 is experimental
upstream, so real-game tests run on amd64 only. (P0)

3.2. **Configuration.** Everything an operator sets at install comes
from environment variables. Any secret value can instead come from a file
named by the same variable with a `_FILE` suffix (for example
`FOREMAN_OIDC_CLIENT_SECRET_FILE`), so Docker secrets and sops work. When
both are set, Foreman refuses to start and says which one to remove. (P0)

These variables are read at start. A variable that is set but empty counts
as unset. Foreman reports every problem with them at once, in plain words.

| Variable | Default | Meaning |
|---|---|---|
| `FOREMAN_DATA_DIR` | `/data` | The data directory inside the container (3.3) |
| `FOREMAN_HOST_DATA_DIR` | discovered | The same directory's path on the host (3.5) |
| `FOREMAN_GAME_UID`, `FOREMAN_GAME_GID` | `845` | The game's user and group (3.6). Never 0 (14.11) |
| `FOREMAN_GAME_NETWORK` | none | The Docker network the game joins (4.4). Required when Foreman runs in a container, or when `FOREMAN_GAME_NETWORK_CONTAINER` is set |
| `FOREMAN_GAME_NETWORK_CONTAINER` | none | A VPN sidecar container whose network the game shares (4.13). Optional. Needs `FOREMAN_GAME_NETWORK` |
| `FOREMAN_LISTEN` | `:8080` | The address Foreman listens on |
| `FOREMAN_PUBLIC_URL` | none | The URL people use to reach Foreman. `https://` makes session cookies `Secure` (12.8), and its origin is accepted for form posts (14.7). Required when `oidc` is on, because the identity provider sends people back to it (12.7) |
| `FOREMAN_PUBLIC_ADDRESS` | none | The host name or IP address players type into Factorio, with an optional port (34197 when none is given). The Server page shows it as a join line (7.1). When it is blank the line is hidden: Foreman never guesses the address |
| `FOREMAN_AUTH_METHODS` | `local` | A comma-separated list of `local`, `headers` and `oidc`. Empty means `local` |
| `FOREMAN_AUTH_USER_HEADER` | none | Header that names the user. Required when `headers` is on (12.5) |
| `FOREMAN_AUTH_GROUPS_HEADER` | none | Header that lists the user's groups. Required when `headers` is on (12.5) |
| `FOREMAN_AUTH_REQUIRED_GROUP` | none | Group a user must belong to. Required when `headers` is on (12.5) |
| `FOREMAN_TRUSTED_PROXIES` | none | Address ranges whose headers are honoured. Required when `headers` is on (12.6) |
| `FOREMAN_OIDC_ISSUER` | none | The identity provider's issuer address, which must start with `https://`. Required when `oidc` is on (12.7) |
| `FOREMAN_OIDC_CLIENT_ID` | none | The client ID the provider gave Foreman. Required when `oidc` is on |
| `FOREMAN_OIDC_CLIENT_SECRET` | none | The client secret. Required when `oidc` is on. It may use its `_FILE` form |
| `FOREMAN_OIDC_REQUIRED_GROUP` | none | Group a user must belong to. Required when `oidc` is on: without it, anyone the provider signs in would become an admin (12.7) |
| `FOREMAN_SESSION_IDLE` | `168h` | Idle time before a session ends, as a Go duration (12.8) |
| `FOREMAN_FACTORIO_USERNAME`, `FOREMAN_FACTORIO_TOKEN` | none | The factorio.com account that downloads mods (9.11). Set both or neither: one alone is a startup error. Either may use its `_FILE` form |
| `FOREMAN_SECRET_KEY` | none | At least 32 characters; a shorter key is a startup error. It seals secrets an admin enters in the UI (9.11). Without it, an account cannot be stored in the UI. Changing it makes stored secrets unreadable, so they must be entered again. It may use its `_FILE` form |
| `FOREMAN_MCP` | none | `off` forces MCP access off (21.2). The setting then shows that the environment turned it off and cannot be changed. Any other value is a startup error |
| `FOREMAN_API` | none | `off` forces REST API access off (23.8), as `FOREMAN_MCP` does for MCP. Any other value is a startup error |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | The Docker socket, or a socket proxy |

A `tcp://` `DOCKER_HOST` is not encrypted and not authenticated. Use it
only for a socket proxy on the same host or on a Docker network shared
with Foreman, never across a network you do not control (14.2).

3.3. **The data directory** is one mounted path. It holds the worlds, the
snapshots, and the app database. Its layout is in section 5.2. (P0)

3.4. **The app database** holds only users, sessions, API token hashes,
app settings, notification settings, the audit log, which world is
active (a table of world ids, so that several could be active later), the
record of a switch in progress (7.10), and each world's unfinished mod plan
(9.14: the names of the mods picked, nothing else; ADR 0021). It never holds
other game data: saves, mods and game settings live only in world
directories. (P0)

3.5. **Host paths.** Foreman creates containers with bind mounts, which
the Docker daemon resolves on the host. Foreman must know the host path of
its data directory. It takes it from `FOREMAN_HOST_DATA_DIR`, or discovers
it by inspecting its own container's mounts. If neither works, Foreman
refuses to start and says so in plain words. (P0)

3.6. **File ownership.** Foreman runs as a non-root user. Everything it
writes into a world is readable and writable by the game's user and group,
which default to 845:845 (the `factoriotools/factorio` default) and are
configurable. Foreman's image runs as 845:845 by default, because a
non-root process cannot change a file's owner: whatever user Foreman runs
as owns every file it writes. Foreman refuses to start only when neither
its user ID nor its group ID matches the game's, because then the game
could not use what Foreman writes. When only the group matches, files
Foreman writes into a world are group-writable (files 0664, folders 0775).
Files under a world's `config/` are the exception: they are 0640, because
they can hold secrets (14.8), whether written by import, snapshot or restore.
Foreman's own database file is the exception: it is 0660, because it holds
password hashes and session hashes and the game has no need of it. The
game's user and group ID may not be 0: Foreman refuses to start and names
the setting, because the game would run as root (14.11). (P0)

3.7. **No downloads at startup.** Foreman never downloads anything when it
starts. It downloads only a game image or a mod the admin asked for, and
reads the mod portal, the game's release list and the list of game image
tags (10.7) when the admin opens a page that shows them. Starting the game
never downloads its image. When the local Docker holds no image under the
world's version tag (`factoriotools/factorio:<version>`, 4.2), the start is
refused and the Server page offers a separate "Download game image" button,
which pulls that exact tag as a visible step and is recorded in the audit
log (12.9). A switch the admin confirmed (7.9) downloads the new world's
missing image as its first step, visible and audited, before the switch
record is written and before the old game is stopped; a failed download
leaves the old world running and untouched. An import the admin ticked
the download for (5.5) downloads the same way, after the world is made.
(P0)

3.8. **Upgrading Foreman never touches a running game** and never rewrites
a world's files without a snapshot first. A Foreman upgrade that changes
the data directory layout migrates it only after snapshotting every
affected world. (P0)

3.9. **Works on a phone.** Every page is usable at 320 CSS pixels wide
(section 17). (P0)

## 4. The game container

4.1. Foreman creates and owns the game container. There is no other
runtime mode. (Adopted mode, which drove a container someone else
defined, was dropped; ADR 0005.) (P0)

4.2. **Rely on the image as little as possible.** The
`factoriotools/factorio` entrypoint changes between image versions (save
selection, missing-save handling and shutdown all changed in 2026), and it
fails when run as any user but its own. So Foreman does not run it. The game
container runs the game binary, `/opt/factorio/bin/x64/factorio`, directly,
as `FOREMAN_GAME_UID:FOREMAN_GAME_GID`, with Foreman's own flags (4.3).
Foreman depends on the image only for that binary and the game's own data.
A small shell wrapper in the container's command reads the RCON password from
the world's file (4.5), so the password is never in the container's
settings.

A world's game version is an exact patch release of a supported line
(10.8): 1.1.x, 2.0.x or 2.1.x, for example `2.0.77`. Its image is
`factoriotools/factorio:<version>`, referenced by that version tag, not
by digest (ADR 0005). **Accepted risk:** upstream can rebuild a tag (2.1.24
was), so the image behind a version number can change when it is
downloaded again; Foreman uses whatever image the local Docker holds under
that tag and downloads only when the admin asks (3.7). A save made by a
version outside the supported lines is refused at start (7.4). Foreman
makes new maps itself (5.6) and never relies on the image generating one.
It never downloads the image when it starts a game (3.7). (P0)

4.3. Foreman creates the game container from the image for the active
world's game version (4.2). Its fixed parts are:

- **Name and labels.** The name is `foreman-<world-id>`. Two labels under
  `io.github.toddawhittaker.foreman.` hold the world id (`world`) and the
  install (`install`, the host data path). Foreman finds its containers by
  label after it restarts, with no database row. A container with that name
  that Foreman did not create for this world and install is never touched:
  Foreman refuses to start and says to rename or remove it.
- **Command.** `--start-server-load-latest`, so the game loads the newest
  save and Docker's own restart after a crash loads the newest autosave
  (7.11); `--mod-directory /factorio/mods`; RCON on port 27015 (bound to
  one address with a VPN sidecar, 4.13); `--server-settings` when the
  world has `config/server-settings.json`; `--server-adminlist` and
  `--server-banlist` always, so `/promote` and `/ban` made in game persist
  in the world's `config/`; and, only when the world's switch "Only
  whitelisted players can join" is on, `--use-server-whitelist` and
  `--server-whitelist` together. With the switch off Foreman passes
  neither: the game refuses `--server-whitelist` alone, and an in-game
  `/whitelist enable` is lost when the game restarts, so the switch is the
  only thing that makes the whitelist count (8.3).
- **Create step.** A world made by Generate or a fresh-map Clone has no
  save and carries `new_map` in `world.json` (5.1). While `new_map` is set,
  Foreman gives the container the environment variables `FOREMAN_NEW_MAP`
  and `FOREMAN_WORLD_ID`, and `FOREMAN_PRESET` and `FOREMAN_SEED` when the
  world has them. The wrapper script then runs the game once with
  `--create saves/<world-id>.zip`, the world's mod folder and, as the world
  has them, `--preset`, `--map-gen-seed`, `--map-gen-settings` and
  `--map-settings`, and only afterwards starts the server. The script
  creates the map only when `saves/` holds no zip, so Docker's own restart
  of a container that still carries the variables can never overwrite a
  save. The values reach the script as environment variables, never in its
  text. A create that fails exits the container, and the failed-start report
  shows the game's own words (7.3). Foreman clears `new_map` when that start
  reaches the in-game state, so the next start loads the save.
- **Restart policy** `unless-stopped`, so the game comes back after a reboot
  whether or not Foreman is running. A stop that Foreman or Docker makes
  leaves the container stopped.
- **Standard input open, no terminal.**
- **Port.** UDP 34197, published on the host, except when the game runs
  in a VPN sidecar's network (4.13). The port is fixed for now; a
  per-world port is an additive `world.json` field for later (5.3).
- **Logs and stop.** A rotating `json-file` log (10 MB, 3 files) and a stop
  timeout of 120 seconds.
- **One bind mount**: the world's directory at `/factorio` (4.6). The
  game's own write folder stays inside the container, so it is lost when the
  container is recreated.

Every start Foreman makes removes the old stopped container and creates a
new one, so settings never drift. Docker's own restarts never recreate it.
Switching worlds, or changing a world's game version, also starts a new
container with the new mount or image. This allows one container per world
later, each on its own port, with no change to the layout (5.3). (P0)

4.4. The game port (UDP, 34197) is published on the host, except with a
VPN sidecar (4.13). The RCON port is never published; Foreman reaches it at
the game container's address (or the sidecar's, 4.13), read from Docker,
on a Docker network shared only by Foreman and the game container. The
operator names that network with `FOREMAN_GAME_NETWORK`. Foreman never
creates or connects networks (14.3). (P0)

- Outside a container (local development and tests) the setting is
  optional, unless a VPN sidecar is set (4.13), and the game joins
  Docker's default `bridge`.
- Inside a container the setting is required; there is no automatic pick.
  Foreman refuses to create a game, and says what to change, when:
  - it cannot find its own container (by ID or hostname) while it can tell
    it runs in one, because a `/.dockerenv` or `/run/.containerenv` file
    exists or `DOCKER_HOST` is `tcp://`;
  - Foreman is not on the named network, or the name is `host`, `none` or
    `container:...`;
  - any container other than Foreman, this install's own labelled game
    containers and the VPN sidecar named by
    `FOREMAN_GAME_NETWORK_CONTAINER` (4.13) is on the game network;
  - `DOCKER_HOST` is `tcp://` and does not resolve into one of Foreman's
    networks other than the game network, so a socket proxy is always
    reached over a network the game is not on;
  - the game network's address range, IPv4 or IPv6, read from Foreman's own
    container, overlaps `FOREMAN_TRUSTED_PROXIES`, because a game could
    then pose as the proxy that signs users in (12.5).
- These checks run when a game is created, not while it runs. A refusal
  never stops Foreman starting or serving pages (7.1). Known limits:
  - a socket proxy that also publishes a port on the host cannot be seen
    from inside Foreman, so the operator guide says never to publish it;
  - a runtime that creates neither marker file, reached over a `unix://`
    socket, gives Foreman no sign that it is in a container;
  - Foreman cannot see how the proxy reaches it. When the proxy runs on the
    host, publish Foreman's port on `127.0.0.1` only, never `0.0.0.0` or a
    LAN address. `FOREMAN_TRUSTED_PROXIES` is then the single address the
    proxy's connections arrive from, as a `/32` (with Docker's port
    forwarder, the gateway of the network that carries the published
    port), never a whole bridge range, and that network must not be one the
    game is on. Better still, do not publish Foreman's port: run the proxy
    in a container on a network Foreman shares and the game does not.

4.5. Foreman generates and owns the RCON password. It is created when
missing, as a random value in the world's `config/rconpw`, readable by the
game's group (mode 0640, written atomically), and never shown. Snapshots
copy it like any other world file; restore and startup recovery narrow any
copy found wider than 0640. Docker's settings never hold it. **Accepted
risk:** Factorio takes the RCON password only on its command line, so it is
visible in the game process's arguments, to anyone who can read process
details on the host. The operator guide advises mounting `/proc` with
`hidepid=2` on the host, and 4.4 keeps the port itself off every network but
Foreman's and the game's. (P0)

4.6. The game container's shape is fixed by Foreman: it runs as the
configured non-root user, with no privileged mode, no host network (with
a VPN sidecar, it shares only that sidecar's network, 4.13), all
capabilities dropped and `no-new-privileges` set, and bind mounts only from
inside the data directory. The game container is never given the data
directory's root, only its world's directory, because the app database
lives at the root. Nothing an admin types in the UI changes that shape
except the published game port. (P0)

4.7. (Removed: adopted mode was dropped; ADR 0005.)

4.8. (Removed: adopted mode was dropped; ADR 0005.)

4.9. (Removed: adopted mode was dropped; ADR 0005.)

4.10. (Removed: adopted mode was dropped; ADR 0005.)

4.11. (Removed: adopted mode was dropped; ADR 0005.)

4.12. (Removed: adopted mode was dropped; ADR 0005.)

### VPN sidecar

4.13. **A VPN sidecar (optional).** Optionally, the game can run inside
another container's network, such as a VPN sidecar (Tailscale, Netbird or
another VPN), so that players join through the VPN. This is one choice an
operator may make, never a requirement. By default, with
`FOREMAN_GAME_NETWORK_CONTAINER` unset, nothing here applies: Foreman
publishes the game port on the host (4.3, 4.4) and needs no VPN. Foreman
must not prevent a sidecar (ADR 0006). Built in Epic 9b (18.9b). (P1)

When an operator sets one:

- The optional setting `FOREMAN_GAME_NETWORK_CONTAINER` names a running
  container, the sidecar, which the operator defines and runs. When it is
  set, Foreman creates the game container with Docker network mode
  `container:<verified container ID>`, so the game shares the inspected
  sidecar's network. A replacement between inspection and creation must not
  silently become the game's sidecar, and a failed create or start reports
  its failure and never falls back to another network. The game container
  declares neither exposed nor published ports in this mode (Docker refuses
  them), so Foreman never publishes the game port itself. Every path that
  starts a game runs the sidecar checks first, before any world change:
  start, restart, world switch, version change and mod changes.
- The game listens on UDP 34197 on every interface of the shared network;
  only RCON is bound to one address (below). LAN players and VPN players
  can play at the same time: VPN players reach the game through the VPN,
  and LAN players through the game port the sidecar publishes on the host.
  The operator sets that port in the sidecar's own definition, because
  Docker lets only the container that owns a network publish its ports,
  so Foreman cannot publish it.
- At every start, and when Foreman takes on a game that is already running,
  Foreman inspects the sidecar. When UDP 34197 has no host binding, the
  Server page shows, while the game runs, a warning, "LAN players cannot
  reach the game: <sidecar> does not publish UDP 34197 on the host.", and
  the start goes ahead, because VPN-only play can be a deliberate choice.
  The warning is evidence from inspection, not a connectivity probe, and it
  is not refreshed while the game runs: a published port does not prove LAN
  players can reach it (the host address it is bound to, a changed host
  port and the firewall are outside this publication check). It is worded in
  all eight languages and is not announced to screen readers.
- The sidecar must be on `FOREMAN_GAME_NETWORK`, which is then required
  even outside a container. The sidecar is the one container other than
  Foreman and its games allowed on the game network (4.4). Foreman reaches
  RCON at the sidecar's address on the game network. The operator may also
  attach the sidecar to other Docker networks, including shared networks;
  those attachments and unrelated services there are not refusal reasons.
  The game inherits access to those networks (14.16).
- RCON is bound only to the sidecar's IPv4 address on the game network,
  with Factorio's `--rcon-bind <address>:27015`, never to `0.0.0.0`. The
  address reaches the game as the environment variable `FOREMAN_RCON_BIND`
  and is a value the start script passes as one argument, never shell text.
  This keeps RCON off the VPN interface, the sidecar's other Docker
  interfaces and loopback; it does not prevent access through an operator's
  subnet route or forwarding rule to that private address (14.16).
- One path finds the RCON address: the first connection, every reconnect,
  and taking on a running game after a Foreman restart. It reads the bind
  address from the game container Foreman created, and trusts it only
  when the container the game's network mode names, looked up by that ID
  and never by the configured name, still exists and has exactly that
  address on the game network, with port 27015. A forged value, a network
  mode that names a container instead of an ID, or a holder that is gone or
  replaced prevents an RCON login. The same check runs on every new
  connection after it is made and before the RCON password is sent. When
  Docker reports the original holder gone or its address changed, the login
  is refused. This is Docker's word at one moment:
  it narrows, but cannot close, the time between the check and the login.
- Every address range of every Docker network the sidecar is on counts as a game
  range for trusted-header sign-in (12.5, 12.6). A start is refused when
  any of them overlaps `FOREMAN_TRUSTED_PROXIES`, as for the game network
  (4.4). Check every attached Docker network for exposure of the configured
  Docker API/socket proxy too (a range that holds the address a `tcp://`
  `DOCKER_HOST` reaches). Unknown ranges fail closed: a range Docker does
  not report refuses the start. Foreman reads the sidecar's ranges before
  accepting trusted headers, on every monitor look, whether Foreman runs in
  a container or on the host, and they count as known only when the
  sidecar and every game container were read in full. An empty network list
  on the game is not evidence that its ranges are known. Ranges learned
  earlier are kept when a later read fails; a game still running in a
  since-replaced sidecar's old namespace contributes its ranges too. Before
  the first complete discovery, unreadable ranges keep trusted-header sign-in
  refused. Later read failures retain previously learned ranges and knowledge.
- A start is refused, with a plain reason, when the sidecar does not
  exist, is not running, uses the host or another container's network,
  is not on the game network, or has an address range Foreman cannot read,
  that overlaps the trusted proxies or that holds the Docker API's address.
  The reasons are worded in all eight languages and identify the configured
  container. Network diagnostics may include inspected addresses and ranges,
  but never the sidecar environment or an inspect dump. Foreman recreates the
  game container at every start (4.3), so it looks the sidecar up by name at
  every start and attaches the game to the sidecar as it is then.
- Known limit: Foreman has no sidecar watcher, warning or automatic
  recovery. The sidecar is the operator's, and so is its lifecycle. When it
  is restarted (same ID, same address) or replaced (new ID) while the game
  runs, the game stays running in a namespace that no longer has its RCON
  listener, or in the old orphaned one, and the monitor keeps showing it
  running. Console, players, chat and stop then say Foreman cannot reach the
  game over RCON, and a graceful stop or restart refuses and never claims
  the world was saved. The operator uses Force stop, which stops the game
  without saving, then starts it from Foreman, which joins the sidecar as it
  is then. This applies after a Foreman restart too. The README and the
  operator guide say to restart the game from Foreman after restarting the
  sidecar.
- Not measured: recovery after a host reboot or a Docker restart, with
  Docker restarting the game (4.3) and the sidecar in no guaranteed order.
  Operators check it on their own host (STATUS, Epic 9b).

The sidecar is a container Foreman did not create; 14.16 states the risk
this accepts.

## 5. Worlds

### Model

5.1. A world is a directory holding:

- `saves/`: the save files;
- `mods/`: mod zips, `mod-list.json` and `mod-settings.dat`;
- `config/`: `server-settings.json`, `map-gen-settings.json`,
  `map-settings.json`, `server-adminlist.json`, `server-banlist.json`,
  `server-whitelist.json`, and `rconpw` (4.5);
- `world.json`: Foreman's metadata for the world: display name, pinned
  game version, Space Age flag, which save to load (section 7.11; if
  missing, the newest save), whether the game enforces the whitelist
  (`use_whitelist`, 8.3), the time it was created, and, for a world
  imported from an inbox folder, `imported_from`: the folder, the newest
  save's name, size and modification time (to the nanosecond), and the
  time of the import (5.5). A world that has no save yet also holds
  `new_map`, such as `{"preset": "rich-resources", "seed": 123}`, where
  either part may be missing. It lets the next start create the map (4.3,
  5.6); Foreman clears it once that start reaches the in-game state.
  It also holds the factory map mode, `factory_map` (5.16), when one was
  set;
- `players.json`, when present: Foreman's record of the players of the
  save the world loads (8.14). Foreman writes it without a snapshot, as it
  does `world.json`.

A person can read, back up and move a world without Foreman. `world.json`
is plain JSON with snake_case keys and a `format_version` (1 today). Fields
are only ever added. Foreman keeps fields it does not know when it
rewrites the file. A world whose `format_version` is newer than Foreman
knows is listed but cannot be changed, and the page says a newer Foreman
wrote it. (P0)

5.2. The data directory holds `worlds/<world-id>/`, `snapshots/`,
`favorites/` (5.12) and the app database. A world's id is a
short, safe directory name that never changes; its display name can. The
id is made from the name (lowercase letters, digits and hyphens, at most
40 characters, with `-2`, `-3` and so on when taken), and an id is never
reused, even after the world is deleted. Two worlds may not share a name,
ignoring case. (P0)

5.3. Exactly one world is active at a time in v1. Nothing in the data
model, the API or the on-disk layout may assume only one could be: each
world has its own settings and port, and the active world is recorded as a
field, not implied by a path. (P0)

### Operations

5.4. **List** worlds with name, game version, Space Age flag, last played
time, disk use, and which one is active. Under disk use each world shows
what its snapshots add (6.6), so an admin can see which worlds hold the
space. There is no total column. (P0)

5.5. **Import** an existing directory of saves and mods (for example a
hand-renamed `saves-2025` folder) as a new world, or upload a save zip and
sync its mods from the portal (section 9.8). Import copies; it never moves
or changes the source. Foreman reads a folder to import only from the inbox,
`<data>/import/`, never from a path the admin types. The Import page names
the inbox's path on the host (3.5) when Foreman knows it. It reads save zips
from the folder's top level and from its `saves/` folder, so a Factorio
server's own layout imports as it is. It copies `mods/` and the `config/`
files that 5.1 lists, except `rconpw`: Foreman makes its own (4.5). Other
files are not copied. A save name found both at the top level and in
`saves/` makes the folder not importable, and the message names the file.
A save zip can also be uploaded to make a new world. A world file
exported by Foreman is imported on this page too (5.14). (P0)

Import also does the following.

- **Several folders at once.** The admin can tick several folders. With one
  ticked, the name, game version and Space Age fields apply. With several,
  each folder becomes a world named after the folder, with the game
  version and Space Age setting read from its save; a folder whose save
  cannot be read, or that is refused (a name taken, a clash), is skipped
  with its reason and the others go ahead. A skipped folder an import on
  its own can fix links to that. A bulk import is several single imports.
- **Already imported.** Folders whose newest save is already in a world are
  listed apart, naming that world and when, and can still be imported. A
  folder counts as already imported when its folder name and its newest
  save's name, size and modification time (to the nanosecond) all match
  the `imported_from` record in a world's `world.json` (5.1). Worlds
  imported before the record existed, and uploads, are never matched. A
  newer or touched save makes the folder new again.
  Folders that cannot be imported are listed apart, each with its reason.
- **The game image.** When Foreman has server control, an import can also
  download the game image its version needs if this host lacks it
  (ticked by default). The download is the world's own image download
  (3.7), started after the import, one per missing version. The
  folder list says which folders' images are not downloaded.
- **Space Age** is refused for a game version before 2.0, in the form and
  in the import itself. The form warns when the choice differs from what
  the save says.
- **The game version** defaults to the save's. A version the admin types or
  picks instead must be an image already downloaded or a version on
  Docker Hub's list (10.7). When Docker Hub cannot be reached and the image
  is not downloaded, the import goes ahead and the new world's page warns
  that the version could not be checked.
- **The suggested name** is the `name` in the folder's
  `config/server-settings.json` when readable, else the folder name. Nothing
  else is read from that file for this purpose.
- **Reading the page** takes only each world's `world.json`; it does not
  measure disk use. (P0)

5.6. **Generate** a new world from map settings (a preset, changed or
not) or a map exchange string, with Space Age on or off. (P0)

- **A guided flow in three steps** (Todd's ruling, 2026-11-21, #775). Each
  step is its own page, and a step bar of three links above each one names
  them and marks the current one. On step 1 the bar is there too, with
  step 1 current; steps 2 and 3 show as plain text, not links, until the
  world exists.
  1. **Make the world**: name, game version, Space Age, optionally a
     favorite (5.12), and the image download. This writes the world folder
     and `world.json` with `new_map`, and the world counts as generated.
     It always lands on step 2.
  2. **Mods**: the world's Mods tab, or the review filled from the
     favorite's mods when it has some (5.12, 9.14). "Skip to map settings"
     goes on without mods.
  3. **Map settings and preview**, at `/worlds/{id}/new-map`, with every
     planet the world's mods give it (below). "Save map settings" writes
     the settings files below. It takes no snapshot, because the world has
     no save. It is refused once the world has a save, and while the
     world's server is starting, running, stopping, crashed or crash-looping,
     because the first start makes the map before any save exists. It is
     also refused when the server's state is unknown because Docker cannot
     be reached. That check runs under the world's lock, so a start cannot
     slip in between.

  The first start then creates the map. Everything below about the map
  form, the leave dialog and the preview belongs to step 3, and "the
  Generate button" means its save button.
- **Unfinished worlds.** A world with `new_map` and no save is shown as
  "Unfinished" on the Worlds list and on its page, with a Continue link
  that opens where work remains: step 2 while the world has mod picks not
  yet installed (9.14), otherwise step 3. It is derived, not stored. Foreman never deletes
  one by itself; Delete works as for any world (5.10).
- **Planets come from the world's mods** (#775). Step 3 offers the planets
  and autoplace controls that the world's own game image and mods define,
  read by the mod-settings run (9.6). That list replaces the embedded one,
  because mods change the built-in planets too. Each control offers only
  what the game gives it (richness, an on/off box). A control that two
  planets share is one value, shown under each planet with "Also changes
  *planet*". Names are the game's localised names in the page language,
  with rich-text tags removed, cut to 100 characters and marked
  `translate="no"`. The planet file is untrusted and checked under 14.6.
  When it is refused or the run fails, step 3 uses the embedded planets
  and says why. Without script, step 3 waits on the server for the run,
  within its limit (14.3); with script the page shows at once and the form
  follows. On 1.1 there are no planets, and modded resource controls sit
  on the one surface. When the mods change after map settings were saved,
  step 3 works from the current mods: settings for planets or controls
  that are gone are listed as "no longer in the mods" and dropped at the
  next save, and new planets and controls start at their defaults. The
  pre-start check refuses a start the game would fail (7.4).
  - **One entry point.** Pages and tools get the planet list only through
    the controller's `WorldCatalogue`. A world whose enabled mods are all
    built in gets the embedded list with no game run. A run that failed or
    was refused gives the embedded list and the reason.
  - **Foreman's names win.** A planet or control Foreman already knows
    keeps Foreman's own translated name; a mod's label is used only for a
    name Foreman does not know.
  - **A planet's own default.** A planet may set its own default for a
    control, kept when it is a finite number from 0 to 1000 (Moshine's
    steam_geyser richness is 150, shown as 15000%). The select starts there
    and shows that value as its own entry when it falls between the steps.
    It is written back unchanged unless the person moves that control. A
    control two planets share is written once; a later planet's copy wins
    only when the person moved it.
  - **Saving needs the world's own list.** While the embedded list stands
    in for a world that has mods of its own, step 3 and `set_new_map` refuse
    to save, because
    saving would drop every setting of the mods' planets. The page offers a
    retry and a link to the Mods tab.
  - **Modded resources** are named with their resolved names, but have no
    map colour, because nothing draws them.
- **Nothing is made until the first start.** Generate writes only the world
  folder, `world.json` with `new_map` (5.1) and the settings files below.
  The world has no save. The world's first start runs the game's own
  `--create` step (4.3) and clears `new_map`; the pre-start check lets a
  world with `new_map` and no save through (7.4). Generate does not write
  `mods/mod-list.json`; the start step already makes it. The game version
  and Space Age choices work as in Import (5.5): Space Age is refused
  before 2.0, and the image can be downloaded with the world.
- **Two ways to make the map**, both in step 3: the map settings form, or
  "Fill from a map exchange string", which decodes the string and fills
  the form from it. Saving then writes both files
  (`map-gen-settings.json`, `map-settings.json`). A string from another
  game line is refused with a message to change the world's version.
- **The string is offered back.** A world's page and each favorite show the
  map exchange string with a "Copy map exchange string" button and a note
  that it carries map settings only, not mods. It is made from the map files
  by Foreman's own encoder for 1.1, 2.0 and 2.1: the payload matches the
  game's byte for byte, the compressed text does not (the game reads
  either). A world with a random seed, or without map files, takes it from
  its newest save's `level-init.dat`, read in Go: it is the map as the game
  created it, so a later script change to the map settings is not in it. It
  matches the game's own `get_map_exchange_string()` byte for byte on
  1.1.110, 2.0.77, 2.1.23 and 2.1.24. A save of another patch is accepted
  only when the result decodes in full and the surface's seed, width and
  height match the header's; otherwise the button is disabled with the
  reason. RCON is never used for it, because a Lua command turns achievements
  off for good. At most 2 saves are read at once, a result is kept per save
  file (path, size, modified time), and the string never goes in an address.
- **One form for map settings.** Choosing a preset fills every tab from
  it. Once any field differs from the preset, the form shows the game's
  own word "[modified]". **One "Reset to defaults"** for the whole world,
  shown while a field differs or a preset other than the default is
  chosen, returns the preset and every map setting to the default
  preset of the chosen version and Space Age choice, keeping the seed,
  and the preview redraws. It asks no confirmation: it changes nothing
  saved, so 17.5 does not apply, and "Undo reset" takes its place with
  the focus until the next change, sending every value it threw away
  back (17.9). Without script the reset is a plain round trip with no
  Undo. The tabs
  are the game's: Resources, Terrain, Enemy and Advanced; without script
  they stack under their headings. A hidden `filled_from` field names the
  preset the fields were filled from. When it differs from the chosen
  preset (a page without script), the page comes back filled from the new
  preset with a note, and nothing is made. "Random seed" fills a seed from
  0 to 4294967295 in the browser. A blank seed is filled with a random one
  that the form keeps, so the world matches the preview.
- **Leaving with unsaved settings** asks. A plain click on a link of the
  page, or the submit of a form other than Generate's (language, Sign
  out), opens Foreman's own dialog, a modal `dialog` (17.15) that names
  what is lost and offers "Stay on this page" and "Leave without saving".
  Stay or Escape closes it and puts focus back on what was used. Reload,
  closing the tab, the address bar and Back still get the browser's own
  leave-page question (`beforeunload`), which stays quiet once Leave was
  chosen; a link opened with a modifier key, a download, a link with a
  target, a same-page `#` link and the picture links that open the large
  view are not caught. Once a redraw of the form (a version, Space Age, preset or
  reset change, or Preview map's seed) has replaced fields that held
  unsaved edits, the page counts as unsaved until it is left or the world
  is made, even if the edits are later changed back, because asking once
  too often is safer than losing settings silently.
- **Steps as sliders.** Where a step setting shows as a slider (DESIGN
  8.7), its default step ("100%", or "0" for a bias) sits at the middle of
  the track, however many steps lie either side, so equal moves from the
  middle mean equal changes of kind. A click takes the nearest step; a key
  press or a screen reader's increment always moves one step. A setting
  with no default step, or a default at an end, uses the plain track. The
  select and the values sent do not change.
- **What is written** (ADR 0010). An unchanged preset is passed to
  `--preset`, with the seed in `--map-gen-seed`, and no file is written.
  Any change writes both files complete and passes no `--preset`, because
  the game lets a preset win over the files. The map-settings file is the
  example file from that exact version's image, with the preset's values
  as the game applies them, plus the form's changes. It is always
  complete, because the game refuses a `map-settings.json` that lacks a
  key its version knows. Both built files are checked against the game
  line's key types and ranges, as the raw editor checks (11.2), before
  they are previewed or written.
- **Untested layouts.** The map-settings layout changed at 2.1.13 and
  2.1.18, and no tested image covers 2.1.13 to 2.1.17. On those versions
  the Enemy and Advanced tabs offer no map-settings fields and say why.
  A changed default preset writes the map-gen file alone, which the game
  pairs with its own default map settings. A changed other preset is
  refused on the form, with what to do instead.
- The presets are, on 1.1: default, rich-resources, marathon,
  death-world, death-world-marathon, rail-world, ribbon-world and island;
  2.0 and 2.1 add lakes. These names, the planets, the autoplace
  controls, the map types and each version's map-settings file are read
  from each game image when the project is built and embedded in
  Foreman; nothing is read or downloaded at startup. The embedded planets
  and controls are used only where a world's own list is not available
  (above).
- **Map-gen fields** (Resources and Terrain, plus the starting area, No
  enemies and Peaceful mode on Enemy, and width and height on Advanced).
  Every autoplace control's frequency, size and richness for each game
  line and, on 2.x, each planet, as a choice of the game's own steps,
  plus seed, water and cliffs. Foreman refuses a form the game would
  refuse: a frequency of none, for example, is not offered.
  - A resource the game lets you switch off has an on/off box. Off writes
    size 0, the game's own off value; frequency 0 is not off (2.1.24
    crashes on it). A post that lacks the box keeps the resource on.
  - **Cliffs off.** Cliffs have the same on/off box where the game lets
    them be switched off: Nauvis on every line, and Gleba and Fulgora on
    2.x; Vulcanus has no cliff control. Off writes
    `cliff_settings.richness = 0` on 1.1, and
    `autoplace_controls.<planet>_cliff.size = 0` on 2.x. On 2.x
    `cliff_settings.richness` is never written as 0, because there it
    removes cliffs on every planet. These keys were proved by counting
    cliffs in real games on 1.1.110, 2.0.77 and 2.1.24. Cliffs off
    disables no achievement.
  - **One planet open at a time.** With several planets, each tab shows
    one planet's settings open at a time, the same planet in every tab
    and over a round trip, or the planet with the first error. The
    preview follows the open planet, and a planet the large view moves
    to opens its settings, so the person sees the planet they are
    changing; the preview has no planet picker of its own.
  - Nauvis has a **map type**: Nauvis elevation (the default), Lakes and
    Island on 2.x; Normal and Island on 1.1. The default writes no
    `elevation` key. Other planets keep their own.
  - **Moisture and terrain type** each have a scale, 12 steps from 17% to
    600% (the file holds the inverse), and a bias, -0.50 to 0.50 in steps
    of 0.05, written only when it is not 0. The game's data does not list
    these steps; they are the game's own map generator screen's, read on
    1.1, 2.0 and 2.1 on 2026-11-06.
  - A raw JSON box covers the rest (11.5); the fields win for every key
    they cover.
- **Map-settings fields** (Enemy and Advanced) are the ones the version's
  file holds, in the units the game's screen shows. Each number takes a
  whole number within the game screen's own ends (read on 2.1.24 and
  1.1.110):
  - Enemy expansion: minimum expansion distance 1 to 20 chunks, maximum
    2 to 20; minimum cooldown 1 to 60 minutes, maximum 5 to 180; on 1.1
    and 2.0, minimum group size 1 to 20 and maximum 1 to 40; on 2.1,
    evolution group size factor 1 to 20 in place of the group sizes.
  - Evolution: time factor and pollution factor 0 to 1000 (stored × 10⁻⁷),
    destroy factor 0 to 1000 (stored × 10⁻⁵). They show as plain numbers,
    as the game shows them, and each setting's tip (17.14) says the scale.
  - Technology: price multiplier 1 to 100000 (the game screen's lower end; the screen shows no upper end, so 100000 is a practical cap).
    On 1.1 also recipe difficulty and technology difficulty (Normal or
    Expensive) and research queue (After victory, Always or Never).
  - Pollution: diffusion ratio 0 to 25%, absorption modifier and attack
    cost modifier 10 to 400%, minimum to damage trees and absorbed per
    damaged tree 0 to 9999.
  - Asteroids (2.x): spawning rate 10 to 400%.
  - Spoiling (2.x): **spoiling rate** 10% to 1000%, shown as the game
    shows it, which is 100 divided by the stored `spoil_time_modifier`
    (checked in a real game: a rate of 200% halves spoil times).
  - Maximum expansion distance must be above the minimum; maximum cooldown
    and maximum group size must be at least their minimum. When a change
    pushes one of a pair past the other, the browser moves the other field
    to fit, as the game does, and says so. The server refuses a bad pair,
    with the error on the maximum, in case the browser did not.
- **Achievement warnings.** A table per game line in `internal/mapgen`,
  built from the game's own text, says which settings disable
  achievements (on 1.1, enemy base frequency or size below 100% and
  peaceful mode; 2.x adds no enemies, a larger starting area, more trees,
  and pollution, evolution or expansion turned off or eased). A short
  warning shows under each setting as soon as it crosses the line, and
  one summary of the game's reasons sits by the Generate button. The
  server works out the same answer from the files it would write.
  Exchange strings get no warning.
- **Needed-resource warnings.** While iron ore, copper ore, stone, crude
  oil or coal is off on Nauvis, a warning under its box and a summary by
  the Generate button say the game needs it unless a mod provides it.
  Every line needs all five (plastic needs coal); uranium is not needed.
  It is a warning, not a refusal, because a mod may supply the resource
  and Foreman does not inspect mods for it.
- **Decision D9 (settled 2026-11-06, ADR 0009): Foreman decodes the
  exchange string itself, in Go**, for the 1.1, 2.0 and 2.1 lines
  (`internal/mapexchange`). It is a port of rfvgyhn's
  factorio-exchange-string-parser (MIT licence; its notice is kept in the
  package). The game's binary layout is undocumented and has changed
  within a line, so the decoder refuses what it cannot read exactly rather
  than guess:
  - the string may carry both `>>>` and `<<<` markers or neither; exactly
    one marker is refused as a cut-off paste. Spaces and line breaks
    anywhere are ignored. The limits are in 14.6;
  - it must be base64 and zlib, use every byte, and match its checksum;
    values must pass structural range checks (finite numbers, true or
    false bytes that are 0 or 1, UTF-8 names that do not repeat). These
    are not the game's numeric bounds: the game still has the last word at
    create;
  - a string from a patch newer than any the decoder was tested against
    (it was tested on 1.1.110, 2.0.66, 2.0.77, 2.1.9 and 2.1.24) is accepted
    only when it reads cleanly by every rule above, is on a known line and
    is not newer than the world's game version;
  - the string must be from the world's game line, and its settings layout
    must be the world's. The game refuses a `map-settings.json` that lacks a
    key its own version knows, and the layout changed at 2.1.13 and 2.1.18.
    A string whose layout differs is refused with the versions that would
    take it;
  - a refusal names the string's version and the reason, and never quotes
    the string.
- **Map preview (P1).** Step 3 shows a picture of the map, one for each
  planet in the world's list: without mods, Nauvis only on 1.1 and on 2.x
  without Space Age, and Nauvis, Vulcanus, Gleba, Fulgora and Aquilo with
  Space Age. The picture comes from the game's own
  `--generate-map-preview` (with `--map-preview-planet` on 2.x), run in a
  locked-down one-off container (14.3) that loads the world's mods (ADR
  0033); the headless game cannot take a screenshot. It is drawn from exactly the settings Generate would write,
  square, at a size of 1024 to 3072 tiles in steps of 128, 1280 by
  default; any other size is a field error (on 2.x one pixel is one tile,
  and the game has no closer scale, so zooming scales the picture with
  sharp pixels). The steps keep the control a short list. The most is
  the demolisher territory cap below, so every size offered can show
  them; 4096 was measured to draw every planet within the container's
  limits (14.3), but its territories ran out of memory.
  Previewing is optional. It uses only an image already downloaded, and
  otherwise says the image must be downloaded first; it never downloads
  one (3.7).
  - **It draws by itself** with script: on page load, and 0.8 seconds
    after the last change to anything that changes the map. The newest
    request wins; a change cancels the one on its way, and Foreman stops
    its run. On a world with several planets the preview offers **One
    planet** (the default: one large picture of the planet whose settings
    are open, so opening another planet's settings draws that one) or
    **All planets** (every planet as thumbnails), one click each way;
    an automatic draw follows the choice, and pictures already drawn
    with the same settings are reused. "Preview map" draws every planet
    and chooses All planets, and is the way without script. An automatic
    request whose settings are invalid gets the problems in the
    preview's place, and nothing moves focus.
  - The seed used is named beside the heading, and the size hint and
    each picture's text say the pin marks where players start, the
    centre of the map. A blank seed field takes the seed the preview
    used; a typed seed is kept.
  - **Two kinds of picture** (#775). One line under the heading says the
    picture is drawn by the game's map generator, so terrain that a mod
    makes by script is not shown. On a 2.1 world, "Draw from a real game
    (about 15 to 40 s)" draws the open planet from a real game instead
    (14.3): a throwaway map made with the world's mods and settings,
    loaded with `--test-save` and charted 1024 by 1024 tiles around the
    origin. It is captioned "From a real game, 1024 tiles", with "Back to
    the fast preview", and a toggletip (17.14) gives the detail. It is
    kept in memory with the preview, keyed by the map settings, seed,
    mods, game version and planet, until the store's size limit evicts it
    or the map is created; a restart loses it. It is dropped when the next
    request for the world finds its map made, not at that moment. One user
    draws one at a time, and only that user can fetch it. The button needs
    script; the fast preview does not. 2.0 has no `--test-save`
    and 1.1 has no planets, so their worlds get the fast preview only.
    The window is always centred on the origin. When a picture is about
    99% one colour, a note says everything near the start is outside the
    map and a larger size may show more.
  - **The large view** is a dialog in the page that works as a picture
    viewer, with Previous and Next (a planet not drawn yet is drawn
    there), the planet's download, and Close. Zoom is continuous, in
    steps of 1.15 by wheel, keys, buttons or pinch, from the fitted size
    up to the larger of 8 times the fitted size and 8 CSS pixels per
    tile; a fixed 800% would leave a 3072 picture at under 3 pixels per
    tile. The picture pans by drag, keys, or a tap that centres the spot
    tapped (17.14). Where the view is wide enough for both, the planet's
    own Resources and Terrain fields sit beside the map, the same fields
    as the page's, not copies, so a change there redraws the picture
    after the usual pause and is kept when the view closes; narrower, the
    view is the map alone. Focus decides the keys: on the map the arrows
    step or pan, in a field they belong to the field. Enter in a field of
    those settings commits the value when the field is left and does
    nothing else; it never generates the world. Enter on a button there
    presses it, and Enter in the main form is unchanged.
  - **Downloads**: one planet's PNG, or every drawn planet as a zip,
    named after the world, the seed and the planet, with `-territories`,
    `-amounts` or `-territories-amounts` when those are drawn in, and the
    link says what is drawn.
  - **Demolisher territories** (Vulcanus, Space Age, 2.x). A "Show
    demolisher territories" box, shown only while Vulcanus is viewed and
    kept while another planet is, draws each territory over the picture
    with its size, and lists the counts under it in words. Foreman reads
    them from the game itself, in a one-off container (14.3, ADR 0011),
    only after the picture has arrived (`POST
    /generate/preview/territories`), and holds them with the preview.
    Territories are read up to 3072 tiles, the preview's own most: 4096 ran out of the
    container's 1 GiB on 2.1.24, and 3072 peaked at 750 MiB and 27
    seconds, leaving room on a slow server. While the box is on, the
    downloads take `?territories=1` and the server draws the territories
    into Vulcanus's PNG, and into Vulcanus's file in the zip, named with
    `-territories`; the overlay PNG is drawn per request, one at a time,
    and not stored.
    The large view offers the same "Show demolisher territories" box,
    with the same hint, while the planet viewed is one that has
    territories (Vulcanus) and not otherwise. It is the page's box, not a
    second setting: ticking it in either place changes both, the choice
    holds as the person steps between planets and when the view closes,
    and the form field `territories` carries it. Ticked, the view says it
    is looking and then says the counts; a failed request is said in the
    view, and unticking and ticking again tries again. The box is a 44 px
    target, keyboard reachable with visible focus, and fits at 320 px and
    200% text (17.2).
  - **Resource amounts** (every planet). A "Show resource amounts" box in
    the large view's bar, and one on the One planet picture, each its own
    choice, off when the page loads and kept while the page lives; one
    count runs at a time. It is a view, not a setting: it has no name and
    is never sent with the form. Foreman counts the resources of one planet at a time, in
    the real map, only after that planet's picture is shown (`POST
    /generate/preview/resources`, the form and a `planet` that must be one
    of the world's planets, or the request is refused and no game runs),
    in a one-off container (14.3, ADR 0015), and holds each planet's count
    with the preview. A new draw, a change of settings, another planet,
    unticking or closing drops the request on its way, and a change of
    settings takes the labels away at once. The large view labels each
    patch with its amount in the page language's short form (compact
    number notation, such as "1.2M"), in the territory labels' look, and
    places them largest first so that none overlap another label, a
    demolisher label or the pin; zooming in shows more. A collapsed text
    list gives every patch, by resource, for keyboard and screen-reader
    users (17.15). A patch's label is left off the picture where a patch
    of the same resource with at least 4 times its amount lies within 32
    tiles on both axes, box edge to box edge; the list still gives it.
    - **Patches.** Ores of one resource are one patch where their tiles
      are within 6 tiles of each other. At 3072 tiles on 2.1.24, 2 tiles
      gave Nauvis 138 ore patches (22 under 10 entities) against 112 (4)
      at 6, Vulcanus 338 (118) against 228 (45) and Gleba 280 (68)
      against 235 (28). On Nauvis no two separate fields join at any gap
      up to 16. The one exception is a dense count, one with more than
      400,000 ore tiles in the picture: its ores join within 2 tiles, so
      its patch list is finer. No default map comes near it (Vulcanus at
      3072 has the most, 141,912), and Nauvis with every resource at 600%
      has about 1.1 million. At 6, the richest of them (Nauvis at 400%
      and above) ran past the 60 s limit before running out of memory,
      and the page said to try again where only a smaller size helps (Todd's decision, 2026-11-11; ADR 0015).
      Aquilo has only wells. Fulgora's only resource, scrap, joins within
      16 tiles whatever the ore gap, since at 2 its islands carried
      dozens of labels each at high frequency: at 3072 Fulgora has 240
      patches (23 under 10) at the defaults, and 296 and 303 on two seeds
      with scrap frequency at 600% (296 and 306 on 2.0.77). A few labels can then join neighbouring islands
      across thin channels (ADR 0015). Wells are the resources whose mineable
      product is a fluid (crude oil, sulfuric acid, fluorine, lithium
      brine), and wells of one resource within 24 tiles are one patch,
      because 12 tiles split real fields. A well shows its **yield**, 100 × the patch's summed
      amount ÷ the resource's normal amount, when the game says the
      resource is infinite (crude oil, sulfuric acid, fluorine); lithium
      brine is finite and shows an amount. The label tile is a tile of the
      patch, and the position in the list is in tiles from where players
      start, as (x, y).
    - **Bounds.** Amounts cover the picture only, so a patch cut by its
      edge counts the part inside, and the page says so. The count loads
      the world's mods (ADR 0033), so modded resources appear, on modded
      and built-in planets alike.
    - **Downloads.** While a box is ticked, every download carries what it
      asks for: the planet's PNG (`?amounts=1`) and every planet in the
      zip draw their amounts, worded as the page's compact notation in the
      page language (ADR 0015), and Vulcanus draws its territories while
      that box is ticked. Before the download starts, the page counts each
      of its planets that has nothing held yet, and reads the territories
      if none are held, one run at a time through the preview's queue
      (14.3), with the current settings. It shows which planet of how many
      ("planet 2 of 5") with Cancel. A planet that fails (busy, timeout,
      out of memory, or any other failure) is named with its reason, and
      the page offers Try again and, for the zip, the zip without that
      planet. What is counted is kept with the preview, so the page and
      later downloads reuse it. A preparation does not start while a
      picture is waiting to be drawn or being drawn, since its counts would
      not match the link; the page says why. Closing the large view,
      showing another planet there, changing a setting, or a new picture
      arriving ends the preparation and says so; a click on another
      download meanwhile says one is being prepared. The server never counts for a download and never serves
      one plain or mixed: a link asking for amounts or territories that
      are not held is refused (409).
    - **Cost, measured** with seed 123456 at default settings, at 3072 tiles
      (the most offered), on every planet, on 1.1.110, 2.0.77 and 2.1.24: at
      most 760 MiB (Vulcanus) and 36 s (Gleba), both on 2.1.24, against the 1 GiB and 60 s
      limits (14.3), so no size is held back. Nauvis with resource
      frequency and size at 600% is a dense count (Patches above): on 2.x
      it runs out of memory at about 30 s (generating alone peaks at 822
      MiB) and fails safe with the out-of-memory message and "choose a
      smaller preview size"; 1.1 fits it, at 1,014 MiB and 37 s.
    - **While it runs and after.** A visible line and an indeterminate bar
      say that counting can take up to a minute. Busy, timeout, out of
      memory, missing image, failed and invalid settings each have their own
      sentence beside the box, with Try again where a retry can work; the
      picture and Generate are not affected.
  - One preview runs at a time across Foreman; a request waits up to 20
    seconds for its turn (14.3). Each user has one current preview, keyed
    by its settings, with a picture per planet; asking again for a planet
    already drawn reuses it. The pictures are held in memory under a
    random token tied to the user for 15 minutes, at most 8 users'
    previews and 160 MiB at once, and only that user can fetch them. The
    total fits one Space Age user's five planets at the largest size plus territories,
    so a second user does not evict the first; the room added for #775 is
    for modded planets and real-game pictures.
    - **Per planet.** A planet's picture, its resource count and
      Vulcanus's territories are kept by that planet's share of the
      settings: all of them except the other planets' own autoplace
      controls, which cannot change its map (checked on 2.1.24). A change
      to one planet's controls redraws and recounts that planet only; any
      other change (seed, size, preset, game version, Space Age, the
      world's mods, the other map-gen fields, map settings) drops them
      all. A control two planets share counts as part of each planet's
      share. The default preset sent
      as no files counts as the files the form writes for it.
- Generate records the audit entry `generate` (12.9). Saving step 3, from the page or through
  `set_new_map`, records `new-map`, naming the preset and whether it was
  modified, and ending with the game version. A refusal appears on
  the form, in the page's language.

5.7. **Clone** a world into a new world, then either start a fresh map or
keep the save. (P1)

- It copies `mods/` and the `config/` files, never `rconpw` (4.5), and
  gives the new world the same game version and Space Age flag. The
  source is left as it was. A source a newer Foreman wrote, or one that is
  busy, is refused.
- **Keep the save** copies the one save that would load (7.11) and records
  it as the new world's chosen save. **A fresh map** copies no save and
  sets `new_map` with a random seed; if the source has not started yet, its
  preset comes along. Once the source has started, only its
  `map-gen-settings.json` carries over, so a fresh map of a started world
  uses the game's defaults when that file is missing. A fresh-map clone
  has no save, so it is an Unfinished world (5.6) whose map settings can
  be set in step 3 before its first start.
- When the source's game is running, the page says the copy is the last
  save on disk, not the live game.
- Clone records the audit entry `clone` (12.9).

5.8. **Rename** a world (display name only). (P0)

5.9. **Archive** a world: it is hidden from the main list and cannot be
started until unarchived. Its files stay where they are. (P1)

- **What changes.** Only the world's `world.json` gains `"archived": true`
  (left out when false; the format version stays 1). No snapshot is taken,
  as for a rename (5.8), since nothing is deleted or overwritten. Archive
  and unarchive are one click each, and are recorded as `world-archive` and
  `world-unarchive` (12.9).
- **The active world cannot be archived.** The refusal is made inside the
  world lock that a switch also holds, so a world is never archived and
  active at once.
- **What is refused.** Start, restart (including a scheduled one) and switch
  refuse an archived world, through the pages, MCP and the scheduler alike.
  So does every path that relaunches a game (restore, a mod change, a
  version update, a settings save). The refusal is recorded like any other
  start, restart or switch refusal. A scheduled restart of a stopped
  archived world does nothing, as for any stopped game (7.12).
- **Where it shows.** The Worlds list leaves archived worlds out of its main
  list and shows them in a closed "Archived worlds (n)" foldout at its foot.
  Each archived row ends with two buttons: Saves, which opens the world's
  Saves tab, and Unarchive world, which unarchives it in one click (left
  out for a world newer than this Foreman reads), so an archived world's
  saves and its way back are found from the list. Switch pickers leave
  them out. An archived world's page shows an "Archived" badge and an
  Unarchive button, and the other worlds' pages have an Archive row in
  their Danger zone. On the active world that row says why it cannot be
  archived and links to the Worlds list to switch. When every world is
  archived the list says so and keeps the upload link.
- **Snapshots.** An archived world keeps its snapshots and still counts in
  the retention figures (6.6). Restoring keeps its archive state (6.5).
- **MCP.** `list_worlds` includes archived worlds, with `archived: true`
  (21.4). `archive_world` and `unarchive_world` take `world_id` and are one
  call each (21.7).

5.10. **Delete** a world. Delete always offers a final snapshot first,
defaulting to yes, and asks the admin to type the world's name. The active
world cannot be deleted. (P0)

5.11. **Saves.** List a world's save files with timestamps and sizes;
download any save; upload a save; choose which save loads (section 7.11).
An autosave is listed as the world's name and its save time in the viewer's
time zone (17.11), with its file name as smaller text; a save a person
named keeps their name. A downloaded autosave is named from the world's
name and its save time (UTC); other saves keep their file name. Files are
never renamed, except by the autosave tidy (11.6), and "Loads next" still uses the file name. A file whose name
ends in `.tmp.zip` is a save still being written: it cannot be uploaded,
loaded or chosen to load next, and a folder import skips it; only its
download is allowed. (P0)

5.12. **Favorites.** A favorite is a saved bundle to start new worlds from:
a game version, Space Age, a map, server settings, mods and mod settings.
(P1)

- **Where it lives in the UI.** The Favorites page (`/favorites`) is one of
  the Worlds area's pages, with All worlds and Deleted worlds, reached
  from the Worlds area's list of pages (17.18), not from a button in the
  Worlds header. Generate and Import stay buttons in that header.

- **Made only from a world.** A world's page has "Make a favorite from this
  world", with a name field already filled with the world's name. Making a
  favorite only reads the world's files, so it takes no snapshot and changes
  nothing in the world. A world a newer Foreman wrote has no such form.
- **What it holds.** The exact game version and Space Age flag. The map and
  map-generation settings and the preset, when the world has them; an
  imported world has none, so its favorite gets the default map and the page
  says so. The server settings with every secret removed (the game
  password, the factorio.com username and token; 14.8). The world's portal
  mods with their exact versions. `mod-settings.dat`, copied as it is, since
  the same admins already see those settings on the Mods tab. Folder mods,
  turned-off mods and zips whose name or version cannot be read are left out,
  and the page says which. Foreman cannot tell an uploaded zip from a portal
  zip, so uploaded mods are kept like portal mods; if the portal lacks the
  release, the review page shows a conflict with the upload fix (9.2). Each
  mod's title from its `info.json` is kept with it (9.15).
- **Where it lives.** Each favorite is a folder `favorites/<id>/` under the
  data directory (3.3), holding `favorite.json`, the settings files and
  `mod-settings.dat`. The id is 16 random hex characters Foreman makes, and
  nothing else reaches a path. A favorite is built in a staging folder and
  renamed into place, and a half-made one is cleared at startup. Its files
  are read as untrusted input (14.5, 14.6): size limits, no links, checked
  JSON, checked mod names and versions, and secrets removed again on every
  read. The app database holds nothing about it (3.4). Names are unique,
  ignoring case, and at most 100 characters.
- **Shared, renamed, not edited.** Every signed-in user sees and uses every favorite. A
  favorite can be deleted, after a page that says no snapshot is taken and
  no world changes. **Rename.** Each favorite can be renamed from its
  Details on the Favorites page. The new name follows the same rules as
  making one: not empty, at most 100 characters, and no name another
  favorite already has, ignoring case; a change of case to its own name is
  allowed. Renaming takes the same lock as making a favorite and rewrites
  only the favorite's `favorite.json`, atomically, so it takes no snapshot
  and changes nothing else in the favorite. An export made afterwards
  carries the new name. A rename, like making and deleting a favorite, is
  not in the audit log. Nothing else in a favorite is edited; to change
  its map, mods or settings, make a new one from a world.
- **Starting from one.** Generate's step 1 (5.6) offers "Start from a
  favorite", a plain form that works without script. Choosing one fills in
  the game version (still changeable) and Space Age. When the world is
  made, the favorite's map settings files, server settings and
  `mod-settings.dat` are written, the favorite's mod names become the
  world's saved picks (9.14, up to 500), and the person lands on step 2,
  the world's review page, with a note that the world was made, the
  favorite's mods, and their pinned versions. Step 3 then starts from the
  favorite's map settings, and the person can change any of them there. Generate passes the
  favorite's id, never its mod names, and the review reads the names and
  versions from the favorite by that id (17.17). One
  "Install N mods" then goes through the checks, checksums and snapshot of
  9.13; there is no second download path. A favorite with no mods leads to
  step 2 on the world's Mods tab, as Generate does. A favorite deleted in the meantime is
  an error on the form and no world is made. Generate's audit entry (12.9)
  names the favorite.
- **Other game lines.** The pinned versions are passed to the review page
  only when the new world is on the favorite's game line. On another line
  only the names go, and the review page takes the newest release that fits,
  or shows a conflict for a mod with none. A favorite saved on a game version
  this Foreman does not support shows a conflict on the review page, which
  cannot then be applied; the admin installs the mods from the Mods tab,
  where Generate saved them as picks.
- **Export and import** (P1). A favorite exports as `<name>-favorite.zip`:
  its folder plus `foreman-favorite.json`, which holds the file format
  version. `mod-settings.dat` travels with it, and a visible warning in
  each favorite's details, just above its Export button, says a mod may
  keep a token there. An import takes a `.zip` of at
  most 8 MiB through the drop zone (14.6) and checks all of it before writing
  anything: the format version, six known entry names with no folder, link
  or repeat, each entry's unpacked size, no unknown field in either JSON
  file, and every mod name, version and title and the game version. Server
  settings are stripped of secrets again. A name already in use is refused,
  not renamed. A mod or game version this Foreman lacks is still imported
  and shows as a conflict on review. It is written in a staging folder and
  renamed into place. An imported favorite names no source world.
- **Limits.** The saved picks and the review take at most 500 mod names
  (9.14), so a favorite of 120 mods installs in one review.

5.13. **Export a world file.** A world's page offers "Export world", which
downloads one ZIP, `<name>-world.zip`. ADR 0031 records the format. (P1)

- **What it holds.** `foreman-world.json` first: the file's own
  `format_version` (1 today), the export time, Foreman's version, which
  saves it holds, and whether a game password was set (never the
  password). Then `world.json` without `imported_from` or `archived`; the
  saves; every regular file under `mods/`, at any depth; and the 5.1
  config files except `rconpw`, with every secret removed from
  `server-settings.json` (14.8). Nothing else in the world folder is in it:
  no `.tmp.zip`, no hidden or stray file, no `players.json` (8.14; the new
  host reads its own), and nothing from the app database.
- **Which saves.** The export card offers "All saves" (the default) or
  "Only the save that loads next" (7.11).
- **The file.** Saves and zips are stored, not compressed again. Each entry
  keeps its file's modification time to the second (the ZIP's own time
  field), so "last played" survives the move.
  Unpacked, a world file is a world folder in the 5.1 layout, so it also
  imports by hand as an inbox folder (5.5).
- **The lowest version rule.** A writer uses the lowest format version that
  can hold what it writes, so an older Foreman can still read every file
  that needs nothing newer.
- **A consistent copy.** Foreman takes the world's lock only while it
  hard-links the files into a staging folder (copying when a link fails,
  6.8). It then streams the ZIP from that folder without the lock and
  without holding the file in memory, and removes the folder afterwards.
  A busy world is refused at once.
- **Refusals.** A world that holds a symlink or other special file is
  refused, naming the path. A world a newer Foreman wrote has no Export.
- **A running world** exports the last save on disk, and the card says so.
- **A Warning** above the button says that saves and `mod-settings.dat` may
  hold what a mod stores, a token included, and that the game password is
  not exported. The card also lists what stays behind in the database:
  saved mod picks, mod plan drafts, the audit log, the restart schedule,
  notifications, and which world is active.
- **Limits.** At most 2 exports run at once; a third is refused as busy. A
  failure partway breaks the download, so no complete-looking partial file
  is saved.
- **Separate from archive.** Archive (5.9) is unchanged; an archived world
  can still be exported, since export only reads.
- Recorded as `world-export` with "all saves" or "loaded save" (12.9). Not
  in MCP or the REST API (21.7).

5.14. **Import a world file.** The Import page (5.5) takes a world file
through its own drop zone. (P1)

- **Upload, preview, confirm.** The upload is staged and checked (14.5,
  14.6). A preview then shows the name (editable), game version, Space
  Age, how many saves and mod files, the unpacked size, which save loads,
  whether the game image is on this host (with its download ticked by
  default, 5.5), and a warning when the export had a game password: the
  world will start with no password until one is set. The staged file is
  named by a posted field, never in the address (17.17).
- **Always a new world.** Import world makes a new world with a fresh id
  (5.2). A file never replaces an existing world. A name in use is refused
  on the name field, and the staged file is kept for another try.
- **What arrives.** The world arrives unarchived, with its saves, mods,
  config and `world.json` (name, which save loads, the whitelist switch,
  `new_map`, and fields this Foreman does not know). Secrets are stripped
  again, and `rconpw` is made at its first start (4.5). It never marks
  "already imported" (`imported_from` is for inbox folders only).
- **Checks.** A file whose format version is newer than this Foreman reads
  is refused, saying a newer Foreman made it. A game version must pass the
  checks of 5.5, and at least one save is needed unless `world.json` has
  `new_map`. A `load_save` that names no save in the file is dropped. A
  refusal of a bad file uses one fixed message that never repeats what
  the file holds.
- **Staged files** are removed after an hour, at startup, or when a third
  is staged.
- **Writing** goes through a staging folder, so any failure changes
  nothing.
- Recorded as `world-import` with the new world and the file's Foreman
  version (12.9). Not in MCP or the REST API (21.7).

5.15. **Snapshots in a world file.** An export may include the world's
snapshots, as format version 2. An export without snapshots stays format 1
(5.13). Not scheduled. (P2)

5.16. **Factory map.** A picture of each surface's built factory, read from
a copy of a save (8.16). (P1)

- **What it draws.** One PNG per surface (planets, space platforms and
  modded surfaces) from the game's own chart
  (`force.get_chunk_chart`), plus building outlines by category from the
  same run. Games from 2.0.61; a 1.1 or older 2.0 world says "not
  supported".
- **Categories** are fixed and come from the engine's prototype type, so a
  modded building lands in one too: belts and inserters, production,
  power, fluids, rail, defence, logistic network and storage, and other.
- **Pictures are drawn on the server**: the base picture and one
  transparent PNG per category, on the same pixel grid, at most 4096
  pixels a side. A larger charted area is scaled down, and the page says
  how many tiles a pixel is.
- **Where they are kept.** In `factorymaps/<world-id>/` under the data
  directory, never in a world or a snapshot. One drawing per world. It is
  removed when the world no longer exists.
- **The viewer** is its own page, `/worlds/{id}/map`, a world tab after
  Snapshots. It works like the Generate page's large view (5.6): zoom by
  wheel, keys, buttons or pinch in steps of 1.15; pan by drag, keys or a
  tap; a list of surfaces; and a checkbox per category that adds its
  layer. Downloads: the base PNG, or the base with the ticked layers
  combined on the server.
- **Modes.** Each world has a mode, stored in `world.json` as
  `factory_map: {"mode": "automatic" | "manual" | "off", "skip_online":
  true}` and set on the map page. The default is manual for every world,
  new or existing. A change is not audited and takes no snapshot (it is
  Foreman's own setting, like the display name).
- **Automatic** draws when the world has been stopped for 2 minutes, no
  game is running on this Foreman, the world is not archived, and the save
  that loads differs from both the last picture's and the last failed
  run's. A check every minute covers this, and a restart of Foreman too.
  Until Foreman has listed Docker's containers it assumes a game may be
  running and draws nothing.
- **Manual** is a "Draw factory map" button. Beside a running game it runs
  only on that click, which says the run needs about as much memory as the
  running game uses now and shows that figure.
- **Skip while players are online** (on by default) refuses that click
  while players are online, and also when Foreman cannot ask the game who
  is online. An automatic run never happens beside a running game, so it
  does not apply there.
- **Off** hides the button and stops automatic runs. The last picture
  stays, with the save it was drawn from, until the world is deleted.
- **Failures** use the failure ids of 8.16 plus `too-large`, for output
  over the limits of 14.6. A failure is kept beside the last picture, which
  stays. A cancelled run is not a failure.
- **Downloads** of the base with layers combined run at most 2 at once; a
  third waits 3 seconds and is then refused as busy.
- A start of the world cancels its run.
- Not in MCP or the REST API (21.7).

## 6. Snapshots

6.1. A snapshot holds a world's saves, mods, config, `world.json` and,
when present, `players.json` (8.14) as they were at one moment, plus a label, the time, who took it, and why
(the risky operation it preceded, or "manual"). (P0)

6.2. Foreman takes a snapshot automatically before every risky operation,
before the operation changes any file. If the snapshot fails, the
operation does not run. A switch snapshots both worlds, labelled "Before
switch" with reason `switch` (7.9). A confirmed settings save snapshots
the world first, labelled "Before settings change" with reason `settings`
(11.3). A mod change snapshots it, labelled "Before mod change" with reason
`mods` (9.2). On a stopped world, a run of mod changes shares the snapshot
taken before its first change; the run ends when the game launches or any
other change touches the world's files, and whenever Foreman cannot be sure
the run still holds it takes a new snapshot (9.4, ADR 0014). (P0)

6.3. An admin can take a snapshot by hand at any time. (P0)

6.4. Snapshots are listed per world, newest first, with label, reason,
time and size. The Server page's list of them updates live (16.3), so a
snapshot an operation has just taken shows without a reload. (P0)

6.5. **Restore** puts a world back exactly as the snapshot recorded it,
in one click with a confirmation. Restoring the active world stops it
gracefully first (section 7.6; the confirmation asks for the countdown),
snapshots its current state, restores, and starts it again. Restore is itself undoable: the snapshot taken before it
is the undo. Foreman restores by building the restored world in a
staging folder, renaming the live folder aside, and renaming the staged
folder into place, so the path `worlds/<world-id>` never holds a half
restored world. If Foreman stops partway, startup recovery finishes or
reverses the swap. Before it writes anything, restore checks that every
file still matches the snapshot (6.7). A restore of a live world keeps
that world's current archive state (5.9), whatever the snapshot's
`world.json` says. A deleted world has no current state, so one restored
from its final snapshot comes back unarchived, and an archived world can
never become active through a restore. (P0)

6.6. **Retention** is an app setting, set on the General tab (13.1): keep
the newest *n* automatic snapshots per world, from 1 to 100 (default 10).
Manual snapshots and the final snapshot of a deleted world are never
removed automatically. Saving a lower figure deletes nothing at once. Each
world drops its extra automatic snapshots at its own next automatic
snapshot, as it always has. The General tab and the MCP preview (21.8) say
how many automatic snapshots, across how many worlds, that will remove.
The count includes archived worlds (5.9), which also take automatic
snapshots, and it is made by the same function that prunes, so the two
cannot disagree. When a save would remove snapshots, the General tab first
shows a confirmation page with the count. It says that removed snapshots
cannot be restored and that raising the figure before the next automatic
snapshot keeps them (17.5). A save that would remove none goes ahead
at once. Foreman shows what
snapshots cost as "Snapshots add X": the space held only by snapshot files
that no world also holds, which deleting them would free. A save or mod
zip a world holds by hard link (6.8) adds nothing, and a file shared by
several snapshots counts once, when every name it has on disk belongs to a
snapshot in the count. The same single counting pass, by inode, gives the
space a selection of snapshots frees, each world's figure and the total, so
they always agree. A file held only by snapshots of two different worlds
counts for neither world's figure but is in the total, so the worlds'
figures can add up to less than the total. (P0)

6.7. **How snapshots are stored.** A snapshot is a plain directory,
`snapshots/<world-id>/<time>/`, with the same shape as the world, plus a
`snapshot.json` manifest (6.1). The folder is named by its UTC time, for
example `20261103T142501Z`, with `-2` added on a clash. The manifest
holds the label, the reason (`manual`, `before-delete`, `before-restore`,
`settings`, `mods`, `autosaves`, or the risky operation), the time, who took it, and for each file its
path, size and modification time to the nanosecond, read from the copy the
snapshot holds once it is placed, so the manifest matches the snapshot even
when a file such as `players.json` changed while it was taken. For each save and mod
zip it also holds a SHA-256 hash, which Foreman reuses from the previous
snapshot when the file's inode, size and modification time are unchanged.
Restore checks every hash first and refuses a changed snapshot, naming the
file. A snapshot's size is read from its manifest, and counts the manifest
file itself; what deleting frees is read from the files (6.6). A person can read it, copy it, or
restore it by hand, as the operator guide shows. (P0)

6.8. Save and mod zips are hardlinked into the snapshot rather than
copied; small files (JSON, `.dat`, `rconpw`) are copied. This is safe
because Factorio writes every save as a new file. Foreman itself never
changes a zip in place. If a hardlink fails, for example because the
snapshot directory is on another filesystem, Foreman copies the file
instead, and the status page says snapshots are using full copies. A
snapshot keeps hidden files, and refuses (naming the path) when a world holds a
symlink or other special file, because it cannot be restored exactly.
(P0)

6.9. A restore puts every file's modification time back from the
manifest, because hardlinked saves share their time with the world
(7.11). (P0)

6.10. A snapshot of a running world is taken after a `/server-save` over
RCON, once the game's log says the save finished, so it holds the current
game state. A snapshot or restore asks Docker whether the game is running
first; when Docker cannot be reached it is refused, because Foreman cannot
tell whether files are in use. With fork saving on (`non_blocking_saving`,
11.1) the game writes a save in a second process, and a `/server-save` sent
while an earlier save is still being written is queued behind it. Foreman
therefore counts the saves the game starts (its "Saving game as" and
"Saving to" lines, which an autosave also writes) and waits for one begun
after its own command, until none is being written and 300 ms have passed
with no new start. A save whose process fails ends the wait with its own
error, "the game could not save the world", not the timeout; a save that
fails without any log line still waits the 2-minute limit. (P0)

6.11. **Fail safe on disk space.** Before a snapshot, restore, import or
upload, Foreman works out how many bytes the write needs (nothing for a
hardlink, the full size for a copy or an upload) and refuses, with a clear
message, when the filesystem's free space minus the bytes already promised
to writes in flight is short of that plus a fixed reserve of 2 GB. The
reserve is kept for the game's autosaves. An upload with no declared
length is refused, and at most two uploads run at once. If the disk fills
anyway, the partial file is removed and nothing changes. The snapshot page
shows free space beside what snapshots add. (P0)

6.12. **The Snapshots tab and deleting snapshots.** A Snapshots item in
the main menu lists every world's snapshots, deleted worlds included,
grouped under a heading for each world that links to it. Each row shows
the label, the reason when the label does not already say it, the time,
who took it, its size, what deleting it would free, and Restore and Delete
links. Each world's heading shows what its snapshots add, and the page
shows "Snapshots add X" for all of them (6.6). Size counts every file in
the snapshot, including its manifest. What deleting frees counts only
files with no other name on disk outside the deleted snapshots, whether a
world or another snapshot holds the name by hard link (6.8).

An admin can delete snapshots from this tab or from a world's Snapshots
page. A row's Delete link opens a confirmation page that names the world,
label and time and says how much space it frees. Each row also has a
checkbox, and "Delete selected" posts the selection and opens one
confirmation page for it, which lists every snapshot with its world, label
and time and says how much space the selection frees. The selection is
sent in a posted form, never in the address (17.17), and after the delete the
page says how many snapshots were deleted; the audit log still names each one. One "Select all automatic"
button, on the list's heading row, ticks every automatic snapshot (6.6)
on the page, also by a post; on the tab that is every world's, and the world headings
carry only the world's name and what its snapshots add. It keeps any hand
ticks and never ticks a manual or "Before delete" snapshot. After it, the page says how many snapshots it
ticked, also to a screen reader, and moves focus to that note.

When a snapshot is the one rollback restores (10.4), its confirmation
says plainly that rollback to that version goes away with it. On the
selection page, a snapshot that cannot go (an operation is running on its
world) is listed under "These stay" with the reason, and one already gone
is listed as already deleted.

Confirming deletes the snapshots one at a time, each through the same
steps: refused while an operation runs on its world, recorded in the audit
log as `snapshot-delete` (one entry per snapshot) before anything is
removed, and removing only that snapshot's folder, never the world or a
file another snapshot shares. If some fail, the ones already deleted stay
deleted, and the page lists the rest with the reason. Submitting the page
again after everything is gone ends on the Done page. (P1)

## 7. Server control

### Status

7.1. Foreman shows the game's state as one of: **stopped**, **starting**,
**running** (the server reached the "InGame" state), **stopping**,
**crashed**, or **crash-looping**. When Foreman cannot ask Docker, or
cannot read the game container, the state is **unknown** and the page says
why; Foreman keeps serving every page. The Server page is where setup and
sign-in land. (P0)

- **The Server page's heading** is the active world's name with its state
  badge beside it, and the badge updates live. The Console page has the
  same heading.
- **The join line** reads "Players join at host:port", from
  `FOREMAN_PUBLIC_ADDRESS` (3.2), with a Copy address button that shows
  only where the browser can copy. With the variable blank there is no
  line.
- The page has Overview and Console tabs, and its links to the World
  page, Settings and Switch world sit on one row. Switch world opens the
  Worlds list, where each world has its Switch button (17.10).

7.2. Alongside the state: uptime, game version, active world, the loaded
save, and CPU and memory use. (P0)

### Start, stop, restart

7.3. **Start** runs the pre-start check (7.4), then starts the game
container with the active world. Start and restart of a world that is not
active are refused; the admin switches to it instead (7.9). It reports success only when the server
has reached "InGame" **and** answers over RCON; reaching "InGame" alone is
not enough, because the RCON port can fail to open after it. If that does
not happen within a time limit (default 5 minutes), Foreman reports a
failed start with the reason if it can name one (section 8.7). A start
that fails after Docker started the container but before Foreman could
inspect it and find its address stops the container, so the failure it
reports matches what runs; a start that times out loading or reaching
RCON leaves the game running and watched, since a large map may still
finish loading. Before the
check, a world with no `mods/mod-list.json` gets one that enables `base`
plus the built-in mods its game version and Space Age flag imply (9.7);
without it, 2.x would turn Space Age on by itself. The start steps are shown
as a list while it runs, and steps that never run after a failure read
"Skipped". A start the pre-start check refuses is titled "Start refused" and
lists only the step that ran. (P0)

7.4. **The pre-start check.** Factorio loads a save whose mods are
missing, or whose Space Age content is turned off, with no error, and
deletes that content from the save the next time it saves. So before
every start, Foreman reads the save that will load (section 9.9) and
compares it with the world. It refuses to start, and says why, when:

- a mod in the save is missing or disabled in the world, or the world's
  Space Age flag is off and the save uses DLC mods;
- the save was made by a newer game version than the world's;
- the save was made by a game version Foreman does not support (it
  supports the 1.1, 2.0 and 2.1 lines only, 10.8);
- the world has no save, unless it carries `new_map` (5.1), which is a
  world made by Generate or a fresh-map Clone whose first start creates
  the map (4.3, 5.6). Without that mark the game would make a map on its
  own. The create step only runs while `saves/` holds no save, so once a
  save exists the checks above apply to it as usual;
- the world is Unfinished (5.6) and its `config/map-gen-settings.json`
  names an autoplace control that is not in the world's current planet
  list, for example after a planet mod was removed. The game's `--create`
  stops on an unknown control and writes no save (checked on 2.0.77 and
  2.1.24, #775). The fix is a link to step 3 of Generate, which drops the
  stale settings at its next save; this refusal has no start anyway. The
  check reads the planet list from the cache only, and warms it before the
  world's lock is taken. When the planet run failed, or the world has no
  `mods/mod-list.json` yet, it does not refuse: the game's `--create` then
  fails before it writes a save, so nothing is lost, and the start's error
  says why. Names in `autoplace_settings` (ores, tiles, decoratives) from a
  removed mod do not stop `--create`, and are not checked;
- `mods/mod-list.json` disagrees with the world's Space Age flag, because
  that file is what the game loads until mod management (section 9) takes
  over;
- an enabled mod in the world declares a Factorio major.minor different
  from the world's game version; this refusal has no start anyway, since
  the game cannot load such a mod;
- for a start, restart or switch, an enabled mod's installed version is
  one the mod portal no longer offers (9.3), so players' games cannot
  download it and they could not join. Foreman asks the portal once per
  action, in the check before anything changes, with answers from the
  15-minute cache (9.1) where it has them, and waits at most 5 seconds in
  all. A portal that cannot be reached or does not answer in time never
  blocks the start: the action goes ahead, and its result says in one line
  that Foreman could not check the portal. Folder mods and mods the portal
  does not have at all are not refused, since players need their files
  from the admin whatever Foreman does. The refusal's first fix is
  "Update *mod*" (or "Update N mods"), the same plan the Server page's
  notice opens (9.3), and it says that going ahead deletes nothing from
  the save.

Each problem says what the game would delete. The admin can fix the world
(for example with sync from save) or, for the first bullet, the mod portal
bullet and an unreadable save only, **start anyway**. The refusal itself is the
confirmation: it names exactly what the game will delete, the snapshot
taken first and how to undo it, and its button starts at once, with no
second page. The box gives the problem, then the fix and the restore of the
snapshot taken, and puts going ahead last, behind a closed choice ("Or start
anyway"). It never sends the admin to the host's file system. A restart
offers restart anyway the same way. The button
carries what the box showed (the kinds of problem, the mods they name and
whether the game runs); when the check before anything changes finds
otherwise, Foreman refuses, shows the box again with what is true now, and
says it changed. A save from a newer or
unsupported version, a world with no save and no `new_map`, and a mod list that disagrees
with the Space Age flag are plain refusals with no start-anyway, because
the game could not load the save or would load the wrong mods. A save
Foreman cannot read is reported as such, never guessed at; "Start anyway"
then says the mods could not be checked. (P0)

7.5. **Stop** is graceful by default (section 7.6). (P0)

7.6. **A graceful stop**: if players are online, warn them in game with a
countdown (default 60 seconds, an app setting on the General tab (13.1);
each stop or restart asks for its own, from 0 to 600 seconds, where 0
means no warning, and an MCP stop that gives none uses the setting;
a scheduled restart keeps its fixed countdown, 7.12); run `/server-save` over RCON
and wait for it to finish; then stop the container with Docker's stop
(SIGTERM, with a 120-second timeout). The warnings are chat lines from the
server, sent at the start of the countdown and again at 30 and 10 seconds
before the end. If RCON cannot be reached, Foreman says so and offers a
forced stop instead of silently forcing one. An exit after a stop Foreman
asked for is never reported as a crash, whatever its exit code. The form
names who is online. The seconds field is the only way to skip the warning.
Beside it a one-tap **No warning** button sets the field to 0 seconds and
moves focus there. It needs script, so without script it stays hidden and
the hint says that 0 means no warning. There is no separate stop-now
checkbox. The page's Cancel returns to the page that opened it (the
settings page after a save, for example) when that is one of Foreman's
own pages, and to the Server page otherwise. (P0)

While players are warned, the Server page shows the time left and who was
online when the countdown began, and offers **Cancel stop**. Cancelling
ends the whole action (stop, restart, switch, restore or update) with
nothing changed; an image already downloaded stays. Warned players get a
chat line saying the stop was cancelled. The audit action is `stop-cancel`.
A cancel after the countdown ended, or when there was no countdown, does
nothing. (P0)

7.7. **A forced stop** stops the container without saving. It is a
separate action with its own confirmation that says progress since the
last save will be lost. It kills the container and takes no world lock, so
it can interrupt an operation that is stuck. The container stays stopped
afterwards, because Docker does not restart a container that was stopped
by request. (P0)

7.8. **Restart** is a graceful stop followed by a start. It runs the
pre-start check (7.4) before it stops anything, so a start that would be
refused never stops the game. (P0)

### Switching worlds

7.9. **Switch** to another world, as one operation with these steps in
order: download the new world's game image if the host does not have it
(3.7); graceful stop of the current world; snapshot of both worlds; the
pre-start check on the new world; make the new world the one the
container presents (4.3); start;
confirm it is running (7.3). The download comes before the switch record
(7.10) and before the stop, so a failed download leaves the old world
running and untouched.
If any step fails, Foreman stops, says which step failed, and offers to
switch back, which restores the previous world as active. (P0)

Before the graceful stop, Foreman runs everything that would refuse the
new world's start: its pre-start check, the image check (3.7), the game
network checks (4.4) and the VPN sidecar checks (4.13), as restart does
(7.8). When the check's problems all allow it, the refusal offers
switch anyway the same way (7.4): the refusal is the confirmation, and it
also names the "Before switch" snapshots of both worlds. The pre-start
check runs again after the snapshots. The confirmation page shows the new
world's problems and offers two buttons, "Switch and start anyway" (when
every problem allows it) and "Switch, leave stopped". Leave stopped makes
the new world active and neither checks nor starts it, so its problems wait
for the next Start. A running game ends up running the new world. A switch
from a stopped game starts the new world when "Start after switching" is
ticked, which is the default, and otherwise leaves it stopped with the new
world active, skipping the stop step. If the game started or stopped, or the
new world's problems changed, between the confirmation page and the post,
nothing is done and the page comes back saying what changed. The old container is left stopped, since every start creates a new one
(4.3). The operation is busy on both worlds, both pages show it, and a
forced stop on either ends it. A switch back is a full switch, with its
own snapshots and confirmation; when the new world never became active,
Foreman offers only Start of the old world. A switch names the world it
switches from and is refused when that world is no longer active. The
audit action is `switch`.

7.10. Foreman records a switch in progress (from which world, to which
world, and the last step done) before its first step. On startup it
finds any unfinished switch and either completes it or reports it, and
never leaves the game running a world that is only partly switched in.
(P0)

The record is a row in the app database (3.4), written before the first
step, updated as each step ends (`begun`, `stopped`, `snapshotted`,
`checked`, `presented`, `started`), and removed when the switch ends,
whether it worked or failed. The order alone keeps this section true: the
new world passes its check, and the old game is stopped, before anything
changes what the container presents. One database transaction swaps the
active world and records `presented`.

Recovery at startup never starts or stops a game (3.8). For an unfinished
switch it clears the record and puts a notice on the Server page, which is
logged and cleared by the next action:

- If the last step is before `presented`, the old world stays active. The
  notice says the switch from A to B stopped at that step, A is still
  active and was not started again, and offers Start of A.
- If the last step is `presented` or `started`, the new world is active.
  The notice says B is active and the switch did not confirm it running,
  and offers Start of B and Switch back to A.

A fault during recovery is logged and Foreman keeps serving pages (7.1);
the world it affects is left unstarted, with a notice where one exists.
(P0)

7.11. **Which save loads** is set per world: the latest save, or a named
save file. The game always loads the save with the newest modification
time. To load a named save, Foreman sets that file's modification time to
now, while the game is stopped, just before start. It never sets a time
in the future, which would pin that save above every later autosave.
A chosen save applies once: when the start that loads it reaches "InGame",
the choice resets to the latest save (`load_save` is removed from
`world.json`). After a crash, the game therefore restarts from the newest
autosave. While a save other than the newest is chosen and its file
exists, the Server page names it as the save that loads next, stopped or
running (17.7).
One function decides "the newest save", and the pre-start check (7.4)
uses it, so the check reads the save the game will load. (P0)

### Scheduled restarts

7.12. An admin can schedule a restart (for example nightly at 04:00 in a
chosen time zone). It runs only when nobody is online. (P1)

- **One schedule for all of Foreman.** It is on a Foreman settings page
  (`/settings/restart-schedule`), beside the factorio.com account, and
  applies to whichever world is active. It holds on or off, a daily time,
  an IANA time zone (UTC for a new schedule) and a wait limit of 0 to 12
  whole hours (default 1). Each save is recorded in the audit log. The
  Server page shows the next restart, and while one is under way, its wait,
  its countdown or its skip.
- **It never starts a game.** At the set time the schedule does nothing
  unless the active world's game is running. A stopped, starting,
  stopping, crashed or crash-looping game is left alone, and the schedule
  never uses "start anyway" (7.4).
- **The wait.** The schedule waits until nobody is online, counted by
  `/players online` as in 8.1. When the game cannot be asked, someone is
  counted as online. A restart by hand, a stop or a crash ends
  the wait, and so does turning the schedule off.
- **The countdown always runs.** When nobody is online, Foreman restarts
  with the 60-second in-game warning of 7.6, even on an empty server. The
  length is fixed. Someone who joins during the countdown triggers Cancel
  stop (7.6), which tells them in chat that the stop was cancelled, and the
  wait begins again. The wait limit is still counted from the set time.
- **Skipping.** A wait that reaches its limit skips that day. A limit of 0
  skips at once when anyone is online. An admin who presses Cancel stop
  also skips that day. The Server page says which.
- **No catch-up.** A restart missed while Foreman or its host was not
  running is not made up. A set time reached more than ten minutes late
  does not run, and a wait that the schedule wakes into more than ten
  minutes past its limit is skipped as "missed". After a long sleep the next
  restart is the next set time after now.
- **Daylight saving.** A time that a clock change skips runs at the next
  valid minute. A time the clocks pass twice runs once, at the first pass.
- A scheduled restart that ran, or was skipped because players stayed
  online, sends the scheduled-restart webhook (8.10). The restart's own
  start sends the start webhook too.

## 8. Live operations

### Players

8.1. List who is online now and for how long. RCON's `/players online`
decides who is on. How long comes from the `[JOIN]` lines in the game
container's log since it started, so a chat line cannot fake a join; a
player whose join line Foreman did not see is listed without a time. (P0)

8.2. Kick, ban, unban, promote to admin, demote, and add to or remove from
the whitelist, sent over RCON to the running game. They start from a
player's row on the Server page, and the ban list and whitelist have
their own on the Settings tab (8.3).

- **Confirm kick and ban only.** Each shows a confirmation page with an
  optional reason (one line, at most 200 characters, with no NUL, CR or
  LF). Promote, demote, unban and whitelist changes act at once and offer
  Undo (17.9).
- **Refuse, never escape.** A player name with a space, a control or
  invisible character, or over 64 bytes is refused, and a reason with a
  line break is refused. Foreman never changes the text to make it pass
  (14.9).
- **Audit first.** The action is recorded in the audit log (12.9) before it
  is sent. If the audit write fails, nothing is sent.
- **No snapshot.** A live action takes no snapshot, because the game
  writes its own list file.
- A reply that does not say the game made the change is shown as a refusal
  with the game's words. A whitelist change is refused while the running
  game was started with the whitelist switch off (8.3), because the game
  would lose it at its next restart. (P1)

8.3. View and edit the admin list, ban list and whitelist of any world,
including one that is not running, on the world's Settings tab.

- **While the game runs**, a change to a list the game keeps goes to the
  game as in 8.2 and the game writes its own file, with no snapshot. The
  game keeps the admin list and the ban list, and the whitelist only when
  it was started with the switch below on. The ban list and whitelist then
  change one name at a time, with Undo. Foreman never writes a file the
  running game keeps.
- **Otherwise** (the game is stopped, the world is not active, or the
  running game does not keep the whitelist), Foreman writes the file after
  the usual settings snapshot (11.3). A ban that stays
  in the list keeps its reason.
- **Only whitelisted players can join** is a per-world switch (stored as
  `use_whitelist`). When on, the game starts with both whitelist flags
  (4.3); when off, with neither. The game reads it only at start, so on a
  running world a change shows a restart hint and a Restart server button.
  The whitelist is edited live only while the running game was started
  with the switch on. A change to the switch is audited as
  `whitelist-switch` before it is made; if the audit write fails, it is
  not made. (P1)

### Console and chat

8.4. An RCON console: send a command, see its response. A command is 1 to
1,000 characters on one line, with no NUL, CR or LF. It is sent to the game
unchanged, and every command is recorded in the audit log (section 12.9)
before it is sent. The console is its own Console tab, apart from the
Server page.

- **Commands that carry a secret** run, but are recorded and shown with the
  secret replaced by `••••`. `/config set password <value>` shows as
  `/config set password ••••`, and the reply to `/config get password` is
  replaced by the same mask. The list of such commands is one constant, with
  tests (14.8).
- **Known secrets** (the RCON password and the game password) are also
  removed from the audit text wherever they appear.
- **Lua commands** (`/c`, `/sc`, `/silent-command`) are recorded as typed,
  because a secret inside Lua cannot be told apart from code. A game
  password set through the console is masked only by the pattern above; it
  is not learned for the log.
- The page keeps the last 20 commands per world, in memory only, so they are
  gone when Foreman restarts. (P0)

8.5. In-game chat: see recent chat and send a message as the server, on a
Chat tab beside Overview and Console. The tab shows the last 200 `[CHAT]`
lines of the game's log, live, and marks a line the server itself said.
A message is 1 to 500 characters on one line.

- **Refused, never escaped.** A message is refused if it is empty, has any
  control character (a NUL or line break has its own message), or starts
  with `/` after any blank, control or invisible character. The game
  strips leading spaces from what RCON sends, so such a message would run
  as a command. Commands belong to the console (8.4), which is the only
  way to run one.
- **Audit first.** The message is recorded in the audit log (action
  `chat`, secrets masked) before it is sent. If the audit write fails,
  nothing is sent.
- The message arrives in the list from the game's own echo, so it is
  shown once. (P1)

### Logs and crashes

8.6. A live stream of the game's log, with the most recent lines shown on
open. The view stays usable on a phone and does not grow without limit:
Foreman keeps and shows at most 1,000 lines. The history is the game
container's own log, so Foreman stores nothing, and the history is lost when
the container is recreated at the next start Foreman makes. The page follows
new lines only while scrolled to the bottom and offers "Jump to newest"
otherwise. The game prints its RCON password as `<private>`, but Foreman
still removes the RCON password, the game password and the factorio.com
username, password and token from every line before showing or keeping it, and masks a printed password command (8.4, 14.8).
Any `--rcon-password` value in a line is always masked. (P0)

The game's own Error and Warning lines are coloured; the word stays in
the line, so colour is not the only sign (17.4). **Download log**
(`GET /worlds/{id}/console/log`) sends the kept lines, already masked, as
a text file, so it holds nothing the page would not show (14.8).

8.7. **Crash detection.** When the game stops without being asked to,
Foreman marks it crashed. It polls Docker every few seconds, and inspects
the container at once when the log stream ends, and compares the
container's restart count, start time and out-of-memory flag with what it
saw last, together with the log's failed-load line. Looks can arrive out of
order, so each exit is told apart by Docker's exit time (`FinishedAt`): a
look at an exit no newer than the last one taken in changes nothing. When a
look finds Docker restarted the game since the last one, Foreman reads the
container's log once, within the bounds of the start's read (ADR 0016), so
the runs that ended between two looks still name the crash. It reads only
when it knows the world's secrets to mask the log, and keeps the read only
if it reached the log's end. It does not use the exit code, because the same failure
exits with different codes in different game versions (1.1 exits 0 after a
failed load). It names the reason when it can, by one of these stable ids, and
otherwise says the reason is unknown:

- from the game's log: `mod-missing-dependency`, `mod-error` (a mod that
  failed to load, or that raised the game's "The mod X (version) caused a
  non-recoverable error", which names the mod and its version) and
  `save-unreadable` (a corrupt or unreadable save);
- from the mods the game lists for another Factorio:
  `mods-for-other-version`;
- from Docker's start error: `port-in-use`, only for "port is already
  allocated". Docker refuses the start, so this is a failed start and not
  a crash;
- `out-of-memory`, only from Docker's `OOMKilled` flag, because the game's
  log just stops when the kernel kills it. The game container has no memory
  limit, so a kill of the whole host shows as unknown. When the socket proxy
  allows Docker's events (14.3), an `oom` event names the exit even after
  Docker restarts the game and clears the flag. Without events, an
  out-of-memory kill that Docker restarts before either look sees the flag
  shows as unknown (Docker clears it). Once seen, the flag names the crash until a newer
  exit says otherwise, or a run Docker began after that exit (by Docker's
  log time) names another reason; the game getting back in game clears it.

In a crash loop the newest run is often still loading when Foreman looks
or stops it, so the reason shown is that of the newest run that said how
its load ended: the last reason witnessed, not a guess about the newest
run. A run that reached the game, or failed for no known reason, said so,
and no older reason is shown after it. The reason is unknown when neither
the follower nor that read saw the run's words: the read was refused, cut
short or not made (the secrets were not known), or the words had left
Docker's log.

On 1.1 a mod with a missing dependency is skipped and the game starts, so
there is nothing to name there. A save that fails to load is
reported as a failed load whichever check sees the stop first, the log or
the container poll (ADR 0016), and a Docker look taken before or during an
operation on the world is dropped, so it never overwrites what the
operation set. **A stop takes in a restart Docker made first.** Every
operation that stops a game (Stop, Restart, Switch, Restore, Rollback, mod
changes and Update) first takes the look it would otherwise miss, when
Docker's restart count is above the one Foreman last saw, before it plans:
it counts the crash, follows the new run's log and drops the old RCON
connection, exactly as the next poll would. A game back in game, which is
decided by the log or by RCON answering (Factorio opens RCON only once in
game), is then warned about and saved, as Stop and Restart always do for a
game in game. A game not yet in game is stopped without a warning or save,
and a Stop is never refused for it; an operation that needs a game in game
or stopped (Update, mod changes) still refuses a game that is loading, and
no refusal tells the user to stop it. A crash in the instant between this
look and the plan still takes the plain-stop path. The Server page shows only that
summary, masked like every log line, and links to the full log on the
Console page (8.6); it
never guesses a reason. (P0 to detect a crash; P1 to name the reason)

8.8. **The reason catalogue** is a data file of log patterns, each
naming a reason. Patterns match message text, never source file and
line numbers, which change between game versions. The catalogue is
`internal/game/crashreasons/catalogue.json`, embedded in the binary, in
priority order: when several entries match, the first wins, and no match
means unknown. Each entry names its test fixtures, captured from a real
game container or from Docker's real start error. The catalogue's scenarios
are re-run against the test image of each supported line (docs/STACK.md,
section 1) whenever a test image changes, and on demand against any
version an operator names (`make test-catalogue VERSION=2.1.22`). (P1)

8.9. **Crash loops.** If the game crashes three times within ten minutes,
Foreman marks it crash-looping and stops the container, since Docker's
restart policy would otherwise keep it spinning. This stop gives the game
10 seconds to exit before Docker kills it; an admin's graceful stop keeps
the 120-second save grace of 7.6. If Docker refuses the crash-loop stop, Foreman
tries again at each poll until Docker accepts it. An admin must start it
again by hand. After Foreman itself restarts, a stopped game shows as
stopped; the crash-loop mark is not remembered. (P0)

### Notifications

8.10. Webhooks on these events, each with a stable id: `start`, `stop`,
`crash` (with the reason id of 8.7 when one is known), `crash-loop`,
`player-join`, `player-leave`, `scheduled-restart` (it ran, or it was
skipped because players stayed online, 7.12), `low-disk` (8.13) and
`token-expiring` (12.10). Each event can be turned on or off per
webhook, and an event added later is on by default, except
`token-expiring`, which is off until an admin turns it on, because a
webhook often posts to a channel players read. Webhooks belong to
Foreman as a whole, not to a world, and every event names its world,
except `token-expiring`, which names a token and its owner and no world.

- Start, stop, crash and crash loop come from changes in the game's own
  state, not from what an operation reports. A crash waits at
  most two polls for the log to name its reason and is sent as unknown
  after that. Joins and leaves come from the game's log lines.
- **Nothing is sent twice.** After Foreman restarts, no event is sent for
  something that happened before it. The first state Foreman sees is only
  noted, and a join or leave is sent only for a log line stamped after
  Foreman started. Events that happen while Foreman is down are not sent. (P1)
- **Token expiring** is sent 5 days and again 1 day before a Valid
  token expires, naming the token, its owner and its expiry, never the
  secret. Foreman checks at start and every 15 minutes. Each reminder is
  sent once per period. The token records which reminders were sent, and
  renewing starts over. Unlike other events, a reminder whose time
  passed while Foreman was down is sent at the next check if the token
  is still Valid.

8.11. Presets for Discord, ntfy and Pushover, plus a generic JSON webhook whose body
format is documented in the operator guide. Each webhook has its own
language for the message, defaulting to the language of the admin who adds
it.

- **Discord** posts `content` with names shortened and then escaped, and
  `allowed_mentions` set to none, so no name can ping anyone.
- **ntfy** posts the message as plain text with a `Title` header, and high
  priority for a crash, a crash loop and low disk.
- **Pushover** posts a form with `token`, `user`, `message` (at most 1,024
  characters), `title` (at most 250) and `priority`: 1 for a crash, a crash
  loop and low disk, 0 otherwise, never 2. The address is fixed
  (`api.pushover.net`); the admin gives the application token and the user
  key, which are secrets (14.8) and are letters and digits, at most 64
  characters each. On an edit, each key field left empty keeps its key. The
  MCP tools `add_webhook` and `change_webhook` take them as `app_token` and
  `user_key` (21.7). There is no test-notification button for any preset.
- **Generic** posts JSON, version 1:
  `{"version":1,"event":"<id>","time": "<RFC 3339,
  UTC>","world":{"id","name"},"text":"<message>"}`, plus `player` for a
  join or leave, `reason` for a crash, `skipped` for a scheduled restart,
  `free_bytes` for low disk, and `token` (`{"name","owner","expires"}`)
  for token expiring, whose `world` has an empty id and name. A new
  optional field keeps the version; any other change to the body raises
  it. A golden file test pins it. A token's name and owner are shortened
  and escaped like other names.
- An address starts with `https://`, or with `http://`, which is allowed
  with a warning that messages then go unencrypted. It has a host, no user
  name or password, no space or control character, and at most 2,048
  characters. A Discord address is on discord.com (or discordapp.com) under
  `/api/webhooks/`; an ntfy address names a topic. Webhook addresses and
  Pushover keys are secrets (14.8). (P1)

8.12. **Delivery.** Emitting an event never blocks or delays a server
operation, even when a target hangs or a queue is full: the event goes into
a queue, and a full queue drops it and logs that. Each webhook has
its own queue and sender, so a slow or dead target delays only itself. Each
attempt takes at most 5 seconds. A failed delivery is logged and retried at
most twice, after 1 and then 5 seconds. No redirect is followed, and a
redirect or a 4xx answer other than 408 and 429 is not retried, because
trying again would get the same answer. Undelivered events are not kept
across a Foreman restart. No log line or error holds the address (14.8). (P1)

8.13. **Low disk space.** While the game runs, the Server page and the
notifications (8.10) warn when free space on the disk that holds the worlds
falls below 4 GB, twice the reserve in 6.11, because the game's autosaves
can then fail. The warning clears only when more than 4.5 GB is free, so it
does not flicker around one figure. Both marks are fixed, not settings.
All worlds share the disk, so one state covers them: switching worlds or
restarting Foreman never sends the event twice, and the first reading after
Foreman starts is shown but not sent. The Server page warning links to the
Snapshots page, where space can be freed. (P1)

### Players of a save

8.14. **Players of a save.** Foreman lists every player who has joined a
world's save, with each one's play time and, when Foreman saw it, when
they were last seen. (P1)

- **Seeded, then tracked.** The list starts from a read of a copy of the
  save (8.16). After that, Foreman adds each session from the game's join
  and leave lines (8.1). Time a player had before the read shows as
  "played before Foreman tracked this world". A read that failed leaves
  the list "tracked since" a date, with no earlier totals.
- **The record** is `worlds/<id>/players.json` (5.1): the save it matches,
  the moment its totals are true for, the read's result, whether it may
  be inexact, the game time, and each player's totals and last seen. It
  matches only while the save that loads next (7.11) is the save it was
  made for. Snapshots, restores and clones carry it. A world file does not
  (5.13). An upload of a save removes it.
- **Last seen** is a real date from a session Foreman tracked, or nothing.
  The save's own `connected` and `last_online` are never used, because
  they are wrong for players who were online when it was saved.
- **When a read runs.** A read starts only from a page view, never from a
  scheduler, and the result arrives live. It runs by itself when the
  record is missing, does not match, or is inexact, while the world is
  stopped and no game runs on this Foreman. While the world runs, it runs
  only when an admin clicks "Read players from the newest save". The
  button says the read needs about as much memory as the running game
  uses now, and shows that figure. A read that failed for one save is not
  tried again until the save changes or an admin clicks "Read again".
- **Drift.** A crash, or Foreman being down during a run, marks the record
  inexact. It is read again at the next view while the world is stopped.
  A "Read again" button is always there.
- **Where it shows.** The full list is on the world's Overview page. The
  Server page shows total play time (8.15) and an "N players in this save"
  link to that list.
- A read changes no file the game uses, so it is not audited and takes no
  snapshot. Not in MCP or the REST API (21.7); `list_players` stays "online
  now" (21.4).

8.15. **Total play time.** A world shows how long its map has been played,
as hours and minutes. It is `game.tick`, which the game's `/time` command
reports. While the game runs it comes from plain `/time` over RCON, never
from Lua, so achievements stay on; Foreman asks at most once a minute and
at each finished save. `/time` reports hours, minutes and seconds, so the
figure is true to the second; a reply in a form Foreman has not seen is
refused, never guessed. Otherwise it comes from the record (8.14). (P1)

8.16. **Reading a copy of a save.** Foreman reads a save's players (8.14)
and its factory map (5.16) by running the save in a one-off container
(14.3), never by starting a server. ADR 0030 records the method. (P1)

- **The run.** The world's own game image runs
  `--benchmark <save> --benchmark-ticks 8 --benchmark-runs 1`, with no
  network, the world's mods, and a small control-only helper mod that
  writes JSON. The game writes only to its in-memory write folder (14.3),
  and only the helper's named output is moved into the work folder. The
  game exits by itself.
- **The save is never written.** It is hard-linked into the work folder
  and mounted read-only over the link, or copied when the link fails,
  after the disk check of 6.11. The work folder holds the world's mods as
  9.6 does, with copies (not links) of `mod-list.json` and
  `mod-settings.dat`, because the game rewrites both. `config/`, `rconpw`
  and `server-settings.json` never enter it.
- **Mods are checked twice**, because the game drops a missing mod
  silently and still succeeds. The pre-start check (7.4) runs first. After
  the run, the mods the helper saw must equal the world's enabled mods and
  versions. Either failure gives `mods-differ`, and no figures are kept.
- **The result is kept** only if the save that loads next is still the
  save that was read.
- **Every start cancels a read.** A start of the world, a switch, an
  update, a rollback, a mod change or a restart cancels any read of it and
  waits until its container is removed before the game's container is
  made; a read asked for meanwhile is refused.
- **A world's lock cancels a read.** Any operation that takes a world's
  lock, a person's or Foreman's own (a snapshot, a restore, the mod
  settings run), first cancels that world's players read or factory map
  run and waits for it to release the world, so it is never refused as
  busy because of one. A cancelled read keeps nothing, and the next view
  reads again. The reads themselves take the lock without cancelling
  anything (`world.Manager.Lock` and `LockBackground`,
  `beginStartSaveCopies` in `internal/game/savecopy.go`).
- **Limits:** 12 GiB of memory with no swap, 5 minutes, and the one preview
  slot (14.3). Measured: 6.9 GB and 44 seconds for a 187 MB save with 44
  mods.
- **Failures** are named `image-missing`, `out-of-memory`, `timeout`,
  `mods-differ`, `save-newer` and `bad-output`.

## 9. Mods (per world)

9.1. **Search** the mod portal by name and see each mod's latest version,
download count and latest release date, with its summary. Foreman reads the
portal's whole mod list for the world's game line once (15-minute cache)
and searches it in memory. A mod matches when every word of the text is in
its name or title, in any order. A mod named exactly like the text comes
first; the rest go by downloads, most first, then by name. A
mods.factorio.com link pasted into the box finds that exact mod, read from
the path after `/mod/` (nothing is fetched from the link); text that is
not such a link is an ordinary search. With script the results follow the
typing, from 2 characters, a short pause after the last key, both on the
search page and under the Mods tab's "Find a mod" box, which keeps its own
address; without it the Search button works as before. Text under 2
characters asks the portal nothing, with or without script and over MCP
(21.8); a search left empty without script is refused. A link "Browse the
mod portal" opens the portal's most downloaded list for the game line in a
new tab. It does this because the portal's own search needs credentials,
and search must work without them (9.11, ADR 0012).

Every match is kept. The "Mods matching" panel is always on the Mods tab and
the search page. It is a list, not a table, that says how many mods match
and which ones it shows, ten at a time. Paging sits above and below the list
(Previous, a page box with a Go button, Next); with script only the list is
replaced and focus goes to its heading, and without script they are links
and a plain form. Each row has the title, linking to
`https://mods.factorio.com/mod/<name>` in a new tab (built-in and folder
mods have no link), the summary, the latest version, the downloads and the
latest release date, and one control: "+", named "Add *mod* to the plan"
(44 px on a phone); "Installed"; "In plan" with a tick and a "−" that takes
the pick back out; or "Needed by *mod*", with no button, for a mod that is in
the plan only as another pick's dependency. "+" takes a moment, since the
plan needs the portal; the row shows "Adding…" until the plan is back.
(P0)

9.2. **Install** with dependency resolution. Foreman reads required,
optional and incompatible dependencies and version constraints, and
before changing anything shows the plan: what will be installed,
upgraded, or left alone, and any conflict. The admin can include optional
dependencies one by one. A plan with a conflict cannot be applied. The
resolver is greedy and does not backtrack. For each required dependency it
keeps the installed version when that meets every constraint, and otherwise
takes the newest release on the world's game line that does. Built-in mods
count as present at the game version. An optional dependency's version
limit holds even while its mod is off, so turning it on in the same plan is
held to it. A recommended dependency (the "+" prefix of Factorio 2.1) is
an optional one that starts ticked; one the portal lacks or that would
conflict is left out with a note and never blocks the plan. A sync adds
none. Dependencies of installed mods are read from their own
`info.json`, so turning mods off or on and removing them need no portal.
Each mod an install or an update names has a version list on the plan
page: "Newest that fits", chosen at first, then the portal's releases for
the world's game line, newest first, with the installed one marked.
Choosing a release draws the plan again with that release and the
dependencies it needs; the mods it pulls in still take the newest release
that fits. On the review page each mod is named by its title, linked to
its portal page (9.15), and an asked mod's version list and its way out of
the plan (a cross named "Remove *title* from the plan") sit in its row. Each
offered mod (optional, recommended or no longer needed) shows its title
link, the portal's summary, the version it would install (or has installed)
and that release's date, apart from its tick box, so a click on the link never
changes the tick. These come from the plan's portal lookups and the cache,
with at most 500 further lookups for offered mods; past that a mod shows its
title and link only. Foreman checks a chosen version against the portal's releases on
the world's line on every request, and downloads only a release from that
list; a version the portal does not have there is a conflict. A
search result's "+" adds the mod to the world's plan, shown in a panel
beside the results on the Mods tab; several mods are one plan, one snapshot
and one restart. The panel is a tree. It lists each mod picked with "+" and,
indented under it and marked "Needed by *mod*", the required dependencies
the plan would change. The newest pick is at the top. A dependency two picks
share is listed once, under the topmost pick that needs it, and its "Needed
by" names every mod that needs it, as does its reason on the review page;
the "Review and install N mods" count counts each distinct mod once. Only changes
show: built-in mods and a dependency already installed at a fitting version
are hidden, and one that must be updated says "Will update to *version*". A
dependency that cannot be had (gone from the portal, or without a release
for this game line) or that conflicts with an installed mod shows a warning
and a short reason on its line; Review still opens, and the review page
offers the fixes below. Review opens the review page, which keeps the
versions, the optional mods, the hazard confirmation and Install; adding
mods happens only on the Mods tab, and the review page's Back link goes
there. "Clear plan" empties the panel. A conflict that turning off one installed mod would clear offers
that as a control on the plan page (17.9): "Remove *mod* as part of this
plan" when two mods cannot run together, "Disable *mod* too" when an
enabled mod needs one the plan turns off. It draws the plan again with
both changes, still one snapshot. A plan with a conflict does not list
the steps of applying it. (P0)

9.3. **Update.** Show which mods in a world have a newer version
compatible with the world's game version, and apply any or all as one
snapshotted operation. Nothing updates by itself: Foreman checks for
updates when a Mods tab opens, after the page has loaded, and never on a
timer. A mod that is turned off is not updated by "update all". The tab
says what the portal answered in one summary line above the installed mods,
with "Update all" beside the list's heading, and each mod's update and
flags in that mod's own row, with the fix there. "Up to date" counts only
the mods the portal has at their installed version. A row's More actions
menu also offers "Change version" when the portal
lists a release of the mod for the world's game line other than the
installed one; the portal status fills it in, so it is never shown and then
taken away, and a folder mod or a portal that cannot be reached shows none.
It opens the plan for that mod held at its installed version,
whose version list (9.2) offers every release on the world's line, older
ones too. Going back after a bad update is choosing the release before it,
a downgrade with its snapshot like any mod change (9.4, 9.13). Until
another release is chosen the plan changes nothing and has no apply
button. (P0)

The Server page shows one line when an enabled mod's installed version is
gone from the mod portal, so players cannot join, with its fix: one plan
that updates the mod to the newest release on the world's line ("Update
*mod*"), or its removal when no release is left. It loads after the page,
says nothing when the portal cannot be asked, and loads again whenever an
operation on the world ends, so the line goes once its fix is done,
without a reload. A start, restart or switch of such a world is refused
by the pre-start check (7.4). When that refusal shows and carries the same
plan, the line keeps its text and drops its own button, so one plan never
has two "Update *mod*" buttons. (P1)

9.4. **Enable, disable and remove** a mod, each as a snapshotted change.
Removing a mod that another enabled mod requires is refused with the
reason, and the plan offers to disable that mod too (9.2). Built-in mods cannot be removed, and a mod installed as a folder is
shown as "local" and never replaced. Turning off or removing a mod that the
save about to load uses needs a confirmation on the plan page, a ticked box
that says the game will delete what that mod built. While the world's
game runs, every enabled mod counts as used, since stopping it to apply
the change saves the world with all of them. The restart after the
change then passes exactly those mods in the pre-start check (7.4), and the
world ends running. Without the confirmation the plan cannot be applied. A
confirmation given on a stopped world is not remembered for the next Start.
On a stopped world, only the first mod change since the game last launched
takes a snapshot; later ones reuse it, so undo returns the world to before
the first, and the page can say "Undo the mod changes since" that
snapshot's time. A start refused by the pre-start check does not end the
run, since the world's files did not change. A running world snapshots
every change. (P0)

Removing a mod also lists, on its review page, each installed mod that no
remaining enabled mod requires, as "*mod*, only needed by *mod*". Each is
ticked, so it is removed in the same change (one snapshot, one restart, the
confirmation above); unticking keeps it. The check follows required
dependencies only (including ones without load order), and repeats: a mod
freed by removing another is offered too. Optional, hidden-optional and
recommended dependencies, built-in mods and folder mods are never offered. A
mod that only a kept mod needs is not offered. Foreman keeps no record of
which mods were picked on purpose; the removal is snapshotted, so it can be
undone.

9.5. **Upload** a local mod zip. Foreman reads its `info.json` to name and
version it, and treats it as any other mod afterwards. (P1)

9.6. **Mod settings.** Upload and download `mod-settings.dat`. The page
shows where the Factorio client keeps the file and, for a running world,
says Foreman restarts it. (P0)

Editing the startup settings of the world's mods in the UI, on a page of
its own linked from the Mods tab. For a world with no save yet, its map
settings are edited there too. (P1) Foreman learns each setting's type,
default, limits, allowed values, owner and visibility from a one-off run of
the world's own game image (14.3), and keeps them in memory per world until
the mods or the game version change. A setting's name comes from its
`localised_name`, resolved against every section of the mods' locale files
as the game does: `__1__` parameters, nested keys, `""` joining its
parameters, and `?` taking the first part that resolves. Without one it is
`[mod-setting-name]`, then the setting's own name. A description is the same
with `localised_description` and `[mod-setting-description]`, else none.
The language is the page's, else English. Resolving stops at 20 levels and
1,000 characters and falls back when either is passed (14.6). Hidden settings are not shown, and a setting with one allowed
value is shown locked. Foreman checks each changed value itself, because
the game silently resets a bad value in the file. An int limit at or past
the 64-bit range means no limit.

- **Startup settings** are written to `mod-settings.dat` as one mod change
  (9.13). Only the changed entries are rewritten; the header, per-player
  settings and entries Foreman does not know keep their bytes. The audit
  entry names the settings, never their values.
- **Map settings** live in the save once the map exists, and the save's
  values win over the file (9.15). A world with no save yet edits them in
  the same form, as file writes that seed the new map. A world with a save
  shows them read-only, marked as values for a new map, with one line saying
  an admin changes them in the game under Settings, Mod settings, Map.
  Foreman does not change a saved world's map settings, and runs no Lua or
  RCON command for them.
- **Per-player settings** are not shown. Each player sets them in the game,
  and their bytes in the file are kept exactly.
- **Planets** (#775). The same run also writes the world's planets and
  their autoplace controls as `planets.json`, for Generate's step 3 (5.6).
  On 2.x it lists every space location of type planet with the controls
  of its map-gen settings, and each control's category, richness, on/off
  box, order and localised name; on 1.1, which has no planets, it lists
  the controls of the one surface. Names resolve as setting names do
  above. It is one run and one cache, kept under the same key as the
  definitions, so a change of mods or game version reads both again. No
  startup setting of the five planet mods tested changes the list, so
  saved startup settings do not reset it. Foreman checks the file under
  14.6.

9.7. **Built-in mods** (`base` and the DLC mods for the world's game
version, section 2) are never offered for download or removal. Their
enabled state follows the world's Space Age flag: on enables `base` and
every DLC mod, off enables only `base`. The Mods tab names them in one line
that says which are on, which are off and why, with a link to the setting.
(P0)

9.8. **Sync from save**: read a save's mod list and versions and make the
world's mods match: install missing ones, change versions, and disable
extras. Extras are disabled, never removed. The plan is shown before it
applies (as in 9.2). Mods the portal no longer has are listed by name as
unresolved, and the admin decides. The Mods tab reads the save that loads
(9.9, no portal) and says whether the world's mods already match it,
counting what differs; it offers the match only when they differ or the
save cannot be read. (P0)

9.9. **The save reader.** Foreman reads a save's game version and mod
list itself, from the header of `level.dat0` inside the save zip, without
running the game. (`level-init.dat` is not used: it lists the mods from
when the map was created.) The format is undocumented and changes between
versions, so the reader has a test save for every game minor version
Foreman supports, and refuses a save it cannot parse rather than
guessing. The same reader serves the pre-start check (7.4). (P0)

9.10. **Unresolved mods.** By default an unresolved mod blocks the sync,
and Foreman offers an upload of that mod's zip. Two other choices, each
explicit: use the nearest release compatible with the world's game
version, shown as a change in the plan; or continue without the mod,
only after a confirmation that the game will delete that mod's content
from the world, and a snapshot. The nearest release is the lowest release
above the missing version on the world's game line; when there is none, the
newest release below it, with a warning. (P0)

9.11. **Portal credentials.** The factorio.com username and token come
from `FOREMAN_FACTORIO_USERNAME` and `FOREMAN_FACTORIO_TOKEN` (or their
`_FILE` forms) first. If those are absent, an admin can enter them in the
UI, where they are stored encrypted with a key from
`FOREMAN_SECRET_KEY` (or its `_FILE` form). They are never shown back. If
neither source has them, mod downloads are disabled with an explanation and
a link to the account page, which returns to where the admin came from;
search still works. Giving only one of the two variables, or a key shorter
than 32 characters, is a startup error (3.2). The account page is at
`/settings/factorio-account`. It says "Set by environment" when the
variables hold the account, and offers no form then. A stored account is
replaced or removed there. The page leads with whether mod downloads are on,
says that every admin's downloads use this one account, and keeps the
replace form folded while an account is stored; neither the username nor the token is shown back,
and the token is not checked against factorio.com when saved. A stored
account that cannot be opened (the key changed) is treated as not set and
asked for again; it never stops Foreman. How it is sealed is in ADR 0013.
(P0)

9.12. Downloaded mods are checked against the SHA-1 checksum the portal
publishes, and a mismatch is refused. A download that fails names the mod
and version, says whether the world kept running, and offers "Try again"
(the same plan, two clicks) or, for a refused login, the account page.
(P0)

9.13. **One operation per change.** Every mod change goes through one path:
install, update, update all, sync from a save, the fix for a flagged mod,
enable, disable, remove, a mod upload, a `mod-settings.dat` upload and a
mod settings edit (9.6). On a
running world it is one operation shaped like a game version update (10.3):
download every release first, then warn the players and stop the game, take
the snapshot (6.2), apply the change, run the pre-start check (7.4), and
start. A download, checksum or zip that fails leaves the game running and
the world untouched. The change is also checked against the mods folder
after the downloads and before the stop, so a change the folder would
refuse costs no downtime. A stopped world skips the stop and the start. A
world that is not active is changed the same way and reports on its own
Mods tab, with "Undo this mod change". After a later change of a run on a
stopped world (9.4) the undo reads "Undo the mod changes since" the run's
snapshot time, and the result says no new snapshot was taken. Undo opens the
restore confirmation (6.5), which for a "Before mod change" snapshot is
headed "Undo the mod changes in" the world and first lists, from both mods
folders, the mods undo removes, puts back, returns to an earlier version or
turns back on or off; when a folder cannot be read it is the usual restore
page. A finished change says which mods were installed, updated, turned on
or off, or removed. After any failed operation, the steps that never ran
fold into one line (DESIGN 7.6). The audit log records the operation
as `mods`. A change is refused before anything is touched when the game
cannot be stopped (paused, not ready, or RCON unreachable) or the mods
folder no longer matches the plan. A mod settings edit also carries a hash
of the `mod-settings.dat` its form was drawn from, and is refused as
changed when the file no longer matches. (P0)

9.14. **The plan is worked out, not stored.** Foreman saves, per world,
only the names of the mods picked with "+", at most 500 (3.4, ADR 0021). Each
time the panel is drawn the tree is worked out again, so removing a pick
also drops every dependency nothing else needs, and a dependency has no
remove button of its own. When "Install N mods" is accepted, the mods it
installed leave the saved picks and the others stay (an Unfinished world's
Continue, 5.6, opens step 2 only while picks are left, and a pick installed
another way, such as an upload, is not work that remains); nothing is cleared by
a changed plan, a failed check, a refused request or any other kind of plan.
The failure page's "Try again" still carries the names. Deleting a world
clears its picks. The picks are a database table, not a file in the world,
so no snapshot, clone or restore carries them. Beyond the picks, Foreman
keeps no plan in storage. The review page keeps its answers (ticks, chosen
versions, mods to remove or turn off) in a **draft held in memory** under a
random token tied to the signed-in user and the world, like the previews of
5.6 and 11.3 (ADR 0022). A draft lasts 2 hours, a user keeps at most 20 and
the server at most 200, the oldest going first; it is lost on a restart. A
draft that has expired or is unknown, or is another user's, is rebuilt
from the saved picks, and the page says so. A link into the review carries
only its kind and at most one name (`update` with no name for Update all, a
favorite's id, or a draft's token), and the review's own links and redraws
carry the token (17.17). The review page posts the request, a hash of
the plan (which covers the mods dropped under 9.4) and a fingerprint of the
mods folder (file names, sizes, times, and the content of `mod-list.json`).
The server works the plan out again; when the hash or the fingerprint
differs it applies nothing and shows the new plan with "The plan changed"
(17.8). The fingerprint is read again after the game stops, because the
game may write files as it exits. (P0)

9.15. **How the game treats mod files, and how Foreman names mods** (checked in a real game on 1.1, 2.0
and 2.1). When `mods/` holds two zips of one mod the game loads the higher
version, so a plan starts from the newest zip and deletes the other zips of
a mod it changes or removes. The game rewrites `mod-list.json` when it exits,
with the same meaning but not the same bytes, and adds a mod the list left
out as on. It writes `mod-settings.dat` at its first start, and only when
a mod declares settings. A mod change that turns off a mod the save uses
ends with the pre-start refusal unless the admin confirmed the drop (9.4).
For mod settings: on a world with a save, the save's map values win over
the file's, so stopping, editing the file and starting changes nothing
(2.1.24: file 42, save 4, the mod saw 4). The game rewrites
`mod-settings.dat` when the server starts, not when it exits. Games before
2.0 store whole numbers as doubles. The only ways to change a map value in
a save are a Lua command in the owning mod's context, which ends the save's
achievements for good, or an admin in the game's Map tab.

**Mods are shown by title.** Wherever a page names a mod, it shows the
mod's title, not its slug, and the slug is the fallback. The title comes from
the mod's `info.json` for an installed mod and from the mod portal otherwise
(cached, with no lookups beyond those a page already makes). A title is
untrusted input: control characters become spaces, the text is cut to 100
characters, it is escaped like any text, and it is marked `translate="no"`.
A mod named on its own (a list row, a plan row, a heading) links to its
portal page in a new tab, except a built-in mod, a folder mod, and a mod the
portal is known to lack; a mod named inside a sentence or on a button is plain
text, because a link inside a sentence or a button is hard to use. Saved
records keep the slug they were written with: snapshot labels, audit entries
and sent notifications. (P1)

9.16. **Public mods page.** Players download the running world's mods
here without signing in, including mods the portal can no longer supply
(VISION, Auth). Whether the mods may be shared is the admin's call;
Foreman shows no licence. (P1)

- **Setting.** Off by default. An admin turns it on or off at
  `/settings/public-mods`. The choice is stored in the app database,
  takes effect at once, and is audited as `public-mods-set` with the
  detail "on" or "off". While it is off, every path under `/public/`
  answers 404, exactly like an unknown path. So does `/public/static`
  without the slash.
- **Page.** `GET /public/mods` needs no sign-in. For each world whose game
  is running (one in v1), it shows the world's name, its game version, and
  its enabled mods that are not built in (9.7). Each mod shows its title
  (9.15), its version and a download link, and a "Download all" link sits
  beside them. When two zips of one mod exist, only the zip the game loads
  is listed (9.15). A mod installed as a folder is listed, marked "Ask the
  server admin for this mod", with no download. Turned-off mods are left
  out. When no game is running, or it is starting, stopping or
  crash-looping, the page says so and lists nothing.
- **What it reveals.** Only the items above. No save, setting, secret,
  player, log, snapshot, file size or path. The page carries
  `<meta name="robots" content="noindex">`, and its only link back into
  Foreman is the product name.
- **Marks.** A mod the Mods tab's portal check flags (9.3) is marked in
  words: "Not on the mod portal" (not on the portal, or that version is
  gone) or "Hidden on the mod portal" (deprecated). The answer is kept per
  world and per mods fingerprint (9.14): 15 minutes after an answer, 1
  minute after a failure. One check runs at a time per world. The page
  waits at most 5 seconds for a check, then shows the list without marks
  and one line saying the portal could not be checked. Downloads never
  depend on the portal.
- **Files.** `GET /public/mods/<world>/files/<file>` sends one mod zip and
  answers conditional and range requests as a save download does (5.11).
  `GET /public/mods/<world>/all.zip` streams one zip holding each mod zip,
  stored without compression, downloaded as `<world name>-mods.zip`. The
  page says where the Factorio client keeps its mods folder on Windows,
  macOS and Linux, as 9.6 does for `mod-settings.dat`.
- **Accessibility and languages.** The page meets 17.1, 17.2 and 17.4,
  and 22.7 in every language. Each download link's name holds the mod's
  title and version, so no two links share a name; marks are words, never
  colour alone. The page has the language menu, which posts to
  `POST /public/language` with the same handling and return check as 22.2
  and 14.17. Its scripts and styles load from `/public/static/`. Its text
  lives in the catalogue area `publicmods`.

## 10. Game versions

10.1. Each world shows its game version: an exact patch release of a
supported line (10.8), such as `2.0.77`. The world runs the image
`factoriotools/factorio:<version>`, referenced by that tag (4.2). (P0)

10.2. When a newer stable or experimental release exists, the world shows
a notice that links to the changelog on the Factorio wiki's Version_history
page for that release's line. Stable and experimental come from
factorio.com's latest-releases list; which releases have an image comes from
Docker Hub's tag list. A release with no image yet shows "No image yet" and
cannot be chosen. Foreman checks only when an admin opens a page that shows
it, never on a timer at startup. (P1)

10.3. **Update** a world's game version to a newer release chosen with
the version picker (10.7), for example from 2.1.19 to 2.1.24, or from a
2.0 release to a 2.1 release: snapshot, download that version's image when
the local Docker does not hold it (a tag already held is not downloaded
again; "Download game image" downloads it again on request, 3.7), and
restart and confirm "InGame" only if the game was running (a stopped world
stays stopped, as in a switch, 7.9). Any world can be updated, active or
not. A world that is not active never runs, so nothing stops or starts;
the world lock keeps a switch out meanwhile, and the pre-start check still
runs as advice. Its update runs in the background, and its own world page
shows the progress and then the result, with "Update anyway" after a
pre-start refusal, as on the Server page.
A world's version only ever moves up: Factorio cannot load a save in an
older version than the one that wrote it, so the picker offers only newer
releases and Foreman refuses an older one. The only way back is rollback
(10.4), which restores the save from before the update.
The pre-start check (7.4) applies as at any start, so mods made for the old
line refuse an update to a new line until they are updated. (P0)

10.4. **Rollback** restores the pre-update snapshot and the previous
version together, as one action. It goes one step back: it restores the
newest "Before update" snapshot whose version is older than the world's
current one, and takes the usual "Before restore" snapshot first as the undo.
Pruning (6.6) never removes a world's newest "Before update" snapshot.
When the local Docker no longer holds the previous version's image, the
start is refused as in 3.7. A world that is not active can be rolled
back too, and nothing starts. (P0)

10.5. Foreman never updates a game version, a mod, or itself on its own.
(P0)

10.6. (Removed: adopted mode was dropped; ADR 0005.)

10.7. **The version picker.** Wherever an admin chooses a world's game
version (import 5.5, generate 5.6, update 10.3), Foreman lists the
`factoriotools/factorio` tags from Docker Hub, filtered to patch releases
of the supported lines. It fetches the list only when the admin opens a
page that shows the picker, like the release check in 10.2, never at
startup or on a timer. It keeps the Docker Hub list and the latest
releases (10.2) in memory for 5 minutes, and does not keep a failed
answer. The admin may also type a version; Foreman accepts only an exact
patch release of a supported line. A page that already has a target
version, such as the update page reached from a world's Update link, shows
it chosen in the list as well as in the typed box; a target not on the
list leaves "Choose a version" chosen and the typed box keeps it. When Docker Hub cannot be reached, the
page says so and the typed version still works. An import also refuses a
typed version that is neither downloaded nor on the list, unless Docker
Hub cannot be reached (5.5). (P0)

10.8. **Supported lines.** Foreman supports the 1.1, 2.0 and 2.1 lines,
and every patch release within them. A new line (for example 2.2) needs a
Foreman update, because the save reader (9.9) and the built-in mod lists
(9.7) are kept per line. A save from an unsupported line is refused at
start (7.4). (P0)

## 11. Game settings

11.1. `server-settings.json`: a form for the common fields (name,
description, game password, visibility, autosave interval and slots,
auto-pause, max players, require user verification, and admins) plus a
raw JSON editor for everything else. (P0 form; P1 raw editor)

- **Admins live in `server-adminlist.json`**, not in `server-settings.json`;
  the game always gets that file (4.3). On a stopped world Foreman writes
  the file. On a running world Foreman sends `/promote` and `/demote` over
  RCON for the names that changed, and the game writes the file, so the
  change applies at once. Names are compared without regard to case, as
  the game does. The game's console has no quoting, so Foreman sends a name
  as typed and refuses, instead of escaping, a name that is empty, over 64
  bytes, not UTF-8, or holds any whitespace (Unicode spaces included) or a
  control or invisible character (14.9).
- **Visibility** offers the local network and hidden. Listing on the
  public server list needs a factorio.com username and token, which
  Foreman does not take (issue #206). A raw edit may still hold public, and
  Foreman keeps it.
- **Every `server-settings.json` Foreman writes has `name`, `description`
  and `visibility`** (with boolean `public` and `lan`), because the game
  refuses the file at start without them (4.3). The first save on a world
  with no file writes the world's name as the name unless another is
  typed, a description that may be empty, and Hidden unless another
  visibility is chosen. The form and the raw editor both refuse a file
  that lacks one of the three.
- **The form changes only the keys it shows.** Every other key, and the
  file's layout and key order, are kept.
- **Groups.** Listing (name, description, visibility), Players and access
  (game password, max players, returning players skip the cap, user
  verification, AFK kick), Gameplay (auto-pause, pause while someone joins,
  only admins can pause, who may use console commands), Saving (interval,
  slots, fork saving), Network (upload limit and slots), Admins, the ban
  list and the whitelist (8.3). `auto_pause_when_players_connect` is hidden
  on 1.1 and never written there.
- **One group per sub-page** (17.18). The form is too long to read whole,
  so each group is a sub-page of Game settings, listed under it in the
  world's list of pages; the plain Game settings link opens Listing. It is
  still one form: the other groups are kept in it, hidden, so a save
  changes only what was edited, and a form with errors shows every group.
  With script, edits made on one sub-page are kept in the browser tab
  while the person moves to another, so one preview covers them all, and
  the save bar counts the unsaved changes; saving or Cancel drops them.
  Without script each sub-page previews and saves on its own. A save
  returns to the sub-page it came from.
- **Every unset field shows the value that applies** (17.16). A
  whole-number field the file lacks is empty with the game's default as its
  placeholder and in a hint; a true-or-false field the file lacks shows the
  game's default state. Clearing a whole-number box removes its key, so
  the next start uses the game's default and the hint ends "Clear the box to
  use it."; a checkbox writes its state when it differs from what the form
  showed. Name and description are never removed. The save takes the usual
  snapshot (11.3).
- **20 autosave slots for new worlds.** Foreman writes `autosave_slots: 20`
  whenever it creates `server-settings.json`: Generate, Import of a save or
  folder with no file, and a world's first settings save. These files hold
  the name, an empty description, Hidden visibility, `auto_pause: true` and
  the 20 slots. A favorite's file gets the key only when it lacks one, so a
  Generate from a favorite writes 20 slots even if the favorite had cleared
  the key. An imported folder's own file is copied as it is. An older file
  without the key shows the game's 5 and a hint that Foreman's default for
  new worlds is 20. Foreman never writes an existing world's file at start.
- **Fork saving** (`non_blocking_saving`) makes the game write a save in a
  second process, so a save no longer pauses the server. It is experimental in
  the game, and the fork can bring memory use close to double on a busy map;
  the form says so. It is disabled, with the reason, when Docker's reported
  operating system (the `Os` field of `GET /version`) is not `linux`, and
  Foreman never writes it on then; one already on stays on and can be turned
  off only in the raw editor. A snapshot waits for a forked save (6.10).
- **Auto-pause is on unless the file says otherwise.** A missing
  `auto_pause` key shows as on, the game's own default. The first save on
  a world with no file writes `auto_pause: true` with the three keys
  above. An explicit `false` is kept.
- **Make admin.** On a running world the page lists the connected players
  who are not admins, each with a Make admin button. It adds the name to
  the admins and goes through the normal preview and confirm (11.3). A
  name the game could not be sent (above) is not listed.

11.2. The raw editor validates before saving and refuses invalid input
with the line and the reason. (P1)

- The editor covers `server-settings.json`. The text must be at most 1 MB
  and one JSON object. Known keys are type-checked and range-checked for
  the world's game line (1.1, 2.0 or 2.1), from the example file in that
  line's image. Following an error's link puts the caret on its line.
  Unknown keys are kept, and the diff lists them as keys
  Foreman does not check. An error never shows a value from the file.
- A secret key (11.4) is left out of the editor's text and put back when
  the change is saved.

11.3. Every settings save is shown as a diff before it is written, and
takes a snapshot first. (P0)

- **Edit, preview, confirm.** The edit page validates and shows the diff.
  The pending change is held on the server, in memory, under a random
  token tied to the user and the world, for 15 minutes and at most 8 per
  user, so no setting and no secret sits in a hidden field. One preview
  saves once. A confirm refused because the world is read-only (a newer
  Foreman wrote it, or its settings cannot be read) does not spend the
  token, so the same preview can be confirmed once that is put right.
- **One confirmation takes one snapshot and writes one audit entry**, even
  when it changes several files. In order, it takes the world's lock,
  saves a running game (6.10), checks that every file is as it was at the
  preview, takes the snapshot with reason `settings` (6.2), records the
  audit entry `settings-save`, which names the files and never a value
  (12.9, 14.8), and writes each file atomically. If a file changed after
  the preview, nothing is written and the page shows the new diff with a
  warning. If the snapshot or the audit entry fails, nothing is written.
- **A running world.** The game reads `server-settings.json` only when it
  starts, so the page says the change takes effect after a restart and
  links to Restart. That link opens the same stop page as the Server
  page's Restart, and its Cancel comes back to the settings (7.6). An admin change applies at once (11.1). If the files
  were written but a promote or demote could not be sent, the page says
  the save was partial.
- A world a newer Foreman wrote is shown read-only (5.1).

11.4. Secrets in `server-settings.json` are write-only: the game password
and the factorio.com `username`, `password` and `token`. (P0)

- The form shows whether a game password is set, never its value. The diff
  says a secret "will be set", "changed" or "removed", without the value.
  When a refused form is shown again, a typed password is not shown back;
  the page tells the person to type it again.
- No page, log line, audit entry or error carries a secret (14.8).

11.5. `map-gen-settings.json` and `map-settings.json` get the same
form-and-raw editor. Map-gen settings are edited only for a world that has
no save yet, in Generate's step 3 (section 5.6). (P1 for map-gen as part of
generate; P2 for editing map-settings afterwards, issue #207)

The game reads both files only at `--create`, when it makes the map.
Editing `map-settings.json` later has no effect, so Foreman offers map
settings only until the first start. Changing them in a running game would need a
separate design (issue #207).

11.6. **Tidying autosaves after the slot count goes down.** (P1)

- **When.** At every start Foreman makes (start, restart, switch, start
  anyway, scheduled restart), after the game has stopped and before the
  save to load is chosen (7.11), Foreman looks at the world's autosaves:
  files named `_autosaveN.zip`, and leftovers of a tidy cut short, named
  `_autosaveN.tidy.zip` (N is the slot the file is moving to, 11.6
  "Order"). It tidies when some N is greater than S, the `autosave_slots`
  value in `server-settings.json` (the game's default of 5 when the key is
  missing), and also whenever any `.tidy.zip` file exists. It never tidies while the game runs, when S or
  `autosave_interval` is 0, or when an admin has chosen an autosave to
  load next; the next start tries again. Docker restarting a crashed game
  never tidies.
- **The last slot written, K.** A graceful stop saves over the file the
  game loaded, so that file can look newest without being the last
  autosave. K is the number of the newest autosave by modification time,
  skipping the file the last run loaded (read from the game's "Loading
  map" line, 8.6). When Foreman has restarted since that run and no
  longer knows the loaded file, K is the number of the newest autosave by
  time.
- **Mapping.** The newest S autosaves stay. The i-th newest (i = 1 is the
  newest) becomes `_autosave(((K − i) mod S) + 1).zip`, the remainder
  running from 0 to S−1, so the oldest kept file takes the slot the game
  writes next. Every other autosave is removed. Other saves and
  `.tmp.zip` files are never touched. Renaming keeps each file's
  modification time. Example: 20 slots down to 10 with K = 17: files 17
  down to 8 become slots 7 to 1, then 10, 9, 8; files 1 to 7 and 18 to 20
  are removed.
- **Order.** (1) A snapshot, reason `autosaves`, labelled "Before
  autosave tidy" (6.2). (2) An audit entry `autosave-tidy` with how many
  files were kept, renamed and removed (12.9), written before any file
  changes. (3) Each kept file is renamed to `_autosaveN.tidy.zip`, N its
  final slot. (4) The rest are removed. (5) Each kept file is renamed to
  its final name. If the snapshot or the audit entry fails, the tidy is
  skipped, the reason is logged, and the start carries on with the files
  untouched. A tidy cut short is finished by the next, which keeps each
  `.tidy.zip` file in slot N and works out the rest of the mapping from
  them; if they fit no mapping for the current S, the tidy is skipped and
  the reason is logged. The snapshot and the audit entry are made by
  "Foreman", not by the user who started the game.
- **Accuracy.** The mapping is exact when K is the last real autosave and
  either the new count divides the old one or the map has made fewer
  autosaves than the old count. Otherwise the game's first overwrite may
  hit a file that is not the oldest. The snapshot is the undo.
- **What the UI says.** When a settings preview (11.3) lowers S and
  autosaves above S exist, it says: "At the next start, Foreman keeps the
  newest {count} autosaves. It removes {count} other autosaves. A snapshot
  is taken first." Each sentence is its own message with one count
  (docs/I18N.md). The second sentence is left out when nothing is removed.
  The snapshot then shows under its own label.

## 12. Sign-in and users

12.1. **One role in v1**: every user is a full admin, and every page
needs a signed-in user, except the public mods page (9.16) while an
admin has it turned on. The MCP endpoint (section 21) and the REST API
(section 23) are not pages. They accept only API tokens (12.10). The user model keeps a role field so a
read-only viewer can be added later without a migration. (P0)

12.2. Three sign-in methods, any combination enabled by configuration:
local accounts (12.3), trusted proxy headers (12.5), and OpenID Connect
(12.7). (P0 for the first two; P1 for OIDC)

12.3. **Local accounts**: username and password. On first run with no
users, Foreman shows a one-time setup page to create the first admin; the
page is reachable only until an admin exists. When `local` is not an
enabled sign-in method there is no setup page, and the first user to
arrive through trusted headers (12.5) becomes the admin. The setup page
reports every problem with its fields at once. (P0)

12.4. Passwords are stored with a slow, salted password hash, chosen in
docs/STACK.md. A password has at least 12 characters. Sign-in failures
are rate-limited per username and per address (limits in 12.11). After a
local sign-in the admin returns to the page they asked for (14.17). (P0)

12.5. **Trusted proxy headers** (forward auth from Authentik, Authelia or
oauth2-proxy): a configured header names the user, a second configured
header lists groups, and a required group names who may sign in. All
three are required: without a required group, anyone the proxy lets
through would become an admin. A group list is split on `|` (Authentik's
form) and on `,`, and a required group that contains either is refused at
startup. A user outside the group gets a 403 page.
Foreman honours these headers **only** on requests whose direct peer address is
inside `FOREMAN_TRUSTED_PROXIES` (a list of address ranges). From any
other address the headers are ignored. A user seen for the first time is
created automatically. (P0)

12.6. Sign-in methods are turned on with `FOREMAN_AUTH_METHODS` (3.2). If
trusted headers are enabled and `FOREMAN_TRUSTED_PROXIES` is empty,
Foreman refuses to start. (P0)

Trusted-header sign-in also ignores the headers when the direct peer is
inside the address range of a game network, IPv4 or IPv6, because a game
is never the proxy. The ranges are the game network's (4.4) and, with a
VPN sidecar, those of every Docker network the sidecar is on (4.13). Foreman
in a container records that range before the first start; outside a
container it learns a game's range right after the game starts. With a
sidecar, in either mode it reads the sidecar's Docker ranges before the
first game starts and restores that knowledge on Foreman restart. Docker
cannot reveal the VPN interface's ranges: operators must not trust the
sidecar's VPN address or a broad VPN range as a sign-in proxy. Use only
an individual address of a separate trusted proxy, or local sign-in.
`fe80::/10` counts as a game range
whenever a game network has IPv6. While those ranges are not yet known
(Docker unreachable, the container missing, or not yet inspected),
header sign-in is refused, the reason is logged once, and the refusal page
says so in plain words without any header value. Existing sessions and
local password sign-in are unaffected. The monitor refreshes the ranges.
(P0)

12.7. **OpenID Connect** (OIDC), for any provider that can put a username
and a list of groups in its ID token. (P1)

- **Settings (3.2).** `oidc` in `FOREMAN_AUTH_METHODS` needs
  `FOREMAN_OIDC_ISSUER`, `FOREMAN_OIDC_CLIENT_ID`,
  `FOREMAN_OIDC_CLIENT_SECRET` (or `_FILE`), `FOREMAN_OIDC_REQUIRED_GROUP`
  and `FOREMAN_PUBLIC_URL`. A missing value is a startup error, reported
  with the others. The required group is mandatory for the reason 12.5
  gives. The issuer must be `https`; an `http` issuer is a startup error,
  so the client secret never crosses the network in the clear. No message
  quotes the secret (14.8). (Todd's rulings, 2026-11-18.)
- **The flow.** The authorization code flow with PKCE (S256), a state and a
  nonce. `GET /signin/oidc` starts it and `GET /signin/oidc/callback`
  ends it; the redirect address is `FOREMAN_PUBLIC_URL` plus the callback
  path. The scopes asked for are `openid profile email groups`. The
  username comes from the claim `preferred_username` and the groups from
  `groups`. These claim names are fixed, not settings. Foreman reads only
  the ID token: no userinfo call and no refresh. It checks the token's
  signature, issuer, audience, expiry and nonce, and that the required group
  is in `groups`.
- **Pending sign-ins** are kept in memory by state. Each lasts 10 minutes,
  works once (a failed callback uses it up too), and at most 1000 are held;
  a new one drops the oldest. A cookie, `foreman_oidc`, holds the state in
  the browser that started the sign-in. It is `HttpOnly`, `SameSite=Lax`,
  `Secure` as in 12.8, and `Path=/signin/oidc`. A callback whose cookie is
  missing or does not match is refused and cancels no one's sign-in. The
  `next` address passes the same check as after a local sign-in (14.17).
- **Limits (12.11).** A failed callback counts against the client's
  address, but only once it names a sign-in this browser started, so a
  hostile page that makes a browser load the callback cannot lock the
  address out. A success gives its count back. An address may start 20
  sign-ins a minute and gets a 429 page beyond that. A refused sign-in is
  logged as in 14.14.
- **Calls to the provider (14.10).** Foreman fetches the provider's
  discovery document the first time someone signs in, never at startup,
  keeps it once it works, and tries again at the next sign-in after a
  failure. While one fetch is under way, and for 10 seconds after one
  fails, a sign-in gets the "unavailable" page (503) at once. A discovery
  document that names an authorize, token or keys address that is not
  `https` is refused.
- **Matching a user.** A user is found by the provider's issuer and the
  `sub` claim, never by name alone, so a rename at the provider keeps the
  account. On first sight, Foreman links to an existing user of the same
  name only when that user has no password and no link, which means a user
  made by trusted headers. The link is made once and cannot move. Any other
  name clash refuses the sign-in with a page that says why (a 403 page).
  Otherwise the user is created, as the first admin when there are no users
  yet, and `user-create` is recorded with the detail `oidc` (12.9).
  **Accepted risk (Todd's ruling, 2026-11-18; ADR 0027):** until a
  headers-made user has signed in through OIDC once, anyone in the group
  who may rename themselves at the provider could take that name and the
  account. The operator guide tells operators to turn off self-service
  renaming at the provider.
- **After sign-in.** A normal session starts (12.8). A user outside the
  required group gets a 403 page. Foreman checks the group only at
  sign-in: removing someone from the group stops their next sign-in, not
  a session already open. Delete the user (12.12) to end it now. Signing
  out ends only Foreman's session. Two tabs that start sign-in at once
  overwrite each other's cookie, so the first tab's callback fails safe
  with a page that offers "Try single sign-on again".
- **The pages.** With `oidc` alone, the sign-in page is one "Sign in with
  single sign-on" button, there is no setup page and no password form
  (12.3), and the signed-out page offers the same button. With `local` too,
  the page shows both, the single sign-on button first and the only primary
  button. Each refusal page says what happened and offers a button: "Try
  single sign-on again" for a failure or an unavailable provider, "Back to
  sign in" for the others.
- **With trusted headers (12.5)** the proxy still comes first: a request it
  signs in never goes to the provider.

12.8. Sessions are server-side, expire after a configurable idle time,
and end on sign-out. The default idle time is 7 days
(`FOREMAN_SESSION_IDLE`). Session cookies are `HttpOnly`, `SameSite=Lax`,
and `Secure` when `FOREMAN_PUBLIC_URL` starts with `https://`. Users who
arrive through trusted headers get no session cookie: the proxy decides
who is signed in, and signing out of Foreman ends nothing for them. (P0)

12.9. **Audit log**: who did what and when, for start, stop, restart,
switch, snapshot and restore, generate and clone, mod changes, settings
changes, user changes, and every RCON command. Viewable and filterable
in the UI (below). Never holds a secret. Each entry holds the time, the user
(the name stays on the entry after the user is deleted), the action, the
world, a detail and, for anything done through an API token, the token's
name (12.10). Server control records start, stop, restart, forced stop,
switch (on the world switched from, with both ids), game image download
and every console command (8.4), every player action and chat message
(8.2, 8.5), a change to the whitelist switch (8.3), and a change to the
restart schedule or to a webhook (7.12, 8.10), never with the webhook's
address, a world file export and import as `world-export` and
`world-import` (5.13, 5.14), the autosave tidy (11.6), and turning the public mods page on
or off (9.16). A manual snapshot and a restore are recorded as `snapshot`
and `restore`, with the token's name when they come through MCP. Saving a
new world's map settings (5.6) records `new-map`, whose detail names the
preset (or a map exchange string), whether it was modified, and ends with
the world's game version. API
tokens record `token-create`, `token-renew` and
`token-revoke`, each naming the token, its owner and its scope. Turning
MCP access on or off records `mcp-set` with the detail "on" or "off"
(21.2), and REST API access `api-set` the same way (23.8). An action through MCP is recorded exactly as the same action in
the UI: same action name, same before-or-after rule, plus the token's
name. Read tools change nothing and are not recorded. The token's "last
used" time covers them (12.10). Player actions, chat messages and the
whitelist switch are recorded before they take effect and are refused if
the record cannot be written. Deleting a world records `world-delete`
with the world's name and whether a final snapshot was taken; it too is
recorded before the delete, which is refused if the record cannot be
written. (Todd's ruling, 2026-11-17.) Reading a copy of a save (8.16)
and the factory map mode (5.16) are not recorded, since neither changes a
file the game uses. Deleting a user records `user-delete`
the same way (12.12). A schedule or webhook change is recorded
after it, and a failure to record it is only logged. (P1, except RCON
commands, which are recorded from the first version that has a console:
P0)

- **The actions Epic 11a adds.** `world-archive` and `world-unarchive`
  (5.9). `user-create`, with the detail `setup`, `headers` or `oidc`
  (12.3, 12.5, 12.7). `user-delete`, with the detail
  `<name>: requested; API tokens: <n>`, recorded before the delete.
  `password-change`, with no detail (12.12). A change on the General tab
  (13.1) records `snapshot-retention-set`, `stop-countdown-set`,
  `audit-retention-set`, `save-upload-limit-set` or `mod-upload-limit-set`,
  with the detail "from A to B" (nothing is recorded for a value that did
  not change). An action through MCP carries the token's name as always.
- **The viewer.** The audit log is a Settings tab, "Audit log", at
  `/settings/audit-log`. It lists entries newest first in a table with the
  columns Time, User, API token, Action, World and Detail, 50 a page by
  default (25, 100 or 200 by choice), with Newer and Older links. The
  filters and the page size carry across pages. The filters (user, action
  and world, each an exact match picked from the values the log holds) and
  the page size sit on one row. At a narrow width each row stacks into a
  block instead of scrolling sideways. Each entry shows its time in the
  viewer's zone (17.11), the user, the token's name (empty when there is
  none), the action in words (or as recorded when no words exist), the
  world (its current name as a link, or its id marked deleted) and the
  detail, escaped. Console arguments are masked when recorded, so
  they stay masked. (Todd's rulings, 2026-11-18.) (P1)
- **Retention.** The log keeps 365 days by default. The setting takes 7 to
  3650 days (13.1). An hourly sweep deletes older entries. The sweep is not
  recorded; a change to the setting is. If the setting cannot be read, or
  holds a value out of range, the sweep deletes nothing.

12.10. **API tokens.** A personal API token lets a program, such as an
AI assistant or a script, act as the user who made it. Only the MCP
endpoint (section 21) and the REST API (section 23) accept one. (P1)

- **Creating.** On the API tokens page (`/settings/api-tokens`), a
  signed-in user names a token and picks its scope. Read is picked by
  default. A name is 1 to 32 characters of letters, digits, spaces, `.`,
  `_` and `-`. It must differ from the names of the owner's other tokens
  that are not revoked. The secret is shown once, on the page that
  answers the create, beside the MCP endpoint's address (21.1) and the
  REST API's base address (23.1). These are `FOREMAN_PUBLIC_URL` plus
  `/mcp` and `/api/v1` when it is set. Otherwise they use
  the request's host, and takes its scheme from `X-Forwarded-Proto` only
  when the request comes from a trusted proxy (12.5). Foreman stores
  only its hash (14.19).
- **Scope**, fixed for the token's life, each including the one before:
  - **read**: the read tools (21.4);
  - **operate**: also the action tools (21.5) and chat (21.6);
  - **admin**: also the admin tools (21.7). These cover everything an
    admin can do in the UI except managing users, API tokens and MCP
    access (21.2), plus the exclusions in 21.7.

  A token never does more than its owner may do at the time of the call
  (12.1).
- **States.** **Valid**: accepted. **Expired**: its time ran out. It
  is refused, and its owner can renew it. **Revoked**: ended by its
  owner or an admin. It is refused, and this is final: it can never be
  renewed.
- **Period.** Every token, whatever its scope, expires 30 days after it
  was created or last renewed. This is not a setting.
- **Renewing** restarts the 30 days from now and keeps the same secret,
  so the program needs no change. Only the owner renews, from a browser
  signed in by any method of 12.2, never through an API token. So a
  token cannot renew itself. An Expired token stays renewable for as
  long as it exists and is never deleted automatically.
- **Revoking.** The owner or any admin revokes a token. It takes effect
  for every request whose token is checked after the revoke. A request
  is checked again after its body arrives, so one that holds its body
  back is refused too.
- **Who sees what.** Every admin sees every token: its owner, name,
  scope, state, expiry, creation time and last use (to the minute).
  Never the secret or its hash. The owner renews their own tokens. Any
  admin revokes any token. In v1 every user is an admin (12.1).
- **Reminder.** From 5 days before a token expires, its owner sees a
  banner on every signed-in page. The banner names the token and when it
  expires, and has a Renew button that renews in one click (a form post,
  14.7). It stays while the token is Expired, until the token is renewed
  or revoked. Each such token has its own line. The `token-expiring`
  webhook event (8.10) is sent at 5 days and at 1 day before expiry.
- **Limits.** Tokens are rate-limited (14.19). A token cannot sign in to
  the UI: pages never read the `Authorization` header.
- **Audit.** Everything a token changes is recorded under its owner and
  the token's name (12.9).
- **Storage.** Each token row holds its owner, name, scope, secret hash,
  creation time, the start of its current period, the time it was
  revoked (if it was), its last use, and which reminders were sent in
  the current period. The state is worked out from these fields, never
  stored. A user's tokens are deleted with the user. Audit entries keep
  both names.

12.11. **Sign-in limits.** A username is allowed 5 failed attempts and an
address 20, in a fixed 15-minute window that starts at the first failure.
Usernames are compared without regard to case, in ASCII only, to match the
database. IPv6 addresses are counted by their /64, since one host usually
holds a whole /64. Each attempt is counted before its password is checked,
so a burst of parallel requests cannot get past the limit; a success
clears the username's count. At most 4 password checks run at once, and a
sign-in beyond that gets a 503 page saying Foreman is busy, with
`Retry-After: 1`. The counts are in memory and start again when Foreman
restarts. Because the limit is per username, anyone who can reach the
sign-in page can keep a known username locked out by failing on purpose;
the per-address limit does not prevent this. (P0)

12.12. **Users.** The Users tab (`/settings/users`) lists every user, oldest
first, with how they sign in (password, proxy or single sign-on) and when
they were created. (P1)

- **Deleting.** Any user may delete another user, from a confirm page
  (`/settings/users/{id}/delete`) that names the user and how many API
  tokens will go with them, and has one danger button and no typed name,
  since nothing on disk is lost. Deleting yourself is refused, so the last
  user can never be deleted and nobody locks everyone out through the UI.
  The delete also requires that the deleting user still exists, so two
  users cannot delete each other. `user-delete` is recorded before the
  delete, and the delete is refused if the record cannot be written. The
  user's sessions and API tokens go with them (12.10): their next request
  is signed out, their live streams end at the next heartbeat, and
  `/mcp` answers 401 for their tokens. Old audit entries keep the name
  (12.9). The result message is the same whoever was deleted.
- **A deleted proxy or single sign-on user** is created again at their next
  request while the proxy or the provider still vouches for them. There is
  no block list. The confirm page says to remove them at the proxy or the
  provider to keep them out. (Todd's ruling, 2026-11-18.)
- **Changing a password.** Only a user with a password may change it, and
  only their own: the current password, the new one twice, at least 12
  characters (12.4), hashed as 12.4 says. A wrong current password counts in
  the sign-in limit (12.11), and once the limit is reached even the right
  password is refused. The new hash and the end of the user's sessions are
  one transaction. A change ends every session of the user and starts a
  fresh one for the browser that made it, and is recorded as
  `password-change` with no detail.
- **Proxy and single sign-on users** have no password and see no form to set
  one, only a line saying their sign-in is managed by the proxy or the
  provider. A posted form from them is refused, because a password would let
  them past the proxy.
- **No reset in the UI.** A forgotten password is reset on the host with
  `foreman reset-password <username>`, run with `docker exec -i`. It reads
  the first line of standard input as the new password (at least 12
  characters), never prints it, ends that user's sessions and refuses a
  user without a password. It is recorded as `password-change` by `system`,
  with the user's name as the detail. (Todd's ruling, 2026-11-18.)
- Users are not in MCP (21.7).

## 13. Foreman's own settings

13.1. App settings an admin changes in the UI: snapshot retention, the
default stop countdown, audit retention, the two upload limits and the
update check (the General tab, below), scheduled restarts, notifications, the public
mods page (9.16), MCP access (21.2), and portal credentials when not set
by environment. Each shows where its value comes from (environment or
UI), and environment values cannot be changed in the UI. (P0 for those
whose features are P0)

Foreman's settings are eight pages under Settings, listed in the Settings
area's list of pages (17.18), in this order: General, Restart schedule,
Notifications, Public mods, API tokens, Users (12.12), Audit log (12.9) and
the factorio.com account. The Settings link in the main navigation opens
General.

**The General tab** (`/settings/general`) holds five number settings and
one switch. None has an
environment form, so each says "set here". A bad value refuses the whole
form and stores nothing. (Todd's rulings, 2026-11-18.)

| Setting | Range | Default | Audit action |
|---|---|---|---|
| Snapshot retention (6.6) | 1 to 100 | 10 | `snapshot-retention-set` |
| Default stop countdown (7.6), seconds | 0 to 600 | 60 | `stop-countdown-set` |
| Audit retention (12.9), days | 7 to 3650 | 365 | `audit-retention-set` |
| Save upload limit (14.6), MB | 100 to 8192 | 1024 | `save-upload-limit-set` |
| Mod upload limit (14.6, 14.10), MB | 10 to 2048 | 1024 | `mod-upload-limit-set` |
| Check GitHub for new Foreman versions (14.10) | on or off | on | `update-check-set` |

A scheduled restart keeps its fixed countdown (7.12). The upload limits are
in whole MB, meaning MiB, as the pages say them. A stored value that is
missing, unreadable or out of range gives the default. The switch is a
checkbox; its audit detail reads "from on to off" or "from off to on".
Turning it off stops the check and hides any notice at once; turning it
on checks at once, in the background. These settings are also read and
changed through MCP (21.7, 21.8). (P1)

## 14. Security

This section lists the trust boundaries the security-reviewer agent
reviews against.

14.1. **Trust zones.** Each is treated as hostile to its neighbours:

| Zone | What it is | Trusted for |
|---|---|---|
| Browser | An admin's browser | Nothing until signed in; may be driven by a hostile page |
| Reverse proxy | Forward-auth proxy in front of Foreman | Identity headers, only from configured ranges |
| Foreman | The app and its data directory | Everything it holds |
| Docker API | The daemon, directly or through a socket proxy | Only the calls in 14.3 |
| Game container | Factorio and its RCON port | Nothing; its logs and files are input |
| VPN sidecar | An optional container the operator runs, whose network the game shares (4.13) | Nothing; Foreman did not create it (14.16) |
| Data directory on the host | Worlds, snapshots, database | Files Foreman wrote itself |
| Identity provider | The OIDC provider an operator configured (12.7) | Only the ID tokens it signs for this client; nothing else it sends |
| Internet | Mod portal, image registry and its tag list, release list, GitHub's latest Foreman release, webhook targets | Nothing |
| AI client | A program, usually an AI assistant, calling the MCP endpoint with an API token (section 21) | Only its token's scope; steerable by text it reads (14.12) |
| Player | Anyone who can reach the public mods page (9.16) | Nothing; may be a script |

14.2. **Docker access is root on the host.** Whoever controls Foreman's
Docker access controls the host. A socket proxy narrows which endpoints
Foreman can call, but common proxies allow by path prefix and cannot
inspect a container-create request, so they cannot stop a privileged
container being created. The operator guide says this plainly, and shows
a socket proxy configuration that permits the smallest set the proxy can
express. Foreman's own guarantee is 14.4. (P0)

14.3. **Docker API surface.** Foreman calls only these Docker Engine API
endpoints:

- `GET /_ping`, `GET /version`, `GET /containers/json`,
  `GET /containers/{id}/json`, `POST /containers/{id}/start`,
  `POST /containers/{id}/stop`, `POST /containers/{id}/kill` (forced stop
  only), `GET /containers/{id}/logs`, `GET /containers/{id}/stats`, and
  optionally `GET /events` filtered to containers. `GET /events` is
  optional: Foreman uses it when the proxy allows it, to name an
  out-of-memory kill (8.7), and polls as the fallback. A 401 or 403 means
  events are off until Foreman restarts; any other failure retries from 1 to
  30 seconds. Docker's operating system is read from the `Os` field of
  `GET /version`.
- `POST /images/create` (pull an exact tag), `GET /images/{name}/json`,
  `POST /containers/create`, and `DELETE /containers/{id}`.

Never: exec, build, volumes, networks, image delete, or anything under
`/system` or `/auth`. Adding an endpoint changes this section first. The
client refuses any path with a `.` or `..` segment, and refuses every
redirect, so a daemon or proxy cannot send a request to a path this list
never checked. Only the path is checked against this list; query strings
(filters, `since`, `follow`) and JSON request bodies are allowed. Calls that
last long (log follow, image pull, stop) have no overall time limit and
set their own deadline. (P0)

The map preview (5.6) uses these same endpoints and no others. It creates
a one-off container from the world's game image, never pulling it (3.7),
starts it, polls it, reads the end of its log when it fails (no page shows
it), and deletes it with `DELETE` and `force`. The container runs as the game's user with
no network, a read-only root, every capability dropped, `no-new-privileges`,
no restart, a memory limit of 1 GiB with no swap, the game's write folder
on a size-limited tmpfs and a 60-second limit, after which it is deleted.
Its only bind mount is a fresh folder under the data directory, never a
world's folder, which is deleted afterwards. One runs at a time. A request
waits up to 20 seconds for its turn and is then refused; at most 8 wait,
and one more is refused at once. A request whose browser goes away stops
waiting, and if its preview is already running, the container is stopped
and deleted with its folder. A container whose create call failed is
deleted by its name. A leftover container (found by label) or folder from a crash is removed at
the start of the next preview, not at startup. The preview's 1 GiB covers
up to 4096 tiles (the measured peak was about 553 MiB and the slowest run
under 6 seconds), which leaves headroom over the 3072 tiles the previews
now stop at.

For a world's step 3 (5.6), the preview, territory and resource containers
load that world's mods (ADR 0033). The work folder then also holds the
world's mods by hard link, falling back to copies, under the same caps as
the mod settings run below, read-only where the game would otherwise write,
and copies of the world's `mod-list.json` and `mod-settings.dat`. It never
holds the world's own folder. A world mod with the name of the run's
control-only mod refuses the run. The planets a run may draw are checked
against the planet list Foreman took from that world's stored run, never
against anything the form posted. The check uses the Space Age the world's
enabled mods give, the same answer the planet list uses, so a world whose
flag disagrees with its mods is not refused. The limits stay 1 GiB, and the time
limit is 60 seconds for each planet drawn, at most 10 minutes in all so
a run cannot hold the one slot for long: with five planet mods, one
planet took 2 to 4 seconds and all eleven 24 to 35 seconds, under 750 MiB
(#775).

Demolisher territories (5.6) use a second kind of preview container,
`foreman-territories-<folder>`, with the same label, endpoints, host
settings, limits, slot and cleanup. Its work folder holds a small mod with
only a control script, and the game runs one `--create` with the
preview's seed, preset and settings files, and no server. The script reads
the territories when the map is created, writes them as JSON with
`helpers.write_file`, and the game exits. Foreman reads that file through
the work folder, at most 2 MiB and 1,000 territories, and checks every
entry, because the game container is untrusted (14.1). RCON is not used:
the container has no network, the image has no RCON client, and exec is
forbidden above. A control-only mod adds no prototypes, so the map is the
same as the picture's.

Resource amounts (5.6) use a third kind, `foreman-resources-<folder>`, in
the same way: the same label, endpoints, host settings, limits, slot and
cleanup, and a small mod with only a control script (it shares the code
that writes and reads such a mod with the territory run). The script
counts one planet, taken only from the world's own planet list and never
from user text, and writes the patches as JSON; Foreman reads that file
through the work folder and checks every entry under the limits in 14.6.
No new Docker endpoint is used.

Mod settings (9.6) use a fourth kind, `foreman-modsettings-<folder>`, in
the same way: the same label, endpoints, host settings, slot and cleanup,
with a memory limit of 4 GiB and a time limit of 5 minutes. It runs the
world's own image with a small control-only mod that writes every setting's
definition as JSON, and the game writes its defaults `mod-settings.dat`
beside it. Its work folder holds the world's mods by hard link, falling back
to copies (at most 100,000 files and 4 GiB), and never the world's own
folder or its `mod-settings.dat`. A world mod named `foreman-modsettings`
refuses the run. Foreman reads the output through the work folder and checks
it under the limits in 14.6.

Reading a copy of a save (8.16) uses a fifth and a sixth kind,
`foreman-saveplayers-<folder>` for the players of a save (8.14) and
`foreman-factorymap-<folder>` for the factory map (5.16), in the same way:
the same label, endpoints, host settings, slot and cleanup, with a memory
limit of 12 GiB with no swap and a time limit of 5 minutes. Each runs the
world's own image in benchmark mode with a control-only helper mod. Its
work folder holds the save as a hard link mounted read-only over the link
(a copy when the link fails), the world's mods as for the fourth kind, and
copies of `mod-list.json` and `mod-settings.dat`; never `config/`,
`rconpw` or `server-settings.json`. The game's write folder is a 1 GiB
in-memory folder, counted against the memory limit, so the game never
writes to the host's disk; only the helper's named output is moved into the
work folder. A world mod
with the helper's name refuses the run. No new Docker endpoint is used.

The real-game picture (5.6) uses a seventh kind, `foreman-chart-<folder>`,
on 2.1 only, with the same label, endpoints, host settings, slot and
cleanup, a memory limit of 1 GiB with no swap and a time limit of 90
seconds in all. Its work folder holds the world's mods as for the preview
above, the step's settings files and a control-only helper mod. The game
runs `--create` to make a throwaway map, then `--test-save` on it, which
loads it with a player; the helper creates the planet's surface, so the
mods' own surface and chunk scripts run, charts the chunks within 16 of
the origin (1024 by 1024 tiles) and writes the chart. Foreman reads it
through the work folder, checks it (one planet per run, at most 1,024
chunks of exactly 32 by 32 pixels, the file at most 4 MiB; anything else
refuses the picture, 14.6), and draws it as the factory map does (5.16). Measured with five planet mods on 2.1.24: 13 to 21 seconds for one
mod's planets and 39 seconds for all five, at most 692 MiB (#775). No new
Docker endpoint is used.

To share a network with the game (4.4), Foreman reads its own container
with `GET /containers/{id}/json`, which gives the networks it is on and
their address ranges. It names the game network in the
`POST /containers/create` request, and lists the containers on that network
with `GET /containers/json` and a network filter, to check that only
Foreman and its own games are on it. It never creates or connects networks;
the network comes from the admin's compose file. Outside a container it
makes none of these checks and uses Docker's default `bridge`. With a VPN
sidecar (4.13) it reads the sidecar with `GET /containers/{id}/json` to
check its state and networks and to learn their address ranges, and names
it in the `POST /containers/create` request as the game's network mode.

14.4. **What Foreman will never do with Docker**: create a privileged
container, use the host network or PID namespace, add capabilities, mount
the Docker socket into the game, or bind-mount any host path outside the
data directory. (P0)

14.5. **Paths from users.** Every path built from user input (world names,
save names, snapshot folder names, uploaded zip entries, world file
entries, mod file names) is resolved and checked
to stay inside the intended directory before use. Symlinks inside a world
are not followed out of it. Uploaded zips are checked entry by entry and
refused if any entry would land outside its target. (P0)

14.6. **Uploads** have size limits (app settings on the General tab, 13.1:
a save 100 MB to 8 GB, default 1 GB; a mod 10 MB to 2 GB, default 1 GB;
each a whole number of MB, meaning MiB) and are written to a temporary file and checked
before they replace anything. Pasted text has limits too: a map exchange
string is at most 64 KB and inflates to at most 1 MB (5.6), and a settings
or map-gen JSON text, including the raw editors' and the files written to a
world's `config/`, is at most 1 MB and must be one JSON object (11.2). A
map preview picture is at most 16 MiB: at 4096 tiles Gleba measured 8.8
MB and Vulcanus 8.5 MB, over the old 8 MB cap, and every other planet
under 6 MB. Previews stop at 3072 tiles, so the cap has headroom. Foreman reads the game's output (pictures, the territory file and the
resource file) only from regular files, so a named pipe left by the game cannot
hang the read. The resource file (5.6) is at most 512 KiB, 5,000 patches and 64
resources, against a measured most of 98 KB, 1,113 patches and 6
resources. Every number is a whole number from 0 to 2^53 (a string is
refused, and a number written with more than 32 bytes is refused before it
is read), names match `^[a-z0-9_-]{1,64}$`, unknown fields are refused,
every box lies inside the picture and its label tile inside the box, each
patch's resource is listed, a resource's `infinite` flag agrees with its
normal amount, and the patch amounts add up to the resource's total. Each
resource carries its map colour as three whole 0 to 255 values, as the game
reads them; the real-game test uses it to check that a label tile lies on
that colour in the preview picture (5.6). An error names counts, never
text from the file. Drawing territories into a download runs one at a time,
with at most 4 waiting; one more is refused with "busy". A mod zip that
lists more than 10,000 entries is left unread: the entries are counted from
the zip's end record before its directory is built, so a crafted zip costs
almost nothing to refuse. The mod settings definitions file (9.6) is at
most 2 MiB, 10,000 settings and 1,000 allowed values each; unknown fields
are ignored and an unknown setting type is shown as not editable. The
planets file of the same run (9.6) is at most 1 MiB, 64 planets, 512
controls, 128 controls per planet and 512 resources; over any of these
the whole file is refused and step 3 uses the embedded planets (5.6).
Planet, control and resource names must match `^[a-z0-9_-]{1,64}$`; one
that does not is left out, and the page gives how many were. A control
with an unknown category or an over-long order is left out. A planet's
own default for a control is kept only when it is a finite number from 0
to 1000, else it is ignored. Invisible Unicode (zero-width and direction
marks) is removed from every label. Localised
names resolve under the 20-level and 1,000-character limits above, then
lose their rich-text tags and are cut to 100 characters. A mod's
`.cfg` locale file is at most 1 MiB, and 8 MiB per language across mods. A
string setting value is at most 10,000 bytes. A mod or `mod-settings.dat` upload waits in a
staging file at the top of the worlds folder, and a staged mod nobody
applied is removed an hour later. A favorite file is at most 8 MiB, is read in
memory and is checked in full before anything is written (5.12). A world
file (5.14) is at most 64 GiB, a fixed limit, and the disk check of 6.11
runs twice: on the upload's length, then on the sum of its unpacked sizes
before anything is extracted. It may list at most 100,000 entries, counted
from the end record. Each entry name must be `foreman-world.json`,
`world.json`, `saves/<save>` with a valid save name, `config/<file>` for a
5.1 config file other than `rconpw`, or `mods/<path>` with no `.` or `..`
segment, empty segment or backslash; any other entry, a repeated name or a
link refuses the whole file. Each entry is limited while it is read:
`foreman-world.json` 1 KiB, `world.json` 64 KiB, each config file 1 MiB,
`mod-settings.dat` as above, a save the save upload limit, and a mod file
the mod upload limit. The unpacked total is at most 50 times the file. The
players output of a save read (8.16) is at most 1 MiB, 10,000 players and
1,000 mods, with names under the 8.2 rule and unknown fields refused. The
factory map output (5.16) is at most 512 MiB in all, 300 surfaces, 65,536
charted chunks a surface and 2,000,000 entities in all, and each decoded
chunk must be exactly 32 by 32 pixels. In both, every number is a whole
number from 0 to 2^53. A save whose
name ends in `.tmp.zip` is refused. The upload limits are read for each
request, so a change applies to the next upload, and the drop zones, hints
and refusals show the current figure. The limits of the `mod-settings.dat`
file, a favorite file and what a mod zip may unpack to stay fixed. A mod zip
is never opened above 2 GB, the setting's largest value; the setting is the
real limit. **Every upload uses one drop zone**
(DESIGN 7.10): its checks of type, count and size only answer sooner, and the
server checks every upload as before. An upload happens in place when its
result is used on that page; otherwise a link marked with a chevron goes to
the tab that owns the file (the Mods tab to Saves, a world's mod settings
to Mods, an empty Worlds list to Import). A link takes you somewhere and a
button does something, so such a link is never drawn as a button. A link
that leaves Foreman opens in a new tab and ends in `↗`; both marks are
drawn by the page's styles and are not part of the link's name. Every link
that opens a new tab points off the site, and a test checks it. (P0)

14.7. **Requests that change state** require a signed-in session and
protection against cross-site request forgery. A browser post is
accepted only from Foreman's own origin, or from the origin of
`FOREMAN_PUBLIC_URL` when that is set, which covers a proxy that
rewrites the `Host` header. The MCP endpoint (`/mcp`) and the REST API
(`/api/`) are the exceptions. Each takes only a bearer API token and never reads a cookie or
trusted header. A browser does not send that header on its own, so a
forged cross-site request carries no identity. It still refuses a
request whose `Origin` header is present and is not one of the origins
above (14.19). (P0)

14.8. **Secrets** (the RCON password, the portal token, the OIDC client
secret, the game password, the factorio.com `username`, `password` and
`token` keys of `server-settings.json`, `FOREMAN_SECRET_KEY`, webhook
URLs, session tokens, and API token secrets) never appear in a log line,
an API response, an error message, the audit log, or the UI. This
includes game log lines Foreman relays: it removes the RCON password,
the game password and those factorio.com values from every line (8.6),
parsing the line before it redacts so a short password cannot hide what
the line means. The console follows one list of commands that carry a
secret: `/config set password` has its argument masked in the audit log,
on the page and in relayed log lines, and the reply to `/config get
password` is masked. A Lua command is recorded as typed (8.4). The one
place the RCON password is visible is the game process's arguments, an
accepted risk (4.5).

A webhook URL often carries a token in its path, so it is stored sealed
with `FOREMAN_SECRET_KEY`, as the stored factorio.com account is (ADR 0013,
ADR 0018). A Pushover webhook's two keys are kept in the same sealed value
and are never put in a request address. Pages show only the preset and the
host, and changing a URL means typing it again. Without the key, webhooks are unavailable. (P0)

14.9. **RCON input.** Commands built by Foreman from user text (chat
messages, player names, reasons) are refused, never escaped, when the text
could become a different command: a name with a space, a control or
invisible character or over 64 bytes (8.2), a reason or chat message with
a control character or line break, and a chat message that starts with `/`
(8.5). The raw console (8.4) sends what the admin typed, by design. (P0)

14.10. **Outbound requests.** Foreman talks only to the mod portal, the
factorio.com release list, Docker Hub's list of `factoriotools/factorio`
tags (`hub.docker.com`, 10.7), GitHub's latest Foreman release
(`api.github.com`, below), and webhook URLs an admin configured. Calls
to the release list and to Docker Hub follow no redirects, take at most 10
seconds, and read at most 2 MB (tag list) or 64 KB (release list). The
size of a game image is one request for a single tag, read at most 64 KB and
limited to 3 seconds, made only when the image is missing and a page says
how big the download is; any failure leaves the size out. Image
pulls are made by the Docker daemon, not by Foreman. It does not follow
redirects for webhooks, and each try takes at most 5 seconds (8.12).
The update check asks
`https://api.github.com/repos/toddawhittaker/foreman/releases/latest`
about one minute after start and then every 6 hours, and at once when an
admin turns its switch on (13.1). It never runs for a build without a
release version, or while the switch is off. It follows no redirects,
takes at most 10 seconds, reads at most 64 KB, and sends a `User-Agent` of
"Foreman/" and its version, nothing about the user. Only `tag_name` is
read; it is cut to version characters and must be major.minor.patch, and
About links to the release page built from that version, never to an
address GitHub sent. The answer is kept in memory only. A failure logs one
warning line and shows nothing. About names the release only when it is
strictly newer than the running build's major.minor.patch (P1).
The mod portal's base is fixed at `https://mods.factorio.com`; there is no
setting for it. Its API calls take at most 10 seconds, follow no redirects,
and read at most 16 MB (the mod list) or 2 MB (one mod's details). At most
4 mod detail requests run at once. A download link is followed only when it
starts with `/download/` on that base, and its `file_name` must equal
`<name>_<version>.zip`. A download takes at most 5 minutes, reads at most
the mod upload limit (14.6), so a mod that can be uploaded can also be
downloaded again. A file over that limit is refused as its own failure,
not as an unreachable portal: before any byte is read when the portal
declares a `Content-Length` over the limit, and by cutting the stream off at
the limit when it does not. The page names the mod, its size when known and
the limit; an admin gets a link to the limit on the General tab, anyone else
is told to ask an admin, and there is no "Try again", since the file is as
large next time. Every failed mod download, for any reason, logs one line
with the mod, version and reason, never the username or token. A download
follows at most 2 redirects, each only to an `https` address
whose host ends in `.factorio.com`. The real portal redirects to
`dl-mod.factorio.com`. The redirect carries no `Referer` header, because Go
would copy the token-bearing address into it. An error message never holds
the request's query string, because the token rides in it (14.8).
A mod plan request names at most 500 mods and ticks at most 500 optional
ones, so one request cannot start unbounded portal calls. A plan page
opened by a request marked cross-site or same-site in `Sec-Fetch-Site` is
refused with a page that offers "Try again", and asks the portal nothing.
The OIDC provider (12.7) is the one other address Foreman calls, and only
when someone signs in. Its calls take at most 10 seconds, follow no
redirects, and read at most 64 KiB (the discovery document) or 1 MiB (the
signing keys and the token answer). Its address must be `https`. (P0)

14.11. **Least privilege.** Foreman's container runs as non-root with a
read-only root filesystem except its data directory and a temporary
directory. The game container is also unprivileged (4.6): a game user or
group ID of 0 is refused at start (3.6). (P0)

14.12. **Token clients.** An AI client connected through MCP (section 21)
is its own trust zone, and so is a program on the REST API (section 23),
which gets the same tools and the same untrusted fields. It is trusted only for its token's scope and
assumed to be steerable by any text it reads. Players control chat,
player names and much of the game log. The mod portal controls mod
titles, summaries and changelogs. Either can write instructions aimed at
the AI. Every tool result keeps such text in fields marked as untrusted
content, apart from Foreman's own words, and each tool's description
says which fields those are. Besides player names, chat and log lines,
those fields hold the label of a snapshot written through a token, mod
names, and the problems a pre-start check finds. A token's scope limits
the harm (12.10). A read token changes nothing. An operate token cannot
delete, overwrite or run raw commands, and it cannot fill a world with
snapshots: a world holds at most 10 snapshots taken through API tokens
(21.5). An admin token's destructive actions need the two-step confirm
(21.8). (P1)

14.13. **Pages refuse framing.** Every page carries
`Content-Security-Policy: frame-ancestors 'none'` and
`X-Frame-Options: DENY`, so no other site can show Foreman inside a frame
and trick a click. (P0)

14.14. **Sign-in logs name the client, never the username.** A failed or
refused sign-in is logged with the client address and no username,
because people sometimes type a password into the username field. A
refused single sign-on (12.7) is logged with the address and a fixed
reason, never a claim, a code, a state or a token. (P0)

14.15. (Removed: adopted mode was dropped; ADR 0005.)

14.16. **The VPN sidecar.** **Accepted risk:** the sidecar (4.13) is a
container Foreman did not create. It sees all game traffic and usually
holds `NET_ADMIN`; Foreman does not police its capabilities. The game
inherits access to its additional Docker networks, reachable VPN peers
and loopback services. The sidecar and any namespace peers are trusted.
Operators own the sidecar's routes, forwarding, services and VPN access
rules, and must not expose the Docker API/socket proxy through it.
RCON binds only the game-network address, not the VPN interface; routing
VPN traffic to that address can still expose RCON, which remains password
protected. All attached Docker ranges count as game ranges for trusted
headers and are checked for proxy overlap and configured Docker API/socket
proxy exposure. VPN ranges are not discoverable through Docker (12.6).
A start is refused when the sidecar is missing, not running, on the host
or another container's network, or off the game network (4.13). Additional
Docker networks alone do not cause refusal. The game keeps the shape in
4.6. (P1)

14.17. **Return after sign-in.** A signed-out visit to a page goes to the
sign-in page with the page asked for in a `next` value. After sign-in
Foreman sends the browser there only if the value is a local path: it
starts with exactly one `/`, has no backslash or control character, has no
scheme or host, is at most 2000 bytes, and is not the sign-in, setup,
signed-out or sign-out page. Anything else goes to the home page. The
value is checked when the sign-in page is shown and again when the form is
posted, and it survives a wrong password. A POST or a partial-page
(htmx) request is never given one. Trusted-header sign-in (12.5) needs no
return path, because the proxy vouches for every request. The setup page
carries none. (P0)

14.18. **The public mods page.** Every line is a condition for shipping
9.16. (P1)

- **Paths.** Foreman never builds a file path from the request. `<world>`
  must equal the id of a world whose game is running. `<file>` must equal,
  byte for byte, the file name of an enabled, non-built-in zip in that
  world's mod list at the time of the request. The file is opened through
  the world's root (14.5), must be a regular file and not a symlink, and
  passes the same-world check a save download uses. Anything else gets a
  plain 404.
- **Limits.** At most 4 public downloads run at once across all visitors;
  the next gets 503 with `Retry-After: 5`. A download write that makes no
  progress for 60 seconds is cut off (the idle limit of 14.6); there is
  no overall deadline. The page itself keeps the server's normal
  timeouts. Only GET is allowed: HEAD and every other method get 405,
  except `POST /public/language`, whose body is capped at 64 KB.
- **No identity.** A public route never reads a session, trusted headers
  or an API token, never creates a user, and sets no cookie except the
  language cookie. Responses, `/public/static/` files included, carry
  `Cache-Control: no-cache`,
  `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and
  the frame refusal of 14.13.
- **Forward-auth proxy.** The operator guide says to let requests whose
  path starts with `/public/` reach Foreman without authentication and to
  keep every other path behind the proxy. It names no particular proxy.
  Without that rule the page asks for sign-in at the proxy.
- **Accepted risks.** A stranger can hold the 4 download slots; turning
  the page off ends that. Anyone who can reach the page learns which world
  runs and which mods it uses; the admin chose that by turning it on.
- **Logging.** The access log records each public request as it does any
  other, with the path and no query string (15.1). Downloads are not
  audited.


14.19. **API tokens, the MCP endpoint and the REST API.** Every line is
a condition for shipping sections 21 and 23. (P1)

- **Format.** A secret is `fmn_` followed by 43 base64url characters: 32
  bytes from the system's secure random source. The prefix lets secret
  scanners and people recognise a leaked token.
- **Storage and checking.** Only the secret's SHA-256 hash is stored, as
  for sessions (STACK 7). A request's token is hashed and looked up by
  its hash, so no secret is ever compared directly. A random 256-bit
  secret needs no slow hash.
- **Where the token goes.** Only in `Authorization: Bearer`. A token in
  a query string or a cookie is ignored. A malformed token is refused
  without a database lookup. An unknown token gets a plain 401. An
  Expired or Revoked token gets a 401 that names its state, so its owner
  knows to renew or replace it. A token is never written to a log. The
  access log records the path only (15.1). The page that shows a new
  secret is sent with `Cache-Control: no-store`.
- **Limits.** Failed token checks are limited per address with the
  numbers of 12.11: 20 in a 15-minute window, counting IPv6 by its /64.
  Each token may make 120 requests a minute (fixed window) with at most
  4 running at once. Beyond any of these limits the answer is 429 with
  `Retry-After`. Counts are in memory, and `/mcp` and `/api/` requests
  count together. A request body is at most 4 MiB. On `/mcp` it holds
  one JSON-RPC message, on `/api/` one JSON object.
- **Off means off.** While MCP access is off (21.2), every `/mcp`
  request gets 403 saying so, before its token is read. While REST API
  access is off (23.8), every `/api/` request does the same.
- **Origin.** On `/mcp` and `/api/`, any request with an `Origin` header is refused
  unless it is exactly the origin of `FOREMAN_PUBLIC_URL`. With that
  unset, any `Origin` is refused. Real MCP clients send none. The
  generic same-origin check is not enough here: under DNS rebinding (a
  page that makes its own domain point at a local address) the browser
  sends an `Origin` equal to the `Host`, and that check lets it through.
- **Confirm codes** (21.8). A code is 128 random bits. It lasts 5
  minutes, works once, and is bound to the token, the tool and the
  call's exact arguments. Any use, right or wrong, spends it. Codes are
  held in memory, so a Foreman restart voids them. A token holds at most
  10 at once, and an eleventh drops the oldest. A code is never logged.
- **Forward-auth proxies.** A proxy that demands its own sign-in blocks
  bearer clients. The operator guide says to either let `/mcp` and
  `/api/` reach
  Foreman without proxy sign-in, or point the client at Foreman directly
  on a trusted network. It names no particular proxy. Trusted headers
  are never read on `/mcp` or `/api/` (12.5).
- **Accepted risks.**
  - Renewing keeps the secret, so a leaked token must be revoked, not
    renewed.
  - A user removed at the proxy or the OIDC provider keeps their Valid
    tokens until those expire, at most 30 days later (renewing needs a
    browser sign-in), or until an admin revokes them.
  - The two-step confirm guards against mistakes, not against a steered
    client: an AI steered by player text can make both calls. The token
    page says this beside the admin scope and suggests the smallest
    scope that does the job.

## 15. Logging

15.1. Foreman writes structured logs to standard output: time, level,
event, and the world or user involved. (P0)

15.2. Logs never contain secrets (14.8), and never contain save or mod
file contents. Game log lines are shown in the UI but not copied into
Foreman's own log. (P0)

## 16. Performance and size

16.1. The image is small. A target size is set in docs/STACK.md once the
stack is chosen. (P1)

16.2. Foreman idles at low memory use; a target is set in docs/STACK.md.
(P1)

16.3. Status, player list and log pages update live without the admin
reloading. They use server-sent events. A heartbeat checks the session but
does not extend its idle time, so an open page cannot keep a session alive.
A stream for a user signed in by trusted headers ends after 5 minutes and
reconnects through the proxy, since those headers are checked only when the
connection opens. A page whose connection stays down for 5 seconds says so
and tells the user to reload. The status strip, which shows the server's
state, follows it the same way on every signed-in page. The Server page's
snapshot list is read again when its stream opens, when an operation ends and
when an action stores its result, not on every status update. (P0)

## 17. Accessibility and design

17.1. Controls Foreman owns meet WCAG 2.2 AA (the Web Content
Accessibility Guidelines, level AA). Every UI change ships with an
automated accessibility scan in its end-to-end tests. No two links on one
page share a name unless they go to the same place. (P0)

17.2. Every page works at 320 CSS pixels wide without horizontal
scrolling, with touch targets at least 24 by 24 CSS pixels. The success
scenarios in docs/VISION.md are tested at phone width. (P0)

17.3. Light and dark themes, following the system setting, with an Auto /
Light / Dark choice in the user menu, remembered per browser. (P1)

17.4. State is never shown by colour alone: running, stopped, crashed and
crash-looping each have a word. (P0)

17.5. Destructive actions say exactly what they will do and which snapshot
will be taken first, and need a confirmation. Lowering snapshot retention
(6.6) shows a confirmation page whenever the change would remove automatic
snapshots, with how many and in how many worlds, counting every world,
archived ones too. No snapshot is taken, since removing snapshots is its
purpose. (P0)

17.6. A design document, docs/DESIGN.md, sets type, colour tokens,
spacing and components before the first UI epic. (P0)

17.7. **The Server page is the hub for the active world.** Everything an
admin does to the world that is running, or about to run, starts from
the Server page and shows its result there: start, stop, restart, force
stop, switch to another world, take a snapshot, update the game version,
and undo or recover after any of these. Each takes one confirmation
page at most, then shows its progress and result on the Server page. A
world's own page manages that world (rename, delete, saves, snapshots,
mods, settings), and offers switching to it, but never sends the admin
there to finish a task begun on the Server page. Text that changes every
second, such as a countdown, updates only itself and never replaces the
controls beside it. While an older save is chosen to load next (7.11), the
page's facts say "Loads next" with the save's name and a link to the Saves
tab, in a live region of its own, so a status change never replaces the
link. (P1)

17.8. **Problems come before the confirmation.** A confirmation page runs
the checks its action would run, and shows any problem, with its fix, on
that page. An action is never refused, after the admin confirms, for a
reason Foreman could have shown on the confirmation page. When an action
is refused anyway, because something changed in between, the refusal says what
changed. (P1)

17.9. **The next step is a control, not an instruction.** Every result,
refusal and failure names the world it is about and offers the next action
as a button or link on the same page. That includes undo after a change
(VISION principle 1, "undone in one click"), the download after a missing
image, and switch back or restore after a failed switch or a crash loop.
Text that tells the admin to go somewhere else and do something, without
a link that does it, does not meet this. The Server page's "Done" box,
which reports a success or a cancelled stop, may be closed by the person
(a close button that works without script); closing it also drops the
undo or Switch back offer it carried, and forgets that result on the
server. A failure's box has no close button. (P1)

17.10. **Click budgets for common tasks.** Starting on the Server page,
with the defaults accepted, these take at most:

| Task | Clicks |
|---|---|
| See who is online and what is running | 0 |
| Start the active world | 1 |
| Stop or restart it, warning players | 2 |
| Take a snapshot | 1 |
| Choose an older save to load (World page, Saves, Load next) | 3 |
| Switch to another world, downloading its game image if needed (Switch world, the world's Switch, confirm) | 3 |
| Switch back after a switch | 2 |
| Undo the last change (switch, update, restore, mod change) | 2 |
| Search for a mod and install it (Mods, type the name, +, Review and install 1 mod, Install 1 mod; typing is not a click) | 4 |
| Install a second mod in the same change (one more +, on the Mods tab) | 5 |
| Update all mods (Mods, Update all, Update N mods) | 3 |
| Change a startup mod setting (Mods, Edit mod settings, tick the setting, Save and restart; on a phone, Mod tools before Edit mod settings) | 4 (5 on a phone) |
| Change a mod's version (Mods, the row's More actions, Change version, choose the version, Change 1 mod; when the portal has an update, the row's Update to *x.y.z* opens the same version choice, so Mods, Update to *x.y.z*, choose, Change 1 mod) | 5 (4 with an update) |
| Fix a mod the Server page flags (Fix, Update N mods) | 2 |
| Fix and start a world refused for a mod players cannot download (Start server, Update *mod*, Update 1 mod, Start server) | 4 |
| Recover after a failed start or a crash loop | 2 |
| Make an online player admin (the player, Make admin) | 2 |
| Make a player admin, then undo it (the player, Make admin, Undo) | 3 |
| Add an online player to the whitelist (the player, Add to whitelist) | 2 |
| Kick a player (the player, Kick, Kick *player*) | 3 |
| Ban a player (the player, Ban, Ban *player*) | 3 |
| Change the restart schedule while one is set (Next restart, Save schedule) | 2 |
| Set the first restart schedule (Settings, Restart schedule, Save schedule; Settings opens General) | 3 |
| Open Notifications (Settings, Notifications) | 2 |
| Try a failed mod change again (Try again, Install N mods) | 2 |
| Generate a world with the defaults (Worlds, Generate a world, Generate world; the world is made at step 1, and the Mods and map settings steps are optional, 5.6) | 3 |
| Clone the active world (World page, Clone world, Clone world) | 3 |
| Change a game setting in Listing (Game settings, Preview changes, Save settings; a setting in another group first opens its sub-page, 11.1) | 3 (4 in another group) |

A click is one activation of a button, link or choice in a list. Typing
in a field doesn't count; ticking a box does. A phone is a window narrower
than 64rem, where the Mods tab's rarer tools fold under one "Mod tools"
disclosure (DESIGN 8.11). The page lists of 17.18 hold the same links at
every width, so the counts are the same on a phone, except where a row
says otherwise. Each task's end-to-end test counts its clicks and fails
when a task goes over budget. (P1)

17.11. **Times show in the viewer's time zone.** A time on a page is a
`time` element with a UTC `datetime`. In the browser it is shown in the
viewer's own zone, with a short zone name and, in visible text, how long
ago it was, refreshed every minute. A time inside text only a screen
reader hears (a control's name) or inside a button label (the Undo
button, the Restore offer) shows the local time alone, so the name does
not go stale. Without JavaScript the page keeps its UTC text.
Durations, such as uptime, are not times of day and are left alone. (P1)

17.12. **Tables keep their meaning when they reflow.** A table that stacks
its rows at a narrow width (a snapshot list at 200% text on a phone) keeps
its table, row and cell roles for assistive technology, and none of its
content may overflow its panel. (P1)

17.13. **Required fields are marked.** Every field the server refuses
when empty has an asterisk after its label, drawn by CSS so the label's
accessible name does not change, and each form with such a field has one
"* Required" note. The one exception is the Console's command field, the
only field of a form that exists only to send it: it has no asterisk and
no note, and keeps its `required` attribute so assistive technology still
announces it. An optional field has no mark. A placeholder, where
there is one, is an example and never repeats the label. (P1)

17.14. **Toggletips and the picture viewer.** A setting may explain
itself with a toggletip (DESIGN 7.7): an (i) button named "About" and the
setting. It opens on click, tap, keyboard focus or hover, and closes on
Escape or a tap elsewhere. Its text is in the page, describes the field,
and shows under the field without script. The map preview's large view
(5.6) puts its controls over the picture: Previous, Next, Close, and Zoom
out and Zoom in, kept as the single-pointer way to zoom besides pinch and
wheel (WCAG 2.5.1). They show on hover, on keyboard focus, and always
under `(hover: none)`. On a zoomed picture a tap centres the spot tapped,
so panning never needs a drag (WCAG 2.5.7). The tip buttons and the
viewer's controls are 24 px targets, and 44 px on touch screens
(`pointer: coarse`). (P1)

17.15. **Resource amounts, the leave dialog and the large view's news.**
The labels on the picture are `aria-hidden`; the same information is a
collapsed `details` under the box, "Resource amounts as text (N patches)",
with a hidden `h3` and, for each resource, an `h4` giving its total, then
a list of its patches sorted by amount, each as "12,345,678 at (-50, -30)"
or "300% yield at (50, -50)". The large view says its own news, because the
page outside the open dialog is inert: the counting sentence ("Counting
resources in the map… This can take up to a minute.") is announced once,
when a count starts, including after a redraw or a change of planet, and
the bar beside it is only seen; the status line says "Resource amounts
ready." when it ends. A failure is shown as a message beside the box with
Try again and is announced once, the sentence alone, by a visually hidden
live region, so it is not read twice. When the list or Try again held
focus and the result is cleared, focus moves to the box. On Vulcanus the
territory counts sentence appears inside the large view too. Foreman's
leave dialog (5.6) is a modal `dialog` named by its question, with "Stay on
this page" focused on open; Escape closes it like Stay, and it passes the
accessibility scan at 1280 and 320 px. In the large view the arrow keys step
the planet unless focus is in a settings field, including from the amounts
list, or a preparation is shown in the view (a failed one with its Try again
too): then Left and Right do nothing, and the first ignored press says why
once. Tab goes round the large view's controls, from the last back to the
first. The One planet picture's box is announced as the large view's is;
at 320 px a short "Counting…" beside it is seen, not said, and a page count
that ends while the large view is open is said once when the view closes.
A download being prepared (5.6) announces each step politely, and its bar
is never empty. On a failure focus moves to the first choice, and Try
again names the planet; a click on another download moves focus to Cancel;
when the focused button goes, focus moves to Cancel, and when the block
closes, back to the download link. "Download started." is said when the
download begins. (P1)

17.16. **Defaults show.** A settings form never shows an empty box that
hides the value in effect: a field the file leaves unset shows that value as
a placeholder and in a hint that carries its meaning, and a checkbox shows the
default state. Every settings form follows the rule: the game settings form
(11.1), the mod settings (the world's file, else the defaults file, else the
mod's own default), the restart schedule and the webhook form (new ones start
filled with Foreman's defaults), Generate (the seed's hint says a blank seed
is random) and the version boxes (the hint says a typed version is used
instead of the choice). Two forms are exempt: the factorio.com account, whose
username and token are write-only secrets (11.4), and the raw settings editor,
which edits the file as text, where adding keys the file lacks would change
the file. A mod setting whose value differs from the mod's own default shows
a hint under the field, "Mod default: X" (booleans as on or off, an empty
text as empty); nothing extra shows when the value is the default. The hint
is part of the field's description for screen readers. (P1)

17.17. **No long addresses.** No address Foreman builds carries a list or
long state. State travels by id, or by a server-side record the address names
by token (the mods review's draft, 9.14; a settings preview, 11.3; a map
preview, 5.6), or in a posted form (a snapshot selection, 6.12). Every address
stays under 512 bytes, and the pages that carry plans or selections have a
test for it. Left as they are, one short value each: a note, a favorite's id,
a player name, a game version, a search and its page, the versions filter. (P0)

17.18. **One navigation pattern.** Every signed-in page shares one frame
(DESIGN 6, "Shell"). From a window 64rem wide, a column on the start side
(the product rail) holds the logo and the four areas (Server, Worlds,
Snapshots, Settings), and a strip across the top of the page names the
live world and the server's state, in words, and holds the user menu; the
strip stays in view while the page scrolls. Below 64rem the same parts
are a header bar with the areas as a row of tabs. (P1)

- **Sub-pages in one place.** Any area with sub-pages lists them in that
  column, under the four areas: the Server view (Overview, Console, Chat),
  Settings (13.1), the Worlds area (All worlds, Favorites, Deleted worlds),
  and a world (Overview, Saves, Snapshots, Mods, Game settings). Below
  64rem each list is a row of tabs under the page's heading. The ids and
  addresses are the same at every width.
- **No in-page section lists.** A page never carries its own list of links
  to its sections. A page too long to read whole is split into sub-pages
  in that column instead, as Game settings is (11.1). The current page's
  link is marked `aria-current="page"`; its parent, when a sub-page is
  open, `aria-current="true"`.
- **Where you are.** Every page under Worlds opens with a breadcrumb of
  its full path (for example "Worlds > Nauvis Prime > Mods"); the Server,
  Settings, Worlds and Snapshots pages name their area in a line above the
  heading. No page has a "Back to" link above its heading.
- **The state is said once.** The strip (or, below 64rem, the header)
  is where the server's state shows on every page. Page headings do not
  repeat it visibly; the Server view's heading keeps it for screen readers.

17.19. **Generate's steps work without script.** Each step of Generate
(5.6) is a plain page with a form that posts. The step bar is a list of
three links in an `ol`, the current step marked `aria-current="step"` and
named in words, never by colour or number alone. Without script, step 3
is drawn by the server once the world's planet run has finished, within
its time limit (14.3); with script the page shows at once and the form
part loads after it, with its progress said once. Skip, Continue and every
"next step" are links or buttons (17.9). (P1)

## 18. Epics, in order

Each epic delivers working, tested features end to end (on disk, server
and UI), not a layer. An epic's plan names the sections above it serves.

18.1. **Epic 1: Foundation.** The stack from docs/STACK.md; `make verify`
and the CI workflow; the container image; configuration loading (3.2);
the data directory and host path check (3.3 to 3.6); the app database;
local accounts and first-run setup (12.3, 12.4); trusted headers (12.5,
12.6); sessions (12.8); the UI shell and docs/DESIGN.md (17.6).

18.2. **Epic 2: Worlds and snapshots.** The world model (5.1 to 5.3);
list, import, rename, delete, saves (5.4, 5.5, 5.8, 5.10, 5.11);
snapshots and restore (section 6).

18.3. **Epic 3: Server control.** Creating the game
container (4.3 to 4.6); status (7.1, 7.2); start, stop, restart (7.3 to
7.8), including the pre-start check and the save reader it needs
(7.4, and 9.9); save selection (7.11); the log stream and crash
detection (8.6, 8.7, 8.9); the player list (8.1); the RCON console (8.4)
and its audit entries. It also finishes snapshot and restore of a running
world (6.5, 6.10).

18.4. **Epic 4: World switching.** Switching (7.9, 7.10), and removing
adopted mode (ADR 0005).

18.5. **Epic 5: Game versions.** Section 10, including the version picker
(10.7).

18.6. **Epic 6: Languages.** Section 22, including the language menu,
translated server-built messages, and the checks in 22.5.

18.7. **Epic 7: Generating worlds and game settings.** Generate (5.6),
clone (5.7), and section 11.

18.8. **Epic 8: Mods.** Section 9.

18.9. **Epic 9: Live operations.** Players (8.2, 8.3), chat (8.5), crash
reasons (8.7, 8.8), scheduled restarts (7.12), notifications (8.10 to 8.12)
and the low-disk warning (8.13).

18.9b. **Epic 9b: VPN sidecars.** The optional sidecar (4.13, 14.16, ADR
0006), split out of Epic 9 so that Epics 10 and 11 keep their numbers.
Built; what is still unproven is in STATUS.md, Epic 9b.

18.9c. **Epic 9c: Public mods and autosave tidy.** The public mods page
(9.16, 14.18, #132) and the autosave tidy (11.6, #671). Built; see
STATUS.md, Epic 9c.

18.10. **Epic 10a: API tokens and MCP.** API tokens with all three
scopes, the token page, the expiry banner and the `token-expiring` event
(12.10, 8.10, 8.11). MCP access on or off (21.2, 13.1, 3.2). The
endpoint and its limits (21.1, 21.3, 14.19). The read tools (21.4),
action tools (21.5) and chat (21.6). Audit entries with token names
(12.9). An admin token has these tools and, since Epic 10b, the admin
tools (18.10b). **Gate:** a real MCP client (Claude Code), using an operate token
against a real `factoriotools/factorio` container, reads status, takes a
snapshot, sends a chat that players see as "[name] ...", and restarts
with a countdown. A read token is refused that action. An Expired token
is refused, then works with the same secret after renewal. A Revoked
token stays refused. With MCP off, every call is refused and tokens can
still be managed.

18.10b. **Epic 10b: Admin tools.** The admin tools for every other UI
area (21.7) and the two-step confirm (21.8, 14.19). Built; see STATUS.md,
Epic 10b. **Gate:** for every two-step tool, the first call changes nothing and
its preview names the snapshot the UI would take. A code is refused for
another token, tool or arguments, after 5 minutes, and on a second use.
Restore, a mod change and a game version update through MCP take the
same snapshots as the UI, checked against a real game container.

18.10c. **Epic 10c: REST API.** Section 23 and its changes to 3, 12.1,
12.9, 12.10, 14.7, 14.12 and 14.19. Built; see STATUS.md, Epic 10c.
**Gate:** against a real
`factoriotools/factorio` container, using `curl` only, an operate token
lists its tools, reads status, takes a snapshot, sends a chat that
players see as "[name] ...", and restarts with a countdown; it is
refused an admin tool with 404. An admin token deletes a snapshot in two
calls: the first changes nothing, and a code from `/mcp` works on
`/api/`. With API access off, every `/api/` call gets 403 while `/mcp`
still works, and the reverse. A token's 121st request in a minute, split
across `/mcp` and `/api/`, gets 429. The OpenAPI document validates as
OpenAPI 3.1 and lists exactly the token's tools.

18.11. **Epic 11** is two epics, 11a and 11b, split on 2026-11-18. The
licence is MIT.

18.11a. **Epic 11a: Sign-in, audit log, archive, settings and users.**
OIDC (12.7, 3.2, 14.10), the audit log viewer, filters and retention
(12.9), archive (5.9, 21.7), the General settings tab (13.1, 6.6, 7.6,
14.6), the Users tab (12.12), the operator guide with the socket proxy
example (14.2), a multi-platform image and a build-only release workflow
(3.1). Built; see STATUS.md, Epic 11a. **Gate (on the trial deployment):**
sign in through a real OIDC provider, where a user outside the group gets
the 403 page and an existing header user is linked; archive a world, see
its start refused in the UI and through MCP, and unarchive it; the audit
tab filters by user and by world; lowering retention shows the count, the
countdown default appears on the stop forms, and an upload over a lowered
limit is refused; delete a second user and see that user's token stop
working; the release workflow's dry run builds both architectures.

18.11b. **Epic 11b: Release and Gate C.** Publishing: on a `v*` tag whose
commit is on `main`, build amd64 and arm64, push
`ghcr.io/toddawhittaker/foreman:<X.Y.Z>` with an exact version tag and no
`latest`, and attach a GitHub artifact attestation. Gate C (19.3), on
`1.0.0-rc.1`, then `1.0.0` on the same image digest. Not built yet.

18.12. **Epic 12: Modded planets and guided Generate** (#775). Generate's
three steps and Unfinished worlds (5.6, 5.7, 5.12, 17.10, 17.19); the
planet list from the mod-settings run (9.6, 14.6); previews, territories
and resource counts with the world's mods, and the real-game picture on
2.1 (5.6, 14.3, ADR 0033); the pre-start refusal of stale controls (7.4);
`get_new_map` and `set_new_map` (21.7). Built; see STATUS.md, Epic 12.
**Gate:** against real
`factoriotools/factorio` containers on 1.1.110, 2.0.77 and 2.1.24, with
five planet mods on 2.x: the mod-settings run lists 11 planets; each
planet's preview draws within 60 seconds and 1 GiB; map settings saved
in step 3 for a modded planet reach the map the first start creates; a
removed planet mod refuses the start until step 3 is saved again; and on
2.1.24 Cerys's real-game picture shows its real terrain within 90
seconds.

## 19. Milestone gates

At each gate the project stops and proves something before going further.
The architect also reviews the whole system at each gate.

19.1. **Gate A, after Epic 5: the reference deployment runs.** Foreman
runs behind Caddy and Authentik with trusted-header sign-in, the existing
hand-renamed folders are imported as worlds, each running on the game
version it was made with, and an admin can switch between two of them and
back from a phone with no save lost. Todd retires OFSM only after this
gate passes.

19.2. **Gate B, after Epic 8: the new-user path works.** From `docker run`
to a running Space Age server with three portal mods in under ten
minutes, following only Foreman's own operator guide.
Until Epic 11b publishes the image, the ten minutes start after `make
image` (Todd's ruling, 2026-11-11).

19.3. **Gate C, after Epic 11b: v1.** Every P0 and P1 requirement is met; a
bad mod update is undone in one click; security-reviewer finds nothing
open in section 14; the image is published.

## 20. Decisions needed

Each is put to Todd with options and a recommendation before the epic
that needs it starts. The answer is written into the section that cites
it, and the decision is removed from this list.

No decision is open.

The tech stack (vision question 6) is settled in docs/STACK.md, not here.

## 21. AI access (MCP)

21.1. **Endpoint.** Foreman serves an MCP (Model Context Protocol)
endpoint at `/mcp` on its main listener (STACK 8), inside the one
Foreman container. It uses the Streamable HTTP transport without
sessions: each POST carries one JSON-RPC message and gets one JSON
answer. GET and DELETE answer 405. (P1)

21.2. **On and off.** MCP access is off by default. An admin turns it on
or off on the API tokens page (12.10). The choice is stored as an app
setting (13.1), takes effect at once, and is audited as `mcp-set`.
`FOREMAN_MCP=off` forces it off. The page then shows that and the switch
cannot be used. While MCP is off, every `/mcp` request is refused
(14.19). Tokens can still be created, renewed and revoked, and they keep
expiring. (P1)

21.3. **Who may call, and how tools work.** The endpoint accepts only
API tokens (12.10), never a session, cookie or trusted header. A client
is listed only the tools its token's scope allows. Any other tool is
refused as unknown. Every tool calls the same code the UI uses, so the
pre-start check, snapshots before risky operations, graceful stops,
refusals and the audit log apply exactly as they do for a person. A tool
that acts on a world takes the world's id, so a second running world
needs no new tool. Tool descriptions, results and errors are in English
(22.3) and never hold a secret (14.8). (P1)

21.4. **Read tools** (every scope): server status (7.1, 7.2), including
the action running now and the last action's result, as the Server page
shows them; list worlds (5.4); players online (8.1); the most recent log
lines, with secrets removed (8.6); list snapshots (6.4); list a world's
mods and available updates (9.3). The tools are `server_status`,
`list_worlds` (which includes archived worlds, with `archived: true`, 5.9),
`list_players`, `recent_log` (the newest 200 lines),
`list_snapshots` and `list_mods`. `world_id` is optional on these and
means the active world when left out. Player and portal text in any
result is marked untrusted (14.12). (P1)

21.5. **Action tools** (operate and admin): start (7.3); graceful stop
(7.6), with a countdown from 0 to 10 minutes that defaults to the app's;
graceful restart (7.8); switch world (7.9); take a snapshot (6.3). The
tools are `start_server`, `stop_server`, `restart_server`,
`switch_world` and `take_snapshot`, and `cancel_stop`, which cancels a
stop countdown exactly as the Server page's **Cancel stop** does (7.6;
Todd's ruling, 2026-11-17). `world_id` is required on each. Each
returns what happened, including any snapshot it took. A call waits up
to 50 seconds. If the action is still running then, the call says so and
server status (21.4) reports the result when it ends. The action
continues if the client goes away, as it does when a browser closes.
`take_snapshot` through a token is refused once the world holds 10
snapshots taken through API tokens. The snapshot's record notes
`via_token` for this. Snapshots taken in the UI neither count nor are
refused, and an admin deleting one frees room. (P1)

21.6. **Chat** (operate and admin): the tool `send_chat` sends an
in-game message as the server (8.5). Foreman puts the token's name in
brackets in front of it, for example "[Claude] Restarting in 5 minutes",
so players can tell it from a person. The prefix cannot be left out. The
whole line, prefix included, follows 8.5's rules and its 500-character
limit, and is audited as sent. (Todd's ruling, 2026-11-17.) (P1)

21.7. **Admin tools** (admin only): everything an admin can do in the UI
in these areas:

- worlds: rename, archive and unarchive, generate (without the preview
  picture), the map settings of an Unfinished world (5.6), clone, delete,
  favorites (5);
- which save loads (7.11);
- snapshots: restore and delete (6);
- forced stop (7.7) and the game image download;
- players, the admin list, ban list and whitelist (8.2, 8.3);
- the raw console (8.4);
- the restart schedule (7.12);
- webhooks (8.10, 8.11), whose address and Pushover keys are accepted but
  never returned (14.8);
- mods and mod settings (section 9);
- the public mods page (9.16);
- the factorio.com account (9.11), write-only;
- game versions (section 10);
- game settings, including the autosave tidy (section 11);
- the General settings (13.1).

Not available through MCP: managing users, API tokens or MCP access;
uploading or downloading files (saves, mod zips, `mod-settings.dat`,
favorite files, world files, 5.13 and 5.14); map preview pictures;
factory map pictures (5.16); and the players of a save (8.14).

The tool names are fixed, one tool per action:

- worlds, favorites and saves: `generate_world`, `get_new_map`, `set_new_map`,
  `clone_world`, `rename_world`, `archive_world`, `unarchive_world`, `delete_world`,
  `list_deleted_worlds`, `list_favorites`,
  `make_favorite`, `rename_favorite`, `delete_favorite`, `list_saves`,
  `choose_save`;
- snapshots and live operations: `restore_snapshot`, `delete_snapshots`,
  `force_stop`, `download_game_image`, `kick_player`, `ban_player`,
  `player_action`, `run_console_command`;
- mods: `search_mods`, `change_mods`, `list_mod_settings`,
  `change_mod_settings`;
- game settings and versions: `get_game_settings`, `change_game_settings`,
  `set_whitelist_switch`, `list_game_versions`, `update_game_version`,
  `rollback_game_version`;
- Foreman-wide: `foreman_settings` (which reports every General tab
  setting), `set_foreman_settings`, `set_restart_schedule`, `add_webhook`,
  `change_webhook`, `delete_webhook`, `set_factorio_account`,
  `remove_factorio_account`, `set_public_mods`.

`get_new_map` (#775) is an admin tool, like `list_mod_settings`. It returns
an Unfinished world's planets (5.6), each control with what it supports,
and the values saved for its new map; it requires `world_id`, and is
refused once the world has a save. Values are percentages as the form shows
them, where 100 is the game's default. A control a planet defaults to
something else is reported at that planet's own default (5.6), which may be
up to 1000, so a percentage is a number, not a whole number: 0.125 reads as
12.5. Planet and control names come from mods and are marked untrusted
(14.12).

`set_new_map` (#775) takes `world_id` and either a preset, a seed and
values per planet and control, or a map exchange string, and saves them as
step 3 of Generate does (5.6), with no snapshot. It is refused for a world
with a save or a busy one. A planet or control not in the world's list,
or a value the control does not support, refuses the whole call with an
error that names it; nothing is dropped silently. A value may be a step
the form offers or the planet's own default for that control; a planet
default on a control shared with a planet that lacks it is refused, with
a message about the shared value. Percentages are numbers, as above. Like
the page, it is refused while the world's server is starting, running,
stopping, crashed or crash-looping, or when its state is unknown (5.6),
and while the built-in planets stand in for the world's own (5.6). It
carries the annotations of
`generate_world` and the audit entry `new-map` (12.9).

`add_webhook` and `change_webhook` take `url`, or for the Pushover preset
`app_token` and `user_key` (8.11); an omitted key keeps its stored value on a
change, and no answer returns an address or a key (14.8).

Each tool that acts on a world requires `world_id`, except where a
favorite or webhook is named instead. Player, portal, game-log and snapshot
text in results is marked untrusted (14.12), and no secret is returned
(14.8). (P1)

21.8. **Two-step confirm.** An admin tool that deletes or overwrites
works in two calls. This covers deleting a world, restoring a snapshot,
a forced stop, a raw console command, mod and mod settings changes, game
settings changes, game version changes, and every other action the UI
asks a person to confirm first. The first call changes nothing. It
returns a preview in the same terms as the UI's confirm page: what will
change or be lost, which snapshot will be taken, and who is online and
will be warned. It also returns a confirm code (14.19). The second call
repeats the tool with the same arguments plus the code. It runs every
check again, as the UI's form post does, then takes the same snapshots
and writes the same audit entries. If what the preview showed has
changed since (for example a new mod release), the second call is
refused and returns a fresh preview and code. The code also stands for
the confirmations a UI confirm page asks for inside the page, such as
typing the world's name or ticking that mods will be dropped, so the
preview lists those items explicitly. An action is two-step exactly when
this section names it or the UI shows a confirm page for it; every other
admin tool is one call. Game settings are changed through the settings
form's fields only, not the raw editor. (Todd's rulings, 2026-11-17.)

The two-step tools are `delete_world`, `delete_favorite`,
`restore_snapshot`, `delete_snapshots`, `force_stop`, `kick_player`,
`ban_player`, `run_console_command`, `change_mods`, `change_mod_settings`,
`change_game_settings`, `update_game_version`, `rollback_game_version`,
`delete_webhook` and `set_foreman_settings`. Each carries `DestructiveHint: true`. The tools that
change nothing carry `ReadOnlyHint: true` instead, and every other admin
tool sets `DestructiveHint` to `false` explicitly. A two-step tool requires `world_id` where it acts on a
world, and its confirm call must repeat the first call's arguments. A confirm
call spends its code before anything else, so a refused confirm still spends
it. Only a bad, used, expired or mismatched code gets the one fixed
message (14.19). Any other refusal, such as a busy or active world or a
page check, keeps its own message, and the code is still spent. A moved
state is refused with a fresh preview and code. Every audit entry written
by an admin tool carries the token's name (12.9). `archive_world` and
`unarchive_world` are one call each, since neither deletes or overwrites
anything. `set_foreman_settings` changes any General tab setting (13.1);
arguments left out keep their value. Its preview lists each change from old
to new value, how many automatic snapshots across how many worlds the
retention will remove (6.6), and how many audit entries the next hourly
sweep deletes. The state a code stands for is the current values, so a
confirm call is refused with a fresh preview and code if any value has
changed since. A call that changes nothing returns no code. (Todd's
rulings, 2026-11-18.) (P1)

## 22. Languages

22.1. Foreman's web UI is available in English, German, Russian,
Simplified Chinese, French, Spanish, Polish and Czech. (P0)

22.2. The language is the best match for the browser's
Accept-Language, else English. A language menu on every page,
including sign-in, overrides it for that browser with a cookie;
nothing is stored per user. The page's `lang` attribute names the
language shown. (P0)

- The cookie is `foreman_lang`. It holds a BCP 47 language tag (for
  example `zh-Hans`), with Path `/`, SameSite Lax, HttpOnly, a life of
  one year, and Secure under the same rule as the session cookie. A
  value that is not one of the eight tags is ignored and never echoed.
- The menu posts to `POST /language`, which accepts only the eight tags
  and goes back to the page the menu was on through the return-path check
  (14.17). On a page shown in answer to a form post, that page is the
  same-site Referer, else home.
- Accept-Language is read entry by entry, at most 32 entries. An entry
  that is unknown or malformed is skipped and does not hide a later
  match.
- Off English, the menu's label adds "(Language)" in English, marked
  `lang="en"`, so a person who landed in a language they cannot read can
  still find it.
- The menu is one row showing the current language's flag and its own
  name; opened, it lists the other seven the same way (DESIGN 6). Each
  language has one flag: the United Kingdom for English, Spain for
  Spanish, and Germany, Russia, China, France, Poland and Czechia for the
  others. The flags are decorative, so the language name carries the
  meaning, and they are bundled files, never fetched (ADR 0020).

22.3. Everything the web UI shows is translated, including problems,
refusals, failures and results built by the server, and text shown by
the page's scripts. Server logs, audit entries as stored, and MCP tool
descriptions, results and errors stay English. Text relayed from
Factorio and anything a person typed (world names, snapshot labels,
player names) is shown as written. (P0)

- Messages built in Go travel as message ids with arguments, never as
  English strings, so a raw string cannot reach a page. The domain
  packages keep English `Error()` text for logs and carry the facts the
  web layer needs in typed errors.
- Foreman's automatic snapshot labels are shown from their stored reason
  code, in the page's language. A label a person typed is shown as
  written. No sentence quotes a label; it names the snapshot through the
  label's own message, so a page and the snapshot list always agree.
- Mod setting names and descriptions are shown as the mod wrote them,
  marked with the language they are in.
- Game text (the log pane, console replies, crash reasons) and English
  error details that Foreman cannot translate are marked `lang="en"`.
  The countdown warning sent into the game chat stays English, because
  players read it in the game.

22.4. Numbers, sizes, dates and plurals follow each language's CLDR
(Common Locale Data Repository) rules. A message that counts something
has every plural form its language needs. A message is never assembled
from translated fragments. (P0)

22.5. Every new user-facing string goes into the catalogue in all
languages in the same change. `make verify` fails on these. (P0)

- hard-coded text in a template, script or stylesheet;
- a key missing from any language, or an unused key;
- mismatched placeholders or plural forms;
- HTML inside a catalogue value;
- a translation made from English text that has since changed;
- a sentence in any language that quotes an automatic snapshot label
  instead of using its message (22.3);
- a link or button whose accessible name does not contain its visible
  text, on every page in every language (22.7, 17.1).

Catalogue keys are stable dotted ids that start with their area
(`server.start.button`), not English sentences. Values use named
placeholders (`{world}`), a counted value is an object of plural
categories using `count`, and values never hold HTML. The files are
`internal/i18n/locales/<language>/<area>.json`; docs/I18N.md says how to
add a string and ADR 0008 gives the reasons.

22.6. Translations are machine-made with the glossary in docs/I18N.md,
so terms stay consistent and follow Factorio's own translations where
it has them. A second pass reviews each before its epic merges. (P0)

22.7. Every page meets 17.1 and 17.2 in every language, including at
320 px in the languages with the longest text. The accessible name of
every control contains its visible text in every language (label in
name, WCAG 2.5.3), including where a hidden part is added to a visible
word. (P0)

## 23. REST API

23.1. **Endpoint.** Foreman serves a JSON API on its main listener.
`GET /api/v1/tools` lists the tools the token's scope allows, each with
its name, description, whether it is read-only or two-step (21.8), and
the JSON Schema of its arguments and of its result.
`POST /api/v1/tools/<name>` calls one tool. Any other method on these
paths answers 405. (P1)

23.2. **Same tools, same code.** The API offers exactly the MCP tools of
section 21: the same names, arguments, results, scopes and refusals. Each
call runs the code an MCP call runs, so the pre-start check, the
snapshots, the token snapshot cap (21.5), the chat prefix (21.6), the
50-second wait (21.5), the two-step confirm (21.8), the untrusted fields
(14.12) and the audit entries with the token's name (12.9) are the same.
A tool outside the token's scope answers 404, as an unknown tool.
Results and errors are in English (22.3) and never hold a secret (14.8).
This includes `get_new_map` and `set_new_map` (21.7). (P1)

23.3. **Who may call.** Only an API token in `Authorization: Bearer`
(14.19). The API never reads a session, cookie or trusted header. (P1)

23.4. **Request.** The body is one JSON object holding the tool's
arguments, sent as `application/json`. An empty body means no arguments.
Another content type answers 415. A body that is not a JSON object
answers 400. A body over 4 MiB answers 413. (P1)

23.5. **Answers.** A call that works answers 200 with `message`, the
tool's one-line summary, and `result`, the tool's result exactly as MCP
returns it. Every refusal by a tool answers 422 with `error` in English,
plus `result` when the tool gave one, such as a fresh preview and code.
A fault Foreman cannot word answers 500 with `error` in general terms; the
reason goes to its log (14.8).
Refusals made before a tool runs (401, 403, 413, 429) are plain text, as
on `/mcp`. (P1)

23.6. **Two-step confirm.** The first POST changes nothing. It answers
200 with `done: false`, the preview and a confirm code. The second POST
repeats the arguments with `confirm_code`. All of 21.8 and 14.19 apply.
A code is bound to its token, tool and arguments, not to the endpoint,
so a code from `/mcp` works on `/api/` and the reverse. (P1)

23.7. **Versions.** Within `/api/v1/`, Foreman may add tools, optional
arguments and result fields. It never removes or renames them, or
changes what one means, without a new version path. This binds the tools
of section 21 too. (P1)

23.8. **On and off.** REST API access is off by default and separate
from MCP access (21.2). An admin turns it on or off on the API tokens
page (12.10). The choice is stored as an app setting (13.1), takes
effect at once, and is audited as `api-set`. `FOREMAN_API=off` forces it
off. The page then shows that and the switch cannot be used. While it is
off, every `/api/` request is refused (14.19). (P1)

23.9. **Shared limits.** The per-address failure limit and each token's
120 requests a minute with at most 4 running at once (14.19) count
`/mcp` and `/api/` requests together. (P1)

23.10. **OpenAPI.** `GET /api/v1/openapi.json` serves an OpenAPI 3.1
document generated at startup from the same tool schemas, describing
the tools the token's scope allows. It needs a token, like the tool
list, and is never written by hand. (P1)
