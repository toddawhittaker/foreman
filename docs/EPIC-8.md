# Epic 8: Mods

Serves SPEC 18.8 (section 9, all of 9.1 to 9.12) and issue #131. Branch
`epic/8-mods`, cut from main `d32299e`. This plan lives only on the epic
branch; the fold task moves its lasting rules into SPEC.md and ADRs and
deletes it (WORKFLOW.md, "Epic plans").

Out of scope: #132 (public mod download page, its own epic next) and
#206 (public server listing). Editing mod setting values (9.6, P2).

## Rulings

Todd, 2026-11-07:

1. **A mod change on a running world is one operation**, shaped like a
   game version update (10.3): download first, then warn and stop,
   snapshot, apply, pre-start check, start. A failed download or checksum
   leaves the game running and untouched.
2. **The #131 warning shows on the Mods tab and on the Server page.** The
   Server page loads a one-line notice after the page opens, with a Fix
   link. Portal replies are cached 15 minutes.
3. **"Nearest release" (9.10)** is the lowest release above the missing
   version on the world's game line; if none, the newest below it, with a
   warning.
4. **Browser tests fake the portal with a test-only HTTPS proxy** in the
   fake game binary, using a test certificate authority, `HTTPS_PROXY`
   and `SSL_CERT_FILE`. The product gets no portal URL setting.

Design rulings (architect, accepted in this plan):

5. **Search** downloads the portal's mod list for the world's game line
   (`/api/mods?page_size=max&version=<line>`, no credentials) and
   searches it in memory: exact name, then prefix, then substring in name
   or title, ordered by downloads, top 25. The portal's own search needs
   credentials, which 9.11 forbids for search.
6. **The resolver is greedy**, with no backtracking. For each required
   dependency it keeps the installed version if it satisfies every
   constraint, else takes the newest release on the world's line that
   does. Built-in mods count as present at the world's game version.
7. **No plan is stored on the server.** The plan page posts back the
   request, the plan hash and a fingerprint of the mods folder. The server
   recomputes; if either differs, it applies nothing and shows the new plan
   with "the plan changed" (17.8).
8. **Every mod change goes through one path**: install, update, update
   all, sync from save, the #131 fix, enable, disable, remove, mod upload
   (9.5) and `mod-settings.dat` upload (9.6). Mod upload (P1) is in this
   epic because 9.10's default choice (P0) needs it.
9. **Sync disables extras, never removes them.** Mods installed as folders
   are shown as "local" and never replaced.
10. **Credentials** come from `FOREMAN_FACTORIO_USERNAME` and
    `FOREMAN_FACTORIO_TOKEN` (or `_FILE`) first. Otherwise the admin enters
    them at `/settings/factorio-account`, sealed in a keyed
    `stored_secrets` table (row `factorio-account`) with AES-GCM, key from
    HKDF of `FOREMAN_SECRET_KEY`, a version byte, and the row name as
    authenticated data. Neither the username nor the token is ever shown
    back. Setting only one of the two variables, or a key shorter than 32
    characters, is a startup error. An unreadable sealed row (key changed)
    asks for the account again; it never crashes. The token is not
    checked on save.
11. **Portal limits** (amend SPEC 14.10): API calls 10 s, no redirects,
    16 MB for the list, 2 MB for one `/full` reply; downloads 200 MB and 5
    minutes per file, at most 2 redirects, only to `https` hosts ending in
    `.factorio.com`; at most 4 `/full` requests at once. Foreman only
    follows `download_url` values starting with `/download/` on the fixed
    base. `file_name` must equal `<name>_<version>.zip`. Errors never carry
    the query string (the token rides in it).
12. **Cache** is in memory only, 15 minutes, never keeps an error. Nothing
    is fetched at startup or on a timer.

## Contracts

Wave 1 fixes these in its first commits; later waves build against them.
Changing one needs the orchestrator's ruling.

```go
// internal/modportal
type Release struct {
    Version, FileName, DownloadURL, SHA1, FactorioVersion string
    Dependencies []string // raw strings from info_json
}
type Mod struct {
    Name, Title, Summary string
    Downloads  int
    Deprecated bool
    Releases   []Release // oldest first
}
type Hit struct { Name, Title, Summary, LatestVersion, FactorioVersion string; Downloads int }
var ErrNotOnPortal, ErrUnreachable, ErrChecksum, ErrUnauthorized error

// internal/factorioaccount
type Account struct { Username, Token secret.Secret }
type Source int // None, Env, Stored, Unreadable

// internal/world
type InstalledMod struct {
    Name, Version, File string // File empty for a folder mod
    Enabled, Local, BuiltIn bool
}
type ModChange struct {
    Add      []string        // staged temp file names to place in mods/
    Remove   []string        // zip file names to delete from mods/
    Enabled  map[string]bool // new enabled state, by mod name
    Settings []byte          // new mod-settings.dat, or nil
}

// internal/mods
type Lookup func(ctx context.Context, name string) (modportal.Mod, error)
type Request struct { /* Kind + Names + Optional + SaveMods + Choices */ }
type Plan struct { Changes []Change; Conflicts []string; Unresolved []Unresolved; Optional []Offer; Hash string }

// internal/game
type ModFetcher interface {
    Download(ctx context.Context, r modportal.Release, dst *os.File) error
}
```

## Tasks

Each task may edit only the files listed. Locale areas are owned in all 8
languages by one task.

### Wave 1 (parallel, from `d32299e`)

| Task | Owner, model | Files | Serves |
|---|---|---|---|
| T1 Portal client and Go fake | builder, Sonnet high | `internal/modportal/**` (client, cache, `modportaltest/`, `testdata/` trimmed from real replies) | 9.1, 9.12, 14.10 |
| T2 Dependency parser and resolver | builder, Opus low | `internal/mods/deps.go`, `resolve.go`, their tests, fuzz test | 9.2, 9.7, 9.8, 9.10 |
| T3 Account and sealed secrets | builder, Sonnet high | `internal/config/*`, `internal/secret/box.go`, `internal/store/migrations/0005_stored_secrets.sql`, `internal/store/secrets.go`, `internal/factorioaccount/**` | 9.11, 14.8 |
| T4 World layer | builder, Opus low | `internal/world/mods.go`, `modversion.go`, new `modchange.go`, `modupload.go`, a public `SaveMods` beside `precheck.go`; snapshot reason `mods`, label "Before mod change" | 9.4, 9.5, 9.6, 9.9, 6.x |

T1 confirms the real download redirect host with Todd's token before
fixing the allowed-host rule, and reports it.

### Wave 2 (from the epic head after wave 1)

| Task | Owner, model | Files | Serves |
|---|---|---|---|
| T5 Apply operation | builder, Sonnet high | `internal/game/mods.go` and tests; audit action `mods` | ruling 1, 9.3, 9.4 |
| T6 Planner, updates, availability (#131) | builder, Opus low | `internal/mods/plan.go`, `availability.go` and tests | 9.2, 9.3, 9.8, 9.10, #131 |
| T7 Account page | builder, Sonnet high | `internal/web/account.go`, `account.html`, locale area `account`; first edit of `internal/web/server.go` (Deps field, route) and `cmd/foreman/main.go` | 9.11 |

### Wave 3

| Task | Owner, model | Files | Serves |
|---|---|---|---|
| T8 Mods screens | ui-designer | `internal/web/mods.go`, `modfiles.go`, templates `mods.html`, `modsearch.html`, `modplan.html`; `layout.html` (Mods tab); `server.html` (Mods link and the #131 notice); the pre-start refusal and import result next-step links; locale area `mods` and its keys in `worlds` and `server`; CSS; second edit of `server.go` and `main.go` | 9.1 to 9.10, 17.9, ruling 2 |

### Wave 4

| Task | Owner, model | Files | Serves |
|---|---|---|---|
| T9 Browser tests | tester, Sonnet high | `e2e/tests/mods*.spec.ts`, `click-budgets.spec.ts` (un-skip the undo row, add mod rows), the HTTPS proxy fake portal in `cmd/fakegame`, `e2e/tests/foreman.ts` | ruling 4, 17.10 |
| T10 Real-game test | tester, Sonnet high | `internal/game/realgame*_mods_game_test.go`: a made-up mod applied with a fake fetcher reaches InGame on 1.1, 2.0 and 2.1 with the mod loaded; which zip loads when two versions sit in `mods/`; whether the game rewrites `mod-list.json` at exit | CLAUDE.md real-game rule |
| Fold | builder, Sonnet | STATUS; SPEC 3.2, 6.7, 9, 14.10, 17.10; DESIGN 8.11; ADR 0012 (portal client and search source), ADR 0013 (sealed secrets format); README section on getting the factorio.com token for Gate B; deletes this plan; `Fixes #131` | WORKFLOW "Epic plans" |

### Click budgets (from the Server page)

| Task | Clicks |
|---|---|
| Search and install a mod | 4: Mods, Search, Install, Install N mods |
| Update all | 3: Mods, Update all, Update N mods |
| Fix a #131 flag | 2: Fix (Server page notice), Update |
| Undo a mod change | 2: Undo this mod change, Restore snapshot |

## After the tasks

Reviews on the epic head: code-reviewer, security-reviewer (portal calls,
secrets, uploads, file paths), a11y-reviewer (new screens). Then the fix
round, the real-game check, the fold, and the epic PR. Gate B (SPEC 19.2)
follows the merge.
