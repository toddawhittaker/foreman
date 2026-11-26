# Status

What each epic delivered and the gaps it left. The epic's last task
(the fold) writes its section once, from the task pull requests. Gaps
here are known and accepted; each is either fixed by a later epic or
waits for a decision.

Epics 6 to 10 were renumbered on 2026-11-05 when Languages became Epic 6
(old 6 Mods is now 8, old 8 is 9, old 9 is 10, old 10 is 11). Older text
below uses the new numbers.

Epics 1 to 9, the Gate A fixes, the fix batches after Gate A (hub
fixes, polish fixes, UI batch, fixes batch 2, fixes batch 3, fixes batch 4),
the Resource amounts epic, fixes batches 5, 6 and 7 and the Mod settings epic
are done. Epic 9b (VPN sidecars) is built, but its live acceptance is still
open (see its section). Epic 9c (public mods and autosave tidy), Epic
10a (API tokens and MCP), Epic 10b (MCP admin tools) and Epic 10c
(REST API) are done. Epic 11
was split on 2026-11-18: Epic 11a (sign-in, audit log, archive, settings and
users) is built, with its gate on the trial deployment still to run, and
Epic 11b (the release and Gate C) is next. Fixes batch 13 (three issues from
checking Epic 11a on the trial deployment) is done, and so are fixes batches 14,
15 and 16. Epic 12 (modded planets and guided Generate) is built, and fixes
batch 17 (the Epic 12 test-drive findings) is done. Fixes batch 18 (mod icons
from the world's zips, and the star map) is done too and closes the gap
that Fixes batch 17 left for mod icons. Fixes batch 19 (spots of fluid tiles
in the resource count) is done.
Gate A (SPEC 19.1) passed on 2026-11-05. Gate B (SPEC 19.2) was skipped for
now, at Todd's choice, when the Mod settings epic started; it is still owed
before Gate C.

Each section describes the game images pinned at its time. Since fixes
batch 6 the real-game tests pin only 1.1.110, 2.0.77 and 2.1.24: older
mentions of 2.1.23 and `GAME_IMAGE_2_1_PREV` are history.

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
  leaves a blank strip under the log. (Closed by the third design climb:
  the button now floats over the log's foot and keeps no row, DESIGN 8.5.)
- A stop's time on a large map is unmeasured. On the small test map
  `docker stop` took under a second; the 120-second timeout is a generous
  guess for big worlds.
- No Space Age world (flag on) was started against a real game. The save
  reader reads real Space Age saves, and the real check ran 2.1.24 with
  Space Age off.
- There is no audit viewer, filter or retention yet (Epic 11), and audit
  rows have carried the API token's name since Epic 10a (SPEC 12.9).
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
- Setup does not carry a return path. (The Deleted worlds page's Restore
  link names showed UTC; closed in fixes batch 4.)
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

## Fixes batch 3

Serves SPEC 6.2, 7.4, 7.9, 9.1 to 9.4, 9.7, 9.11 to 9.13, 14.8, 17.9 and
17.10 (a new row), with DESIGN.md 7.1, 7.4, 7.6 and 8.11, and ADR 0014
(one snapshot for a run of mod changes on a stopped world). The plan's
lasting rules are in those sections. Issues closed: #337, #341, #344,
#345, #354 to #360, #362 to #377 (every number in that range except #361,
which was the plan's own pull request).

### Delivered

- **Recommended dependencies (#357).** Factorio 2.1's "+" prefix parses.
  A recommended mod is an optional one that starts ticked. One the portal
  lacks, or one that would conflict, is left out with a note and never
  blocks the plan. A sync adds none. A recommended mod is offered only for
  a mod the plan changes, so disabling or removing an unrelated mod never
  asks the portal.
- **Drop warning on a running world (#358).** Stopping the game saves
  every enabled mod, so on a running world disabling or removing any mod
  asks for the drop confirmation. A stopped world still reads the save on
  disk.
- **Test race (#337).** A test changed controller fields while the monitor
  ran; fixed.
- **Account page (#354, #377).** It leads with one sentence saying whether
  mod downloads are on, says that every admin shares the one factorio.com
  account (also in the README), and folds the Replace form away while an
  account is stored. Replacing now costs one more click, on purpose.
- **Pre-start refusal (#356, #363).** Lists use the box's full width (the
  step rule is now `.steps > li`). The box gives the problem, then the main
  fix, then the restore of the snapshot taken, with "Or start anyway" last
  in a closed choice. The advice to copy files into a host folder is gone,
  from the refusal and the switch page.
- **A mod version the portal dropped (#359, #362).** A start, restart or
  switch of a world whose enabled mod is at a version the portal no longer
  offers is refused, with "Update *mod*" first and start anyway kept. The
  portal is asked once per action, for at most 5 seconds, from the cache
  where it can. A portal that is down or slow never blocks a start; the
  result says it was not checked. A switch that will not start the world
  skips the check. The Server page's mods notice reloads whenever an
  operation ends, and is not read out again when nothing changed.
- **One snapshot per run of mod changes (#341, #370).** On a stopped
  world only the first change since the game launched takes a snapshot
  (a marker file names it; ADR 0014). Undo then reads "Undo the mod changes
  since <time>". The undo confirmation lists the mods it removes, puts
  back, returns to an earlier version, or turns on or off. A running world
  still snapshots every change. The plan page says when the run's snapshot
  is kept.
- **Mod search (#355, #345).** Results are a table, ten rows a page, with
  the total count in the caption. Several mods can be ticked and added to
  one plan from the Mods tab's search. The portal client no longer cuts the
  results at 25. The plan page's "Add another mod" uses the same table. The
  world is back in the `h1`.
- **Mods tab (#360, #342, #343, #365, #371, #372, #373).** One summary line
  with "Update all"; each mod's flags and fix in its own row; a two-column
  table that stacks on a phone; "up to date" counts only mods the portal
  has at their installed version; the built-in mods in one plain line; the
  tab says whether the mods match the loading save; upload and
  `mod-settings.dat` replace are folded away.
- **Operation results (#366, #367, #369).** A failed mod download names the
  mod and version, says whether the world kept running, and offers Try again
  (2 clicks) or the account page. After any failed operation the steps that
  never ran fold into one line. A finished mod change says which mods were
  installed, updated, turned on or off, or removed.
- **Mod plan page (#374, #344, #364, #376, #368, #375).** Changes first,
  short steps. A conflict offers "Remove *mod* as part of this plan" or
  "Disable *mod* too" as a control, in one snapshot, and hides the steps.
  The drop warning comes first and is said once. Red is for what destroys
  something: removing a mod, dropping save content, continuing without.
  Installing or updating a mod while the game runs is a primary button.
- **Spec and design.** SPEC 17.10 has a new row for Try again (2 clicks)
  and the second-mod row says "on the plan page". DESIGN 7.1 names the
  quiet red Remove, 7.4 describes the undo confirmation and the new refusal
  box, and 8.11 describes the new account page.
- All new text is translated into the eight languages, and two
  second-agent reviews (SPEC 22.6) corrected the translations. The mod
  enable and disable wording was reworded in all eight.

### Verified

- `make verify` green on every task head, and the whole `make e2e` suite
  (with 4 workers) after the changes to shared files; the last full run,
  at the portal and notice review fixes, was 2028 passed, 116 skipped, 0
  failed. The two expected-failure tests in `errorlog.spec.ts` show as
  failures by design.
- New specs: refusal-layout, mods-search, mods-tab, mods-undo,
  mods-failures and mods-prestart, each at desktop and 320 px, in both
  themes, with the accessibility scan. The click-budget test counts the new
  rows.
- Real games: the drop on a running world (#358), one snapshot per run
  (#341) and recommended dependencies (#357) were run against
  `factoriotools/factorio` 1.1, 2.0 and 2.1 and pass (the recommended
  dependency test runs on 2.1 only, where "+" exists). They are in
  `internal/game/realgame1_modsplan_game_test.go`.
- `FuzzParseDep` for 20 seconds with the "+" prefix: pass.

### Gaps left

- Per-admin factorio.com accounts are still future work (#354).
- Closed in fixes batch 4: #340 (choose a mod's version) and #378.
- Code-review refactors left for later: the web layer works out which
  snapshot undoes a change from the files. Closed in fixes batch 5:
  `restoremods.go` re-reading mod lists the world package already parses,
  and `currentCount` re-reading the mods list.
- Flagged rows on the Mods tab are not sorted first (still open; see fixes
  batch 5). Closed in fixes batch 5: at 320 px a row with Update wrapped
  Remove to a second line, and a folder mod's Disable sat at the row's end
  instead of under the others'.
- Closed in fixes batch 4: the Undo button's time was the server's UTC
  text; it now shows local time.
- The snapshot step reads "done" when a run's snapshot is reused.
- Closed in fixes batch 5: the refusal and the Server page notice could
  show together with two "Update *mod*" buttons for the same plan; the
  notice now drops its own.
- The pre-start refusal's main fix is a secondary button, because DESIGN
  7.1 forbids a primary one while the game runs. If a live update redraws
  the control region while "Or start anyway" is open, it closes again.
- A game version update and the restart after a mod change do not ask the
  portal. The start-anyway box says the game drops from the save whatever
  depends on the problems, which says nothing useful for a gone mod.
- Other mod-change failures (after the stop) keep their generic words, and
  a plan that used an uploaded zip has no Try again.
- No fix is offered for a conflict from a mod with no release, Space Age
  off, or a mod not on the portal. Closed in fixes batch 5:
  `settings.preview.undo` kept the old "restore from the Snapshots page"
  wording.
- Closed in fixes batch 5: `server.problem.modlist_space_age_on` and `_off`
  said "turned off/on" for entries in `mod-list.json`; they now say
  enabled/disabled.
- There is no filter-as-you-type on the search page. Closed (found already
  gone in fixes batch 5): the old `.mod-results*` and `.mod-summary` CSS.
- Closed in fixes batch 5: code comments citing issue numbers, and older
  tests citing "ruling N" from earlier epics.

## Fixes batch 4

Serves SPEC 5.6, 5.11, 7.4, 7.11, 9.2, 9.3, 17.5, 17.9 to 17.12 and 22,
with DESIGN.md 2, 4, 7.2, 8.3, 8.6 to 8.8 and 8.11. Issues closed: #315 to
#323, #340, #378, #398 and #399. The sweep of the remaining "labelled" and
"cancelled" in English strings (#320) was a separate task of the batch.

### Delivered

- **Generate page (#315, #316, #320, #321, #323).** Leaving Generate asks
  with the browser's own "leave this page?" question (`beforeunload`)
  when a setting differs from what the page loaded with. It does not ask
  for views (tab, preview size, territory box), after a successful
  Generate, or when nothing changed. A redraw of a dirty form keeps the
  mark, and a page restored from the browser's back-forward cache warns
  again. The "Without iron ore" warning sits on its own line under the
  resource's grid, so the columns line up. The thumbnail's territory
  labels are short ("Sm.", "Med.", "Big") and pulled inward so they stay
  inside the picture; the counts stay as visually hidden text, still
  announced. The seed shows at the end of the "Map preview" heading's
  line, the size hint is one line and says what the pin is, and the large
  view's key help is one line on a desktop. English on Generate uses
  American spelling.
- **One planet or All planets (#319, #322).** The Planet select is gone:
  opening a planet's settings row picks the planet. On a world with
  several planets the preview has two tabs, One planet (the default, one
  large picture) and All planets (thumbnails). "Preview map" draws every
  planet and selects All planets, so One planet is one click back.
  Without script the tabs are hidden and Preview map works as before.
- **One size cap (#317).** The Preview size runs 1024 to 3072 tiles, the
  same cap as the territory box, which lost its turned-off state.
- **Reset to defaults with Undo (#318).** One button resets the whole
  world to the version's default preset and values, keeping the seed. It
  asks no confirmation, because nothing saved changes; "Undo reset"
  takes its place with focus and puts every value back in one click
  (SPEC 17.9). Any later change to the settings, preset, seed, game
  version or Space Age ends the Undo, and focus never drops to the page
  body. A change made while a reset is on its way does not stop Undo
  from being offered; it brings back the settings as they were when Reset
  was pressed. Without script the reset is a plain round trip with no
  Undo.
- **Settings beside the large map (#399).** In a large view at least
  77rem wide, in Map settings mode, the viewed planet's Resources and
  Terrain settings sit in a column on the left, with the map on the right.
  They are the form's own fields, moved in when the view opens and back
  when it closes or before any redraw of the form, so sending, checking,
  "[modified]", reset, Undo and the leave warning behave as for a change
  on the page. A change redraws the map after the page's usual pause; the
  newest request wins. Keys belong to the focused field; Escape closes
  the view. While the map redraws, a visible line says so without being
  read out; the status line says only "Map preview ready.", warnings and
  errors. If the view narrows with a field focused, focus moves to the map.
  Narrower views, phones and exchange-string maps show the map only. Changing a planet's setting while looking at it takes 1 click
  instead of 3 or 4.
- **Saves tab (#398, SPEC 7.11).** One list of saves replaces the radio
  list and its "Use this choice" button. The row that loads next has a
  "Loads next" badge; every other row has "Load next". The newest row's
  button sets the choice back to "the newest save" (an empty choice), so
  later autosaves win, and it is offered whenever a save is chosen, so a
  choice that points at a save removed by hand can always be cleared.
  Choosing an older save takes 3 clicks from the Server page, down from 4.
  The form works without script; the server still checks the posted name.
- **Mod versions (#340, SPEC 9.2, 9.3).** The plan page gives each mod an
  install or update names a "Version of *mod*" list: "Newest that fits"
  first, then the world's game line's releases, newest first, the
  installed one marked. At the installed version the page says "Nothing
  changes at this version". Each portal mod's row on the Mods tab has
  "Change version", which is also how to go back after a bad update.
  The server checks every chosen version against the portal's release
  list (a new `no-such-release` conflict) and always downloads the
  portal's own release record. A plan's release list is left out of its
  hash. Changing a mod's version takes 4 clicks. The plan form syncs
  all redraws so the newest choice wins. A focused Change version link
  that the status takes away hands focus to the row's mod name, which
  reads its note.
- **Local times (SPEC 17.11).** The Undo button, the crash-loop Restore
  offer and the Deleted worlds Restore link names show the browser's zone
  (a `time` element; inside a button the local time alone, without "ago").
  The Server page's Done box is localized just before it takes focus
  (`window.foremanTimes.run`), so a screen reader never reads the UTC
  text.
- **Headings (#378, SPEC 17.12).** Section headings (`h2`) are 1.25rem at
  weight 650, and each top-level panel after the first has a one-pixel
  rule above it. `h3` is unchanged. DESIGN 2 and 4 record it.
- **Tests.** The flaky refusal-layout restart test waited too little for
  a refusal arriving by a live update; the test now waits. New real-game
  tests cover a chosen save loading and a cleared choice loading the
  newest (SPEC 7.11), and a mod held to an older release with one
  snapshot first and Undo (SPEC 9.2 to 9.4). The real-game phone scenario
  was out of date with the Server page header; it now matches.
- **Spec and process.** SPEC 17.10 has two new rows (older save, change a
  mod's version). I18N says English uses American spelling. The
  `epic-run` skill's rules template now makes task agents run only fast
  checks (each under 3 minutes, in the foreground) and leaves the full
  `make verify`, `make e2e`, `make test-game` and `make e2e-game` to the
  orchestrator.
- All new and changed text is in the eight languages.

### Verified

- Task heads ran fast checks (`make check-go fmt-check vet lint typecheck`,
  `go test` for the changed packages, and the browser specs for the
  touched pages, desktop and 320 px, both themes, with the accessibility
  scan). Late in the batch the full `make verify` and `make e2e` moved to
  the orchestrator, who runs them on the batch head before merging.
- Every new test was run first against the base and failed there: for
  example 17 failures for the Generate changes, and the local-time and
  late-redraw tests.
- Full browser runs on task heads: 2052 passed after the first Generate
  task, 2036 after the Saves task, and 2104 in six shards after the
  second Generate task, with 116 skipped and none failing apart from
  `errorlog.spec.ts`'s two expected failures.
- The last Generate task ran fast checks itself; before its merge the
  orchestrator ran `make verify` and the whole browser suite on it: 2,182
  passed, with only `errorlog.spec.ts`'s two expected failures.
- Real games: `make test-game` passed on 1.1, 2.0 and 2.1 with the new
  tests, and the 3072-tile preview test passed. `make e2e-game` passes
  after the phone scenario fix.
- Several fast-check runs were made at machine loads of 70 to 100 from
  another project.

### Gaps left

- The large view's settings column has Resources and Terrain only; the
  Enemy and Advanced settings are not in it.
- In All planets mode a change made in the large view redraws every
  planet, as it does on the page.
- Closed in fixes batch 5: the large view's key help did not say that keys
  inside a setting work the setting, and its column `h3` headings looked
  like body text.
- The mod plan page's "Add another mod" panel has no section rule, because
  it follows a confirmation box.
- Closed in fixes batch 5: the newest Saves row's button was named after
  its file though it loads "the newest save", and the Load next buttons did
  not line up when save names differ in length.
- On a phone, All planets is one column of full-width pictures. The key
  help takes two lines at 320 px. Without script, Reset has no Undo.
- "Update all mods" has no per-mod version lists.
- Closed (found already there in fixes batch 5): the click-budget test for
  "Change a mod's version" (4 clicks, SPEC 17.10). The new strings keep the Generate preview's
  0.75rem label size, which has no design token.
- The Generate page was not checked in screenshots after the heading
  change.
- Hand-test: NVDA's reading of a focused Undo or Restore link when the
  Server page's control region redraws.
- Closed in fixes batch 5: no test pressed Tab through the large view's
  dialog, or Escape on an info tip's button inside its settings column.
- generate-leave.spec.ts has no accessibility scan of the states it
  reaches (edited, after a reset redraw, with Undo reset showing).
- Closed in fixes batch 5: the Server page did not say which save loads
  next while the world is stopped (#419).
- Still deferred: #400 (a headless game per preview), #353, #339 (P2),
  #132, #92, #206 and #207.

## Resource amounts

Serves SPEC 5.6, 14.1, 14.3, 14.6, 14.7 and 17, with DESIGN.md 7.4, 8.7,
8.8 and 12 and ADR 0015. Issues closed: #400 (resource amounts in the large
view), #422 (Foreman's own leave dialog) and #423 (sliders centred on 100%).
Built in four lanes at once: the count run and its measurements, the
parser and fake game, the web side, and the sliders and leave dialog. The
pull requests were #430 to #436, then #438 and #439 for the integration
review.

Integration review: a code review across the tasks and an accessibility
review of the flows between them, on the epic head. Fixed in #439: the fake
game now writes the game's real entity names, and the real-game test checks
names exactly (`resource.EntityName`); one shared mod-info helper and one
shared failure mapper for territories and resources; one rule for yield;
spike-only measuring tests removed. Fixed in #438: the counting sentence is
also spoken after a redraw or a planet change; on Vulcanus the territory
counts are inside the large view; the text list's totals are h4 under a
hidden h3. Kept: `color` in the count file (the real-game alignment check
uses it); ←/→ change the planet from anywhere in the view (SPEC 5.6).

### Delivered

- **Resource amounts in the large view (#400, SPEC 5.6).** A "Show
  resource amounts" box in the large view's bar, off when the page loads
  and kept while the page lives. Once the shown planet's picture is
  there, `POST /generate/preview/resources` counts that one planet in the
  real map and labels each resource patch with its amount ("1.2M",
  through `Intl.NumberFormat` compact notation, so it reads right in all
  eight languages with no new number strings). Wells (resources whose
  product is a fluid: crude oil, sulfuric acid, fluorine, lithium brine
  and the geysers) show yield as a percentage; ores show their amount.
  Labels share the territory labels' look, are placed greedily so none
  overlap, and more of them show as you zoom in. A collapsed "Resource
  amounts as text" list gives every patch for keyboard and screen-reader
  users. Counts are kept per planet with the preview, so going back is
  instant; a settings change drops them at once. Busy, timeout, out of
  memory, image missing, failed and invalid each have their own sentence,
  with Try again where a retry helps. All new text is in the eight
  languages (17 `worlds.generate.resources.*` strings).
- **The count run (SPEC 5.6, 14.3).** A third kind of preview container,
  `foreman-resources-<folder>`, with the same slot, cleanup, host settings
  and limits as the picture and territory runs. A control-only mod
  (`foreman-resources`) reads the resource entities one chunk at a time,
  groups them in Lua and writes `resources.json`. Ores join at a gap of 2
  tiles, wells within 24 (ADR 0015). The code that runs a control-only mod
  moved out of `territories.go` into `internal/game/controlmod.go`, which
  territories and resources now share.
- **The parser (SPEC 14.1, 14.6).** `internal/resource` checks the file as
  untrusted input: at most 512 KiB, 5,000 patches and 64 resources;
  unknown fields refused; whole numbers from 0 to 2^53 only (a number token
  is at most 32 bytes, and float and exponent spellings are read exactly);
  names matching `^[a-z0-9_-]{1,64}$`; every box inside the picture and
  every label tile inside its box; patch amounts that add up to each
  resource's total. Errors carry counts, never text from the file. The
  fake game answers a count with canned patches, so the browser specs run
  without a game.
- **Sliders centred on 100% (#423).** Each map-generator slider puts its
  default step ("100", or "0" for bias) at the middle of the track. A click
  takes the nearest step; a key press or a screen reader's increment always
  moves one step. A select with no default, or a default at an end, keeps
  the plain track. Nothing changed on the server, in the posted values or
  in the labels.
- **Foreman's own leave dialog (#422).** A plain click on a link, or the
  submit of a form other than Generate (language, Sign out), opens a dialog
  when the settings are unsaved. Stay or Escape closes it and returns focus
  to what was used (the user menu's button if the link was in the closed
  menu). Leave goes on without the browser's own question. `beforeunload`
  is unchanged and still covers reload, closing the tab, the address bar
  and Back. Four `worlds.generate.leave.*` strings in all eight languages.
- **Real-game checks.** `TestRealGameD2ResourceInvariants` runs on 1.1.110
  (Nauvis) and 2.0.77 and 2.1.24 (all five planets) and pins: the same
  request gives the same result and a distant seed gives other patches; a
  resource switched off has no patches; richness 200% gives 1.99 to 2.01
  times the total; wells are the fluid-product resources only, and ores
  never show a yield; the patch count equals a test-only scan's clusters at
  the stated gaps, with the same entities, amounts and boxes; each label is
  a tile of its patch; at each large ore patch's label tile the preview
  picture has the resource's map colour (this is what the file's `color` is
  for); 3072 on every planet fits 60 s and 1 GiB; a cancelled count returns
  within about 150 ms and leaves no container, folder or held slot.
- **Measured (seed 123456, default settings, 3072 tiles).** Peaks are the
  container's `memory.peak`; times run from the call to the end of the read.
  On 2.1.24 Gleba was the slowest at 29 to 33 s and the largest at 750 to
  757 MiB; Vulcanus 24 s and 750 MiB; Nauvis 21 s and 734 MiB; Fulgora 22 s
  and 676 MiB; Aquilo 13 s and 676 MiB. On 2.0.77 the peaks were 609 to
  686 MiB and on 1.1.110 Nauvis 339 MiB in 18 s. No default run came near
  the limits, so there is no size cap and no lean run. At 1280 tiles every
  run took 4 to 8 s and peaked under 470 MiB. The largest file was 97,948
  bytes (1.1 at 600% resource settings) and the most patches 1,113; at
  default settings 79,683 bytes and 956 patches (Fulgora).
- **Findings that changed the design.** 1.1's `--create` does run a mod's
  `on_init`. A well gap of 12 tiles split real fields, so it is 24. Ores
  fragment on Fulgora (3,301 scrap patches, 2,743 of them under 10
  entities) and Vulcanus, so ores join at a gap of 2 tiles. Lithium brine
  is finite in the game, so grouping by "infinite" was wrong and wells are
  now resources whose product is a fluid. The game already reads
  `map_color` back as 0 to 255, so the file's colours are used as read. A
  first Lua that kept 400 bytes per tile ran out of memory at 600%; the
  committed Lua keeps a key, an amount and a parent per tile and gives
  identical patches on all 22 default runs.
- **Spec and process.** SPEC 5.6, 14.3, 14.6 and 17.15 and DESIGN 7.4, 8.7,
  8.8 and 12 carry the lasting rules; ADR 0015 records why the count is
  made as it is. The epic plan was deleted.

### Verified

- Task heads ran fast checks (`make check-go fmt-check vet lint
  typecheck`, `go test` for the changed packages, and the browser specs for
  the touched pages at 1280 and 320 px in both themes, with the
  accessibility scan). Every new test was run against the base first where
  it could fail there (the centring test and the removal of a parser check
  or of Lua's union).
- Browser specs on the web head: `resources.spec.ts` (8 tests on 4
  projects) and `territories.spec.ts` 70 passed, 2 skipped (wide-only on
  phones); `large-view.spec.ts` and `large-view-settings.spec.ts` 102
  passed, 18 skipped; `sliders.spec.ts` 46 passed, 38 skipped (phones skip
  sliders); `generate-leave.spec.ts` 72 passed, including an axe scan of the
  open dialog at 1280 and 320 px.
- Real games on 1.1.110, 2.0.77 and 2.1.24 on the pve1-dev lane: the
  measurements above, the invariants test (1.1: 46 s, 2.0: 298 s, 2.1:
  301 s), and the territory test after the shared-code move (18 territories
  at 1280, 83 at 3072). With Lua's `union` made a no-op, or tiles dropped at
  `x % 97`, the invariants fail as intended.
- Screenshots of the counting, labels, text list, zoomed and failed states
  at desktop and phone widths in both themes were reviewed.
- The full `make verify`, `make e2e` and `make test-game` on the epic head
  are the orchestrator's; their results are in the integration review.

### Gaps left

- **Alignment on Fulgora and Aquilo is not checked.** The preview draws only
  part of those tiles (scrap on a quarter of its tiles, oil for 61% of
  wells), so the colour-at-the-label test skips them and says why. Wells are
  not alignment-checked on any planet.
- **Nauvis at 600% resource settings runs out of memory on 2.x** (the
  game's own generation peaks at 825 MiB, 913 MiB with the entities found).
  It fails safe with the existing out-of-memory message. 1.1 fits it
  (1,017 MiB measured).
- **At 320 px the map shifts while counting.** The counting line and bar
  take room in the bar. Accepted.
- **One user can fill the 8 preview queue places** with concurrent
  requests. This is by design for every preview route (SPEC 14.3), and is
  accepted.
- The leave dialog does not catch a link opened with a modifier key,
  downloads, links with a target, same-page `#` links or the picture links
  that open the large view, and has no click-outside-to-close.
- ←/→ change the planet even from the amounts list; this is the large
  view's rule that arrows step the planet unless the focus is in a settings
  field (SPEC 5.6), kept.
- Amounts cover the picture only: a patch cut by the edge counts the part
  inside it, and the page says so. No mods' resources (the preview never loads mods,
  ADR 0011). Closed in fixes batch 5: amounts in the PNG download, the zip
  and the One planet picture.
- Territories and resources on Vulcanus are two separate runs (about 27 s
  and 24 s) when both boxes are on.
- Hand-test: NVDA's and VoiceOver's reading of the counting sentence and of
  a failure when it is said by the hidden live region.

## Fixes batch 5

Serves SPEC 5.6, 7.3, 7.11, 8.7, 9.3, 17, 17.7, 17.10 and 17.15, with
DESIGN.md 8.3, 8.7, 8.8 and 8.11 and ADR 0016. Chosen by Todd on
2026-11-09 before Gate B. Issues closed: #441 (resource amounts on One
planet and in downloads), #424 (Change version as a row action) and #419
(the save that loads next on the Server page), with gaps from fixes
batches 3 and 4 and the Resource amounts epic. The pull requests were
#444 and #448 (Enter), #447 (amounts), #445 (Mods tab rows), #446 (Loads
next and Saves), #453 (large view polish), #452 (mod-change wording), #455
(comment cleanup), and three flake fixes found on the way, #449, #451 and
#454.

Rulings (Todd, 2026-11-09): lithium brine stays an amount, not a yield,
because the game marks it finite (SPEC 5.6 already says so); Enter in a
field of the large view's settings does nothing. Ruled by the orchestrator:
flagged mod rows are not sorted first, because the flags arrive after the
page loads and moving rows under the focus hurts more than it helps (Todd
may revisit).

### Delivered

- **Enter in the large view's settings (SPEC 5.6).** Enter in a text,
  decimal, check box, radio or slider field inside the settings column only
  blocks the form's submit: it never creates the world and sends no change
  of its own, so the value commits once, when the field is left. Enter on a
  button there still presses it, Enter in the main form is unchanged, and a
  composing Enter (keyCode 229) is left alone.
- **Resource amounts on One planet and in downloads (#441, SPEC 5.6,
  17.15).** The One planet picture has its own "Show resource amounts" box,
  off on load and kept while the page lives, with the same count path as the
  large view (`preview.js`): counting line and bar, announcements,
  cancelling, Try again, labels without overlap. Only one count runs at a
  time. While a box is ticked, a planet's download (`?amounts=1`) and the
  "Download all" zip draw the amounts the preview store already holds; a
  planet without held counts is served plain under its plain name. The link
  says "with resource amounts" or "with territories and resource amounts",
  and the files end in `-amounts` or `-territories-amounts`. No download
  path counts, and there are no new routes and no Docker calls. Labels in a
  download read as the page's `Intl.NumberFormat` output in all eight
  languages, from a table of CLDR compact and percent patterns in
  `internal/web/compact.go` (ADR 0015). `internal/territory` draws the
  captions after the territory labels in the same look. The Chinese Noto
  subset gained the digits, `.`, `,`, `%`, 万 and 亿, and `fonts/NOTICE` pins
  its exact source and command. Four new strings in eight languages, and
  `worlds.generate.territories.draw_busy` reworded since amounts share the
  render queue.
- **Change version as a row action (#424, SPEC 9.3, 17; DESIGN 8.11).**
  Change version is a quiet button among the row's actions, after Update,
  shown only when the portal lists another release of the mod for the
  world's game line. The tab draws an empty slot that the portal status
  fills, so nothing appears and then disappears. One planner call
  (`mods.Planner.Status`) reads the world once and asks the portal once for
  the updates, flags and changeable mods; a world briefly away gets an empty
  status. The actions are two groups (Update and Change version, then
  Disable or Enable and Remove), so Remove never wraps alone at 320 px, and
  on wide screens a folder mod's Disable lines up with the others'.
- **Loads next and Saves (#419, SPEC 7.11, 17.7).** While an older save is
  chosen and its file exists, the Server page shows a "Loads next" fact
  naming it, with a link to the Saves tab, stopped or running, in a live
  region of its own (`loads`) so a status event never replaces the link.
  `Manager.LoadChoice` reads `world.json` only, so redraws no longer list
  saves. On the Saves tab the Load next buttons sit in their own last
  column, and at phone width each row is a grid with the button on its own
  line. The newest row's button is named "Load next: the newest save".
- **Large view polish (SPEC 5.6, 17.15; DESIGN 8.8).** The Resources and
  Terrain headings are set at `--text-lg` over a rule; a hint at the top of
  the column says "In a setting, the keys change the setting: an arrow moves
  a slider, not the map." Tab wraps inside the dialog in both directions,
  where Chromium's modal dialog let focus leave for the browser. The amounts
  block is shared as `.resource-amounts`. Thumbnail resource labels match
  the territory labels in size, inset and forced colours. A page count that
  finishes or fails while the view is open is said once when it closes. At
  320 px a short "Counting…" shows beside the One planet box (seen, not
  said).
- **Mod-change wording and reads (SPEC 7.4, 9.3, 9.8 to 9.10).** When the
  pre-start refusal carries the same plan as the Server page's mods notice,
  the notice keeps its text and drops its own Update button.
  `server.problem.modlist_space_age_on`/`_off` say enabled/disabled, and
  `settings.preview.undo` says the message after saving links to the
  snapshot. `restoremods.go` reads `mod-list.json` through
  `world.ModListStates`, and `mods.Status` carries the list it read, so the
  status handler does not read it twice. The dead version-slot focus rule in
  `live.js` was removed.
- **Comment cleanup.** About 75 references to issues, rulings, epics and
  review codes in comments and test names now cite a SPEC section or are
  dropped. No behaviour change.
- **Races found and fixed.** The monitor counts each world's operations
  (`worldRun.ops`) and drops a Docker look taken before or during one, so a
  start that finishes between the read and the lock no longer shows as
  stopped (SPEC 8.7; ADR 0016). When Start's container check sees the game
  stop before the log follower reads why, it reads the start's container
  log, so a save that failed to load is reported as such either way (SPEC
  7.3, 8.7; ADR 0016); under load this had failed 53 of 400 runs, 0 after.
  Two tests were fixed at their cause: the sliders "any width" test measures
  each step once (still near its 90 s limit at a load of about 100), and the
  mods-install test holds the install with a countdown so the page sees its
  steps.
- **Spec and process.** SPEC 5.6, 7.11, 8.7, 9.3, 17.7 and 17.15 and DESIGN
  8.3, 8.7, 8.8, 8.11 and the Server page's facts carry the lasting rules;
  ADR 0016 records the two race fixes and ADR 0015 the download wording
  table. The plan was deleted.

### Verified

- Each task: `make verify` and the full `make e2e` on its head before
  merge, plus the browser specs for the pages it touched at 1280 and 320 px
  in both themes. New tests were run against the base first where they
  could fail there (Enter creating the world, the monitor's stale look, the
  failed-load classification, Tab leaving the dialog).
- Real games at 1514442: `make test-game` 391 subtests on 1.1.110, 2.0.77,
  2.1.24 and 2.1.23, and `make e2e-game` 2 passed.
- Real-game recheck at 636bd81, after the Start race fix: `make test-game` passed with no failures (internal/game 1372 s, internal/web 92 s): every line reached InGame with RCON answering, stop, forced stop and re-attach passed, and a corrupt save is reported as "could not load the save" on 1.1, 2.0 and 2.1. `make e2e-game` passed 2 of 2 (phone start, players, restart with warning, stop; switch and back with no save lost).
- Flake loops: `TestStartLoadFailure` 0 of 400 under 24 busy loops with
  `-race` (53 of 400 before); the mods-install test 120 of 120 under load
  (1 of 120 failed before).

### Gaps left

- **Flagged mod rows are not sorted first.** Dropped by the orchestrator:
  the flags arrive after the page loads, and moving rows under the focus
  hurts. Todd may revisit.
- **Mods tab rows on phones.** Disable and Remove shift once when the status
  fills Change version, as they already did for Update. In German and
  French a folder mod's Disable can sit up to about 3em off the others' on
  wide screens.
- No Change version when the mod portal cannot be reached (the tab keeps
  "could not reach the mod portal").
- A chosen save that has gone is silent on the Server page; the pre-start
  check refuses. A choice made in another tab shows on a stopped world's
  Server page only at the next status change or reload.
- The Saves button cell's text is `--text-sm` at 320 px.
- The large view's settings hint is not reached by Tab, and Close is far
  down the view's tab order (Escape closes it). (Closed in fixes batch 9:
  Close comes first in the view's body, and the settings group's name carries
  the hint.)
- The Escape-on-info-tip test moves an Enemy field into the column, since
  no Resources or Terrain setting has a tip yet.
- A world briefly away shows 0 up to date on the Mods tab. (Already fixed by
  #452 before this gap was written; checked in fixes batch 9.)
- If Docker's newest run stops loading for another reason, Start reports
  the earlier run's failed load. (Closed in fixes batch 9, with a remainder
  listed there.)
- **Watch item.** `TestInvariantCrashAtEveryPointOfASwitch` failed once
  under stress loops ("cannot tell its state") and passed 8 of 8 after.
- Still deferred: #353, #339 (P2), #207, #206, #132 and #92, and Enemy and
  Advanced settings in the large view.

## Fixes batch 6

Serves SPEC 5.6 ("Resource amounts", "Patches", "Downloads", "Cost,
measured"), 14.3 and 17.15, with ADR 0015, STACK 1 and 13, and WORKFLOW
"Local development". Issues closed: #460 (downloads with amounts count
what is missing) and #461 (crumbs of ore patches). The pull requests were
#463 (per-planet counts), #464 (Fulgora scrap gap), #465 (ore gap 6 and
label hiding), #466 (download preparation), #467 (ores kept on the
per-tile scan), #468 (failed Vulcanus draw in the large view), #469
(review fixes to the preparation) and #470 (dense counts and the real-game
test set).

Rulings (Todd, 2026-11-10): Fulgora's scrap joins within 16 tiles, other
ores within 6, wells within 24; a label is hidden beside a patch of the
same resource 4 times richer within 64 tiles; Download all counts every
planet missing a count, with progress, Cancel and failure choices. (Todd,
2026-11-11): a count over 400,000 ore tiles joins ores within 2, chosen
over rewording the SPEC's out-of-memory promise; real-game tests run only
the latest patch of each line.

### Delivered

- **Counts and pictures kept per planet (SPEC 5.6).** The preview store
  keys each planet by its share of the settings, everything except the
  other planets' own autoplace controls (`internal/web/genpreviewkey.go`).
  Changing Fulgora's sliders keeps Nauvis's picture and count, and only
  the changed planet is redrawn and recounted. On 2.1.24 with seed 123456
  this left the other planets' pictures and counts byte-identical.
- **Fulgora's scrap joins within 16 tiles (SPEC 5.6, ADR 0015).** Gaps
  live in a Lua table keyed by planet and resource. At 3072 tiles and 600%
  scrap frequency Fulgora has about 300 scrap patches instead of about
  2,300, in 33 s and 675 MiB. A few labels can join neighbouring islands
  across thin channels.
- **Ores join within 6 tiles, and small labels hide (#461, SPEC 5.6).**
  At 3072 tiles Nauvis drops from 138 ore patches to 112, Vulcanus from
  338 to 228, Gleba from 280 to 235, with far fewer crumbs. A label is
  left off the picture when a patch of the same resource with at least 4
  times its amount lies within 64 tiles on both axes, box edge to box edge
  (`resource.HiddenLabels`). Page and downloads hide the same labels; the
  text list keeps every patch.
- **Ores stay on the per-tile scan.** The Lua's per-tile offset scan now
  covers gaps up to 6, the path the cost figures were measured on. Only
  scrap (16) and wells (24) use cell buckets. Output is unchanged.
- **Dense counts join ores within 2 (SPEC 5.6, ADR 0015).** A count with
  more than 400,000 ore tiles in the picture joins its ores within 2. No
  default map comes near it (Vulcanus has the most, 141,912); Nauvis at
  600% has about 1.1 million. At 6 that map ran past the 60 s limit and
  the page said "try again"; now it runs out of memory at about 30 s on
  2.x and says to choose a smaller size, as SPEC 5.6 promises. A unit
  test keeps the threshold 2.5 times away from both measured ends.
- **Downloads count what is missing first (#460, SPEC 5.6, 14.3, 17.15).**
  While "Show resource amounts" is ticked, every download asks for
  amounts: the planet's PNG, the large view's download and the zip.
  Before it starts, the page counts each planet with nothing held, and
  reads territories if none are held, one run at a time through one
  queue, showing "planet 2 of 5", a progress bar and Cancel. A failed
  planet is named with its reason, with "Try again: (planet)" and, for
  the zip, "Download without (planet)". Counts made this way stay in the
  preview store. Closing the large view, showing another planet, changing
  a setting or a new picture arriving ends the preparation and says
  "Download canceled."; a preparation does not start while a redraw is
  waiting, and says why. The server never counts for a download: one that
  asks for amounts or territories not held is refused with 409. The zip
  takes `without=<planet>`. One list of counted planets,
  `data-counted-planets` on `#preview`, replaces the old per-link marks.
- **Speakable states (SPEC 17.15).** Each step is announced politely, and
  "Download started." when it begins. Focus moves to the first choice on a
  failure, to Cancel when another download is clicked or the focused
  button goes, and back to the link when the block closes. In the large
  view the block lies over the top of the stage between Previous and Next,
  so the picture does not move, and no control sits under it at 320 px.
  New strings in all eight languages.
- **A failed Vulcanus draw in the large view** is said once instead of
  being asked for again and again.
- **Real-game test set (STACK 1, 13).** The real-game tests pin only
  1.1.110, 2.0.77 and 2.1.24. `make test-game` checks every image first
  (`game-images`), runs the rest within 30 minutes, then the resource
  tests; `make test-game-resources` runs the resource tests alone within
  90 minutes. The 600% Nauvis count must now end with the out-of-memory
  error on 2.0.77 and 2.1.24, and a forced dense count is checked against
  a scan of every tile. The Generate leftover check counts only its own
  run's containers.
- **Spec and process.** SPEC 5.6 and 17.15 carry the rules; ADR 0015
  records the ore gap 6, the dense exception, the scrap table, label
  hiding and the page-side preparation. The plan was deleted.

How it went: #468 fixed a Vulcanus redraw loop with a line that read a
variable #466 had removed, so every draw threw and the zip held only the
last planet; #469 found and fixed it, and 11 of 12 download tests that
failed on the epic head passed after.

### Verified

- Each task: `make check-go fmt-check vet lint typecheck` and the e2e
  specs it touched in all four projects (download-all 60 passed,
  resources 86, ui-fixes 180, territories 40, large-view 94 at #469).
- `make test-game-resources` on all three lines at #470: passed in 2,650
  s. The rest of `make test-game` passed after the leftover-check fix.
- Measured at 3072 tiles, seed 123456: default maps at most 760 MiB
  (Vulcanus) and 36 s (Gleba) on 2.1.24; scrap at 600% 296 to 306
  patches at 33 s and 675 MiB.
- Real-game recheck on the epic head (code at d3dae03; docs-only changes
  after): `make test-game` passed (the main suite in internal/game 780 s
  and internal/web 146 s, then the resource tests 2,846 s), `make
  test-game-resources` passed on its own for all three lines (2,765 s),
  `make e2e-game` passed (2 tests), and a cross-check on two other
  machines passed per line: 2.0.77 in 645 s and 2.1.24 in 1,253 s, with
  default Nauvis at 3072 giving
  131 patches on 2.0.77 and 130 on 2.1.24, the dense invariant passing on
  Nauvis, Vulcanus and Gleba, and Nauvis at 600% ending with the
  out-of-memory message on both 2.x lines.

### Gaps left

- **No real-game test for the 2.1.9 map-settings layout or for updates
  within a line (SPEC 10.3)** any more, since only the latest patch of
  each line is pinned (Todd's ruling, 2026-11-11). 2.1.24 is checked to
  refuse the 2.1.9 layout's file.
- **1.1 at 600% Nauvis** fits with about 10 MiB to spare; its success is
  logged, not asserted.
- **Forced-colours contrast** of the Generate world button
  (`#generate-button`) in the dark theme is 1.11:1 by axe. It predates
  this batch; the forced-colours scan covers only the preparation block.
  (Closed in fixes batch 13: the primary button takes `ButtonText` in forced
  colours, and a test checks it.)
- **`preview.js` is about 1,300 lines**, past the 800-line split point.
- **Arrow keys in the large view** still step the planet during a
  preparation, which ends it (SPEC 5.6). (Closed in fixes batch 9: the
  arrow keys wait while a preparation is shown, SPEC 17.1.)
- Aquilo has only wells, so the ore gap and label hiding do not change it.
- No real-game check of time and memory for five counts in a row.
- DESIGN.md does not yet describe the preparation block. (Closed in fixes
  batch 9: DESIGN 8.8.)
- Still deferred: as listed under fixes batch 5.

## Mod settings epic

Serves SPEC 9.6, with 9.13, 9.15, 14.3, 14.6, 17.10 and 22.3, and ADR 0017.
Issue closed: #339. The pull requests were #479 (contract), #478 (test
data), #480 and #483 (fake game), #482 and #488 (codec and bounds), #481
(world layer), #485 (definitions run), #486 (page) and #484 (real-game
tests).

Rulings (Todd, 2026-11-11): Foreman does not change map settings on a world
that has a save, and runs no Lua or RCON for them; editing is P1; the
definitions run has a 4 GiB memory limit and a 5-minute time limit.

### Delivered

- **Codec (`internal/modsettings`).** Reads and writes `mod-settings.dat`
  byte for byte: any file it accepts encodes back to the same bytes, on the
  files written by 1.1.110, 2.0.77 and 2.1.24. An edit changes only the
  edited value nodes; a missing entry is copied from the game's defaults
  file. Reads are bounds-checked, nesting is capped at 32 levels and memory
  follows the entries actually read. Fuzzed (decode, definitions and locale
  parsers, 10 to 30 s each) with no findings.
- **Definitions and locale parsing.** The definitions parser streams the
  settings, stops past 10,000, and marks a setting that does not fit its
  type, or has an unknown type, as not editable instead of failing the file.
  An int bound at or past the 64-bit range, which is how the game writes
  "no limit", means no limit (#488). The `.cfg` parser keeps values as
  written; the eight Foreman languages map to Factorio's locale codes.
- **Value checks.** Every changed value is checked by Foreman (SPEC 9.6):
  setting type, hidden, locked, bool, whole or finite numbers within
  limits, allowed values, strings (UTF-8, no NUL, 10,000 bytes, `auto_trim`
  first, blank only when allowed) and colors. An untouched value is never
  checked or rewritten.
- **World layer (`internal/world`).** `ModSettingsKey` (game version, Space
  Age flag and the mods fingerprint without `mod-settings.dat`),
  `LinkModsInto` (hard links, copies when linking fails, at most 100,000
  files and 4 GiB, symlinks and odd file types refused), `ModLocaleFiles`
  (newest zip or folder mod, 1 MiB per file, 8 MiB per language), and the
  `SettingsSeen` guard in `ApplyModsHeld` and `CheckModChange`.
- **Definitions run (`internal/game`).** `Controller.ModSettingDefs` runs
  the world's own image with a control-only mod as a fourth kind of preview
  container (SPEC 14.3), reads the definitions, the defaults file and the
  locale text from regular files only, and keeps them in memory per world
  (at most 8 worlds), keyed by `ModSettingsKey` and read before and after
  the run. Callers for one world share one run. Failures are four typed
  errors the page maps to messages.
- **Fake game.** The fake game runs the same kind with canned definitions
  (the `fmtest` mod's settings, a hidden one, a forced one and one of an
  unknown type, 18 in all, all matching the embedded defaults file), and
  `POST /preview-mode` accepts `oom` and `broken` beside `fail` and `hang`.
- **Page (SPEC 9.6, 9.13, 17.10, 22.3).** `GET /worlds/{id}/mods/settings`,
  linked first in the Mods tab's Mod settings panel; the form loads after
  the page, with a progress line, because a cache miss runs the game.
  Settings are grouped by mod; controls fit the type; labels come from the
  mod's locale in the page language, else English (marked `lang="en"`),
  else the setting name. Saving goes through the mod change (9.13); a refused
  value comes back in its field; a stale file gives 409 with the fresh
  form. Map settings are editable only with no save, and otherwise read-only
  with the line about the game's Map tab. Run failures show a message with
  retry, the file download and the upload. Eight languages; a click-budget
  row of 4.
- **Real-game tests.** `TestRealGameModSettings{Definitions,File,Apply}` on
  1.1.110, 2.0.77 and 2.1.24, using the extended `fmtest` mod: definitions
  match, a game-written file round-trips, a startup edit takes effect with
  per-player bytes and a 300-byte string intact, and the game's own rules
  (the save's map value wins, no rewrite at exit, a bad value is reset).

### Gaps

- The definitions container can write the world's hard-linked mod zips, as
  the main game container already can. (Closed in fixes batch 9 for the
  definitions container, which now gets the zips read-only; the main game
  container is still open.)
- Editing files inside an unpacked folder mod does not change the cache key.
  (Closed in fixes batch 9.)
- Copying a folder mod, or a zip when linking fails, reserves no disk space
  (it is capped at 4 GiB and 100,000 files).
- In the error summary at the top of the page, an English setting name is
  not marked `lang="en"`; the summary is in the shared layout template.
  (Closed in fixes batch 9.)
- The file name `mod-settings.dat` may wrap at its hyphen at 320 px.
- Gate B (SPEC 19.2) is still owed.

## Epic 9: Live operations

Serves SPEC 7.12, 8.2, 8.3, 8.5, 8.7, 8.8 and 8.10 to 8.13, with 12.9, 14.8
to 14.10, 17.10 and 22, DESIGN 8.12 to 8.19, and ADR 0018 (webhooks) and ADR
0019 (the scheduler). Issue closed: #462. The pull requests were #503 to
#520.

Rulings (Todd, 2026-11-12), now in SPEC: VPN sidecars moved to Epic 9b (4.13,
18.9b); the restart schedule waits for an empty server, then counts down 60
seconds (7.12); the skip limit is 0 to 12 hours, default 1; one schedule for
all of Foreman; webhook addresses are sealed (14.8); low disk warns below 4 GB
and clears above 4.5 GB (8.13); the language is chosen per webhook; the
whitelist is enforced by a per-world switch (8.3), because a real game showed
that an in-game `/whitelist enable` does not survive a restart; a live player
action takes no snapshot; only kick and ban confirm (8.2); Chat has its own
tab (8.5); an `http://` webhook address is allowed, with a warning (8.11).

### Delivered

- **Real-game captures (#504).** Transcripts from 1.1.110, 2.0.77 and 2.1.24
  in `internal/game/testdata` (README section 10): the replies to kick, ban,
  unban, promote, demote and the whitelist commands, which list files the
  game writes and when, `[CHAT]` lines from the server, a mod that fails to
  load, Docker's port-in-use start error, and an out-of-memory start under a
  memory limit. What they showed: an in-game `/whitelist enable` is lost at
  restart on all three; RCON strips leading spaces, so `" /c ..."` and `"
  /ban x"` run as commands; the game checks no player name; 2.x folds
  accents in the admin list and 1.1 does not; 1.1.110 skips a mod with a
  missing dependency and starts, where 2.x exits 1; and `--server-whitelist`
  without `--use-server-whitelist` makes the game exit 1.
- **Design (#503).** DESIGN 8.12 to 8.19: player row actions, the ban list
  and whitelist, Chat, the restart schedule, notifications, the low-disk
  notice, the click budgets and the new CSS.
- **Scaffold (#505).** The Chat tab, the Notifications and Restart schedule
  links in the user menu, the route files, the no-op `notify` and
  `schedule` packages, and the empty `players`, `chat`, `crash`, `notify`
  and `schedule` locale areas in all eight languages.
- **Fakes (#506, #515).** The fake game answers the player and whitelist
  commands byte for byte as captured, strips leading spaces as RCON does,
  writes the ban and admin lists at once, keeps the whitelist in memory unless
  both whitelist flags are set, refuses `--server-whitelist` alone, can say
  a chat line, and can fail with port in use or out of memory. Player names
  match case-sensitively there (case was not captured).
- **Notifications (#507, SPEC 8.10 to 8.12, ADR 0018).** `internal/notify`
  with Discord, ntfy and generic JSON presets (body version 1, pinned by a
  golden file), eight event ids, a queue that never blocks, a sender and
  queue of 20 for each webhook, 5-second attempts with two retries, and no
  redirects. Addresses are sealed with `FOREMAN_SECRET_KEY` in migration
  0006, written by a page at `/settings/notifications` that shows only the
  preset and host, and every add, change and delete is audited without the
  address. An address that cannot be opened shows as unreadable.
- **Crash catalogue (#508, SPEC 8.7, 8.8).** `internal/game/crashreasons`
  with an embedded `catalogue.json` (`mod-missing-dependency`, `mod-error`,
  `save-unreadable`, `port-in-use`), plus `out-of-memory` from Docker's
  `OOMKilled` flag and `mods-for-other-version`. `LoadReason.Kind` and
  `Status.Crash` carry the id. `make test-catalogue VERSION=x.y.z` runs four
  scenarios on one pinned image, and `make test-game` runs it for each line;
  all four passed on each of 2.1.24, 2.0.77 and 1.1.110.
- **Players and lists (#510, SPEC 8.2, 8.3).** `Controller.Player` sends kick,
  ban, unban, promote, demote and whitelist add and remove over RCON. Names
  and reasons are refused, never escaped; each action is audited before it is
  sent; no snapshot is taken. `POST /worlds/{id}/players/{action}` and a
  confirmation page for kick and ban. The ban list and whitelist are on the
  Settings tab, as live cards while the game keeps the list and as form
  fields with a snapshot otherwise. The "Only whitelisted players can join"
  switch is `use_whitelist` in `world.json`; on, the game gets both
  whitelist flags; off, neither. A container label tells whether the running
  game keeps the whitelist. A real-game test of the switch
  (`TestRealGameWhitelistSwitch`) covers a missing file and a file kept
  across a restart; enforcement itself needs a client.
- **Chat (#509, #520, SPEC 8.5).** The Chat tab shows the last 200 `[CHAT]`
  lines live and sends a message as the server. A message is refused if it
  starts with `/` after any blank, control or invisible character, or has any
  control character, and it is audited first. Rows are drawn by one
  template for the page and the stream, with the time, the name and the text,
  and the server's own lines marked.
- **Events and low disk (#511, SPEC 8.10, 8.13).** `Controller.SetNotifier`
  and a non-blocking emit send start, stop, crash (with the reason id),
  crash loop, join, leave and low disk. After a Foreman restart nothing is
  sent for something that happened earlier. A port-in-use start names its
  reason from Docker's error. Real-game test `TestRealGameEvents` on 2.1.24:
  a start, a stop and a corrupt-save crash each sent one webhook, the crash
  loop one more, and two Foreman restarts sent no duplicates.
- **Scheduled restart (#513, SPEC 7.12, ADR 0019).** `internal/schedule` and
  migration 0007: one schedule on `/settings/restart-schedule`, a wait for
  nobody online, a 60-second countdown that always runs, a skip at the limit
  or on an admin's cancel, no catch-up, and the daylight-saving rules. The
  controller's `StopOptions` gained `WarnEmpty` and `OnlyRunning` for it. The
  Foreman settings sub-navigation is one shared template.
- **Server page (#519, SPEC 7.12, 8.2, 8.7, 8.13).** Player row actions with
  Undo that survive the live updates, the crash reason and port-in-use in
  plain words (new `crash` area), the low-disk warning with a Snapshots
  link, and the next restart, the wait, the countdown and the skip, live.
  The Chat tab's not-running note follows the game. The epic's CSS, and a
  `--target-inline` token for 24 px links.
- **Operator guide (#512).** OPERATOR sections 12 to 14: webhooks with the
  generic body, low disk, and the restart schedule; "When something fails"
  is section 15.
- **Click budgets (SPEC 17.10), measured:** make admin 2 (with Undo 3), kick
  3, ban 3, whitelist add 2, change the schedule 2.

### Fixed on the way

- **#514, `TestInvariantCrashAtEveryPointOfASwitch` and
  `TestInvariantSwitchKeepsRunState`** (closes #462). The tests started a
  switch before the monitor's first look, so the switch was rightly refused
  as unsettled. The rig now waits. Main had the same flaw.
- **#516, the any-width slider test.** One test walked 22 widths and ran
  out of its 90 seconds on main as well. It is now one test per language and
  width. Nothing it checks was removed.
- **#517, the world page.** It read "is an action running?" and then the
  update result, so an update that ended between the reads showed the result
  and also reloaded the page away from it. It reads the result first. Main
  had this too.
- **#518, the Mods tab** had the same race and the same fix, and the
  `WarnEmpty` test waited on the status instead of the warning.
- **#520, integration review.** Chat rows follow DESIGN 8.14; the dead
  `.name-list` CSS is gone; `CheckChat` refuses every control character.

### Verified

- Each task ran the Go tests and e2e specs it owns; its pull request body
  has the commands. The web package takes longer than the tool limit, so it
  was run in `-run` slices. Mutation and before-and-after checks are recorded in #511
  and #514 to #518.
- **Real games.**
  - `TestRealGameWhitelistSwitch` passed on 2.1.24, 2.0.77 and 1.1.110 when
    the players task was checked, and again in `make test-game` on the epic
    head ad4cd32. Phase 1 of that run passed every `TestRealGame*` test,
    among them `TestRealGameCatalogue`, `TestRealGameWhitelistSwitch` and
    `TestRealGameEvents`. Phase 2, the older `TestRealGameD2Resources`, was
    cut off by the harness's 30-minute cap; it predates Epic 9.
  - `make e2e-game` passed.
  - `make test-catalogue` passed on 2.1.24, 2.0.77 and 1.1.110.
  - `TestRealGameEvents` covered a start, a stop and a corrupt-save crash,
    each sending one webhook, with no duplicates after a controller restart.
  - Not run on a real game: the scheduled restart, the chat `/` refusal, and
    anything that needs a client.
- The scheduler's invariants run against a fake clock, and
  `TestWithTheController` runs it against the real controller on the fake
  game.

### Hand checks for Todd after the epic PR

1. A banned player, and a player who is not on the whitelist while the
   switch is on, are refused.
2. Joining and leaving each send one webhook.
3. A client's chat line appears on the Chat tab, and its log becomes
   `internal/game/testdata/2.1.24/chat-client.log`.
4. A scheduled restart waits while he is online and runs after he leaves.
5. A chat message that starts with `/` does not run and leaves achievements
   enabled.

### Gaps left

- **Real-game gaps.** No `[CHAT]` line from a real client was captured, so
  the client line is read like the server's. Joins and leaves, and a
  whitelist refusing a player, were not checked with a real client. Also
  uncaptured: the log lines when an online player is kicked or banned,
  `/players` with players listed, and a trailing newline in the list files.
- **Crash and OOM.** The game container has no memory limit, so a host-wide
  OOM kill shows as unknown. An OOM kill that Docker restarts before the
  monitor sees it also shows as unknown, because Docker clears the flag. 1.1
  gives no reason for a missing dependency, since it starts without the mod.
- **Events.**
  - Events that happen while Foreman is down, or before its first look, are
    not sent. That is deliberate, so nothing is sent twice.
  - A `docker restart` outside Foreman sends a start with no stop.
  - "address already in use" is not named port-in-use; only "port is
    already allocated" is.
  - A crash whose reason reaches the log more than two polls late is sent as
    unknown.
  - Docker's log times are compared with Foreman's clock, which assumes one
    host; a remote Docker with a fast clock could repeat a join or leave.
  - A hanging target loses its own events once its queue of 20 is full.
- **Chat.**
  - Each Chat tab opens a second live connection, so three Chat tabs use
    all six HTTP/1.1 connections to one host.
  - A mod's `print()` can fake a `[CHAT]` line.
  - Text typed while a chat answer is in flight can be lost.
- **Schedule.**
  - The time zone defaults to UTC; DESIGN 8.15 asks for the viewer's zone,
    which needs a script that was not written.
  - A scheduled restart that fails sends no webhook; the failure shows only
    on the Server page and in the log.
  - An admin's cancel, which skips the day, sends no webhook, and neither
    does a missed one (the message names players as the reason).
  - A scheduled restart sends both the scheduled-restart and the start
    webhooks. Not a gap: SPEC 7.12 says the restart's own start sends the
    start webhook too (ruled in fixes batch 9).
  - No "restarted on schedule" Done message: the scheduler keeps no
    field for it.
- **Audit.** Schedule and webhook saves are audited after the change, and a
  failure to audit is only logged. Player actions, chat messages and the
  whitelist switch are audited first and refused if the audit fails.
- **Server page.** Low disk has no e2e test, since Foreman reads the real
  disk and `game` has no hook for it; the Go render tests cover it. The
  player lists are read, one Docker inspect per update, only while someone is
  online.
- **Not built (planned).** A test-send button, signed webhook bodies, days of
  the week for the schedule, "skip tonight", and undelivered events kept
  across a Foreman restart.
- Epic 9b (VPN sidecars) was not built yet; it is now (see Epic 9b).
- Gate B (SPEC 19.2) is still owed.

## Fixes batch 7

Serves SPEC 5.6, 6.11, 7.1, 7.12, 17.1, 17.2, 17.10 and 22, with DESIGN 6,
8.8, 8.15, 8.16 and 8.18, STACK 3 and ADR 0020. Issues closed: #523 (the
schedule's time zone), #496 and #487 (the upload reserve test), #495 (the
Docker outage test), #499 (territories in the large view), #497 (language
and theme choices, About) and #524 (a Settings tab). The pull requests were
#526 (#495), #527 (#523), #528 (#496, #487), #529 (#499), #530 (#524 and
the language and theme rows of #497) and #532 (About).

Rulings (Todd, 2026-11-12): the user menu loses its settings links, since
the new Settings tab is their one home; the language stays in the user menu
as one row that expands, with a flag and the language's own name; theme is
one row of three icon buttons; English shows the United Kingdom's flag and
Spanish shows Spain's, from bundled flag-icons SVG files (ADR 0020).
Ruling 2 replaced #497's request for a language picker on the header bar:
nothing new went on the header. Ruled by the orchestrator: the Settings tab
is last in the main navigation and opens Restart schedule; the world's
"Settings" tab is now "Game settings"; About shows the product, version,
commit, Go version, licence and a project link, and no game versions.

### Delivered

- **The Docker outage test was a race (#495, SPEC 7.1).** A check could list
  the containers, fail to inspect one, and set the "cannot read the game
  container" reason; the next check's failed listing then replaced it with
  "cannot reach Docker". The old test stopped waiting as soon as the state
  was unknown and so sometimes read the brief inspect reason. The monitor is
  unchanged. `TestDockerUnreachable` now waits for the SPEC 7.1 reason, and
  `TestDockerDownAfterInspectFailure` forces the order and proves the
  outage replaces the inspect reason.
- **The upload reserve test reads a fixed free-space figure (#496, #487,
  SPEC 6.11).** `world.SetFreeBytesForTest` fixes the reading at 128 MB for
  a test and returns a restore function; production reads the disk as
  before. `TestDiskFullMidUpload` runs on its own tiny mount and was left
  alone.
- **A never-saved schedule preselects the browser's time zone (#523, SPEC
  7.12, DESIGN 8.15).** A small script, `schedule.js`, selects the zone from
  `Intl.DateTimeFormat().resolvedOptions().timeZone` when the list has it.
  A saved schedule keeps its zone, a refused form keeps the posted zone, and
  without script the page offers UTC. This closes the Epic 9 schedule gap.
- **The large map view has the territories box (#499, SPEC 5.6, DESIGN
  8.8).** It is the page's box seen from the view, not a second setting, and
  is shown only while Vulcanus is viewed. It holds across Previous, Next,
  All planets and closing. It is disabled while Vulcanus is still being
  drawn, says "Finding the demolisher territories…" while it looks, and says
  the counts or the failure in the view's status line.
- **A Settings tab (#524, SPEC 17.10).** It is last in the main navigation,
  opens Restart schedule and is marked current on the schedule, account and
  notifications pages and the webhook pages under them. SPEC 17.10 counts
  "Set a nightly restart" and "Open Notifications" at 2 clicks each, and
  click-budgets.spec.ts tests both.
- **"Game settings" (SPEC 22, I18N.md).** The world's tab and the Server
  page's link to it carry the new name in all eight languages, so no page
  has two navigation links named "Settings".
- **The user menu (#497, DESIGN 6, SPEC 22.2).** Rows: "Signed in as", a
  folded language row (a nested native `details`, the current flag and name;
  opened, the other seven as submit buttons), the theme row (a `fieldset`
  of Auto, Light and Dark icon buttons, each with a hidden name and hover
  text, 44 px targets), About and Sign out. It works without script. Escape
  inside the open language list folds it first, then closes the menu. The
  menu's accessible name now says "user menu" in all eight languages.
- **Flags (STACK 3, ADR 0020).** flag-icons 7.6.0, eight 4x3 SVG files under
  `internal/web/static/flags/` with their licence and `SHA256SUMS`, checked
  by `TestFlagChecksums`. The static handler serves `.svg` with a `sandbox`
  content security policy and does not serve the licence or the sums.
- **About (#497, DESIGN 6).** An item in the user menu: a link to `/about`,
  and with script a native modal dialog that traps focus and returns it.
  `internal/version` reads the version and commit from `-ldflags` (set by
  the Makefile, the Dockerfile and the CI image build), else Go's build
  info, else "development build". The Version shows only for a release tag;
  a commit built from a changed tree is marked "(modified)".
- **Narrow screens.** At 320 px the four main tabs draw tighter and a step
  smaller so they keep one row in English and German; under 22rem the open
  menu takes the window width less 0.5rem each side, so language names keep
  their line at 200% text.
- **Spec and process.** SPEC 5.6, 17.10 and 22.2, DESIGN 6 and 8.8, STACK 3
  and ADR 0020 carry the rules. The plan was deleted.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck` and the specs
  it touched in all four projects. The flake tests passed their repeats:
  `go test -race -count=50` on the two Docker tests, and `-count=20` on the
  upload reserve test.
- Task 5 ran the menu, header, settings, i18n and click-budget specs in
  all four projects (1,314 tests passed) and `go test ./internal/web/`
  in four parts (678 tests).
- Real-game checks: none needed; no task touches server control, world
  switching, snapshots, mods or game versions.

### Gaps left

- **The large map view has no "Try again" for a failed territory request
  (#499).** Unticking and ticking the box again retries. (Closed in fixes
  batch 9.)
- **The theme buttons' names show only to screen readers and as hover text.**
  Keyboard and touch users see bare icons (ruling 2a).
- **The language is in the user menu, not on the header bar.** Ruling 2
  replaced #497's request for a header picker.
- **Two navigation links named "Snapshots" on a world's pages (#531).** The
  main tab and the world's tab share a name with different targets. It
  predates this batch; user-menu.spec.ts excludes that one name.
- **Spain's flag is 81 KB** (the coat of arms); every other flag is under
  1 KB. The seven flags in the language list load lazily, so the file is
  fetched only when the list opens, or at once when Spanish is the current
  language.
- Still deferred: as listed under fixes batch 6 and Epic 9.

## Fixes batch 8

Serves SPEC 17.1 and 17.4, with DESIGN 6, 7.4, 7.5 and 8.3. Issues closed:
#531, #537, #536, #535 and #475. The pull requests were #540 (#475) and #541
(#531, #537, #536, #535 and the Server page order).

Rulings: a wide-screen Tab order that differs from the drawn order of two
independent cards is acceptable (WCAG 2.4.3 passes). The fold changed DESIGN
7.4 to match the CSS and added a sentence to SPEC 17.1.

### Delivered

- **A world's Snapshots tab is named "Snapshots of this world" (#531, SPEC
  17.1).** The visible word stays "Snapshots"; the name starts with it, in
  all eight languages. No page now has two links with one name, and
  user-menu.spec.ts no longer excludes it.
- **The header says the server's state in words at every width (#537, SPEC
  17.4, DESIGN 7.5).** First built under "Server" in its tab on wide
  screens; the fixes batch moved it beside the logo on every width. It is
  hidden from screen readers there, since the Server link already carries
  "Server Running". The header height
  does not change with the state; German and Russian were checked from 320
  to 1440 px.
- **Delete selected is one Shift+Tab from the first snapshot box (#536,
  DESIGN 8.3).** The selection bar comes first in the code and is drawn at
  the foot with CSS `order`. "Show older" stays below it.
- **About shows the rusty wordmark (#535).** Dialog and `/about` use
  `wordmark-480.webp`; Foreman now serves `.webp`.
- **Server page order.** The pages card comes before the players in the
  code; the CSS order rules are gone.
- **The crash-loop retry test flake is fixed (#475).** It was a race in the
  fake Docker (`internal/fakegame`), not in Foreman: a game that died during
  a refused stop was never restarted. The fake now schedules the restart.
- **Each planet with two or more resources has an "All resources" row at the
  top of its Resources list (SPEC 5.6, 11.5; DESIGN 8.7).** It has Frequency,
  Size and Richness with the resources' own steps. Moving one sets it on every
  resource that is switched on, through each resource's own select, so
  sliders, warnings, "[modified]" and the preview follow. It shows "Mixed"
  while the resources differ. It needs script and sends nothing; the
  checkboxes and the server's form handling are unchanged.
- **The Server page's Done box has a close button (SPEC 17.9, DESIGN 7.3).**
  It shows after a success or a cancelled stop, never after a failure.
  Closing forgets that world's held result on the server (only when it is
  still the result the page showed), which also drops its Undo or Switch
  back offer; the switch record is untouched. It works without script (a
  form that posts and redirects) and with script removes the box and moves
  focus to the first control of the panel.

### Gaps left

- On wide screens (64rem and up) Tab reaches the pages card (drawn right)
  before the players (drawn left). Ruled acceptable.
- At 320 px the Russian word beside the logo breaks inside the
  word, since the browser has no Russian hyphenation.
- Between 40 and 50rem in German the header wraps to a second row, as it did
  before.
- The Generate phone map disclosure is still unbuilt. There is no issue; it
  awaits Todd.
- Five translations still need a native check: French and Spanish whitelist
  enforcement, Czech Undo, Chinese Jump to section, Polish More actions.

## Epic 9b: VPN sidecars

Serves SPEC 4.3, 4.4, 4.6, 4.13, 12.6, 14.3, 14.16 and 18.9b, with 8.4 and
22, and ADR 0006. Issues closed: none (none concerned sidecars). The pull
requests were #559 to #568, followed by #569, #572, #573 and #574,
with the contract in #555 to #558. Built across
three machines.

**Not fully accepted.** The code is built and tested as listed below, but
the live VPN and reboot checks were not run (see "Remaining acceptance").
All automated gates below passed. Todd deferred live acceptance on
2026-11-14 and asked that the epic pull request remain draft.

Rulings (Todd, 2026-11-14), now in SPEC 4.13, 12.6 and 14.16 and ADR 0006:
shared sidecars are allowed, on additional Docker networks too, and an
unrelated service there is no reason to refuse; the sidecar's lifecycle is
the operator's, with no watcher and no automatic recovery; the game joins
the checked container ID and declares no port, and the sidecar publishes
UDP 34197 so LAN and VPN players play together; RCON stays on the sidecar's
private game-network address; Tailscale in kernel mode is the reference
setup, while Foreman stays independent of the VPN provider; reboot recovery
must be tested on a disposable host, and a failure goes to Todd rather than
being waived or fixed by auto-start.

### Delivered

- **The game runs in the sidecar's network (SPEC 4.13, 14.16, ADR 0006;
  #560, #563).** With `FOREMAN_GAME_NETWORK_CONTAINER` set, every start
  looks the sidecar up by name, checks it, and creates the game with
  network mode `container:<inspected ID>`. A container made later under the
  same name is never joined, and a failed create or start falls back to
  nothing. The game declares no exposed or published port. Start, restart,
  switch, version change and mod changes all run the check before any world
  change, so a refused start writes no mod list and takes no snapshot. The
  sidecar must be running, on `FOREMAN_GAME_NETWORK`, and not on the host
  network or inside another container's. The old startup refusal is gone.
  Container inspect gained the environment, network mode and ports. The
  environment and full inspect result are never put in an error or log. No database, API, endpoint or
  dependency changed.
- **Shared sidecars (SPEC 4.13).** Every network the sidecar is on has its
  IPv4 and IPv6 ranges checked: an unreadable range, an overlap with
  `FOREMAN_TRUSTED_PROXIES` and a range holding the address a `tcp://`
  `DOCKER_HOST` reaches each refuse the start. Extra networks and unrelated
  services on them do not.
- **RCON is private to the sidecar's game-network address (SPEC 4.13, 14.16;
  #563).** The game gets `FOREMAN_RCON_BIND=<IPv4>:27015` and the start
  script passes `--rcon-bind` in place of `--rcon-port`. One path resolves
  the address for the first connection, each reconnect and a Foreman
  restart. It inspects the container the game's network mode names, by
  ID, and trusts the bind only when it is that container's exact address
  on the game network and port 27015. Each new RCON connection runs the
  check again after it is made and before the login (`rcon.NewChecked`).
  When Docker reports the original holder gone or its address changed, the
  login is refused; this remains a point-in-time check. Ordinary mode is unchanged.
- **The sidecar's ranges protect trusted-header sign-in (SPEC 12.6;
  #563).** They are read before the first game starts and on every monitor
  look, with Foreman in or out of a container, and again after a Foreman
  restart. They count as known only when the sidecar and every game
  container were read in full, an empty network list on a game proves
  nothing, ranges learned earlier survive a failed read, and a game in a
  since-replaced sidecar's old namespace contributes its ranges too. Before
  the first complete discovery, unreadable ranges keep sign-in refused;
  later read failures retain previously learned ranges and knowledge.
- **The Server page warns, without blocking, when LAN players cannot join
  (SPEC 4.13; #563, #564).** When the sidecar has no host binding for
  `34197/udp`, the start goes ahead and the page warns while the game
  runs, in the low-disk warning's live region and in all eight languages.
  The warning is read at start or when a running game is taken on, and is
  not refreshed while it runs. The eight refusals are worded in all eight
  languages and name only the configured container.
- **A running game with no RCON client says so (SPEC 8.4, 4.13; #568).**
  After a Foreman restart, when the sidecar the game joined is gone, the
  Console, Players and Chat now say the game cannot be reached over RCON
  instead of "not running". A game that is not running or not yet in game
  still says "not running".
- **A forced stop reports one stop (SPEC 7.7, 8.10; #562).** Found by the
  epic's full verify: a monitor look read just before Force stop's kill
  could be taken in afterwards, so the game read as Stopping and sent a
  second stop event. `refresh` now drops any look read before the kill.
- **Test fakes (#559, #566).** The fake Docker models `container:<ref>`:
  create refuses ports and exposes, start needs a running holder, and an
  old ID cannot join a replaced name. The fake game takes `-sidecar` and
  `-sidecar-port`, binds RCON only at the sidecar's address, and keeps no
  RCON after a sidecar restart until it restarts itself.
- **Browser tests (#564, #572).** `e2e/tests/sidecar.spec.ts` covers a published
  and an unpublished start, the Console, the warning following a change
  live in a second tab, missing and stopped sidecars, no script, 320 px
  and axe in four projects, and that the sidecar's secret never reaches a
  page or the log. Sidecar restart recovery is covered in all four projects:
  graceful Restart refuses without creating a container, then Force stop
  and Start restore the Console. The shared accessibility helper waits for
  animation state rather than promises Chromium can leave unresolved in
  closed details elements.
- **Real-container tests (#565).** `internal/game/sidecar_game_test.go`
  runs under `make test-game` on 1.1.110, 2.0.77 and 2.1.24, with a
  game-image container as the namespace holder on a temporary game network
  and a second shared network with an unrelated service. Docker accepts the
  generated spec; the game reaches InGame; the only listener on 27015 is
  the holder's game-network address, refused on the holder's other
  interface and on loopback; the console works; a Foreman restart
  re-attaches; a graceful stop saves; restart and a switch to another world
  and back work; five refusals happen before any container exists. The
  same-address trap (a replacement at the old address) receives 0 bytes
  through both the running controller and a restarted Foreman.
  Measured on Docker 29.1.3, identical on all three lines: a holder
  restart keeps its ID and address, and the running game then sees only
  loopback; a replacement gets a new ID at the same address, and the game
  stays in the orphaned namespace with the old ID. In both, the Console and
  a graceful stop fail, nothing is saved or reported stopped, and Force stop
  followed by a start recovers.
- **Operator guide and Tailscale recipe (#561).** OPERATOR section 16 and
  `docs/examples/tailscale-sidecar/compose.yaml`: kernel networking, a state
  volume, `/dev/net/tun`, `NET_ADMIN` and `NET_RAW` on the sidecar only, the
  image pinned by tag and index digest, and a tag with a grant for
  `udp:34197`. Sign-in is interactive or by an auth key mounted as a file
  into the sidecar alone. It covers LAN and VPN play together, what a
  published port cannot prove, RCON's private bind with checks that must
  fail, what the game inherits from additional networks, never trusting a
  VPN address or range as a proxy, and manual recovery after a sidecar
  restart. The README links it. `docker compose config` passed; nothing was
  started.
- **Failure diagnostics for browser tests (#567).** A failing e2e test keeps
  a Playwright trace and the last 256 KiB of Foreman's output with secret
  literal credential values of at least four characters redacted, and CI
  uploads them for seven days. Browser traces are not redacted and must
  contain only the test fixtures' synthetic credentials. This explains
  failures; it fixes none.

- **Crash-loop fixes found by the gates (#569, #573, #574; SPEC 8.7–8.9,
  ADR 0016).** The OOM test restarts under both memory limits and tolerates
  a failed exec only after Docker confirms that run ended. The monitor
  orders exit evidence by Docker's exit time, captures the OOM flag at log
  EOF, and reads a bounded log tail on a restart. Older evidence cannot
  overwrite newer exits; a newer run's explicit reason can supersede an
  older OOM kill. Events wait for the crashed run's reason when only an
  earlier follower supplied one. Crash-loop stops give Docker 10 seconds
  before killing a game that hangs during loading; admin stops retain
  their 120-second save grace.

### Evidence

Epic head `70476fb8` has exactly the verified candidate `60206373`'s tree,
`747d02dd8b26b43bf5c83b8a35dd6cf2a09c2ed7`:

- Full `make verify`: passed, 90.9% coverage across `internal/`.
  Test counts: game 413, world 304 and web 694; race detector and lint pass.
- Full fake-browser suite: 3082 passed, 254 skipped, 0 failed or flaky.
- Full `make test-game`: passed, including sidecar lifecycle, crash
  catalogue, crash-loop stops, web and resource tests on the pinned
  1.1.110, 2.0.77 and 2.1.24 images. Four existing resource-label checks
  are skipped.
- Full `make e2e-game`: 2 passed.
- Code and security reviews covered Docker, trusted headers, RCON and the
  crash-loop changes; accessibility and translation reviews covered the
  sidecar UI. All blocking findings were corrected and rechecked.

### Remaining acceptance

Not done, and not to be read as passed:

- **No live Tailscale test has run, and no client has played.** The pinned
  `tailscale/tailscale:v1.104.7` image has not been run against Foreman. pve has no
  `/dev/net/tun`, so a kernel-mode sidecar needs another host or TUN
  passthrough. A real VPN, routes and the concurrent LAN and VPN players
  are unproven: the real-container tests use a `sleep` container as the
  namespace holder, and the fakes model no routing.
- **Reboot and Docker-restart recovery is unmeasured.** Section 16's reboot
  paragraph is cautious wording, not a result.
- **One timeout under load.** The first run of the map-settings test
  timed out waiting for Docker at load 27; the focused rerun and the whole
  map suite (59 tests) passed. Not reproduced, not explained.
- **A rare CI chat failure is unexplained.** `chat.spec.ts:146` once timed
  out in CI (run 43638303346) on the first slash-message submission. 250
  local repeats pass, and the cause is unknown. #567 only adds diagnostics,
  and its artifact upload had not run on GitHub when it merged.

The live acceptance, to run on a separate test stack with a disposable world
and never on Todd's active game. Take any credential through the secret tool
at the time; no authentication value goes into the repository, a pull
request, command output or notes. Record the image tags and digests, the
Docker version, the topology and each result:

1. Start through Foreman; the Console and Players work.
2. A LAN client joins the published host address while a client on another
   network joins the sidecar's Tailscale address. Both play at once.
3. RCON works from Foreman, but connections to TCP 27015 on the LAN host
   address and the Tailscale address fail. Advertise no route to the Docker
   game subnet and forward no Tailscale traffic to the private RCON address.
4. Recreate the test sidecar without the published port and restart the
   game: the warning appears, remote VPN play works, and LAN play does not.
5. Restart the sidecar, then separately recreate it, while the test game
   runs. Write down the symptoms, restart the game through Foreman, and
   confirm both clients rejoin. Expect no new sidecar warning.
6. Restart Foreman only: the game stays up and RCON re-attaches. Then save
   and stop gracefully, restart, switch worlds and switch back.
7. Test the trusted-header boundary. Limit the sidecar's VPN access to the
   peers and services it needs by a tailnet policy; no Docker API or proxy
   service may be reachable through the sidecar.
8. On a disposable host, restart Docker or reboot with the game and sidecar
   running; record the recovery order and when the game and RCON are ready.
   If recovery fails, bring the failure to Todd; do not waive the vision's
   principle 2 or add auto-start. Never restart Todd's own host for this.

Afterwards correct OPERATOR section 16 to the measured behavior, and
optionally try Tailscale's userspace mode as an exploration only: it is not
a second promised configuration.

### Gaps left

- **No sidecar watcher, warning or automatic recovery, by design (SPEC
  4.13).** After the sidecar is restarted or replaced, a running game keeps
  showing Running on the Server page while it has no RCON; the operator
  uses Force stop, which saves nothing, and starts it again. This holds
  after a Foreman restart too.
- **The RCON check is a point in time.** Docker confirms the holder before
  each login but cannot rule out a swap between that check and the login.
- A stopped or missing sidecar when Foreman starts keeps trusted-header
  sign-in refused until it runs; local sign-in still works.
- VPN interface ranges are not visible through Docker. Documentation, not
  the product, forbids trusting a VPN address or range as a proxy.
- The LAN warning is evidence from inspection: a published port does not
  prove reachability (host address, changed host port, firewall).
- The sidecar and its namespace peers are trusted: Foreman has no scanner
  for peers and no VPN firewall manager (SPEC 14.16).
- The eight sidecar refusals and the warning are AI-written translations
  that no native speaker has checked.
- Docker's own error texts for joining a stopped or removed holder differ
  between the cases measured; Foreman refuses before creating anything and
  relies on none of them.

## Fixes batch 9

Serves SPEC 5.6, 8.7, 8.9, 9.1, 9.6, 9.11, 14 and 17.1, with DESIGN 8.8.
Issues: #549, #551, #552 and #499, and gaps from earlier sections. The pull
requests were #582, #583, #588 and #589 (mod search), #584 (large view), #585
(error summary), #581 (Start's reason), #580 (mod settings reader), #590
(slider tests) and #591 (Generate seed).

Rulings: a scheduled restart sends both the scheduled-restart and the start
webhooks, as SPEC 7.12 says, so that is not a gap. In the large view the
arrow keys do nothing while a preparation is shown (SPEC 17.1).

### Delivered

- **Mod search matches every word in any order, exact name first (#549, SPEC
  9.1; #582).** A mod matches when each word of the query is in its name or
  title. A mod whose name equals the whole query comes first; the rest go by
  downloads, then name. The old prefix and contains groups are gone.
- **A pasted mod portal link finds that mod (#551, SPEC 9.1, 9.11; #583).**
  The name is read from the path after `/mod/` and looked up in the cached
  list, then with the portal's `/full` call. It shows as the one result.
  "No mod named X is on the mod portal" and "X has no release for Factorio
  2.0" are told apart. Other text, including other sites' links, is
  ordinary search, and nothing is fetched from the link. The fake portal now
  leaves out a mod with no release for the asked game line, as the real one
  does.
- **Results follow the typing, and a link to browse the portal (#549, #552,
  SPEC 9.1; #588).** With script, the search starts at 2 characters, 300 ms
  after the last key, replaces only the results, keeps focus in the box and
  announces the result count. Ticked boxes survive. Without script the form
  works as before. "Browse the mod portal (opens in a new tab)" links to the
  portal's most downloaded list for the world's game line, with a hint that
  a pasted mod link works, on the search page and on the Mods tab's Find a
  mod form. Both boxes take 300 characters. The address follows the typed
  search without adding history entries, so a reload keeps it. A refused
  search shows and announces its error; under 2 characters the results
  hide and the page says "Type at least 2 characters to search. Ticked mods
  are kept."
- **The install plan's "Add another mod" reads pasted links too (SPEC 9.1;
  #589).** One length check serves both boxes: a link's mod name, or plain
  text, may be at most 100 characters, and a longer search says so.
- **The large view's Close comes first in Tab order, and the settings hint is
  read (#499, SPEC 17.1, DESIGN 8.8; #584).** Close moves to the start of the
  view's body and is still drawn in the picture's corner. The settings
  column is a group whose name carries the hint.
- **The arrow keys wait during a preparation (SPEC 17.1; #584).** While a
  preparation is shown, Left and Right do not step the planet, including a
  failed one with its Try again.
- **A failed territory request offers Try again (#499; #584).** It sends the
  request again, moves focus to the box and says "Finding the demolisher
  territories..." in the status line.
- **The error summary marks English setting names `lang="en"` (SPEC 9.6,
  17.1, 22.3; #585).** It uses the same `msgHTML` as the field error.
- **Start reports the newest run's own reason (SPEC 8.7, 8.9; #581).** An
  earlier run's failed load stands only while the newest run is silent. A
  known crash reason from the catalogue, or a failed load of its own,
  replaces it.
- **The mod settings reader locks the zips and notices folder mod changes
  (SPEC 9.6, 14; #580).** Each mod zip is mounted read-only over itself in
  the definitions container, using Docker's `Mounts` field so odd file names
  need no quoting. The cache key now covers every entry inside a folder mod
  (name, type, size, modified time), within the 100,000 file and 4 GiB limits.
- **Typing over a selected Generate field replaces it (SPEC 5.6; #591).** A
  settings redraw that landed while a field's whole text was selected put
  the cursor at its start, so typing went in front of the old text (a
  random seed). The page now selects the same text again after a redraw.
  This was the cause of the `generate.spec.ts:144` flake.
- **Mods tab "0 up to date" needed no change.** #452 had fixed it already.
- **Spec and docs.** SPEC 9.1 and 17.1 carry the new rules, DESIGN 8.8
  describes the preparation block, and the plan was deleted.

### Verified

- Each task ran the full `make verify` and the browser specs for its files, on
  linux-dev, Todd-Desktop-2 or pve1-dev. G1 (large view), G2 (error summary)
  and M3 (as-you-type search) ran the whole browser suite.
- G3 (Start's reason) and G4 (mod settings reader) passed `make test-game`
  against real `factoriotools/factorio` containers, including
  `TestRealGameModSettingsDefinitions` on 1.1, 2.0 and 2.1. M2 and M3 change
  only search and the portal link, so they had no real-game check.
- The epic with every task ran the full `make verify` and the whole browser
  suite on linux-dev.
- `sliders.spec.ts:393` timed out three times in full runs. It was just
  long (18 to 27 s alone, on main too), so #590 split it into one test per
  slider kind, keeping every check.

### Gaps left

- **The plan page's "Add another mod" search** does not search as you type.
- **A pasted name in the wrong case** falls back to the name as pasted.
- **The main game container can still write the world's hard-linked zips.**
  G4 covered only the definitions reader.
- **A run that dies of a reason the crash catalogue does not know** still
  reports an earlier run's failed load.
- **Ticked mods and "Add ticked mods" hide with the results** under 2
  characters.
- **The Previous and Next buttons in the large view** still end a
  preparation, as stepping always has.
- **A refused search's error sits below the search panel**, a little away
  from the box on a phone; the box still points to it.
- **The paste hint holds the portal link** and is the box's description, so
  a screen reader reads the link text with the box.
- **Under 2 characters the address keeps the last search**, so a reload
  brings that search back.
- Still deferred: as listed under fixes batch 8.

## Fixes batch 10

Serves SPEC 3.4, 5.12 (new), 9.1, 9.2, 9.4, 9.6, 9.14 and 17.10, with DESIGN
8.11 and ADR 0021. Issues: #593, #595, #596, #597, #598 and #547, plus the
removal of orphaned dependencies that Todd asked for while the batch was
planned. The pull requests were #601 (saved plan storage), #604 (release
dates), #607 (Mods tab), #609 (review page), #611 (orphans on removal), #605
(mod setting names), #603 (flaky focus test), #602, #606 and #610
(favorites), #612 (out of memory on start) and #613 (integration fixes).

Rulings: the saved plan is a database table, not a file in the world folder,
so no snapshot, clone or restore carries it (ADR 0021). A favorite is made
only from a world. Generate fills in a favorite and the new world's review
page installs its mods, so there is no second download path. Export and
import of favorites is #599 and is not built.

### Delivered

- **Mods tab: a results list with "+", and a plan panel (#595, #596, #597,
  SPEC 9.1, 9.2, 9.14; #601, #604, #607).** The "Mods matching" panel is
  always on the Mods tab and the search page. It is a list of ten rows, with
  a count line, paging above and below (Previous, a page box with Go, Next)
  and script that replaces only the list. Each row has the title linking to
  the portal in a new tab, a summary, the latest version, downloads and the
  latest release date, and then "+", "Installed", "In plan" with "−", or
  "Needed by *mod*". "+" shows "Adding…" until the panel is back.
- **The plan panel is a tree.** It lists each picked mod with the required
  dependencies that change nested under it. A shared dependency shows under
  each pick. Built-in mods and installed mods that already fit are hidden.
  An update reads "Will update to *version*". A dependency that cannot be
  had or that conflicts shows a warning and a short reason; Review still
  opens. "Review and install N mods" counts each distinct mod once. On a
  phone the panel holds to the bottom as a bar once it has a mod.
- **Only the picked names are saved (SPEC 3.4, 9.14; #601).** The table
  `mod_plan_mods` keeps up to 50 names per world; the tree is worked out
  fresh each time. Removing a pick drops every dependency nothing else
  needs. Deleting a world clears its picks.
- **The review page lost "Add another mod" (SPEC 9.2; #609).** Adding
  happens only on the Mods tab. Cancel and a conflict's "Back to the mods"
  go to the Mods tab. When "Install N mods" is accepted, the installed mods
  leave the saved picks (others stay; #613). The failure page's "Try again"
  still carries the names. Click budgets rose by one: search and install
  is 4 clicks, a second mod 5 (SPEC 17.10).
- **Removing a mod offers the mods only it needed (SPEC 9.4; #611).** The
  review page lists each installed mod that no remaining enabled mod
  requires, as "only needed by *mod*", ticked, and the choice goes through
  the same snapshot and hazard confirmation. The check is transitive.
  Optional, hidden-optional and recommended dependencies, built-in mods and
  folder mods are never offered.
- **Mod settings use each setting's own names (#598, SPEC 9.6; #605).**
  The definitions run reports `localised_name` and `localised_description`;
  Foreman resolves them against every section of the mods' locale files as
  the game does (parameters, nested keys, `""` joining, `?`), then falls
  back to `[mod-setting-name]` and the setting's own name. It stops at 20
  levels and 1,000 characters.
- **Favorites (#547, SPEC 5.12; #602, #606, #610, #613).** A world's page
  has "Make a favorite from this world". A favorite is a folder under
  `favorites/<id>/` with its description, the map files, the server settings
  with every secret removed, the portal mods with their versions and
  `mod-settings.dat`. `/favorites` lists them, with "Generate from it" and a
  delete that asks first. Generate offers "Start from a favorite": it fills
  in the version (still changeable), Space Age, the preset and every map
  field. After the world is made it gets the server settings and
  `mod-settings.dat`, the favorite's mods become its saved picks, and the
  person lands on its review page with a "world made" note, so one "Install
  N mods" goes through the checks, checksums and snapshot of SPEC 9.13.
  Versions are passed only when the new world is on the favorite's game
  line; on another line the review page picks the newest release that fits
  or shows a conflict. A world with no map files (an imported one) makes a
  favorite with the default map, and the page says so. Delete is a trash
  icon, and a favorite's folder is read as untrusted input.
- **The flaky focus test is fixed (#593; #603).** The page now comes to the
  front before the focus check, because a second page in the same browser
  took the focus.
- **A start names out of memory after Docker restarts the game (SPEC 8.7;
  #612).** Docker clears its out-of-memory flag when it restarts a killed
  game, about 100 ms later, and the start only looked on its next poll. The
  look at the end of the log stream now also happens during an operation.
  The switch test waits for the world's first state before switching.
- **Spec and docs.** SPEC 3.4, 5.12, 9.1, 9.2, 9.4, 9.6, 9.14 and 17.10
  carry the new rules, DESIGN 8.11 describes the Mods tab and the review
  page, the operator guide's mod walk-through uses the plan panel, ADR 0021
  records where the picks live, and the plan was deleted. DESIGN has no
  section for the favorites screens yet.

### Verified

- Each task ran the fast checks (`make check-go fmt-check vet lint
  typecheck`), the Go tests of the packages it changed and the browser specs
  for its files at desktop and 320 px, dark and light, with the accessibility
  scan. #603's test was repeated 20 times with no failure, and fails when
  the fix is removed.
- #605's definitions run was tested against real game containers on 1.1.110,
  2.0.77 and 2.1.24, which return the localised fields.
- #612's new test failed 30 of 30 times without the fix under load and
  passed 30 of 30 with it.

### Gaps left

- **A favorite with more than 50 mods cannot go through the review page.**
  The page's address carries every name and accepts at most 50. Such a world
  is made, but the review page refuses its mods, and only the first 50
  names are saved as picks. Wanted: "Review page for favorites with more
  than 50 mods".
- **A favorite's pinned versions live only in the review page's address.**
  Leaving and returning through the Mods tab's panel picks the newest
  release that fits, since only names are saved.
- **Uploaded mods are kept in a favorite.** Foreman cannot tell an uploaded
  zip from a portal zip, so a favorite lists it as a portal mod; on another
  machine or after the portal drops it, it shows as a conflict with the
  upload fix. Folder mods, turned-off mods and zips with unreadable names
  are left out, with a note.
- **A favorite carries `mod-settings.dat` as it is.** A mod that keeps a
  token in its settings carries it into new worlds. It also carries the
  world's map seed, so a new world has the same map unless the seed is
  changed.
- **DESIGN.md has no section for the favorites screens.** Their strings and
  behavior are in SPEC 5.12 and the tests only.
- **Making and deleting a favorite are not in the audit log.** Generating
  from one is, with the favorite's name.
- **Out of memory on start can still be missed** under extreme load, or after
  a second fast crash. The look at the end of the log can lose to Docker's
  100 ms restart; the fix is Docker's `oom` event, and the Docker interface
  has no event stream yet. Wanted: "Read Docker's OOM event so out-of-memory
  on start is never missed".
- **The plan panel's focus-leave close is untested.** On a phone the open
  list folds when focus leaves the panel; no browser test covers it.
- **Removing a mod on the review page does not remove it from the saved
  picks.** The link changes only that review, and Cancel returns to a panel
  that still has the mod.
- **Each "+" waits for the portal**, because the tree needs it, and the
  panel shows a portal-down note when it cannot be reached.
- **A pasted name in the wrong case** still falls back to the name as pasted
  (from fixes batch 9). The old gaps about the plan page's search and about
  ticked mods hiding with the results are gone with the tick boxes.
- Still deferred: as listed under fixes batch 9.

## Fixes batch 11

Serves SPEC 5.6, 5.11, 5.12, 6.10, 6.12, 8.7, 9.2, 9.14, 9.15, 11.1, 14.3,
14.6 and 17 (new 17.16 and 17.17), with DESIGN 7.10, 7.11, 8.10 and 8.11 and
ADR 0022. Issues: #599, #615, #616, #618, #619, #620, #621, #622, #623,
#624, #625 and #626. The plan was #627. The pull requests were #628
(map string encoder), #629 (saves and the Favorites button), #630 (drop
zone), #631 (mod titles), #632 (Docker events), #633 (map string from a
save), #634 (snapshot wait under fork saving), #635, #639, #640, #644, #645
and #650 (test and small fixes), #637 (game settings form), #638 (titles on
other pages), #641 (review page rows), #642 (favorites export and import),
#643 (map string screens), #647 (drop zone everywhere), #648 (snapshot
selection), #649 (review draft and short addresses) and #651 (integration
fixes).

Rulings (Todd, 2026-11-15): all twelve issues in one batch; fork saving is
in the form; Foreman writes 20 autosave slots whenever it creates
`server-settings.json`; defaults show in every unset field; an upload
happens in place when its result is used on that page, otherwise a button
goes to the tab that owns the file; pages show titles while saved records
keep the slug; no address Foreman builds is long; Docker events are
optional; the review page keeps its answers in a draft in memory (ADR
0022); a snapshot selection is posted and the result shows a count.

### Delivered

- **Mods show by title (#623, #619, #620; SPEC 9.15, 9.2; #631, #638, #641,
  #647, #651).** A mod's title comes from its `info.json` or the portal,
  with the slug as fallback, and is drawn by one `mod-name` partial: cut to
  100 characters, escaped and `translate="no"`. Named on its own it links
  to the portal in a new tab, except built-in mods, folder mods and mods the
  portal lacks. The Installed list, update lines, More menus, plan panel,
  phone bar, review page, mod settings page, Server page results,
  pre-start refusal, fix labels and the favorites list use it. Inside a
  sentence or on a button a title is plain text. Snapshot labels, audit
  entries and sent notifications keep the slug.
- **The review page rows (#620; #641).** Each mod's version list and a
  cross named "Remove *title* from the plan" sit in its row. An offered mod
  (optional, recommended, no longer needed) shows its title link, the portal's
  summary, the version and its release date; the link never ticks the box.
  Portal lookups for offered mods are capped.
- **One drop zone for every upload (#622, SPEC 14.6, DESIGN 7.10; #630,
  #647, #651).** `templates/dropzone.html` and `static/dropzone.js` give a
  dashed box that takes a drop, a click or the keyboard, refuses a wrong
  type, several files and an oversized file before uploading, and shows
  "Chosen: name, size" with Clear. Without script it is a plain file field.
  Import, Saves, the Mods tab's zip and `mod-settings.dat` and the review
  page's missing-mod zip use it. An upload for another tab ("Upload a save"
  on the Mods tab) is a button, never a link. The server's checks are
  unchanged.
- **Favorites export and import (#599, SPEC 5.12, DESIGN 7.11; #642).** An
  export is `<name>-favorite.zip`: the folder plus a format-version file,
  with `mod-settings.dat` and a visible warning that a mod may keep a token
  there. An import (8 MiB, drop zone) is checked in full before anything is
  written. A name in use is refused. Missing mods or a game version this
  Foreman lacks are still imported and show as conflicts on review. The
  list shows each favorite's mods by title.
- **The map exchange string is offered back (#618, SPEC 5.6; #628, #633,
  #643).** A world's page and each favorite have a read-only field with a
  Copy button and a note that mods are not included. It is encoded from the
  map files for 1.1, 2.0 and 2.1 (`mapexchange.Encode`), or, for a random
  seed or an imported world, read from the newest save's `level-init.dat`
  (`savefile.ExchangeString`). No container was needed. When neither works,
  the button is disabled with the reason.
- **Saves read as world and time (#625; #629).** An autosave shows the
  world's name and its save time in the viewer's zone, with the file name
  beside it; its download is named from the world and the time. Files are
  never renamed. A Favorites button sits beside Generate and Import (#621).
- **Game settings form (#624, #626, SPEC 11.1, 17.16; #637).** Every unset
  field shows the game's default as a placeholder and a hint; unset
  checkboxes show the default state. New groups: Players and access,
  Gameplay, Saving and Network. `auto_pause_when_players_connect` is hidden
  on 1.1 and added to the 2.0 schema. Fork saving is in the form with its
  note, disabled when Docker's `/version` says the host is not Linux.
  Foreman writes `autosave_slots: 20` whenever it creates the file, so a new
  world is also Hidden from its first start. Clearing a number box removes
  its key.
- **Snapshots wait for a forked save (#626, SPEC 6.10; #634, #644, #645).**
  A `/server-save` sent while an earlier forked save is still being written
  is queued; the snapshot now waits for the save begun after its command and
  for a 300 ms settle. A failed save process is named as a failure. After a
  crash restart the stale RCON connection is dropped, so a restart reaches
  the game that came back.
- **Docker events (#616, SPEC 8.7, 14.3; #632, #639).** Foreman follows
  `oom` and `die` events when the socket proxy allows them, so an
  out-of-memory kill is named even after Docker restarts the game. It
  retries from 1 to 30 seconds. With `EVENTS=0` it polls as before and logs
  the refusal once. The operator guide recommends `EVENTS=1`.
- **Short addresses and the review draft (#615, SPEC 9.14, 17.17, ADR 0022;
  #648, #649).** The review page keeps its answers in a draft in memory
  under a random token; links carry only a kind and one name, or the token.
  Generate passes a favorite's id. A plan holds up to 500 picks, and a
  120-mod favorite installs in one review. A snapshot selection is posted,
  and the result says how many were deleted. A test on each of the review
  and snapshot pages keeps every address under 512 bytes.
- **Partial saves are refused (#651).** A file ending in `.tmp.zip` cannot be
  uploaded, loaded or chosen to load next, and a folder import skips it; its
  download is allowed.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck` and its own Go
  tests and end-to-end specs (four projects: phone and desktop, dark and
  light, axe clean). The whole `internal/web` package takes over three
  minutes, so tasks ran it in slices.
- The fork-save wait was checked in real containers on 1.1.110, 2.0.77 and
  2.1.24. The queued-save bug failed its test before the fix.
- The encoder round-trips all 98 real-game strings in `testdata` to the same
  inflated payload. The save reader matches the game's own string on all
  eight test saves from 1.1.110, 2.0.77, 2.1.23 and 2.1.24, and fuzzed for 60
  seconds without a failure.
- The defaults table is checked against the game's own example files for
  1.1.110, 2.0.77 and 2.1.24. The real-game first-save and auto-pause tests
  pass on all three lines with the generated file (#650).
- The race in `TestRefusalIsAuditedAndLoggedWithReason` and the crash restart
  flake were test and product bugs fixed here (#639, #645).

### Gaps left

- **A removed mod's result names its slug.** Its file is gone, so no title
  can be read.
- **A title inside a sentence or on a button is plain text**, not a link.
- **Plan panel and review titles are tested end to end only as slugs.** The
  fake portal's title equals its slug and its added mods have no release
  date; the Go tests cover real titles and dates.
- **The Favorites page reads each favorite's files on every view** to make
  its map string.
- **Map strings are proven byte for byte only on 1.1.110, 2.0.77, 2.1.23
  and 2.1.24.** Another patch is accepted only if the result decodes in full
  and its fields match; otherwise the button is disabled with a reason.
- **The map string read from a save is the map as created** (`level-init.dat`);
  a later script change to the map settings is not in it. The compressed text
  of an encoded string differs from the game's; the payload is the same.
- **Docker events are checked once, at startup.** After changing `EVENTS`,
  restart Foreman. The polling fallback can still miss an out-of-memory kill
  under heavy load.
- **A save that fails without any log line** still waits the 2-minute
  timeout. A failing save process that logs its failure ends the wait at
  once (#644).
- **Snapshot bulk selection is capped by the 64 KB form limit**, about 1,500
  snapshots; and after "Select all automatic" the address is
  `/snapshots/select`, so a reload asks to resend the form.
- **Fork saving that is already on can be turned off only in the raw
  editor** on a host that is not Linux.
- **Generate from a favorite writes 20 autosave slots** even if the favorite
  had cleared the key (ruling 3 as written).
- **New worlds are Hidden from their first start**, because Generate and
  Import now write a full `server-settings.json`. The alternative, writing
  only the slots and the three required keys with LAN visibility, is a
  one-line change in `gamesettings.NewServerSettings`.
- **`allow_commands`** is written as `true`, `false` or `"admins-only"`; the
  real-game check covers only the last.
- **A favorite on an unsupported game version** is imported, and Generate
  uses the default version without saying so. The review page shows a
  conflict that blocks the install, so the admin installs from the Mods tab,
  where Generate saved the picks. Blocking is the orchestrator's ruling
  (2026-11-16).
- **Drafts live in memory.** A restart or two hours sends an open review back
  to the saved picks, and the page says so.
- **The download time of an autosave is UTC**, not the viewer's zone, and a
  named save keeps its file name on download.
- **The Worlds list's empty state is text only**; the Import button sits
  above it.
- **Other settings forms still show no defaults** (#636), and **a game back
  from a crash can briefly be treated as still crashed** (#646).
- **Real-game results.** Out of memory on start was named 20 of 20 times
  with events on and 20 of 20 with events off (`TestRealGameOOMOnStart`,
  2.1.24, on pve1). The fork-save snapshot test passed on 1.1, 2.0 and 2.1,
  including a save queued behind an autosave. `TestRealGameFirstSettingsSave`
  and `TestRealGameUIBAutoPause` passed on 1.1.110, 2.0.77 and 2.1.24 after
  #650: the generated file is Hidden with auto-pause on and 20 slots, and
  the game starts. `make test-game` on the final head is recorded in the
  batch pull request. Todd checks a pasted map string in the game client at
  the test drive.
- Still deferred: as listed under fixes batch 10, except the gaps about
  favorites with more than 50 mods, out-of-memory on start and the missing
  favorites section in DESIGN.md, which this batch closes.

## Fixes batch 12

Serves SPEC 5.12, 8.7, 14.6 and 17.16, with DESIGN 7.1, 7.8, 7.10, 7.11 and
8.3 and ADR 0023. Issues: #636, #646, #656, #657, #658 and #659. The plan was
#660. The pull requests were #661 (a stop sees a restarted game), #662 (one
foldout style), #663 (favorites rows and Rename), #664 (link marks, Saves file
names, mod default hint) and #665 (integration fixes).

Rulings (Todd, 2026-11-16): a link to another tab ends in a chevron and a link
that leaves Foreman ends in `↗`, both drawn in CSS so neither is read aloud;
one foldout style, with `.plan-panel__list` taking only the orange marker;
Emergency actions keeps its red label and only its marker is orange; a
favorite can be renamed under Make's name rules; a "Mod default: X" hint shows
under a mod setting that differs from the mod's default. Orchestrator rulings:
the foldout class stays `disclosure`; a rename takes the favorites lock and is
not audited; a Stop is never refused for a game not in game, and a game in
game is always warned and saved.

### Delivered

- **A stop sees a game back from a crash (#646, SPEC 8.7, ADR 0023; #661).**
  Every operation that stops a game now takes the monitor's missed look at a
  restart Docker made before it plans, so Stop, Restart, Switch, Restore,
  Rollback, mod changes and Update treat a game back in game as in game: it is
  warned and saved. When the log has not yet said "in game", the stop asks
  RCON, which Factorio opens only once in game. A game not yet in game is
  stopped without a warning or save, never refused, and no refusal tells the
  user to "stop it". New tests run with the monitor off and failed before the
  change; the crash-loop tests still pass.
- **One foldout style (#657, DESIGN 7.8; #662).** Every `details.disclosure`
  has the orange triangle, a bold label, a fill under a pointer that can hover,
  a compact row with a fine pointer and 44 px on touch, and a line only
  between stacked foldouts. Generate's planet rows and the raw JSON foldout
  use it. The old button look, the `disclosure--add` plus style and the
  snapshot narrow-row link look are gone. The plan panel's list keeps its bar
  and takes only the marker. The player list's name, the row menus and the user
  menu are unchanged.
- **Favorites are closed rows with Details, and can be renamed (#658, SPEC
  5.12, DESIGN 7.11; #663, #665).** Each row shows the name and a line of
  facts, with Generate ("Generate a world from {name}") at its right end.
  Details holds made from and on, the mods, the exchange string, Rename, and
  Export and Delete. The export warning shows once. Rename is
  `POST /favorites/{id}/rename`; it follows Make's rules, allows a change of
  case to the favorite's own name, rewrites only `favorite.json`, and shows
  "Renamed the favorite to ..." from the favorite's current name. A refusal
  re-opens that favorite's Details with the error. The name field's limit
  comes from the server's own. Browsers without `::details-content` place the
  open body on the second row, across the whole row.
- **Link marks (#656, SPEC 14.6, DESIGN 7.1, 7.10; #664).** The three links to
  another tab's upload (Mods to Saves, mod settings to Mods, an empty Worlds
  list to Import) are plain links ending in a chevron (`link-tab`), not
  buttons. One rule on `a[target="_blank"]` ends every link out, including mod
  titles linked to the portal, in `↗`. The changelog link on the versions page
  now opens in a new tab with the hidden "opens in a new tab" text. A Go test
  checks that every new-tab link points off the site and has
  `rel="noopener noreferrer"`, and that every `https://` link opens in a new
  tab.
- **Saves file names on their own line (#659, SPEC 5.11, DESIGN 8.3; #664).**
  An autosave's file name sits under its "World, date time" label, muted, and
  the label uses tabular numbers, so file names line up at the label's left
  edge on desktop and at 320 px.
- **Mod default hint (#636, SPEC 17.16; #664).** A mod setting whose value
  differs from the mod's default shows "Mod default: X" under the field, in
  the field's description. The other forms in the #636 list were already
  compliant; the account form and the raw settings editor are exempt (SPEC
  17.16).

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck`, its Go tests
  and its end-to-end specs on all four projects (phone and desktop, dark and
  light, axe clean), at 320 px and 200% text in German and Russian for the
  UI tasks. The whole `internal/web` package takes over three minutes, so L1
  ran it in slices.
- G1's new tests failed on the base (`TestStopWarnsAGameBackInGameBeforeAnyLook`,
  `TestOnlyRunningRestartsAGameBackInGameBeforeAnyLook`; two more failed on
  the first head) and pass now, also under `-race -count=3`.
- The new foldout spec (`foldouts.spec.ts`) covers the line rules, the marker
  colour and turn, the focus ring and 44 px rows under touch emulation. The
  `@supports` fallback was checked by hand in Chromium with a plain div
  standing in for an old browser's Details: the body was 600 px wide on row 2,
  and without the rule it sat on Generate.

### Flaky tests fixed

Three tests failed once or more under heavy machine load during the batch.
Each was diagnosed:

- **Mod plan adds could fail under contention (a real bug).** With hundreds
  of writers at once, SQLite's busy timeout does not serve waiting writers in
  turn, so some adds failed with "database is locked". Mod plan writes now
  queue in Foreman before they reach the database, and the test fails on any
  error except "plan full" (#668).
- **The failed-switch test read the page too early (test only).** The reason
  sentence is stored a moment after the failed state shows, and the live page
  sends a fresh copy once it is; the test now waits for it (#668).
- **The drop-zone upload test timed out (test only).** The shared `dropFile`
  helper sent a 4 MB file as an array of numbers; it now sends base64, and the
  test takes about 3.5 s instead of 30 to 56 s (#667).

### Gaps left

- **A crash in the instant between the catch-up and the stop plan** still
  takes the plain-stop path (no warning or save). The window is microseconds,
  not a poll interval.
- **A stale monitor look applied after the catch-up** can wind the run's start
  time back until the next poll. The RCON check still warns and saves.
- **The plan bar's CSS triangle does not flip for right-to-left text.** There
  is no right-to-left language today.
- **The foldout end-to-end checks cover one page per kind**, not every
  foldout.
- **The link-mark guard test reads templates only.** A `target="_blank"` link
  built in Go or JavaScript would pass it; there is none today.
- **A long `link-tab` label that wraps on a phone** shows its chevron at the
  middle right of the wrapped text, not after the last word.
- **`link-tab` marks the three "upload on another tab" links** (SPEC 14.6).
  Ordinary navigation links inside Foreman (tabs, breadcrumbs, list rows)
  carry no chevron.
- **Closed from batch 11:** the gaps about other forms showing no defaults
  (#636) and a game back from a crash briefly treated as crashed (#646). The
  batch 11 note that an upload for another tab is a button is replaced by the
  link rule in SPEC 14.6.
- Still deferred: as listed under fixes batch 11.

## Epic 9c: Public mods and autosave tidy

Serves SPEC 9.16, 11.6 and 14.18, with the edits to 5.11, 6.2, 6.7, 11.3,
12.1, 12.9, 13.1 and 14.1, and the VISION Auth amendment. Issues closed:
#132 and #671. The plan was #672. The pull requests were #673 (autosave
tidy), #674 (public mods backend), #675 (public mods page and setting), #676
(a tidy cut short) and #677 (review and translation fixes).

Rulings (Todd, 2026-11-16), now in SPEC 9.16 and 11.6: the public page
offers every enabled, non-built-in mod of the running world, one by one and
as one zip, and marks the ones the portal cannot supply; turned-off mods are
left out and folder mods are listed with no download; Foreman handles no
licences; the page is off by default and one admin setting turns it on; the
setting lives in a new `app_settings` table that Epic 11's settings reuse;
the autosave tidy takes a snapshot first and treats K as the newest autosave
by time, skipping the file the last run loaded; player IP addresses stay in
the access log.

### Delivered

- **The public mods page (SPEC 9.16; #674, #675, #677).** `GET /public/mods`
  shows the running world's name, game version and mods to anyone, with a
  link for each mod and a "Download all" link. A mod the portal does not
  supply is marked in words, a folder mod has no link, and a page with no
  running game says so. It has its own shell, `noindex`, the language menu
  (`POST /public/language`) and its files under `/public/static/`. The
  catalogue area `publicmods` is in all eight languages.
- **The downloads and their rules (SPEC 14.18; #674, #677).**
  `GET /public/mods/<world>/files/<file>` sends one zip with conditional and
  range requests, and `all.zip` streams the lot, stored. A file name must
  equal a listed zip's name byte for byte and opens through the world root
  as a regular file that is not a link. At most 4 downloads run at once (503
  with `Retry-After: 5`) and a stalled write is cut off after 60 seconds. Only
  GET is allowed: HEAD and every other method get 405. A public route never
  reads a session, trusted header or token, and sets only the language
  cookie. A `mods` folder that is a link is never read. `/public/static/`
  files carry `Cache-Control: no-cache`.
- **The portal marks (SPEC 9.16).** The answer is kept per world and per
  mods fingerprint, 15 minutes after an answer and 1 minute after a failure,
  with one check at a time and a 5-second wait. A page whose mods have no
  zip skips the check.
- **The admin setting (SPEC 9.16, 12.1, 12.9).** `/settings/public-mods` is a
  fourth tab in the Foreman settings. One button turns the page on or off,
  and each change is audited as `public-mods-set`. While it is off, every
  path under `/public/` answers 404 like an unknown path, whatever the
  method. The setting is stored in the new `app_settings` table (migration
  `0009_app_settings.sql`).
- **The operator note (docs/OPERATOR.md section 6).** How to let `/public/`
  through a forward-auth proxy, naming no proxy.
- **The autosave tidy (SPEC 11.6; #673, #676).** At every start Foreman
  makes, when the slot count S is lower than some autosave's number, Foreman
  takes a snapshot ("Before autosave tidy", reason `autosaves`), writes the
  audit entry `autosave-tidy`, keeps the newest S autosaves under the slot
  numbers the game writes next, and removes the rest. Files go through
  `_autosaveN.tidy.zip` names holding their final slot, so a tidy cut short
  at any step is finished by the next start with the same result. If the
  snapshot or the audit entry fails, the tidy is skipped and the start carries
  on. The game's "Loading map" line gives the loaded file, which is kept
  after the run ends. The settings preview names how many autosaves stay and
  how many go, in three one-count sentences in all eight languages.
- **A shared template renderer.** The web package builds one renderer for
  the whole process (`sharedRenderer`) instead of one per test server. This
  halved the memory of the test run.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck`, its Go tests
  and its end-to-end specs. The new end-to-end specs are `public-mods.spec.ts`
  (all four projects, axe clean) and `autosave-tidy.spec.ts`. The security
  file `publicmods_security_test.go` walks the 14.18 conditions.
- `TestAutosaveTidyResumesAfterEveryStep` cuts the tidy short after every
  single rename and removal and checks that the next start gives the same
  folder as an uninterrupted tidy.
- The public download was tried by Todd on the trial deploy on 2026-11-16.
- Real-game autosave test: `TestRealGameAutosaveTidy` passed against factoriotools/factorio 2.1.24 on 2026-11-16 (20 slots to 10, K = 17; the game loads `_autosave7.zip`, modification times kept).

### Gaps left

- **The tidy's snapshot and audit entry name "Foreman"**, not the user who
  started the game.
- **Leftover `.tidy.zip` files that fit no mapping for the current slot count
  make every start skip the tidy** and log the reason, as happens when S
  changes after a tidy was cut short. The admin fixes the files by hand or
  restores the snapshot.
- **`public-all` and `public-unchecked` ids repeat per world** in
  `publicmods.html`. Only one world is listed today, so no page has a
  duplicate.
- **The public page serves only a running world.** A stopped world's mods
  cannot be downloaded from it.
- **The tidy's mapping is exact only in the cases SPEC 11.6 names under
  "Accuracy";** otherwise the game's first overwrite may hit a file that is
  not the oldest, and the snapshot is the undo.

## Epic 10a: API tokens and MCP

Serves SPEC 12.10, 14.19 and 21.1 to 21.6, with the edits to 3.2, 8.10,
8.11, 12.1, 12.9, 13.1, 14.1, 14.7, 14.8, 14.12 and 22.3. The task pull
requests were #681 and #682 (token core and its tests), #683 (the
`token-expiring` event), #684 (the endpoint and tools), #685 (the page and
banner), #686 (security, real-game and end-to-end tests), #687 (manual
snapshots and restores audited), #688 (integration fixes) and #689 (a
load-sensitive test). Epic 10b (admin tools, the two-step confirm, SPEC
21.7 and 21.8) is a later epic.

Rulings (Todd, 2026-11-17), now in SPEC 12.10 and 21: three nested scopes
(read, operate, admin); tokens last 30 days from creation or renewal, and
an expired token stays renewable with the same secret; a revoked one is
final; every admin sees every token; chat through MCP carries the token's
name; MCP is an admin setting, off by default.

### Delivered

- **API tokens (SPEC 12.10, 14.19; ADR 0024; #681, #682).** A secret is
  `fmn_` plus 32 random bytes in base64url. Only its SHA-256 is stored.
  Migration `0010_api_tokens.sql` adds the `api_tokens` table and the audit
  log's `token_name` column. A name is 1 to 32 letters, digits, spaces, `.`,
  `_` or `-`, and unique among the owner's live tokens. State (working,
  expired, revoked) is worked out from the stored times. Renew keeps the
  secret and is owner only. Revoke is final. Last use is written at most once
  a minute. A gitleaks rule catches `fmn_` secrets.
- **The API tokens page and the banner (SPEC 12.10, 21.2; #685).**
  `/settings/api-tokens` holds the MCP switch, the user's tokens with Renew
  and Revoke (revoke goes through a confirm page), a create form with read
  picked by default, and an "All tokens" list. The secret appears once, on
  a `no-store` page, beside the endpoint address. The banner shows on every
  signed-in page from 5 days before expiry and while a token is expired. The
  strings are in all eight languages. `FOREMAN_MCP=off` forces MCP off and the
  page says so. The setting is audited as `mcp-set`, and token changes as
  `token-create`, `token-renew` and `token-revoke`.
- **The `/mcp` endpoint and its tools (SPEC 21.1 to 21.6; ADR 0025; #684).**
  The official Go SDK in stateless JSON mode, one server per scope. Read
  tools: `server_status`, `list_worlds`, `list_players`, `recent_log`,
  `list_snapshots`, `list_mods`. Action tools: `start_server`, `stop_server`,
  `restart_server`, `switch_world`, `take_snapshot`, `send_chat`. They run
  the same code as the Server page, wait up to 50 seconds, and never offer
  "start anyway". Chat is sent as `[token name] message`.
- **The limits (SPEC 14.19; #681, #684, #686).** Bearer header only. A
  malformed secret is refused with no lookup. A 401 names an expired or
  revoked state. 20 failures per address in 15 minutes (IPv6 by /64, kept
  apart from sign-in's count), 120 requests a minute and 4 at once per token,
  429 with `Retry-After`, a 4 MiB body, one JSON-RPC message per body, 403
  when MCP is off before the token is read, and any `Origin` refused unless
  it equals `FOREMAN_PUBLIC_URL`'s. No cookie or trusted header gives an
  identity on `/mcp`. `mcp_security_test.go` walks 14.19 line by line.
- **Untrusted text (SPEC 14.12; #684, #688).** Player, log, portal and
  snapshot-label text sits in `untrusted_*` fields, and each tool's
  description names them.
- **The snapshot cap (SPEC 14.12, 21.5; #688).** `take_snapshot` through a
  token is refused once the world holds 10 snapshots taken through tokens.
  The snapshot's `snapshot.json` records `via_token`. Snapshots taken in the
  UI neither count nor are refused.
- **A revoke takes effect on a held request (SPEC 12.10; #688).** `/mcp`
  checks the token again by id after the body is read. The Renewed message
  shows only for a working token.
- **Manual snapshots and restores are audited (SPEC 12.9; #687).** The
  controller records `snapshot` and `restore` entries with the token's name,
  before the action, and refuses if the entry cannot be written. The
  automatic "Before restore" snapshot adds no extra entry.
- **The `token-expiring` event (SPEC 8.10, 8.11; #683).** Sent 5 days and 1
  day before a working token expires, once per period, checked at start and
  every 15 minutes. If both came due while Foreman was down, only the later
  one is sent. It is off by default on new webhooks and was turned off on
  existing ones by the migration. The generic body gains an optional `token`
  field, and `world` has an empty id and name for this event. Eight languages.
- **The operator guide (docs/OPERATOR.md).** The `token-expiring` event, the
  `token` body field, and "Connect an AI assistant", which says how to let
  `/mcp` past a sign-in proxy without naming one.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck` and the Go tests
  and end-to-end specs for its files. New end-to-end specs: `api-tokens.spec.ts`
  and `mcp.spec.ts` (token made in the browser, `/mcp` called with no cookie,
  last use updates, revoke then 401 naming it, MCP off gives 403).
- `TestRealGameMCP` (tag `game`, 2.1.24) reads status, takes a snapshot, sends
  chat (the game log shows `[Claude] ...`), restarts with a 3-second
  countdown, and shows a read token refused a restart. It found that manual
  snapshots were not audited, which #687 fixed.
- Tests written first for the revoke re-check, untrusted labels, the
  snapshot cap and the audit entries failed before their fixes.

### Gaps left

- **A token-expiring reminder is sent on a best-effort basis (SPEC 8.12).**
  It is queued and then recorded as sent. A full queue or a shutdown between
  the two can drop one, and it is not retried.
- **The MCP snapshot cap can overshoot by one or two** when calls race at the
  limit. It guards against a flood, not an exact count. Snapshots from before
  Epic 10a lack `via_token` and never count.
- **A request whose token check finishes a few milliseconds before a revoke
  commits may still run.** Every request checked after the revoke is refused.
- **Admin scope has only the operate tools until Epic 10b.** The admin tools
  and the two-step confirm (SPEC 21.7, 21.8) are not built. (Closed by Epic
  10b: 37 admin tools and the two-step confirm.)
- **Snapshots and restores taken without Docker control write no audit
  entry.** The web paths used when Foreman has no Docker control act without
  recording one.
- **No viewer role exists yet,** so the "All tokens" list and the MCP switch
  show to every user (SPEC 12.1).
- **The language cookie is still read on `/mcp`** by the shared locale
  middleware. It is not used for identity.
- **Audits that take a user instead of an actor cannot carry a token name:**
  Generate, settings, snapshot deletion and the world manager's `by`
  parameters. Epic 10b changes them, once admin tools reach them. (Closed by
  Epic 10b: `s.audit` and the snapshot, settings and world functions take an
  actor, so a token's name is recorded.)
- **No audit viewer yet (Epic 11).** Token names are recorded but only the
  database shows them.
- **Not built, on purpose:** OAuth discovery metadata, progress updates over
  a stream, tokens limited to one world, and a paged token list.
- **The 18.10 gate with a real Claude Code client on trial** is the
  orchestrator's check before the epic merges, not part of the build.

## Epic 10b: MCP admin tools and the two-step confirm

Serves SPEC 21.7, 21.8, 14.19 and 18.10b, with the edits to 12.9
(`world-delete`), 21.5 (`cancel_stop`) and 21.8. The task pull requests were
#695 (the plan), #696 (T1, the foundation), #698 (T2, worlds, favorites and
saves), #701 (T3, snapshots and live operations), #700 (T4, mods), #697 (T5,
game settings and versions), #699 (T6, Foreman-wide settings), #702
(integration fixes) and #703 (T7, the gate tests).

Rulings (Todd, 2026-11-17), now in SPEC 12.9, 21.5 and 21.8:

1. A tool is two-step exactly when SPEC 21.8 names the action or the UI shows
   a confirm page for it. Every other admin tool is one call.
2. The confirm call is refused, with a fresh preview and code, when the state
   the preview showed has changed. That state is kept with the code.
3. The code stands for a confirm page's inner confirmations (typing the world
   name, the drops tick, the "go on without" tick). The preview lists the
   drops and the "without" mods.
4. One tool per action, with fixed names.
5. `change_game_settings` takes the settings form's fields only, never the raw
   editor.
6. A snapshot's `taken_by` stays the owner's username. The audit log carries
   the token name. `snapshot.json` is unchanged.
7. Deleting a world is audited as `world-delete`, before the delete, which is
   refused if the entry cannot be written. The UI and MCP do the same.
8. `cancel_stop` is an operate tool.

### Delivered

- **The confirm-code store and helpers (SPEC 14.19, 21.8; ADR 0026; #696).**
  A code is 16 random bytes in base64url. It lasts 5 minutes, is bound to the
  token, the tool and the SHA-256 of the arguments in canonical JSON, and is
  spent by any use, right or wrong. A token holds 10 and an eleventh drops the
  oldest. Codes live in memory and are never logged. A bad, used, expired or
  mismatched code gets one fixed message. Any other refusal keeps its own
  message, and the code is still spent. `s.mcpAsk` issues a code and `s.mcpSpend` spends one. A
  two-step tool is marked `DestructiveHint: true`, every other admin tool
  `false`, and the reads `ReadOnlyHint`.
- **37 admin tools (SPEC 21.7; #696 to #699, #700, #701).** Worlds, favorites
  and saves (#698): `generate_world`, `clone_world`, `rename_world`,
  `delete_world`, `list_deleted_worlds`, `list_favorites`, `make_favorite`,
  `rename_favorite`, `delete_favorite`, `list_saves`, `choose_save`.
  Snapshots and live operations (#701): `restore_snapshot`, `delete_snapshots`,
  `force_stop`, `download_game_image`, `kick_player`, `ban_player`,
  `player_action`, `run_console_command`. Mods (#700): `search_mods`,
  `change_mods`, `list_mod_settings`, `change_mod_settings`. Game settings and
  versions (#697): `get_game_settings`, `change_game_settings`,
  `set_whitelist_switch`, `list_game_versions`, `update_game_version`,
  `rollback_game_version`. Foreman-wide (#699): `foreman_settings`,
  `set_restart_schedule`, `add_webhook`, `change_webhook`, `delete_webhook`,
  `set_factorio_account`, `remove_factorio_account`, `set_public_mods`.
  Fourteen are two-step: `delete_world`, `delete_favorite`, `restore_snapshot`,
  `delete_snapshots`, `force_stop`, `kick_player`, `ban_player`,
  `run_console_command`, `change_mods`, `change_mod_settings`,
  `change_game_settings`, `update_game_version`, `rollback_game_version` and
  `delete_webhook`. The tools that need Docker control register only when
  Foreman has it.
- **The preview matches the confirm page (SPEC 21.8).** Each two-step tool
  builds the page the UI's confirm page uses. The preview lists what will
  change, the snapshot label the UI would take (empty when none), and who is
  online. The first call changes nothing. The confirm call spends the code
  first, runs every check again, and is refused with a fresh preview and code
  if the state moved (mod plan hash and folder fingerprint, settings file
  hashes, mod setting flags). The same code stands for typing the world name
  and the other ticks. Each area's test renders the UI template and checks that
  every `will` sentence appears in it.
- **Handler logic lifted into request-free functions (#696 to #701).** The
  UI and MCP call the same function, for example `s.generateWorld`,
  `s.cloneWorld`, `s.deleteWorld`, `s.restorePage`, `s.bulkPlan`,
  `s.playerConfirmPage`, `s.planOp`, `modSettingsOp`, `h.formChange`,
  `s.setWhitelistSwitch`, `s.rollbackPage`, `s.saveSchedule`, the webhook
  functions, `s.setAccount` and `s.setPublicModsBy`. UI behaviour did not
  change.
- **`cancel_stop` (SPEC 21.5; #696).** An operate tool that cancels a stop
  countdown as the Server page's Cancel stop does, audited as `stop-cancel`.
  Finding no countdown is not an error.
- **Audit by actor (SPEC 12.9; #696).** `s.audit` takes an actor, so every
  audit row from an admin tool carries the token's name. This closes the
  Epic 10a gap about audits that took a user.
- **`world-delete` is audited before the delete (SPEC 12.9; #698).** The UI
  and MCP both go through `s.deleteWorld`.
- **`mcpRun` takes a lock parameter and long work is detached (#696).**
  `download_game_image` runs without the world lock. `s.mcpDetached` runs
  work that can outlast the call and has no Server page result.
- **Untrusted text (SPEC 14.12; #697, #698, #700, #701).** Player, portal,
  game-log and snapshot text sits in `untrusted_*` fields, such as
  `untrusted_online`, `untrusted_reply`, `untrusted_mod_steps` and
  `untrusted_admins`. Each tool's description names them. No secret is
  returned: `get_game_settings` gives `has_password`, a webhook's address and
  the factorio.com account are write-only, and a secret row in a settings
  preview says only that it changes.
- **Integration fixes (#702).** The epic head did not compile because two
  tasks declared the same names. They were renamed. Four tools (settings,
  update, rollback, webhook delete) spent their code after a lookup and now
  spend it first. `search_mods`, `get_game_settings` and `list_game_versions`
  are marked read-only. The shared helpers and test helpers moved to one
  place.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck`, the Go tests for
  its package and the end-to-end specs for the pages whose handler it lifted.
- `mcpadmin_security_test.go` (#703) finds the two-step tools from `tools/list`
  with no hand list. For each of the 14, the first call leaves the data folder
  (path, size, modification time) and every audit row unchanged, and the
  preview names the snapshot label the UI takes. A code is refused for another
  token, tool or arguments, after 5 minutes, on a second use and after a refused
  confirm. No code or secret appears in a result, an error, the log or the
  audit log on any of three rigs. Operate and read tokens get "unknown tool" for
  all 37 admin tools.
- `e2e/tests/mcp-admin.spec.ts` (#703) makes an admin token in the browser and
  calls `/mcp` with no cookie. After the restore preview the Snapshots tab
  shows no "Before restore". After the confirm the Server page shows the
  restore and the tab shows "Before restore". A deleted world stays on Worlds
  after its preview and is gone after the confirm.
- `realgame4_mcp_admin_game_test.go` (#703, tag `game`) runs `change_mods`,
  `restore_snapshot` and `update_game_version` (2.0.77 to 2.1.24) through MCP
  against a real `factoriotools/factorio` container. For each it checks the
  snapshot's reason and label, exactly one snapshot per action, the one audit
  row with the token's name, and that the game reaches InGame. It also checks
  that the "Before mod change" snapshot still has the probe mod enabled and
  that the restore brings the probe back. Passed on a real 2.1 game
  (`TestRealGameMCPAdmin`, a9fcf338, on pve1).
- **Whole-epic checks (orchestrator).** `make verify` passed (90.7%
  coverage), the full end-to-end suite passed on 7d4cdf18 (3492 passed, 0
  failed), and the `internal/game` and `internal/web` real-game packages
  passed.

### Gaps left

- **The world-delete entry is written before a delete that may still fail.**
  Ruling 7 accepts this. The entry's detail says "requested", not "done".
- **Detached work past 50 seconds is not shown by `server_status`.** Clone,
  world delete and bulk snapshot delete have no Server page result, so after
  the call gives up waiting, only the audit log and the pages show the
  outcome.
- **`change_mod_settings` and the UI share one snapshot wording,** so the
  snapshot's label does not say which of the two took it. The audit row does.
- **The two-step confirm guards against mistakes, not a steered client.** An
  assistant that is tricked into asking can also confirm. SPEC 14.19 accepts
  this risk. The token's scope and the operator's choice of scope are the
  defence.
- **Confirm codes are lost on a restart.** The client asks again.
- **Some tools are web only:** the raw settings editor, "update anyway" and
  "start anyway", mod zip upload and the mod plan's picks panel, favorite
  import and export, map preview pictures and anything excluded by SPEC 21.7.
- **`restore_snapshot` needs Docker control,** as do `update_game_version` and
  `rollback_game_version`. The tools are not listed without it.
- **A console command's preview has no UI confirm page to compare with,** so
  the test checks the preview's own sentences instead.
- **Tools are registered when no scheduler is wired.** The Foreman-wide tools
  then say the feature is unavailable.
- **The 18.10b gate with a real Claude Code client on trial** is the
  orchestrator's check before the epic merges, not part of the build.
- **Not built, on purpose:** snapshot `taken_by_token`, the raw settings
  editor over MCP and archive (Epic 11; archive came in Epic 11a).

## Epic 10c: REST API

Serves SPEC 23, with the edits to 3, 12.1, 12.9, 12.10, 14.7, 14.12 and
14.19, and the gate in 18.10c. The task pull requests were #776 (A1, the
core), #771 (D1, the operator guide), #779 (T1, the token page and switch),
#777 (O1, the OpenAPI document), #778 (V1, the invariant tests) and #780
(F1, integration fixes).

Rulings (Todd, 2026-11-21), now in SPEC 23 and 14.19:

1. The routes are `GET /api/v1/tools` and `POST /api/v1/tools/<name>`, not
   resource-style routes.
2. REST API access has its own switch, off by default, with `FOREMAN_API=off`
   and the audit action `api-set`. The MCP switch is unchanged.
3. The REST API is P1 and was built before Epic 11b, so Gate C's security
   review covers it.
4. An OpenAPI 3.1 document is generated from the tool schemas at
   `/api/v1/openapi.json`. It needs a token and describes that token's scope.
   No OpenAPI library was added.
5. Every tool refusal is 422 with an English `error`. A fault Foreman cannot
   word is 500.
6. One token has one budget across `/mcp` and `/api/`.
7. A call is handed in process to the same per-scope SDK server `/mcp` uses
   (ADR 0029).
8. No new dependency.

### Delivered

- **The REST API core (SPEC 23.1 to 23.9; ADR 0029; #776).**
  `GET /api/v1/tools` and `POST /api/v1/tools/<name>` in
  `internal/web/api.go`. A call becomes an MCP `tools/call` message and goes,
  in the same process and with the request context kept, to the SDK server
  for the token's scope. No tool file changed, so scopes, schema checks,
  confirm codes, the snapshot cap, the chat prefix and the audit entries are
  the MCP ones. The answers are 200, 422 (with the result when the tool gave
  one), 404 for a tool outside the scope, 400, 415, 405 with `Allow`, 413 and
  the plain-text refusals of `/mcp`. Every other `/api/` path is gated, then
  answers 404.
- **One set of token checks for both paths (SPEC 14.19, 23.9; #776, #780).**
  The address failure limit, the bearer token, the per-token limits, the write
  deadline lift, the whole-body read and the second token check are built once
  in `tokenChecks` in `internal/web/mcp.go`. A token has one budget across
  `/mcp` and `/api/`. Each path keeps its own switch, Origin and method
  checks. An unknown `/api/` path needs a valid token too: 401 without one,
  the JSON 404 with one (#780).
- **The REST switch (SPEC 23.8; #776, #779).** The `api` app setting is off by
  default, and `FOREMAN_API=off` forces it off. With it off, every `/api/`
  request gets 403 before the token is read. The API tokens page has a second
  switch beside the MCP one, admin only, audited as `api-set`. When
  `FOREMAN_API=off` is set the page says so and shows no button. The 4 MiB
  body cap covers the tool paths.
- **Both addresses on the token page (SPEC 12.10; #779).** The page that shows
  a new secret shows the MCP endpoint and the REST API base address `/api/v1`,
  both copyable, using `FOREMAN_PUBLIC_URL` when it is set. The "will not
  work" hint shows only when MCP and REST access are both off. New strings
  are in all eight languages.
- **The OpenAPI document (SPEC 23.10; #777, #780).** `GET /api/v1/openapi.json`
  is built once at startup from the same per-scope tool lists, in
  `internal/web/apiopenapi.go`. Each tool gets its input schema unchanged, a
  200 answer with its output schema, and the 400, 401, 403, 404, 405, 413,
  415, 422, 429 and 500 answers. It needs a token and describes that token's
  scope.
- **Fault answers (SPEC 23.5; #777, #780).** A JSON-RPC internal error (code
  -32603, such as a recovered panic) answers 500 with a general `error`, and
  other JSON-RPC errors answer 422. When the SDK handler answers with a status
  other than 200, the log keeps only the status and the first 200 bytes.
- **The operator guide (#771).** `docs/OPERATOR.md` has a "Use the REST API"
  subsection with `curl` examples (the tool list, a read, an action, a
  two-step call) and the meaning of each answer. `FOREMAN_API` is in the
  settings table, and the forward-auth and Origin notes cover `/api/`.
  `README.md` mentions it.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck` and the tests of
  the files it touched. The orchestrator ran `make verify` on each task and on
  the epic head.
- `api_test.go` (#776) covers every status, a confirm code moving between
  `/mcp` and `/api/` in both directions, the 403 while off, `FOREMAN_API=off`,
  the shared per-token and address budgets, and an answer outliving the
  server's write timeout. Every existing `mcp_*_test.go` passes unchanged.
- `api_parity_test.go` and `api_security_test.go` (#778) were written from SPEC
  23 and 14.19, not from the code. For the read, operate and admin scopes,
  `/api/v1/tools` lists exactly the tools `/mcp` lists, with the same
  descriptions, schemas and flags. A tool outside a scope answers 404 with no
  audit entry and no snapshot. With the switch off, the answer is 403 for any
  token and the token is never looked up. Each switch turns off only its own
  path. The Origin rule holds. A token in the query string or a cookie, a
  session cookie and trusted headers all get 401. Expired and revoked tokens
  get 401s naming the state, and a revoke while the body is held back is
  refused. The limits are shared. A code is refused for other arguments or
  another tool and is spent by any use, on either path. The snapshot cap is
  shared, the audit entries name the token, and no secret appears in an answer
  or a log line.
- `api.spec.ts` (#779) makes a token in the browser, turns REST access on,
  calls the API with Playwright's request API, then turns it off and gets 403.
  `api-tokens.spec.ts` checks both token screens in all eight languages at 320
  px, with axe and no horizontal scroll. It caught a button pushed off screen
  in German, Russian and Czech, so the button reads "Copy REST API address".
- **Whole-epic checks (orchestrator).** `make verify` passed on the epic head.
  The full browser suite passed on the epic head (3826 passed, 0 failed). The
  real-game suite, `make test-game`, passed on 4cc1dd81 with 0 failures,
  including `TestRealGameAPI` (`realgame5_api_game_test.go`, #778), which runs
  the SPEC 18.10c gate over HTTP against a real `factoriotools/factorio`
  container. The OpenAPI document was linted with `@redocly/cli` 1.25.11 with
  no structural errors.

### Gaps left

- **The REST switch's badge reuses the MCP badge's CSS classes**
  `state--mcp-on` and `state--mcp-off`. A neutral class name would be clearer.
- **The OpenAPI document has no `servers` entry.** A client generated from it,
  under a `FOREMAN_PUBLIC_URL` with a path prefix, must have its base address
  set by hand.
- **The linter's `info.license` warning is not addressed.**
- **The language sweep in `i18n.spec.ts` does not visit the API tokens page.**
  `api-tokens.spec.ts` covers its eight languages.

## Epic 11a: Sign-in, audit log, archive, settings and users

Serves SPEC 12.7, 12.9, 12.12 (new), 5.9, 13.1, 6.6, 7.6, 14.6, 14.10, 21.7,
21.8, 18.11a and the operator guide (14.2), with edits to 3.1, 3.2, 6.5, 14.1,
14.14, 17.5 and 21.4. Epic 11 was split on 2026-11-18: this epic is
everything but publishing the image and Gate C, which are Epic 11b (SPEC
18.11b). The task pull requests were #707 (the plan), #708 (R1, the image and
the release workflow), #709 (T0, the scaffold), #710 (T2, archive), #711 (T4,
General tab), #712 (T1, OIDC), #713 (T3, audit log), #714 (a status race fix),
#715 (T7, users), #716 (T5, the sign-in page), #717 (T8, the operator guide),
#718 (T6, the settings reach the pages), #719 (T10, translation review), #720
(integration fixes) and #721 (T9, the gate tests). The epic gate (SPEC 18.11a)
is Todd's check on the trial deployment and is not recorded here.

Rulings (Todd, 2026-11-18, and later in the epic), now in SPEC:

1. Epic 11 splits into 11a and 11b (18.11a, 18.11b).
2. OIDC needs a required group. Username from `preferred_username`, groups
   from `groups`, scopes `openid profile email groups`. Fixed names (12.7, 3.2).
3. A user is matched by issuer and subject. A header-made user with no
   password and no link is linked once by name; any other name clash refuses
   (12.7, ADR 0027). Todd accepted the risk of that link.
4. The audit log keeps 365 days (7 to 3650), swept hourly. The sweep is not
   audited (12.9).
5. The issuer must be `https`. Discovery is lazy. Calls take 10 seconds, follow
   no redirects and are size-limited (12.7, 14.1, 14.10).
6. The audit log is a Settings tab with filters and 50 entries a page (12.9).
7. `list_worlds` includes archived worlds. `archive_world` and
   `unarchive_world` are one call each (21.4, 21.7).
8. Lowering retention deletes nothing on save, and the count is shown (6.6).
9. A restore keeps a live world's archive state (5.9, 6.5).
10. `set_foreman_settings` is two-step (21.7, 21.8).
11. The save and mod upload limits are settings (14.6, 13.1).
12. The General tab holds five settings (13.1).
13. The Users tab (12.12).
14. The image is built for amd64 and arm64 (3.1).
15. Tags and signing are Epic 11b, with ADR 0028 (not written yet).
16. One mod limit covers uploads and portal downloads (14.10).
17. A deleted proxy or OIDC user comes back at their next request (12.12).
18. `foreman reset-password` resets a forgotten password on the host (12.12).

Later rulings in the epic, now in SPEC: a retention change shows a confirm page
whenever it would remove automatic snapshots, counting every world including
archived ones (17.5, 6.6); a deleted world restored from its final snapshot
comes back unarchived (6.5); the world-level mod zip ceiling is 2 GB and the
setting is the real limit (14.6); `user-delete` is audited before the delete
(12.9, 12.12); `reset-password` reads the first line of standard input and is
audited as `password-change` by `system` (12.12).

### Delivered

- **OpenID Connect sign-in (SPEC 12.7, 3.2, 14.10; #712, #716, ADR 0027).**
  `oidc` joins `FOREMAN_AUTH_METHODS`, with `FOREMAN_OIDC_ISSUER`,
  `_CLIENT_ID`, `_CLIENT_SECRET` (and `_FILE`) and `_REQUIRED_GROUP`, all
  required with `FOREMAN_PUBLIC_URL`; an `http` issuer is a startup error.
  `GET /signin/oidc` and `/signin/oidc/callback` run the authorization code
  flow with PKCE (S256), a state and a nonce. The ID token's signature, issuer,
  audience, expiry, nonce and the required group are checked. Discovery runs at
  the first sign-in, is kept once it works, and is refused when it names an
  endpoint that is not `https`. Pending sign-ins live in memory (10 minutes,
  once, 1000 at most) with the `foreman_oidc` binding cookie. A failed
  callback counts against the address, but only after it names a sign-in this
  browser started, and each address may start 20 sign-ins a minute. Users are
  found by issuer and subject (migration 0011), created with the issuer and
  subject in one insert, and a headers-made user is linked once. The sign-in
  and signed-out pages show a "Sign in with single sign-on" button, alone with
  `oidc` only or beside the password form. Refusal pages say what happened and
  offer "Try single sign-on again" or "Back to sign in". `cmd/fakeidp` is a
  fake provider over `https` for tests. New libraries: go-oidc v3.21.0,
  go-jose v4.1.9 and oauth2 v0.36.0 (STACK 1, 7).
- **The audit log viewer and retention (SPEC 12.9, 17.11; #713).** The Audit
  log tab, `/settings/audit-log`, filters by user, action and world, newest
  first, 50 entries a page with an "Older entries" link that keeps the
  filters. Each entry shows time, user, token badge, action in words, world
  (a link, or its id marked deleted) and detail. The hourly sweep deletes
  entries older than the retention setting and writes nothing. A new `audit`
  text area has words for every action, and a test fails when a later
  `Audit...` constant has none.
- **Archive (SPEC 5.9, 6.5, 21.4, 21.7; #710).** `world.json` gains
  `"archived": true`; no snapshot is taken. The active world cannot be
  archived. Start, restart, switch and every relaunch path refuse an archived
  world, in the UI, MCP and the scheduler. The Worlds list holds them in a
  closed "Archived worlds (n)" foldout, and switch pickers leave them out.
  `archive_world`, `unarchive_world` and `list_worlds` (with `archived`) cover
  MCP. A restore keeps the live world's flag; a deleted world restored from its
  final snapshot comes back unarchived.
- **The General tab (SPEC 13.1, 6.6, 7.6, 14.6, 21.7, 21.8; #711, #718,
  #720).** `/settings/general` holds snapshot retention, the default stop
  countdown, audit retention and the save and mod upload limits, each audited
  with old and new value. Retention is now `Manager.SetKeepAutomatic`; the
  trim count and the pruning share one function. A save that would remove
  snapshots shows a confirm page with the count; a save that removes none goes
  ahead. `set_foreman_settings` is a two-step tool, and `foreman_settings`
  reports every setting. The settings reach the pages: every stop form and
  default uses the countdown setting, every upload reads its limit per
  request, the drop zones show the current figure, and the portal download cap
  is the mod upload limit. The mod zip ceiling is 2 GB.
- **Users (SPEC 12.12, 12.9; #715).** The Users tab lists users with their
  sign-in kind and creation date. Any user may delete another through a confirm
  page (`user-delete` is audited first, with the name and token count;
  sessions and tokens go with the user; deleting yourself, or two users
  deleting each other, is refused). A user with a password may change their own
  (current password, new twice, 12 characters, rate-limited, other sessions
  end). Proxy and single sign-on users have no password form.
  `foreman reset-password <username>` resets a password from the host, reading
  one line of standard input.
- **The settings tabs (DESIGN 6, 8.15; #709, #720).** Eight tabs, in two
  columns on phones and wrapping from 40 rem, with 44 px cells.
- **The image and release workflow (SPEC 3.1, STACK 10; #708).** The
  Dockerfile cross-compiles for amd64 and arm64 and carries OCI labels.
  `.github/workflows/release.yml` builds both and publishes nothing.
- **The operator guide (SPEC 14.2; #717).** OIDC for any provider (with the
  hairpin and renaming warnings), the General, Users and Audit log tabs,
  archive, the `tcp://` warning and the arm64 note.
- **Translation review (SPEC 22.6; #719)** in all seven non-English languages,
  with new text areas `audit`, `general` and `users`.
- **A fix found on the way (#714).** `server_status` read the controller's
  state before the action's result, so a finished action could appear beside a
  stale running state. It now reads the result first.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck`, the Go tests of
  its packages and the end-to-end specs for the pages it touched. The OIDC
  tests run against the fake provider over `https`. New end-to-end specs:
  `oidc`, `archive`, `auditlog`, `general`, `users`, `settings-reach` and
  `settings-tabs`, each at 320 px and desktop width in both themes with an
  axe scan.
- The gate tests (#721) cover the 11a gate in code: a replayed or foreign
  state, a missing binding cookie, a wrong nonce, audience or issuer, an
  expired token and a missing group are refused; a header user is linked once
  and the link cannot move; archived worlds cannot be started, restarted,
  switched to or relaunched, through the pages, MCP and the scheduler; restore
  keeps the flag; users cannot delete themselves, and a delete that cannot be
  audited changes nothing; a password change is rate-limited; and
  `set_foreman_settings` makes no change on its first call, refuses out-of-range
  values and gives a fresh preview when a value moved. They found one defect:
  a failed single sign-on page repeated the callback's code and state in the
  layout's hidden return fields. The integration fixes (#720) return those
  pages to sign-in, and the test passes.
- The whole-epic runs (`make verify`, the full end-to-end suite and the
  real-game tests) are the orchestrator's and are recorded in the epic's pull
  request.

### Gaps left

- **Group membership is checked only at sign-in (SPEC 12.7).** Removing someone
  from the group at the provider does not end a session already open (7 days
  idle by default). Delete them on the Users tab.
- **A header user is linked to OIDC by name on first sign-in (SPEC 12.7, ADR
  0027).** Until that user signs in once through OIDC, anyone in the group who
  can rename themselves at the provider could take the account. Todd accepted
  the risk. Operators must turn off self-service renaming at the provider; the
  guide says so.
- **Two tabs that start single sign-on at once** overwrite each other's
  binding cookie, so the first tab fails safe and the person tries again.
- **A database fault when reading an upload limit** falls back to the default
  limit, not a refusal.
- **The deleted-user message is generic by design.** It names no user, so a
  name from the address is never echoed.
- **The real identity provider is checked only by hand,** at the 11a gate on
  the trial deployment. Every automated test uses the fake provider.
- **No sign-out at the provider, userinfo call, token refresh or per-provider
  claim settings.** Left out on purpose.
- **The audit log has no date-range filter or CSV export,** and the action
  menu is sorted by recorded name, not by the translated words.
- **No user rename, role, password reset in the UI or block list.** A user
  made by a name clash must be deleted by an admin. `reset-password` reads
  standard input as it is, so a terminal shows what is typed; the guide shows
  the `< file` form.
- **Users are not in MCP** (SPEC 21.7).
- **The 320 px language layout test does not visit** the user delete page,
  because that needs a second user. Its fit was reviewed by reading only.
- **The release workflow was never run on GitHub.** It builds both
  architectures locally and passes the workflow linter. Publishing, login,
  attestation and the extra permissions are Epic 11b. The operator guide says
  no image is published until then.
- **The arm64 game image is experimental upstream,** and real-game tests run
  on amd64 only.
- **docs/I18N.md section 7's list of text areas** still omits several older
  areas (`about`, `account`, `apitokens`, `favorites`, `mods`).

## Fixes batch 13

Serves SPEC 8.11, 12.9, 13.1, 14.8, 17.1, 17.10 and 21.7, with DESIGN 6, 8.15
and 8.16. Issues: #724, #725 and #726, filed by Todd while checking Epic 11a on
the trial deployment. The plan was #729. The pull requests were #730 (settings
tab order), #731 (Pushover) and #732 (audit log table).

Rulings (Todd, 2026-11-19, and from the plan):

1. Pushover's two keys are stored in the existing sealed `url` column, with no
   migration. Pages and MCP show the host `api.pushover.net` only. Rejected:
   two new columns, which need a migration and a second copy of the secret
   rules.
2. Pushover priority is 1 for a crash, a crash loop and low disk, 0 otherwise,
   never 2. There is no device field and no test-notification button.
3. The audit log at 320 px stacks each row instead of scrolling sideways.
4. The Generate button's forced-colours contrast is fixed with one rule, as
   STATUS had recorded it at 1.11:1 since fixes batch 6.
5. The Settings link opens General (reversing the earlier choice that it open
   the restart schedule), and is marked current on every settings page.
6. Setting the first restart schedule costs 3 clicks (Settings, Restart
   schedule, Save schedule). Changing an existing one stays 2, through the
   Server page's next-restart link.

### Delivered

- **Settings tabs (#726, SPEC 13.1, 17.10, DESIGN 6 and 8.15; #730).** The
  tabs run General, Restart schedule, Notifications, Public mods, API tokens,
  Users, Audit log and the factorio.com account. The main Settings link opens
  General. It is marked `aria-current="page"` on General and `"true"` on every
  other settings page and sub-page, by changing what each page reports as its
  navigation target in Go (`navItem`). Direct links to a tab from other pages
  are unchanged. `TestSettingsSubnav` checks the order, the one current tab
  and the link's target and mark on all eight pages.
- **Pushover preset (#725, SPEC 8.11, 14.8, 21.7, DESIGN 8.16; #731).** The
  sealed `url` column holds the endpoint with the application token and user
  key; only the sender reads the keys back, and it posts them in the form body
  to the bare endpoint, so no key is in a request address. Keys are letters
  and digits, at most 64 characters. The body is form-encoded `token`, `user`,
  `message` (cut to 1,024 characters), `title` ("Foreman", cut to 250) and
  `priority`; cuts fall on a character boundary. The form swaps the address
  field for two password fields while Pushover is chosen (`notifyurl.js`); on
  an edit each empty field keeps its stored key; a refused form asks for the
  keys again. MCP `add_webhook` and `change_webhook` take `app_token` and
  `user_key`, and no answer holds a key. The operator guide, section 12, says
  where to find both keys.
- **Audit log as a filter bar and a table (#724, SPEC 12.9, DESIGN 8.15;
  #732).** The filters and an "Entries per page" choice (25, 50, 100, 200;
  default 50) sit on one row. The entries are a table with six fixed columns:
  Time, User, API token, Action, World and Detail. Newer and Older links keep
  the filters and a page size other than 50. The query uses a keyset cursor
  (`AuditFilter.After` beside `Before`), so pages do not shift while new
  entries arrive; a cursor past either end goes to the first page. At 320 px
  each row stacks (DESIGN 8.15).
- **Forced-colours Generate button (SPEC 17.1; #732).** One rule gives
  `.button--primary` and its hover and busy states `ButtonText`. The new test
  in `generate.spec.ts` failed on the base (axe, 1.11:1) and passes on all four
  projects.

### Verified

- Each task ran `make check-go fmt-check vet lint typecheck`, its Go tests and
  its end-to-end specs. New tests were written first and failed on the base
  where they cover a change in behaviour.
- S1: `go test ./internal/web -run 'Account|Settings|Schedule'`; end-to-end
  `schedule`, `live-ops-scaffold` and `user-menu` (104 passed), and
  `click-budgets`, `account`, `general` and `settings-tabs` (224 passed).
- N1: `go test ./internal/notify ./internal/i18n ./internal/store
  ./internal/web`, including a golden body for each priority, truncation at
  both limits with multi-byte text, delivery to a fake endpoint (keys in the
  body, none in the address or logs, 400 not retried, 429 retried) and MCP
  answers, logs and audit entries with no keys; `notifications.spec.ts` (32
  passed) and `i18n.spec.ts` (46 in each of four projects).
- A1: `go test ./internal/store ./internal/i18n` and the whole
  `./internal/web` package; `auditlog.spec.ts` and `long-words.spec.ts` (80
  passed), the forced-colours test in `generate.spec.ts`, and `i18n.spec.ts`,
  whose 320 px layout test now seeds a token and a long detail.
- The whole-epic runs (`make verify` and the full end-to-end suite) are the
  orchestrator's, on the epic head.

### Gaps left

- **The Pushover end-to-end test adds and edits a webhook but sends nothing.**
  Delivery is tested in Go only, against a fake endpoint.
- **There is no test-notification button,** for any preset (ruled out).
- **Screen readers read the labels of a stacked table twice** (the visible
  label and the header). This is the repo-wide table pattern and is not
  changed here.
- **The audit log still has no date-range filter or CSV export** (from Epic
  11a).
- Still deferred: as listed under Epic 11a.

## Fixes batch 14

Serves SPEC 16.3, 17 and 22, with DESIGN 2, 6, 8.1, 8.3 and 11. Issues: #736,
#737, #738 and #740 from the whole-app UI climb (#741), and #751, which Todd
added on 2026-11-20. The plan was #744. The pull requests were #745, #747,
#748 and #749 (#740, folding the climb's override blocks), #746 (#736, the live
status strip), #752 (#737 and #738, the breadcrumb and the labels), #753 and
#754 (#751, folding the remaining repeated selectors) and #750 (a test time
limit).

Rulings (the plan, then Todd and the orchestrator, 2026-11-20):

1. The live status strip reuses the existing stream: the same server-sent
   state update and out-of-band swap of `#site-state` that the Server page
   uses, sent to every signed-in page. Server, Console and Chat keep their own
   stream and are not connected twice.
2. The CSS tasks are pure refactors: nothing may look or behave differently.
   Each override was folded into the earliest rule for the same selector and
   condition. Where a fold would change which rule wins, the task kept the
   result the same or left the rule in place.
3. Every refactor task is proved by a pixel comparison of every page before
   and after.
4. Todd approves the breadcrumb and label screenshots before that pull request
   (B1).
5. The climb's "wide frame" part of `foreman.css` was also override blocks, so
   #740 covered it too. The orchestrator added two tasks for it: #748 (the
   first half) and #749 (the rest).
6. Todd added #751 (the selectors still defined more than once in the older
   parts of `foreman.css`) to this batch. It became #753 (the first half of the
   file) and #754 (the second half).
7. The breadcrumb also goes on the Settings pages, as "Settings > page", with
   Settings linking to General and shown as plain text on General itself. The
   Server page gets none: its trail would be one item. Column names and fact
   names are 13 px sentence case; only the overline keeps mono capitals.
8. B1 also hides a closed player row's buttons
   (`.player-actions:not([open]) > .actions`), because the shorter
   sentence-case header otherwise squeezed the hidden Kick button at 320 px
   in Polish. Nothing visible changes.
9. The large view paging test gets Playwright's slow time limit (#750). Its
   samples alone take 28.4 to 28.9 seconds of the 30 second default on a quiet
   machine, and it timed out in 3 of 5 full runs under load. No assertion
   changed.

### Delivered

- **The status strip is live on every signed-in page (#736, SPEC 16.3,
  DESIGN 6; #746).** A new route, `GET /state/events`, sends a `strip` event
  holding only the two out-of-band swaps of `#site-state` and `#nav-lamp`,
  and only when the state changes. It ends on sign-out or idle expiry, at
  shutdown, and when another world becomes active, so the browser reconnects
  to the new one. A signed-in page that shows a state and has no stream of its
  own connects through its `<main>` element (`view.StripURL`). `live.js` now
  loads on every signed-in page, so the "live updates stopped" note works the
  same way there. SPEC 16.3 now says so in one sentence.
- **The CSS override blocks are folded (#740; #745, #747, #748, #749).** Every
  block the climb added near the end of `foreman.css` was folded into the rule
  it restyled, or moved into the section it belongs to. The shell's wide-screen
  rail now sits with the shell, and each component's narrow, wide-screen,
  forced-colours and reduced-motion rules sit with the component. The stamped
  labels (the overline and the fact names) were the last blocks left late
  because equal-weight label rules came earlier; #749 folded them too, and
  the file now ends with the webhook form. #749 also removed `.crumb::after`
  and `.plate::after`, which nothing drew.
- **Repeated selectors folded (#751; #753, #754).** The older sections of
  `foreman.css` defined 170 selectors more than once, 215 extra definitions in
  all. Each was folded into one rule unless it was a shared group. The count
  fell to 79 selectors and 87 extra definitions. DESIGN 11 describes the layout and the shared groups.
- **The breadcrumb is navigation (#737, DESIGN 6 and 8.1, SPEC 17; #752).**
  One shared `crumb` partial in `layout.html`, fed by a small `crumbs`
  template function, renders `<nav class="crumb" aria-label="Breadcrumb"><ol>`
  with the current page last and marked `aria-current="page"`. All 25 pages
  that had a `.crumb` call it, and so do the 11 Settings pages. Links keep a 1 px
  underline at 68 % of their colour (3:1 or better, WCAG 1.4.11) and take the
  full colour on hover. The chevrons are drawn by CSS and painted `CanvasText`
  in forced colours. The new string `common.crumb.label` is in all eight
  languages. The Settings pages open with "Settings > page" in place of their
  overline.
- **Labels in sentence case (#738, DESIGN 2; #752).** Table column names, the
  heads of the world, token and user lists, and fact names are 13 px Plex Sans
  at weight 600 with no capitals, tracking or mono face. The rule for the
  plate and status card fact names, which repeated `.facts dt`, is gone. The
  Chinese exception now covers only the overline.
- **The paging test no longer times out under load (#750).**

### Verified

- **No change beyond the issues, proved by pixels.** The climb's capture tool
  took all 28 pages in light and dark at 1920 and 390 px (94 shots) for the
  base, for each refactor task (L1, C1 to C6) and for the epic head. Two
  captures of the base differ only in the console's log times, the RCON port
  and the free-space figure; each refactor task and the epic head (against
  B1's final head) differ from their base only there. B1's differences are
  the breadcrumb and the labels Todd approved from side-by-side screenshots.
- Each task ran `make check-go fmt-check vet lint typecheck`, its Go tests and
  the end-to-end specs for the pages it touches. Each task's head also passed
  `make verify` and the full end-to-end suite. Runs that failed only under
  machine load passed when the specs were rerun alone; the one real failure,
  a save bar covering the focused control by 3 px after B1's first version,
  was fixed in B1 without changing the test.
- Code reviews ran on every task; L1 also had a security review and B1 an
  accessibility review. Their findings were fixed or ruled on above.
- The epic head before this fold (d1d80a88): `make verify` passed, and the
  full end-to-end suite passed 3,798 tests (290 skipped by design).
- No real-game check: nothing touches server control, worlds, snapshots, mods
  or game versions.

### Gaps left

- **The world name in the status strip (and on the Server page) does not
  update live after a world switch** until the page reloads. The state beside
  it does.
- **87 extra definitions of 79 selectors remain in `foreman.css`.** Every
  one is a shared group kept on purpose, where one rule gives several
  selectors the same look and a member also has a rule of its own. Splitting
  them would copy values or change which rule wins. #753 and #754 list them.
- **Some declarations look dead but were not provably dead, so they stay:**
  the phone tab grid's `flex-wrap`, `scroll-padding-inline` and the
  `.subnav ul::after` sizes; the `flex` values of `.mod-status__line > p` and
  `.page-head--row`; the repeated padding of `.empty--art`; the weight of
  `.facts.stat-grid dd code`; and the properties of `main h2[id]::before`
  under `content: none`. A later pass with a computed-style comparison could
  remove them.
- **Four breadcrumbs still end on a link rather than on the page shown:** a
  favorite's delete step, the notification form, the created API token and the
  mod plan. As before, the page is a step under another page.
- **The Server page has no breadcrumb.** "Server > Console" and "Server >
  Chat" would be a meaningful trail on those two tabs; it is not built.

## Fixes batch 15

Serves SPEC 5.4, 6.4, 8.7, 9.2, 10.7 and 16.3, with DESIGN 8.2, 8.8 and 8.11.
Issues: #759, #760 and the seven items of #761, all found while taking the
screenshots for the documentation pass (#758). The plan was #763. The pull
requests were #764 (U1), #766 (R1), #767 (M1), #770 (G1), #772 (S1), #783 (P1)
and #782 (the epic-run skill).

Rulings (the plan, then Todd, 2026-11-21):

1. The crash warning for #760 reuses the existing message "A mod failed to
   load." rather than a new one, so no locale file changed.
2. A shared dependency is listed once, naming every mod that needs it, in the
   plan panel and on the review page.
3. Item 6 of #761 was reproduced first against a real
   `factoriotools/factorio` 2.1.24 container; item 7 is fixed from the
   mechanism without a reproduction.
4. Todd added task P1 during the batch: the Mods page's Plan card is capped to
   the window on wide screens, lists picks newest first and is alone in the side
   column.
5. From this batch the epic-run skill tests a task only on what it touched;
   CI runs the full suite on each push to an epic branch (#782).

### Delivered

- **Last played no longer runs into Disk use (#759, DESIGN 8.2; #772).** The
  worlds list no longer forbids wrapping in its time. The date, the clock and
  the age break only between themselves, 24 px clear of Disk use, at every width
  and in every language, and a narrow cell shows the age alone.
- **The sticky Plan card clears the top strip (#761 item 1, DESIGN 8.11;
  #772).** It now sticks below the shell's top strip, as the Generate preview
  does, so its heading never slides out of sight.
- **The Plan card is capped, newest pick first, and alone in the side column
  (#761, SPEC 9.2, DESIGN 8.11; #783).** On a wide screen the card is never
  taller than the window below the top strip: the heading, Clear plan and
  Review stay in view and only the list of mods scrolls. The newest pick is at
  the top, and a new pick scrolls the list back to it. Mod tools and the "Match
  mods to the save" card moved to the main column, so nothing passes under the
  sticking card. While keyboard focus is on a card under the Plan, the Plan stops
  sticking so the focused control is never hidden (WCAG 2.4.11).
- **A shared dependency is listed once (#761 item 2, SPEC 9.2; #767).** The
  plan panel shows it under the topmost pick that needs it, and both the panel
  and the review page name every mod that needs it. The review reason has its
  own message for several needing mods, in all eight languages.
- **The Server page's snapshot card updates live (#761 item 5, SPEC 6.4 and
  16.3; #767).** The card is a live region. The event stream reads the list when
  it opens, when an operation ends and when an action stores its result, so a
  snapshot a mod install has just taken shows with no reload.
- **The update page's dropdown shows the target (#761 item 4, SPEC 10.7;
  #764).** The server marks the target selected once the list loads. A target not
  in the list leaves "Choose a version" chosen, and the typed box keeps it.
- **Generate no longer restores stale fields on Back (#761 items 3 and 6, SPEC
  5.6; #770).** The Generate form turns off the browser's form restore, so a
  page reached by Back shows the server's whole, consistent form. The first
  Generate world then makes the map shown (the Ribbon world refusal was
  Foreman's own refill message, caused by a restored preset beside a stale hidden
  field; the real 2.1.24 game makes that world without complaint). A page restored
  from the back-forward cache redraws its "[modified]" mark, warnings and
  preview.
- **A newer preview draw cancels a running Preview map (#761 item 7, DESIGN 8.8;
  #770).** Its older reply can no longer put in its own seed, and a Preview map
  reply that lands under the open large view shows there.
- **A mod's non-recoverable error is named (#760, SPEC 8.7; #766).** The game's
  line "The mod X (version) caused a non-recoverable error" is recognised as the
  `mod-error` reason, and the crash warning says the mod and version without the
  stray quote it had before. Two real 2.1.24 captures are in
  `internal/game/testdata/2.1.24/`.
- **The epic-run skill tests only what a task touched (#782).** CI runs the
  full suite on each push to an epic branch and on the epic pull request.

Verified: each task with `make verify` and the e2e specs for the files it touched; S1 and M1 also with the full e2e suite (3,820 and 3,770 passed; M1's 32 failures were one plural rule, then fixed and rerun green); CI's full run on each push to the epic branch; `make verify` on the fold head dacb2e11 (coverage 90.8%); `make test-game` on 171b2a2d, where seven tests lost the game port to another real-game run and then passed on a rerun; real 2.1.24 containers for the Even Distribution crash loop (R1) and a Ribbon world with Space Age off (G1).

### Gaps left

- **#760 was not reproduced.** The "could not tell why" warning did not appear
  with the real mod on a real 2.1.24 game, on a normal start, on a Foreman
  restart in the middle of a crash loop, or after the loop stopped. The batch adds
  recognition and clean wording of "The mod X (version) caused a non-recoverable
  error". What could not be tested is the deployment that took the screenshots
  (the mod installed from the portal through the web UI, on its build at the time).
- **#761 item 3's cause is inferred, not reproduced.** The likely cause is the
  browser restoring the visible fields on Back but not the hidden preset fields.
  No cause was found on a fresh visit.
- **#761 item 7 was fixed from the mechanism and never seen live.** An end-to-end
  test that delays the first reply pins it.
- **The crash wording "A mod failed to load." is slightly off for a mod that fails
  while the game runs.** #785 suggests "A mod stopped the game." in all eight
  languages.
- **The Server page's snapshot card does not follow a snapshot taken or deleted on
  the Snapshots page in another tab.** That page runs inline, with no task, so no
  live event fires (#784).
- **With a long plan, a pointer user cannot see Mod tools on a wide screen**,
  because they scroll under the sticking Plan card. Keyboard users now reach them.
  This was so before the batch.
- **The worlds screenshots in the README still show the old layout.**
  `docs/screenshots/worlds.webp` and `worlds-light.webp` were not retaken: the
  docs pass made them on a throwaway stack with real worlds that no longer
  exists, and no script for it was kept.

## Fixes batch 16

Serves SPEC 5.1, 5.5, 5.13 to 5.16, 8.7, 8.14 to 8.16, 12.9, 14.3, 14.5,
14.6, 16.3 and 21.7, with DESIGN 7.12, 8.2 and 8.22, and ADRs 0030, 0031
and 0032. Issues: three small fixes (#781, #784, #785) and three features:
the players of a save with their play time (#769), the factory map (#773)
and world file export and import (#774). The plan was #791 and the spec and
ADR task #792. The task pull requests were #793 to #808, #811 to #823 and
#825 to #832, and the CI fix #835; #809, #810 and #824 were closed because a
later pull request included them.

Rulings (the plan, then Todd, 2026-11-21):

1. A save is read in a one-off benchmark run with a control-only helper mod,
   never by starting a server (ADR 0030). The same run serves #769 and #773.
2. The players record is `worlds/<id>/players.json`, so snapshots, restores
   and clones carry it with no new code; a world file leaves it out.
3. A read, and an automatic map drawing, run by themselves only while no
   game runs on this Foreman. Beside a running game they run only on an
   admin's click, which first shows the memory the read needs.
4. The factory map is P1 with its full overlay, viewer and modes. Its
   default mode is manual for every world, new or existing. "Skip while
   players are online" governs the click to draw a running world, and is on
   by default.
5. A world file holds no snapshots for now (5.15 records format 2 as P2),
   only ever makes a new world, and is not in MCP or the REST API.

### Delivered

- **Switch back no longer goes missing (#781, SPEC 16.3; #793).** The Server
  page no longer shows Done without Switch back when a request lands
  between a switch ending and its result being stored. The page reads the
  status again when a result is stored while the status still shows an
  operation running.
- **The Server page's snapshot card follows the Snapshots page (#784, SPEC
  6.4, 16.3; #795).** A snapshot taken or deleted inline on the Snapshots
  page, in another tab, now shows on the Server page with no reload, and so
  does a settings save.
- **"A mod stopped the game." (#785, SPEC 8.7; #794).** The mod-error crash
  now reads this way in all eight languages, since it can also happen while
  the game runs.
- **Reading a copy of a save (#769, #773, SPEC 8.16, 14.3; #816, #797,
  #818).** The world's own image runs the save in benchmark mode for 8
  ticks in a one-off container with no network, 12 GiB of memory with no
  swap and 5 minutes, in the one preview slot. The save is hard-linked into
  the work folder and mounted read-only over the link (copied when linking
  fails, after the disk check). `mod-list.json` and `mod-settings.dat` are
  copies, and `config/`, `rconpw` and `server-settings.json` never enter the
  folder. The game writes only to a 1 GiB in-memory folder, and only the
  helper's named output is moved out. The pre-start check runs first, and
  the players read also compares the helper's list of active mods with the
  world's enabled mods afterwards. Every start (a switch, an update, a
  rollback, a mod change or a restart too) cancels a running read and waits
  for its container to go before the game's container is made, and a read
  asked for meanwhile is refused (#835). Any operation that takes a world's
  lock, a person's or Foreman's own such as the mod settings run, cancels a
  running background read or map run instead of being refused as busy
  (#835). Two helper mods,
  `internal/game/saveplayers` and `internal/game/factorymap`, each write
  their output on 1.1, 2.0 and 2.1.
- **Players of a save (#769, SPEC 8.14; #804, #801, #806, #822, #825).**
  The players record is read from a copy of the save when the world page
  asks, by itself while no game runs, or on an admin's click beside a
  running game. It is kept only for the save that loads next. After the
  read, join and leave lines add each session, under one lock per world
  shared by reading and tracking, so replayed lines count once. A crash, a
  game re-attached after a Foreman restart, or a container gone while
  running marks the record inexact, and the next view while stopped reads
  it again. Mod runs ignore the record, an upload of a save removes it, a
  keep-save clone carries it and a fresh-map clone has none. The world's
  Overview lists every player with play time, last seen or "Online now", and
  "Played before Foreman tracked this world". It has states for reading,
  inexact, a failed read ("tracked since"), never read, no save and an
  unsupported game version, and a "Read again" button that, beside a running
  game, states the memory the read needs. The panel works without script;
  with script it asks again every 3 seconds while a read runs.
- **Total play time (#769, SPEC 8.15; #805, #822, #826).** Foreman parses the
  game's plain `/time` reply (hours, minutes and seconds, in the game's own
  list form) and refuses any form it has not seen. While the game runs it
  asks at most once a minute and at each finished save, never through Lua.
  The Server page shows "Play time" in hours and minutes and an "N players in
  this save" link to the Overview's list; it says nothing until a record
  exists and "Being read" while the first read runs. It follows
  `UpdateSavePlayers` live.
- **The factory map (#773, SPEC 5.16; #799, #800, #802, #818, #820, #821,
  #829).** One picture per surface from the game's own chart, at most 4096
  pixels a side, with a transparent outline layer for each of eight
  building categories on the same grid. Pictures are kept in
  `factorymaps/<world-id>/` under the data folder, replaced atomically, and
  removed when the world is deleted (and at startup for a world deleted
  while Foreman was down). The mode (automatic, manual or off, and "skip
  while players are online") is in `world.json`, and a missing or damaged
  setting reads as manual with skipping on. Automatic mode draws a world
  stopped for 2 minutes when no game runs on this Foreman. A draw beside a
  running game asks the game who is online, and refuses when it cannot ask.
  The helper's output is checked as untrusted: regular files only, opened
  without blocking, held to 512 MiB in all and to the counts of 14.6. The
  viewer is a world tab after Snapshots: zoom by wheel, keys, buttons or
  pinch, pan by drag, arrow keys or a tap, a surface list, a checkbox and
  colour swatch per layer, and a download of the base or of the base with
  the ticked layers, at most 2 combined downloads at once. While a drawing
  runs the page polls every 3 seconds and keeps focus across the swap.
- **World file export (#774, SPEC 5.13; #814, #823, #827).** "Export world
  file" on a world's Overview streams one ZIP: all saves (the default) or
  only the save that loads next. The world's lock is held only while the
  files are hard-linked into a staging folder. Secrets, `rconpw`,
  `players.json`, hidden files and leftovers stay out, and each entry keeps
  its file's modification time. At most 2 exports run at once, and a failure
  partway breaks the download. Each export is audited as `world-export`.
- **World file import (#774, SPEC 5.14, 14.5, 14.6; #812, #828, #827).** A
  third form on the Import page uploads a world file to a staged name, which
  is checked under every limit of 14.6. A preview shows the facts, the
  game-password and mod-data warnings, the name field and the image
  download. Confirm makes a new world. A name in use, an empty name or too
  little disk space shows the preview again with no second upload. No file
  chosen, or a file over the limit, shows on the Import page's own field.
  Each import is audited as `world-import`.
- **Tests.** A fake game answers the players read and the map run (#798,
  #813). Hostile-output tests cover the map parser and store (#807, #830),
  and attack tests the world file import (in #812). End-to-end specs cover
  the map (#811), world files (#815) and the players of a save (#803).
  Real-game tests cover the map on 2.0.77, 2.1.24 and 1.1.110 (#819), the
  world file round trip on 2.1.24 (#817), and the players read on 2.0.77 and
  2.1.24, with the tick and players checked against a separate game run and a
  hostile write to the save that must fail (#808). The e2e tests count only
  game creates, since a page view may now start a players read (#835).
- **A snapshot always matches its files (#835, SPEC 6.7).** A snapshot now
  records each file's size and time from the copy it holds. Before, a
  session ending while a snapshot was taken could rewrite `players.json`
  between the listing and the copy, and a restore was then refused with
  "players.json has changed".
- **Translations.** New locale areas `saveplayers`, `factorymap`,
  `worldexport`, `worldimport` and `worldfile`, with additions to `server`,
  `audit` and `crash`, in all eight languages, reviewed a second time
  (#832).

Verified: each task with `make verify` and the e2e specs for the files it
touched; CI's full run on each push to the epic branch; `make verify` on
the combined branches; `make test-game` in full on 76eba729 (61 minutes,
with the map's played saves on 2.0 and 2.1, and the 4 resource label skips
that were there before); `TestRealGameWorldFile` and A12's players tests,
including two played saves (`FOREMAN_TEST_PLAYERS_SAVE`, `_SPACE_AGE` and
`_MODS`): dans-2.0 on 2.0.77 and cur-2.1 on 2.1.24, two players each; and
the fold head with `make e2e E2E_ARGS="tests/design.spec.ts
--workers=4"`. The hand check of the three trial saves is recorded in the
epic pull request.

### Gaps left

- **A staged world file is not tied to the user who uploaded it.** Foreman
  has no roles, and the staged name is 64 random bits, so any signed-in
  user who learned it could confirm it.
- **No test sends a cross-site POST to the new routes.** The server-wide
  check covers them.
- **The players record:** `not-supported` is never returned yet, because no
  game line is known to refuse benchmark mode. There is no time limit on
  how long a re-attached game's replayed log may take to catch up. Without
  script the players panel shows the state at load and does not refresh
  while a read runs. The Overview's players panel polls every 5 seconds
  while the world runs, instead of following the live stream.
- **Players read end to end:** a start cancelling a read and the
  not-supported state are covered by unit tests only, since the fake's reads
  finish at once. The real-game test checks a played save's player list only
  when one is named in `FOREMAN_TEST_PLAYERS_SAVE`, since no save with
  players is committed.
- **Map runs skip the after-run check of the mods the game loaded.** Only the
  pre-start check runs. A wrong mod set can only draw a slightly different
  picture and never touches a world file.
- **A failed map drawing is tried again once after a Foreman restart**,
  because the store keeps a failure next to the previous picture's save
  rather than the save that failed.
- **Map surfaces past the helper mod's 360 MiB chart cap are left out**,
  with no note on the page.
- **The map pans only by arrow keys, drag or tap.** There are no pan buttons,
  and a screen reader's browse mode keeps the arrow keys.
- **The layer swatch colours are written twice**, in
  `internal/factorymap/categories.go` and `static/factorymap.css`. The map
  viewer's zoom code is also separate from the Generate preview's (#833).
- **The map real-game test needs a played save** named in
  `FOREMAN_TEST_CHARTED_SAVE_2_0` or `_2_1`, since a headless game charts
  nothing, so `make test-game` skips that case unless it is set. The "other"
  and rail layers are not asserted. Automatic mode's timing is unit-tested
  only.
- **World file export:** the audit entry is written before streaming starts,
  so it records an export that was begun, and a download that fails after
  its first byte can only break the connection. The card's note about a
  running game is always shown, because the world page does not know
  whether the game runs. The real-game round trip covers only 2.1.24 and
  "All saves".
- **World file import:** a bad or newer file, or an unsupported game
  version, gets an error page with a way back, not a field error on the
  Import page. A staged file that fails a version check stays until the
  hourly cleanup.
- **The fake game's players setting is per process**, not per world.

## Epic 12: Modded planets and guided Generate

Serves SPEC 5.6, 5.7, 5.12, 7.4, 9.6, 9.14, 11.5, 14.3, 14.6, 17.10, 17.19,
18.12, 21.7 and 23.2, with DESIGN 8.6 and 8.8 and ADR 0033. Issue #775. The
plan was #839. The task pull requests were #840 to #853 and #855 to #866.

Rulings (Todd, 2026-11-22, then the plan's own):

1. Generate is three steps: the world, its mods, then its map settings.
   Step 1 always lands on step 2, with a way to skip to step 3, so the
   3-click budget for generating with the defaults stays.
2. A world with no save and a pending `new_map` is "Unfinished". That is
   derived, never stored. Foreman never deletes one, and Continue opens
   step 2 while mod picks are left, otherwise step 3.
3. The planet list comes from the world's mod-settings run and replaces the
   embedded list. It is one run and one cache, under the same key as the
   settings.
4. The fast preview, territories and resource counts load the world's mods
   (ADR 0033). On 2.1 an opt-in real-game picture draws a planet from a
   real game with `--test-save`. It is kept in memory, with no disk store.
5. MCP and REST gain `get_new_map` and `set_new_map`.
6. Saving step 3 takes no snapshot, because the world has no save.

### Delivered

- **Planet catalogue (SPEC 5.6, 14.6; #840, #844).** `mapgen.Catalogue`
  holds a world's planets, controls and resources. `Builtin` wraps the
  embedded data. `ParseWorldPlanets` checks a world's file under 14.6: at
  most 1 MiB, 64 planets, 512 controls, 128 per planet and 512 resources;
  names matching `^[a-z0-9_-]{1,64}$`; a planet default kept only when it
  is finite and from 0 to 1000; invisible Unicode removed; rich-text tags
  removed and names cut to 100 characters.
- **The mod-settings run exports the planets (SPEC 9.6; #843).** The same
  run writes `planets.json`, and both are cached under one key.
  `Controller.WorldCatalogue(ctx, id, lang)` is the one entry point. A world
  whose mods are all built in gets the embedded list with no run. A failed
  or refused run gives the embedded list plus the reason.
- **Previews with the world's mods (SPEC 14.3, ADR 0033; #842, #848, #850).**
  Preview, territory and resource runs take a world. Its mods are
  hard-linked into the work folder, with `mod-list.json` and
  `mod-settings.dat` copied. The limits are 1 GiB and 60 seconds for each
  planet drawn, at most 10 minutes in all. A world mod named like Foreman's
  helper mod refuses the run. The server checks planets against the stored
  catalogue, never against the form. The preview store holds 160 MiB. The
  preview, mod-settings and picture runs share one work-folder fill and one
  map-creation script.
- **Step 3 save (SPEC 5.6, 12.9; #841, #849).** `world.SetNewMap` writes or
  removes the map files and updates `new_map` for a world with no save,
  under the world's lock and with no snapshot. It is refused while the
  world's server is starting, running, stopping, crashed or crash-looping,
  checked under the lock. Each save is audited as `new-map`.
- **The map form from a catalogue (#847).** `MapForm` is built from a
  catalogue, and the map exchange string moved with it.
- **Step 3 page (SPEC 5.6, 11.5, 17.19; #852).** `/worlds/{id}/new-map`, for
  an Unfinished world only. It has the step bar, "Fill from a map exchange
  string" (a string from another game line is refused, with a link to the
  world), a list of controls the mods no longer have, a wait on the server
  without script, and a form loaded after the page with script. Saving is
  refused while the embedded planets stand in for a world that has mods of
  its own.
- **Labels and notes (#855, #860).** A planet or control that Foreman knows
  keeps Foreman's translated name; a mod's label is used only for names it
  does not know. A shared control shows "Also changes *planet*" and is
  written once. A planet's own default, such as 15000%, is where the select
  starts, and is written back unchanged unless moved. The fast preview says
  what it cannot show, notes a picture of almost one color, and says when it
  fell back to the embedded planets. A mod named like the helper mod has its
  own message.
- **Step 2 (SPEC 5.6, 9.14; #853).** For an Unfinished world the Mods tab
  and the review show the step bar, "Skip to map settings" and, after an
  install, "Next: map settings".
- **Step 1 and Unfinished worlds (SPEC 5.6, 5.7, 5.12, 17.10; #863).**
  `/generate` asks only for the name, version, Space Age, favorite and image
  download, and lands on step 2. An "Unfinished" badge and a Continue link
  show on the Worlds list and the world page. `generate_world` keeps its
  arguments and results. Generating with the defaults is still 3 clicks.
- **The real-game picture (SPEC 5.6, 14.3, 14.6; #845, #859).**
  `Controller.Chart` runs `--create` and then `--test-save` in one container
  (1 GiB, 90 seconds, radius 16) with a control-only helper mod, and draws
  the result as the factory map does. The output is checked: one planet, at
  most 1,024 chunks of 32 by 32 pixels, at most 4 MiB. Step 3 of a 2.1 world
  offers "Draw from a real game", with a caption, "Back to the fast preview"
  and a toggletip. Busy, timeout and out of memory each have their own
  sentence. The picture is kept in the preview store by the planet's
  settings, the seed, the mods, the game version and the planet.
- **Stale controls (SPEC 7.4; #851).** The pre-start check refuses an
  Unfinished world whose saved map settings name a control its mods no longer
  have, with a link to step 3. It reads the catalogue cache only.
- **MCP and REST (SPEC 21.7, 23.2; #849, #860).** `get_new_map` (an admin
  tool) reports a world's planets, their controls, what each supports and
  the saved values, at each planet's own default. `set_new_map` takes a
  preset, a seed, control values or an exchange string, refuses an unknown
  planet or control by name, and writes through `SetNewMap`. Percentages are
  numbers, so 12.5 works.
- **Tests.** Hostile-input tests cover the planet file and the picture
  output. The fake game answers a planet run with one extra planet and
  control when the world holds `fake-planet`. About twenty existing e2e
  specs moved onto step 3 (#856, #858). New specs cover the flow
  (`generate-flow`, `generate-mods-step`), step 3 (`newmap`), modded planets
  (`modded-planets`), the real-game picture (`real-game-picture`) and stale
  controls (`newmap-stale`). Real-game tests cover the picture's real terrain
  on 2.1.24 and its refusal on 2.0.77 (#846), and a pass on 1.1.110, 2.0.77
  and 2.1.24 with five planet mods (#862): the catalogue (11 planets, 46
  controls and 28 resources), fast previews of all 11 planets (2.4 to 3.1
  seconds each, at most 657 MiB), resource counts and territories with the
  world's mods, step 3's settings reaching the first map (including a shared
  control and Moshine's default of 150), and the refusal after a planet mod
  is removed. The same pass found that `--create` ignores `autoplace_settings`
  names from a removed mod, so the stale-controls check needs no extension.
- **Translations (SPEC 22.6; #865).** New and changed strings are in all
  eight languages. A second review fixed 61 keys in `server`, `newmap`,
  `mapgen` and `audit`. A second round (#868) read `worlds` and `mods`, 8
  keys in each language, and changed 7: de 1, es 2, pl 3 and ru 1. Nothing
  in `js` changed.
- **Integration review fixes (#869).** Space Age has one definition, taken
  from the world's enabled mods, for the planet list, previews and
  real-game pictures, so a world whose flag disagrees with its mods is no
  longer refused. `set_new_map` refuses while the built-in planets stand in
  for a world's mods, as step 3 does. The step 3 save and `set_new_map` also
  refuse when the server's state is unknown because Docker cannot be
  reached. Step 1 shows the step bar, with steps 2 and 3 as plain text until
  the world exists. On the review page, Skip comes after Install. Both
  audit details end with the game version. Dead and duplicated code is
  removed, and `mapgen.StaleControls` is shared by step 3 and the pre-start
  check.
- **Flaky tests fixed on the epic (#857, #861, #864, #866).** #857 added a missing
  `{count}` to a plural string that CI refused. #861, #864 and #866 fixed races in tests. None was
  a product bug.

Verified: each task with `make verify` and the e2e specs for the files it
touched; CI's full run on each push to the epic branch; the real-game pass
on 1.1.110, 2.0.77 and 2.1.24 with five planet mods (#862); and the picture's
real-game test on 2.1.24 (#846). The full checks on the fold head are in the
epic pull request.

### Gaps left

- **A world with no `mods/mod-list.json` skips the stale-controls check.** The
  first start writes that file under the lock, which changes the cache key.
  This is rare, and `--create` then fails without writing a save, so nothing
  is lost.
- **The Server page's Done box has no "Next: map settings".** An Unfinished
  world that is also the active world ends a mod change there, and
  `server.html` has no link to step 3.
- **The real-game picture needs script.** It is an extra; the fast preview
  works without script.
- **Real-game pictures are dropped lazily.** A world's pictures go on the
  next picture request once its map is made, not at that moment. A restart
  loses them all.
- **On a pane 100rem or wider, a language whose Download and real-game labels
  together are wider than the pane wraps that line without the reserved
  room.** The pane then scrolls a little, as it does when the picture is at
  its smallest.
- **No real-game test covers a start after `set_new_map`, or a start from a
  1.1 world's step 3.** The first has unit and fake-game tests only. For 1.1,
  only the catalogue is checked for real.
- **`set_new_map` covers the preset, the seed and the planet controls only.**
  It does not set map size, starting area, enemy switches, terrain type or the
  map-settings fields.
- **Modded resources have no map colour,** because nothing in the planet
  file carries one. They are listed and counted under their own names.
- **The mod settings page (SPEC 9.6) still has no path without script
  (#838).** Step 3 has one. (Fixed in fixes batch 17.)
- **Left out, as planned:** map presets that mods add; achievement warnings
  for modded enemy controls; changing map settings after the first start
  (#207); real-game pictures on 2.0; a radius of 32 for the picture; sharing
  the viewer code with the factory map (#833, done in fixes batch 17); offset panning of the preview
  window; and a disk store for real-game pictures.
- **The planet-mod real-game tests need the folders of zips** named in
  `FOREMAN_TEST_PLANET_MODS` (2.1) and `FOREMAN_TEST_PLANET_MODS_2_0` (2.0),
  and skip without them.
- **`main` has the same three flaky tests that are fixed here.** The two
  timing tests for out-of-memory and refused stops (#861), the chat test's
  background `/time` (#864) and the click counter's reload (#866) are fixed on
  the epic branch only, and reach `main` with the epic.

## Fixes batch 17

Serves SPEC 5.6, 5.16, 8.1, 8.14, 9.6, 9.12, 9.14, 13.1, 14.3, 14.6, 14.10,
17.9, 17.10 and 21.4, with DESIGN 7.3, 8.6, 8.8, 8.11 and the Server page,
and ADRs 0034 and 0035. Issues: the test-drive findings from Epic 12 (#873
to #886) and three older ones (#833, #838, #854). The plan was #887. The
task pull requests were #888 to #913.

Rulings (Todd, 2026-11-22, then the plan's own):

1. Recommended ("+") dependencies stay ticked (SPEC 9.14); the Install
   button names their count.
2. The default mod size limit is 1,024 MB (the maximum stays 2,048 MB). A
   download over the limit is its own error, and every failed mod download
   is logged without the account.
3. Runs that load a world's mods get 2 GiB and 120 seconds per planet,
   measured with a heavy set of 31 mods. Base-game runs keep 1 GiB and 60
   seconds.
4. A control gets the on/off box by rule (the game says it can be disabled
   and it is a resource, or on 2.x cliffs or enemies), never by planet name.
5. Generate gets a new step 3, Mod settings, between Mods and Map settings.
6. The Server page panel lists every player of the save, online first.
7. Wiki icons only, as an opt-in admin setting; mod icons from zips are a
   new issue.
8. #854 was already met: every spec runs in the four Playwright projects.
9. The layer colours of the factory map stay duplicated in Go and CSS.

### Delivered

- **Planet mods with no settings keep their planets (#883, SPEC 9.6; #888).**
  When the definitions file lists no settings, a missing
  `mods/mod-settings.dat` is empty defaults, not a failure. A missing file
  with settings listed, an unreadable definitions file and an unusable file
  (link, pipe, too big) still fail.
- **Game labels for controls (#881, SPEC 9.6, 14.6; #888).** The
  definitions run also copies the game's own `.cfg` files (`base`,
  `elevated-rails`, `quality`, `space-age`, in the eight languages) from the
  world's image. They are read under the 14.6 rules, only the `*-name` and
  `*-names` sections are kept, and a label is looked up in mod text in the
  language, mod English, game text in the language, then game English.
  Nothing is downloaded.
- **The real-game button and the zip link no longer overlap (#873, DESIGN
  8.8; #899).** The picture's Download line is a wrapping flex line, the
  button and its info icon wrap together, and the second row is reserved at
  every width. Tests cover four window sizes and all eight languages.
- **Larger limits for runs with mods (#874, #884, SPEC 5.6, 14.3; #897,
  #902).** A preview, territory, resource count or real-game picture that
  loads a world's mods gets 2 GiB and 120 seconds per planet, at most 10
  minutes in all. The limit is 1.5 times the worst resource count (1407 MiB,
  Corrundum with every control at its most). An error now names the run that
  failed ("the resource count ran out of memory"). A planet with mod
  resource controls and no patches says "No resource patches were found in
  this picture. This planet's resources may be tiles, which can't be
  counted." A heavy-mods real-game test skips unless
  `FOREMAN_TEST_HEAVY_MODS` is set.
- **On/off boxes by rule (#876, #884 item 1, SPEC 5.6; #891, #906).**
  `controlToggles` gives a box when the game says a control can be disabled
  and it is a resource, or on 2.x a cliff or enemy control. Off writes size
  0. `mapgen.CategoryEnemy` is new and `CliffToggle` is gone. A real-game
  test with a tiny planet mod shows size 0 removes the cliffs and
  spawners.
- **Mod size limit (#875, SPEC 9.12, 14.6, 14.10, 17.9; #894, #895).** The
  default is 1,024 MB. A download over the limit is `modportal.ErrTooLarge`,
  refused from `Content-Length` before the body is read, and the message
  names the mod, its size and the limit. An admin gets a link to the limit,
  anyone else "ask an admin", and nobody gets Try again. Every failed
  download is logged with mod, version and reason, each cut to 200 bytes,
  never the username or token. MCP wording follows the token's scope.
- **Rich text in mod text (#877, SPEC 14.6, DESIGN 8.11; #892, #901, #907).**
  Setting names, descriptions, mod titles and summaries keep a safe subset
  of the game's rich text, rebuilt from an allowed list on the server: bold,
  the named colours as theme colours, object tags as a readable name, the
  warning and info sprites, `[space-age]`, key-binding macros as the action's
  name, and line breaks. The no-argument macros are read from a fixed list
  checked against the game's English text. Planet and control labels keep
  `StripRichText`.
- **Wiki icons, opt in (#882 wiki part, SPEC 13.1, 14.1, 14.10, ADR 0035;
  #901, #907, #908, #911).** An admin switch on the General tab, off by
  default and audited as `wiki-icons-set`, in MCP and REST as `wiki_icons`.
  On, an object tag is an `<img>` for `wiki.factorio.com` with
  `referrerpolicy="no-referrer"`, built only from a prototype name matching
  `^[a-z0-9_-]+$`, and a failed load shows the name. Every page sends
  `Content-Security-Policy: frame-ancestors 'none'; img-src 'self' data:`,
  with the wiki added only while the switch is on and never on a public
  page. The General page saves its numbers, the update check and the switch
  in one database transaction. The setting is read once per request.
- **Mod settings origin line and a no-script link (#838, #879, SPEC 9.6,
  DESIGN 8.11; #890, #898, #903).** Under a mod's group heading, "Required
  by", "Recommended by" or "Offered by" names the enabled mods that list it
  in `info.json`, each name a link to its group (links across the startup
  and map lists). Mods nobody names come first. The page has a link with
  `?wait=1` that reads the form on the server. Each group takes focus on a
  jump.
- **The Install button names the recommended mods (#880, SPEC 9.14; #893).**
  "Install 3 mods and 10 recommended", following each tick, and the same
  label without script.
- **The Done message keeps room for its close button (#885, DESIGN 7.3;
  #889).** The paragraph reserves the button's width on the end side, tested
  in English, German and Russian and at phone width.
- **The Players panel (#886, SPEC 8.1, 8.14, 8.15; #900, #909, #911).** The
  Server page lists every player of the save: online first with an "Online"
  chip and the session, longest first, then the others by play time, at most
  ten rows with every online player shown, then "All N players" linking to
  the Overview list. The caption reads "Players: 2 online, 7 in this save".
  Offline rows have no actions. Joins and leaves re-sort the rows live, with
  one announcement per change. `list_players` stays online-now.
- **One picture viewer (#833, SPEC 5.6, 5.16, DESIGN 8.8; #896, #905).**
  `viewer.js` (`window.ForemanViewer`) holds zoom, pan, pinch, the zoom keys
  and the three buttons for both the factory map and the Generate large
  view. No visible change. The large view now accepts `_` for zoom out.
- **Generate step 3, Mod settings (#878, SPEC 5.6, 9.6, 9.14, 17.10, 17.19,
  21.7, DESIGN 8.6, ADR 0034; #904, #912, #913).** Generate is now World,
  Mods, Mod settings, Map settings. Step 3 shows the startup settings with
  the mod settings page's own form, saves like it (a snapshot, then the
  write) and lands on step 4. With no startup settings it sends the browser
  on and the step bar shows "3. Mod settings (none)" as plain text; step 4
  then says why. Step 4's planets follow the saved settings through a second
  "planets" run keyed by the definitions key and the hash of
  `mod-settings.dat` (ADR 0034). `get_new_map` reflects saved settings.
  Continue opens step 3 only while the world has no `mod-settings.dat` and
  its map settings were never saved. A real-game test covers the planets run
  following a saved setting. Saving shows a busy state, and without script the
  step loads as the mod settings page does.
- **Review fixes (#895, #898, #905, #906, #907, #909, #910, #911, #913).**
  Each task was reviewed as it landed; the fixes are in its entry above. The
  translation review (#910) corrected 16 strings in 9 locale files.
- **Test fixes (#903, #908).** The key-use check saw the origin-line keys as
  unused because they were picked from a map, and the framing test expected
  the old exact policy. Neither was a product bug.
- **#854 needed no code.** `e2e/playwright.config.ts` runs every spec in
  desktop-dark, desktop-light, phone-dark and phone-light, so every
  `checkPage` already scans dark.

Verified: each task with its unit tests and the e2e specs for the pages it
touched; CI's full run on each push to the epic branch; real-game tests on
2.1.24 for the no-settings fix and the base game labels (#888), the cliff
and enemy boxes (#891), the planets run (#912) and a heavy-mods resource
count at the product limits (#897). The full checks on the fold head are in
the epic pull request.

### Gaps left

- **Mod icons from the world's zips are not built.** Wiki icons are the only
  object icons. This is the second half of #882, filed as #914.
- **Continue after Skip decides step 4 from the audit log.** Nothing else
  records that map settings were saved, so if audit pruning removes the
  `new-map` entry, Continue shows step 3 again. It can still be skipped.
- **The modded-planet real-game test was not run against the 2 GiB limit.**
  `FOREMAN_TEST_PLANET_MODS` is not set on the runner. The limit rests on
  the 31-mod heavy set and its measured peak of 1407 MiB.
- **The mod settings run's 4 GiB memory was not measured with a heavy mod
  set.** The earlier measurement covered resource counts only.
- **The time limit of 120 seconds rests on a survey, not a run here.** The
  survey saw 58 seconds on foliax; the longest run here was 28.5 seconds.
  The janus and nix planets were not measured, since they are not in the
  trial set.
- **A 1 GB download can fill a nearly full disk between space checks.** The
  Foreman checks the space before it starts, not as the file arrives.
- **The over-limit check on `Content-Length` has unit tests only.** The fake
  portal's e2e zips are small.
- **Two mods that require each other both sort as dependencies** on the mod
  settings page, and a mod the player chose that another mod also lists says
  "Required by". Only enabled mods count, and origin names are bare mod
  names, not titles.
- **The no-script `?wait=1` link is a GET that can start a run,** as the
  `/form` address already can.
- **The step bar shows step 3 as a link on the Mods pages** until the setting
  definitions of a modded world are cached. Step 3 then sends the browser on
  to step 4.
- **No test checks that `get_new_map` changes after a step 3 save.** The
  game-level test covers the catalogue it reads.
- **Tile resources such as Vesta's are not counted.** The page says so, and
  the fake game has no Vesta case in e2e; a unit test covers it.
- **Object names in rich text are prototype names made readable,** not
  localised. String-setting choices show their raw values, and mod titles in
  screen-reader-only text and in translated messages stay escaped plain
  text with the raw markup.
- **A rich-text icon's name joins its label's accessible name.**
- **Wiki icons follow the prototype name only,** so objects the wiki names
  differently show their name. Only the listed no-argument macros are
  handled; others still show as text.
- **The fake game supplies no game locale,** so web and e2e labels from game
  text are not exercised there.
- **Offline rows of the Players panel offer no ban or admin action.**
- **The layer colours stay duplicated in Go and CSS (#833).**
- **The German install label joins two messages** ("(dazu {count}
  empfohlene)"), because a message holds one count.
- **Left out, as planned:** mod icons from zips (above); resource-like
  terrain controls under Resources, so Muluna's lunar rocks keep the mod's
  category; starting recommended mods unticked; sharing the layer colours.

## Fixes batch 18

Serves SPEC 5.6, 9.6, 14.3, 14.5, 14.6, 14.10 and 17.20, with DESIGN 8.23 and
ADRs 0036 and 0037. Issues: #914 (mod icons from the world's mod zips) and
#920 (the star map), the last two open issues. The plan was #930. The task
pull requests were #931 to #949.

### Delivered

- **Mod icons from the world's zips (#914, SPEC 9.6, 14.5, 14.6, 14.10;
  ADR 0036, 0037).** An object tag in a mod setting, such as
  `[item=fake-widget]`, shows the mod's own icon on the mod settings page and
  in Generate's Mod settings step, with the object's name as its `alt`. The
  page asks for `/worlds/{id}/icons/{kind}/{name}.png`, never a file path.
  Foreman finds the path in the cached run output, checks it twice, reads
  the file from the installed zip without extracting it, crops the first
  mipmap, scales it and answers with a freshly encoded PNG. The order is
  the mod's icon, then the wiki icon (when the wiki setting is on), then
  the name. The game's own art is never served. Only a plain `icon`, or
  `icons` with one untinted layer, gets an icon (see the gaps).
- **The run export (#936).** The control mod gains a small
  `data-final-fixes.lua` that copies icon paths from `data.raw` into one
  invisible `mod-data` prototype, and `control.lua` writes them to
  `icons.json`, and the star map's places and routes to `starmap.json`. The
  control mod takes an optional dependency on each world mod so that it
  loads last. Both files come from the mod settings runs that already exist
  and are cached with their results, so there is no new game run. The fake
  game writes both files too. The icon index refuses the `recycler` built-in
  mod (#937).
- **The star map (#920, SPEC 5.6, 17.20; DESIGN 8.23).** Generate's map
  preview has a Star map picture beside the planet pictures. It draws the
  sun, a faint ring at each planet's distance, each location sized by its
  magnitude, moons beside their planet with an orbit ring when the data
  names a parent (PlanetsLib), and routes curved around the two ends'
  nearest common parent, as the game draws them. Hidden locations and the
  routes that touch them are left out. A modded location's own star map icon
  goes on top; a base-game place and the sun get the wiki's pictures only
  while the wiki setting is on, and a failed picture leaves the circle.
  Labels are drawn at the game's `label_orientation`. The picture opens in
  the large view with the same zoom and pan as a planet picture, and the
  "Download all drawn planets" zip holds it as `starmap.svg`, the same SVG
  the page draws. There is no table, separate page or world-page map.
  Plain Space Age worlds use star map data extracted from the real 2.0.77
  and 2.1.24 games and shipped with Foreman (#940). Later tasks fixed the
  look: rings to the edge, smooth curves and labels off the routes (#946),
  moons beside their planet at the game's own position (#948), and sizes in
  the game's proportions, measured from an in-game screenshot (#949).
- **Rulings.** Todd's, on 2026-11-23: hidden locations are left out; moons
  are drawn where the game places them; a place is a theme-coloured circle
  with a picture on top; the map is one more picture in Generate's preview;
  layered mod icons show only their name. The plan's own: an accessible name
  for the picture and a `<title>` for each planet, the spiral arc taken from
  community reports and checked against the game, the `starmap.svg` file
  format, and mod icons shown only on per-world pages whose run is cached.
  Pages carry `Referrer-Policy: no-referrer` while the wiki setting is on.
- **Review fixes.** A route repeated in either direction is drawn once, an
  icon path's mod prefix ends at the first `__/` (a mod named `x_` keeps its
  icons), and an icon name with `..` inside it is no longer refused (#932,
  #934). The star map's own review fixes: the picture fits each label as it
  is shown, a failed picture is removed without an inline handler, the faint
  rings are a little stronger, and the fake game has a straight route from
  its moon to its planet, so that case has a test.
- **Tests (#939).** `mod-icons.spec.ts` (8 tests) and `starmap.spec.ts` (7
  tests): the icon shows with the wiki setting off and on, a failed icon
  shows the name, the address answers `image/png` with `nosniff`, bad
  addresses are 404, a signed-out request is sent to sign in, the star map
  has the right titles, a curved route, a moon and its orbit ring, wiki
  pictures only while the setting is on, the large view's zoom, pan and fit,
  and `starmap.svg` in the zip with no image, script or wiki address; axe in
  both themes at desktop and 320 px.

Verified: each task with its unit tests and the e2e specs for the pages it
touched; reviews by the security, code, accessibility and design reviewers,
with findings fixed or listed below; the real-game test
`TestRealGameStarMapAndIcons` on 2.0.77 and 2.1.24 with Space Age (Vulcanus
at distance 10 and orientation 0.1, Nauvis to Vulcanus 15,000 km, no game
paths in `icons.json`) and, on 2.1.24 with 31 real planet mods, 647 icons,
12 locations and 19 routes, PlanetsLib moons naming their parent and five
modded star map icons that open from their zips (run 4.3 seconds, peak 661
MiB against the 4 GiB limit); and the existing `TestRealGameModSettings*`.
The full checks on the fold head are in the epic pull request.

### Gaps left

- **Layered mod icons show the object's name (ruling 7).** Only a plain
  `icon`, or `icons` with one layer and no tint, shift, scale or floating,
  gets an icon. Barrels and recipe variants with several or tinted layers
  keep their name.
- **Folder (unzipped) mods get no icons, and 1.1 worlds get none.** 1.1 has
  no `mod-data`. 1.1 has no star map either.
- **A mod with a space in its name is not in the control mod's load-after
  list,** so it could change an icon after the export. The result is a wrong
  or missing icon, never a failed run.
- **The export's size estimate is rough.** A huge pack with unusual names
  could have its icon file refused whole, and its pages then show names.
- **The browser may show a mod's old icon for up to 5 minutes** after the
  world's mods change (`Cache-Control: private, max-age=300`).
- **htmx fragments run `ShowModIcons` without the public-page guard.** It is
  safe because only two signed-in pages attach an icon index; a page that
  attached one without the guard would show icons on a public page.
- **No icons until a run is cached, and a refused icon is not remembered.**
  A bad file is read again on each request until the mods change.
- **Star map labels:** moons and modded locations that are not planets show
  the mod's label in English or the raw name, not a translation. The sun has
  no wiki picture and stays a circle.
- **"One planet" shows no star map,** and a modded world with no cached
  planets run shows none. A world with other mods whose planets could not
  be read shows none either, and so does 1.1.
- **Routes use the community-reported rule.** 2.1's route `shape` overrides
  are not read, so a mod that sets one could draw a different arc than the
  game.
- **The star map shows the mod set's map, not the save's:** which places a
  save has discovered is not read.
- **The real-game planet-mod check ran on 2.1 only.** There is no 2.0 planet
  mod set (`FOREMAN_TEST_PLANET_MODS_2_0` is not set).
- **Left out, as planned:** icons on the portal search, mod plan and public
  mods page; the star map in the REST API and MCP.

## Fixes batch 19

Serves SPEC 5.6, "Resource amounts". One issue, #955.

### Delivered

- **Spots of fluid tiles in the resource count (#955).** The count also
  finds spots of tiles that carry a fluid and are tied to a resource
  control: Vesta's methane and hydrogen sulfide. A spot is a group of one
  fluid's tiles joined by the ore gap. Its label gives its size in tiles,
  since pumping never runs out. The text list adds one list per fluid after
  the ores, most tiles first ("Methane gas: 14 spots"). Result files written
  before this change still decode. Strings are in all eight languages.
- **The rule used.** A tile counts when it carries a fluid and every
  autoplace control its noise expression reaches is a resource control, with
  at least one. No tile in the game sets `autoplace.control`, so the count
  follows the expression through the named expressions and functions it
  calls, with the planet's own names for them. The game's oceans reach a
  terrain control or none, and lava also reaches Vulcanus's volcanism, so no
  planet of the game has spots.
- **The zero-patch sentence.** "May be tiles, which can't be counted" is no
  longer said for a planet whose resource controls the count looked for
  spots of.
- **Limits.** Spots share the result file's limits with the ore patches, and
  only the counted planet's own tiles are looked at.

Verified: unit tests for the script, decoder, labels and page; the browser
specs for resources, the large view, Generate's preview and the star map,
including a fake planet with four spots; real-game check on 2.1.24 with
Space Age and Vesta, where Vesta at 1280 px found 13 methane spots and 7
hydrogen sulfide spots (`TestRealGameResourcesFluidTiles`, which runs only
when `FOREMAN_FLUID_TILE_MODS` names a folder of mod zips), and the Space
Age resource tests (`make test-game-resources`) on 1.1.110, 2.0.77 and
2.1.24.

### Gaps left

- **Fluid pools a mod places with no resource control are not counted,** and
  neither are rocks that give items when mined.
- **A control named only in a noise expression's local expressions is not
  seen,** because the game does not show local expressions to a mod.
- **Spots past the result file's limits are cut, smallest first,** and the
  page does not say so.
- **A fluid is named by its control's label,** since the catalogue holds no
  fluid names. Two fluids with the same name each add their own in brackets.
- **The summary's patch count includes spots.**
