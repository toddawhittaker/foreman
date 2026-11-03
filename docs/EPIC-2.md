# Epic 2 plan: Worlds and snapshots

Serves SPEC 18.2: the world model (5.1 to 5.3); list, import, rename,
delete and saves (5.4, 5.5, 5.8, 5.10, 5.11); snapshots and restore
(section 6, except 6.10). Branch `epic/2-worlds-snapshots`, cut from
`main` at 969201e. No GitHub issues are open for this area.

This plan lives only on the epic branch. The fold task (wave 4) moves its
lasting rules into SPEC.md and an ADR, then deletes it.

## Rulings

Settled by Todd (hard to undo):

- **R1. Active world** is a row in a new app-database table,
  `active_worlds(world_id PRIMARY KEY, since)`. A set of rows leaves room
  for several worlds running at once, unbuilt. SPEC 3.4 changes to allow
  this one piece of game state.
- **R2. World id** is made from the name: lowercase letters, digits and
  hyphens, at most 40 characters, with `-2`, `-3` and so on added when the
  id is taken by a world or a snapshot folder. An id is never reused and
  never changes on rename.
- **R3. `world.json`** has `format_version` (starts at 1), `name`,
  `game_version`, `space_age`, `load_save` (missing means the newest
  save) and `created_at`, with snake_case keys. Fields are only added.
  Rewrites keep fields Foreman does not know. A world whose
  `format_version` is newer than Foreman knows is listed but cannot be
  changed, with a message saying a newer Foreman wrote it.
- **R4. Snapshot folders** are named by UTC time, for example
  `20261103T142501Z`, with `-2` on a clash. Each holds `snapshot.json`:
  label, reason (`manual`, `before-delete`, `before-restore`, or the risky
  operation), `taken_at`, `taken_by`, and each file's path, size and
  modification time to the nanosecond. Disk use is read from manifests.
- **R5. Import reads only from a fixed inbox**, `<data>/import/`. The page
  lists its subfolders. A folder is imported by copying its top-level save
  zips, and its `mods/` subfolder if it has one. Todd's old games are
  saves only; mod sync from the portal is Epic 5 (9.8).

Settled by the orchestrator (easy to change):

- **R6.** The first world created or imported becomes active, so Epic 3
  always has a world to start. Switching is Epic 4.
- **R7.** Epic 2 runs in managed mode only. The end-to-end fixture and
  `make run` move to managed mode with `FOREMAN_HOST_DATA_DIR` set, so
  no Docker call is made. Adopted mode's `FOREMAN_GAME_DIR` (SPEC 4.8)
  comes with Epic 4. The world manager takes its folders as parameters.
- **R8.** Long operations run inside the request, with the read and write
  deadlines lifted on upload, download, import and restore routes only.
  The background step runner (DESIGN 7.6) comes with Epic 3.
- **R9.** Restore builds the restored world in a `.staging-*` folder,
  renames the live folder to `.restore-old-<id>-*`, then renames the
  staged folder into place. Startup recovery finishes or reverses a
  half-done swap. The path `worlds/<id>` never changes.
- **R10.** Uploading a save whose name already exists is refused.
- **R11.** Snapshot disk use counts each file once, by inode, since saves
  and mods are hardlinked between snapshots.
- **R12.** Two worlds may not share a name, ignoring case.
- **R13.** Retention (10 automatic snapshots per world) and upload limits
  (save 1 GB, other uploads 200 MB, SPEC 14.6) are constants. The
  settings table comes with Epic 10.
- **R14.** Labels "Before restore" and "Before delete", or the admin's own
  text for a manual snapshot. Times are shown as `2026-11-03 14:25 UTC`
  in a `<time>` element. Lists over 50 rows end in a "Show older" link.
- **R15.** `internal/world` exports `Manager.Lock(id)` (a try-lock that
  fails with "another operation is running on this world"), so Epic 3
  can hold it across stop, snapshot, restore and start.
- **R16.** One function returns a world's newest save (SPEC 7.11). Epic
  3's pre-start check must use it.
- **R17.** Every file operation inside a world, snapshot or the inbox
  goes through `os.Root`. Zips are never unpacked in this epic; only their
  entry names (`filepath.IsLocal`) and stated sizes are checked. Uploads
  stream through `MultipartReader` to a temp file beside the destination.
  Every write is temp file, fsync, rename. Copies get explicit modes
  (files 0664, folders 0775, SPEC 3.6). Imports copy, never hardlink.
- **R18.** Secondary-button hover changes from orange to grey (approved
  by Todd after Epic 1): token `--color-button-hover`, `#c4beb5` dark and
  `#e6e1d8` light, used by `.button:hover`; DESIGN.md updated to match.
- **R19. Fail safe on disk space** (Todd: fail safe as a general rule,
  without undue complexity). Before a snapshot or an import, Foreman
  estimates the bytes it will write (nothing for a hardlink, the full size
  for a copy) and refuses to start, with a clear message, when the
  filesystem's free space is short of that plus a fixed 2 GB reserve
  kept for the game's autosaves (revised after review: 1% of a very large
  disk refused every snapshot). All checks share one in-process count of
  bytes already promised to writes in flight, so parallel writes cannot
  together overrun the reserve. An upload is
  refused up front when its declared length exceeds free space; if the
  disk fills anyway, the temp file is removed and nothing changes. The
  snapshot page shows free space beside snapshot disk use. Free space comes
  from `syscall.Statfs`; no new dependency.

- **R20. Snapshots detect tampering** (Todd, after the security review:
  SPEC 6.8 trusts the game to write saves as new files, SPEC 14.1 does
  not trust the game). `snapshot.json` records each zip's SHA-256. A hash
  is reused from the previous snapshot when the file's inode, size and
  modification time are unchanged, so only new files are read. Restore
  checks every hash first and refuses a changed snapshot, naming the
  file.

## Tasks

Base for wave 1: 969201e. Later waves are cut from the epic head after the
previous wave lands. No two tasks in a wave touch the same file.

### Wave 1

| Task | Owner, model | Files | Done means |
|---|---|---|---|
| 1a. World core and the list | builder, Opus | `internal/world/` (`world.go`, `meta.go`, `fsutil.go`, `lock.go`, `list.go`, `recover.go` and tests); `internal/datadir/datadir.go`; `internal/store/migrations/0002_active_worlds.sql`; `internal/store/worlds.go`; `cmd/foreman/main.go`; `internal/web/` (`server.go`, `render.go`, `middleware.go`, `upload.go`, `worlds.go`, stub `saves.go` and `snapshots.go`); templates `layout.html`, `worlds.html` (replaces `home.html`); `e2e/tests/foreman.ts`, `e2e/tests/worlds.spec.ts`; `Makefile` (`run` target, R7) | The list (5.4) shows worlds found on disk, the active one marked. `/` goes to `/worlds`. `render.go` loads every template without a hard-coded list. Upload routes get their own body limit and deadline (R8, R13). The upload helper streams and checks zips (R17). Unit tests: id rules (R2), `world.json` round trip with unknown fields and a newer `format_version` (R3), recovery of leftover temp folders. `fsutil.go` provides the free-space
check (R19) and the upload helper uses it. End-to-end: list, empty state, axe, 320 px, both themes. |
| 1b. Styles for the epic's components, and grey hover | ui-designer, Sonnet | `internal/web/static/foreman.css`; `docs/DESIGN.md` (R18 lines only) | DESIGN 7.4, 7.6, 8.2, 8.3 components and the "Active" badge exist, written from DESIGN.md's HTML examples. R18 done, with contrast checked in both themes. |

### Wave 2

| Task | Owner, model | Files | Done means |
|---|---|---|---|
| 2a. Import and rename | builder, Opus | `internal/world/import.go`, `rename.go` and tests; `internal/web/worlds.go`; templates `world.html`, `import.html`; `e2e/tests/worlds.spec.ts` | Import from the inbox (R5) and by save upload (5.5, 5.8), by copying, with the free-space check (R19). End-to-end: import, rename, duplicate name refused (R12), source folder unchanged. |
| 2b. Saves | builder, Sonnet | `internal/world/saves.go` and tests; `internal/web/saves.go`; template `saves.html`; `e2e/tests/saves.spec.ts` | List, download, upload and choose the save that loads (5.11); the newest-save function (R16). End-to-end: upload, download, bad zip refused, oversized upload refused, duplicate name refused (R10). |
| 2c. Snapshots and restore | builder, Opus | `internal/world/snapshot.go`, `restore.go`, `recover.go` and tests; `internal/web/snapshots.go`; templates `snapshots.html`, `restore.html`; `e2e/tests/snapshots.spec.ts` | Section 6 except 6.10: manual and automatic snapshots, hardlinks with a copy fallback and a note when copying, times restored exactly (6.9), retention (R13), disk use (R11), restore by swap (R9), the free-space check before a snapshot and free
space shown on the page (R19). Unit tests prove restored contents and times match. |

### Wave 3

| Task | Owner, model | Files | Done means |
|---|---|---|---|
| 3a. Delete, and snapshots of deleted worlds | builder, Sonnet | `internal/world/delete.go` and tests; `internal/web/worlds.go`; templates `delete.html`, `deleted.html`; `e2e/tests/delete.spec.ts` | Delete per 5.10 and DESIGN 7.4: final snapshot on by default, name typed exactly, active world refused. A deleted world's snapshots stay listed; restoring one recreates the world. |
| 3b. Security and failure tests | tester, Opus | `internal/world/security_test.go`, `failure_test.go`; `internal/web/upload_security_test.go` | Tests for `..` and absolute paths, planted symlinks in a world and the inbox, hostile zip names and sizes, a snapshot refused for lack of space, disk
full mid-snapshot and mid-upload, a crash mid-restore, and two admins at once. Reports bugs; fixes none. |
| 3c. UI polish | ui-designer, Sonnet | `internal/web/static/foreman.css`; every template except `delete.html` and `deleted.html` | Every new page passes DESIGN 10. |

### Wave 4

| Task | Owner, model | Files | Done means |
|---|---|---|---|
| 4a. Fold | builder, Sonnet | `docs/STATUS.md`, `docs/SPEC.md`, `docs/adr/0002-world-and-snapshot-formats.md`, `docs/STACK.md` and `docs/WORKFLOW.md` if `make run` changed, this plan (deleted) | Epic 2's STATUS section from the task PR bodies; R1 to R5 and R9 folded into SPEC 3.4, 5.1, 5.2, 5.5 and section 6; drop "the MIT license" from SPEC 18.10; ADR 0002 records R1 to R5. SPEC gains a P0 rule for R19 in section 6 and a P1 low-disk warning on the status page and in notifications (section 8), to be built in Epic 8. |

## Checks

- After each wave: `make verify` and `make e2e` on the epic head.
- Reviews after wave 3: code-reviewer, security-reviewer (file paths,
  uploads) and a11y-reviewer (new screens), on a read-only worktree.
- Real-game check before the epic PR (`make test-game` is still a stub):
  import a real save folder, take and restore a snapshot, then start a
  `factoriotools/factorio` container by hand on that world and confirm it
  reaches "InGame" with the expected save.

## Left out on purpose

Mod sync (9.8, Epic 5); reading a save file (9.9) and snapshotting a
running world (6.10), both Epic 3; a low-disk warning while the game
runs (P1, added to SPEC section 8 by the fold, built in Epic 8); archive (5.9); audit log entries
(12.9, Epic 10); a settings table; deleting a single save.
