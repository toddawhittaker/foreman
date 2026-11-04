# Epic 5 plan: game versions

Serves SPEC 18.5: section 10 (10.1 to 10.5, 10.7, 10.8). VPN sidecars
(4.13) and pinning by exact tag (ADR 0005) shipped in Epic 4. Branch
`epic/5-game-versions`, cut from main 77c9533. Gate A (SPEC 19.1) follows.
This file is deleted by the fold.

## The gap this epic closes first

A world runs the newest pinned image of its line, not its own version:
`cmd/foreman/main.go` maps `1.1`, `2.0`, `2.1` to three images and
`internal/game/container.go` `imageFor` picks by major.minor. A world made
at 2.0.72 runs 2.0.77. Gate A needs each world on its own version, so
task A fixes this. docs/STATUS.md wrongly says 10.1 is delivered; the
fold corrects it.

## Rulings (Todd, 2026-11-04)

1. Changelog: the notice links to the wiki's
   `wiki.factorio.com/Version_history/<major>.<minor>.0` page. Showing it
   inside Foreman is filed as an issue. SPEC 10.2 changes "shows" to
   "links to".
2. Stable and experimental come from factorio.com
   `/api/latest-releases` (`stable.headless`, `experimental.headless`);
   which images exist comes from the Docker Hub tag list. A release with
   no image yet shows "No image yet" and Update is turned off.
3. Only the active world can be updated; another world says "Switch to
   this world to update it". Updating any world is filed as an issue.
4. Updating a stopped world leaves it stopped, as switching does (7.9).
   SPEC 10.3 is amended to "restart if it was running".
5. Rollback goes one step back: it restores the newest "Before update"
   snapshot whose version is older than the world's current one. Pruning
   never removes a world's newest `game-update` snapshot.
6. The Docker Hub tag list and latest releases are cached in memory for
   5 minutes, fetched only when a page needs them. Errors are not cached.
7. Real-game coverage: a cross-line update (2.0.77 to 2.1.24) and a
   same-line update (2.1.23 to 2.1.24), each with rollback. 2.1.23 is
   pinned as
   `factoriotools/factorio:2.1.23@sha256:1e1773337f5be1d93f61d1d64757f18c19c8eb345c96acc57e0d3567a4fd4c3f`.

Settled by the architect: a typed version must be an exact patch release
of a supported line, and its existence is not checked against Docker Hub;
the notice is on the world page only; Update downloads a missing image
before the stop; Rollback's undo is the normal "Before restore" snapshot;
the picker lists newest first and leaves out rootless, `stable-` and alias
tags; Docker Hub's `next` URL is never followed.

## Contracts (code against these; do not change them without the orchestrator)

**`internal/releases` (new package)**

```go
func New() *Client // fixed URLs: hub.docker.com/v2/repositories/factoriotools/factorio/tags, factorio.com/api/latest-releases
type Latest struct{ Stable, Experimental string } // the "headless" values
func (c *Client) Versions(ctx context.Context) ([]string, error) // newest first, deduplicated, ^\d+\.\d+\.\d+$ and world.SupportedGameVersion
func (c *Client) Latest(ctx context.Context) (Latest, error)
var ErrUnreachable = errors.New("...") // wraps every network, status, size and parse failure
func newClient(hubURL, latestURL string, hc *http.Client, now func() time.Time) *Client // for tests
```

One query per supported line (`name=<line>.`, `page_size=100`), page
numbers built by Foreman, at most 10 pages per line. No redirects;
`http.ProxyFromEnvironment`; 10-second deadline per call; 2 MB per tag
page and 64 KB for latest releases; 5-minute in-memory cache; one fetch
per method at a time. It keeps its own small version sort and must not
import `internal/world` beyond `world.SupportedGameVersion`.

**`internal/world`**

- `version.go`: `ParseGameVersion(v string) (savefile.Version, bool)`,
  `CompareGameVersions(a, b string) int`.
- `update.go`: `ReasonGameUpdate = "game-update"`,
  `LabelBeforeUpdate = "Before update"`; `ErrVersionNotNewer`,
  `ErrVersionUnsupported`, `ErrNoRollback`;
  `CheckUpdateTarget(meta Meta, target string) error`;
  `(m *Manager) SetGameVersionHeld(id, version string) error`;
  `(m *Manager) RollbackTarget(id string) (Snapshot, string, error)`
  (newest `game-update` snapshot whose `world.json` version is older than
  the current one; the string is that version).
- `precheck.go`: `(m *Manager) PreStartCheckAt(id, version string)
  (PreCheck, error)`; `PreStartCheck` calls it with the world's version.
- `snapshot.go`: `prune` keeps a world's newest `game-update` snapshot.

**`internal/game`**

- `imageFor(v string) (string, error)` returns
  `"factoriotools/factorio:"+v` for a supported version. New exported
  `ImageRef(version string) string`. `Config.Images`,
  `checkSaveAgainstImage` and `ErrSaveNewerThanImage` are removed (the
  pre-start check covers a newer save).
- `controller.go` (task A writes these): `AuditUpdate = "game-update"`,
  `AuditUpdateRefused = "game-update-refused"`, `AuditRollback =
  "rollback"`, and the `refusedActions` entry mapping `AuditUpdate` to
  `AuditUpdateRefused`.
- `update.go` (task D):
  `(c *Controller) Update(ctx, id, version string, by Actor, stop StopOptions, opts StartOptions) error`;
  `(c *Controller) Rollback(ctx, id string, by Actor, stop StopOptions) (undo world.Snapshot, err error)`;
  `StepSnapshotUpdate = "Take the snapshot before the update"`,
  `StepSetVersion = "Set the game version"`.
- Update order, under the world's lock: (1) everything that can refuse:
  `CheckUpdateTarget`, the world is the active one, `PreStartCheckAt(target)`
  read-only, the game network and sidecar checks; a problem is a
  `*PrecheckError` so the refusal box shows it; (2) download the image if
  Docker lacks it, as a visible step; (3) graceful stop; (4) the "Before
  update" snapshot, which also serves as the start-anyway snapshot;
  (5) `SetGameVersionHeld`; (6) `startLocked` only if it was running.
- Rollback: `RollbackTarget`, then the `restoreLocked` path, audited as
  `rollback`. A missing old image refuses the start (3.7, 10.4); nothing
  is lost.

**`internal/web`**

- Routes: `PathGameUpdate = "/worlds/{id}/update"` (GET, POST),
  `PathGameRollback = "/worlds/{id}/rollback"` (GET, POST),
  `PathWorldReleases = "/worlds/{id}/releases"` (notice fragment),
  `PathVersions = "/versions"` with `?above=<v>` (picker fragment).
- Templates: `update.html`, `rollback.html`, `versions.html` (defines
  `version-list` and `release-notice`). Form field `game_version`. Ids:
  `#act-update`, `#act-rollback`, `#release-notice`, input
  `#<prefix>-version`.
- `server.go`: `Deps.Releases` of type
  `interface{ Versions(context.Context) ([]string, error); Latest(context.Context) (releases.Latest, error) }`;
  nil means no outbound calls.
- The picker is a typed input with a `<datalist>` filled by the fragment
  (`hx-trigger="load"`). The page never waits on Docker Hub; typing works
  without the list and without JavaScript.
- Page structs (task UI writes them in new `web/update_pages.go`; task W
  fills them):

  ```go
  type VersionPicker struct{ Above string; Versions []string; Stable, Experimental string; Unreachable bool }
  type ReleaseNotice struct{ Current, Stable, Experimental, UpdatePath, ChangelogURL string; Unreachable bool }
  type UpdatePage struct{ World world.Summary; Target, VersionError string; Running bool; CountdownForm; Refusal string }
  type RollbackPage struct{ World world.Summary; Snapshot world.Snapshot; Version string; Running bool; CountdownForm; Error string }
  // WorldPage gains: Rollback *RollbackOffer; ReleasesPath string
  ```

No migration. `world.json` is unchanged.

## Tasks

Wave 1 runs seven tasks in parallel from 77c9533. Wave 2 runs four in
parallel from the epic head after wave 1 lands. Each file has one owner
per wave.

| # | Task | Owner, model | Files | SPEC |
|---|---|---|---|---|
| A | Image by exact tag; pin 2.1.23 for tests; e2e sets `HTTPS_PROXY=http://127.0.0.1:9`; `test-game`/`e2e-game` check each test tag resolves to its pinned digest | builder, Sonnet high | `internal/game/{container,controller,actions}.go`, `rig_test.go`, `game_integration_test.go` (image wiring only), `start_test.go`; `cmd/foreman/main.go`, `main_test.go`; `cmd/fakegame/main.go`; `internal/fakegame/*`; web tests naming `ErrSaveNewerThanImage` or `Images` and `web/game.go`; `e2e/tests/foreman.ts` and specs using `image21`; `e2e/game/phone.spec.ts`; Makefile | 10.1, 4.2 |
| B | Import accepts only supported lines; advice never says to lower a version | builder, Sonnet high | `internal/world/import.go`, `modversion.go` and their tests | 10.3, 10.7, 10.8 |
| C | Releases client | builder, Sonnet high | `internal/releases/*.go` (no tests) | 10.2, 10.7, 14.10 |
| CT | Releases tests | tester, Sonnet high | `internal/releases/*_test.go`, `internal/releases/testdata/` | 10.2, 10.7, 14.10 |
| D | World and game update primitives | builder, Opus low | `internal/world/{version,update,precheck,snapshot}.go`, `internal/game/update.go` | 10.3 to 10.5 |
| DT | Update invariant tests | tester, Opus low | `internal/world/update_test.go`, `version_test.go`, `internal/game/update_test.go` | 10.3 to 10.5, 6.2 |
| UI | Pages and fragments | ui-designer, Opus low | `templates/{update,rollback,versions}.html` (new), `world.html`, `import.html`, `static/foreman.css`, `static/import.js`, `web/update_pages.go` (new), the `WorldPage` struct in `web/worlds.go` only, template samples in `render_test.go` | 10.2, 10.7, 17 |
| W | Handlers and wiring | builder, Opus low | `web/update.go` (new), `worlds.go`, `serverpage.go`, `server.html` ("Update anyway"), `snapshots.go` (`reasonText`), `server.go` (`Deps.Releases`), `cmd/foreman/main.go` | 10.2 to 10.4, 10.7 |
| WT | Web tests | tester, Sonnet high | `web/update_test.go`, `web/versions_test.go` | same |
| GT | Real game: 2.0.77 to 2.1.24 and 2.1.23 to 2.1.24, each reaching "InGame", then rollback with the pre-update save byte-identical | tester, Sonnet high | `internal/game/update_game_test.go` (`//go:build game`) | 10.3, 10.4 |
| ET | End-to-end: update by typed version, "Docker Hub cannot be reached", "Update anyway" from the refusal box, rollback, axe at 320 px in both themes | tester, Sonnet high | `e2e/tests/versions.spec.ts` | 10.3, 10.4, 10.7, 17.1, 17.2 |
| F | Fold | builder, Sonnet high | `docs/STATUS.md` (Epic 5, and correct the Epic 4 claim), SPEC 10.2, 10.3, 14.10, STACK 13, this file deleted | — |

Wave 1: A, B, C, CT, D, DT, UI. Wave 2: W, WT, GT, ET. Then reviews
(code, security, a11y), fixes, the fold.

Shared files: task A alone edits `controller.go`; task D needs only the
constants above and, if A has not landed, adds those exact lines so they
apply cleanly with `--3way`. Land A first. `cmd/foreman/main.go` is A's in
wave 1 and W's in wave 2. `web/worlds.go`: UI changes the struct only, W
fills it. `rig_test.go` and `e2e/tests/foreman.ts` are A's; others only use
them. CT and DT write tests against the contracts before the code exists;
expect one reconcile pass when C and D land.

## Risks

- After a 2.0 to 2.1 update, `mod-list.json` lacks 2.1's built-in mods
  (it is written only when missing, `internal/world/mods.go`). GT shows
  whether the game copes; if not, D rewrites the built-in entries at
  update (9.7).
- Rollback loses play since the update; the rollback page says so (17.5).
- End-to-end tests never see a filled picker, since Docker Hub is blocked
  there; Go tests and one manual check at the fold cover it.

## Issues to file

- Show the changelog inside Foreman (ruling 1).
- Update a world that is not active (ruling 3).
