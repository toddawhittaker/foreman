# Status

What each epic delivered and the gaps it left. The epic's last task
(the fold) writes its section once, from the task pull requests. Gaps
here are known and accepted; each is either fixed by a later epic or
waits for a decision.

Epics 6 to 10 were renumbered on 2026-11-05 when Languages became Epic 6
(old 6 Mods is now 8, old 8 is 9, old 9 is 10, old 10 is 11). Older text
below uses the new numbers.

Epics 1 to 8, the Gate A fixes and the fix batches after Gate A (hub
fixes, polish fixes, UI batch, fixes batch 2) are done. Gate A (SPEC 19.1)
passed on 2026-11-05. Gate B (SPEC 19.2) follows Epic 8.

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
  in Epic 11 must say so.

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
  folder's save zips and its `mods/` folder (the Gate A fixes below added
  `saves/` and `config/`), or by uploading one save zip. Imports copy, keep times, never hardlink and never change the
  source. A new world is built in one staging folder and renamed into
  place, so a refused or failed import leaves no world behind. Mod sync
  for an uploaded save is Epic 8.
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
  takes it only on the command line (SPEC 4.5). The operator guide in Epic 11
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
- There is no audit viewer, filter or retention yet (Epic 11), and audit
  rows have no token name (Epic 10).
- Crash reasons are not named; a crash says "unknown" and shows the last 20
  lines (Epic 9). A crash loop is not remembered across a Foreman restart:
  the game stays stopped and shows as stopped.
- Nothing builds an RCON command from user text yet, so there is no
  escaping helper for SPEC 14.9. It arrives with chat in Epic 9.
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
versions are now recorded as exact patch releases (SPEC 4.2); running each
world on its own exact tag came in Epic 5. Everything below is built and
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
  exact patch release of the 1.1, 2.0 or 2.1 line. In this epic the image
  was still the newest pinned image of the world's line, so a world made on
  2.0.72 ran 2.0.77; Epic 5 made each world run its own exact tag. The
  three game images pinned in the Makefile are test images only.
- **VPN sidecars (SPEC 4.13, ADR 0006): not built.** An earlier version of
  this section said they shipped. They did not: nothing reads
  `FOREMAN_GAME_NETWORK_CONTAINER`. The Gate A fixes below make Foreman
  refuse to start when it is set. Sidecars are now planned for Epic 9.
- **Import (SPEC 5.5).** The version and Space Age flag are read from the
  newest save and prefill the form; an empty version field means "use the
  save's version". Names are suggested from the chosen folder or file
  name and never overwrite typed text. A single importable folder is
  picked for you. A folder with no save is listed as not importable, with
  its radio disabled.
- **Test tools.** The fake game and fake Docker no longer model an adopted
  container.

### Verified

- Checks on 58507de, all green: `make verify` (coverage 90.7%), `make e2e`
  (368 passed), and `make test-game` (33 cases across 1.1.110, 2.0.77 and
  2.1.24).
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
- Restart anyway uses the default countdown, with no field to change it.
  (A switch's confirmation page has the field; see Hub fixes.)
- "Last played" shows the import time for a world that was never played.
- `mods/mod-list.json` differs in every snapshot, because the game and the
  image's entrypoint rewrite it at each start.
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

## Epic 5: Game versions

Serves SPEC 18.5: section 10 (10.1 to 10.5, 10.7, 10.8). VPN sidecars
(4.13) were planned here and moved to Epic 9. Everything below is built
and tested. Epic 4's
STATUS had said 10.1 was delivered; it was not until this epic.

### Delivered

- **Each world runs its exact version (SPEC 10.1, 4.2, ADR 0005).** The
  image is `factoriotools/factorio:<game_version>`, so a world made on
  2.0.72 runs 2.0.72. `Config.Images`, the image-version check and
  `ErrSaveNewerThanImage` are gone; the pre-start check (SPEC 7.4) refuses
  a save newer than the world's version, with no "Start anyway".
- **Import accepts only supported lines (SPEC 5.5, 10.8).** Folder and
  upload imports, and the import form, refuse a version that is not an
  exact patch release of 1.1, 2.0 or 2.1. The mod-version advice never
  says to lower a world's version.
- **Releases client (SPEC 10.2, 10.7, 14.10).** The new package
  `internal/releases` reads Docker Hub's tag list (one query per supported
  line, at most 10 pages of 100) and factorio.com's latest releases
  (stable and experimental `headless`). It follows no redirects, caps
  replies at 2 MB and 64 KB, allows 10 seconds per call, caches for 5
  minutes in memory and makes one fetch per call kind at a time. Errors are
  not cached. A failure is `ErrUnreachable`. If one latest release is on a
  line Foreman does not support (for example experimental 2.2.0), only
  that field is empty; the other still shows. It makes no request at
  startup or when a page is rendered without the picker or notice.
- **Update (SPEC 10.3, 7.4, 3.7).** The active world can be updated to a
  newer exact release. Everything that can refuse runs first, before any
  download or stop: the target is a newer supported version, the world is
  active, the game is in a state a stop would accept, the pre-start check
  at the target version and the game network. Then the image
  is downloaded if Docker lacks it (a visible step), the game stops
  gracefully, a "Before update" snapshot is taken, the version is set, and
  the game starts and is confirmed only if it was running. A stopped world
  stays stopped. A refusal that has a choice shows the refusal box with
  "Update anyway". If the update was set but the start was refused, the box
  offers "Start anyway" instead. Audited as `game-update` and
  `game-update-refused`.
- **Rollback (SPEC 10.4, 6.6).** One step back: it restores the newest
  "Before update" snapshot whose version is older than the world's, with
  the version and the save together, and takes a "Before restore" snapshot
  first as the undo. The page warns that play since the update is lost.
  With nothing to roll back to, the page shows only the error. A missing
  old image refuses the start (SPEC 3.7) and loses nothing. Pruning never
  removes a world's newest `game-update` snapshot. Audited as `rollback`.
- **Version picker (SPEC 10.7).** The import form and the Update page
  have a dropdown of every newer release, filled after the page loads,
  newest first, with stable and experimental marked once. It leaves out
  rootless, `stable-` and alias tags. Under it, "Or type a version" wins
  when filled. When Docker Hub cannot be reached, the dropdown says so and
  the typed field is the way in; typing also works without JavaScript. A
  typed version must be an exact patch release of a supported line; its
  existence is not checked against Docker Hub. The Update page says a
  version only moves forward and the only way back is Roll back. (#113,
  after Todd's test pass.)
- **Snapshots tab and delete (SPEC 6.12, added in this epic).** A
  Snapshots item in the main menu lists every world's snapshots, deleted
  worlds included, with size, what deleting would free, and the total disk
  used, counting hard-linked files once. Delete, one at a time, from the
  tab or a world's Snapshots page, after a confirmation that names the
  snapshot and the space it frees, and warns when Rollback depends on it.
  Audited as `snapshot-delete` before anything is removed; refused while an
  operation runs on the world. (#114)
- **No layout shift between pages.** Every page keeps room for the
  scrollbar (`scrollbar-gutter: stable`), so short and tall pages line up.
  (#112)
- **Release notice (SPEC 10.2).** The world page shows, once it has
  loaded, a newer stable or experimental release with a link to the wiki's
  Version history page for its line, and a link to the Update page. A
  release with no image yet reads "No image yet", with no button. The
  notice says Docker Hub cannot be reached only when the call it needs
  fails. Only a notice that reports a newer release is a live region.
- **Real-game update tests (SPEC 10.3, 10.4, 9.7).** `make test-game`
  runs 2.0.77 to 2.1.24 and 2.1.23 to 2.1.24 against real containers. Each
  reaches "InGame" on the new version, rolls back to "InGame" on the old
  one, and the restored save is byte for byte the "Before update" save. The
  new game logs no error and loads 2.1's built-in mods (`recycler`) even
  though the old `mod-list.json` lacks them.
- **Test image checks (STACK 1, 13).** `GAME_IMAGE_2_1_PREV` pins 2.1.23.
  `make test-game` and `make e2e-game` check that each plain tag they use
  on the host is the pinned digest, and refuse with a `docker pull` and
  `docker tag` hint if not. `make test-game TAG=` checks only the images
  that tag runs. End-to-end runs set an unreachable `HTTPS_PROXY`, so
  Foreman cannot reach Docker Hub from them.

### Verified

Checks on 7e4c15d, all green: `make verify` (coverage 90.3%), `make e2e`
(420 passed), `make test-game` (1.1.110, 2.0.77, 2.1.23 and 2.1.24, with
both update and rollback cases), and `make e2e-game` (2 passed).

- Invariant tests in `internal/world` and `internal/game`
  (`update_test.go`): a version only moves up; a refused update changes
  nothing and takes no snapshot; a download happens before the stop and a
  failed download leaves the game running; rollback goes back one update at
  a time; pruning keeps the newest update snapshot.
- Releases client tests use `httptest` fakes: filtering and ordering,
  paging limits, a `next` URL pointing elsewhere, redirects, size limits,
  error text, caching and single-flight.
- Web tests cover session, CSRF and method rules on every new route, the
  picker, the notice and hostile text from the release source.
- `make e2e` covers update by typed version (stopped and running worlds),
  the Docker Hub notice, "Update anyway", rollback, and the axe scan at
  desktop and 320 px in dark and light.
- Code, security and accessibility reviews ran over the epic, and fix
  rounds closed their findings except AR4 below.

### Gaps left

- The newer-release and "No image yet" notice states get no axe scan,
  because end-to-end tests block Docker Hub (accessibility finding AR4).
  The Go tests cover them, and the markup is shared with scanned pages.
- End-to-end tests never see the picker list filled, for the same reason.
  Go tests cover the fragment.
- `TestReattachAfterForemanRestart` is flaky (issue #109).
- Only the active world can be updated; another world says "Switch to this
  world to update it" (issue #93).
- The changelog is a link to the wiki, not shown inside Foreman (issue
  #92).
- The "Update anyway" fingerprint leaves out the target version. This is an
  accepted trade-off.
- The supported lines are listed in `internal/releases` as well as in
  `world.SupportedGameVersion`; a new line needs both.
- Not tested: `http.ProxyFromEnvironment` and the real 10-second deadline
  in the releases client.
- Not covered by the real-game test: a 2.0 world given a `recycler` mod
  entry by hand, and mods that break across a line.
- The end-to-end tests assert the update step list and the refusal box
  wording loosely.

## Gate A fixes

A fix batch from the architect's Gate A review. It serves SPEC 5.1, 5.5,
4.5, 4.13, 8.3, 12.5, 12.6 and 14.8.

### Delivered

- **Trusted-header groups (SPEC 12.5).** Sign-in reads Authentik's
  `|`-separated groups as well as comma lists. Parts are trimmed, empty
  parts are dropped, and matching stays exact. A required group that
  contains `|` or `,` is refused at startup when headers sign-in is on.
- **Import reads the server layout (SPEC 5.1, 5.5).** Save zips come from
  the folder's top level and from `saves/`. Mods are copied, and so are
  the six `config/` files SPEC 5.1 lists, never `rconpw` (Foreman makes
  its own). A save name in both places makes the folder not importable;
  the listing and the submit-time refusal name the file and copy nothing.
  The inbox save count includes `saves/`. Symlinks are not followed, the
  source is never changed, and no config content is logged or shown.
- **Admin, ban and white lists reach the game (SPEC 4.3).** The start
  script always passes `--server-adminlist` and `--server-banlist`, so
  `/promote` and `/ban` made in game persist; it passes
  `--server-whitelist` only when the file exists. The six config file
  names are one set of constants shared by import and the start script.
- **Config files are 0640 (SPEC 3.6, 14.8).** Every file copied into
  `config/` by import, snapshot or restore is written at 0640, from one
  helper. Saves and mods keep 0664.
- **VPN sidecar refusal (SPEC 4.13).** Setting
  `FOREMAN_GAME_NETWORK_CONTAINER` stops Foreman at startup with a plain
  message, because the sidecar is not built. An empty value counts as
  unset.
- **README example compose (SPEC 14.11, 4.4, 12.6).** Foreman runs
  read-only with a `/tmp` tmpfs, no capabilities and `no-new-privileges`,
  and waits for the socket proxy. A snippet shows the reverse proxy joining
  `foreman-front` at a fixed address. The README import section describes
  the server layout. The compose was run for real: Foreman healthy, a
  server-layout folder imported on 2.1.24, the game reached InGame, and the
  imported settings file was applied.

### Verified

- `make verify`, `make e2e` (428 tests, including the import layout and
  clash specs) and `make test-game TAG=2.1`.
- A script test runs the real start script against a fake world folder.
- The README compose was brought up for real under a scratch project.

### Gaps left

- Whitelist enforcement waits for the game settings in SPEC section 11:
  `--use-server-whitelist` is never passed.
- In-game `/promote` and `/ban` persistence is not tested with a game
  client. Only the start command and a real start without the files were
  checked.
- VPN sidecars (SPEC 4.13, ADR 0006) are not built; they are planned for
  Epic 9.

## Gate A

Gate A (SPEC 19.1) passed on 2026-11-05. Todd ran it on a trial stack on
his development machine, built from `fe4b814`.

### What was checked

- **Behind Caddy and Authentik with trusted-header sign-in.** Caddy
  removes any `X-Authentik-*` headers the client sends, asks an Authentik
  proxy outpost (forward auth), and Foreman trusts the headers only from
  Caddy's fixed address. Todd signed in through Authentik. With the
  required group set to one he is not in, Foreman refused him. Requests
  with no sign-in, and requests with forged headers, were both sent to
  Authentik.
- **The hand-renamed folders imported as worlds.** All four imported
  (`current` in the server layout, `saves-2026` with 45 mods,
  `saves-10-2026`, `dans-achievement-saves`).
- **Each on the version it was made with.** `current` ran on 2.1.22,
  `dans-achievement-saves` on 2.0.76 and `saves-10-2026` on 2.1.9 (then
  updated to 2.1.24, which also worked). `saves-2026` was updated from
  2.0.66 to 2.0.77 before its first run, because the Steam client offers
  no 2.0.66; Todd accepted this, since the limit was the client and the
  other three worlds showed per-version running on both 2.0 and 2.1.
- **Switching with no save lost.** Todd switched between worlds several
  times, with the game running and stopped, from a desktop browser, and
  joined the worlds from a game client. Foreman opened and worked on his
  phone once the phone's browser was allowed to reach the local network
  (an Android permission, not a Foreman issue). From the phone he then
  started `saves-2026` and, with it running, switched back to `current`,
  which closed the round trip begun from the desktop (`current` to
  `dans-achievement-saves` to `saves-10-2026` to `saves-2026`).
- Foreman downloaded a game image through the socket proxy (`POST`
  allowed), and live page updates worked through Caddy.

### Not done

- OFSM is not retired. Todd keeps it, unused, until Foreman is deployed
  on his real host; the development machine is not Foreman's permanent
  home.

### Found during the gate

Filed as issues, not fixed: #124 (bulk import with checkboxes), #125 (say
a game image is missing before Start), #126 (clearer game network
refusal), #127 (list already imported folders separately), #128 (switch
in one confirmation, including the download), #129 (a switch refused for
a missing image offers the wrong world's download, so it cannot
succeed), #130 (download the image at import), #131 (warn about mod
versions players cannot get from the portal), #132 (public page for
players to download the running world's mods; needs a vision change).
#125, #128, #129 and #130 are to be built as one batch.

## Hub fixes

The first of two fix batches after Gate A, before i18n (#87). It makes the
Server page the hub for the active world and fixes the switch and stop
flows. It serves SPEC 3.7, 7.3, 7.6, 7.9, 14.10 and 17.7 to 17.10.

### Delivered

- **A switch is one confirmation (#128, #129, #145, #136).** One rule set
  in the controller (`PlanSwitch`) runs every check that could refuse a
  switch. The confirmation page and the switch both call it, so they
  cannot disagree. A missing game image is downloaded first, before the
  switch record and before the old game stops; a failed download leaves
  the old world running. A stopped game's switch starts the new world when
  "Start after switching" is ticked, which is the default. When the new
  world has problems, the page lists them and offers "Switch and start
  anyway" and "Switch, leave stopped"; leave stopped skips the check, and
  the problems show at the next Start. The confirmation page asks who is
  online and what the image will cost to download (its size, from Docker
  Hub, when the image is missing). A switch result never offers the wrong
  world's download (#129), and after a refused start the button reads
  "Start <world>" and says that world is still active (#136). The
  missing-mod advice names the world's own mods folder on the host (#145).
  This closes Gate A review finding S3 (two copies of the refusal rules).
- **The switch is refused if things changed (SPEC 17.8).** The
  confirmation page posts whether the game was running and which problems
  it showed. If either differs at confirm time, nothing is done and the
  page comes back saying what changed.
- **Missing image shown before Start (#125).** The Server page says so
  and names the image; the download replaces Start, and afterwards says
  "Downloaded <image>; <world> is ready to start." A world's own page says
  it too. A failed download names the image, says whether Docker Hub has no
  such version, and logs the cause.
- **Start refused (#142, one point).** A start the check refused is
  titled "Start refused" and lists only the step that ran.
- **Stop can be cancelled (#143).** While players are warned, the Server
  page counts the time left down and keeps the list of who was online when
  the countdown began. "Cancel stop" ends the whole action (stop, restart,
  switch, restore or update) with nothing changed; a downloaded image
  stays. The action is audited as `stop-cancel`, and warned players get a
  chat line. The seconds field is the only way to skip the warning (0
  seconds); the "No countdown" checkbox is gone. The stop, restart,
  switch and restore forms say who is online, and force stop names who
  will be disconnected. The countdown text updates itself in the browser
  (`countdown.js`), so the buttons beside it are never replaced and a slow
  tap is not lost.
- **Results offer their undo (#140, #152).** Every Done message has its
  undo as a button: "Undo this restore", "Undo this rollback", "Roll back
  to X", "Switch back to A". It also says when it finished, and the next
  action clears it. A failed restore links straight to restoring "Before
  restore".
- **Crash loop recovery (#147).** One box offers restoring the newest
  "Before start anyway" snapshot (only while it is the newest) and
  "Switch back to A" while Foreman holds the switch result. After a
  Foreman restart only the restore is offered, and no schema changed.
- **Status is honest (#137).** During a switch the status reads "Switching:
  From A to B" and the active-world link moves to the new world once it is
  presented. After the switch, and whenever the state is Unknown, uptime,
  save, CPU, memory and the running version are left out instead of shown
  stale. The players panel says Foreman cannot see who is online. An
  unreachable Docker is one plain sentence naming `DOCKER_HOST`, with the
  raw error logged once per fault (#144); another Docker fault on a
  container also reads as a plain sentence.
- **Switching and snapshots from the Server page (#141).** A panel lists
  every other world with its own "Switch to X" button, and "Take snapshot"
  is one click. The Worlds list has a "Switch" button per world, named
  "Switch to <world>" for screen readers, and a world's page leads with
  Switch. A running switch shows on the list and on both of its worlds'
  pages. Restoring the stopped active world runs in the background and
  lands on the Server page.
- **Worlds pages (#146, #138, #154 in part).** A world with an operation
  running shows why it cannot be deleted, before any name is typed. Rename
  lands on the world's page with "Done: Renamed to X."
- **Network refusal (#126).** It says why sharing is refused, names each
  other container with what Foreman can tell, and gives the fix first
  (`docker rm` for a stopped one, `docker network disconnect` for a running
  one, never remove advice for a running one, then
  `FOREMAN_GAME_NETWORK`).
- **Phone layout (#142, #153, #155 in part).** A two-row header (about 112
  px tall at 320 px, down from 218). "Start and stop" and its steps come
  before Status and Players, so Stop and Cancel stop are on the first
  screen. Steps are one row each, and at 200% text at 320 px the step name
  gets its own line. Force stop is a plain link. The console field comes
  before the log. The Worlds list is compact, with Switch beside the
  name.
- **Click budgets (#157).** One end-to-end test per SPEC 17.10 budget
  counts real clicks and fails over budget. Every budget is met: switch
  with a download takes 2 clicks (budget 3) and Start anyway takes 1
  (budget 2).
- **Flaky test (#109).** `TestReattachAfterForemanRestart` polls for its
  log lines instead of racing them.
- **Server page wiring.** After the admin's own Cancel stop or Take
  snapshot, focus moves to the Done message, also when that message arrives
  by a live update. The "Switch to another world" panel is live: it is
  empty while an action runs and comes back when the action ends, with no
  reload. The "Docker Hub has no image with that version" message links to
  the world's page (the target world, for a switch). A cancelled stop's
  message no longer returns after a later snapshot or download. A changed
  switch confirmation keeps the admin's "Switch, leave stopped" choice. At
  320 px with 200% text the header keeps Sign out on one line, with the
  name above it.

### Verified

- `make verify` and `make e2e` green at the last task (568 passed, 4
  skipped, across desktop and phone, dark and light).
- New Go tests for the plan, cancel, wording and size lookup; new end-to-end
  specs for the switch flow, the worlds pages, the Server page hub, phone
  layout, accessibility fixes and click budgets. The accessibility tests
  failed on the old sources.
- Code, accessibility and design reviews ran over the batch, and fix
  rounds closed their findings.
- The security reviewer checked the host mods path shown on the switch and
  Server pages and the Docker Hub size call (SPEC 14.10) on `bc071c1`. It
  found nothing high or medium. Its one low finding, raw Docker text on the
  Server page, was fixed.
- Against real `factoriotools/factorio` containers on `bc071c1`:
  `make test-game` for all tags (48 pass: 1.1, 2.0 and 2.1, including the
  update across lines 2.0.77 to 2.1.24 and within a line 2.1.23 to
  2.1.24) and `make e2e-game` (2 pass).
- A real switch that downloaded `factoriotools/factorio:2.0.73` (about
  135.5 MB, shown on the confirmation page). Docker events show the pull
  finished before the old world stopped. The old world saved over RCON and
  exited cleanly, and the new world reached InGame with its save. Both
  "Before switch" snapshots were taken and no save was lost.
- A real switch to a version with no image (2.0.74) failed at the download
  and left the old world running, with nothing saved or snapshotted.

### Gaps left

- The stop countdown's player list is a snapshot from its start, not
  live.
- The update and rollback forms do not name who is online.
- The Worlds list does not show the game's state beside "Active".
- The crash-loop restore offers the newest "Before start anyway" snapshot
  whatever its age; the age is shown on the button.
- The step list has no "Step N of M" summary line (it needs the step
  number from Go), and finished steps are not collapsed, because the list
  is redrawn live and a collapsed part would reopen on every update.
- The countdown uses the browser's clock, so a phone with a wrong clock
  shows a wrong time left; the stop itself runs on the server's clock.
- The Server page's own problems box appears only when problems arise
  after the confirmation.
- The world page's inline "See the Server page" link is under 44 px
  tall on a phone.
- Two things have unit tests but no end-to-end test: figures left from a
  run before Docker became unreachable, and the active-world line moving
  mid-switch. The changed-state refusal is covered by an integration test
  only.
- A screen reader may hear both the announcement and the focused Done
  message.
- After a switch, the old world's Server page briefly lists that world in
  the switch panel until the page reloads.
- The cancel-then-snapshot integration test polls, so a very quick snapshot
  weakens it. A deterministic test of the runs tracker covers the same
  rule.
- The mod-change undo click-budget test is skipped until Epic 8.
- Cancel stop was not tried with a real player online, because no game
  client can join a headless server.
- Issues #154 and #155 were only partly done here; the polish fixes batch
  finished them.

## Polish fixes

The second of two fix batches after Gate A, before i18n (#87). It finishes
the Import page, the snapshot pages, sign-in and setup, and local times,
and makes every end-to-end test fail on an undeclared Foreman error log
line. It serves SPEC 3.7, 5.1, 5.4, 5.5, 6.6, 6.12, 10.7, 12.3, 14.17 and
17.11 to 17.12, and DESIGN 8.3 and 9. ADR 0007 records where an import is
remembered.

### Delivered

- **Import several folders at once (#124).** Folders are checkboxes. With
  one ticked, the name, version and Space Age fields apply. With several,
  each folder becomes a world named after the folder, with its save's
  version and Space Age setting, and the per-world fields are emptied,
  unticked and turned off, with a hint saying why. A folder whose save
  cannot be read, or that is refused, is skipped with its reason, and the
  Done list links each new world. A skipped folder that an import on its
  own can fix links to that ("Import X on its own"); for an unreadable
  save it asks for the version and focuses that field. Ticking back to one
  folder puts the typed version and choice back.
- **Already imported (#127).** `world.json` gains `imported_from` (folder,
  save, save_size, save_mtime to the nanosecond, at). A folder whose name
  and newest save's name, size and time match a record is listed apart,
  naming the world and the time, and can still be imported. The Import
  page reads only each world's `world.json` (ADR 0007).
- **Image download with the import (#130).** A box, ticked by default,
  downloads the game image the version needs if this host lacks it, after
  the import and once per missing version. It reuses the world's own image
  download, so it is audited and shown as before, and it needs no new
  Docker call. Folder rows say "game image not downloaded". The world
  page shows the download as running, or failed.
- **Other import fixes.** Space Age is refused before 2.0, in the form and
  in the import, and warned about when it differs from the save (#139).
  The page names the inbox's host path (#148). The empty version option
  reads "Same as the save (X)", the suggested name is the one in the
  folder's `server-settings.json` when readable, and unimportable folders
  are listed apart (#155). The world page says "Done: Imported X." and
  shows "Imported from" (#154). A version typed by hand must be a
  downloaded image or on Docker Hub's list; when Docker Hub cannot be
  reached the import goes ahead and the world page warns that the version
  could not be checked.
- **Snapshots: one counting pass (#150, #135).** One pass over snapshot
  files by inode gives what a selection frees, each world's figure and the
  total, so they agree. "Snapshots add X" replaces "All snapshots use" and
  "Snapshots use": the space held only by snapshot files that no world also
  holds. A snapshot's own `snapshot.json` counts, so figures are byte
  exact. The Worlds list shows "snapshots add X" under each world's Disk
  use, from the same pass; if the count fails it says "unknown" and logs
  one error.
- **Delete several snapshots (#134).** A checkbox per snapshot, "Delete
  selected", and "Select all automatic" (never manual or "Before delete"),
  on the Snapshots tab, with one button per world, and on each world's
  page. After the shortcut the page says how many it ticked and focuses
  that note. One confirmation page lists every snapshot, says what the
  whole selection frees, warns when one is the rollback point, and
  separates "These stay" (an operation is running) from "Already deleted".
  Foreman deletes one at a time through the single-delete path, with an
  audit entry per snapshot before removal; a partial failure keeps what
  was deleted and lists the rest, and a resubmitted page ends on Done.
- **Snapshots tab and tables (#151).** The tab is grouped under a heading
  per world, with a Restore link per row. Every snapshot table is measured
  against its panel at all four projects and in a wide-zone run. The
  overflow at 200% text on a 320 px phone predated this batch (the page
  was 574 px wide on `main`); it is fixed here by stacking each row below
  14rem. The tables carry explicit table, row and cell roles so the
  stacked layout keeps its semantics.
- **Sign-in return and setup (#154).** A signed-out visit goes to
  `/signin?next=<path>`, and after sign-in the admin lands there. Only a
  local path starting with one `/` is accepted (SPEC 14.17), checked when
  the page is shown and again on the post, and it survives a wrong
  password. A POST, an htmx request and `/` get none. Setup lists every
  field problem at once.
- **Local times (#149).** `time.js` shows each `time` element in the
  browser's zone with a short zone name and a relative time refreshed every
  minute. A time inside a control's spoken name gets the local time alone.
  Without JavaScript the UTC text stays. The snapshot Done message and
  Restore link names are `time` elements, so they follow suit.
- **End-to-end tests fail on an ERROR log line.** The fixture stops
  Foreman after each test and fails it on any ERROR line it did not
  declare with `foreman.expectError`, and on a declaration that matches no
  line. A committed spec tests the check, and a test also fails when
  Foreman exits early. A cancelled action at shutdown and a cancelled bulk
  import are no longer logged as errors.
- **Review fixes.** world.json is read once per list; the Space Age
  warning texts live only in Go; "Select all automatic" reads "ticked N
  snapshots"; a gone snapshot with no date shows its name.

### Verified

- `make verify` 90.9% coverage and `make e2e` 660 passed, 16 skipped, on
  the final epic head, with main merged in. Twelve
  of the skips are the ERROR-check self-test, which runs in one browser
  project only. Four are the click-budget mod-change test, which waits for
  Epic 8.
- New Go tests pin the figures against the disk:
  `TestFreesMatchesTheDiskForEverySelection` deletes every selection of
  three snapshots and compares the claimed "frees" with the real fall in
  disk use, to the byte. Others cover the `imported_from` record to the
  nanosecond, the return path rules, bulk import and bulk delete, and the
  Import page reading only `world.json`. New end-to-end specs cover import,
  snapshots and the tab, selection, times, sign-in return, the Worlds list
  figure and the ERROR check.
- Code and accessibility reviews ran over the batch. Their fix rounds were
  pull requests #181 (import review), #182 (snapshot figures, tick note,
  times, large text), #183 (the Import page reads only `world.json`; an
  undated gone snapshot is shown by name) and #184 (explicit table roles,
  many-folder version fields emptied, reworded tick note, version hint on
  import alone).
- The security reviewer checked the return path, the import folder paths,
  bulk delete and secrets. It found nothing.
- Against real `factoriotools/factorio` containers on `80c5f26`:
  `make test-game TAG=2.1` (24 passed) and `make e2e-game` (2 passed, no
  ERROR lines). A bulk import downloaded the missing 2.0.77 image, and a
  switch to that world reached InGame with its save. A hand-typed 2.0.999
  was refused. A bulk snapshot delete's "frees" matched the real drop in
  disk use except for `snapshot.json`, which the review fix round (#182)
  then counted. The active save was untouched, and the world started
  afterwards.

### Gaps left

- In a bulk import only the first world of each version shows the
  download on its page, because a download is tracked per world.
- The download checkbox shows whenever Foreman has server control, not
  only when the chosen version's image is missing.
- A world page does not update live during an image download, and its
  note is not announced to a screen reader (accessibility review LOW7).
  The note says to reload.
- The several-folders state on page load (fields emptied before any
  click) has no end-to-end test, because Playwright's reload does not
  restore ticks.
- A file held only by snapshots of two different worlds counts toward
  neither world's figure, so the Worlds list figures can add up to less
  than the Snapshots tab total. The list has no column total.
- There is no unit test for the bulk delete refusal while a game operation
  runs. The single delete has the same gap.
- The Deleted worlds page still shows UTC in its Restore link names, and
  setup does not carry a return path.
- The "Taken:" and "Size:" labels of stacked snapshot rows are English
  text in `foreman.css`; i18n (#87) must move them into templates.
- The restore page shows a snapshot's size without `snapshot.json`, while
  the lists include it, so they differ by a few hundred bytes.
- The explicit table roles were checked in Chromium only, not in Safari
  with VoiceOver.

## Epic 6: Languages

Serves SPEC 22 (issue #87), with SPEC 14.17 for the menu's return path and
17.1, 17.2 and 17.12 for layout and names in every language. ADR 0008
records the design of the catalogue. The plan's lasting rules are in
SPEC 22.

### Delivered

- **Eight languages.** English, German, Russian, Simplified Chinese,
  French, Spanish, Polish and Czech. Every page, form, error page,
  refusal, pre-start problem and failure text built in Go, and the text
  the page's scripts show, comes from the catalogue.
- **The `internal/i18n` package.** JSON catalogues embedded in the binary,
  in five areas (common, accounts, server, worlds, js), over
  `golang.org/x/text` v0.43.0 for matching, plural rules and number
  formats. Nothing downloads at startup. Templates are parsed once and
  copied per language. The binary grew by about 760 KB and the image is
  still about 8 MB compressed.
- **Language choice.** The best match for Accept-Language, read entry by
  entry (at most 32; a bad entry is skipped), overridden by the
  `foreman_lang` cookie that the menu on every page sets through
  `POST /language`. The menu works without JavaScript, returns through
  the return-path check, and after a form post returns to the page the
  form was on (from the Referer), else home. Off English its label adds
  "(Language)" in English.
- **Messages built in Go.** Page fields hold `i18n.Msg`, so a raw string
  cannot reach a page. `internal/game` and `internal/world` carry typed
  errors (`StartError`, `LoadReason`, `ForcedStopError`, `NotActiveError`,
  `NetworkError`, `NoSpaceError`, `DamagedError`, `UnsupportedFileError`,
  `SaveClashError`, and facts on `Problem`) and keep their English
  `Error()` text for logs and the API. `internal/web/gametext.go` words
  them in the page's language. A test pins that each English page text
  matches its log text.
- **Snapshot labels.** Automatic labels are shown from their stored reason
  code in the page's language; a typed label is shown as written. No
  sentence quotes a label, and a check fails if one does.
- **Game text stays English.** The log pane, console replies, crash
  reasons and English error details are marked `lang="en"`.
- **Scripts.** `import.js`, `live.js` and `countdown.js` read their text
  from an `#i18n` JSON block that `layout.html` renders. `time.js`
  formats times and relative times in the page's language. Times stay
  UTC without JavaScript.
- **Checks in `make verify` (SPEC 22.5).** Hard-coded text in templates,
  scripts and the stylesheet; missing, unused and misplaced keys;
  placeholders and plural forms against CLDR; HTML in a value; a stale
  translation (the `translated/<area>.json` hash, refreshed by
  `make i18n-hash`); a quoted snapshot label; and a rendered-page test
  that every link and button's accessible name contains its visible text
  in every language (label in name). Every page renders in all eight
  languages in a Go test.
- **Layout in every language.** No page scrolls sideways at 320 px with
  text at 200%. Single-column grids use `minmax(0, 1fr)`; tables stack
  below 14rem; long Worlds buttons drop under the name; prose hyphenates
  in the page's language. Stacked table labels come from `data-label`
  instead of CSS text, which closes the "Taken:" and "Size:" gap the
  polish fixes left. The player, saves and deleted-worlds tables carry
  explicit table roles.
- **Glossary and review.** docs/I18N.md holds the glossary (from Factorio
  2.1.24's own locale files where the game has a term), the address rule
  per language and the label tables. Two AI reviews of the translations
  were applied, and a second-pass check made French and Polish use one
  verb for taking a snapshot.
- **One English change.** Numbers follow each language's grouping, so the
  sign-in error reads "at most 1,024 bytes" (SPEC 22.4).
- **Fixes along the way.** An unknown or malformed Accept-Language entry no
  longer hides a later match. The language menu after a form post no
  longer returns to a POST-only address (405). `Link` refuses a path that
  is not Foreman's own, and the menu's return path refuses control
  characters.
- **Layout fixes after the first matrix.** The deleted-worlds tables now
  stack at large text. In a stacked cell a clock's zone (for example
  `GMT+5:45`) wraps onto its own line while the date and sizes never
  break, and stacked figures are start-aligned. The 320 px matrix now
  also covers the deleted-worlds page, restore, snapshot delete, bulk
  delete, rollback, three 422 error states, and a test in the
  Asia/Kathmandu zone.
- **Wide time zones.** `time.js` renders a date, a clock and a zone as
  three unbreakable units with normal spaces between them, so a wide zone
  such as `GMT+5:45` drops to its own line. The Asia/Kathmandu test
  covers the saves page and both snapshot pages in all eight languages at
  100% and 200% text. German and Russian snapshot tables were 345 and
  333 px wide before.

### Verified

- `make verify` passed on the epic head with coverage 90.7% across
  `internal/`. `make e2e`: 868 passed, 88 skipped (the 320 px language
  matrix runs in phone-dark only, plus the existing skips), none failed.
- New tests: Accept-Language cases and the cookie, the menu's return on
  every kind of page, plural forms checked against the browser's own CLDR
  data, decimal commas, a 320 px matrix at 100% and 200% text over every
  page in all eight languages with axe, a check that the log pane and
  console are `lang="en"`, German and Russian script output, and the
  catalogue checks above.
- Against real `factoriotools/factorio` containers, on the head after the
  server pages landed (4bfff98): `make test-game` passed on all four
  images and `make e2e-game` passed both tests, with no ERROR log line. A
  failed start, switch and update each named the right step and world in
  English and German, Factorio's text was marked `lang="en"`, and the
  English text of those five pages was identical to main before the epic.
- Code, security and accessibility reviews ran over the epic, and two
  translation reviews (de, fr, es, pl; ru, cs, zh-Hans) back-translated
  every value. Security found nothing. Every other finding was fixed and
  confirmed closed by a second code and accessibility pass and a second
  translation check, or is listed below.

### Gaps left

- Translations are AI-made and reviewed by a second AI pass, not by
  native speakers (SPEC 22.6). The glossary and address rules have not
  had a native review either. Corrections are welcome as issues.
- A game operation keeps its failure twice (`Operation.Error` text and
  `Operation.Err`), and `serverpage.go` picks the panel by comparing
  `Error` strings. This is deferred cleanup.
- The countdown warning sent into the game chat stays English on purpose,
  because players read it in the game.
- Docker's own state words (running, paused) stay English inside the
  shared-network refusal.
- The test helper `sentence()` ties some English page text to `Error()`
  text, so rewording a log message can fail a page test.
- Behind a proxy that rewrites the Host header, the language menu after a
  form post falls back to home, because the Referer's host no longer
  matches the request's.
- The plural forms for 5 and 21 are checked against the catalogue, not on
  a rendered page, because no page shows those counts yet.
- The 320 px language matrix checks each language in one browser project
  (two languages per project since Epic 7, so the CI shards stay even);
  colour does not change layout.
- `make test-game` binds host UDP 34197, so it cannot run on a host where
  a real Factorio server holds that port. The real-game check had to
  patch the port locally.

## Epic 7: Generating worlds and game settings

Serves SPEC 5.6 (Generate, with the map preview), 5.7 (Clone) and section
11 (game settings), with 4.3 (the create step), 5.1 (`new_map`), 6.7 (the
`settings` snapshot reason), 7.4, 14.3 (the preview container), 14.6 and
14.8. ADR 0009 records the decision to decode map exchange strings in Go
(D9, settled 2026-11-06). The plan's lasting rules are in those SPEC
sections.

### Delivered

- **Generate (SPEC 5.6).** One page makes a world with no save from a
  preset and seed, a map exchange string, or a custom map-gen form. The
  world carries `new_map` in `world.json` and its first start runs the
  game's own `--create`, only when `saves/` holds no zip, so a Docker
  restart can never overwrite a save. `new_map` is cleared once that start
  reaches InGame. A failed create uses the existing failed-start report
  with the game's own words. Generate writes no `mod-list.json`; the start
  step already does. The page is at `/generate`, because a world named
  "Generate" would take the id `generate` and hide `/worlds/generate`.
- **Exchange strings (D9, `internal/mapexchange`).** A Go port of
  rfvgyhn's parser (MIT; its notice is kept in the package) for 1.1, 2.0
  and 2.1. It takes both markers or neither, and refuses what it cannot
  read exactly: a bad checksum, left-over bytes, bad values, or a newer
  patch that does not read cleanly. Two fixes to the ported layout were
  found against the real game: from 2.0 `cliff_smoothing` comes before
  `richness`, and 2.1.18 removed `max_pollution_to_restore_trees`.
  `Compatible` refuses a string of another line, newer than the world, or
  laid out differently from the world's version (2.1.13 and 2.1.18 are the
  two layout boundaries), and names the versions that would work. A 2.1.9
  string on a 2.1.24 world failed at `--create` in the real game; it is now
  refused on the form. Fuzz runs of 30 and 60 seconds found no panic.
- **Game data and fixtures.** `internal/mapgen` embeds, for each line, the
  presets, planets, autoplace controls and each preset's values, read from
  the real images by `go generate`. `internal/mapexchange/testdata` holds
  98 golden strings made by the real game on 1.1.110, 2.0.66, 2.0.77,
  2.1.9 and 2.1.24 with the game's own parse; `make test` runs them.
- **The custom map-gen form.** Every autoplace control's frequency, size
  and richness per line and per planet; seed, size, starting area, water,
  cliffs and peaceful mode; a raw JSON box for the rest. It opens filled
  from the chosen preset, and terrain controls read Scale and Coverage as
  the game words them. Steps are the percentages the game's sliders use.
  "None" is not offered for frequency, because the real 2.0.77 game refuses
  a frequency of 0 at create. On 2.x cliffs go to the planet's control and
  never to `cliff_settings`.
- **Map preview.** "Preview map" draws one picture per planet from exactly
  the settings Generate would write, with the game's
  `--generate-map-preview` in a locked-down one-off container (no network,
  read-only root, all capabilities dropped, 1 GiB memory, 60 seconds, a
  fresh folder only). The pictures are held 15 minutes under a random token
  tied to the user. A blank seed is filled and kept. Real games drew a
  planet in 0.6 to 0.9 seconds and five Space Age planets in 5 to 6. 512 MB
  was the smallest memory that drew a Space Age planet, so the limit leaves
  room. No endpoint was added to the Docker list (SPEC 14.3).
- **Clone (SPEC 5.7).** Copies mods and config (never `rconpw`) and the
  game version and Space Age flag. "Keep the save" copies the one save
  that would load; "fresh map" sets `new_map` with a random seed and the
  source's preset while the source has not started. From the Server page,
  Generate and Clone each take three clicks.
- **Game settings (SPEC 11).** A form for the common fields, a raw JSON
  editor for `server-settings.json` with line-and-reason errors, a diff
  preview, and a confirmation held on the server under a random token for
  15 minutes. One confirmation takes one world lock, one game save, one
  snapshot (reason `settings`, "Before settings change") and one audit
  entry (`settings-save`, file names only). It checks every file is as it
  was at the preview, then writes. The game password and the factorio.com
  `username`, `password` and `token` are write-only, and log redaction
  knows them. A change to a running world's settings says it needs a
  restart and links to Restart.
- **Admins live by RCON.** A stopped world writes `server-adminlist.json`.
  A running world is sent `/promote` and `/demote` and the game writes the
  file. The real game's console has no quoting (`/promote "bob smith"`
  promotes `"bob`), so a name with any whitespace is refused, always,
  rather than escaped.
- **A complete `server-settings.json`.** Every `server-settings.json`
  Foreman writes has `name`, `description` and `visibility`, which the game
  requires (SPEC 11.1). Before this was found, the first form save on a
  world with no file wrote a file without them, and the next start went
  into a crash loop on the real game. Hidden is the default visibility, the
  world's name is prefilled, and `Validate` refuses a file without the
  three keys. The raw editor names the missing key.
- **Design and phones.** DESIGN 8.6 to 8.10 hold the patterns. Preview
  keeps focus while it runs, a failed request writes a translated
  sentence, buttons and the version select fit at 320 px and 200% text, and
  a read-only world's Admins box is read-only rather than disabled. A typed
  game password that a refused form drops is explained by a Note. The new
  pages are in the 320 px language matrix in all eight languages.
- **Last played on a phone.** Todd's ruling (2026-11-06): in the world
  list on a phone, Last played shows the age only ("10 years ago"). The
  200 px compact row stays. A wide panel and the world's own page keep the
  date, the clock with its zone and the age.
- **Translations.** The `settings` and `mapgen` areas are in all eight
  languages. A review of the seven non-English languages was applied,
  including the reversed raw-key hint in Russian, Polish and Czech.
- **Review fixes.** Settings save through one path (`SaveSettingsHeld`);
  the web and game layers share the preview types; a fresh-map Clone
  carries the source's preset; unused map-settings data was removed.
- **Follow-up issues.** #206 (public server listing), #207 (editing
  `map-settings.json` after creation), #220 (one list of supported game
  lines) and #221 (accessibility follow-ups from the review).

### Verified

- `make verify` passed on the fix-round head with coverage 90.6% across
  `internal/`. `make e2e` there: 1024 passed, 88 skipped, none failed (the
  320 px language matrix runs in one project).
- New tests: Go tests for the world, game, settings, decoder and web
  pieces; the golden test; end-to-end specs for Generate, Clone, the
  settings pages, the first save and the new pages in every language, each
  with axe, in four projects, plus keyboard-only runs.
- Against real `factoriotools/factorio` containers (1.1.110, 2.0.77 and
  2.1.24, with and without Space Age on 2.x): each way of making a map
  reached InGame, and the game's own `/seed` and iron-ore control matched
  the form. Real exchange strings made the right map. A preview drew every
  planet and removed its container and folder. A settings save on a running
  game took its snapshot and needed a restart (the new `max_players` showed
  only after it). Admins promoted and demoted by RCON were written to the
  game's file. Both Clone ways started. No page held a game password or the
  RCON password, and no ERROR line was logged. `make test-game` now has
  real-game tests for Generate and the first settings save, and `make
  e2e-game` passed its two existing tests.
- The real-game check refuted an old gap: the image does **not** write
  example config files into a world on its first start. After a first start
  `config/` held only `rconpw`, on 1.1.110, 2.0.77 and 2.1.24. The gap was
  removed from Epic 4's list.
- Code, security, accessibility and translation reviews ran over the epic.
  Security found nothing high or medium; its one low finding (a preview's
  game output in Foreman's log) was fixed. The other findings were fixed in
  the fix round or filed (#220, #221).

### Gaps left

- Leftover preview containers and work folders, after a crash, are removed
  at the next preview and not at startup.
- Exchange-string values get structural range checks only (finite numbers,
  booleans, UTF-8 and unique names), not the game's numeric bounds, so the
  game can still refuse a string at create. The settings rules check
  `map-gen-settings.json` and `map-settings.json` only for non-negative
  numbers, and `water` and `terrain_segmentation` on 1.1 only.
  `area_to_generate_at_start` is read from a string and not written.
- Strings from 2.1.13 to 2.1.17 are untested: there is no local image for
  those patches. A future layout change needs a new boundary constant in
  `internal/mapexchange`.
- The map data for the whole 2.1 line is read from 2.1.24, though
  `map-settings.json` differs between 2.1 patches.
- A fresh-map clone of a world that has already started has lost its
  preset, so it uses default settings unless the source has
  `map-gen-settings.json`.
- A preset's map-settings (evolution, pollution) are not carried into
  Custom. Filling the custom form from a preset needs script.
- The partial save, where the files are written and the RCON promote or
  demote then fails, is untested because the fake game cannot fail that
  way.
- `make e2e-game` has no real-game test for Generate, Clone or Settings.
  They were checked by hand-run scripts and by the real-game tests in
  `internal/game`. A preview picture was not compared with the generated
  map.
- A refused preview for a missing image has no download button, because
  the image download belongs to a world and none exists yet.
- On a stopped world, if a confirmation changes two files and the second
  write fails (for example a full disk), the page shows a fresh preview of
  what is left but does not say the first file was saved. The snapshot was
  taken first, so nothing is lost.
- Choosing another preset after editing in Custom replaces those edits; the
  "Filled in from the preset" note is the only hint.
- A stopped world whose existing `server-adminlist.json` holds a name with a
  space shows the list as unreadable (Factorio names cannot hold spaces).
- The accessibility follow-ups are in #221. A French header at 200% text,
  named in the review, did not reproduce.
- Not built, by decision: public server listing (#206), editing
  `map-settings.json` after creation (#207), whitelist enforcement, and a
  check of a string's planets against the world's mods (the game's create
  step reports that).

## UI batch

Serves SPEC 3.2 (`FOREMAN_PUBLIC_ADDRESS`), 5.6 (Generate and the map
preview), 7.1, 7.6, 8.6, 11.1 to 11.3, 11.5, 14.3, 17 (17.10 and the new
17.13) and 22, with DESIGN.md sections 1 to 8. ADR 0010 records why
Generate writes complete map-settings files. The plan's lasting rules are
in those sections. Issues closed: #220, #221, #232 to #242 and #244.

### Delivered

- **Look and shell.** A flat style with an orange primary button; a
  header with the rusty logo and a user menu holding the languages and
  Sign out; required fields marked by an asterisk with one note per form
  (SPEC 17.13); number fields 10 characters wide.
- **Server page.** The heading is the active world's name with its live
  state badge; a "Players join at" line with Copy address from
  `FOREMAN_PUBLIC_ADDRESS`, hidden when it is blank; Overview and Console
  tabs; the switch list replaced by a link to the Worlds list, so a switch
  is 3 clicks. The Console colours the game's Error and Warning lines and
  offers Download log.
- **World pages** have tabs (Overview, Saves, Snapshots, Settings) with
  the Active badge on each, and no "Back to" links. Confirmation pages
  open with one `h1` that asks the question.
- **Stop and restart.** A "No warning" button sets the countdown to 0;
  Cancel returns to the page that opened the form.
- **Settings.** Make admin for connected players, auto-pause on when the
  key is missing (and written on a first save), the raw editor's caret on
  the named line, world tabs and a section rail.
- **Generate.** One game-like form: a preset fills the Resources, Terrain,
  Enemy and Advanced tabs, "[modified]" with a reset, Random seed, sliders
  on wide panels and selects on phones, the full map-settings values per
  version with the game screen's limits and minimum and maximum pairs
  kept in order, Spoiling rate as the game shows it, 1.1's difficulty and
  research queue choices, each resource's on/off box, Nauvis's map type,
  and moisture and terrain type scale and bias. An unchanged preset
  passes `--preset`; any change writes both files complete. Achievement
  warnings show live and the server confirms them.
- **Map preview.** 1280 by 1280, drawn on load and after changes (newest
  wins, one planet at a time, pictures reused), the seed used and a start
  pin, a large view with Previous, Next and zoom, PNG and zip downloads.
  Previews wait up to 20 seconds for their turn, and a cancelled preview
  leaves no container or folder.
- **Internals.** One list of supported game lines in `internal/mapgen`;
  `make test` writes a coverage profile per run; `make test-game` also
  runs the web package's real-game tests.
- All new text is translated and reviewed in the eight languages.

### Verified

- `make verify` green on every task head; the full `make e2e` suite in all
  four projects (1280 and 320 px, dark and light), with new specs for the
  header, hub, required fields, confirmation pages, Generate, the preview
  and the fixes.
- Real-game checks on 1.1.110, 2.0.77 and 2.1.24 (and 2.1.9, 2.1.12,
  2.1.18, 2.1.23 for the layouts): previews at 1280 per planet with
  cleanup after normal and cancelled runs; the complete files for every
  preset reach the in-game state and read back as written, including
  Space Age worlds on all five planets; the spoiling rate's direction;
  map type, resource off, and moisture and terrain bias read back from
  `--create`; first-save auto-pause stops ticks with nobody connected.

### Gaps left

- The Console page keeps two live streams by design: the game's log lines
  and the page facts that carry the live badge.
- With two Generate tabs open, each one's preview replaces the other's,
  because each user has one current preview.
- A preset chosen during the sub-second settings redraw (after a version
  or Space Age change) is lost.
- Moisture and terrain type scale and bias use the same steps on every
  line; they were read from the game's screen, not its data.
- Exchange strings get no achievement warnings.
- Versions 2.1.13 to 2.1.17 have no tested map-settings layout, so their
  Enemy and Advanced settings are not offered. The edge tags 2.1.12 and
  2.1.18 used by the real-game layout check are not digest-pinned in the
  Makefile.
- Whether the game really disables achievements is decided in its client
  and is not tested; the table comes from the game's own text.
  `enemy_expansion.min_base_spacing` on 1.1 cannot be read back.
- The large view has no pinch zoom of its own (the browser's works).
- The live-fit announcement for a moved field that alone crosses an
  achievement line is covered only through the shared announcement path;
  no such pair exists today.
- Not built, by decision: changing map settings after a map is made
  (#207; see SPEC 11.5).

## Fixes batch 2

Serves SPEC 5.6 (Generate and the map preview), 10.3 and 10.4 (update and
rollback), 14.3 and 14.6 (the preview containers and their limits), 17
(the new 17.14) and 19, with DESIGN.md 7.6, 7.7, 8.6, 8.7, 8.8 and 12,
and STACK.md 1 and 13. ADR 0011 records how demolisher territories are
read and drawn. The plan's lasting rules are in those sections. Issues
closed: #93, #265, #272 and #274 to #284.

### Delivered

- **Update and rollback of any world.** A world that is not active can be
  updated and rolled back, and nothing starts or stops. Its update runs in
  the background. Its world page shows "Updating to Factorio X…", keeps
  focus on the busy note while it polls, then shows the result once and
  moves focus to it, with "Update anyway" after a pre-start refusal. A
  stopped world's update skips the game network check.
- **Map preview size.** 1024 to 4096 tiles in steps of 128, 1280 by
  default, a slider on a wide panel and a select on phones, kept over a
  form error. The picture cap is 16 MiB and the preview store 96 MiB.
- **Large view as a picture viewer.** Continuous zoom by wheel, keys,
  buttons or pinch, up to the larger of 8 times fit and 8 px per tile.
  Drag, keys or a tap pan the picture. Arrows, Close and zoom buttons sit
  over the picture, and a download icon by the heading. The preview
  picture fills its column, capped by the window's height. Two races
  behind large-view flakes are fixed.
- **Planet rows.** One click opens a planet row and it stays open. One
  planet is open at a time, the same in every tab and over a round trip,
  and the preview follows it both ways.
- **Cliffs off** on Nauvis (every line), Gleba and Fulgora (2.x), proved
  by counting cliffs in real games on 1.1.110, 2.0.77 and 2.1.24.
- **Needed-resource warnings** while iron ore, copper ore, stone, crude
  oil or coal is off on Nauvis, under the box and in a summary.
- **Demolisher territories** on Vulcanus (Space Age): a box that draws
  them over the picture and the large view, with counts in words. They
  are read by a control-only mod in a one-off `--create` container and
  parsed under limits. Downloads and the zip get them drawn in by the
  server, in all eight languages. They are offered up to 3072 tiles. The
  territory and preview reads refuse named pipes. The PNG renderer runs
  one at a time, stops waiting when the request is cancelled, and refuses
  a fifth waiter with a 503. Preview and territory runs share one setup
  helper.
- **Toggletips** on every Enemy and Advanced setting, in the game's own
  words, opening on click, tap, focus or hover. The × 10⁻ⁿ unit is gone.
  Sliders reach both ends, and a slider row keeps its shape while
  dragged. The preset's "[modified]" line keeps its room, so nothing
  below it moves. An off box keeps its name at full strength. Slider
  values are no longer read out twice.
- **Tests and tools.** Foreman binds before it logs `listening` with its
  real address, and the e2e harnesses listen on port 0, which ends the
  port race. `make deploy` builds the image and recreates only the
  Foreman service of a local Compose stack named by `DEPLOY_DIR`. The
  flaky restore test is fixed. Foreman no longer logs an error or sends a
  500 when a browser cancels a request mid-page.
- All new text is translated into the eight languages, and a second pass
  reviewed every new or changed string.
- The large view no longer jumps back to the previous planet when a
  planet row the page opened itself reports its toggle late; this was
  behind an intermittent end-to-end failure at 320 px.
- After a world page reloads with a result, the result is read once,
  through focus, and not repeated in the announcer.
- `make test` allows 30 minutes, since the web package takes about 11
  under `-race` on a loaded machine. Playwright runs 8 workers locally so
  two runs fit the machine, and its default in CI. `make e2e E2E_ARGS=...`
  runs chosen specs.

### Verified

- `make verify` green on every task head, and the full `make e2e` suite
  in all four projects, with new specs for the large view, planet rows,
  territories, tips and sliders (sliders in en, de and ru, and also in
  Firefox).
- Real games: previews at 4096 for every planet on 1.1.110, 2.0.77 and
  2.1.24; territories on 2.0.77 and 2.1.24 (seed 123456 at 1280 gives 12
  small and 6 medium, and 3072 peaks at 750 MiB); cliffs off on all three
  lines; an update of a world that is not active from 2.0.77 to 2.1.24,
  and its rollback.
- `make deploy` was run by hand against a throwaway Compose stack, with
  and without `DEPLOY_DIR`, with a dirty tree and with `FORCE=1`.

### Gaps left

- Update of a world that is not active: no e2e test of the "Updating to
  X…" note, because the fake game's image pull is instant (a Go render
  test covers it).
- The phone e2e projects do not emulate touch, so touch is tested only in
  the specs that turn it on.
- Download links are built twice, on the server and in preview.js.
- The "vulcanus" check and the territory size cap are each made in two Go
  layers.
- Territories are offered only up to 3072 tiles, because 4096 runs out of
  memory. Generating only every fourth chunk could fit 4096, but it is not
  proven for every seed and is not built.
- Territory labels crowd in the 320 px thumbnail, and the zip link's text
  does not mention territories.
- The cliffs-off keys were inferred from the resource toggle and proved
  in real games, not decoded from a cliffs-off exchange string.
- `make deploy` has no automated test; it needs a Docker daemon and a
  Compose stack.
- `phone.spec.ts` was type-checked but not run against a real game after
  the port change.
- The PNG renderer's one-at-a-time slot has no test beyond the waiter cap
  and cancel.
- The slider row jump of #279 could not be reproduced on the base; the
  fixed grid and the tests rule it out by construction.
- No swipe between planets in the large view (optional in the issue).

## Epic 8: Mods

Serves SPEC 18.8 (section 9, all of 9.1 to 9.15), 3.2, 6.2, 6.7, 14.6,
14.10 and 17.10, with DESIGN.md 8.11, ADR 0012 (the portal client and
where search reads from) and ADR 0013 (sealed stored secrets). The plan's
lasting rules are in those sections. Closes #131 (a world using a mod
version players cannot download is flagged), and the running-switch flake
#327.

### Delivered

- **Portal client** (`internal/modportal`). A fixed base,
  `https://mods.factorio.com`, with no setting for it. It lists the mods of
  a game line once and searches that list in memory (exact name, prefix,
  then substring, by downloads, 25 shown), reads one mod's details, and
  downloads a release with a running SHA-1. A download follows at most 2
  redirects, only to `https` hosts ending in `.factorio.com`, sends no
  `Referer`, and is capped at 200 MB and 5 minutes. The real portal
  redirects to `dl-mod.factorio.com`, confirmed with a real token. Errors
  never carry the query string. Replies are cached in memory for 15
  minutes (404s too, errors never), at most 4 detail requests run at once,
  and nothing is fetched at startup. A Go fake portal (`modportaltest`)
  serves recorded replies and the failure modes.
- **Resolver and planner** (`internal/mods`). A parser for dependency
  strings (fuzzed) and a greedy resolver: keep the installed version when
  it fits, else the newest release on the world's line. It covers install,
  update, update all, enable, disable, remove, upload and sync. Plans read
  installed mods' dependencies from their own `info.json`, so disabling,
  enabling and removing work with the portal down. Conflicts and reasons
  are codes plus mod names, so they translate. Sync pins the save's exact
  versions, disables extras and never removes them; "nearest release" is
  the next one up on the line, else the newest below, with a warning.
  Updates are found when a Mods tab opens. The availability check (#131)
  flags a version the portal no longer has (error), a deprecated mod
  (warning) and a mod not on the portal (note). `Planner.SaveDrops` names
  the mods a change turns off that the loading save uses. `HashPlan` covers
  the plan and those drops.
- **Account and sealed secrets.** `FOREMAN_FACTORIO_USERNAME`,
  `FOREMAN_FACTORIO_TOKEN` and `FOREMAN_SECRET_KEY`, each with `_FILE`.
  One of the two account variables alone, or a key under 32 characters, is
  a startup error. The account page (`/settings/factorio-account`) shows
  "Set by environment", the enter form, or a stored account with Replace
  and Remove, writes audit entries without values, never shows either
  value back, and returns to the page that asked. The stored account is an
  AES-GCM sealed row of the new `stored_secrets` table (migration 0005,
  `internal/secret`, `internal/factorioaccount`); an unopenable row asks
  for the account again. A "factorio.com account" link is in the user menu.
- **World layer** (`internal/world`). Listing mods (built-in first, folder
  mods marked local), the mods fingerprint, reading a save's mods, staging
  and reading uploaded zips, and `ApplyModsHeld`, which places zips, writes
  `mod-list.json` keeping unknown keys, removes replaced zips and writes
  `mod-settings.dat`. The newest of two zips of one mod is used and the
  others are deleted. The zip entry count is made from the end record by
  the same rules as Go's `archive/zip`; zip info is cached by name, size
  and time; staged mods older than an hour are removed. The snapshot
  reason is `mods`, labelled "Before mod change". A read of `mod-list.json`
  during an atomic replace no longer fails with "not a regular file".
- **Apply operation** (`Controller.ApplyMods`). One operation, shaped like
  a game update: download every release first, then warn and stop,
  snapshot, apply, pre-start check and start. A failed download or
  checksum leaves the game and the world untouched; the change is checked
  against the folder before the stop; the fingerprint is read again after
  the stop. `ModOp.ConfirmedDrops` lets the restart pass exactly the mods
  the admin confirmed, so a confirmed drop ends with the game running. A
  world that was started anyway comes back after a mod change. Audited as
  `mods` and `mods-refused`.
- **Screens** (DESIGN 8.11). The Mods tab (search, installed table, status
  block, built-in mods in one line, match to the save, mod upload,
  `mod-settings.dat` download and upload with help text), the search page,
  and the plan page, whose "Add to plan" and "Add another mod" build one
  plan with several mods: one snapshot and one restart. The plan page
  carries the request, a plan hash and the folder fingerprint, so a change
  between page and post applies nothing and shows "The plan changed". Mod
  names never hyphenate. Portal links appear on search results, plan rows
  and installed rows. The Server page has a Mods link and a one-line notice
  next to the status facts, with a Fix link that goes straight to the
  plan. The pre-start refusal and the import result link to "Match the mods
  to the save". A plan request names at most 50 mods and is refused
  cross-site. Everything is in all eight languages. A second agent checked
  every new string (SPEC 22.6) twice: at the first review, and again at the
  confirmation round for the later strings, including the last screen
  task's. The fixes from both rounds are applied.
- **Click budgets** (SPEC 17.10), measured: install one mod 4, two mods 5,
  update all 3, fix a flagged mod 2, undo a mod change 2.
- **Tests and real-game checks.** A test-only fake portal (`cmd/fakeportal`)
  is an HTTPS proxy with its own certificate authority, reached through
  `HTTPS_PROXY` and `SSL_CERT_FILE`; the product has no portal setting or
  test hook. Seven `mods-*.spec.ts` files, the account spec and the click
  budgets run in all four projects with the axe scan and phone width. Each
  review finding has a test that failed first. In real games on 1.1, 2.0
  and 2.1 (`make test-game`), a made-up mod is installed, updated and
  disabled through `ApplyMods`, and the tests record that the game loads
  the higher of two zips of one mod, rewrites `mod-list.json` at exit (same
  meaning, other bytes), and writes `mod-settings.dat` at its first start
  and only when a mod declares settings.

### Verified

- `make verify` green on every task head, coverage above 90 percent.
- Full `make e2e` on the last task heads: 1924 passed, 116 skipped (the
  usual project skips), 0 failed. The last task's own specs passed 88 of 88
  after a regression it found was fixed.
- `make test-game` on 1.1, 2.0 and 2.1: all pass, including
  `TestRealGameModsChange` (with the confirmed drop) and
  `TestRealGameModFilesHandling`.
- A real token downloaded a release through the redirect to
  `dl-mod.factorio.com`.
- No test calls the real portal.

### Gaps left

- A confirmed drop on a stopped world is not remembered for the next Start,
  so that Start is refused until the admin starts anyway.
- A drop that is not a mod problem, for example a Space Age mismatch, is not
  passable.
- A sync plan accepts one uploaded zip.
- The sync after an import is offered only when the pre-start check finds a
  save mod missing or off.
- The in-progress display of a change to a world that is not active is only
  lightly tested: the fake download is too fast to catch it, so the tests
  check the step list.
- Removing a stored factorio.com account asks no confirmation, and the
  token is not checked against factorio.com when it is saved.
- A disabled mod is not updated by "update all", and folder mods are not
  checked for availability.
- A data race in an `internal/game` test (`selfContainer` read during
  `refreshSubnets`) is #337; it is not fixed here.
- Deferred, filed as issues: choose a mod's version (#340); one snapshot for
  a run of changes on a stopped world (#341); the Mods tab review items
  (#342 portal status in each row, #343 a two-column table on phones, #344 a
  fix offered for a conflict, #345 denser search results); editing mod
  setting values (#339, SPEC 9.6 P2); and the American spelling sweep,
  including "labelled" (#320). The public mod download page is #132.
