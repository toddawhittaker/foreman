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
- **Managed mode**: Foreman creates and owns the game container.
- **Adopted mode**: Foreman controls a game container that someone else
  defined, for example in a compose file.
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
with an exact version tag. It needs no other service to run. Until v1 the
image is published to the Forgejo registry; from v1 it is also published
to GHCR (GitHub's container registry). (P0)

3.2. **Configuration.** Everything an operator sets at install comes
from environment variables. Any secret value can instead come from a file
named by the same variable with a `_FILE` suffix (for example
`FOREMAN_OIDC_CLIENT_SECRET_FILE`), so Docker secrets and sops work. When
both are set, Foreman refuses to start and says which one to remove. (P0)

3.3. **The data directory** is one mounted path. It holds the worlds, the
snapshots, and the app database. Its layout is in section 5.2. (P0)

3.4. **The app database** holds only users, sessions, app settings,
notification settings and the audit log. It never holds game data: saves,
mods and game settings live only in world directories. (P0)

3.5. **Host paths.** In managed mode, Foreman creates containers with
bind mounts, which the Docker daemon resolves on the host. Foreman must
know the host path of its data directory. It takes it from
`FOREMAN_HOST_DATA_DIR`, or discovers it by inspecting its own container's
mounts. If neither works, Foreman refuses to start managed mode and says
so in plain words. (P0)

3.6. **File ownership.** Foreman runs as a non-root user. Everything it
writes into a world is readable and writable by the game's user and group,
which default to 845:845 (the `factoriotools/factorio` default) and are
configurable. (P0)

3.7. **No downloads at startup.** Foreman never downloads anything when it
starts. It downloads only a game image or a mod the admin asked for, and
reads the mod portal and the game's release list when the admin opens a
page that shows them. (P0)

3.8. **Upgrading Foreman never touches a running game** and never rewrites
a world's files without a snapshot first. A Foreman upgrade that changes
the data directory layout migrates it only after snapshotting every
affected world. (P0)

3.9. **Works on a phone.** Every page is usable at 320 CSS pixels wide
(section 17). (P0)

## 4. Runtime modes

4.1. The operator chooses the mode at install with `FOREMAN_MODE`
(`managed` or `adopted`). Managed is the default. Changing mode later is
not supported in v1. (P0)

4.2. **Rely on the image as little as possible.** The
`factoriotools/factorio` entrypoint changes between image versions (save
selection, missing-save handling and shutdown all changed in 2026), and
pinned worlds keep old images for years. Foreman depends only on: the
`/factorio` directory with `saves/`, `mods/` and `config/`; loading the
newest save; generating a map when `saves/` is empty; reading
`config/rconpw`; and the `RCON_PORT`, `PUID` and `PGID` variables. Every
other behaviour Foreman needs, it does itself. (P0)

### Managed mode

4.3. Foreman creates the game container from `factoriotools/factorio` at
the active world's pinned version tag, with a Docker restart policy of
`unless-stopped`, so the game comes back after a reboot whether or not
Foreman is running. The active world's directory is bind-mounted at
`/factorio`. Switching worlds, or changing a world's game version,
removes the container and creates it again with the new mount or tag.
This also allows one container per world later, each on its own port,
with no change to the layout (5.3). (P0)

4.4. The game port (UDP, default 34197) is published on the host. The
RCON port is never published; Foreman reaches it on a Docker network
shared only by Foreman and the game container. (P0)

4.5. Foreman generates and owns the RCON password. It is stored only in
the world directory's config, readable by the game, and never shown. (P0)

4.6. The game container's shape is fixed by Foreman: no privileged mode,
no host network, no added capabilities, and bind mounts only from inside
the data directory. Nothing an admin types in the UI changes that shape
except the published game port. (P0)

### Adopted mode

4.7. The operator names an existing container with
`FOREMAN_GAME_CONTAINER`. Foreman starts, stops and restarts it, reads its
logs and stats, and talks to it over RCON. It never recreates or
reconfigures it. (P0)

4.8. Foreman switches worlds by changing what the container's bind mount
presents, without anyone editing the compose file per switch. The
container's folder for `/factorio` (the game folder) is also mounted into
Foreman, at the path in `FOREMAN_GAME_DIR`, and holds:

- `worlds/<world-id>/`, one directory per world (5.1);
- `current`, a relative symbolic link to `worlds/<world-id>` for the
  active world;
- `saves`, `mods` and `config`, fixed relative links to `current/saves`,
  `current/mods` and `current/config`.

A switch, done only while the game is stopped, creates a new link
`current.new` and renames it over `current`. That rename is atomic, so at
every instant, including after a reboot mid-switch, the game folder
presents exactly one whole world. The first time Foreman adopts a game
folder that still has real `saves`, `mods` and `config` directories, it
stops the game, snapshots them, and turns them into the first world, with
a confirmation that says so. (P0)

4.9. The game version belongs to whoever owns the container definition.
Foreman shows the running version, warns when the active world's save was
made by a different version, and never tries to change the image. (P0)

4.10. The RCON password comes from the active world's `config/rconpw`,
re-read after every switch, or from `FOREMAN_RCON_PASSWORD` (or its
`_FILE` form). (P0)

4.11. Foreman refuses to adopt a container whose environment sets
`SAVES`, `MODS`, `CONFIG` or any `SERVER_*_FILE` variable, because those
move the game's files away from where switching puts them. It says which
variable to remove. (P0)

4.12. Foreman warns, on the status page, when the adopted container
publishes its RCON port on the host. (P0)

## 5. Worlds

### Model

5.1. A world is a directory holding:

- `saves/`: the save files;
- `mods/`: mod zips, `mod-list.json` and `mod-settings.dat`;
- `config/`: `server-settings.json`, `map-gen-settings.json`,
  `map-settings.json`, `server-adminlist.json`, `server-banlist.json`,
  `server-whitelist.json`, and (managed mode) `rconpw`;
- `world.json`: Foreman's metadata for the world: display name, pinned
  game version, Space Age flag, which save to load (section 7.11), and the
  time it was created.

A person can read, back up and move a world without Foreman. `world.json`
is plain JSON, and Foreman ignores fields it does not know so a newer
Foreman can add fields. (P0)

5.2. The data directory holds `snapshots/` and the app database. In
managed mode it also holds `worlds/<world-id>/`. In adopted mode the
worlds live in the game folder instead (4.8), because the links that
switch them must resolve inside the game container. A world's id is a
short, safe directory name that never changes; its display name can.
(P0)

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
or changes the source. (P0)

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
operation does not run. (P0)

6.3. An admin can take a snapshot by hand at any time. (P0)

6.4. Snapshots are listed per world, newest first, with label, reason,
time and size. (P0)

6.5. **Restore** puts a world back exactly as the snapshot recorded it,
in one click with a confirmation. Restoring the active world stops it
gracefully first (section 7.6), snapshots its current state, restores, and
starts it again. Restore is itself undoable: the snapshot taken before it
is the undo. (P0)

6.6. **Retention** is an app setting: keep the newest *n* automatic
snapshots per world (default 20). Manual snapshots and the final snapshot
of a deleted world are never removed automatically. Foreman shows total
snapshot disk use. (P0)

6.7. **How snapshots are stored.** A snapshot is a plain directory,
`snapshots/<world-id>/<time>/`, with the same shape as the world, plus a
`snapshot.json` manifest (6.1) that records each file's modification
time. A person can read it, copy it, or restore it by hand, as the
operator guide shows. (P0)

6.8. Save and mod zips are hardlinked into the snapshot rather than
copied; small files (JSON, `.dat`, `rconpw`) are copied. This is safe
because Factorio writes every save as a new file. Foreman itself never
changes a zip in place. If a hardlink fails, for example because the
snapshot directory is on another filesystem, Foreman copies the file
instead, and the status page says snapshots are using full copies. In
adopted mode the operator guide says to keep the data directory on the
same filesystem as the game folder, outside the game's mount. (P0)

6.9. A restore puts every file's modification time back from the
manifest, because hardlinked saves share their time with the world
(7.11). (P0)

6.10. A snapshot of a running world is taken after a `/server-save` over
RCON, so it holds the current game state. (P0)

## 7. Server control

### Status

7.1. Foreman shows the game's state as one of: **stopped**, **starting**,
**running** (the server reached the "InGame" state), **stopping**,
**crashed**, or **crash-looping**. (P0)

7.2. Alongside the state: uptime, game version, active world, the loaded
save, and CPU and memory use. (P0)

### Start, stop, restart

7.3. **Start** runs the pre-start check (7.4), then starts the game
container with the active world. It reports success only when the server
has reached "InGame" **and** answers over RCON; reaching "InGame" alone is
not enough, because the RCON port can fail to open after it. If that does
not happen within a time limit (default 5 minutes), Foreman reports a
failed start with the reason if it can name one (section 8.7). (P0)

7.4. **The pre-start check.** Factorio loads a save whose mods are
missing, or whose Space Age content is turned off, with no error, and
deletes that content from the save the next time it saves. So before
every start, Foreman reads the save that will load (section 9.9) and
compares it with the world. It refuses to start, and says why, when:

- a mod in the save is missing or disabled in the world, or the world's
  Space Age flag is off and the save uses DLC mods;
- the save was made by a newer game version than the world's;
- the world has no save, unless the admin is generating a new map
  (otherwise the image silently generates one).

The admin can then fix the world (for example with sync from save), or
start anyway after a confirmation that names exactly what the game will
delete, with a snapshot first. A save Foreman cannot read is reported as
such, never guessed at. (P0)

7.5. **Stop** is graceful by default (section 7.6). (P0)

7.6. **A graceful stop**: if players are online, warn them in game with a
countdown (default 60 seconds, configurable per action); run
`/server-save` over RCON and wait for it to finish; then stop the
container with an explicit stop timeout. If RCON cannot be reached,
Foreman says so and offers a forced stop instead of silently forcing one.
An exit after a stop Foreman asked for is never reported as a crash,
whatever its exit code. (P0)

7.7. **A forced stop** stops the container without saving. It is a
separate action with its own confirmation that says progress since the
last save will be lost. (P0)

7.8. **Restart** is a graceful stop followed by a start. (P0)

### Switching worlds

7.9. **Switch** to another world, as one operation with these steps in
order: graceful stop of the current world; snapshot of both worlds; the
pre-start check on the new world; make the new world the one the
container presents (4.3 in managed mode, 4.8 in adopted mode); start; confirm it is running (7.3).
If any step fails, Foreman stops, says which step failed, and offers to
switch back, which restores the previous world as active. (P0)

7.10. Foreman records a switch in progress (from which world, to which
world, and the last step done) before its first step. On startup it
finds any unfinished switch and either completes it or reports it, and
never leaves the game running a world that is only partly switched in.
(P0)

7.11. **Which save loads** is set per world: the latest save, or a named
save file. The game always loads the save with the newest modification
time. To load a named save, Foreman sets that file's modification time to
now, while the game is stopped, just before start. It never sets a time
in the future, which would pin that save above every later autosave.
After a crash, the game therefore restarts from the newest autosave.
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

8.1. List who is online now and for how long. (P0)

8.2. Kick, ban, unban, promote to admin, demote, and add to or remove from
the whitelist, each with a confirmation for the destructive ones. (P1)

8.3. View and edit the admin list, ban list and whitelist files of any
world, including one that is not running. (P1)

### Console and chat

8.4. An RCON console: send a command, see its response. Every command
sent is recorded in the audit log (section 12.9). (P0)

8.5. In-game chat: see recent chat and send a message as the server.
(P1)

### Logs and crashes

8.6. A live stream of the game's log, with the most recent lines shown on
open. The view stays usable on a phone and does not grow without limit.
The image prints the RCON password in its log when it starts; Foreman
removes it from every line before showing or storing it (section 14.8).
(P0)

8.7. **Crash detection.** When the game stops without being asked to,
Foreman marks it crashed, using Docker's container state (exit code,
out-of-memory flag, start error, restart count), since every game
failure exits with the same code. It names the reason when it can: a mod
that failed to load, a corrupt save, a version mismatch, a port in use,
or out of memory. Otherwise it says the reason is unknown and shows the
last 20 log lines; it never guesses a reason. (P0 to detect a crash; P1
to name the reason)

8.8. **The reason catalogue** is a data file of log patterns, each
naming a reason. Patterns match message text, never source file and
line numbers, which change between game versions. Each entry has a test
fixture captured from a real game container. The catalogue's scenarios
are re-run against every image tag Foreman supports before that tag is
listed as supported. (P1)

8.9. **Crash loops.** If the game crashes three times within ten minutes,
Foreman marks it crash-looping and stops it (in managed mode by stopping
the container; in adopted mode the same, since Docker's restart policy
would otherwise keep it spinning). An admin must start it again by hand.
(P0)

### Notifications

8.10. Webhooks on: start, stop, crash, crash loop, player join, player
leave, and scheduled restart. Each event can be turned on or off per
webhook. (P1)

8.11. Presets for Discord and ntfy, plus a generic JSON webhook whose body
format is documented. (P1)

8.12. A failed webhook delivery is logged and retried at most twice; it
never blocks or delays a server operation. (P1)

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
guessing. The same reader serves the pre-start check (7.4) and the
version warning in adopted mode (4.9). (P0)

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

## 10. Game versions (managed mode)

10.1. Each world shows its pinned game version. (P0)

10.2. When a newer stable or experimental release exists, the world shows
a notice with the release's changelog. Foreman checks only when an admin
opens a page that shows it, never on a timer at startup. (P1)

10.3. **Update** a world's game version: snapshot, pull the exact image
tag, restart, and confirm "InGame". (P0)

10.4. **Rollback** restores the pre-update snapshot and the previous
version together, as one action. (P0)

10.5. Foreman never updates a game version, a mod, or itself on its own.
(P0)

10.6. In adopted mode, sections 10.2 to 10.4 do not apply; section 4.9
does. (P0)

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
page is reachable only until an admin exists. (P0)

12.4. Passwords are stored with a slow, salted password hash, chosen in
docs/STACK.md. Sign-in failures are rate-limited per username and per
address. (P0)

12.5. **Trusted proxy headers** (forward auth from Authentik, Authelia or
oauth2-proxy): a configured header names the user, and an optional
configured header lists groups, with an optional required group. Foreman
honours these headers **only** on requests whose direct peer address is
inside `FOREMAN_TRUSTED_PROXIES` (a list of address ranges). From any
other address the headers are ignored. A user seen for the first time is
created automatically. (P0)

12.6. If trusted headers are enabled and `FOREMAN_TRUSTED_PROXIES` is
empty, Foreman refuses to start. (P0)

12.7. **OpenID Connect**: discovery URL, client ID and client secret
(env or `_FILE`), and an optional required group claim. (P1)

12.8. Sessions are server-side, expire after a configurable idle time,
and end on sign-out. Session cookies are `HttpOnly`, `SameSite=Lax`, and
`Secure` when served over HTTPS. (P0)

12.9. **Audit log**: who did what and when, for start, stop, restart,
switch, snapshot and restore, mod changes, settings changes, user changes,
and every RCON command. Viewable and filterable in the UI. Never holds a
secret. (P1, except RCON commands, which are recorded from the first
version that has a console: P0)

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
| Data directory on the host | Worlds, snapshots, database | Files Foreman wrote itself |
| Internet | Mod portal, image registry, release list, webhook targets | Nothing |

14.2. **Docker access is root on the host.** Whoever controls Foreman's
Docker access controls the host. A socket proxy narrows which endpoints
Foreman can call, but common proxies allow by path prefix and cannot
inspect a container-create request, so they cannot stop a privileged
container being created. The operator guide says this plainly, and shows
a socket proxy configuration that permits the smallest set the proxy can
express for each mode. Foreman's own guarantee is 14.4. (P0)

14.3. **Docker API surface.** Foreman calls only these Docker Engine API
endpoints:

- Both modes: `GET /_ping`, `GET /version`, `GET /containers/json`,
  `GET /containers/{id}/json`, `POST /containers/{id}/start`,
  `POST /containers/{id}/stop`, `POST /containers/{id}/kill` (forced stop
  only), `GET /containers/{id}/logs`, `GET /containers/{id}/stats`, and
  optionally `GET /events` filtered to containers.
- Managed mode only: `POST /images/create` (pull an exact tag),
  `GET /images/{name}/json`, `POST /containers/create`, and
  `DELETE /containers/{id}`.

Never: exec, build, volumes, image delete, or anything under `/system`
or `/auth`. Adding an endpoint changes this section first. (P0)

14.4. **What Foreman will never do with Docker**: create a privileged
container, use the host network or PID namespace, add capabilities, mount
the Docker socket into the game, or bind-mount any host path outside the
data directory. (P0)

14.5. **Paths from users.** Every path built from user input (world names,
save names, uploaded zip entries, mod file names) is resolved and checked
to stay inside the intended directory before use. Symlinks inside a world
are not followed out of it. Uploaded zips are checked entry by entry and
refused if any entry would land outside its target. (P0)

14.6. **Uploads** have size limits (an app setting, default 1 GB for a
save, 200 MB for a mod) and are written to a temporary file and checked
before they replace anything. (P0)

14.7. **Requests that change state** require a signed-in session and
protection against cross-site request forgery. (P0)

14.8. **Secrets** (the RCON password, the portal token, the OIDC client
secret, the game password, `FOREMAN_SECRET_KEY`, and session tokens) never
appear in a log line, an API response, an error message, the audit log,
or the UI. This includes game log lines Foreman relays, since the image
prints the RCON password at startup (8.6). (P0)

14.9. **RCON input.** Commands built by Foreman from user text (chat
messages, player names) are escaped so the text cannot become a different
command. The raw console (8.4) sends what the admin typed, by design. (P0)

14.10. **Outbound requests.** Foreman talks only to the mod portal, the
factorio.com release list, the image registry, and webhook URLs an admin
configured. It does not follow redirects for webhooks. (P0)

14.11. **Least privilege.** Foreman's container runs as non-root with a
read-only root filesystem except its data directory and a temporary
directory. (P0)

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
reloading. (P0)

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

18.3. **Epic 3: Server control in managed mode.** Creating the game
container (4.3 to 4.6); status (7.1, 7.2); start, stop, restart (7.3 to
7.8), including the pre-start check and the save reader it needs
(7.4, and 9.9); save selection (7.11); the log stream and crash
detection (8.6, 8.7, 8.9); the player list (8.1); the RCON console (8.4)
and its audit entries.

18.4. **Epic 4: World switching and adopted mode.** Switching (7.9, 7.10);
adopted mode (4.7 to 4.12).

18.5. **Epic 5: Mods.** Section 9.

18.6. **Epic 6: Generating worlds and game settings.** Generate (5.6),
clone (5.7), and section 11.

18.7. **Epic 7: Game versions.** Section 10.

18.8. **Epic 8: Live operations.** Players (8.2, 8.3), chat (8.5), crash
reasons (8.7, 8.8), scheduled restarts (7.12), notifications (8.10 to 8.12).

18.9. **Epic 9: Release.** OIDC (12.7), the full audit log (12.9),
archive (5.9), Foreman's settings page (13.1), the operator guide with the
socket proxy example (14.2), the MIT license, and the first public image
on GHCR (3.1).

## 19. Milestone gates

At each gate the project stops and proves something before going further.
The architect also reviews the whole system at each gate.

19.1. **Gate A, after Epic 4: the reference deployment runs.** Foreman
runs in adopted mode behind Caddy and Authentik with trusted-header
sign-in, the existing hand-renamed folders are imported as worlds, and an
admin can switch between two of them and back from a phone with no save
lost. Todd retires OFSM only after this gate passes.

19.2. **Gate B, after Epic 6: the new-user path works.** From `docker run`
to a running Space Age server with three portal mods in under ten
minutes, following only Foreman's own operator guide.

19.3. **Gate C, after Epic 9: v1.** Every P0 and P1 requirement is met; a
bad mod update is undone in one click; security-reviewer finds nothing
open in section 14; the image is published.

## 20. Decisions needed

Each is put to Todd with options and a recommendation before the epic
that needs it starts. The answer is written into the section that cites
it, and the decision is removed from this list.

| Id | Question | Needed by | Sections |
|---|---|---|---|
| D9 | How Generate makes a map from a preset or a map exchange string, given that the image generates only from `map-gen-settings.json` and `map-settings.json` | Epic 6 | 5.6 |

The tech stack (vision question 6) is settled in docs/STACK.md, not here.
