# Foreman: vision

*Working name. Drafted 2026-11-02. This is the intent; the spec comes next.*

## The problem

Self-hosted Factorio needs two things: a server that stays up, and a way for an admin
to run it without a shell. The usual tool for the second, OpenFactorioServerManager
(OFSM), fails on both counts:

- **It can only control a game it runs itself.** Most people run the
  game in the well-maintained `factoriotools/factorio` image, so OFSM ends up beside the
  server, sharing its folders. Mod installs land, but Start, Stop and Restart act on a
  process that was never running.
- **It's abandoned.** Last release March 2021, last commit July 2024. That predates
  Factorio 2.0 and Space Age.
- **It's unsafe to run.** It runs as root, downloads whatever "stable" is at every boot
  (unpinned), doesn't restart the game after a reboot, and has no snapshots before risky
  operations.

Admins also juggle several games: a long-running family base, a friend's achievement
run, last year's save. Today that means renaming folders by hand (`saves-2025`, `saves-achievement-run`,
`mods-old`, …), keeping mods and saves in step yourself.

## What Foreman is

A small, self-hosted web app that **runs Factorio servers through Docker** and makes
**worlds** the unit you manage. A world is a map with its own mods, settings and game
version. You switch worlds with one click, and nothing gets lost on the way.

It's a public product: one container, image on GHCR, permissive license (MIT or
Apache-2.0, decided at the spec). The first deployment is a homelab, but nothing in the product is specific to it.

## Principles

1. **Never lose a save.** Every operation that could break a world (mod change,
   version change, world switch, settings change) snapshots it first and can be undone
   in one click. Stops are graceful: warn players, save, then stop.
2. **The game outlives the panel.** The game runs in its own container with a Docker
   restart policy. If Foreman crashes, is upgraded or is removed, the server keeps
   running and comes back after a reboot.
3. **Pinned, never drifting.** Every world records its exact game version. Foreman
   *tells* you an update exists, with the changelog, and updates only when asked.
   It never downloads anything at startup to stay current.
4. **Plain files on disk.** Worlds are ordinary directories (saves, mods, config) that
   a human can read, back up and move without Foreman. No opaque database holds game
   data. The app database holds only users, sessions, app settings,
   notification settings and the audit log.
5. **Least privilege.** It doesn't run as root. It talks to Docker through a restricted
   socket proxy where possible and documents exactly which API calls it needs.
6. **Small and boring.** One container, a small image, no runtime downloads beyond
   the mod portal and game images the admin asked for. It works on a phone.

## Core concepts

- **World**: a named directory holding saves, mods (`mod-list.json`, `mod-settings.dat`,
  zips), `server-settings.json`, `map-gen-settings.json`, `map-settings.json`, the admin,
  ban and whitelist files, a pinned game version, and a Space Age on/off flag.
- **Active world**: the one world currently running. **Exactly one at a time in v1.**
  The data model doesn't assume one, so running several worlds at once, each on its own
  port, can come later without a migration.
- **Runtime mode**, chosen at install:
  - **Managed** (the default for the public product): Foreman creates, configures and
    owns the game container (a `factoriotools/factorio` image at the world's pinned
    version), with its own ports and restart policy.
  - **Adopted**: Foreman controls an existing container that someone else defined, for
    example in a compose file, by name. It starts, stops and restarts it, and switches
    worlds by changing what the container's bind mount presents, **without anyone
    editing the compose file per switch**. In this mode the game version belongs to
    whoever owns the container definition. Foreman shows the version, warns on a
    save/version mismatch, and doesn't try to change it.
- **Snapshot**: a point-in-time copy of a world (saves, mods, config), taken
  automatically before risky operations or by hand, and restorable in one click.

## v1 features

### Server control
- Status: running, stopped, starting, crash-looping. Shows uptime, game version, the
  loaded save, and CPU and memory use.
- Start, Stop, Restart. **A graceful stop** warns in game with a countdown, runs
  `/server-save` over RCON, then stops. A forced stop is a separate, deliberate action.
- **Switching worlds**: a graceful stop of the current world, a snapshot of both
  worlds, the swap, a start, and confirmation that it reached "InGame".
- **Scheduled restarts** (e.g. nightly 04:00) that **run only when nobody is online**,
  with an in-game warning if someone joins during the countdown.

### Live operations
- Players: who's online and for how long. Kick, ban, unban, promote and demote, and
  whitelist. Edit the admin, ban and whitelist files.
- RCON console and in-game chat: send commands and messages, and see chat.
- Live log stream, plus **crash detection** that names the reason when it can (a mod
  mismatch, a failed save load, a version mismatch, a port in use) and flags a crash
  loop instead of letting it spin.
- Notifications: webhooks on start, stop, crash, player join and leave, and scheduled
  restarts. Presets for Discord and ntfy, plus a generic JSON webhook.

### Worlds
- Create a world in one of three ways:
  - **Generate**: from a preset, a map exchange string, or map-gen settings plus a
    seed, with Space Age on or off.
  - **Clone**: copy an existing world's mods and settings, then start a fresh map or
    keep the save.
  - **Import**: adopt an existing directory of saves and mods (the `saves-2025`
    style), or upload a save zip and sync its mods from the portal.
- Rename, archive and delete. Delete always offers a final snapshot first.
- Saves: list the slots with timestamps and sizes, download any save, upload a save,
  and choose which save loads (the latest, or a specific file).
- Snapshots: automatic before every risky operation, by hand on demand, listed per
  world, restorable in one click. Retention is a setting.

### Mods (per world)
- Search the mod portal, install with **dependency resolution**: required, optional,
  incompatible and version constraints, with the plan shown before applying it.
- Update: show which mods in a world have updates, and apply them as one snapshotted
  operation.
- Enable, disable and remove. Upload local zips. Edit `mod-settings.dat`, or at least
  upload and download it.
- **Sync from save**: read the mod list and versions from a save and make the world's
  mods match it.
- Built-in and DLC mods (`base`, `space-age`, `quality`, `elevated-rails`, and
  `recycler` from game 2.1) are handled correctly, never offered for
  download, and follow the world's Space Age flag.
- Portal credentials (factorio.com username and token) **come from env vars or
  `*_FILE` paths first**, so they work with sops or Docker secrets. If those are absent,
  they can be entered in the UI and stored encrypted with a key supplied by env. They
  are never shown back in the UI.

### Game versions (per world, managed mode)
- Each world shows its pinned version, and a notice when a newer stable or
  experimental release exists, with its changelog.
- Updating takes a snapshot, pulls the exact image, restarts and verifies. Rollback
  restores the snapshot and the previous version together.
- Foreman never auto-updates.

### Settings
- `server-settings.json`: a form for the common fields (name, description, password,
  visibility, autosave interval and slots, auto_pause, admins, max players,
  verification), plus a validated raw JSON editor for everything else. Every save shows
  a diff and takes a snapshot first.
- The same form-and-raw approach for `map-gen-settings.json` and `map-settings.json`,
  where they apply (map-gen only matters when generating).

### Auth
- **Single role in v1**: every user is a full admin. The model leaves room for roles
  later, such as a read-only viewer.
- Three ways to sign in, any combination enabled:
  - **Local accounts**: username and password, with first-run admin setup.
  - **Trusted proxy headers**: forward-auth (Authentik, Authelia, oauth2-proxy), with a
    configurable user header and optional group header with a required group. Honoured
    **only** from configured proxy CIDRs.
  - **Native OIDC**: discovery URL, client ID and secret (env or `*_FILE`), and an
    optional required group claim.
- An audit log of who did what: start, stop, switch, mod changes, settings changes,
  RCON commands.

## Non-goals (v1)

- Several worlds running at once (designed for, not built).
- Multiple Docker hosts, clustering, or remote agents.
- Fine-grained roles or per-world permissions.
- Running the game without Docker (no child-process mode).
- Commercial hosting features: billing, quotas, tenants.
- Scheduled backups to external storage. Snapshots are local. Users back up the data
  directory with their own tools.
- Automatic updates of anything.
- Windows hosts, and any game other than Factorio.

## Constraints the spec must respect

- **Host paths.** In managed mode Foreman creates containers with bind mounts, which
  the Docker daemon resolves on the *host*. Foreman must know the host path of its
  worlds directory (configured, or discovered from its own mounts) and refuse to start
  if it can't resolve it. This is the classic Docker-in-Docker path trap.
- **File ownership.** `factoriotools/factorio` runs as PUID/PGID 845 by default. Worlds
  that Foreman writes must stay readable and writable by the game's uid/gid, and both
  are configurable. Foreman itself runs as non-root.
- **RCON** is how Foreman talks to the game: save, chat, players, commands. In managed
  mode Foreman owns the RCON password. In adopted mode it reads the password from the
  world's `config/rconpw`, or takes it from config. RCON is never published beyond
  Foreman's network.
- **Docker API surface.** Document the minimum set of endpoints (containers, images
  pull, logs, stats) so that a socket proxy (e.g. Tecnativa or LinuxServer
  socket-proxy) can be locked down to exactly those.
- **No secrets in the UI or logs**: the RCON password, portal token, OIDC secret and
  game password.
- **Upgrading Foreman must never touch running games** or rewrite world files without
  a snapshot.

## Success looks like

- An existing OFSM user can retire it, and nobody needs a shell to run the server.
- From a phone: see who's on, warn and restart the server, switch from the family base
  to a friend's achievement run, and switch back, with no save lost and no mod mismatch.
- A new user goes from `docker run` to a running Space Age server with three portal
  mods in under ten minutes, without reading the factoriotools README.
- A bad mod update is undone in one click.

## Reference deployment

The first real deployment uses **adopted mode**: the game stays a compose stack whose
image is pinned and updated by Renovate, and Foreman runs as its own stack behind a
forward-auth proxy (Caddy + Authentik), signing users in by **trusted headers** with a
required group. Existing hand-renamed save and mod folders are imported as worlds.

## Open questions for the spec

1. *(Answered in SPEC 4.3 and 4.8.)* **How world switching works in adopted mode**: symlinks inside the bound directory
   (relative, so they resolve in the container), moving directories, or the factoriotools
   `SAVES`/`MODS`/`CONFIG` env paths. The answer must not require editing the compose
   file, and it must survive a reboot mid-switch.
2. *(Answered in SPEC 6.7 to 6.9.)* **How snapshots are stored**: plain copies, hardlink dedup, or compressed archives.
   Autosaves are around 30 MB × 30 slots per world, so space matters.
3. *(Answered in SPEC 7.11.)* **Which save loads**: drive factoriotools' `LOAD_LATEST_SAVE`/`SAVE_NAME`, or have
   Foreman place the chosen save so that "latest" is right.
4. *(Answered in SPEC 9.9 and 9.10.)* **How to read a save's mod list** for "sync from save" (the level header inside the
   zip), and what to do with mods that no longer exist on the portal.
5. *(Answered in SPEC 8.7 and 8.8.)* **Crash-reason detection**: a log-pattern catalogue versus exit-code heuristics, and
   how to keep the catalogue current across game versions.
6. *(Answered: docs/STACK.md.)* **Tech stack**: deliberately left open. The constraints are one small container, a
   responsive UI, and no runtime downloads.
7. *(Answered: MIT, SPEC 18.10.)* **License**: MIT or Apache-2.0.
