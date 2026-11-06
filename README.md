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
control, switching worlds and game versions. Mods and generating worlds
come in later epics. See [docs/STATUS.md](docs/STATUS.md)
for what each epic has delivered, and [docs/SPEC.md](docs/SPEC.md),
section 18, for the epic order.

The web UI is available in eight languages. The translations are made by
AI, and corrections are welcome as GitHub issues.

## Running Foreman

Foreman creates and owns the game container itself (SPEC section 4, ADR
0003, ADR 0005). You run Foreman and a Docker socket proxy; you do not
define a game service. Each world runs the `factoriotools/factorio` image
for its own exact game version. This section is enough to try it; the full
operator guide comes later.

### What Foreman needs

- **A data directory** mounted into Foreman (`FOREMAN_DATA_DIR`, `/data` by
  default), owned by 845:845. It holds the worlds, the snapshots and the
  app database. Foreman creates the game with a bind mount of one world's
  folder, so it must know the folder's path on the host: it reads it from
  its own mounts, or you set `FOREMAN_HOST_DATA_DIR` (SPEC 3.5).
- **The same user and group as the game.** `FOREMAN_GAME_UID` and
  `FOREMAN_GAME_GID` default to 845, which is also the user Foreman's image
  runs as (SPEC 3.6).
- **A game network holding only Foreman and its game**, named by
  `FOREMAN_GAME_NETWORK`. Foreman puts the game on it and reaches RCON
  there; RCON is never published. Foreman refuses to create a game when
  anything else is on that network (SPEC 4.4).
- **The Docker API through a socket proxy** on a network of its own that
  the game is not on, rather than mounting `/var/run/docker.sock` into
  Foreman. Docker access is root on the host (SPEC 14.2). Foreman calls
  only the endpoints in SPEC 14.3: ping and version; list, inspect, create,
  start, stop, kill and remove containers, and read their logs and stats;
  inspect an image and pull an exact tag. With the Tecnativa proxy that
  means `CONTAINERS=1`, `IMAGES=1` and `POST=1`; turn `EVENTS` off.
- **The reverse proxy on another network**, never on the game network.
- **The game image on the host.** Foreman never downloads at startup. When
  a world's image is missing, the Server page offers **Download game
  image**, which pulls that exact tag (SPEC 3.7).

### Example compose file

Build Foreman first with `make image` (it is tagged `foreman:dev`), and make
`./data` owned by 845:845.

```yaml
services:
  foreman:
    image: foreman:dev
    restart: unless-stopped
    environment:
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
      - ./data:/data
    # Least privilege (SPEC 14.11): Foreman writes only /data and /tmp.
    read_only: true
    tmpfs:
      - /tmp
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    # Foreman asks Docker for its own mounts when it starts, so the proxy
    # must be up first.
    depends_on: [socket-proxy]
    networks: [game, docker, front]

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
  front:
    name: foreman-front
    ipam:
      config:
        - subnet: 172.30.0.0/24
```

Pick a subnet that no existing Docker network uses, or compose fails.

Your reverse proxy (Caddy, Traefik, nginx ...) runs from its own compose
file. It joins only `foreman-front`, with the fixed address named in
`FOREMAN_TRUSTED_PROXIES`, and forwards to `http://foreman:8080`:

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

Foreman creates the game container itself, named `foreman-<world-id>`, on
`foreman-game`, and publishes the game port (UDP 34197) on the host.

Notes on the example:

- The header names and group are examples; use what your forward-auth
  proxy sends. With `local` only, leave out the four `FOREMAN_AUTH_*`
  header settings and `FOREMAN_TRUSTED_PROXIES`.
- Do not set `hostname:` on Foreman; it finds its own container by the
  default hostname.
- The socket proxy cannot inspect a container-create request, so it
  narrows the Docker API (no networks, volumes or exec) but is not a full
  boundary. Never publish its port.

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

### Importing and switching

1. Start the stack and open Foreman. With local sign-in on, the first visit
   creates the admin.
2. To import a folder, such as a hand-renamed `saves-2025` folder, copy it
   into `<data>/import/<name>/` (here `./data/import/<name>/`), with the
   save zip at the top level and the mods in `mods/`
   (`./data/import/family-2025/family.zip`,
   `./data/import/family-2025/mods/...`). The folder may instead use the
   Factorio server layout (`saves/`, `mods/`, `config/`): saves are read
   from the top level and from `saves/`, and the settings files in
   `config/` that SPEC 5.1 lists are copied too, except `rconpw`, because
   Foreman makes its own. Then choose **Import a world** on
   the Worlds page, pick the folder, give the exact game version the save
   was made with (such as `2.0.77`) and the Space Age flag, and choose
   **Import folder**. Import copies; the source is never changed (SPEC
   5.5).
3. Choose **Start** on the Server page to run the active world, or switch
   to another world (step 4). If a world's game image is not on the host
   yet, the Server page offers **Download game image** first.
4. To switch, open a world that is not active and choose **Switch to this
   world**. The confirmation says which world stops and that both get a
   "Before switch" snapshot. The game is stopped gracefully and started
   again on the new world, with that world's game version, if it was
   running. To go back, switch to the first world the same way.

### VPN sidecars (not built yet)

Running the game inside a VPN sidecar's network (Tailscale, Netbird and
similar, SPEC 4.13, ADR 0006) is planned for Epic 9 and is not built.
Foreman refuses to start when `FOREMAN_GAME_NETWORK_CONTAINER` is set, so
leave it unset.

## Documentation

- [docs/VISION.md](docs/VISION.md): what Foreman is for, its principles
  and its non-goals.
- [docs/SPEC.md](docs/SPEC.md): the numbered requirements.
- [docs/STACK.md](docs/STACK.md): the technology choices and why.
- [docs/DESIGN.md](docs/DESIGN.md): the user interface design rules.
- [docs/WORKFLOW.md](docs/WORKFLOW.md): branches, pull requests, checks
  and local development.
- [docs/I18N.md](docs/I18N.md): the languages, the glossary and the rules
  for writing and translating text.
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
