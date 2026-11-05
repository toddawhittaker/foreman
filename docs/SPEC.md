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

## 3. Deployment

3.1. **One container.** Foreman ships as one container image, published
with an exact version tag. It needs no other service to run. The
image is published to GHCR (GitHub's container registry) as
`ghcr.io/toddawhittaker/foreman`. (P0)

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
| `FOREMAN_GAME_NETWORK_CONTAINER` | none | A VPN sidecar container whose network the game shares (4.13). Optional |
| `FOREMAN_LISTEN` | `:8080` | The address Foreman listens on |
| `FOREMAN_PUBLIC_URL` | none | The URL people use to reach Foreman. `https://` makes session cookies `Secure` (12.8), and its origin is accepted for form posts (14.7) |
| `FOREMAN_AUTH_METHODS` | `local` | A comma-separated list of `local` and `headers`. Empty means `local`. `oidc` joins the list with OIDC (12.7) |
| `FOREMAN_AUTH_USER_HEADER` | none | Header that names the user. Required when `headers` is on (12.5) |
| `FOREMAN_AUTH_GROUPS_HEADER` | none | Header that lists the user's groups. Required when `headers` is on (12.5) |
| `FOREMAN_AUTH_REQUIRED_GROUP` | none | Group a user must belong to. Required when `headers` is on (12.5) |
| `FOREMAN_TRUSTED_PROXIES` | none | Address ranges whose headers are honoured. Required when `headers` is on (12.6) |
| `FOREMAN_SESSION_IDLE` | `168h` | Idle time before a session ends, as a Go duration (12.8) |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | The Docker socket, or a socket proxy |

A `tcp://` `DOCKER_HOST` is not encrypted and not authenticated. Use it
only for a socket proxy on the same host or on a Docker network shared
with Foreman, never across a network you do not control (14.2).

3.3. **The data directory** is one mounted path. It holds the worlds, the
snapshots, and the app database. Its layout is in section 5.2. (P0)

3.4. **The app database** holds only users, sessions, API token hashes,
app settings, notification settings, the audit log, which world is
active (a table of world ids, so that several could be active later), the
record of a switch in progress (7.10). It never holds other game data: saves, mods and game settings live only in
world directories. (P0)

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
log (12.9). (P0)

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
  one address with a VPN sidecar, 4.13); and `--server-settings` when the
  world has `config/server-settings.json`.
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
must not prevent a sidecar (ADR 0006). (P1)

When an operator sets one:

- The optional setting `FOREMAN_GAME_NETWORK_CONTAINER` names a running
  container, the sidecar, which the operator defines and runs. When it is
  set, Foreman creates the game container with Docker network mode
  `container:<that container>`, so the game shares the sidecar's network,
  and publishes no port itself.
- The game listens on UDP 34197 on every interface of the shared network;
  only RCON is bound to one address (below). LAN players and VPN players
  can play at the same time: VPN players reach the game through the VPN,
  and LAN players through the game port the sidecar publishes on the host.
  The operator sets that port in the sidecar's own definition, because
  Docker lets only the container that owns a network publish its ports,
  so Foreman cannot publish it.
- At every start Foreman inspects the sidecar. When UDP 34197 is not
  published on the host, the Server page shows a warning, "LAN players
  cannot reach the game: <sidecar> does not publish UDP 34197 on the
  host.", and the start goes ahead, because VPN-only play can be a
  deliberate choice.
- The sidecar must be on `FOREMAN_GAME_NETWORK`, which is then required
  even outside a container. The sidecar is the one container other than
  Foreman and its games allowed on the game network (4.4). Foreman reaches
  RCON at the sidecar's address on the game network.
- RCON is bound only to that address, with Factorio's
  `--rcon-bind <address>:27015`, never to `0.0.0.0`, so VPN peers cannot
  reach RCON.
- Every address range of every network the sidecar is on counts as a game
  range for trusted-header sign-in (12.5, 12.6). A start is refused when
  any of them overlaps `FOREMAN_TRUSTED_PROXIES`, as for the game network
  (4.4).
- A start is refused, with a plain reason, when the sidecar does not
  exist, is not running, uses the host network, or is not on the game
  network. Foreman recreates the game container at every start (4.3), so it
  looks the sidecar up by name at every start and attaches the game to the
  sidecar as it is then.
- Known limit: when the sidecar is restarted or recreated while the game
  runs, the game may lose its network until Foreman starts it again.
  Foreman does not watch the sidecar for this. The README and the operator
  guide say to restart the game from Foreman after restarting the
  sidecar.

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
  missing, the newest save), and the time it was created.

A person can read, back up and move a world without Foreman. `world.json`
is plain JSON with snake_case keys and a `format_version` (1 today). Fields
are only ever added. Foreman keeps fields it does not know when it
rewrites the file. A world whose `format_version` is newer than Foreman
knows is listed but cannot be changed, and the page says a newer Foreman
wrote it. (P0)

5.2. The data directory holds `worlds/<world-id>/`, `snapshots/` and
the app database. A world's id is a
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
time, disk use, and which one is active. (P0)

5.5. **Import** an existing directory of saves and mods (for example a
hand-renamed `saves-2025` folder) as a new world, or upload a save zip and
sync its mods from the portal (section 9.8). Import copies; it never moves
or changes the source. Foreman reads a folder to import only from the inbox,
`<data>/import/`, never from a path the admin types. It copies the
folder's top-level save zips and its `mods/` folder. A save zip can also
be uploaded to make a new world. (P0)

5.6. **Generate** a new world from a preset, a map exchange string, or
map-gen settings plus a seed, with Space Age on or off. How a preset or
an exchange string becomes a map is **Decision D9**. (P0)

5.7. **Clone** a world's mods and settings into a new world, then either
start a fresh map or keep the save. (P1)

5.8. **Rename** a world (display name only). (P0)

5.9. **Archive** a world: it is hidden from the main list and cannot be
started until unarchived. Its files stay where they are. (P1)

5.10. **Delete** a world. Delete always offers a final snapshot first,
defaulting to yes, and asks the admin to type the world's name. The active
world cannot be deleted. (P0)

5.11. **Saves.** List a world's save files with timestamps and sizes;
download any save; upload a save; choose which save loads (section 7.11).
(P0)

## 6. Snapshots

6.1. A snapshot holds a world's saves, mods, config and `world.json` as
they were at one moment, plus a label, the time, who took it, and why
(the risky operation it preceded, or "manual"). (P0)

6.2. Foreman takes a snapshot automatically before every risky operation,
before the operation changes any file. If the snapshot fails, the
operation does not run. A switch snapshots both worlds, labelled "Before
switch" with reason `switch` (7.9). (P0)

6.3. An admin can take a snapshot by hand at any time. (P0)

6.4. Snapshots are listed per world, newest first, with label, reason,
time and size. (P0)

6.5. **Restore** puts a world back exactly as the snapshot recorded it,
in one click with a confirmation. Restoring the active world stops it
gracefully first (section 7.6; the confirmation asks for the countdown),
snapshots its current state, restores, and starts it again. Restore is itself undoable: the snapshot taken before it
is the undo. Foreman restores by building the restored world in a
staging folder, renaming the live folder aside, and renaming the staged
folder into place, so the path `worlds/<world-id>` never holds a half
restored world. If Foreman stops partway, startup recovery finishes or
reverses the swap. Before it writes anything, restore checks that every
file still matches the snapshot (6.7). (P0)

6.6. **Retention** is an app setting: keep the newest *n* automatic
snapshots per world (default 10). Manual snapshots and the final snapshot
of a deleted world are never removed automatically. Foreman shows total
snapshot disk use. (P0)

6.7. **How snapshots are stored.** A snapshot is a plain directory,
`snapshots/<world-id>/<time>/`, with the same shape as the world, plus a
`snapshot.json` manifest (6.1). The folder is named by its UTC time, for
example `20261103T142501Z`, with `-2` added on a clash. The manifest
holds the label, the reason (`manual`, `before-delete`, `before-restore`,
or the risky operation), the time, who took it, and for each file its
path, size and modification time to the nanosecond. For each save and mod
zip it also holds a SHA-256 hash, which Foreman reuses from the previous
snapshot when the file's inode, size and modification time are unchanged.
Restore checks every hash first and refuses a changed snapshot, naming the
file. Disk use is read from manifests. A person can read it, copy it, or
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
tell whether files are in use. (P0)

6.11. **Fail safe on disk space.** Before a snapshot, restore, import or
upload, Foreman works out how many bytes the write needs (nothing for a
hardlink, the full size for a copy or an upload) and refuses, with a clear
message, when the filesystem's free space minus the bytes already promised
to writes in flight is short of that plus a fixed reserve of 2 GB. The
reserve is kept for the game's autosaves. An upload with no declared
length is refused, and at most two uploads run at once. If the disk fills
anyway, the partial file is removed and nothing changes. The snapshot page
shows free space beside snapshot disk use. (P0)

6.12. **The Snapshots tab and deleting a snapshot.** A Snapshots item in
the main menu lists every world's snapshots, deleted worlds included,
with the world, label, reason, time, who took it, its size and what
deleting it would free, and the total disk all snapshots use. Size counts
every file in the snapshot. What deleting frees counts only files no other
snapshot or world shares by hard link (6.8), and the total counts each
file once. An admin can delete one snapshot at a time, from this tab or
from a world's Snapshots page, after a confirmation page that names the
world, label and time and says how much space it frees. When the snapshot
is the one rollback restores (10.4), the confirmation says plainly that
rollback to that version goes away with it. Deleting removes only that
snapshot's folder, never the world or a file another snapshot shares. It
is refused while an operation runs on that world, and is recorded in the
audit log as `snapshot-delete` before anything is removed. (P1)

## 7. Server control

### Status

7.1. Foreman shows the game's state as one of: **stopped**, **starting**,
**running** (the server reached the "InGame" state), **stopping**,
**crashed**, or **crash-looping**. When Foreman cannot ask Docker, or
cannot read the game container, the state is **unknown** and the page says
why; Foreman keeps serving every page. The Server page is where setup and
sign-in land. (P0)

7.2. Alongside the state: uptime, game version, active world, the loaded
save, and CPU and memory use. (P0)

### Start, stop, restart

7.3. **Start** runs the pre-start check (7.4), then starts the game
container with the active world. Start and restart of a world that is not
active are refused; the admin switches to it instead (7.9). It reports success only when the server
has reached "InGame" **and** answers over RCON; reaching "InGame" alone is
not enough, because the RCON port can fail to open after it. If that does
not happen within a time limit (default 5 minutes), Foreman reports a
failed start with the reason if it can name one (section 8.7). Before the
check, a world with no `mods/mod-list.json` gets one that enables `base`
plus the built-in mods its game version and Space Age flag imply (9.7);
without it, 2.x would turn Space Age on by itself. The start steps are shown
as a list while it runs, and steps that never run after a failure read
"Skipped". (P0)

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
- the world has no save, unless the admin is generating a new map
  (otherwise the game would make one on its own);
- `mods/mod-list.json` disagrees with the world's Space Age flag, because
  that file is what the game loads until mod management (section 9) takes
  over;
- an enabled mod in the world declares a Factorio major.minor different
  from the world's game version; this refusal has no start anyway, since
  the game cannot load such a mod.

Each problem says what the game would delete. The admin can fix the world
(for example with sync from save) or, for the first bullet and for an
unreadable save only, **start anyway**. The refusal itself is the
confirmation: it names exactly what the game will delete, the snapshot
taken first and how to undo it, and its button starts at once, with no
second page. A restart offers restart anyway the same way. The button
carries what the box showed (the kinds of problem, the mods they name and
whether the game runs); when the check before anything changes finds
otherwise, Foreman refuses, shows the box again with what is true now, and
says it changed. A save from a newer or
unsupported version, a world with no save, and a mod list that disagrees
with the Space Age flag are plain refusals with no start-anyway, because
the game could not load the save or would load the wrong mods. A save
Foreman cannot read is reported as such, never guessed at; "Start anyway"
then says the mods could not be checked. (P0)

7.5. **Stop** is graceful by default (section 7.6). (P0)

7.6. **A graceful stop**: if players are online, warn them in game with a
countdown (default 60 seconds; each stop or restart asks for its own, from
0 to 600 seconds, where 0 means no warning); run `/server-save` over RCON
and wait for it to finish; then stop the container with Docker's stop
(SIGTERM, with a 120-second timeout). The warnings are chat lines from the
server, sent at the start of the countdown and again at 30 and 10 seconds
before the end. If RCON cannot be reached, Foreman says so and offers a
forced stop instead of silently forcing one. An exit after a stop Foreman
asked for is never reported as a crash, whatever its exit code. (P0)

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
order: graceful stop of the current world; snapshot of both worlds; the
pre-start check on the new world; make the new world the one the
container presents (4.3); start;
confirm it is running (7.3).
If any step fails, Foreman stops, says which step failed, and offers to
switch back, which restores the previous world as active. (P0)

Before the graceful stop, Foreman runs everything that would refuse the
new world's start: its pre-start check, the image check (3.7), the game
network checks (4.4) and the VPN sidecar checks (4.13), as restart does
(7.8). When the check's problems all allow it, the refusal offers
switch anyway the same way (7.4): the refusal is the confirmation, and it
also names the "Before switch" snapshots of both worlds. The pre-start
check runs again after the snapshots. A switch leaves
the running state as it found it: a running game ends up running the new
world, and a stopped one ends up stopped with the new world active,
skipping the stop and start steps. The old container is left stopped, since every start creates a new one
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
autosave.
One function decides "the newest save", and the pre-start check (7.4)
uses it, so the check reads the save the game will load. (P0)

### Scheduled restarts

7.12. An admin can schedule a restart (for example nightly at 04:00 in a
chosen time zone). It runs only when nobody is online. If someone joins
during the countdown, they see an in-game warning, and the restart waits
until the server is empty again, up to a configurable limit after which it
is skipped for that day. (P1)

## 8. Live operations

### Players

8.1. List who is online now and for how long. RCON's `/players online`
decides who is on. How long comes from the `[JOIN]` lines in the game
container's log since it started, so a chat line cannot fake a join; a
player whose join line Foreman did not see is listed without a time. (P0)

8.2. Kick, ban, unban, promote to admin, demote, and add to or remove from
the whitelist, each with a confirmation for the destructive ones. (P1)

8.3. View and edit the admin list, ban list and whitelist files of any
world, including one that is not running. (P1)

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

8.5. In-game chat: see recent chat and send a message as the server.
(P1)

### Logs and crashes

8.6. A live stream of the game's log, with the most recent lines shown on
open. The view stays usable on a phone and does not grow without limit:
Foreman keeps and shows at most 1,000 lines. The history is the game
container's own log, so Foreman stores nothing, and the history is lost when
the container is recreated at the next start Foreman makes. The page follows
new lines only while scrolled to the bottom and offers "Jump to newest"
otherwise. The game prints its RCON password as `<private>`, but Foreman
still removes the RCON password and the game password from every line before
showing or keeping it, and masks a printed password command (8.4, 14.8).
Any `--rcon-password` value in a line is always masked. (P0)

8.7. **Crash detection.** When the game stops without being asked to,
Foreman marks it crashed. It polls Docker every few seconds, and again when
the log stream ends, and compares the container's restart count, start time
and out-of-memory flag with what it saw last, together with the log's
failed-load line. It does not use the exit code, because the same failure
exits with different codes in different game versions (1.1 exits 0 after a
failed load). It names the reason when it can: a mod that failed to load, a
corrupt save, a version mismatch, a port in use, or out of memory.
Otherwise it says the reason is unknown. The Server page shows only that
summary, masked like every log line, and links to the full log on the
Console page (8.6); it
never guesses a reason. (P0 to detect a crash; P1 to name the reason)

8.8. **The reason catalogue** is a data file of log patterns, each
naming a reason. Patterns match message text, never source file and
line numbers, which change between game versions. Each entry has a test
fixture captured from a real game container. The catalogue's scenarios
are re-run against the test image of each supported line (docs/STACK.md,
section 1) whenever a test image changes, and on demand against any
version an operator names (`make test-catalogue VERSION=2.1.22`). (P1)

8.9. **Crash loops.** If the game crashes three times within ten minutes,
Foreman marks it crash-looping and stops the container, since Docker's
restart policy would otherwise keep it spinning. If Docker refuses that stop, Foreman
tries again at each poll until Docker accepts it. An admin must start it
again by hand. After Foreman itself restarts, a stopped game shows as
stopped; the crash-loop mark is not remembered. (P0)

### Notifications

8.10. Webhooks on: start, stop, crash, crash loop, player join, player
leave, and scheduled restart. Each event can be turned on or off per
webhook. (P1)

8.11. Presets for Discord and ntfy, plus a generic JSON webhook whose body
format is documented. (P1)

8.12. A failed webhook delivery is logged and retried at most twice; it
never blocks or delays a server operation. (P1)

8.13. **Low disk space.** While the game runs, the status page and the
notifications (8.10) warn when free space approaches the reserve in 6.11,
because the game's autosaves can then fail. (P1)

## 9. Mods (per world)

9.1. **Search** the mod portal by name and see each mod's summary, latest
version, supported game versions, and download count. (P0)

9.2. **Install** with dependency resolution. Foreman reads required,
optional and incompatible dependencies and version constraints, and
before changing anything shows the plan: what will be installed,
upgraded, or left alone, and any conflict. The admin can include optional
dependencies one by one. A plan with a conflict cannot be applied. (P0)

9.3. **Update.** Show which mods in a world have a newer version
compatible with the world's game version, and apply any or all as one
snapshotted operation. (P0)

9.4. **Enable, disable and remove** a mod, each as a snapshotted change.
Removing a mod that another enabled mod requires is refused with the
reason. (P0)

9.5. **Upload** a local mod zip. Foreman reads its `info.json` to name and
version it, and treats it as any other mod afterwards. (P1)

9.6. **Mod settings.** Upload and download `mod-settings.dat`. (P0)
Editing its values in the UI. (P2)

9.7. **Built-in mods** (`base` and the DLC mods for the world's game
version, section 2) are never offered for download or removal. Their
enabled state follows the world's Space Age flag: on enables `base` and
every DLC mod, off enables only `base`. (P0)

9.8. **Sync from save**: read a save's mod list and versions and make the
world's mods match: install missing ones, change versions, and disable
extras. The plan is shown before it applies (as in 9.2). Mods the portal
no longer has are listed by name as unresolved, and the admin decides.
(P0)

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
from the world, and a snapshot. (P0)

9.11. **Portal credentials.** The factorio.com username and token come
from `FOREMAN_FACTORIO_USERNAME` and `FOREMAN_FACTORIO_TOKEN` (or their
`_FILE` forms) first. If those are absent, an admin can enter them in the
UI, where they are stored encrypted with a key from
`FOREMAN_SECRET_KEY` (or its `_FILE` form). They are never shown back. If
neither source has them, mod downloads are disabled with an explanation;
search still works. (P0)

9.12. Downloaded mods are checked against the SHA-1 checksum the portal
publishes, and a mismatch is refused. (P0)

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
stays stopped, as in a switch, 7.9). Only the active world can be updated.
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
start is refused as in 3.7. (P0)

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
patch release of a supported line. When Docker Hub cannot be reached, the
page says so and the typed version still works. (P0)

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

11.2. The raw editor validates JSON syntax and field types before saving,
and refuses invalid input with the line and reason. (P1)

11.3. Every settings save shows a diff of what will change and takes a
snapshot first. A change to a running world says whether it needs a
restart to take effect. (P0)

11.4. The game password is write-only: the form shows whether one is set,
never its value. (P0)

11.5. `map-gen-settings.json` and `map-settings.json` get the same
form-and-raw editor. Map-gen settings are edited only when generating a
world (section 5.6). (P1 for map-gen as part of generate; P2 for editing
map-settings afterwards)

## 12. Sign-in and users

12.1. **One role in v1**: every user is a full admin. The user model
keeps a role field so a read-only viewer can be added later without a
migration. (P0)

12.2. Three sign-in methods, any combination enabled by configuration:
local accounts (12.3), trusted proxy headers (12.5), and OpenID Connect
(12.7). (P0 for the first two; P1 for OIDC)

12.3. **Local accounts**: username and password. On first run with no
users, Foreman shows a one-time setup page to create the first admin; the
page is reachable only until an admin exists. When `local` is not an
enabled sign-in method there is no setup page, and the first user to
arrive through trusted headers (12.5) becomes the admin. (P0)

12.4. Passwords are stored with a slow, salted password hash, chosen in
docs/STACK.md. A password has at least 12 characters. Sign-in failures
are rate-limited per username and per address (limits in 12.11). (P0)

12.5. **Trusted proxy headers** (forward auth from Authentik, Authelia or
oauth2-proxy): a configured header names the user, a second configured
header lists groups, and a required group names who may sign in. All
three are required: without a required group, anyone the proxy lets
through would become an admin. A user outside the group gets a 403 page.
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
VPN sidecar, those of every network the sidecar is on (4.13). Foreman
in a container records that range before the first start; outside a
container it learns a game's range right after the game starts.
`fe80::/10` counts as a game range
whenever a game network has IPv6. While those ranges are not yet known
(Docker unreachable, the container missing, or not yet inspected),
header sign-in is refused, the reason is logged once, and the refusal page
says so in plain words without any header value. Existing sessions and
local password sign-in are unaffected. The monitor refreshes the ranges.
(P0)

12.7. **OpenID Connect**: discovery URL, client ID and client secret
(env or `_FILE`), and an optional required group claim. (P1)

12.8. Sessions are server-side, expire after a configurable idle time,
and end on sign-out. The default idle time is 7 days
(`FOREMAN_SESSION_IDLE`). Session cookies are `HttpOnly`, `SameSite=Lax`,
and `Secure` when `FOREMAN_PUBLIC_URL` starts with `https://`. Users who
arrive through trusted headers get no session cookie: the proxy decides
who is signed in, and signing out of Foreman ends nothing for them. (P0)

12.9. **Audit log**: who did what and when, for start, stop, restart,
switch, snapshot and restore, mod changes, settings changes, user changes,
and every RCON command. Viewable and filterable in the UI. Never holds a
secret. Each entry holds the time, the user (the name stays on the entry
after the user is deleted), the action, the world and a detail. Server
control records start, stop, restart, forced stop, switch (on the world
switched from, with both ids), game image download and
every console command (8.4). (P1, except RCON commands, which are recorded
from the first version that has a console: P0)

12.10. **API tokens.** A user can create, name, list and revoke their own
API tokens in the UI. A token acts as that user: everything it does is
recorded in the audit log under that user and the token's name. A token
is shown once when created, stored only as a hash, can have an expiry
date, and has one of two scopes: **read** (only the read tools in 21.3)
or **operate** (also the actions in 21.4). Tokens are rate-limited. A
token cannot create tokens, manage users, or sign in to the UI. (P1)

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

## 13. Foreman's own settings

13.1. App settings an admin changes in the UI: snapshot retention, the
default stop countdown, scheduled restarts, notifications, and portal
credentials when not set by environment. Each shows where its value comes
from (environment or UI), and environment values cannot be changed in the
UI. (P0 for those whose features are P0)

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
| Internet | Mod portal, image registry and its tag list, release list, webhook targets | Nothing |
| AI client | An assistant calling the MCP endpoint (section 21) | Only its token's scope; steerable by text it reads (14.12) |

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
  optionally `GET /events` filtered to containers. Foreman does not use
  `GET /events`; it polls (8.7).
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
save names, snapshot folder names, uploaded zip entries, mod file names) is resolved and checked
to stay inside the intended directory before use. Symlinks inside a world
are not followed out of it. Uploaded zips are checked entry by entry and
refused if any entry would land outside its target. (P0)

14.6. **Uploads** have size limits (an app setting, default 1 GB for a
save, 200 MB for a mod) and are written to a temporary file and checked
before they replace anything. (P0)

14.7. **Requests that change state** require a signed-in session and
protection against cross-site request forgery. A browser post is accepted
only from Foreman's own origin, or from the origin of
`FOREMAN_PUBLIC_URL` when that is set, which covers a proxy that rewrites
the `Host` header. (P0)

14.8. **Secrets** (the RCON password, the portal token, the OIDC client
secret, the game password, `FOREMAN_SECRET_KEY`, and session tokens) never
appear in a log line, an API response, an error message, the audit log,
or the UI. This includes game log lines Foreman relays: it removes the RCON
password and the game password from every line (8.6), parsing the line
before it redacts so a short password cannot hide what the line means. The
console follows one list of commands that carry a secret: `/config set
password` has its argument masked in the audit log, on the page and in
relayed log lines, and the reply to `/config get password` is masked. A Lua
command is recorded as typed (8.4). The one place the RCON password is
visible is the game process's arguments, an accepted risk (4.5). (P0)

14.9. **RCON input.** Commands built by Foreman from user text (chat
messages, player names) are escaped so the text cannot become a different
command. The raw console (8.4) sends what the admin typed, by design. (P0)

14.10. **Outbound requests.** Foreman talks only to the mod portal, the
factorio.com release list, Docker Hub's list of `factoriotools/factorio`
tags (`hub.docker.com`, 10.7), and webhook URLs an admin configured. Calls
to the release list and to Docker Hub follow no redirects, take at most 10
seconds, and read at most 2 MB (tag list) or 64 KB (release list). Image
pulls are made by the Docker daemon, not by Foreman. It does not follow
redirects for webhooks. (P0)

14.11. **Least privilege.** Foreman's container runs as non-root with a
read-only root filesystem except its data directory and a temporary
directory. The game container is also unprivileged (4.6): a game user or
group ID of 0 is refused at start (3.6). (P0)

14.12. **AI clients.** An AI client connected through MCP (section 21) is
its own trust zone: trusted only for its token's scope, and assumed to
be steerable by any text it reads. Players control chat, player names,
and much of the game log, so a player can write instructions aimed at
the AI. Every tool result that carries such text marks it as untrusted
player content, and no MCP tool can delete, force-stop, restore, change
mods or settings, run raw RCON commands, or manage users and tokens.
(P1)

14.13. **Pages refuse framing.** Every page carries
`Content-Security-Policy: frame-ancestors 'none'` and
`X-Frame-Options: DENY`, so no other site can show Foreman inside a frame
and trick a click. (P0)

14.14. **Sign-in logs name the client, never the username.** A failed or
refused sign-in is logged with the client address and no username,
because people sometimes type a password into the username field. (P0)

14.15. (Removed: adopted mode was dropped; ADR 0005.)

14.16. **The VPN sidecar.** **Accepted risk:** the sidecar (4.13) is a
container Foreman did not create. It sees all of the game's traffic and
usually holds `NET_ADMIN`, and Foreman does not police its capabilities.
The defences: RCON is bound only to the sidecar's game-network address,
so VPN peers cannot reach it; every range of the sidecar's networks counts
as a game range for trusted-header sign-in (12.6); and a start is refused
when the sidecar is missing, not running, on the host network, or not on
the game network (4.13). The game container itself keeps the shape in
4.6. (P1)

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
and tells the user to reload. (P0)

## 17. Accessibility and design

17.1. Controls Foreman owns meet WCAG 2.2 AA (the Web Content
Accessibility Guidelines, level AA). Every UI change ships with an
automated accessibility scan in its end-to-end tests. (P0)

17.2. Every page works at 320 CSS pixels wide without horizontal
scrolling, with touch targets at least 24 by 24 CSS pixels. The success
scenarios in docs/VISION.md are tested at phone width. (P0)

17.3. Light and dark themes, following the system setting. (P1)

17.4. State is never shown by colour alone: running, stopped, crashed and
crash-looping each have a word. (P0)

17.5. Destructive actions say exactly what they will do and which snapshot
will be taken first, and need a confirmation. (P0)

17.6. A design document, docs/DESIGN.md, sets type, colour tokens,
spacing and components before the first UI epic. (P0)

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
(10.7), and VPN sidecars (4.13, ADR 0006).

18.6. **Epic 6: Mods.** Section 9.

18.7. **Epic 7: Generating worlds and game settings.** Generate (5.6),
clone (5.7), and section 11.

18.8. **Epic 8: Live operations.** Players (8.2, 8.3), chat (8.5), crash
reasons (8.7, 8.8), scheduled restarts (7.12), notifications (8.10 to 8.12),
the low-disk warning (8.13).

18.9. **Epic 9: AI access.** API tokens (12.10) and the MCP endpoint
(section 21).

18.10. **Epic 10: Release.** OIDC (12.7), the full audit log (12.9),
archive (5.9), Foreman's settings page (13.1), the operator guide with the
socket proxy example (14.2), and the first public image
on GHCR (3.1).

## 19. Milestone gates

At each gate the project stops and proves something before going further.
The architect also reviews the whole system at each gate.

19.1. **Gate A, after Epic 5: the reference deployment runs.** Foreman
runs behind Caddy and Authentik with trusted-header sign-in, the existing
hand-renamed folders are imported as worlds, each running on the game
version it was made with, and an admin can switch between two of them and
back from a phone with no save lost. Todd retires OFSM only after this
gate passes.

19.2. **Gate B, after Epic 7: the new-user path works.** From `docker run`
to a running Space Age server with three portal mods in under ten
minutes, following only Foreman's own operator guide.

19.3. **Gate C, after Epic 10: v1.** Every P0 and P1 requirement is met; a
bad mod update is undone in one click; security-reviewer finds nothing
open in section 14; the image is published.

## 20. Decisions needed

Each is put to Todd with options and a recommendation before the epic
that needs it starts. The answer is written into the section that cites
it, and the decision is removed from this list.

| Id | Question | Needed by | Sections |
|---|---|---|---|
| D10 | Whether the chat-message tool (21.4) is in v1 or waits, given that it lets an AI speak to players | Epic 9 | 21.4 |
| D9 | How Generate makes a map from a preset or a map exchange string, given that the image generates only from `map-gen-settings.json` and `map-settings.json` | Epic 7 | 5.6 |

The tech stack (vision question 6) is settled in docs/STACK.md, not here.

## 21. AI access (MCP)

21.1. Foreman serves an MCP (Model Context Protocol) endpoint, so an AI
assistant can check on and run the server as tools. It is part of the
one Foreman container, not a separate service, and is off unless
`FOREMAN_MCP=on`. (P1)

21.2. The endpoint accepts only API tokens (12.10), never a browser
session. Every tool calls the same code the UI uses, so snapshots before
risky operations, graceful stops, the pre-start check and the audit log
apply exactly as they do for a person. (P1)

21.3. **Read tools** (read or operate scope): server status (7.1, 7.2);
list worlds (5.4); players online (8.1); the most recent log lines,
with secrets removed (8.6); list snapshots (6.4); list a world's mods
and available updates (9.3). Player-written text in any result is marked
untrusted (14.12). (P1)

21.4. **Action tools** (operate scope only): start (7.3); graceful stop
(7.6); graceful restart (7.8); switch world (7.9); take a snapshot
(6.3); and send an in-game message as the server (8.5), which is
**Decision D10**. Each action returns what happened, including the
snapshot it took. (P1)

21.5. **Never through MCP**, whatever the scope: delete a world, forced
stop, restore a snapshot, mod changes, settings changes, game version
changes, raw RCON commands, and user or token management. These stay
with a person in the UI. (P1)
