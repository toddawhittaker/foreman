<img src="docs/brand/foreman-wordmark-800.webp" alt="Foreman" width="400">

# Foreman

[![ci](https://github.com/toddawhittaker/foreman/actions/workflows/ci.yml/badge.svg)](https://github.com/toddawhittaker/foreman/actions/workflows/ci.yml)
[![AI transparency](https://img.shields.io/badge/AI-transparency_notice-6f42c1)](AI-DISCLOSURE.md)

Foreman (a working name) is a small, self-hosted web app that runs
Factorio servers through Docker.

It manages **worlds**. A world is a map together with its own mods, its
own settings and its own pinned game version, so a modded Space Age run
and a vanilla 1.1 map can live side by side and never break each other.
You switch between them with one click.

Every risky change, such as switching worlds, changing mods, updating the
game or restoring an old save, takes a **snapshot** of the world first. A
snapshot is a complete copy of the world's folder, so a bad mod update or
a wrong click is undone in one click, and a save is never lost.

<img src="docs/screenshots/server.webp" width="900" alt="The Server page in dark mode with the world Space Age main running on Factorio 2.1.24: uptime, CPU and memory, the snapshot, restart and stop buttons, players online and recent snapshots.">

## Features

### Worlds, versions and snapshots

- **Worlds with pinned game versions.** Each world runs on the exact
  Factorio version it was made with, from 1.1 to 2.1. Switching to another
  world stops the game, takes a snapshot of both worlds, and starts the
  new one on its own version. Foreman downloads a game image only when
  you ask it to.
- **Updating the game, with a way back.** Updating a world's game version
  takes a snapshot first, and rolling back restores it in one step.
- **Automatic snapshots** before every risky change, plus snapshots you
  take by hand. You choose how many automatic snapshots each world keeps.
- **Import, clone and archive.** Import a world you already have from a
  folder or a save upload, clone a world to try something on a copy, and
  archive the worlds you no longer play. A deleted world can be restored
  from the Deleted worlds page.

<img src="docs/screenshots/worlds.webp" width="900" alt="The Worlds page in dark mode: seven worlds on game versions 2.0.77 to 2.1.24, with Space Age on or off, disk use, and a Switch button on each.">

<img src="docs/screenshots/snapshots.webp" width="900" alt="A world's snapshots: one taken by hand before the first rocket launch and two Foreman took automatically before mod changes.">

<img src="docs/screenshots/version-update.webp" width="900" alt="Updating a world from game version 2.1.23: the confirmation says Foreman takes a Before update snapshot first.">

### Generating worlds, with a real map preview

Generate a world from a preset or from your own map settings, and see the
map before you commit to it. The preview is drawn by the real game, not
an imitation, so what you see is the map you get.

- **Resource patches are labelled with their amounts**, such as "1.2M",
  on the map, with the same list as text for keyboard and screen-reader
  users.
- **Space Age planets.** On Vulcanus, the preview draws the demolisher
  territories, so you can see where the big worms live before you land.

<img src="docs/screenshots/generate-nauvis.webp" width="900" alt="The Generate page's map preview of Nauvis, drawn by the real game, with each resource patch labelled with its amount, such as 2.2M or 600.5K.">

<img src="docs/screenshots/generate-vulcanus.webp" width="900" alt="The map preview of Vulcanus with demolisher territories outlined and marked Small or Medium, eleven small and seven medium, beside the resource amounts.">

### Mods

- **Mods from the mod portal**, searched from the world's Mods tab. Only
  mods that fit the world's game version are listed.
- **Dependency resolution.** Foreman works out every mod your picks
  need, shows one plan of what it will install, and takes a snapshot
  before it changes anything. Mod settings can be changed on the same
  tab.
- **Favorites.** Save a world's mods, mod settings, map settings and
  server settings as a favorite, then generate new worlds from it with
  the same mod bundle in a few clicks.
- **A public mods page for players.** Turn it on, and players can see and
  download the running world's mods without signing in. Nothing else is
  shown there.

<img src="docs/screenshots/mods.webp" width="900" alt="A world's Mods page: eight mods from the mod portal installed, including Rate Calculator, Factory Planner and Bottleneck Lite, all up to date.">

<img src="docs/screenshots/mod-search.webp" width="900" alt="Searching the mod portal for rail from a world's Mods page, with the picked mod gathered in a plan beside the results.">

<img src="docs/screenshots/favorites.webp" width="900" alt="The Favorites page with two saved setups, Rail planning kit and Space Age QoL pack, the second opened to show its eight mods and map exchange string.">

<img src="docs/screenshots/favorites-apply.webp" width="900" alt="Generating a new world from a favorite: the review lists the mods Foreman will install from the portal.">

### Running the server

- **Start, stop and restart** with a countdown that warns players in the
  game's chat. After a crash the game is restarted, and after three
  crashes in ten minutes Foreman stops it and says so.
- **Live operations**: the player list, kicking and banning players, the
  game's chat and console, and the game's log as it is written.
- **A daily restart schedule** that waits until nobody is online.
- **Webhooks** to Discord, ntfy, Pushover or any web address, for starts,
  stops, crashes, players joining and leaving, and more.
- **A low-disk warning** before the game's autosaves are at risk.
- **An optional VPN sidecar**, so players can join through Tailscale or
  another VPN while LAN players still join the host.

### AI assistants through MCP

Foreman has an MCP (Model Context Protocol) endpoint, so an AI assistant
such as Claude Code can read the server's state and run it for you. Each
assistant gets its own token with a scope: **Read** only looks,
**Operate** can also start, stop and switch, and **Admin** can change
worlds, mods and settings. Actions that delete or overwrite something
show a preview and need a second, confirming call. Everything a token
does is in the audit log under its name.

<img src="docs/screenshots/mcp.webp" width="900" alt="The API tokens settings page: MCP access switched on, and a list of tokens with their scopes and expiry dates.">

### Sign-in, languages and the interface

- **Three ways to sign in**: a local username and password; trusted
  headers from a reverse proxy that signs people in for Foreman; or
  OpenID Connect (OIDC), a standard single sign-on protocol, with any
  provider that speaks it. You can turn on more than one.
- **An audit log** of who did what, with filters and a retention setting.
  Actions an AI assistant takes are listed under its token's name.
- **Eight languages**: English, German, Russian, Simplified Chinese,
  French, Spanish, Polish and Czech. The translations are made by AI, and
  corrections are welcome as GitHub issues.
- **Works on a phone**, down to 320 pixels wide, in light and dark mode.
  Pages are built and tested for keyboard and screen-reader use.

<img src="docs/screenshots/signin.webp" width="600" alt="The sign-in page in light mode, offering single sign-on through OpenID Connect above the username and password form.">

<img src="docs/screenshots/i18n.webp" width="900" alt="The Server page in German.">

<img src="docs/screenshots/audit-log.webp" width="900" alt="The audit log: console commands, a token created, MCP turned on, a game version update, a snapshot and a mod change, each with who did it and when.">

<img src="docs/screenshots/worlds-light.webp" width="900" alt="The Worlds page in light mode.">

<table>
  <tr>
    <td><img src="docs/screenshots/phone.webp" width="260" alt="The Server page on a phone, in light mode."></td>
    <td><img src="docs/screenshots/phone-dark.webp" width="260" alt="The Server page on a phone, in dark mode."></td>
  </tr>
</table>

## Quick start

You need a Linux host with Docker and Docker Compose. Foreman runs in a
container next to a Docker socket proxy (a small container that passes
on only the Docker requests Foreman needs), and creates the game's
container itself.

The image is published as `ghcr.io/toddawhittaker/foreman`, with exact
version tags only. Until the first release is published, building from
source with `make image` is the only way to get it (operator guide,
section 2).

1. Make a data directory owned by user and group 845:

   ```sh
   mkdir -p data && sudo chown 845:845 data
   ```

2. Copy the compose file from the [operator guide](docs/OPERATOR.md),
   section 5, next to `data/`, and set its image to the newest release
   from the [releases page](https://github.com/toddawhittaker/foreman/releases),
   for example `ghcr.io/toddawhittaker/foreman:1.0.0`.
3. Start it with `docker compose up -d`, open `http://<your host>:8080`,
   and create the first admin account.

Read the operator guide's security advice (section 4) before you put
Foreman anywhere but a home network you trust: access to Docker is root
on the host. The guide then covers the rest: a reverse proxy, generating
a world, installing mods, joining the game, upgrades, backups and every
setting.

## Documentation

- [docs/VISION.md](docs/VISION.md): what Foreman is for, its principles
  and its non-goals.
- [docs/OPERATOR.md](docs/OPERATOR.md): how to install and run Foreman.
- [docs/SPEC.md](docs/SPEC.md): the numbered requirements.
- [docs/STACK.md](docs/STACK.md): the technology choices and why.
- [docs/DESIGN.md](docs/DESIGN.md): the user interface design rules.
- [docs/WORKFLOW.md](docs/WORKFLOW.md): branches, pull requests, checks
  and local development.
- [docs/I18N.md](docs/I18N.md): the languages, the glossary and the rules
  for writing and translating text.
- [docs/adr/](docs/adr/): decision records.
- [AI-DISCLOSURE.md](AI-DISCLOSURE.md): where AI helps make Foreman,
  and what Foreman itself does not do.

## Development

You need Go 1.27.4 (exactly; see WORKFLOW.md, "Local development"),
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
