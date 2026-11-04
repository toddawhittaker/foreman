# Status

What each epic delivered and the gaps it left. The epic's last task
(the fold) writes its section once, from the task pull requests. Gaps
here are known and accepted; each is either fixed by a later epic or
waits for a decision.

## Epic 1: Foundation

Serves SPEC 18.1. Everything below is built and tested.

### Delivered

- **Skeleton, build and CI.** A binary with `serve` and `healthcheck`,
  and a `/healthz` endpoint. Every make target in STACK 13 exists;
  `test-game` and `e2e-game` are stubs until Epic 3. The container image
  is distroless, runs as 845:845, and is about 7 MB compressed (limit
  30 MB, checked in CI). CI is GitHub Actions on `ubuntu-24.04`, in
  parallel jobs with caching and every action pinned by commit (STACK 1
  and 9). `make run` starts Foreman locally with `go run ./cmd/foreman serve`,
  using a `.dev-data` folder and your own user and group ids.
  The toolchain is pinned (STACK 1, ADR 0001), and `make verify` fails on
  a wrong local Go. A 70% coverage floor across `internal/` is enforced
  inside `make verify`; coverage is about 91%.
- **Configuration (SPEC 3.2).** Fourteen environment variables load with
  defaults and are validated, with every problem reported together.
  `_FILE` support is built and tested generically but no variable uses it
  yet. A `Secret` type always prints as "[redacted]".
- **App database (SPEC 3.4).** SQLite with write-ahead logging, foreign
  keys and a busy timeout, embedded numbered migrations, users and
  sessions, and a first-admin insert that is safe when two requests race.
  The file is mode 0660.
- **Data directory and Docker client (SPEC 3.3, 3.5, 3.6, 14.3).** Host
  path from the variable or by reading Foreman's own container, the
  startup ownership and writability check, and a small Docker client that
  allows only the SPEC 14.3 endpoints, agrees the API version (1.44 to
  1.52), refuses redirects and refuses `.` path segments. Only ping,
  version and inspect-container are built.
- **Sign-in core (SPEC 12.4, 12.5, 12.8, 12.11).** argon2id hashes that
  are rehashed at sign-in when settings rise, the rate limiter, at most
  4 password checks at once, the client address from trusted proxies,
  header identity with a required group, and server-side sessions with
  idle expiry.
- **Server wiring (SPEC 12.3 to 12.8, 14.7, 14.13, 14.14, 15.1).** Startup
  order, first-run setup open until an admin exists, local sign-in and
  sign-out, trusted-header sign-in, idle expiry with an hourly sweep,
  cross-origin protection, the Secure cookie rule, JSON logs, and
  anti-framing headers.
- **UI shell (SPEC 17, docs/DESIGN.md).** Layout, sign-in, setup,
  signed-out, home and error pages, with hand-written CSS and htmx 2.0.11
  vendored with a checksum test. The error summary is a full-size target
  and error pages have an "Error:" title.
- **End-to-end and security tests.** Playwright with axe at desktop and
  320 px, in dark and light, for first run, sign-in, sign-out, the rate
  limit, the keyboard and header sign-in. Go tests over a real listener
  for spoofed headers, second setup, cross-origin posts, secrets in logs
  and cookie attributes.
- **Review fixes.** Sign-in attempts are counted before the password is
  checked, so parallel requests cannot exceed the limit; IPv6 clients are
  counted by /64; failed sign-in logs hold no username; header sign-in
  requires a groups header and a required group; an empty
  `FOREMAN_AUTH_METHODS` means `local`; the public URL's origin is
  trusted for form posts; unused exports were removed. The limiter folds
  usernames in ASCII only, to match the database, and the "busy" message
  shows at form level. The hash-cap comment now says only what is true.

### Gaps left

- The limiter's tables hold at most 10,000 entries each; when full, the
  oldest entry is evicted, which can drop a count that was still in force.
- The limiter is in memory and resets when Foreman restarts.
- Anyone who can reach the sign-in page can keep a known username locked
  out by failing on purpose (SPEC 12.11).
- No browser test of forced-colours mode.
- Idle session expiry is tested at unit level only, not in a browser.
- The "busy" sign-in page has no end-to-end test.
- The session sweeper lives in `internal/web`, not beside the session
  code in `internal/auth`.
- `cmd/foreman` startup and graceful shutdown have no automated test.
- Host path discovery (SPEC 3.5) was checked once by hand against a real
  Docker daemon (Engine 29.9.2): the image found its data directory's host
  path in managed mode. There is no automated test against a real daemon.
- No tables yet for API tokens, app settings or the audit log; later
  epics add them as migrations 0002 and up.
- No user management or password change. Only the placeholder home page
  requires sign-in. There are no navigation links yet.
- Signing out a header user ends nothing; the proxy decides (SPEC 12.8).
- A `tcp://` `DOCKER_HOST` is unencrypted (SPEC 3.2); the operator guide
  in Epic 10 must say so.

## Epic 2: Worlds and snapshots

Serves SPEC 18.2: the world model (5.1 to 5.3); list, import, rename,
delete and saves (5.4, 5.5, 5.8, 5.10, 5.11); snapshots and restore
(section 6, except 6.10). Everything below is built and tested. Coverage
across `internal/` is about 89%. The format decisions are in ADR 0002.

### Delivered

- **World model (SPEC 5.1 to 5.3).** The new `internal/world` package.
  `world.json` has a `format_version`, keeps fields Foreman does not know,
  and is listed but never changed when a newer Foreman wrote it. A world's
  id is made from its name and is never reused. Names are unique ignoring
  case. Each world has a try-lock (`Manager.Lock`), so Epic 3 can hold it
  across stop, snapshot, restore and start. Startup recovery clears
  leftover `.partial-*`, `.staging-*` and `.deleting-*` folders. Every file
  operation inside a world, a snapshot or the inbox goes through `os.Root`.
  Which world is active is a row in a new table, `active_worlds` (SPEC
  3.4); the first world created becomes active.
- **List, world page, rename (SPEC 5.4, 5.8).** The world list at
  `/worlds` (the home page now redirects there) shows the active world
  first, and a world page shows its facts. Rename changes only the display
  name. "Last played" comes from the newest save, and shows "unknown" when
  it cannot be read.
- **Import (SPEC 5.5).** From the inbox `<data>/import/`, copying a
  folder's top-level save zips and its `mods/` folder, or by uploading one
  save zip. Imports copy, keep times, never hardlink and never change the
  source. A new world is built in one staging folder and renamed into
  place, so a refused or failed import leaves no world behind. Mod sync
  for an uploaded save is Epic 5.
- **Saves (SPEC 5.11, 7.11).** List (newest first), download, upload and
  choose which save loads. An upload with a name that exists is refused.
  An upload to a busy world waits up to 60 seconds for it. If the world
  was restored or deleted meanwhile, the upload is refused and nothing is
  saved, even when the old folder survived a failed clean-up.
  `NewestSave` is the one function for the newest save; Epic 3's pre-start
  check must use it.
- **Snapshots (SPEC 6.1 to 6.4, 6.7 to 6.9).** Manual and automatic
  snapshots in `snapshots/<world-id>/<UTC time>/`, with `snapshot.json`
  holding label, reason, time, who, and each file's size, time to the
  nanosecond and (for zips) SHA-256. Zips are hardlinked, with a recorded
  copy fallback and a note on the page. Hidden files are kept; a symlink or
  other special file in a world makes the snapshot refuse. Retention keeps
  the newest 10 automatic snapshots and never removes manual or
  before-delete ones. Disk use counts each file once by inode.
- **Restore (SPEC 6.5).** Under the world's lock, restore checks the
  manifest and every hash, takes a "Before restore" snapshot (and stops if
  that fails), builds the world in a staging folder with every time set
  from the manifest, then swaps folders through `.restore-old-*`. Startup
  recovery finishes or reverses a half-done swap. A failure after the
  "Before restore" snapshot says so and links to it. Restore refuses when
  another world has the snapshot's name.
- **Delete (SPEC 5.10, 6.6).** Final snapshot on by default (aborts the
  delete if it fails), name typed exactly, active world refused, and the
  page says why when a world cannot be deleted. The `snapshots/<id>` folder
  is always made so an id is never reused. `/deleted` lists deleted worlds
  and their snapshots; restoring one recreates the world.
- **Fail safe on disk space (SPEC 6.11).** Snapshot, restore, import and
  upload refuse up front when free space minus the bytes already promised
  to writes in flight is short of the write plus a fixed 2 GB reserve. All
  checks share one in-process count. Uploads without a declared length get
  411, and at most two uploads run at once (a third gets 429).
- **Uploads and long requests (SPEC 14.6).** Save uploads are limited to
  1 GB and other uploads to 200 MB, streamed to a temp file beside the
  destination. A zip's entry names and stated sizes are checked (50 times
  the zip's size at most) but a zip is never unpacked. Long routes (upload,
  download, import, restore) use a 60-second idle deadline instead of the
  short one, only for a signed-in request. A malformed upload header is
  refused with 422 in words.
- **Managed mode for `make run` and the end-to-end fixture.** Both run in
  managed mode with `FOREMAN_HOST_DATA_DIR` set, so no Docker call is made.
  `golang.org/x/sys` is now a direct dependency (hardlinks across
  snapshot folders).
- **UI.** Styles for confirmation, steps, world list, facts, the Active
  badge, tables, fieldsets and message kinds; the header "Worlds" tab; long
  names wrap at 320 px; the secondary button hovers grey. Forced-colours
  tab marking and a styled file picker. Every new page passes axe at
  desktop and 320 px in both themes.
- **Tests.** Unit tests for ids, `world.json`, copies, restore contents and
  times, and recovery of a half-done swap. Security and failure tests for
  hostile names, symlinks, hostile zips, a lying upload length, a full disk
  (a tmpfs under `unshare`; skipped when the machine cannot mount one),
  a crash at each restore step, and two admins at once. End-to-end tests
  for every page and flow.
- **Real-game check.** Against `factoriotools/factorio` (Factorio 2.1.24,
  image digest `sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28`):
  import, snapshot, upload, the load choice and restore all through
  Foreman's routes. The world matched the snapshot's manifest in size,
  time and SHA-256, and a real server loaded the save and reached InGame.

### Gaps left

- A session that expires during a long upload, download, import or restore
  does not stop it.
- The idle limit is 60 seconds without progress, not a minimum speed. A
  client that sends a byte just under every 60 seconds keeps an upload open
  (it needs a signed-in session). One signed-in session trickling two
  uploads holds both upload places, so every other admin gets 429, and
  keeps their declared bytes promised. When the disk is fairly full,
  snapshots, restores and imports needing more than free space minus
  (promised bytes plus 2 GB) are refused until the connections close or
  Foreman restarts. Nothing is damaged.
- Snapshot hashes are checked before restore stages files, not again
  while linking; a file changed in that window is not caught. Reusing a
  hash when the inode, size and time are unchanged can be fooled by a
  deliberate same-size rewrite with the time set back (it needs write
  access to the world).
- Free space is briefly counted twice while a write runs. This errs on
  the safe side.
- Warnings go only to the log: skipped unreadable snapshot folders, and
  retention or cleanup failures after a success.
- The failed-restore undo link has a render test only, because no handler
  test can make a restore fail after its snapshot.
- Taking a snapshot (POST) when `snapshots/<id>` is a symlink answers 500,
  not 404. Nothing is written.
- Restore's final name re-check lists every world while holding the create
  lock.
- Test helpers (tree readers, zip builders) are duplicated across test
  files.
- A restored running world (stop, snapshot, restore, start) and a snapshot
  of a running world (SPEC 6.10) wait for Epic 3.
- No delete of a single save. The Worlds tab covers the import page, and
  there is no Import tab.
- The 7.5 state badge (`.state`) is still unstyled.
- The `factoriotools/factorio` image's entrypoint fails when run as a uid
  other than its own (its `sed` on `/opt/factorio/config`), and the server
  exits on stdin EOF unless it is started with `-i`. Epic 3 must handle
  both (found in the real-game check).

## Epic 3: Server control in managed mode

Serves SPEC 18.3: creating the game container (4.2 to 4.6); status (7.1,
7.2); start, stop and restart (7.3 to 7.8) with the pre-start check and
the save reader it needs (7.4, 9.9); save selection (7.11); the log stream
and crash detection (8.6, 8.7, 8.9); the player list (8.1); the RCON
console (8.4) and its audit entries; and snapshot and restore of a running
world (6.5, 6.10). Everything below is built and tested. Coverage across
`internal/` is about 89.8%. The container's shape and identity are in ADR
0003.

### Delivered

- **Docker client (SPEC 14.3, STACK 5).** Every endpoint the epic uses is
  wrapped: ping, version, list (by label and by network), inspect, create,
  start, stop, kill, remove, logs with follow, stats, image inspect and
  image pull. Long calls (log follow, pull, stop) use a second HTTP client
  with no total timeout and set their own deadline. Docker's framed log
  output is split into standard output and error lines. An error inside a
  200 pull reply is returned. A pull reference with no tag or digest is
  refused. 404, 304 and 409 are named errors. `docker.Allowed` is exported
  and the `dockertest` fake daemon fails any call outside 14.3.
- **RCON client and audit log (SPEC 8.4, 12.9, 14.8).** One command at a
  time, dial and read timeouts, one reply packet per command up to 4 MiB
  (the real game never answers an empty command), a distinct login error,
  and the password as a `Secret` that no error holds. The audit log table
  (migration 0003) is shaped for all of 12.9: time, user (null once the user
  is deleted, the name stays), action, world and detail.
- **Save reader (SPEC 9.9).** `internal/savefile` reads the game version
  and mod list from `level.dat0` in 1.1, 2.0 and 2.1 saves, compressed or
  not, with typed errors and a fuzz test whose seeds run in `make verify`.
  Seven real saves are the fixtures.
- **Game container (SPEC 4.2 to 4.6, ADR 0003).** The game binary runs
  directly as `FOREMAN_GAME_UID:FOREMAN_GAME_GID` through a small wrapper
  that reads the RCON password from `config/rconpw`. All capabilities are
  dropped, `no-new-privileges` is set, stdin is open, the restart policy is
  `unless-stopped`, UDP 34197 is published, RCON is not, and the one bind
  mount is the world's folder. The name is `foreman-<world-id>`, with labels
  for the world and install. Every start makes a fresh container. Three
  images, for 1.1.110, 2.0.77 and 2.1.24, are pinned by digest (STACK 1), and
  a test fails if `cmd/foreman` and the Makefile differ. A game user or
  group of 0 is refused (3.6, 14.11). `FOREMAN_GAME_NETWORK` is required in
  a container, with the refusals in 4.4: Foreman not on the network, other
  containers on it, a `tcp://` Docker host not on another of Foreman's
  networks, an overlap with `FOREMAN_TRUSTED_PROXIES` (IPv4 and IPv6), and
  Foreman unable to find its own container while a marker file or `tcp://`
  shows it is in one. The RCON password file is created 0640 and written
  atomically; startup recovery narrows older, wider copies in worlds and
  snapshots (4.5).
- **State, start, stop, restart (SPEC 7.1 to 7.8).** The states are
  stopped, starting, running, stopping, crashed, crash-looping and unknown
  (with the reason, when Docker cannot be asked). Start runs under the world
  lock: write a default `mod-list.json` if missing (9.7), run the pre-start
  check, take a snapshot for "Start anyway", check the image (never a
  pull), set which save loads, create and start the container, then wait for
  InGame and an RCON answer within 5 minutes. A graceful stop checks RCON
  first, warns players in game (the countdown, then 30 and 10 seconds),
  runs `/server-save`, waits for "Saving finished", then calls Docker's stop
  (SIGTERM, 120-second timeout). A stop that Docker refuses puts the earlier
  state back. A forced stop kills, takes no lock and leaves the container
  stopped. Restart runs the check before it stops anything. Foreman starting
  or stopping never touches a running game: it finds the container by label
  and trusts an RCON answer (3.8).
- **Pre-start check and save selection (SPEC 7.4, 7.11).** The check returns
  problems that each say what the game would delete. "Start anyway" is
  offered only for missing or disabled mods, Space Age off with a Space Age
  save, and an unreadable save. A newer or unsupported save, no save, and a
  mod list that disagrees with the Space Age flag are plain refusals. A
  chosen save is made newest once, then the choice resets when the start
  reaches InGame. Built-in mods follow the game version.
- **Log and crashes (SPEC 8.6, 8.7, 8.9, 14.8).** The log is followed with
  Docker's timestamps and resumes by them. Each line is classified before
  it is redacted, so a short numeric game password cannot hide what a line
  is. Then the RCON and game passwords are removed and a printed `/config
  set password` is masked. The last 1,000 lines are kept. A crash is seen
  from the restart count, start time and the failed-load line, never the
  exit code. Three crashes in ten minutes make a crash loop, and Foreman
  stops the container, one stop at a time, retrying at each poll if Docker
  refuses.
- **Players and console (SPEC 8.1, 8.4).** RCON's `/players online` decides
  who is on, and join times come from `[JOIN]` log lines since the container
  started. A console command is 1 to 1,000 characters on one line with no
  NUL, and is audited before it is sent. `/config set password` and the
  reply to `/config get password` are masked, known secrets are removed from
  audit text, and Lua commands are recorded as typed.
- **Running-world snapshot and restore (SPEC 6.5, 6.10).** A snapshot of a
  running world runs `/server-save` and waits for it. A restore of a running
  world stops it gracefully, snapshots, restores and starts it again.
- **Server page and Console tab (SPEC 7, 8.1, 8.4, 8.6, 16.3, 17).** The
  Server page is where setup and sign-in land. It shows the state badge,
  uptime, version, save, CPU and memory, players with time online, step
  lists, and start, stop, restart (each with a countdown form), forced stop,
  "Start anyway" and "Download game image" (audited, and the only way an
  image is pulled). Live updates use server-sent events: heartbeats check
  the session without extending it, streams for header-signed users
  reconnect every 5 minutes, and a connection down for 5 seconds shows a
  note. Failures and finished restores are announced, focus survives
  updates, and steps that never run read "Skipped". The Console tab shows
  the log and the console, works without JavaScript, follows only at the
  bottom and offers "Jump to newest". Every page state passes axe at desktop
  and 320 px in both themes.
- **Test tools.** `internal/fakegame` and `cmd/fakegame` (a fake Docker
  socket plus a fake game with RCON, replaying real transcripts, with hooks
  for crashes, joins, a hanging save and a missing image) back `make e2e`.
  `make test-game` runs the controller against the three pinned images.
  `make e2e-game` runs the phone scenario against the real 2.1 image.
  `internal/game/testdata` holds real transcripts for all three versions.

### Verified

- `make verify` is green: lint with 0 issues, race tests, coverage about
  89.8% against the 70% floor, and the end-to-end type check.
- `make e2e`: 308 tests pass in four projects (desktop and 320 px, dark and
  light), including axe on each page state.
- `make test-game` passed against 1.1.110, 2.0.77 and 2.1.24 (about 70
  seconds for all three), and against 2.1.24 again after the fix rounds.
  `make e2e-game` passed against 2.1.24.
- Code, security and accessibility reviews ran over the epic head, and
  three fix rounds closed their findings.
- **Real-game check.** On 2026-11-03 Todd joined from a Factorio 2.1.24
  client: setup, start to Running, join, the player listed with time
  online, a console command, leave and rejoin, and a stop with a 30-second
  countdown whose warnings showed in chat at 30 and 10 seconds, then the
  save and "Goodbye". The real `[JOIN]` and `[LEAVE]` lines matched the
  documented format exactly and are now test data
  (`internal/game/testdata/2.1.24/join-leave.log`). The RCON password was in
  neither the game's log nor Foreman's, and the audit rows for start,
  console and stop were right.

### Gaps left

- While the game runs, `mods/mod-list.json` is mode 0646, because the game
  rewrites it that way, so any local user on the host can edit it. Every
  Foreman start re-checks it. Low risk, host-local.
- The RCON password is in the game's process arguments, because Factorio
  takes it only on the command line (SPEC 4.5). The operator guide in Epic 10
  must give the `hidepid=2` advice.
- Snapshot and restore are refused while Docker cannot be reached, because
  Foreman cannot tell whether the game is running.
- The check that nothing else shares the game network runs at start only,
  so a container that joins later is not noticed until the next start.
- A socket proxy that also publishes a host port cannot be seen from inside
  Foreman, so the operator guide must say never to publish it.
- A runtime that creates neither `/.dockerenv` nor `/run/.containerenv`,
  used with a `unix://` socket, gives Foreman no sign that it is in a
  container, so none of the network checks run.
- A game password set through the console is masked by pattern only. The
  log redaction does not learn it, so a later line that prints it is not
  masked. A secret typed inside a Lua command is not hidden either.
- The console history (the last 20 commands per world) is in memory only.
  The log history is the container's log and is lost when the container is
  recreated at the next start.
- The "Jump to newest" row keeps its space while the button is hidden, which
  leaves a blank strip under the log.
- A stop's time on a large map is unmeasured. On the small test map
  `docker stop` took under a second; the 120-second timeout is a generous
  guess for big worlds.
- No Space Age world (flag on) was started against a real game. The save
  reader reads real Space Age saves, and the real check ran 2.1.24 with
  Space Age off.
- There is no audit viewer, filter or retention yet (Epic 10), and audit
  rows have no token name (Epic 9).
- Crash reasons are not named; a crash says "unknown" and shows the last 20
  lines (Epic 8). A crash loop is not remembered across a Foreman restart:
  the game stays stopped and shows as stopped.
- Nothing builds an RCON command from user text yet, so there is no
  escaping helper for SPEC 14.9. It arrives with chat in Epic 8.
- The save reader skips six bytes of the 2.x header whose meaning is
  unknown. A save whose layout differs there is refused, not misread.
- The game port, 34197, is fixed. A per-world port is an additive
  `world.json` field for later (SPEC 5.3).
- The fakes differ from Docker: the fake game's restart delay stops at 3
  seconds (Docker's reaches a minute), the `on-failure` restart policy is
  not modelled, and a kill with a chosen signal writes no shutdown log.
- The game network refusals have Go tests but no end-to-end test, because
  the fake game runs outside a container. The test for a dropped live
  connection routes the stream to a 503; it does not stop Foreman.
- A form post with neither an Origin nor a Sec-Fetch-Site header is
  accepted, by design of Go's cross-origin protection.
- `GET /events` stays on the SPEC 14.3 list but is not wrapped or used;
  Foreman polls (ADR 0003).

## Epic 4: World switching

Serves SPEC 18.4: switching worlds (7.9, 7.10) and removing adopted mode.
Adopted mode was built first, then dropped before release (ADR 0005), so
Foreman now has one mode: it creates and owns the game container. Game
versions are now exact patch tags (SPEC 4.2). Everything below is built and
tested. Coverage across `internal/` is about 90%.

### Delivered

- **Switching (SPEC 7.9).** A non-active world's page offers "Switch to
  this world". Everything that would refuse the new world's start (the
  pre-start check, a missing image, the game network, the container
  checks) runs before the old world is stopped. The steps are: graceful
  stop, "Before switch" snapshots of both worlds, the pre-start check,
  make the new world active, then start it and confirm it runs, only if
  the old world was running. A switch leaves the game running or stopped
  as it found it. The operation holds both worlds, both pages show its
  step list, and a forced stop on either ends it. After a failure the page
  offers "Switch back", or says nothing was switched. The old world's open
  page reloads onto the Server page when the switch ends and speaks how it
  ended. Start and Restart of a world that is not active are refused. A
  switch is audited as `switch` only after its checks pass; a refusal is
  audited as `switch-refused`.
- **Switch record and recovery (SPEC 7.10, 3.8).** The switch is recorded
  in the app database (table `world_switches`, migration 0004) before its
  first step, then each step as it completes, and the row is removed when
  it ends. At startup an unfinished switch becomes a notice on the active
  world, with Start or Switch back offered. Recovery never starts or stops
  a game, and its faults are logged while pages keep serving.
- **The refusal box is the confirmation (SPEC 7.4, 7.9).** When a start,
  restart or switch is refused and a choice exists, the Warning on the
  Server page lists the problems and the steps: the "Before start anyway"
  snapshot (for a switch, the "Before switch" snapshots of both worlds),
  what the game drops, and how to undo. Its danger button posts the action
  at once. There is no separate anyway page. Restart anyway and a switch
  anyway of a running game use the default 60-second countdown, and the
  box says so. A step the check refused reads "Refused": warning style
  with a choice, error style without. After a crash loop or a plain
  refusal, a note says why a start would not run and the primary button
  becomes "Try to start again" (SPEC 8.9). The newest message leads; an
  older one is marked "Earlier, at <time>".
- **Pre-start check (SPEC 7.4).** Mods made for another Factorio than the
  world's pinned version are refused before anything stops, using
  `factorio_version` from each enabled mod's zip. Missing mod files and
  mods that are present but turned off are two separate problems, with
  plain one-line texts. A refused start, restart or switch is audited
  (`start-refused`, `restart-refused`, `switch-refused`) with its reason.
- **Crash reason (SPEC 8.7).** A game that stops while loading gives the
  game's own reason, read from the run's log, trimmed and masked with the
  world's secrets. The Server page shows that reason (or says it is
  unknown) and links to the Console, not raw log lines.
- **Managed only (ADR 0005).** The settings `FOREMAN_MODE`,
  `FOREMAN_GAME_CONTAINER`, `FOREMAN_GAME_DIR` and `FOREMAN_RCON_PASSWORD`
  are no longer read, and a leftover one changes nothing. The adopted
  client, game folder, adoption, adopted checks and pages, the `gamedir`
  package, and the `adoption` table are gone. The SPEC 4.4 network checks
  and the trusted-header refusal for game ranges (12.5, 12.6) stay.
- **Game versions by tag (SPEC 4.2, ADR 0005).** A world's version is an
  exact patch release of the 1.1, 2.0 or 2.1 line, and its image is
  `factoriotools/factorio:<version>`. The three game images pinned in the
  Makefile are test images only.
- **VPN sidecars (SPEC 4.13, ADR 0006).** The game can share the network
  of an operator-run sidecar container. A start is refused when the
  sidecar is missing, stopped, on the host network, or off the game
  network. A sidecar's ranges count as game ranges for trusted-header
  sign-in.
- **Import (SPEC 5.5).** The version and Space Age flag are read from the
  newest save and prefill the form; an empty version field means "use the
  save's version". Names are suggested from the chosen folder or file
  name and never overwrite typed text. A single importable folder is
  picked for you. A folder with no save at its top level is listed as not
  importable, with its radio disabled.
- **Test tools.** The fake game and fake Docker no longer model an adopted
  container.

### Verified

- Checks on 58507de: `make verify`, `make e2e`, `make test-game` (1.1.110,
  2.0.77, 2.1.24). Results are to be confirmed by Todd.
- Switch invariant tests (`internal/game/switch_invariants_test.go`): a
  crash before and after every record write, a cancel at every point,
  injected failures, many kinds of refusal, a random walk over three
  worlds, and recovery. Each ends with one active world, no save lost or
  changed, and a way back that works.
- `make e2e` covers switch, switch back, the refusal box in each state,
  import and the axe scan, at desktop and 320 px, in dark and light.
- `make e2e-game`: the phone scenario switches A to B and back to A
  against the real 2.1 image with every save kept.
- Code, security, accessibility and design reviews ran over the epic, and
  fix rounds closed their findings.

### Gaps left

- No game client joined in Epic 4's checks, so the in-game stop warning on
  a switch and live updates through a real proxy are untested. Todd joins
  at Gate A.
- Restart runs the pre-start check twice.
- Pass-1 fix 10 (the dropped report of a superseded failed switch, and the
  "Earlier" marking) is covered by unit tests only, with no end-to-end
  test.
- The start-anyway fingerprint leaves out the save name. This is an
  accepted trade-off.
- Restart anyway and a switch anyway use the default countdown, with no
  field to change it.
- "Last played" shows the import time for a world that was never played.
- `mods/mod-list.json` differs in every snapshot, because the game and the
  image's entrypoint rewrite it at each start.
- On a world's first start the image writes its example config files into
  the world, after the switch snapshot was taken.
- The server keeps no limit on a world name's length; only the form's
  `maxlength` (100) does.
- A running switch is silent to screen readers until it ends (DESIGN 7.6).
- With Foreman outside a container, a new game's range is recorded only
  right after its container starts, so for that moment the header range
  guard does not cover it.
- The fake game does not model `chown`, `LOAD_LATEST_SAVE=false`,
  `GENERATE_NEW_SAVE` or `UPDATE_MODS_ON_START`.
- The header refusal for a peer in a game range has Go tests but no
  end-to-end test, because a browser cannot connect from a game address.
- The mod-version check and the crash-reason reader are not checked
  against a real container's log.
