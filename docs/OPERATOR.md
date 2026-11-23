# Operator guide

This guide takes you from nothing to a running Factorio server that
players can join. It covers the new-user path: install Foreman, create the
first admin, generate a Space Age world, install mods from the mod portal,
start the server and join it. It also covers the security advice every
operator needs, running behind a reverse proxy, importing, moving and switching
worlds, webhook notifications, the low-disk warning, the restart
schedule, what to do when something fails, and, optionally, letting
players join through a VPN sidecar such as Tailscale. It also covers
signing in with OpenID Connect (section 17), Foreman's own settings, users
and audit log (section 18) and archiving worlds (section 19). Sections 20
to 22 are for running Foreman over time: upgrading it, backing up and
restoring, and every environment variable in one table. Logs and the
health check are in section 15.

## 1. What you need

- **A Linux host with Docker** and Docker Compose. Foreman runs in a
  container and creates the game's container itself.
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

Foreman's image is published to GitHub's container registry as
`ghcr.io/toddawhittaker/foreman`. Each release has an exact version tag,
such as `1.0.0`; there is no `latest` tag, so you always know which
version you run and nothing changes under you. Use the newest release
listed on the [releases page](https://github.com/toddawhittaker/foreman/releases).
This guide writes it as `1.0.0`.

```sh
docker pull ghcr.io/toddawhittaker/foreman:1.0.0
```

Until the first release is published, building from source (below) is
the only way to get the image.

**Optional: check where the image came from.** Each published image has
a GitHub artifact attestation, a signed record that it was built by this
repository's release workflow. With the GitHub command-line tool, `gh`:

```sh
gh attestation verify oci://ghcr.io/toddawhittaker/foreman:1.0.0 --owner toddawhittaker
```

It succeeds only for an image the release workflow built.

**Alternative: build from source.** You need git and make as well as
Docker:

```sh
git clone https://github.com/toddawhittaker/foreman.git
cd foreman
make image
```

The image is tagged `foreman:dev`; use that name in the compose file
(section 5) instead of the published one. Use `make image` rather than a
bare `docker build`: the Makefile passes the exact Go build image the
project pins. The build takes a few minutes the first time.

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
    # The newest release from the releases page (section 2), or
    # foreman:dev if you built the image yourself.
    image: ghcr.io/toddawhittaker/foreman:1.0.0
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
- Set `FOREMAN_PUBLIC_URL` (section 6). Foreman refuses any `/mcp` or `/api/`
  request that carries an `Origin` header unless it equals that address.
  Real MCP clients and scripts send none. Without `FOREMAN_PUBLIC_URL`, any `Origin` is refused.
- If a proxy in front of Foreman asks people to sign in, it will also block
  the assistant, which cannot sign in that way, and any script that uses the
  REST API (next section). Add one rule to the proxy: requests whose path
  starts with `/mcp` or `/api/` go to Foreman without the proxy's sign-in.
  Foreman checks the token itself and ignores any sign-in header on these
  paths. Every other path stays behind the proxy. Or point the assistant or
  script at Foreman directly on a network you trust.
- Test it from a machine that is not signed in. A call with the token
  should work. A call with no token should get 401.

### Use the REST API

The same tokens also work on a REST API, for scripts and programs that do
not speak MCP (SPEC section 23). It offers the same tools as MCP, with the
same names, arguments, results and scopes, and it runs the same code, so
the snapshots, the two-step confirm and the audit log work the same way.
It is off until an admin turns it on.

1. Open **Settings**, **API tokens**, and turn REST API access on. It has
   its own switch, apart from MCP access: each can be on while the other
   is off. To keep it off whatever the page says, set `FOREMAN_API: off`
   in the compose file. `off` is the only value Foreman accepts; anything
   else stops it at startup. The page then says the environment turned
   REST API access off.
2. Use a token from the same page. The page that shows a new secret also
   shows the REST API's base address, `/api/v1`, after your Foreman's
   address.
3. Send the token in an `Authorization: Bearer` header, and send a body
   as `application/json`. A token in the address (the query string) or in
   a cookie is ignored. In these examples `$FOREMAN_TOKEN` holds the token
   and `https://foreman.example` stands for your Foreman.

List the tools your token may use, each with its description, whether it
only reads or works in two steps, and the schema of its arguments and its
result:

```sh
curl -H "Authorization: Bearer $FOREMAN_TOKEN" \
  https://foreman.example/api/v1/tools
```

Call a tool with `POST /api/v1/tools/<name>`. The body is a JSON object
with the tool's arguments. A tool that needs none takes `{}`, or an empty
body:

```sh
curl -X POST -H "Authorization: Bearer $FOREMAN_TOKEN" \
  -H "Content-Type: application/json" -d '{}' \
  https://foreman.example/api/v1/tools/server_status
```

An action names the world it acts on. This one needs an **Operate** or
**Admin** token; take the world's id from `list_worlds`:

```sh
curl -X POST -H "Authorization: Bearer $FOREMAN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"world_id": "<world id>", "label": "Before the update"}' \
  https://foreman.example/api/v1/tools/take_snapshot
```

An action that deletes or overwrites something works in two steps, as in
"Admin tokens" above. The first call changes nothing. It answers
`done: false`, a preview and a `confirm_code`:

```sh
curl -X POST -H "Authorization: Bearer $FOREMAN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"world_id": "<world id>"}' \
  https://foreman.example/api/v1/tools/force_stop
```

Read the preview. To go ahead, send the second call within 5 minutes:
the same arguments plus the code. A code works once, and only for that
token, tool and arguments. A code from `/mcp` works here too, and the
other way round.

```sh
curl -X POST -H "Authorization: Bearer $FOREMAN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"world_id": "<world id>", "confirm_code": "<code from the first call>"}' \
  https://foreman.example/api/v1/tools/force_stop
```

What the answers mean:

- **200**: the call worked. The body has `message`, a one-line summary,
  and `result`, the tool's result exactly as MCP gives it. A two-step
  tool's first call is also a 200, with `done: false` in its result.
- **422**: the tool refused the call. The body has `error`, in English,
  and `result` when the tool gave one. If something changed since a
  preview, `result` holds a fresh preview and code.
- **404**: no such tool for this token. A tool outside the token's scope
  looks the same as one that does not exist.
- **400**: the body is not a JSON object. **415**: the content type is
  not `application/json`. **405**: any method other than `GET` on the
  tool list or `POST` on a tool.
- **401, 403, 413 and 429** come before any tool runs and are plain
  text, as on `/mcp`. 401 is a missing, unknown, expired or revoked token
  (an expired or revoked one says so). 403 means REST API access is off.
  413 means a body over 4 MiB. 429 means a limit was hit
  and comes with `Retry-After`.
- **500**: Foreman failed in a way it cannot word; the body has `error`,
  and the reason is in Foreman's log.

Limits are the same as for MCP, and `/mcp` and `/api/` count together:
a token may make 120 requests a minute with at most 4 running at once, and
failed token checks are limited per address. A token cannot double its
share by using both.

`GET /api/v1/openapi.json` serves an OpenAPI 3.1 description of the API,
for tools that build clients from one. It needs a token, like the tool
list, and describes only the tools that token's scope allows.

Within `/api/v1/`, Foreman may add tools, optional arguments and result
fields. It will not remove or rename them, or change what one means,
without a new version path.

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
6. Choose **Generate world**. This makes the world and opens step 2, its
   Mods tab.
7. Pick the world's mods there (see the next section), or choose **Skip to
   map settings**.
8. On step 3, **Map settings**, leave the default preset or change it.
   **Preview map** draws the map first, once the game image is on the
   host. Choose **Save map settings**.

Generating is three steps, each its own page: the world, its mods, then its
map settings. Only the first is needed. The world exists after step 1, so
if you stop there, it shows as **Unfinished** on the Worlds list. Choose
**Continue** to pick up where you left off. Foreman never deletes an
unfinished world for you.

The first world you make becomes the active one: the world the Server page
starts. Its map is made when it first starts, from the settings you saved
in step 3. You can change them until then. Once the world has a save, or
while its server is running or starting, they are locked.

### Planet mods

If the world's mods add planets, step 3 lists them with the controls of
their resources, and the preview draws them. Foreman reads the planet list
from a one-off run of the world's own game, the same run that reads the mod
settings. It can take up to a minute the first time. If the list cannot be
read, step 3 says so, shows the game's own planets and refuses to save, so
a save never drops a modded planet's settings.

If you remove a planet mod after saving step 3, the world will not start
until you save step 3 again. The game would fail to make the map, so
Foreman refuses the start and links you to step 3.

On a 2.1 world, **Draw from a real game** next to a planet's picture makes a
throwaway map with the world's mods and charts 1024 by 1024 tiles around
the start. It shows terrain that a mod makes by script, which the fast
preview cannot. It takes 15 to 40 seconds.

### Memory for planet previews

Each preview run, and each real-game picture, is a one-off container. Both
run one at a time and are limited to 1 GiB of memory with no swap. A fast
preview of one planet is limited to 60 seconds. With five planet mods I
measured 2 to 4 seconds and under 750 MiB for each planet. A real-game
picture is limited to 90 seconds, and I measured 13 to 39 seconds and at
most 692 MiB. Foreman keeps the pictures in memory, up to 160 MiB for all
users, until you save the map settings or restart Foreman.

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

### Move a world to another Foreman

A world file moves a whole world in one ZIP: its saves, mods, settings and
which save loads.

1. On the old Foreman, open the world's page and choose **Export world**.
   Pick **All saves** or **Only the save that loads next**. A running world
   exports the last save on disk.
2. On the new Foreman, choose **Worlds**, then **Import a world**, and drop
   the file on **Import a world file**. Check the preview, change the name
   if you like, and choose **Import world**.

The game password, your factorio.com login and the RCON password are not
in the file. Set a game password again in **Game settings**. A mod may keep
a token in `mod-settings.dat` or in a save, and that does travel, so treat
the file like a save. The audit log, restart schedule, notifications and
saved mod picks stay on the old Foreman. A world file always makes a new
world; it never replaces one.

### Memory for reading a large save

To list the players of a save and draw its factory map, Foreman runs a
copy of the save in a one-off game container for a few seconds. Its peak
memory is about as much as running that save: I measured 6.9 GB for a
187 MB save with 44 mods. Foreman does this by itself only when no game is
running. Beside a running game it does it only when you click, and the
button shows how much memory the running game uses now. If the host has
too little memory, the read fails and nothing else is harmed.

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
- **Pushover**: no address, just two keys; Foreman always posts to
  `https://api.pushover.net/1/messages.json`. Sign in at pushover.net:
  your **user key** is at the top of your dashboard (a group key works
  too). For the **application token**, choose "Create an Application/API
  Token" at the bottom of the dashboard, name it Foreman, and copy the API
  token the new application's page shows. Crash, crash loop and low disk
  are sent at high priority (1), everything else at normal priority (0);
  Foreman never uses emergency priority. Messages longer than Pushover's
  1,024 characters are cut. Both keys are kept like an address: never
  shown again or logged. When you edit the webhook, leave a key field
  empty to keep that key.
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
  never holds a password or token. Foreman writes it to standard output as
  JSON (one object per line, with the time, level and message), so any log
  collector that reads Docker's logs can keep it. Docker keeps it only as
  long as the container exists; set up log rotation or a collector if you
  need history.
- **The game's log.** Choose **Console** in the menu: it streams the
  game's log as the game prints it, with the RCON password hidden.
  **Download log** saves it. The game's container is named
  `foreman-<world-id>`, so `docker logs foreman-<world-id>` also shows it,
  but without that filtering: the raw log can hold the game password, so
  do not paste it anywhere public.
- **The health check.** The image has a Docker health check: every 30
  seconds it asks Foreman's `/healthz` page, which answers `ok` while
  Foreman can serve requests. `docker compose ps` shows the result as
  healthy or unhealthy. It checks only Foreman itself, not the game; the
  Server page shows the game's state.
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
| Largest mod upload (MB) | 10 to 2048 | 1024 |
| Check GitHub for new Foreman versions | on or off | on |

- **Check GitHub for new Foreman versions.** About a minute after it
  starts, and then every 6 hours, Foreman asks `api.github.com` for its
  latest release. When that is newer than the running version, About says
  so and links to the release notes. The request sends only "Foreman" and
  its version; nothing about you, your users or your worlds. Builds without
  a release version never ask. To stop it, untick this box and choose
  **Save settings**: Foreman then makes no request to GitHub. If your
  network blocks GitHub, the check fails quietly with one warning line in
  the log.
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

The Audit log tab lists what was done, newest first, in a table. It shows
50 entries a page by default; **Entries per page** offers 25, 50, 100 or
200. Each entry shows the time (in your time zone), the user, the name of the
API token as a badge when an assistant did it, the action in words, the
world and a detail. The log never holds a secret.

- Use **User**, **Action** and **World**, then choose **Filter**, to narrow
  the list. The menus list what the log holds. **Clear filters** shows
  everything again. **Older entries** and **Newer entries** at the foot move
  between pages and keep your filters and the page size. An entry for a deleted world shows the world's id and
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

## 20. Upgrade Foreman

Take a backup first (section 21). Then change the version in the `image:`
line of the compose file to the new release, and run:

```sh
docker compose pull
docker compose up -d
```

Read the release notes before you upgrade; the About page links to them
when a newer release is out (section 18).

- **The game keeps running.** Upgrading Foreman never stops or touches a
  running game. The new Foreman finds the game container again when it
  starts.
- **The database is migrated.** At start, Foreman brings its database up
  to date by applying each change the new version needs, one at a time.
  Each change is all or nothing, so a failure leaves the database at the
  last good step and Foreman does not start; the log says why.
- **World files.** If a new version changes how the data directory is
  laid out, it takes a snapshot of every world it affects before it
  changes any file (SPEC 3.8).
- **Downgrading is not supported once the database has changed.** An
  older Foreman refuses to start with a database a newer one has migrated,
  and its log says the database is at a newer version. To go back, stop
  Foreman, put back `foreman.db` from the backup you took before the
  upgrade, delete any `foreman.db-wal` and `foreman.db-shm` beside it that
  the backup does not have, and start the older image. Anything done since that backup, such
  as new users or audit entries, is lost; worlds and snapshots are not in
  the database, so they stay.

If you built the image from source, pull the new code, run `make image`
again, and run `docker compose up -d`.

## 21. Back up and restore

Everything Foreman knows is in the data directory (section 3), plus the
compose file and the secrets you gave it:

| Path | What it holds |
|---|---|
| `foreman.db` | The database: users, sessions, API tokens, settings, webhooks, the stored factorio.com account, the restart schedule, the audit log and which world is active. The SQLite database keeps recent changes in `foreman.db-wal` and `foreman.db-shm` beside it; copy those with it. |
| `worlds/` | One folder per world: its saves, mods, settings and game version. |
| `snapshots/` | Every snapshot of every world. |
| `favorites/` | The favorites made from worlds. |
| `import/` | Folders you placed for import. Foreman only reads them. |

Keep `FOREMAN_SECRET_KEY` with the backup but apart from it, for example
in a password manager. The factorio.com account and the webhook addresses
in the database are encrypted with that key, and a restored database is
no use for them without it (section 5).

**Back up.** Stop the game from the Server page, so no autosave is being
written, then stop Foreman, copy the whole directory, and start Foreman
again:

```sh
docker compose stop foreman
sudo tar -C . -czf foreman-backup.tar.gz data
docker compose start foreman
```

Copy the whole directory in one command, as above: save and mod files in
snapshots are hard links to the world's files (section 15), and `tar` or
`cp -a` keep them as links, so the copy is no larger than the original.
Copying `foreman.db` alone while Foreman runs can catch it half written.

**Restore.** Stop Foreman, move the current data directory aside, unpack
the backup, give it back to user 845, and start Foreman:

```sh
docker compose stop foreman
sudo mv data data.old
sudo tar -C . -xzf foreman-backup.tar.gz
sudo chown -R 845:845 data
docker compose start foreman
```

Use the same Foreman version as the backup, or a newer one (section 20).
Once the restored Foreman works, delete `data.old`. To put back a single
world rather than everything, restore one of its snapshots instead
(section 15).

## 22. Environment variables

Foreman reads these when it starts. A variable that is set but empty
counts as unset. If any is wrong, Foreman reports every problem at once
and does not start. The variables marked "file form" can instead name a
file that holds the value, with `_FILE` added to the name (for example
`FOREMAN_SECRET_KEY_FILE`), for Docker secrets. Setting both forms of one
variable stops Foreman at startup.

| Variable | Default | Meaning |
|---|---|---|
| `FOREMAN_DATA_DIR` | `/data` | The data directory inside the container (section 3). |
| `FOREMAN_HOST_DATA_DIR` | found from Foreman's own mounts | The same directory's path on the host. Set it only if Foreman says it cannot find it (section 3). |
| `FOREMAN_GAME_UID`, `FOREMAN_GAME_GID` | `845` | The game's user and group. Never 0 (section 3). |
| `FOREMAN_GAME_NETWORK` | none | The Docker network the game joins. Required when Foreman runs in a container or uses a VPN sidecar (section 5). |
| `FOREMAN_GAME_NETWORK_CONTAINER` | none | The VPN sidecar whose network the game shares (section 16). |
| `FOREMAN_LISTEN` | `:8080` | The address and port Foreman listens on, inside its container. |
| `FOREMAN_PUBLIC_URL` | none | The address people use to reach Foreman, such as `https://factorio.example.com`. An `https://` address makes the sign-in cookie secure. Required for OpenID Connect, and checked by MCP (sections 6 and 17). |
| `FOREMAN_PUBLIC_ADDRESS` | none | The host name or IP address players type into Factorio, with an optional port. Shown on the Server page; without it, no join address is shown (section 5). |
| `FOREMAN_AUTH_METHODS` | `local` | Which sign-in methods are on: a comma-separated list of `local`, `headers` and `oidc` (sections 6 and 17). |
| `FOREMAN_AUTH_USER_HEADER` | none | The header that names the user. Required with `headers` (section 6). |
| `FOREMAN_AUTH_GROUPS_HEADER` | none | The header that lists the user's groups. Required with `headers`. |
| `FOREMAN_AUTH_REQUIRED_GROUP` | none | The group a header user must be in. Required with `headers`. |
| `FOREMAN_TRUSTED_PROXIES` | none | The addresses whose headers Foreman trusts, such as `172.30.0.10/32`. Required with `headers`. |
| `FOREMAN_OIDC_ISSUER` | none | The identity provider's issuer address, which must start with `https://`. Required with `oidc` (section 17). |
| `FOREMAN_OIDC_CLIENT_ID` | none | The client ID the provider gave Foreman. Required with `oidc`. |
| `FOREMAN_OIDC_CLIENT_SECRET` | none | The client secret. Required with `oidc`. File form. |
| `FOREMAN_OIDC_REQUIRED_GROUP` | none | The group a single sign-on user must be in. Required with `oidc`. |
| `FOREMAN_SESSION_IDLE` | `168h` (7 days) | How long a session lasts without use, written like `12h` or `30m`. |
| `FOREMAN_SECRET_KEY` | none | At least 32 characters. Encrypts the factorio.com account and webhook addresses entered in the web pages (sections 5 and 12). File form. |
| `FOREMAN_FACTORIO_USERNAME`, `FOREMAN_FACTORIO_TOKEN` | none | The factorio.com account for mod downloads, instead of entering it in the web pages. Set both or neither. File form, each. |
| `FOREMAN_MCP` | none | `off` keeps MCP access off whatever the settings page says. Any other value stops Foreman at startup (section 6). |
| `FOREMAN_API` | none | `off` keeps REST API access off whatever the settings page says. Any other value stops Foreman at startup (section 6). |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | How Foreman reaches Docker; with the compose file, the socket proxy (sections 4 and 5). |

Foreman also passes a few `FOREMAN_*` variables to the game's container
itself, such as the world's id. Those are not settings; do not set them
on Foreman.
