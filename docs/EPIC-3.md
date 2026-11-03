# Epic 3 plan: Server control in managed mode

Serves SPEC 18.3: creating the game container (4.3 to 4.6); status (7.1,
7.2); start, stop and restart (7.3 to 7.8), including the pre-start check
and the save reader it needs (7.4, 9.9); save selection (7.11); the log
stream and crash detection (8.6, 8.7, 8.9); the player list (8.1); the
RCON console (8.4) and its audit entries. It also finishes snapshot and
restore of a running world (6.5, 6.10), which docs/STATUS.md assigns to
this epic. Branch `epic/3-server-control`, cut from `main` at e1d36ac. No
GitHub issues are open for this area.

This plan lives only on the epic branch. The fold task moves its lasting
rules into SPEC.md, STACK.md and ADR 0003, then deletes it.

## Rulings

Settled by Todd:

- **R1. The game runs without the image's entrypoint.** The container runs
  `/opt/factorio/bin/x64/factorio` directly as
  `FOREMAN_GAME_UID:FOREMAN_GAME_GID`, with Foreman's own flags. A small
  shell wrapper reads the RCON password from the world's file, so it never
  appears in the container's settings. All capabilities are dropped and
  `no-new-privileges` is set. SPEC 4.2 is rewritten in the fold. Foreman
  generates maps itself in Epic 6.
- **R2. Container identity.** The name is `foreman-<world-id>`. Labels
  under `io.github.toddawhittaker.foreman.` hold the world id and the
  install (the host data path). Foreman finds its containers by label
  after a restart, with no database row. A name already taken by a
  container Foreman did not create is refused in plain words.
- **R3. Network.** A new setting, `FOREMAN_GAME_NETWORK`. When Foreman's
  container is on exactly one network it is used without the setting;
  when on several, the setting is required. Outside a container (local
  development and tests) the game joins Docker's default `bridge`.
  Foreman reaches RCON at the container IP from inspect, on port 27015,
  which is not published.
- **R4. A chosen save applies once.** After the start that loads it
  reaches InGame, the choice resets to the newest save (`load_save` is
  removed from `world.json`).
- **R5. A save Foreman cannot read** refuses the start, with "Start
  anyway" behind a confirmation that says the mods could not be checked.
  Foreman takes a snapshot first.
- **R6. Console commands that carry a secret** (such as `/config set
  password`) run, but are recorded in the audit log and shown on the page
  with the secret replaced by `••••`. The list of such commands is a
  constant with tests.
- **R7. The game image** is never downloaded by Start. When the pinned
  image is missing, Start refuses and the Server page offers a separate
  "Download game image" button, which pulls the exact tag as a visible
  step and is audited.
- **R8. Game versions 1.1, 2.0 and 2.1** are supported. The newest 1.1.x,
  the newest 2.0.x and 2.1.24 are pinned by digest. The save reader and
  the real-game tests cover all three. A save from any other version is
  refused at start.
- **R9. Running-world snapshot and restore are built in full.** A snapshot
  of a running world sends `/server-save` and waits for it to finish
  first. A restore of a running world stops the game, takes the
  before-restore snapshot, restores, and starts again.
- **R10. The log and the console have their own Console tab.** The Server
  page holds status, players and the start, stop and restart controls.
- **R11. The audit log table is shaped for all of SPEC 12.9**: id, time,
  user id (null once the user is deleted), username, action, world id,
  detail. This epic records start, stop, restart, forced stop, image
  download and console commands. Epic 9 adds the token name; Epic 10 adds
  the viewer and the Epic 2 actions.

Settled by the orchestrator (easy to change):

- **R12. Fresh container per start.** Every start Foreman makes removes
  the old stopped container and creates a new one, so settings never
  drift. Docker's own restarts (`unless-stopped`) never recreate it.
- **R13. Polling, not events.** A monitor inspects the container every
  few seconds and whenever the log stream ends. It compares restart
  count, start time, exit code and out-of-memory flag with what it saw
  last. Docker's `GET /events` is not used.
- **R14. Container shape (SPEC 4.3, 4.6).** Stdin open, no TTY (the server
  quits on EOF); restart policy `unless-stopped`; one bind mount,
  `<host data>/worlds/<id>` at `/factorio`; UDP 34197 published; a
  rotating `json-file` log driver; the stop signal and timeout found by
  the spike (1d).
- **R15. Port 34197 is fixed** in this epic. A per-world port (SPEC 5.3)
  is an additive `world.json` field for later.
- **R16. Docker unreachable** shows the state "Unknown" with the reason;
  Foreman keeps serving pages.
- **R17. "Start anyway"** is offered only for mod and Space Age mismatches
  and an unreadable save (R5). A save from a newer game version, a save
  from an unsupported version, or no save at all is a plain refusal.
  Until Epic 5, `mod-list.json` is what will load; when it disagrees with
  the world's Space Age flag, the start is refused and says so.
- **R18. Player list.** RCON `/players online` decides who is on. "How
  long" comes from `[JOIN]` lines in the container's log since it
  started, so a chat line cannot fake a join.
- **R19. The log history is the container's log.** It is lost when the
  container is recreated at the next Foreman start; the page keeps at
  most 1,000 lines. Nothing is stored on Foreman's side.
- **R20. The RCON password file** is created when missing, mode 0640.
  Snapshots copy it like any other world file.
- **R21. The 60-second stop countdown** is a constant until the settings
  page (Epic 10). Audit retention is left to Epic 10.
- **R22. SSE (server-sent events) for live pages.** The htmx SSE
  extension is vendored at an exact version with its checksum. SSE runs
  through the existing long-request path with a heartbeat inside the
  60-second idle limit. Foreman's shutdown closes SSE connections first.
- **R23. The Docker client** gains a second HTTP client with no total
  timeout, so each long call (log follow, pull, stop) sets its own
  deadline. Queries and request bodies are allowed; the path is still
  checked against the SPEC 14.3 list.
- **R24. `make run` contacts Docker** from this epic on. STACK 13 and
  WORKFLOW change in the fold.

## Tasks

Every task starts from the epic head named in its prompt. No two tasks in
one wave edit the same file.

### Wave 1 (base e1d36ac)

| Task | Owner, model | Files it may touch | Done means |
|---|---|---|---|
| 1a Docker client | builder, Sonnet high | `internal/docker/**` including a new `dockertest/`; `internal/datadir/datadir.go` and its test | Every SPEC 14.3 call is wrapped, with R23. Log stream split into stdout and stderr. Pull errors inside a 200 reply surfaced. 404, 304 and 409 as named errors. `Allowed` exported; `SelfID` moved here from `datadir`. The fake refuses any call outside 14.3. |
| 1b RCON and audit store | builder, Sonnet high | `internal/rcon/**` including `rcontest/`; `internal/store/migrations/0003_audit_log.sql`; `internal/store/audit.go` and its test | One command at a time, dial and read timeouts, replies split across packets, a distinct error for a failed login, the password as `secret.Secret` and never in errors. Audit rows inserted and listed (R11). |
| 1c Save reader | builder, Opus | `internal/savefile/**` including `testdata/` | `Read` returns the game version and the mod list from 1.1, 2.0 and 2.1 saves (Space Age on and off where it exists), or a typed error saying why not. A fuzz test whose seeds run in `make verify`. `testdata/README` says how each fixture was made. |
| 1d Real-image spike | tester, Opus low | `internal/game/testdata/**`; the Makefile's `GAME_IMAGE_*` variables only | For each of the three pinned images: the command line and flags that work as a user other than 845 with a writable write-data folder; which stop signal saves cleanly, and the timeout; whether `POST /kill` keeps an `unless-stopped` container stopped; the exact log lines for loading, InGame, save, join, leave and crash; the replies to `/players online` and `/server-save`; the built-in DLC mods; image digests. Transcripts saved as test data. |
| 1e Styles and SSE vendor | ui-designer, Sonnet high | `internal/web/static/foreman.css`; `internal/web/static/vendor/*`; `internal/web/render_test.go` (checksum test only); `docs/DESIGN.md` | The state badge (DESIGN 7.5), the step list in progress, the log box and the console (DESIGN 8.5) are styled in both themes and at 320 px. The SSE extension is vendored with its checksum tested (R22). |

### Wave 2 (from the epic head after wave 1)

| Task | Owner, model | Files it may touch | Done means |
|---|---|---|---|
| 2a World support | builder, Opus low | `internal/world/{precheck,mods,builtin,rconpw}.go` (new), `saves.go`, `restore.go`, `snapshot.go` and their tests | The pre-start check (SPEC 7.4, R5, R17) returns structured problems, each saying what the game would delete. `PrepareLoad` sets the chosen save's time, never in the future (7.11), and R4's reset. Held-lock variants of snapshot and restore for the controller (R9). The RCON password file (R20). |
| 2b Pure game helpers | builder, Sonnet high | `internal/game/{logs,players,broadcast}.go` and their tests | Recognises the log lines from 1d's transcripts for all three versions, removes the RCON password from relayed lines, keeps a 1,000-line buffer, parses `/players online` (R18), and fans out to live pages, dropping slow viewers. |
| 2c Fake game | builder, Sonnet high | `internal/fakegame/**`; `cmd/fakegame/`; the Makefile `e2e` target; `e2e/tests/foreman.ts` | The fake serves Docker on a Unix socket and RCON on its own loopback address (127.x.y.z, port 27015), replays real transcripts, and has test hooks (crash, player joins, RCON refuses, save hangs, image missing). One e2e smoke test uses it; all existing e2e tests still pass. Never in the image. |

### Wave 3

| Task | Owner, model | Files it may touch | Done means |
|---|---|---|---|
| 3a Controller | builder, Opus | Everything else in `internal/game/*.go` | Unit tests against the fake pin SPEC 4.3 to 4.6, 7.1 to 7.8, 7.11, 8.1, 8.4, 8.6, crash and crash-loop detection in 8.7, 8.9, R2, R3, R7, R9, R12, R13 and R16, with an injected clock. Re-attaches to a running game after Foreman restarts and never stops the game when Foreman starts or stops (3.8). Every method takes a world id and the acting user; audit entries are written here. |

### Wave 4

| Task | Owner, model | Files it may touch | Done means |
|---|---|---|---|
| 4a Server page and actions | builder, Opus | `internal/web/server.go`, `render.go`, `render_test.go`, `layout.html`; new `internal/web/game.go`; `templates/server.html` and its confirm templates; `internal/web/snapshots.go`; `cmd/foreman/main.go`; `e2e/tests/server.spec.ts` | Server page at `/`: state, players, start, stop (with the countdown), restart and forced stop with confirmations; pre-start refusals and "Start anyway"; the step list; "Download game image" (R7); live updates over SSE. Running-world snapshot and restore (R9). Shutdown order in `main.go` (R22). axe passes in both themes, desktop and 320 px. |
| 4b `make test-game` | tester, Sonnet high | `internal/game/game_integration_test.go` (build tag `game`); the Makefile `test-game` target | Against each of the three pinned images: reaches InGame and RCON answers; players listed; console works; a graceful stop saves; a forced stop stays stopped; a corrupt save crash-loops and ends stopped; no password in relayed log lines; the save reader reads a save the game just wrote. |

### Wave 5

| Task | Owner, model | Files it may touch | Done means |
|---|---|---|---|
| 5a Log and console | builder, Sonnet high | new `internal/web/console.go`; `templates/console.html`; `static/log.js`; `render.go`, `server.go`, `layout.html`; `e2e/tests/console.spec.ts` | Console tab (R10), DESIGN 8.5: at most 1,000 lines; follows new lines only while at the bottom; "Jump to newest"; announces the reply to your own command. Each command audited, with R6. |
| 5b Invariant tests | tester, Sonnet high | New files only: `e2e/tests/server-invariants.spec.ts`, `internal/web/game_security_test.go` | Tests for every SPEC 14 boundary this epic touches. Bugs found are left as skipped tests named `BUG`. |
| 5c `make e2e-game` | tester, Sonnet high | The Makefile `e2e-game` target; `e2e/game/**`; `e2e/playwright.config.ts` | The vision's phone scenario passes against the real 2.1 image: see who is on, warn them, restart. |

### Wave 6

Reviews (code, security, accessibility), the fix round, the real-game
check, and the fold (builder, Sonnet high, `docs/**` only, including
ADR 0003 on the container's shape and identity), as in the epic-run skill.

## Shared files and their owners

- Migration 0003: 1b. No other migration in this epic.
- `foreman.css` and `DESIGN.md`: 1e. Later style needs go to a small
  ui-designer follow-up.
- `render.go` and `render_test.go`: 1e, then 4a, then 5a.
- `layout.html` and `server.go`: 4a, then 5a. `main.go`: 4a.
- Makefile: 1d, 2c, 4b, 5c, each only its own variables or target.
- `e2e/tests/foreman.ts`: 2c. `e2e/playwright.config.ts`: 5c.
- `internal/world`: 2a only. `internal/game`: 2b, then 3a; 4b adds only
  its tagged test file.
- SPEC, STACK, STATUS, WORKFLOW and the ADR: the fold only.

## For the fold

SPEC changes: 4.2 rewritten (R1); 4.3 and 4.6 list the fixed container
parts (R14); 4.4 and 14.3 cover Foreman on several networks or outside a
container (R3); 3.7 and 4.2 cover the image download (R7); 7.1 adds
"Unknown" (R16); 7.4 per R5 and R17; 7.11 per R4; 8.4 and 14.8 per R6;
8.1 per R18; 8.6 per R19; 4.5 per R20; 18.3 names 6.5 and 6.10. STACK
pins the three game images and records R23 and R24. ADR 0003 records R1,
R2, R12 and R13 and the options rejected.

## Left out on purpose

- Docker `GET /events` (R13).
- Escaping user text for RCON (SPEC 14.9): nothing here builds a command
  from user text; it arrives with chat in Epic 8.
- Naming crash reasons (Epic 8): Epic 3 says "unknown" and shows the last
  20 log lines.
- Remembering "crash-looping" across a Foreman restart: the game stays
  stopped either way and shows as Stopped.
- A console transcript kept on the server: reloading the page clears it.
- A per-world port, map generation, switching and adopted mode: later
  epics.
- A settings table (Epic 10).
