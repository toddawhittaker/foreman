# Operator guide

This guide takes you from nothing to a running Factorio server that
players can join. It covers the new-user path: install Foreman, create the
first admin, generate a Space Age world, install mods from the mod portal,
start the server and join it. It also covers the security advice every
operator needs, running behind a reverse proxy, importing and switching
worlds, webhook notifications, the low-disk warning, what to do when
something fails, and, optionally, letting players join through a VPN
sidecar such as Tailscale. It also covers signing in with OpenID Connect
(section 17), Foreman's own settings, users and audit log (section 18) and
archiving worlds (section 19).

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

No image is published yet; it will come with the release (Epic 11b).
Build it yourself:

```sh
git clone https://github.com/toddawhittaker/foreman.git
cd foreman
make image
```

The image is tagged `foreman:dev`. Use `make image` rather than a bare
`docker build`: the Makefile passes the exact Go build image the project
pins. The build takes a few minutes the first time.

Foreman's image is built for amd64 and arm64, so it also runs on an arm64
host. Be aware that the Factorio game image is
experimental on arm64 upstream, and Foreman's tests against a real game run
on amd64 only. Plan on amd64 for a server that matters.

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
logs and stats; inspect an image and pull an exact tag; and, if allowed,
read Docker's event stream. With the Tecnativa socket proxy in the compose
file below, that means `CONTAINERS=1`, `IMAGES=1`, `POST=1` and
`EVENTS=1`.

`EVENTS=1` is recommended, not required. Foreman reads only the
out-of-memory and exit events of its own game containers. Docker restarts
a game the kernel killed for lack of memory within a fraction of a second
and forgets why it stopped, so without events Foreman can miss the cause,
for example on a busy host or when the game crashes again at once, and
shows a plain crash instead of "out of memory". With `EVENTS=0` Foreman
works as before: it checks Docker every few seconds, notes once in its log
that events are refused, and shows no error. An install set up with
`EVENTS=0` keeps working; change it to `1` and recreate the proxy
(`docker compose up -d socket-proxy`), then restart Foreman, to get the
events.

A socket proxy narrows what Foreman can call, but it is not a full
boundary. It allows requests by path and cannot look inside a request to
create a container, so it cannot stop a privileged container being
created. Foreman itself only ever creates the game container in a fixed,
unprivileged shape. Treat Foreman's admin accounts with the same care as
root on the host.

Never publish the socket proxy's port. Foreman cannot see a published port
from inside its own container, so it cannot warn you.

`DOCKER_HOST` is a `tcp://` address in the compose file below, because the
socket proxy is another container. A `tcp://` Docker address is not
encrypted and has no sign-in. Anyone who can send packets to it along the
network path has root on the host, as if they held the socket. Keep it on a
private Docker network that holds only Foreman and the socket proxy, as the
compose file does, and prefer the socket proxy to pointing Foreman at a
Docker daemon that listens on the network. Never point `DOCKER_HOST` at an
address on a network you do not fully control.

### RCON stays private

Foreman controls the game through RCON (the game's remote console). Foreman
makes the RCON password itself, keeps it in the world's `config/rconpw`,
and never shows it. RCON is never published on the host. The game sits on
its own network, which holds only Foreman and the game, and Foreman
refuses to create a game when anything else is on that network. Keep your
reverse proxy and the socket proxy off it. The one exception is a VPN
sidecar (section 16), whose own address there is the only one RCON
listens on.

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
      EVENTS: "1"
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
- Leave `FOREMAN_GAME_NETWORK_CONTAINER` unset unless you want players to
  join through a VPN sidecar (section 16).

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
- With a VPN sidecar (section 16), never put the sidecar's VPN address or
  a VPN range in `FOREMAN_TRUSTED_PROXIES`.

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

### Let players reach the public mods page

Foreman can show the mods of the running world to anyone, without a
sign-in, so players can download them. An admin turns this on at
**Settings**, **Public mods**; it is off until then. While it is off,
every path that starts with `/public/` answers "not found".

When a proxy signs people in, it will ask players to sign in before they
can see that page. Add one rule to the proxy: requests whose path starts
with `/public/` go to Foreman without authentication, and every other path
stays behind the proxy's sign-in. Match on the path prefix `/public/`,
nothing wider. The rule needs nothing else from the proxy, so it works the
same with any proxy that can skip authentication for a path.

- The public paths show the running world's name, its game version, and
  its turned-on mods, and send those mod files. They show no save,
  setting, password, player or log.
- Foreman ignores the sign-in headers and any cookie on these paths, so
  nobody is signed in by them.
- At most 4 public downloads run at once. A fifth visitor is told to try
  again in 5 seconds. Turn the page off to stop them all.
- Test the rule from a machine that is not signed in: open
  `https://<your address>/public/mods`. It should show the page (or "not
  found" while the setting is off), never the proxy's sign-in page. Then
  open `https://<your address>/worlds`; it should still ask you to sign in.

### Connect an AI assistant

Foreman can let an AI assistant, such as Claude Code, read the server's
state and run a few actions through MCP (Model Context Protocol), at
`/mcp` (SPEC section 21). It is off until an admin turns it on.

1. Open **Settings**, **API tokens**, and turn MCP access on. To keep it
   off whatever the page says, set `FOREMAN_MCP: off` in the compose file.
   `off` is the only value Foreman accepts; anything else stops it at
   startup. The page then says the environment turned MCP off.
2. On the same page, name a token and pick its scope. **Read** only
   looks. **Operate** can also start, stop and restart the server, switch
   world, take snapshots and send chat. **Admin** can also change worlds,
   mods, settings and more (see "Admin tokens" below). Pick the smallest
   scope that does the job. Foreman shows the secret once, so copy it then. It starts with
   `fmn_`; treat it like a password.
3. Add Foreman to the assistant. For Claude Code:

   ```sh
   claude mcp add --transport http foreman https://<your Foreman>/mcp --header "Authorization: Bearer <token>"
   ```

   The page that shows the secret also shows the address to use.

Admin tokens:

- An **Admin** token can do nearly everything an admin can do in the web
  pages: manage worlds (including archiving them, section 19), snapshots,
  mods, game settings, game versions, players, the console, webhooks, the
  restart schedule and the General settings (section 18). It cannot manage
  users, tokens or the MCP switch, and it cannot upload or download files.
  Give an admin token only to an assistant you trust to act for you, and
  prefer **Operate** or **Read** when that is enough. An assistant that is
  tricked by text it reads can still use whatever its token allows.
- Actions that delete or overwrite something work in two steps, as the web
  pages ask you to confirm. The first call changes nothing. It returns a
  preview of what will change or be lost, the snapshot Foreman will take
  and who is online, with a confirm code. The assistant calls the tool again
  with the same arguments and the code, within 5 minutes. A code works once,
  only for that token, tool and arguments. If something changed since the
  preview, such as a new mod release, Foreman refuses and gives a new
  preview and code. A Foreman restart voids all codes.
- The step guards against mistakes. It does not stop an assistant that has
  been steered into confirming, so the scope is the real limit.
- Everything an admin token does is in the audit log with the token's name.

Things to know:

- A token expires 30 days after it was made or last renewed. From 5 days
  before, its owner sees a banner on every page with a Renew button, and an
  expired token's call is refused with a message that says so. Renewing
  keeps the same secret, so the assistant needs no change. A revoked token
  cannot be renewed. If you suspect a secret leaked, revoke the token and
  make a new one: renewing does not change the secret. To be told by
  webhook, tick `token-expiring` on a webhook (section 12).
- Chat sent through a token starts with the token's name, such as
  `[Claude] Restarting in 5 minutes`.
- A token can take at most 10 snapshots per world. An admin deleting some
  in the web UI frees room.
- Set `FOREMAN_PUBLIC_URL` (section 6). Foreman refuses any `/mcp` request
  that carries an `Origin` header unless it equals that address. Real MCP
  clients send none. Without `FOREMAN_PUBLIC_URL`, any `Origin` is refused.
- If a proxy in front of Foreman asks people to sign in, it will also block
  the assistant, which cannot sign in that way. Add one rule to the proxy:
  requests whose path starts with `/mcp` go to Foreman without the proxy's
  sign-in. Foreman checks the token itself and ignores any sign-in header on
  this path. Every other path stays behind the proxy. Or point the assistant
  at Foreman directly on a network you trust.
- Test it from a machine that is not signed in. A call with the token
  should work. A call with no token should get 401.

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
3. Under **Find a mod**, type part of the first mod's name in **Mod name**.
   The results appear as you type. Only mods for this world's game version
   are listed.
4. Choose **+** on the mod you want. It joins the plan beside the results.
5. Search for the second mod and choose **+**, then the third. The picks are
   kept while you search, page and leave the tab. The plan lists each pick
   with the mods it needs under it, and you can take a pick out with its
   cross or **Clear plan**.
6. Choose **Review and install 3 mods**. Foreman shows one plan: the three
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
to players. With a VPN sidecar the sidecar publishes the port instead
(section 16).

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

**Events.** Each webhook has its own list of events, all on by default
except `token-expiring`. Each event has a stable id, which the generic
body uses:

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
| `token-expiring` | an API token will expire in 5 days or in 1 day (section 6, "Connect an AI assistant") |

`token-expiring` is off until you tick it, because a webhook often posts to
a channel that players read. It names the token and its owner, never the
secret.

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
- `token`: for `token-expiring`, an object with `name` (the token's name),
  `owner` (the user who made it) and `expires` (when it expires, UTC, in
  RFC 3339). For this event `world` is still sent, with an empty `id` and
  an empty `name`, because the event belongs to no world.

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
which the main Settings tab opens; it is also in the Foreman settings
sub-navigation (SPEC 7.12).

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
- **A VPN sidecar.** When a start is refused because of the sidecar, or
  players cannot join through it, see section 16.
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

## 16. Optional: players join through a VPN sidecar

You do not need a VPN. Without one, Foreman publishes the game's port on
the host (section 10) and players join that. This section is for when you
also want players outside your network to join through a VPN, such as
Tailscale, without opening a port on your router.

A VPN sidecar is a container you define and run next to Foreman. When you
name it in `FOREMAN_GAME_NETWORK_CONTAINER`, Foreman runs the game inside
the sidecar's network. Players on the VPN then join the sidecar's VPN
address, and players on your LAN join the port the sidecar publishes on
the host. Both can play at the same time.

Foreman never installs, signs in, configures, restarts or removes the
sidecar, and it never holds a VPN credential. The setup below uses
Tailscale; any VPN that runs in a container works the same way.

### How it fits together

- At every start Foreman looks the sidecar up by name, checks it, and
  creates the game in that exact container's network. If the sidecar is
  missing, not running, on the host's network, inside another container's
  network, or not on `FOREMAN_GAME_NETWORK`, the Server page says so and
  the game does not start. Nothing in the world is changed first.
- The game listens on UDP 34197 on every interface it shares with the
  sidecar: the VPN's, and the Docker networks'.
- Foreman publishes no port for the game. Docker lets only the container
  that owns a network publish its ports, so the sidecar publishes UDP
  34197 for LAN players, in its own definition.
- RCON listens only on the sidecar's address on `FOREMAN_GAME_NETWORK`, and
  Foreman reaches it there. It is not on the VPN interface.
- `FOREMAN_GAME_NETWORK` is required with a sidecar, even when Foreman runs
  outside a container, and the sidecar must be on it.

### The compose file

[docs/examples/tailscale-sidecar/compose.yaml](examples/tailscale-sidecar/compose.yaml)
is the compose file from section 5 with a Tailscale sidecar added. Copy it
next to `data/` as `compose.yaml` and replace the example values. It holds
no Tailscale key. It defines Foreman, the socket proxy and the sidecar,
never the game: Foreman creates the game's container itself at every start.

The sidecar's part of it:

```yaml
  tailscale:
    image: tailscale/tailscale:v1.104.7@sha256:72c1e19e0aedca96b8533bbb0b4368564410ff20b17e53b804eb6c1f04a31be4
    container_name: foreman-tailscale
    restart: unless-stopped
    environment:
      TS_USERSPACE: "false"
      TS_STATE_DIR: /var/lib/tailscale
      TS_AUTH_ONCE: "true"
      TS_HOSTNAME: factorio
      TS_EXTRA_ARGS: --advertise-tags=tag:factorio
      TS_BOOT_TIMEOUT: "5m"
    volumes:
      - tailscale-state:/var/lib/tailscale
    devices:
      - /dev/net/tun:/dev/net/tun
    cap_add: [NET_ADMIN, NET_RAW]
    ports:
      - "34197:34197/udp"
    networks: [game]
```

and in Foreman's `environment`:

```yaml
      FOREMAN_GAME_NETWORK_CONTAINER: foreman-tailscale
```

What each part does:

- **`container_name`** gives the sidecar the fixed name Foreman looks for.
  Without it, Compose names the container after the project.
- **`TS_USERSPACE: "false"`** turns on Tailscale's kernel networking: a
  real `tailscale0` interface inside the sidecar's network, which the game
  listens on. This guide supports kernel networking only. Tailscale's
  userspace mode works differently and is not tested with Foreman.
- **`TS_STATE_DIR` and the `tailscale-state` volume** keep the sidecar's
  identity in your tailnet. Without them, every restart is a new machine
  that must be signed in again.
- **`TS_AUTH_ONCE: "true"`** signs in only when the saved state is not
  already signed in.
- **`TS_HOSTNAME`** names the machine in your tailnet.
- **`TS_EXTRA_ARGS`** requests `tag:factorio` at sign-in. Define its owners
  in your tailnet policy before starting the sidecar.
- **`TS_BOOT_TIMEOUT`** gives initial sign-in five minutes in this pinned
  release; its default is one minute.
- **`devices` and `cap_add`** are what Tailscale's documentation asks for
  in kernel mode: the TUN device, `NET_ADMIN` and `NET_RAW`. They are the
  sidecar's own. The game container still runs with every capability
  dropped (SPEC 4.6).
- **`ports`** publishes UDP 34197 on the host for LAN players.
- **`networks: [game]`** puts the sidecar on `FOREMAN_GAME_NETWORK`, which
  Foreman requires. See "What the game can reach" before adding others.

Keep routing and forwarding features out of this example: `TS_ROUTES`,
`TS_DEST_IP`, `TS_SERVE_CONFIG`, `TS_TAILNET_TARGET_IP`,
`TS_TAILNET_TARGET_FQDN`, `--advertise-routes`, `--advertise-exit-node`,
and `--accept-routes`. They can widen what reaches the game network or
what the game reaches. Also leave `TS_SOCKS5_SERVER`,
`TS_OUTBOUND_HTTP_PROXY_LISTEN`, `TS_ENABLE_METRICS` and
`TS_ENABLE_HEALTH_CHECK` unset: they add listeners this setup does not need.
See [Tailscale's Docker parameters](https://tailscale.com/docs/features/containers/docker/docker-params).

The host must have the TUN device. Check with `ls -l /dev/net/tun`; if it
is missing, load the module with `sudo modprobe tun`. When the host is
itself a container, such as a Proxmox LXC container, pass `/dev/net/tun`
through to it first.

To use a newer Tailscale release, take its exact tag and digest from
Docker Hub, change the `image:` line, and see "When the sidecar restarts"
below: replacing the sidecar cuts the running game off.

### Sign the sidecar in

First configure `tag:factorio` and its owners as shown under "Limit who can
reach the game over the VPN" below. Sign in as an owner of that tag. A tagged device uses its
tag's identity instead of the signer's personal access; leave the tag
without outbound grants and remove broader rules that would grant it
unwanted access. Verify the tag in the admin console before starting a
game, especially when reusing an existing state volume. See
[Tailscale tags](https://tailscale.com/docs/features/tags).

The example uses interactive sign-in, without an auth key:

1. Start everything with `docker compose up -d`.
2. Run `docker compose logs -f tailscale` and wait for the line
   `To authenticate, visit:` followed by a link.
3. Open the link, sign in to your tailnet, and approve the machine. Keep
   the link to yourself: whoever opens it can add this machine to their
   own tailnet. This example allows five minutes for startup and sign-in;
   the pinned release defaults to one minute. If it expires, Docker
   restarts the sidecar with a new link. Use the newest link in the log.
   The setting is defined in the pinned release's
   [container startup source](https://github.com/tailscale/tailscale/blob/v1.104.7/cmd/containerboot/settings.go).
4. Check it: `docker exec foreman-tailscale tailscale status` lists the
   machine, and `docker exec foreman-tailscale tailscale ip -4` shows its
   tailnet address.

The state volume keeps the sign-in across restarts. Tagged devices have
key expiry disabled by default; check your device's actual expiry setting
in the admin console.

If you prefer an auth key, mount it only into the sidecar as a Compose
secret and set `TS_AUTHKEY: file:/run/secrets/ts_authkey`. Keep the source
file outside git, readable only by its owner. Do not pass the key itself
in `environment` or `env_file`: Docker includes those values in inspect
responses, which Foreman reads. The pinned release supports the
[file-based key form](https://github.com/tailscale/tailscale/blob/v1.104.7/cmd/tailscale/cli/up.go).
For example, add to your local compose file:

```yaml
services:
  tailscale:
    environment:
      TS_AUTHKEY: file:/run/secrets/ts_authkey
    secrets:
      - ts_authkey
secrets:
  ts_authkey:
    file: /absolute/private/path/ts_authkey
```

This keeps key contents out of the inspect environment, not out of reach
of the host or someone with unrestricted Docker administration.

### Join the game

Start the server from Foreman as in section 10. Then:

- **LAN players** connect to the host's address, as before.
- **VPN players** use the IPv4 address from
  `docker exec foreman-tailscale tailscale ip -4`. The port is 34197;
  they can leave it out. MagicDNS names such as `factorio` may resolve to
  IPv6 too; use the name only after testing it with your game clients.

`FOREMAN_PUBLIC_ADDRESS` shows one join line on the Server page. Choose
the address most of your players use.

If the sidecar does not publish UDP 34197 on the host, the Server page
warns "LAN players cannot reach the game: foreman-tailscale does not
publish UDP 34197 on the host." The game still starts, because VPN-only
play can be what you want. The warning is checked when the game starts,
so after you change the sidecar's ports, restart the game.

A published port does not prove LAN players can reach the game. Foreman
reads only what Docker reports, and cannot see:

- **the host address the port is published on.** `"34197:34197/udp"`
  publishes on every address of the host. A line such as
  `"192.168.1.20:34197:34197/udp"` publishes on that one address, and LAN
  players must use it.
- **a different host port.** With `"34200:34197/udp"`, LAN players use
  port 34200; put it in `FOREMAN_PUBLIC_ADDRESS` too, as
  `192.168.1.20:34200`.
- **the host's firewall.** Open the UDP port there, as in section 10.

### Limit who can reach the game over the VPN

Tailscale's access rules decide which people and machines in your tailnet
can reach the sidecar. They are your configuration; Foreman cannot see or
enforce them. Allow only UDP 34197 to the game, for only the people who
play. For example, in the tailnet policy file, with the machine tagged
`tag:factorio`:

```json
{
  "groups": {
    "group:factorio-players": ["alice@example.com", "bob@example.com"]
  },
  "tagOwners": {
    "tag:factorio": ["you@example.com"]
  },
  "grants": [
    {
      "src": ["group:factorio-players"],
      "dst": ["tag:factorio"],
      "ip": ["udp:34197"]
    }
  ]
}
```

This narrows access only if no other rule in the file allows more, such
as a default rule that allows everything.

### RCON stays private

RCON listens on TCP 27015 only at the sidecar's address on
`FOREMAN_GAME_NETWORK`, never on every address. Check it once the game is
running: from another machine on your LAN, and from a device on your
tailnet, a connection to TCP 27015 must fail.

```sh
nc -vz -w 3 192.168.1.20 27015   # from the LAN: must fail
nc -vz -w 3 100.101.102.103 27015 # from the tailnet: must fail
```

Replace `100.101.102.103` with the actual address from
`docker exec foreman-tailscale tailscale ip -4`; a DNS lookup failure is
not a successful isolation check. Confirm the game is running and its
Console works before testing these connections.

That address is private, not firewalled. A subnet route, exit node or
forwarding rule that carries VPN traffic into the game network can reach
it, which is why the settings listed above stay out. RCON still needs its
password, which Foreman keeps and never shows.

### What the game can reach

The game shares the sidecar's whole network, so it can reach everything
the sidecar can:

- every Docker network the sidecar is on, and the services there;
- every VPN peer your tailnet rules let the sidecar reach;
- anything listening on the sidecar's own loopback address.

You may put the sidecar on other Docker networks, including networks
other services share. Foreman allows that, but the choice and what it
exposes are yours. Foreman refuses to start the game when any network the
sidecar is on overlaps `FOREMAN_TRUSTED_PROXIES`, or exposes the socket
proxy Foreman uses. Never put the sidecar on the socket proxy's network
(`foreman-docker`) or the reverse proxy's network (`foreman-front`).

The sidecar sees all of the game's traffic and holds `NET_ADMIN`. Foreman
does not check what the sidecar does; trust its image as you trust
Foreman's (SPEC 14.16).

### Sign-in through a reverse proxy

If Foreman starts while the sidecar is stopped or missing, trusted-header
sign-in stays unavailable until the sidecar runs and Foreman can read all
of its Docker network ranges. Local sign-in still works.

With a sidecar, Foreman counts every Docker network the sidecar is on as a
game network, and ignores sign-in headers from those addresses (section
6). Docker cannot tell Foreman the VPN's addresses. So never put the
sidecar's tailnet address, or a VPN range such as Tailscale's
`100.64.0.0/10` or `fd7a:115c:a1e0::/48`, in `FOREMAN_TRUSTED_PROXIES`: the game, or anything
else that reaches Foreman through the sidecar, could then pose as the
proxy and sign in as anyone. Use only the single address of a separate reverse proxy, or
local accounts.

### When the sidecar restarts

Foreman does not watch the sidecar. When the sidecar is restarted or
recreated while the game runs (after a change to its definition, a new
image, or `docker compose up -d` that replaces it), the game can lose its
network. Players drop, nobody new can join, and Foreman may lose its RCON
connection, so the Console and Players pages stop updating. The Server page
can keep showing the game as running, because Foreman does not watch the
sidecar; the Console then says it cannot reach the game over RCON.

Afterwards, restart the game from Foreman:

1. Choose **Server**, then **Restart server**. Foreman saves, stops, and
   starts the game in the sidecar's current network.
2. If the page says "Cannot reach the game over RCON, so it cannot warn
   players or save first", Foreman cannot save it. Choose **Force stop**,
   which stops the game without saving: progress since the last autosave
   is lost. Then choose **Start server**.

Change the sidecar when nobody is playing.

After a host reboot or Docker restart, check both player connectivity
and a Console command through Foreman. Docker restart policies do not
provide Compose's startup ordering, and a game may run while its saved
RCON bind address is no longer usable. If stopped, start it from Foreman;
if either player connectivity or RCON fails, follow the restart and
force-stop recovery steps above. Reboot recovery still needs to be
checked by you after restarting your test host: join with a game client
and send a Console command before relying on unattended recovery. This
example does not guarantee ordering.

## 17. Optional: sign in with OpenID Connect

Foreman can sign people in through any identity provider that speaks OpenID
Connect (OIDC), whether you run it yourself or use a hosted one. Foreman
needs no reverse proxy for this. The provider must be able to put a
username and a list of groups in its ID token.

### Set up the provider

1. Register Foreman as a web application (a confidential client) that uses
   the authorization code flow. Register this redirect URL, which is
   `FOREMAN_PUBLIC_URL` followed by `/signin/oidc/callback`:

   ```
   https://factorio.example.com/signin/oidc/callback
   ```

2. Note the client ID and the client secret the provider gives you.
3. Make a group for the people who may use Foreman, and add them to it.
4. Make sure the ID token carries two claims. Foreman asks for the scopes
   `openid profile email groups`. It reads `preferred_username`, which
   becomes the user's name, and `groups`, a list of group names. These
   claim names are fixed, not settings. If your provider keeps groups
   under another scope or claim name, map them to `groups` in its
   settings. Foreman reads only the ID token; it never calls the
   provider's userinfo address.

### Set up Foreman

Add these settings to Foreman's `environment`, with your own values:

```yaml
      FOREMAN_PUBLIC_URL: https://factorio.example.com
      FOREMAN_AUTH_METHODS: local,oidc
      FOREMAN_OIDC_ISSUER: https://id.example.com
      FOREMAN_OIDC_CLIENT_ID: foreman
      FOREMAN_OIDC_CLIENT_SECRET_FILE: /run/secrets/foreman_oidc_secret
      FOREMAN_OIDC_REQUIRED_GROUP: factorio-admins
```

- `FOREMAN_AUTH_METHODS` is a list. Add `oidc` to it. Keep `local` to keep
  password accounts, or leave it out to use single sign-on alone.
- `FOREMAN_OIDC_ISSUER` must be an `https` address. Foreman refuses to start
  with an `http` one, so the client secret never crosses the network in the
  clear. Write it exactly as the provider reports it, including any
  trailing slash: the ID token's issuer must equal it letter for letter.
- `FOREMAN_OIDC_CLIENT_SECRET` holds the secret. For Docker secrets, use
  `FOREMAN_OIDC_CLIENT_SECRET_FILE`, which names a file holding it, as in
  the example.
- `FOREMAN_OIDC_REQUIRED_GROUP` is required. Everyone who signs in is an
  admin, so without a group, anyone the provider signs in could use
  Foreman. A person outside the group sees a page that says their account
  is not allowed to use Foreman.
- `FOREMAN_PUBLIC_URL` is required, because the provider sends people back
  to it.
- If any of these is missing, Foreman reports every missing one at startup
  and does not start. It never prints the secret.

### How it behaves

- Foreman contacts the provider the first time someone signs in, never at
  startup. A wrong issuer therefore shows up as a "cannot reach the
  provider" page at sign-in, not as a startup error. Foreman keeps the
  provider's answer once it works, and tries again at the next sign-in
  after a failure.
- Foreman, inside its container, must reach the issuer at the same address
  your browser uses. A provider on the same host or network, reached by a
  public name, often answers browsers but not containers on that host (a
  "hairpin" problem). If sign-in says the provider is unavailable while the
  provider's own page opens in your browser, make that name resolve and
  connect from inside Foreman's container too, for example with a Docker
  network alias or an `extra_hosts` entry. Calls to the provider take at
  most 10 seconds and follow no redirects, so the issuer must be the final
  address, not one that redirects. The provider's certificate must be valid
  for that name.
- A user is found by the provider's issuer and the `sub` claim, never by
  name alone. A person who is renamed at the provider keeps their Foreman
  account.
- The first time someone signs in, Foreman creates a user with their
  `preferred_username` and writes `user-create` with the detail `oidc` to
  the audit log. If a Foreman user of that name already has a password, or
  is already linked to another identity, Foreman refuses the sign-in with a
  page that says why. Delete that user in the Users tab
  (section 18).
- With `headers` on as well (section 6), the proxy still comes first: a
  request the proxy signs in never goes to the provider. A user made by the
  proxy has no password and no link, so the first single sign-on with the
  same name links that user to the provider's identity, and they keep their
  history.
- With `local` on as well, the sign-in page shows the single sign-on button
  and the password form. With `oidc` alone, the page is one button, and
  there is no setup page and no password form; the first person to sign in
  is the first user.
- **Turn off self-service username changes at the provider.** The link by
  name above is the reason. Until a proxy-made user has signed in through
  single sign-on once, anyone in the group who can rename themselves at the
  provider could rename themselves to that user's name and take over the
  account. After the first sign-in the link is by `sub`, and a rename no
  longer matters. If you never combine `headers` and `oidc`, there is no
  such user.
- Foreman checks the group only at sign-in. Removing someone from the group
  stops their next sign-in, not a session already open (sessions end after
  `FOREMAN_SESSION_IDLE`, 7 days by default). To cut someone off now,
  remove them from the group, then delete them in the Users tab.
- Signing out ends only Foreman's session, not the provider's. With `oidc`
  alone, the signed-out page offers the same single sign-on button, and if
  the provider's session is still open you are signed straight back in.

## 18. Foreman's settings pages

**Settings** in the menu opens Foreman's own pages, as tabs: Account,
General, Restart schedule, Notifications, Public mods, API tokens, Users
and Audit log. The restart schedule, notifications, public mods and API
tokens are covered in sections 14, 12 and 6. This section covers General,
Users and Audit log.

### General

The General tab holds the settings that have no environment variable. Each
one says "Set here"; nothing in the environment sets or overrides them.

| Setting | Range | Default |
|---|---|---|
| Automatic snapshots to keep per world | 1 to 100 | 10 |
| Default stop countdown (seconds) | 0 to 600 | 60 |
| Keep audit entries for (days) | 7 to 3650 | 365 |
| Largest save upload (MB) | 100 to 8192 | 1024 (1 GB) |
| Largest mod upload (MB) | 10 to 2048 | 200 |

- **Automatic snapshots to keep.** Each world keeps this many of its newest
  automatic snapshots. Manual snapshots are not removed by it. Lowering it
  deletes nothing when you save. Each world drops its extra automatic
  snapshots at its next automatic snapshot. If the new value would remove
  any, Foreman first asks "Keep fewer automatic snapshots?" and says how
  many snapshots, in how many worlds, will go. Choose **Save settings** on
  that page to go ahead, or the link below it to keep the current settings.
- **Default stop countdown.** The warning a stop or restart gives players
  when it is not given a countdown of its own. 0 means no warning. A
  scheduled restart keeps its own fixed 60-second countdown (section 14).
- **Audit retention.** See "Audit log" below.
- **Upload limits.** The largest file Foreman accepts when someone uploads
  a save or a mod. "MB" means 1,048,576 bytes, as the upload pages say it.
  A change applies to the next upload. The mod limit also caps a mod
  downloaded from the mod portal, so a mod you can upload can also be
  downloaded again. The limits on `mod-settings.dat`, favorite files and
  unpacked sizes stay fixed. A reverse proxy in front of Foreman may have
  its own, smaller body limit; raise that too, or large uploads still fail.
- A value outside its range refuses the whole form and saves nothing. Each
  changed setting is written to the audit log as `snapshot-retention-set`,
  `stop-countdown-set`, `audit-retention-set`, `save-upload-limit-set` or
  `mod-upload-limit-set`, with the old and new value.
- An admin API token (section 6) can read these with `foreman_settings` and
  change them with `set_foreman_settings`. It works in two steps: the first
  call changes nothing and returns each change from old to new, with how
  many snapshots and audit entries the change will remove; the second call,
  with the confirm code, applies it. Arguments left out keep their value.

### Users

The Users tab lists every user with how they sign in (password, proxy or
single sign-on) and the date they were created. Every user is an admin.

- **Delete a user.** Choose **Delete** beside another user. A confirm page
  names the user and says how many API tokens go with them. Their sessions
  end, so their next request is signed out, and their API tokens stop
  working. Their old audit log entries keep their name. Foreman writes
  `user-delete` to the audit log before it deletes, and refuses to delete
  if it cannot. You cannot delete yourself, so someone who can sign in is
  always left.
- **Proxy and single sign-on users come back.** If you delete a user who
  signs in through the proxy or the identity provider, Foreman creates them
  again at their next request while the proxy or provider still vouches for
  them. Foreman keeps no block list. To keep such a person out, remove them
  at the proxy or the provider (or take them out of the required group),
  not only here.
- **Change your password.** The tab shows a form for your own password, and
  only if you sign in with one: the current password, the new one twice, at
  least 12 characters. A wrong current password counts against the same
  sign-in limit as a wrong password at sign-in. The change signs you out
  everywhere else, keeps this browser signed in, and is written to the
  audit log as `password-change`. Proxy and single sign-on users have no
  password and see no form: a password would let a proxy user get around
  the proxy.
- The web pages cannot reset another user's password.

#### Reset a forgotten password

Run this on the host. It reads the new password from standard input, so put
the password in a file and feed the file in:

```sh
docker exec -i <container> foreman reset-password <username> < new-password.txt
```

`<container>` is Foreman's container name from `docker ps`.

- Create `new-password.txt` with your editor, with the password alone on one
  line, and make it readable only by you (`chmod 600 new-password.txt`).
  Delete the file afterwards.
- Why standard input from a file: a password written on the command line
  shows in the host's process list and in your shell history, and one typed
  at the terminal appears on the screen as you type it. A file shows
  neither. Foreman never prints the password.
- The password needs at least 12 characters. The command reads the first
  line only.
- The command ends all of that user's sessions, so they sign in again with
  the new password, and writes `password-change` to the audit log as done
  by `system`.
- Only a user who signs in with a password can be reset. A proxy or single
  sign-on user is refused, because their sign-in is managed elsewhere.

### Audit log

The Audit log tab lists what was done, newest first, 50 entries a page.
Each entry shows the time (in your time zone), the user, the name of the
API token as a badge when an assistant did it, the action in words, the
world and a detail. The log never holds a secret.

- Use **User**, **Action** and **World**, then choose **Filter**, to narrow
  the list. The menus list what the log holds. **Clear filters** shows
  everything again. **Older entries** at the foot gives the next page and
  keeps your filters. An entry for a deleted world shows the world's id and
  says it is deleted.
- Foreman keeps entries for 365 days by default. Set the number of days on
  the General tab (7 to 3650). Once an hour Foreman deletes entries older
  than that. The deletion itself is not written to the log, but a change to
  the setting is. Lowering the number removes the older entries for good at
  the next hourly deletion.

## 19. Archive a world

Archive a world you no longer play but do not want to delete. Open the
world's page (**Worlds**, then the world) and choose **Archive world**. It
takes one click.

- Only a mark on the world changes. Its files and snapshots stay as they
  are, so Foreman takes no snapshot for it.
- An archived world leaves the main Worlds list. It shows in a closed
  **Archived** foldout at the foot of that page, and the pickers that
  switch worlds leave it out.
- It cannot be started, restarted or switched to, from the web pages, from
  MCP or from the restart schedule.
- The active world cannot be archived. Switch to another world first.
- To bring it back, open its page and choose **Unarchive world**.
- Restoring a snapshot keeps the world's current archive state, whatever
  the snapshot recorded.
- Archiving and unarchiving are written to the audit log as `world-archive`
  and `world-unarchive`. An admin API token can do both (`archive_world`
  and `unarchive_world`, one call each), and `list_worlds` shows which
  worlds are archived.
