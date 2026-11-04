# Epic 4 plan: World switching and adopted mode

Serves SPEC 18.4: switching (7.9, 7.10) and adopted mode (4.7 to 4.12).
Gate A (SPEC 19.1) follows this epic, so the end state must let Foreman
run in adopted mode behind Caddy and Authentik, import the hand-renamed
folders, and switch between two worlds and back from a phone with no save
lost. Branch `epic/4-switching-adopted`, cut from `main` at a38c137. No
GitHub issues are open for this area.

This plan lives only on the epic branch. The fold task moves its lasting
rules into SPEC.md and ADR 0004, then deletes it.

## What the image's entrypoint does

In adopted mode the game runs the factoriotools image's own
`/docker-entrypoint.sh`, not Foreman's command (ADR 0003 applies to
managed mode only). Checked on the pinned 1.1.110, 2.0.77 and 2.1.24
images:

1. The image sets `SAVES`, `MODS` and `CONFIG` to `/factorio/saves`,
   `/factorio/mods` and `/factorio/config`. SPEC 4.11 must refuse only a
   value other than those defaults.
2. `DLC_SPACE_AGE` (default `true`) rewrites `mods/mod-list.json` at every
   start, after Foreman's pre-start check.
3. `LOAD_LATEST_SAVE=false`, `GENERATE_NEW_SAVE=true` and
   `UPDATE_MODS_ON_START=true` can load the wrong save, overwrite
   autosaves, or change mods with no snapshot.
4. The script runs with `set -x`, so the RCON password appears in
   `docker logs` at every start, and the adopted container keeps one log
   across switches.
5. Run as root, it does `chown -R` to `PUID:PGID` over the whole game
   folder.

## Rulings

Settled by Todd:

- **R1. Switch leaves the running state as found.** A running world ends
  up running on the new world; a stopped one ends up stopped with the new
  world active.
- **R2. Adopted container refusals beyond SPEC 4.11.** Foreman refuses a
  container with `LOAD_LATEST_SAVE` other than `true`,
  `GENERATE_NEW_SAVE=true`, `UPDATE_MODS_ON_START=true`, any mount under
  `/factorio` other than the game folder itself, or `PUID`/`PGID` other
  than `FOREMAN_GAME_UID`/`FOREMAN_GAME_GID`. It names the variable, never
  its value. Starting a 2.x world whose Space Age flag disagrees with the
  container's `DLC_SPACE_AGE` is a plain pre-start refusal (7.4). So one
  adopted container serves either Space Age worlds or non-Space-Age
  worlds. Todd's Gate A worlds share the setting.
- **R3. The switch record lives in the app database**, migration 0004,
  table `world_switches`. SPEC 3.4's list grows by one line in the fold.
- **R4. First adoption keeps the originals.** The real `saves`, `mods` and
  `config` directories are moved into `pre-foreman-<UTC time>/` inside the
  game folder. Nothing is deleted; save and mod zips are hardlinked into
  the new world.
- **R5. Accepted risk (SPEC 14 in the fold).** In adopted mode the game
  container can read and write every world, because the links must
  resolve inside it. Defences: Foreman opens files through `os.Root` and
  never follows a link out of the folder, refuses a `worlds` folder that
  is itself a link, and keeps snapshots, `import/` and `foreman.db` in
  `FOREMAN_DATA_DIR`, outside the game folder. Managed mode stays the
  safer public default.

Settled by the orchestrator (easy to change):

- **R6. Checks before the stop.** The target world's pre-start check and
  the container (or image) checks run before the graceful stop, as
  restart does (7.8). The check named as step 3 in 7.9 still runs after
  the snapshots.
- **R7. Switch back is a full switch** (snapshots and confirmation). When
  the active world never changed, the offer is just Start.
- **R8. Managed mode leaves the old container stopped.** Every start
  creates a fresh container anyway (ADR 0003).
- **R9. `Switch(ctx, from, to, …)` takes `from` explicitly** and refuses
  when `from` is no longer active, ready for the MCP endpoint (21.4) and
  for several active worlds later, which stays unbuilt.
- **R10. Labels.** Switch snapshots are labelled "Before switch" with
  reason `switch`. The audit action is `switch`.
- **R11. The container's environment is never logged or shown.** Only
  known names are picked out of it.
- **R12. Recovery never starts or stops a game (3.8).** On startup:

  | Last step recorded | Startup does |
  |---|---|
  | before `presented`, and `current` (adopted) still names `from` | Clears the record. Notice: "Switch from A to B stopped at *step*. A is still active and was not started again." Offers Start A. |
  | `checked`, but `current` already names `to` (died between rename and database write) | Treated as `presented`: sets the active row to `to`, then as below. |
  | `presented` or `started` | Clears the record. Notice: "B is active; the switch did not confirm it running." Offers Start B and Switch back to A. |

  The notice is held in memory, cleared by the next action, and logged.
- **R13. Switch steps:** `begun`, `stopped`, `snapshotted`, `checked`,
  `presented`, `started`. The row is written before the first step and
  deleted when the switch ends, whether it worked or failed. The order
  alone keeps 7.10: the target passes the check, and the old game is
  stopped, before anything changes what the container presents.
- **R14. One controller.** Adopted mode is a set of `if c.adopted != nil`
  branches calling functions in `internal/game/adopted.go`, not an
  interface. They cover: the monitor's container lookup, `ourContainer`,
  the container step of a start, and step names. `startLocked` splits
  into `checkLocked` (check, start-anyway snapshot) and `launchLocked`
  (container through RCON).
- **R15. A switch is busy on both worlds.** `opRun` holds a list of worlds
  and a current one. Step changes are written holding every world's lock
  (taken in id order). Both pages show the operation; a forced stop on
  either interrupts it.
- **R16. Present.** Managed mode: one transaction that swaps the active
  row (`ReplaceActiveWorld(from, to)`, deleting only `from`'s row) and
  sets the record's step. Adopted mode: `gamedir.Present(to)` (missing
  fixed links made as `<name>.new` and renamed in; `current.new` linked to
  `worlds/<id>`, renamed over `current`, folder synced), then the same
  transaction.
- **R17. Directories in adopted mode.** `worlds/` is
  `FOREMAN_GAME_DIR/worlds`; `snapshots/`, `import/` and `foreman.db` stay
  in `FOREMAN_DATA_DIR`. Startup refuses when the data directory is inside
  the game folder. The ownership check (3.6) runs on both.
- **R18. Game folder states.** *Fresh* (none of `saves`, `mods`, `config`,
  `current`; links made at first start), *Unadopted* (real directories;
  show the adoption form), *Ready* (`current` is a relative link to
  `worlds/<valid id>` and the fixed links are right), *Broken* (anything
  else; server control refused, the page names the path). `current` is
  read with Readlink and checked exactly. A leftover `current.new` is
  removed at recovery. In *Ready* the link decides the active world; the
  database row is corrected to match, and the correction is logged.
- **R19. First adoption steps:** reserve the world id; container checks;
  graceful stop (password from the folder's own `config/rconpw`); build
  `worlds/<id>` in staging (zips hardlinked, small files copied); write
  `world.json` and rename into place; "Before adoption" snapshot; cut over
  each of `saves`, `mods`, `config` (link as `<name>.new`, move the real
  directory into `pre-foreman-<time>/`, rename the link in); make it
  active; start again if it was running. Recovery finishes a half-done
  cut-over from what is on disk. The form asks for name, game version and
  Space Age (defaults from the container's `VERSION` and
  `DLC_SPACE_AGE`) and the countdown; its danger box says what will be
  stopped, snapshotted and moved.
- **R20. Container checks** (inspect only, before any stop): R2's
  refusals and 4.11; no mount at `/factorio` refused; its `/factorio`
  source must equal Foreman's `FOREMAN_GAME_DIR` mount source (outside a
  container, compare paths); Foreman must share a Docker network with it.
  RCON uses the address on the shared network and the container's
  `RCON_PORT` (default 27015).
- **R21. Version (4.9).** Running version from the container's `VERSION`,
  else "unknown". A save newer than the running version is refused, as
  `ErrSaveNewerThanImage` is in managed mode; unknown skips that check. A
  save from a different version is a warning on the Server and switch
  pages.
- **R22. RCON port warning (4.12)** when `RCON_PORT/tcp` is published on
  the host, or the network mode is `host`.
- **R23. Password (4.10).** `FOREMAN_RCON_PASSWORD` (or `_FILE`) when set;
  otherwise the world's `config/rconpw`, created at 0640 when missing,
  re-read whenever the world presented changes.
- **R24. The log in adopted mode** is followed from the current run's
  start time only, so one world's password line never shows on another
  world's page. Redaction (Epic 3) still applies.
- **R25. Foreman never, in adopted mode,** creates or removes the
  container, pulls or inspects images, or changes environment, command or
  networks, and never stops the container at startup. The adopted Docker
  client refuses those calls; the fake Docker fails a test that makes one.

## Tasks

Every task branch is `epic-4/<task>`, opens its own PR into the epic
branch, and runs `make verify`. Tasks that touch `internal/game` or
`internal/web` also run `make e2e` and `make e2e-game`. "Opus" tasks need
design judgment; "Sonnet" tasks have every choice settled here.

| Wave | Task | Owner, model | Files it may touch |
|---|---|---|---|
| 1 | T1 settings: `FOREMAN_GAME_CONTAINER`, `FOREMAN_GAME_DIR`, `FOREMAN_RCON_PASSWORD` (+`_FILE`); R17 refusal | builder, Sonnet high | `internal/config/config.go`, `config_test.go`, `env.go` |
| 1 | T2 Docker: container env, user, ports, network mode, mounts in inspect; adopted client refusing R25 calls | builder, Sonnet high | `internal/docker/containers.go`, `client.go`, `allowlist.go`, their `_test.go`, `internal/docker/dockertest/daemon.go`, `handlers.go` |
| 1 | T3 store: migration 0004, `switches.go`, `ReplaceActiveWorld` | builder, Sonnet high | `internal/store/migrations/0004_world_switches.sql`, `switches.go`, `switches_test.go`, `worlds.go`, `worlds_test.go` |
| 1 | T4 game folder layout (R16 adopted half, R18, recovery), crash test at every rename | builder, Opus | `internal/gamedir/*` (new) |
| 1 | T5 adoption world build (R19 staging part), refuse `worlds` as a link | builder, Sonnet high | `internal/world/adopt.go`, `adopt_test.go`, `world.go`, `snapshot.go` |
| 1 | T6 fake game: seeded adopted container with env, mount, ports and `set -x` password lines | builder, Sonnet high | `internal/fakegame/*`, `cmd/fakegame/main.go` |
| 2 | T7 switch in the controller (7.9, 7.10, R6 to R16), recovery (R12), `opRun` and `startLocked` split | builder, Opus | `internal/game/switch.go` (new), `switch_test.go`, `actions.go`, `controller.go`, `rig_test.go` |
| 3 | T8 adopted mode in the controller (R19 to R25) | builder, Opus | `internal/game/adopted.go` (new), `adopted_test.go`, `container.go`, `monitor.go`, `live.go` |
| 3 | T9 switch web pages: world-page Switch button, confirmation with countdown, step list, switch back, recovery notice, live reload when the active world changes | builder, Sonnet high | `internal/web/switch.go`, `switch_test.go`, `game.go`, `live.go`, `serverpage.go`, `snapshots.go`, `templates/switch.html`, `world.html`, `server.html`, `static/live.js`, `static/foreman.css` |
| 4 | T10 adopted web pages: adoption form, 4.9, 4.12 and layout warnings, no image download in adopted mode | builder, Sonnet high | `internal/web/adopt.go`, `adopt_test.go`, `templates/adopt.html`, `server.go`, `serverpage.go`, `templates/server.html` |
| 4 | T11 startup wiring for adopted mode and recovery order | builder, Sonnet high | `cmd/foreman/main.go`, `main_test.go` |
| 4 | T12 switch invariant tests: crash at each step, fail safe | tester, Sonnet high | `internal/game/switch_invariants_test.go` (new) |
| 5 | T13 end-to-end with the fake: switch and back at 320 px, adoption, refusals, import in adopted mode, axe | tester, Sonnet high | `e2e/tests/switch.spec.ts`, `adopted.spec.ts`, `e2e/tests/foreman.ts` |
| 5 | T14 real-image adopted test: image's own entrypoint, `PUID` set to the runner, adopt, import, switch and back, file manifests compared, DLC refusal; all three images | tester, Opus | `internal/game/adopted_integration_test.go`, `Makefile` (`test-game` only) |
| 5 | T15 real-game phone scenario: switch and back in managed mode | tester, Sonnet high | `e2e/game/phone.spec.ts` |

T10 follows T9 because both touch `server.html` and `serverpage.go`.
Wave 2 and later are cut from the epic head after the wave before it
lands.

## After the tasks

1. Reviews on a read-only worktree at the epic head, in parallel:
   code-reviewer (including dead code), security-reviewer (Docker API,
   RCON, file paths, secrets), a11y-reviewer, and ui-designer review only.
2. Fix rounds through task PRs; each reviewer confirms closure.
3. Real-game check with Todd: a compose stack with the factoriotools
   image in adopted mode; a real client joins world A; switch to B, then
   back to A at 320 px; every save unchanged except new autosaves. Also
   check live updates through Caddy (16.3).
4. Fold task: STATUS Epic 4 section; SPEC 3.2 (three new settings), 3.4
   (switch record), 4.8 (layout details, R4), 4.11 (image defaults, R2),
   5.1 (`rconpw` in adopted mode too), 7.9 (R6), 14 (R5 accepted risk);
   ADR 0004 (adopted layout and container rules); README adopted-mode
   section with the Gate A steps (copy folders into `<data>/import/<name>/`
   with a `mods/` folder; Foreman joins the game's compose network); delete
   this plan.
5. Epic PR into main.

## Left out on purpose

Several worlds running at once beyond the schema shape; per-world ports;
checking the container's `command:` arguments; images other than
factoriotools beyond "version unknown"; switching from the world list;
the operator guide (Epic 10; the README section is enough for Gate A).
