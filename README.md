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

<img src="docs/screenshots/server.webp" width="900" alt="The Server page in dark mode with Space Age main running on Factorio 2.1.24: the join address, uptime, snapshot, restart and stop buttons, CPU, memory and play time.">

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
- **Move a world to another Foreman.** Export a world as one world file,
  with its saves, mods and settings, and import it on another Foreman.
  The game password, the RCON password and your factorio.com login stay
  behind.
- **Players of a save and play time.** A world lists every player who has
  joined its save, with each one's play time and when Foreman last saw
  them, and shows how long the map has been played.

<img src="docs/screenshots/worlds.webp" width="900" alt="The Worlds page in dark mode: six worlds on game versions 1.1.110 to 2.1.25, with Space Age on or off, when each was last played, disk use, a Switch button on each, and an unfinished world with a Continue button.">

<img src="docs/screenshots/snapshots.webp" width="900" alt="A world's snapshots: two taken by hand, before rebuilding the main bus and before the Gleba expedition, and three that Foreman took automatically before world switches and a mod change.">

<img src="docs/screenshots/version-update.webp" width="900" alt="Updating Friends co-op from game version 2.1.23 to 2.1.25: the confirmation says Foreman downloads the game image and takes a Before update snapshot first.">

### Generating worlds, with a real map preview

Generate a world in four short steps: the world, its mods, the mods'
startup settings, then the map settings. Only the first step is needed,
and an unfinished world waits on the Worlds page until you continue it.
Pick a preset or your own map settings, and see the map before you
commit to it. The preview is drawn by the real game, not an imitation, so
what you see is the map you get.

- **Resource patches are labelled with their amounts**, such as "1.2M",
  on the map, with the same list as text for keyboard and screen-reader
  users.
  Each planet gets its own count. Fluids that come from tiles, such as a
  modded planet's methane, are counted as spots, sized in tiles.
- **Space Age planets.** On Vulcanus, the preview draws the demolisher
  territories, so you can see where the big worms live before you land.
- **Planet mods.** When a world's mods add planets, the map settings list
  them and the preview draws them with the world's own mods loaded. On a
  2.1 world you can also draw a planet from a throwaway real game, which
  shows terrain a mod makes by script.
- **A star map.** With all planets shown, the preview ends with a map of
  the solar system: the planets, moons and routes, placed as the game
  places them, including a mod's own planets.
- **A large view** to zoom and pan any picture, with a download of each.

<img src="docs/screenshots/generate-nauvis.webp" width="900" alt="The large map preview of Nauvis, drawn by the real game, with each resource patch labelled with its amount, such as 5.2M or 685.1K, beside the planet's resource settings.">

<img src="docs/screenshots/generate-vulcanus.webp" width="900" alt="The map preview of Vulcanus with demolisher territories outlined and marked Small or Medium, ten small and eleven medium, beside the resource amounts.">

<table>
  <tr>
    <td><img src="docs/screenshots/generate-gleba.webp" width="440" alt="The map preview of Gleba with resource amounts, beside its stone, cliff, water and plant settings."></td>
    <td><img src="docs/screenshots/generate-fulgora.webp" width="440" alt="The map preview of Fulgora: scrap patches on its islands labelled with amounts from 1.6K to 38.5M, beside the scrap and island settings."></td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/generate-aquilo.webp" width="440" alt="The map preview of Aquilo, with crude oil and lithium brine yields shown as percentages and other patches as amounts."></td>
    <td><img src="docs/screenshots/generate-maraxsis.webp" width="440" alt="The map preview of Maraxsis, a planet from a mod, with its coral reef patches labelled with amounts."></td>
  </tr>
  <tr>
    <td><img src="docs/screenshots/generate-moshine.webp" width="440" alt="The map preview of Moshine, a planet from a mod, crowded with labelled patches, with its own default richness of 15000% shown in the settings."></td>
    <td><img src="docs/screenshots/generate-vesta.webp" width="440" alt="The map preview of Vesta, a gas planet from a mod, where each spot of gas tiles is labelled with its size in tiles, such as 146 tiles."></td>
  </tr>
</table>

<img src="docs/screenshots/starmap.webp" width="600" alt="The star map of a world with planet mods: the sun, the five Space Age planets, and Maraxsis, Moshine, Muluna, Cerys, Corrundum and Vesta, with the routes between them.">

<img src="docs/screenshots/large-view.webp" width="900" alt="The large view zoomed in on Cerys, a moon from a mod, with every resource patch labelled with its amount beside the moon's resource settings.">

### Mods

- **Mods from the mod portal**, searched from the world's Mods tab. Only
  mods that fit the world's game version are listed.
- **Dependency resolution.** Foreman works out every mod your picks
  need, shows one plan of what it will install, and takes a snapshot
  before it changes anything. Mod settings can be changed on the same
  tab.
- **Mod text as the game shows it.** Mod descriptions and settings show
  the game's bold text and colours, and the mod's own icons for the items
  and fluids they name. Where a mod has no icon, Foreman can show the
  Factorio wiki's icon instead. That is off until you turn it on in the
  settings, because each viewer's browser then contacts the wiki.
- **Favorites.** Save a world's mods, mod settings, map settings and
  server settings as a favorite, then generate new worlds from it with
  the same mod bundle in a few clicks.
- **A public mods page for players.** Turn it on, and players can see and
  download the running world's mods without signing in. Nothing else is
  shown there.

<img src="docs/screenshots/mods.webp" width="900" alt="A world's Mods page: eight mods from the mod portal installed, including Rate Calculator, Factory Planner, Bottleneck Lite and Todo List, all up to date.">

<img src="docs/screenshots/mod-search.webp" width="900" alt="Searching the mod portal for train from a world's Mods page, with Project Cybersyn picked into the plan beside the results.">

<img src="docs/screenshots/favorites.webp" width="900" alt="The Favorites page with two saved setups, Planet pack and Space Age QoL pack, the second opened to show its eight mods.">

<img src="docs/screenshots/favorites-apply.webp" width="900" alt="Generating a new world from a favorite: the review lists the eight mods Foreman will download from the portal and the snapshot it takes first.">

<img src="docs/screenshots/mod-settings.webp" width="900" alt="Editing a world's mod settings: Maraxsis's checkboxes and water opacity, then Muluna's settings, with the mods' coloured text and an inline planet icon.">

### Factory map

A world's Factory map tab draws each surface of its newest save as the
game's own map, with optional layers that outline belts, production,
power, rail and other kinds of building. Zoom, pan and download the
picture. Foreman draws it from a copy of the save, so the game is never
touched. It draws when you click, or by itself once the world has been
stopped for a while, if you set it to. It needs Factorio 2.0.61 or newer.

### Running the server

- **Start, stop and restart** with a countdown that warns players in the
  game's chat. After a crash the game is restarted, and after three
  crashes in ten minutes Foreman stops it and says so.
- **Live operations**: the players online and everyone who has played the
  save, kicking and banning players, the game's chat and console, and the
  game's log as it is written.
- **A daily restart schedule** that waits until nobody is online.
- **Webhooks** to Discord, ntfy, Pushover or any web address, for starts,
  stops, crashes, players joining and leaving, and more.
- **A low-disk warning** before the game's autosaves are at risk.
- **An optional VPN sidecar**, so players can join through Tailscale or
  another VPN while LAN players still join the host.

<img src="docs/screenshots/console.webp" width="900" alt="The Console page: the game's live log with a chat message from the server, the command box, and a history of the /version, /time, /evolution and /players commands with the game's replies.">

### AI assistants through MCP

Foreman has an MCP (Model Context Protocol) endpoint, so an AI assistant
such as Claude Code can read the server's state and run it for you. Each
assistant gets its own token with a scope: **Read** only looks,
**Operate** can also start, stop and switch, and **Admin** can change
worlds, mods and settings. Actions that delete or overwrite something
show a preview and need a second, confirming call. Everything a token
does is in the audit log under its name. The same tokens also work on a
REST API, so a script can call the same tools with `curl`.

<img src="docs/screenshots/mcp.webp" width="900" alt="The API tokens settings page: MCP and REST API access switched on, and three tokens, Claude Code, Discord bot and Home Assistant, with their scopes and expiry dates.">

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

<img src="docs/screenshots/signin.webp" width="600" alt="The sign-in page in light mode, offering single sign-on above the username and password form.">

<img src="docs/screenshots/i18n.webp" width="900" alt="The Server page in German.">

<img src="docs/screenshots/audit-log.webp" width="900" alt="The audit log filtered to one world: a snapshot taken by the Claude Code API token, console commands, a chat message, a manual snapshot, a world switch and server starts and stops, each with who did it and when.">

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
