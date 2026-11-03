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
  and 9). `make run` starts Foreman locally in adopted mode.
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
