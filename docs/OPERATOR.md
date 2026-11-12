# Operator guide

This guide takes you from nothing to a running Factorio server that
players can join. It covers the new-user path: install Foreman, create the
first admin, generate a Space Age world, install mods from the mod portal,
start the server and join it. It also covers the security advice every
operator needs, running behind a reverse proxy, importing and switching
worlds, webhook notifications, the low-disk warning, and what to do when
something fails.

The rest of the guide comes later, with Epic 11 (docs/SPEC.md, section
18): signing in with OpenID Connect, the archive, Foreman's settings page
and API tokens.

## 1. What you need

- **A Linux host with Docker** and Docker Compose. Foreman runs in a
  container and creates the game's container itself.
- **git and make**, to build Foreman's image (section 2). No image is
  published yet.
- **A factorio.com account**, to download mods from the mod portal. Foreman
  needs its username and its token. Sign in at factorio.com and open
  <https://factorio.com/profile>: both are shown there. The token is not
  your password, but treat it like one. Every admin's mod downloads use
  this one account, so you may prefer to create a separate factorio.com
  account just for this server.
- **Space Age for each player who joins a Space Age world.** The server
  itself needs no purchase: the game image Foreman runs is the free
  headless server, which includes Space Age. Each player's own copy of
  Factorio must have Space Age to join.

Foreman downloads only when you ask it to: the game image for a world's
version, and mods. It never downloads anything when it starts.

## 2. Get the image

No image is published yet; it will be in Epic 11. Build it yourself:

```sh
git clone https://github.com/toddawhittaker/foreman.git
cd foreman
make image
```

The image is tagged `foreman:dev`. Use `make image` rather than a bare
`docker build`: the Makefile passes the exact Go build image the project
pins. The build takes a few minutes the first time.

## 3. The data directory

Foreman keeps everything in one directory: the worlds (`worlds/<id>/`),
the snapshots (`snapshots/`) and its database. Make it before the first
start, owned by user and group 845, which is the user Foreman's image and
the game both run as:

```sh
mkdir -p data
sudo chown 845:845 data
```

Foreman creates the game container with a bind mount of one world's
folder, and Docker resolves that path on the host. So Foreman must know
where the data directory is on the host. It reads this from its own
container's mounts, so with the compose file below you set nothing. If that
fails, Foreman refuses to start and says so; then set
`FOREMAN_HOST_DATA_DIR` to the host path.

`FOREMAN_GAME_UID` and `FOREMAN_GAME_GID` set the game's user and group.
Both default to 845, the default of the `factoriotools/factorio` image.
Leave them alone unless you have a reason. Neither may be 0, because the
game would then run as root; Foreman refuses to start if you try.

## 4. Security you should know about

### Docker access is root on the host

Whoever controls Foreman's access to Docker controls the host. Foreman
needs Docker to create, start and stop the game. Do not mount
`/var/run/docker.sock` into Foreman. Put a Docker socket proxy between
them instead: a small container that holds the socket and passes on only
the kinds of request it is told to allow.

Foreman calls only these Docker endpoints: ping and version; list,
inspect, create, start, stop, kill and remove containers, and read their
logs and stats; inspect an image and pull an exact tag. With the Tecnativa
socket proxy in the compose file below, that means `CONTAINERS=1`,
`IMAGES=1` and `POST=1`, with `EVENTS=0`.

A socket proxy narrows what Foreman can call, but it is not a full
boundary. It allows requests by path and cannot look inside a request to
create a container, so it cannot stop a privileged container being
created. Foreman itself only ever creates the game container in a fixed,
unprivileged shape. Treat Foreman's admin accounts with the same care as
root on the host.

Never publish the socket proxy's port. Foreman cannot see a published port
from inside its own container, so it cannot warn you.

### RCON stays private

Foreman controls the game through RCON (the game's remote console). Foreman
makes the RCON password itself, keeps it in the world's `config/rconpw`,
and never shows it. RCON is never published on the host. The game sits on
its own network, which holds only Foreman and the game, and Foreman
refuses to create a game when anything else is on that network. Keep your
reverse proxy and the socket proxy off it.

### Hide process details on the host

Factorio takes the RCON password only on its command line, so anyone who
can list the processes on the host can read it. To hide other users'
processes, mount `/proc` with `hidepid=2` on the host. Add this line to
`/etc/fstab`:

```
proc /proc proc defaults,hidepid=2 0 0
```

and apply it now with `sudo mount -o remount,hidepid=2 /proc`. Check first
that nothing you run on the host needs to see other users' processes.

## 5. The compose file

This compose file runs Foreman with local accounts (a username and password
kept by Foreman) and no reverse proxy. Put it next to `data/` as
`compose.yaml`.

```yaml
services:
  foreman:
    image: foreman:dev
    restart: unless-stopped
    environment:
      FOREMAN_DATA_DIR: /data
      FOREMAN_GAME_NETWORK: foreman-game
      DOCKER_HOST: tcp://socket-proxy:2375
      # Where players join, shown on the Server page. Use your host's name or
      # IP address; the port is 34197 when none is given.
      FOREMAN_PUBLIC_ADDRESS: 192.168.1.20
      # Lets you enter the factorio.com account on Foreman's account page.
      # At least 32 random characters, for example from: openssl rand -hex 32
      FOREMAN_SECRET_KEY: replace-with-at-least-32-random-characters
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    # Least privilege: Foreman writes only /data and /tmp.
    read_only: true
    tmpfs:
      - /tmp
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    # Foreman asks Docker for its own mounts when it starts, so the proxy
    # must be up first.
    depends_on: [socket-proxy]
    networks: [game, docker]

  socket-proxy:
    image: ghcr.io/tecnativa/docker-socket-proxy:v0.5.0@sha256:1f5038b54f06c3e18422902cf00ba21803d1c97805aae032e5e6673d532d3459
    restart: unless-stopped
    environment:
      CONTAINERS: "1"
      IMAGES: "1"
      POST: "1"
      EVENTS: "0"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks: [docker]

networks:
  game:
    name: foreman-game
  docker:
    name: foreman-docker
```

Notes:

- `ports: "8080:8080"` serves Foreman over plain HTTP on every address of
  the host. That is fine on a home network you trust. Anywhere else, put
  Foreman behind a reverse proxy with HTTPS (section 6) and remove this
  line.
- `FOREMAN_PUBLIC_ADDRESS` is only shown to you; Foreman never guesses it.
  Leave it out and the Server page shows no join address.
- The secret key encrypts the factorio.com token that Foreman stores. Keep
  it apart from the data directory, and do not lose it: with a new key you
  must enter the account again. For Docker secrets, use
  `FOREMAN_SECRET_KEY_FILE`, which names a file holding the key.
- Instead of the key, you can give the account directly as
  `FOREMAN_FACTORIO_USERNAME` and `FOREMAN_FACTORIO_TOKEN` (or
  `FOREMAN_FACTORIO_TOKEN_FILE`). Set both or neither. Then the account
  page only says where the account comes from.
- Do not set `hostname:` on Foreman; it finds its own container by its
  default hostname.
- Leave `FOREMAN_GAME_NETWORK_CONTAINER` unset. VPN sidecars are not built
  yet, and Foreman refuses to start when it is set.

Start it:

```sh
docker compose up -d
```

## 6. Optional: behind a reverse proxy

A reverse proxy in front of Foreman gives you HTTPS, and it can also sign
people in for Foreman (forward authentication): it sends Foreman headers
that name the user and their groups. Foreman calls these trusted headers.

Add these settings to Foreman's `environment`, with your own names:

```yaml
      FOREMAN_PUBLIC_URL: https://factorio.example.com
      FOREMAN_AUTH_METHODS: local,headers
      FOREMAN_AUTH_USER_HEADER: X-Forwarded-User
      FOREMAN_AUTH_GROUPS_HEADER: X-Forwarded-Groups
      FOREMAN_AUTH_REQUIRED_GROUP: factorio-admins
      FOREMAN_TRUSTED_PROXIES: 172.30.0.10/32
```

- Use the header names your proxy actually sends. All three header
  settings are required; without a required group, anyone the proxy lets
  through would become an admin. Group lists are split on `|` and `,`.
- With `local` left out, there is no setup page: the first user who
  arrives through the headers becomes the admin.
- Foreman honours the headers only from addresses in
  `FOREMAN_TRUSTED_PROXIES`, and never from the game network. Make it the
  proxy's own single address, as a `/32`.

The best layout runs the proxy in a container on a network that Foreman
shares and the game does not, with Foreman's port not published at all.
Remove `ports:` from Foreman, add a `front` network to it, and give that
network a fixed subnet that no other Docker network uses:

```yaml
# In Foreman's compose file: add front to Foreman's networks list,
# and this to the networks section.
  front:
    name: foreman-front
    ipam:
      config:
        - subnet: 172.30.0.0/24
```

```yaml
# In the reverse proxy's compose file.
services:
  proxy:
    networks:
      foreman-front:
        ipv4_address: 172.30.0.10   # the same address as FOREMAN_TRUSTED_PROXIES

networks:
  foreman-front:
    external: true   # created by Foreman's compose file; start that stack first
```

The proxy forwards to `http://foreman:8080`. If the proxy runs on the host
instead, publish Foreman's port on `127.0.0.1` only
(`"127.0.0.1:8080:8080"`), never on every address. Then
`FOREMAN_TRUSTED_PROXIES` is the address the proxy's connections arrive
from, which with Docker's port forwarding is the gateway of the network
that carries the published port (for example `172.30.0.1/32`). Never use a
whole network range, and never a network the game is on.

## 7. First run: create the admin

Open `http://<your host>:8080`. Foreman shows **Create the first admin
account**. Enter a username and a password of at least 12 characters, type
the password again under **Confirm password**, and choose **Create admin
account**. The page closes for good once the account exists.

## 8. Generate a Space Age world

1. Choose **Worlds** in the menu, then **Generate a world**.
2. Under **World name**, type a name.
3. Under **Game version**, pick a 2.x version from the list. The list comes
   from Docker Hub and marks the stable release; pick that one unless you
   need another. **Or type a version** takes an exact version such as
   `2.0.72`.
4. Tick **Space Age is on**.
5. Leave **Also download the game image if this host does not have it**
   ticked. Foreman then pulls the game image for that version from Docker
   Hub after the world is made.
6. Leave the map settings at the default preset, or change them.
   **Preview map** draws the map first, once the game image is on the
   host.
7. Choose **Generate world**.

The first world you make becomes the active one: the world the Server page
starts. Its map is made when it first starts.

## 9. Install three mods from the mod portal

1. Enter the factorio.com account. Open the menu with your name at the top
   right, choose **factorio.com account**, fill in **factorio.com username**
   and **factorio.com token**, and choose **Save account**. Foreman never
   shows the token again. (If you set the account in the environment
   instead, skip this step.)
2. Choose **Worlds**, open your world, and choose its **Mods** tab.
3. Under **Find a mod**, type part of the first mod's name in **Mod name**
   and choose **Search**. Only mods for this world's game version are
   listed.
4. Tick the mod you want.
5. Search again for the second mod and tick it, then for the third. Ticks
   are kept across new searches and pages; mods ticked in an earlier search
   are listed under **Also ticked in other searches or on other pages**,
   where you can untick one.
6. Choose **Add ticked mods to plan**. Foreman shows one plan: the three
   mods, any mods they depend on, and the steps it will take. Read it.
7. Choose **Install 3 mods** (the count is higher when the plan adds mods
   they depend on).

Foreman takes one "Before mod change" snapshot first, so the change can be
undone. If the server were running, Foreman would warn players, stop the
game, change the mods and start it again; on a stopped world it just
changes the mods.

## 10. Start the server and join it

1. Choose **Server** in the menu. If the game image is still downloading,
   wait; if it is missing, choose **Download game image**.
2. Choose **Start server**. The page shows each step until the state is
   **Running**. The first start of a generated world makes its map, which
   takes a little longer.
3. The page shows "Players join at" and the address, with a button to copy
   it. Without `FOREMAN_PUBLIC_ADDRESS`, use your host's name or IP address.
4. In Factorio, choose **Multiplayer**, then **Connect to address**, and
   type the address. The game's port is UDP 34197; you can leave it out.
   Factorio offers to download the world's mods from the mod portal when a
   player joins.

Foreman publishes UDP port 34197 on every address of the host. Open that
port in the host's firewall, and forward it on your router for players
outside your network. Foreman's web port (8080) does not need to be open
to players.

## 11. Import a world you already have

1. Copy the folder into `data/import/<name>/`, with the save zip at the
   top level and the mods in `mods/`
   (`data/import/family-2025/family.zip`, `data/import/family-2025/mods/...`).
   The folder may instead use the Factorio server layout (`saves/`,
   `mods/`, `config/`): saves are read from the top level and from
   `saves/`, and the settings files in `config/` are copied too, except
   `rconpw`, because Foreman makes its own.
2. Choose **Worlds**, then **Import a world**. Under **Import a folder**,
   pick the folder, check the game version and Space Age setting, and
   choose **Import folder**. Import copies; the source is never changed.

To switch, choose **Switch** next to another world on the Worlds page. The
confirmation says which world stops. Both worlds get a "Before switch"
snapshot, and the game starts again on the new world, with that world's
game version, if it was running.

## 12. Webhooks

A webhook is a web address that Foreman posts a short message to when
something happens on a server, so a chat channel or phone hears about it
without anyone watching Foreman. Admins manage them on the Notifications
page, `/settings/notifications` (SPEC 8.10 to 8.12).

**What it needs.** Webhook addresses are stored encrypted with
`FOREMAN_SECRET_KEY` (section 5). Without the key, webhooks are
unavailable and the page says so. If you change the key, the stored
webhooks show as unreadable and nothing is sent to them; type each address
again.

**Presets.** Each webhook has a preset, which sets the shape of the
message:

- **Discord**: a Discord webhook address (`https://discord.com/api/webhooks/...`).
  Mentions are turned off, so a world or player name cannot ping anyone.
- **ntfy**: the address of an ntfy topic. The message is plain text. Crash,
  crash loop and low disk are sent at high priority.
- **Generic JSON**: any address. Foreman posts the JSON body described
  below.

**http:// and secrets in addresses.** `https://` is best. `http://` is
allowed, but the message and the address then travel unencrypted. After
you save, the page shows only the service and the host name, never the
rest of the address, and the address is never logged. A few services put
the secret in the host name itself; for those the secret is shown.

**Events.** Each webhook has its own list of events, all on by default.
Each event has a stable id, which the generic body uses:

| Id | Sent when |
| --- | --- |
| `start` | the server started |
| `stop` | the server stopped |
| `crash` | the server crashed |
| `crash-loop` | the server kept crashing, so Foreman stopped restarting it |
| `player-join` | a player joined |
| `player-leave` | a player left |
| `scheduled-restart` | a scheduled restart ran, or was skipped |
| `low-disk` | free disk space fell too low (section 13) |

**Language.** The message text is in the language chosen for that webhook.
It starts as the language of the admin who adds it.

**Delivery.** Each attempt times out after 5 seconds. A failed delivery is
retried at most twice, after 1 and 5 seconds. A refusal that would not
change, such as a 404, is not retried. Foreman does not follow redirects.
Sending never delays the server or any action on it. Events wait in a
short queue in memory, one per webhook; if a target is so slow that its
queue fills, the extra events are dropped and one line is logged. Events
are not kept across a Foreman restart. Failures are logged without the
address.

A `docker restart` run outside Foreman sends a `start` webhook with no
`stop` before it, because Foreman sees the game running again.

A scheduled restart sends both a `scheduled-restart` and a `start`
webhook. A restart from the Server page sends only `start`.

### The generic JSON body

Foreman sends a `POST` with `Content-Type: application/json`. Example:

```json
{
  "version": 1,
  "event": "player-join",
  "time": "2026-11-12T23:30:05Z",
  "world": {
    "id": "main-base",
    "name": "Main base"
  },
  "text": "Main base: ann joined.",
  "player": "ann"
}
```

Fields in every body:

- `version`: the body format, `1`.
- `event`: one of the ids above.
- `time`: when it happened, UTC, in RFC 3339.
- `world`: `id` (the world's folder name) and `name` (as shown in Foreman).
- `text`: a sentence in the webhook's language, ready to show.

Fields that appear only for some events:

- `player`: the player's name, for `player-join` and `player-leave`.
- `reason`: why the server crashed, for `crash` and `crash-loop`, only when
  Foreman could tell. One of `mod-missing-dependency`, `mod-error`,
  `save-unreadable`, `port-in-use`, `out-of-memory` or
  `mods-for-other-version`. Leave room for others: treat an unknown value
  as "unknown".
- `skipped`: `true` or `false`, for `scheduled-restart`. `true` means the
  restart did not run.
- `free_bytes`: free disk space in bytes, for `low-disk`.

A field that does not apply is left out, not sent empty. Within version 1,
new fields may be added, but none is removed or renamed, and no meaning
changes. Ignore fields you do not know. A change that breaks this raises
`version`.

## 13. Low disk

While a game runs, Foreman checks the free space on the disk that holds the
worlds. It warns when free space falls below 4 GB, and shows the warning on
the status page and sends the `low-disk` event (section 12). The warning
clears only once free space is above 4.5 GB, so it does not flip back and
forth near the line. Each fall below 4 GB sends one event. These numbers
are fixed; they are not a setting (SPEC 8.13).

The reason: the game's autosaves can fail when the disk fills, and
Foreman itself refuses to write a world's files unless 2 GB stay free
(SPEC 6.11). The warning comes at twice that reserve, early enough for you
to free space or add disk before saves are at risk. Foreman checks only
while the game runs.

## 14. Restart schedule

Foreman can restart the game every day, for example to free memory. There
is one schedule for all of Foreman, and it restarts whichever world is
active. Set it on the Restart schedule page, `/settings/restart-schedule`,
which is in the user menu and in the Foreman settings sub-navigation
(SPEC 7.12).

**Settings.** A switch (off until you save it on), a time of day, a time
zone and a wait limit. The time is a daily time in the zone you choose,
from the standard IANA list of names such as `America/New_York`; the zone
starts as UTC and the time as 04:00. The wait limit is a whole number of
hours from 0 to 12, and starts at 1.

**What happens at the set time.**

1. If the game is running, Foreman waits until nobody is online. It asks
   the game over RCON (section 4) about every 30 seconds. If it cannot
   get an answer, or the game is busy with another action, it counts that
   as someone being online.
2. When nobody is online, Foreman restarts the game after a countdown of
   60 seconds, announced in the game's chat. The countdown always runs,
   even on an empty server, so a player who joins during it is told.
3. If someone is online during the countdown, or joins during it, the
   countdown is cancelled, the game tells them in chat, and the wait in
   step 1 starts again. The countdown is cancelled the same way if Foreman
   cannot tell who is online.
4. The wait limit is counted from the set time. When it runs out with
   players still online, that day's restart is skipped, and the
   `scheduled-restart` webhook is sent with `skipped` set to `true`
   (section 12). A limit of 0 skips at once if anyone is online. A restart
   that runs sends the same event with `skipped` set to `false`.
5. An admin who presses **Cancel stop** during the countdown skips that
   day's restart. No webhook is sent for that.

The Server page shows where today's restart stands.

**What it never does.**

- It never starts a game that is stopped, starting, crashed or crash
  looping. If the game is not running at the set time, or stops, crashes
  or is restarted by hand during the wait, nothing more happens that day.
- It does not make up a restart that was missed while Foreman or its host
  was not running, or while the host was suspended. A restart reached up
  to 10 minutes late still runs. Later than that it is counted as missed,
  and the next one is the next set time after now. A wait that Foreman
  wakes into more than 10 minutes past its limit is skipped the same way,
  and shown as missed. No webhook is sent for a missed restart.
- It never starts a game. The restart refuses if the game stopped or
  crashed after the schedule looked, and does nothing.
- A scheduled restart that fails sends no webhook. The Server page shows
  the failure.

**Daylight saving time.** If the clocks skip the set time, the restart
runs at the next valid minute. If the clocks pass the set time twice, it
runs once, the first time.

Saving the schedule is recorded in the audit log, and the restarts it
makes are recorded under the name "restart schedule".

## 15. When something fails

- **Foreman's own log.** Run `docker compose logs -f foreman`. Every error
  page that says "the details are in Foreman's log" means this log. It
  never holds a password or token.
- **The game's log.** Choose **Console** in the menu: it streams the
  game's log as the game prints it, with the RCON password hidden.
  **Download log** saves it.
- **The Server page** says why a start failed, in the game's own words
  where it can, and offers **Try to start again**.
- **Foreman will not start.** The log names the setting that is wrong.
  Common causes: the data directory is not owned by 845:845, the socket
  proxy is not running, or a required setting is missing.
- **Snapshots.** Choose **Snapshots** in the menu to see and restore them.
  Foreman takes one before every risky change, and restoring one puts the
  world back exactly as it was.

### Restoring a snapshot by hand

A snapshot is a plain folder, `data/snapshots/<world-id>/<time>/`, named by
its UTC time, such as `20261103T142501Z`. It has the same shape as the
world's folder, `data/worlds/<world-id>/`, plus a `snapshot.json` that
lists its files. If Foreman cannot restore it for you:

1. Stop the server, and stop Foreman: `docker compose stop foreman`.
2. Move the world's folder aside, rather than copying over it. Save and mod
   zips in a snapshot are hard links to the same files as the world's, so
   writing into the world's files in place would change the snapshot too.

   ```sh
   sudo mv data/worlds/<world-id> data/worlds-<world-id>.old
   sudo cp -a data/snapshots/<world-id>/<time> data/worlds/<world-id>
   sudo rm data/worlds/<world-id>/snapshot.json
   sudo chown -R 845:845 data/worlds/<world-id>
   ```

3. Start Foreman again with `docker compose start foreman`, check the
   world, and then delete the `.old` folder.
