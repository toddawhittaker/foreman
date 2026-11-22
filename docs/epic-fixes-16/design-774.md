# Design: export a whole world as one ZIP, import it into another Foreman (#774)

Base commit 5b718314. Fixes batch 16. Read-only design; nothing in the repo was changed.

Section numbers below are the spec's real ones. The brief's numbers were off by one in places:
import is SPEC 5.5, saves 5.11, archive 5.9, delete 5.10, favorites 5.12.

## 1. The design

### 1.1 What a world file is

A **world file** is one ZIP, `<world name>-world.zip`. Unpacked, it is a world folder in the
SPEC 5.1 layout plus one format file:

| Entry | What | ZIP method |
|---|---|---|
| `foreman-world.json` | Format file (below). Always the first entry. | Deflate |
| `world.json` | The world's metadata, with `imported_from` and `archived` removed | Deflate |
| `saves/<save>.zip` | Every save (or only the one that loads next; decision D3). Never a `.tmp.zip`. | Store (already compressed) |
| `mods/...` | Every regular file under `mods/`, at any depth: zips, folder mods, `mod-list.json`, `mod-settings.dat` | Store for `.zip`, Deflate otherwise |
| `config/<file>` | The six `world.ConfigFiles`. `server-settings.json` has its secrets stripped. | Deflate |

`foreman-world.json`:

```json
{"format_version": 1, "exported_at": "2026-11-22T10:00:00Z", "foreman_version": "1.0.0",
 "saves": "all", "game_password_set": true}
```

- `format_version` is the world file's own version, separate from `world.json`'s.
- `game_password_set` says only that a password existed, never the value. The import uses it to
  warn that the imported world will start with no password.
- **Writer rule, stated in the spec now:** the writer uses the lowest format version that can hold
  what it writes. If snapshots are added later (format 2), an export without snapshots stays
  format 1, so older Foremans can still read it. This costs nothing now, and it keeps users' saved
  files readable.
- Each entry's ZIP time is the file's modification time, so "last played" survives the move.

**Why this shape.** Unpacked, a world file is also a valid inbox folder for SPEC 5.5 (`saves/`,
`mods/`, `config/`). Someone without Foreman can still read it, and an old Foreman can import it
by hand. That keeps vision principle 4 ("plain files on disk"). The file set is the same set that
import 5.5 and clone 5.7 copy, plus `world.json`. One rule covers all three.

**This is the hard-to-undo part.** Exported files sit on people's disks for years, so the layout
and the format file are fixed now. Everything else in this design is code inside two modules and
can be refactored later.

### 1.2 What is stripped and never exported

| Item | Where it lives | How it is handled |
|---|---|---|
| RCON password | `config/rconpw` | Never exported. It is not in `world.ConfigFiles`. The new host makes its own at the first start (`RCONPassword` creates it when missing, SPEC 4.5). |
| Game password, factorio.com `username`, `password`, `token` | `config/server-settings.json` | `gamesettings.StripSecrets` on export, **and again on import** (the favorites rule, SPEC 5.12). |
| Portal token, OIDC secret, webhook URLs, API tokens, sessions, `FOREMAN_SECRET_KEY` | App database or environment | Never in a world folder, so never exported. Only the world folder is read. |
| `imported_from` | `world.json` | Dropped. It names a folder on the old host. |
| `archived` | `world.json` | Dropped. An imported world arrives unarchived, as a restored deleted world does (SPEC 6.5). |
| Any file outside the 5.1 set (hidden files, `.tmp.zip`, stray files, leftovers with Foreman's work prefixes) | Anywhere | Not exported. |
| Symlinks and other special files | Anywhere | The export is refused and names the path. This is the snapshot rule (SPEC 6.8), reusing `filesUnder`. |

**What cannot be stripped.** `mod-settings.dat` and the saves' own mod data (`script.dat`) can hold
anything a mod chose to keep, a token included. The export card carries the same kind of Warning
the favorites export has (DESIGN 7.11), above the button and tied to it with `aria-describedby`.

**Database state that stays behind** (said on the card; not exported): the saved mod picks (9.14),
the mod plan drafts, the audit log, the restart schedule, notifications, and which world is active.

### 1.3 Export flow

1. On the world page, an "Export world" card in the `world-share` row sits beside "Copy map
   exchange string" and "Make a favorite". It shows the estimated size (the world's disk use) and
   the warning, then has one button. That is 2 clicks from the Server page: World page, then Export
   world.
2. `POST /worlds/{id}/export` is a form post. A post, not a link, because it does work and writes
   an audit entry, and the same-origin check of 14.7 covers it. It is registered with
   `handleLong`, so the idle deadline lets a slow download keep running.
3. **Consistent copy without holding the lock.** `PrepareExport` takes `Lock(id)` (a try-lock;
   `ErrBusy` is refused at once), checks the world with `readChangeable` (a world a newer Foreman
   wrote is refused), and lists files with `filesUnder`, keeping only the 1.1 set. It then builds
   `<worlds>/.staging-export-<random>/`. Zips are hard-linked and small files copied, through
   `placeFile` (snapshot.go). `server-settings.json` is written stripped into the staging folder.
   Then it unlocks. That takes milliseconds to seconds, so a download that runs for hours never
   blocks a start, stop or mod change.
4. `WriteZip` streams the staging folder straight into the response with `zip.NewWriter(w)`. It
   never holds a file in memory. Go's writer switches to zip64 by itself above 4 GB. The response
   has no `Content-Length`. It uses `Content-Type: application/zip`, `Cache-Control: no-store` and
   `nosniff`, and the file name comes from `attachment(...)`.
5. **A failure partway** panics with `http.ErrAbortHandler`, as `publicmods.go` already does. The
   browser then marks the download as failed instead of saving a truncated file that looks
   complete. A truncated file has no central directory, so an import refuses it anyway.
6. `Close` removes the staging folder. A crash leaves it with the `.staging-` prefix, which
   `Recover` already clears at startup (`leftoverPrefixes`).
7. At most 2 exports run at once, through their own slot channel like `uploadSlots`. A third gets
   429 with the busy page.
8. **A running world** exports the last save on disk, and the card says so, as clone does (5.7).
   See D7.
9. Audit entry `world-export` with the world and "all saves" or "loaded save" (D6).

### 1.4 Import flow (recommended: upload, preview, confirm; see D2)

1. The Import page gets a third form, "Import a world file". It has one drop zone (DESIGN 7.10)
   for a `.zip` and an "Upload world file" button.
2. `POST /import/world-file` is registered with `handleUpload` and a fixed limit of `MaxWorldFile`
   (D4). It is wrapped in `guardUpload`, so it shares the 2 upload slots and the 411 and 429
   refusals. The body streams into `CreateWorldFileTemp()`, a staged file
   `<worlds>/.partial-worldfile-<random>`, through `receiveInto`. Nothing is held in memory.
3. `ReadWorldFile(temp, limits)` reads only the ZIP's directory and its small entries, then shows
   the **preview page** with:
   - the name from `world.json`, editable;
   - the game version and Space Age;
   - how many saves and mod files, the unpacked size, and which save loads;
   - whether this host has the game image, with "Download the game image" ticked by default (5.5);
   - the warning when `game_password_set` is true: "The game password was not in the file. This
     world will start with no password until you set one in Game settings."
   The staged file's random id is in a hidden posted field, never in the address (17.17).
4. `POST /import/world-file/confirm` calls `ImportWorldFile(ctx, temp, name, limits)`, then
   `downloadFor` when ticked, then redirects to `importedPath(id, false)`. The world page then
   shows the "imported" message and the mod sync offer that `importFacts` already gives.
5. **Name clash:** the confirm is refused on the name field and the staged file is kept, so the
   person types another name with no second upload. That keeps the favorites rule: a name in use
   is refused, not silently renamed.
6. **Ids:** the new world always gets a fresh id from `newID(name)` (5.2: never reused). Nothing
   in the file reaches a path except through the entry rules in 1.5.
7. **Staged files** older than an hour are removed whenever a new one is created (the
   `removeOldStagedMods` pattern), and at startup by `Recover`. The `.partial-` prefix is already
   cleared, so `recover.go` does not change. At most 2 staged world files may exist; a third
   upload removes the oldest.
8. Audit entry `world-import` with the new world and the file's `foreman_version` (D6).

### 1.5 Checks on import (zip-slip, zip-bomb, size)

Everything in the file is untrusted (14.5, 14.6). In order:

1. **Size.** The body is at most `MaxWorldFile` (D4). The disk check of 6.11 applies twice: the
   upload's declared length when receiving, then the sum of the unpacked sizes before extracting.
   That is `Reserve(worlds, sum)`, which keeps the 2 GB reserve.
2. **Entry count before the directory is built.** `zipEntriesOver(f, size, 100_000)` (in
   `modversion.go`) refuses a ZIP of millions of tiny entries before `zip.NewReader` builds them.
   100,000 is the file cap the mod settings work folder already uses.
3. **Every entry name** must pass `worldFileEntry`: exactly `foreman-world.json` or `world.json`;
   `saves/<n>` with `ValidSaveName(n)` and no subfolder; `config/<one of ConfigFiles>` (so
   `config/rconpw` is refused); or `mods/<rel>` where `filepath.IsLocal(rel)` holds, with no
   backslash, no empty segment, and no `.` or `..`. Folder entries are allowed only for these
   prefixes. Any other entry, a repeated name, or a non-regular mode (a symlink entry) refuses
   the whole file. Writing then goes only through `os.Root`, which refuses escapes a second time.
4. **Per-entry limits** come from the header and are enforced again while reading, with
   `io.LimitReader(limit+1)`; Go's reader also fails on more bytes than the header states. The
   limits are: `foreman-world.json` 1 KiB; `world.json` 64 KiB; each config file 1 MiB
   (`maxConfigFile`); `mod-settings.dat` `MaxModSettings`; a save, the save upload setting; a mod
   file, the mod upload setting.
5. **Ratio.** The total unpacked size must be at most 50 times the file size (`maxZipRatio`, as
   for save uploads). Saves and mods are stored, not deflated, so a real file is about 1 to 1.
6. **Content.** `foreman-world.json` is read with `strictJSON`, and `format_version` must be 1. A
   newer version gives `ErrWorldFileNewer`, which says a newer Foreman made the file.
   `world.json` must decode as `Meta` and have `format_version` at most `FormatVersion`. Unknown
   fields at the same format version are kept, as 5.1 says. The game version must pass
   `checkNew` (supported, with no Space Age before 2.0). `load_save` must name a save in the file,
   or is dropped. `server-settings.json` must be one JSON object of at most 1 MiB and is stripped
   again. At least one save is needed, unless `world.json` has `new_map`.
7. **Writing.** Extraction runs inside `create()`'s `fill` callback, into the staging folder, at
   `copyMode` modes, with `Chtimes` from each entry's time. Go checks each entry's CRC-32 while
   reading. Any failure removes the staging folder, so nothing changes (create already does this).
   `createOptions` gains `useWhitelist` and the unknown fields, so `world.json` keeps them.
8. Refusals of a bad file use one fixed message that never repeats what the file holds (the
   favorites rule). A refusal the form can fix (name, disk space) keeps the staged file.

### 1.6 How it relates to import 5.5 and archive 5.9

- **Import 5.5** stays as it is. The world file is a third way in, on the same Import page. It
  copies the same file set and follows the same rules for names, game versions, image downloads
  and "copies, never moves". It differs in four ways: it also carries `world.json` (the name,
  which save loads, the whitelist switch, `new_map`), strips secrets, and never marks "already
  imported" (`imported_from` is for inbox folders only).
- **Archive 5.9** stays as it is: hide in place, keep snapshots, one click to undo (D5). An
  archived world can still be exported, since export only reads.
- **Favorites 5.12** stay separate. A favorite is a recipe for new worlds; a world file is the
  world itself. They share helpers (`strictJSON`, `readZipEntry`, `StripSecrets`), not a format.

### 1.7 REST API and MCP

Neither gets it (D8). SPEC 21.7 already rules out "uploading or downloading files", and 23.4
caps a request body at 4 MiB of JSON. The spec only adds "world files" to that list.

### 1.8 Several worlds at once

A world file holds one world. It has no active flag and no port that is assumed to be free. If a
later Foreman keeps a port per world in `world.json`, it travels as a field. A clash on import
then becomes that feature's check, not a change to this format.

### 1.9 New and changed SPEC text

**5.13. Export a world file.** (P1) A world's page offers "Export world", which downloads
`<name>-world.zip`. It holds `foreman-world.json` (`format_version` 1, the export time, Foreman's
version, which saves, and whether a game password was set), `world.json` without `imported_from`
or `archived`, every save (D3) except a `.tmp.zip`, every regular file under `mods/`, and the
5.1 config files except `rconpw`, with every secret removed from `server-settings.json` (14.8).
Nothing else in the world folder and nothing from the app database is in it. Saves and zips are
stored, not compressed again. Each entry keeps its file's modification time. Foreman takes the
world's lock only while it hard-links the files into a staging folder (copying when a link
fails, 6.8). It then streams the ZIP from that folder without the lock and without holding the
file in memory, and removes the folder afterwards. A world that holds a symlink or other special
file is refused, naming the path. A world a newer Foreman wrote has no Export. A running world
exports the last save on disk, and the page says so. A Warning above the button says that saves
and `mod-settings.dat` may hold what a mod stores, a token included, and that the game password
is not exported. At most 2 exports run at once. A failure partway breaks the download, so no
complete-looking partial file is saved. A writer uses the lowest format version that can hold
what it writes. Recorded as `world-export` (12.9).

**5.14. Import a world file.** (P1) The Import page (5.5) takes a world file through its own drop
zone. The upload is staged and checked (14.5, 14.6), and a preview shows the name (editable),
game version, Space Age, saves, mod files, unpacked size, whether the game image is here (with
the download ticked by default, 5.5), and a warning when the export had a game password. Import
world makes a new world with a fresh id (5.2). A name in use is refused on the name field and the
staged file is kept for another try. A staged file is removed after an hour, at startup, or when
a third is staged. The world arrives unarchived, dated now, with `rconpw` made at its first start
and secrets stripped again. A file whose format version is newer than this Foreman reads is
refused, saying so. A file never replaces an existing world. Recorded as `world-import` (12.9).

**5.15. World file snapshots.** (P2) An export may include the world's snapshots as format 2.
Not scheduled. See D1.

**Changes to existing sections:**
- 5.5: add "A world file exported by Foreman is imported on this page too (5.14)."
- 12.9: add `world-export` and `world-import`.
- 14.5: add "world file entries" to the list of user paths.
- 14.6: add the world file limits: at most `MaxWorldFile` (D4); at most 100,000 entries, counted
  from the end record; the per-entry limits in 1.5; unpacked size at most 50 times the file; a
  disk check before extracting.
- 17.10 (optional): add "Export a world (World page, Export world) | 2".
- 21.7: in "Not available through MCP", add "world files (5.13, 5.14)".
- STACK: no change and no new dependency (`archive/zip` is in Go's standard library).

## 2. Task split

### 2.1 Task 0: the contract (serial head, about 30 minutes)

**T0 `wf-contract`**, builder, Sonnet. It lands the following; everything else branches from its
verified head.

- `internal/world/worldfile.go`: constants `WorldFileFormatFile`, `WorldFileFormat = 1`,
  `MaxWorldFile`, `maxWorldFileEntries = 100_000`, `PrefixWorldFile = PrefixPartial +
  "worldfile-"`; types `WorldFileDoc`, `ExportSaves` (`"all"`, `"loaded"`), `WorldFileLimits{Save,
  Mod int64}`, `WorldFileInfo`; errors `ErrWorldFileBad`, `ErrWorldFileNewer`; the classifier
  `worldFileEntry(name) (entryKind, bool)`, with its unit test.
- `internal/world/worldfile_export.go`, stub bodies that return a "not built" error. Signatures:
  `(m *Manager) PrepareExport(ctx, id string, saves ExportSaves) (*WorldExport, error)`,
  `(*WorldExport) Name() string`, `Bytes() uint64`, `GamePasswordSet() bool`,
  `WriteZip(w io.Writer, foremanVersion string) error`, `Close() error`.
- `internal/world/worldfile_import.go`, stubs: `(m *Manager) CreateWorldFileTemp() (*os.File,
  string, error)`, `ReadWorldFile(temp string, lim WorldFileLimits) (WorldFileInfo, error)`,
  `ImportWorldFile(ctx, temp, name string, lim WorldFileLimits) (string, error)`. ImportWorldFile
  removes `temp` on success and on a bad file, and keeps it on a refusal the form can fix.
- `internal/web/worldfile.go`: `patternWorldExport = "POST /worlds/{id}/export"`,
  `PathWorldFileImport = "/import/world-file"`, `PathWorldFileConfirm =
  "/import/world-file/confirm"`, `AuditWorldExport = "world-export"`, `AuditWorldImport =
  "world-import"`.
- `internal/web/worldexport.go` and `internal/web/worldimport.go`, with empty route functions
  `worldExportRoutes()` and `worldImportRoutes()`.
- `internal/web/server.go`: the two calls after `s.favoriteRoutes()`. This is T0's only edit to a
  shared file.
- The element ids, written in the plan for the UI and e2e tasks: `act-export-world`,
  `export-saves` (radios `export-saves-all` and `export-saves-loaded` if D3 is (b)),
  `export-warning`, `world-file` (drop zone input), `act-upload-world-file`, `world-file-name`,
  `world-file-download`, `act-import-world-file`, `world-file-password-warning`.

### 2.2 Parallel tasks (all start from T0's head)

| Task | Owner and model | Files it alone owns | Depends on | Day one? |
|---|---|---|---|---|
| **T1 `wf-export-core`** | builder, Sonnet (high) | `internal/world/worldfile_export.go`, `worldfile_export_test.go` | T0 | Yes |
| **T2 `wf-import-core`** | builder, Opus (low); it parses hostile input | `internal/world/worldfile_import.go`, `worldfile_import_test.go`, the `createOptions` edit in `internal/world/world.go` | T0 | Yes |
| **T3 `wf-export-web`** | builder, Sonnet (high) | `internal/web/worldexport.go`, `worldexport_test.go`, the `WorldPage.ExportPath()` method (defined in worldexport.go, so `worlds.go` is untouched), locale area `worldexport.json` in 8 languages plus `translated/worldexport.json`, the audit labels for both actions in `internal/web/auditlog.go`, `locales/*/audit.json` and `translated/audit.json` | T0; its tests finish on T1's head | Yes (handler, slots, abort); green after T1 |
| **T4 `wf-import-web`** | builder, Sonnet (high) | `internal/web/worldimport.go`, `worldimport_test.go`, `internal/web/templates/worldfile.html` (preview page), the `ImportPage.WorldFileZone()` and `WorldFilePath()` methods (in worldimport.go, so `import.go` is untouched), locale area `worldimport.json` in 8 languages plus `translated/worldimport.json` | T0; tests finish on T2's head | Yes; green after T2 |
| **T5 `wf-attack-tests`** | tester, Opus (low) | `internal/world/worldfile_security_test.go` (crafted ZIPs: zip-slip, `..`, backslash, absolute paths, symlink entries, repeated names, `config/rconpw`, nested saves, lying headers, a 100,001-entry ZIP, zip64, CRC damage, truncation, a newer format, a newer `world.json`, secrets in server-settings, `load_save` naming a missing save) | T0 contract only | Yes (written test-first; red until T2) |
| **T6 `wf-ui`** | ui-designer, Sonnet | the export card in `templates/world.html`, the third form in `templates/import.html`, the rules it needs in `static/foreman.css`, DESIGN.md new section 7.12 "World files", locale area `worldfile.json` for template text | T0 ids and paths; renders with the stubs | Yes |
| **T7 `wf-e2e`** | tester, Sonnet | `e2e/tests/world-file.spec.ts`: export, unzip in the test, assert no `rconpw` and no secrets; import round trip; name clash keeps the staged file; preview warnings; axe scan at 320 px; click count | T0 ids; runs green after T3, T4, T6 | Yes (written against the ids) |
| **T8 `wf-real-game`** | tester, Sonnet | `internal/web/realgame6_worldfile_game_test.go` (`game` tag): run a world in a real container, export, import into a second data folder, start, reach InGame, compare save bytes, check that `rconpw` is new and that the game password is absent | T1, T2 | Write day one; run after T1 and T2 |
| **T9 `wf-docs`** | builder, Haiku | `docs/OPERATOR.md` new section "Moving a world to another Foreman", plus one line on what is not carried (the database state in 1.2, the game password) | SPEC text in the plan | Yes |

The SPEC text in 1.9 lands in the plan PR (orchestrator). `docs/STATUS.md` is the fold task's,
as always. Reviews (not file owners): security-reviewer on T2, T4 and T1; code-reviewer on each;
a11y-reviewer on T6 and T4's page.

**Critical path:** T0, then T2, then T4's green tests, then T7 green. Everything else runs beside
it. Seven tasks run at once from about minute 30.

**Split across machines** (per the multi-machine note): T1, T2 and T5 are Go-only with fast
checks, good for pve1. T6 and T7 need Playwright, so linux-dev or Todd-Desktop-2. T8 needs the
real image, so the machine that has it cached.

### 2.3 Files other batch 16 features will likely touch (chain these)

| File | Ours | Likely also touched by |
|---|---|---|
| `internal/web/templates/world.html` | T6 (export card) | #769 (player list, if it goes on the world page), #773 (factory map picture) |
| `internal/web/templates/import.html` | T6 | probably none |
| `internal/web/server.go` | T0 (2 lines) | any feature with new routes (#769, #773) |
| `internal/web/static/foreman.css` | T6 | every UI feature in the batch |
| `internal/web/auditlog.go`, `locales/*/audit.json`, `translated/audit.json` | T3 | any feature with a new audit action |
| `internal/world/world.go` (`createOptions`, `finishCommitted`) | T2 | #769, if it seeds "when a world is created" there. Recommend #769 hooks after `create`, so a world-file import seeds too. |
| `internal/web/import.go` and `internal/world/import.go` | none (we avoid them) | #769 (seed after import or upload) |
| `internal/web/render.go` (`partials`) | none (the preview is a page, not a partial) | features adding partials |
| `docs/SPEC.md`, `docs/DESIGN.md`, `docs/OPERATOR.md` | plan PR, T6, T9 | every feature |
| `e2e/tests/click-budgets.spec.ts` and SPEC 17.10 | only if the budget row is added | any feature adding a budget |
| `docs/I18N.md` (list of areas, if it keeps one) | the fold task | other new areas |

New locale areas (`worldexport`, `worldimport`, `worldfile`) are new files, so they never clash.
Each owner runs `make i18n-hash`, which writes only its own `translated/<area>.json`. Check that
it rewrites the other files byte for byte; if it does not, chain the hash runs.

## 3. Decisions for Todd

The recommendation is listed first in each.

**D1. Snapshots in the export.**
- (a) **Not now.** Format 1 has no snapshots; a later format 2 adds them (5.15, P2), and the
  writer rule keeps format-1 files readable. Gains: small and safe. Costs: a moved world starts
  with no undo history on the new host.
- (b) An optional "Include snapshots" checkbox now. Gains: the full history moves. Costs: every
  snapshot holds `rconpw` and an unstripped `server-settings.json`. Stripping them changes sizes
  that the snapshot's manifest records, so each manifest must be rewritten, and restore's checks
  must accept that. Hard links do not survive a ZIP, so 10 snapshots of a 160 MB save can make
  the file 10 times bigger, unless the format stores each file once by hash. That would be a new,
  hard-to-undo format design. Roughly doubles the work.
- (c) Always included. All of (b)'s costs, and no way to make a small file.
- **Why (a):** snapshots are cheap only on one filesystem. Moving them needs a deduplicated
  format, and that deserves its own design.

**D2. Import steps.**
- (a) **Upload, preview, confirm** (1.4). Gains: the person sees what is inside before gigabytes
  are written; a name clash costs no second upload; the game password warning has a place.
  Costs: one more page and one more click (3 clicks after the Import page); a staged file with
  hourly cleanup (an existing pattern).
- (b) One step: a name field (empty means the file's name) and the file, then Import. Gains: 2
  clicks, no staging. Costs: a name clash or a missing image means uploading gigabytes again; no
  look inside first.
- (c) One step, and a clash renames the world automatically ("Name (2)"). Gains: never re-upload.
  Costs: breaks the favorites rule (refuse, don't rename), and the person may not notice.
- **Why (a):** world imports are rare and large. Seeing what is inside first is worth one click.

**D3. Which saves an export holds.**
- (a) **A choice on the card, "All saves" (default) or "Only the save that loads next".** Gains:
  "never lose a save" by default, and a small file when wanted. Costs: one radio group; still 2
  clicks with the default.
- (b) Always all saves. Gains: the simplest card. Costs: autosave slots can make the file much
  larger than needed.
- (c) Only the save that loads. Gains: the smallest file. Costs: older saves are silently left
  behind.

**D4. Import size limit.**
- (a) **A fixed cap (64 GiB) plus the existing disk check** (6.11). Gains: no new setting, and
  the real guard (free space) already exists. It is easy to make a setting later. Costs: the cap
  is not adjustable.
- (b) A new General setting, "World file upload limit". Gains: it matches how saves and mods are
  limited. Costs: it touches the General tab, `appsettings.go`, and the MCP `foreman_settings`
  and `set_foreman_settings` tools (21.7 reports every General setting), so it adds files shared
  with other tasks.
- (c) Reuse the save upload limit. Gains: nothing new. Costs: the 1 GB default refuses ordinary
  worlds with a few autosaves.

**D5. Archive and export.**
- (a) **Keep them separate, unchanged.** Gains: archive stays undoable in one click and keeps
  snapshots. Costs: freeing disk still means export, then delete, by hand.
- (b) Same as (a), plus an "Export first" link on the Delete confirm page, beside the final
  snapshot offer. Gains: a natural moment to keep an offline copy. Costs: a small edit to
  `delete.html`; it could be its own issue.
- (c) Archive becomes "export, then delete". Gains: frees disk. Costs: archive becomes
  destructive and needs a snapshot, the type-the-name confirm and MCP two-step; it loses the
  snapshots and its one-click undo. It breaks 5.9 as built.

**D6. Audit.**
- (a) **Record `world-export` and `world-import`.** Gains: the log shows who took a full copy of
  a world off the host. Costs: two labels in 8 languages. Note that folder import and save upload
  are not audited today (12.9 lists generate and clone only). That is a separate gap; file it as
  an issue.
- (b) Record neither. Gains: matches favorites and folder import. Costs: a whole world can leave
  with no trace.

**D7. Exporting a running world.**
- (a) **Last save on disk, and the card says so** (as clone, 5.7). Gains: no game control in the
  export, and nothing to wait for. Costs: up to one autosave interval of play is missing unless
  the admin stops first.
- (b) Send `/server-save` first and wait, as a snapshot does (6.10). Gains: the current state.
  Costs: the export depends on the game controller, its waits and its failures; more code and
  more tests.

**D8. REST API and MCP.**
- (a) **Neither.** Gains: it follows 21.7 ("no uploading or downloading files") and 23.4 (4 MiB
  JSON bodies). Costs: no scripted host moves.
- (b) An MCP tool that writes the export into a server folder (for example `<data>/exports/`),
  and an import from the inbox. Gains: scriptable. Costs: a new folder in the data layout
  (hard to undo), disk use nobody watches, and an AI client that can be steered could copy whole
  worlds.
- (c) A binary REST endpoint outside the tools model. Costs: it breaks 23.2's "same tools, same
  code" rule.

**D9. Replacing an existing world from a file.**
- (a) **New world only.** Gains: never overwrites, and needs no snapshot. Costs: to replace,
  import, then delete the old world.
- (b) Also "Replace this world from a file", with a snapshot first and restore's swap (6.5).
  Gains: an in-place restore from an offline copy. Costs: a new risky operation with the
  snapshot, confirm page, MCP two-step question, and archive and active-world rules. It is
  restore by another name; make it P2 if wanted.

## 4. Risks

1. **The format is a promise.** Users' exported files outlive this version. The layout, the
   format file and the writer rule must be right in this batch. Have security-reviewer read the
   format itself, not only the code.
2. **Hostile ZIP parsing** is the largest attack surface added since uploads. Defences are layered
   (entry count, name rules, `os.Root`, per-entry limits, ratio, disk check, CRC). T5 must land
   before T2 merges, and Go's `zip.NewReader` must never run before `zipEntriesOver`.
3. **A world that had a game password becomes open** on the new host until one is set. This is
   mitigated by `game_password_set` and the warnings. Consider refusing a start of an imported
   world with a public listing and no password. That is not designed here; ask if it is wanted.
4. **`mod-settings.dat` and saves can carry mod-held tokens.** This cannot be fixed; the Warning
   is the mitigation, as for favorites.
5. **Disk peaks at twice the file size** during import (the staged ZIP plus the extracted world).
   The disk check counts both, so large imports on a full disk are refused, not half-done.
6. **Staging hard links during a long export** give the world's zips one extra name. "What
   deleting frees" (6.6) for a snapshot can read lower until the export ends. This is cosmetic
   and lasts only as long as the download.
7. **Long downloads and proxies.** A reverse proxy with a short read timeout can cut a
   multi-gigabyte stream. There is no resume (no `Content-Length`, no ranges). The operator guide
   (T9) should say so. Resume would need the ZIP built to a temp file first, which doubles the
   disk use; left out.
8. **ZIP time precision** is seconds, so save times lose their nanoseconds. "Last played" and the
   save order are unaffected. Snapshot hash reuse (6.7) is per host and does not apply.
9. **Clash with #769 seeding.** If #769 seeds "on import" in `web/import.go`, world-file imports
   are missed. Agree that it hooks after `world.create`, or in T4's confirm handler.
10. **Abort semantics.** `http.ErrAbortHandler` must pass through the recovery middleware
    (`middleware.go:63` already lets it through). T3 needs a test that a failure partway closes
    the connection with no complete ZIP.

## 5. Left out, and why

- Snapshots in the file (D1), replacing a world (D9), API and MCP access (D8): see the decisions.
- Resumable downloads and a `Content-Length`: they need a temp ZIP on disk first; not asked for.
- SHA-256 per file in the manifest: ZIP's CRC-32 already catches damage in transit, and hashing
  gigabytes twice costs time. Snapshots keep their own hashes on each host.
- Signing or encrypting the file: no key exchange between Foremans exists, and nothing asks for it.
- Exporting a deleted world's final snapshot: restore it first, then export.
- Carrying database state (mod picks, schedule, webhooks): not world data. Picks rebuild from the
  Mods tab.
- Scheduled or automatic exports: a vision non-goal ("scheduled backups to external storage").
