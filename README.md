# Foreman

[![ci](https://github.com/toddawhittaker/foreman/actions/workflows/ci.yml/badge.svg)](https://github.com/toddawhittaker/foreman/actions/workflows/ci.yml)

Foreman (a working name) is a small, self-hosted web app that runs
Factorio servers through Docker. It manages **worlds**: a world is a map
with its own mods, settings and pinned game version. You switch between
worlds with one click, and every risky change takes a snapshot first so
a save is never lost.

## Status

Early development; no image is published yet, so you build it yourself
(`make image`). Built so far: sign-in, worlds and snapshots, server
control, switching worlds, and adopted mode. Mods, generating worlds and
game versions come in later epics. See [docs/STATUS.md](docs/STATUS.md)
for what each epic has delivered, and [docs/SPEC.md](docs/SPEC.md),
section 18, for the epic order.

## Running Foreman beside a game you already run (adopted mode)

In adopted mode Foreman drives a `factoriotools/factorio` container you
already have, and switches worlds by changing what that container's
`/factorio` folder presents (SPEC 4.7 to 4.12, ADR 0004). This section is
enough to try it; the full operator guide comes later.

### What Foreman needs

- **The game folder mounted into both containers.** The game sees it at
  `/factorio`; Foreman sees the same host folder at `FOREMAN_GAME_DIR`.
  Foreman's data directory is a separate folder, never inside the game
  folder or holding it.
- **The same user and group.** Set the game's `PUID` and `PGID` to
  `FOREMAN_GAME_UID` and `FOREMAN_GAME_GID` (845 by default, which is also
  the user Foreman's image runs as). The image changes the owner of the
  whole game folder to `PUID:PGID` at each start.
- **`cap_drop: [NET_RAW]` on the game.** Without it a game could claim
  another address on its network, and Foreman refuses the container.
- **A game network holding only Foreman and the game.** Foreman reaches
  RCON there, so do not publish the RCON port. Foreman checks this network
  all the time and shows a warning if anything else joins it.
- **The Docker API through a socket proxy** on a network of its own that
  the game is not on, rather than mounting `/var/run/docker.sock` into
  Foreman. Docker access is root on the host (SPEC 14.2).
- **The reverse proxy on another network**, never on the game network.
- Leave the game's `SAVES`, `MODS`, `CONFIG`, `LOAD_LATEST_SAVE`,
  `GENERATE_NEW_SAVE` and `UPDATE_MODS_ON_START` at the image's defaults.
  Foreman refuses a container that changes them, a privileged one, and one
  that mounts the Docker socket, and the Server page names the setting to
  fix. SPEC 4.11 has the full list.

### Example compose file

Build Foreman first with `make image` (it is tagged `foreman:dev`), and make
`./game` and `./data` owned by 845:845.

```yaml
services:
  game:
    image: factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28
    container_name: factorio
    restart: unless-stopped
    environment:
      PUID: "845"            # = FOREMAN_GAME_UID
      PGID: "845"            # = FOREMAN_GAME_GID
      DLC_SPACE_AGE: "false" # must match each world's Space Age flag
    volumes:
      - ./game:/factorio
    ports:
      - "34197:34197/udp"    # the game port only; never RCON
    cap_drop: [NET_RAW]
    networks: [game]

  foreman:
    image: foreman:dev
    restart: unless-stopped
    environment:
      FOREMAN_MODE: adopted
      FOREMAN_GAME_CONTAINER: factorio
      FOREMAN_GAME_DIR: /game
      FOREMAN_DATA_DIR: /data
      FOREMAN_GAME_NETWORK: foreman-game
      DOCKER_HOST: tcp://socket-proxy:2375
      FOREMAN_PUBLIC_URL: https://factorio.example.com
      FOREMAN_AUTH_METHODS: local,headers
      FOREMAN_AUTH_USER_HEADER: X-Authentik-Username
      FOREMAN_AUTH_GROUPS_HEADER: X-Authentik-Groups
      FOREMAN_AUTH_REQUIRED_GROUP: factorio-admins
      FOREMAN_TRUSTED_PROXIES: 172.30.0.10/32   # the proxy's fixed address below
    volumes:
      - ./game:/game
      - ./data:/data
    networks: [game, docker, front]

  socket-proxy:
    image: ghcr.io/tecnativa/docker-socket-proxy:v0.5.0@sha256:1f5038b54f06c3e18422902cf00ba21803d1c97805aae032e5e6673d532d3459
    restart: unless-stopped
    environment:
      CONTAINERS: "1"
      POST: "1"
      EVENTS: "0"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks: [docker]

  # Your reverse proxy (Caddy, Traefik, nginx ...) joins only "front", with
  # the fixed address named in FOREMAN_TRUSTED_PROXIES, and forwards to
  # http://foreman:8080.

networks:
  game:
    name: foreman-game
  docker:
    name: foreman-docker
  front:
    name: foreman-front
    ipam:
      config:
        - subnet: 172.30.0.0/24
```

Notes on the example:

- The header names and group are examples; use what your forward-auth
  proxy sends. With `local` only, leave out the four `FOREMAN_AUTH_*`
  header settings and `FOREMAN_TRUSTED_PROXIES`.
- Do not set `hostname:` on Foreman; it finds its own container by the
  default hostname.
- The socket proxy has no way to allow start and stop without also
  allowing container create, so it narrows the Docker API (no images,
  networks, volumes or exec) but is not a full boundary. Never publish its
  port.
- One adopted container serves either Space Age worlds or worlds without
  it, set by `DLC_SPACE_AGE`. Foreman refuses to start a world that
  disagrees.

### Reaching Foreman through the proxy

Trusted headers are honoured only from `FOREMAN_TRUSTED_PROXIES`, and never
from a game network's address range (SPEC 12.5, 12.6).

- Best: run the proxy in a container on a network Foreman shares and the
  game does not (`front` above), and do not publish Foreman's port at all.
- If the proxy runs on the host instead, publish Foreman's port on
  `127.0.0.1` only (`"127.0.0.1:8080:8080"`), never `0.0.0.0` or a LAN
  address. `FOREMAN_TRUSTED_PROXIES` is then the single address the
  proxy's connections arrive from, as a `/32`: with Docker's port
  forwarder that is the gateway of the network carrying the published port
  (for example `172.30.0.1/32`), never a whole bridge range. That network
  must not be one the game is on.

### Adopting, importing and switching

1. Start the stack and open Foreman. With local sign-in on, the first visit
   creates the admin.
2. If the game folder still has real `saves`, `mods` and `config` folders,
   the Server page offers **Adopt this game folder**. Give the world a name,
   check the game version and Space Age flag, and confirm. Foreman stops
   the game, takes a "Before adoption" snapshot, moves the originals into
   `pre-foreman-<UTC time>/` inside the game folder (nothing is deleted),
   links the folder to the new world, and starts the game again if it was
   running. An empty game folder needs no adoption: import a world, then
   Start.
3. To import other folders, such as hand-renamed `saves-2025` folders, copy
   each into `<data>/import/<name>/` (here `./data/import/<name>/`), with
   the save zip at the top level and the mods in `mods/`
   (`./data/import/family-2025/family.zip`,
   `./data/import/family-2025/mods/...`). Then choose **Import a world** on
   the Worlds page, pick the folder, and choose **Import folder**. Import
   copies; the source is never changed (SPEC 5.5).
4. To switch, open a world that is not active and choose **Switch to this
   world**. The confirmation says which world stops and that both get a
   "Before switch" snapshot. The game is stopped gracefully, the folder is
   switched in one step, and the game starts again on the new world if it
   was running. To go back, switch to the first world the same way.

## Documentation

- [docs/VISION.md](docs/VISION.md): what Foreman is for, its principles
  and its non-goals.
- [docs/SPEC.md](docs/SPEC.md): the numbered requirements.
- [docs/STACK.md](docs/STACK.md): the technology choices and why.
- [docs/DESIGN.md](docs/DESIGN.md): the user interface design rules.
- [docs/WORKFLOW.md](docs/WORKFLOW.md): branches, pull requests, checks
  and local development.
- [docs/adr/](docs/adr/): decision records.

## Development

You need Go 1.27.3 (exactly; see WORKFLOW.md, "Local development"),
Docker and `make`. The linter and the browser tests run in pinned
containers, so you do not install them.

```sh
make verify   # format, vet, lint, unit tests with coverage, e2e type check
make run      # serve locally on http://localhost:8080
make e2e      # browser and accessibility tests
make image    # build the container image as foreman:dev
```

CI runs `make ci` on every pull request and on every push to `main`.

## License

[MIT](LICENSE).
