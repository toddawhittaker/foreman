# Import, snapshots and polish fix batch

The second of two fix batches before i18n (#87). It finishes the Import
page, the snapshot pages, sign-in and setup, and local times, and makes
every end-to-end test fail on a Foreman error log line. Branch
`epic/polish-fixes`, cut from main d6e089f. No design step (fix batch);
the architect grouped the issues during batch 1's planning.

## Rulings

Todd, 2026-11-05:

1. Bulk import (#124): with one folder ticked, the name, version and
   Space Age fields apply. With several ticked, each folder uses its
   folder name, its save's version and its save's Space Age setting.
   Folders whose save cannot be read are skipped, each with its reason.
2. Where a world was imported from (#127) is a new world.json object,
   `"imported_from": {folder, save, save_size, save_mtime, at}`. A
   folder counts as already imported when its folder name, newest save
   name, size and modification time (to the nanosecond) all match.
3. The snapshot total (#150, #135) becomes "Snapshots add X": the space
   held by files no world also holds, which is what deleting them would
   free. SPEC 6.6 and 6.12 are reworded at the fold.
4. A version typed by hand on import is refused when it is neither a
   downloaded image nor on Docker Hub's version list. When Docker Hub
   cannot be reached and the image is not downloaded, the import goes
   ahead with a warning that the version could not be checked.
5. Every regular end-to-end test fails when Foreman writes an ERROR log
   line. A test that causes an error on purpose must declare the line it
   expects.

Orchestrator rulings:

- One counting pass over snapshot files (by inode) gives the selection's
  "frees", each world's column and the total, so the three always agree.
- The return path after sign-in (#154) accepts only a local path
  starting with a single `/`; anything else goes to `/`. SPEC 14 records
  it at the fold.
- The import download (#130) reuses the image pull Foreman already makes.
  No new Docker API call.
- Wave-1 tasks do not edit `e2e/tests/foreman.ts`; they keep helpers in
  their own spec files. U5 owns it in wave 2.
- Only U2 edits `foreman.css` in wave 1. U1 and U3 use existing classes,
  and list any style they still need in their PR body for U4 to add.

## Tasks

| Task | Owner, model | Files it may touch | Closes |
|------|--------------|--------------------|--------|
| U1. Import | builder, Opus | `internal/world/import.go`, `meta.go` and tests; new `internal/web/import.go` (the import handlers moved out of `worlds.go`; `worlds.go` only loses them); `import.html`, `import.js`, `world.html` ("Imported from" line, download status); `internal/game/actions.go` only if the download must be tracked per image; new `e2e/tests/import.spec.ts`; existing `import-layout.spec.ts` | #124, #127, #130, #139, #148, #155 (import points), #154 (import Done message), ruling 4 |
| U2. Snapshots | builder, Sonnet high | `internal/world/snapshot_delete.go` and tests; `internal/web/snapshots.go`, `snapshots_all.go`; `snapshots.html`, `allsnapshots.html`, `snapshotdelete.html`, a new bulk confirmation template if needed; `foreman.css`; `snapshots.spec.ts`, `snapshots-tab.spec.ts`, new snapshot spec | #134, #150, #151 |
| U3. Sign-in, setup and times | builder, Sonnet high | `internal/web/identity.go`, `accounts.go`, `middleware.go`, `render.go` (`SetupPage` only); `signin.html`, `setup.html`, `layout.html` (script tag); new `static/time.js`; `accounts.spec.ts`, new e2e spec | #154 (sign-in return, setup errors), #149 |
| U4. Snapshot column (wave 2) | builder, Sonnet high | `internal/web/worlds.go` (`handleWorlds`), `render.go` (`WorldsPage`), `worlds.html`, `foreman.css`; `worlds.spec.ts` | #135, plus styles listed by U1 and U3 |
| U5. Error log check (wave 2) | builder, Sonnet high | `e2e/tests/foreman.ts`, `e2e/playwright.config.ts`, and any existing spec only to declare an expected error line | ruling 5 |
| F. Fold (last) | builder, Sonnet high | `docs/SPEC.md` (5.1, 5.4, 5.5, 6.6, 6.12, 14), `docs/DESIGN.md` 9 (times), `docs/STATUS.md` (this batch), delete this plan | `Fixes #N` for each issue |

## Waves and checks

Wave 1: U1, U2, U3 in parallel. Wave 2: U4 (after U1 moves the import
handlers out of `worlds.go` and U2's counting pass lands) and U5 (after
wave 1, so it sees every new test). Then code-reviewer, security-reviewer
(the sign-in return path and import folder paths) and a11y-reviewer on
the epic head; the real-game check (`make test-game`, `make e2e-game`)
on an import that downloads its image and a bulk snapshot delete; F; and
the batch PR to main.

## Left out

- #131 and #132 need the mods epic (SPEC 9); #132 also needs a vision
  change.
- #87 (i18n), #92 (changelog) and #93 (update a world that is not
  active) are later work.
- Changing a field per folder in a bulk import: import one at a time for
  that.
