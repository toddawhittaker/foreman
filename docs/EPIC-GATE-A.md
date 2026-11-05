# Gate A fix batch

A fix batch before Gate A (SPEC 19.1), from the architect's Gate A
review. Branch `epic/gate-a-fixes`, cut from main 0edcfee. No design step
(fix batch).

## Rulings (Todd, 2026-11-04)

1. Import also copies the folder's `config/` files that SPEC 5.1 lists,
   except `rconpw`: Foreman makes its own (4.5). Other files there
   (`config.ini`, `mapexchangestring.txt`) are not copied.
2. Import takes save zips from the folder's top level and from `saves/`,
   so the Factorio server's own layout (`saves/`, `mods/`, `config/`)
   imports as it is.
3. VPN sidecars (SPEC 4.13) are not built. Until they are, Foreman
   refuses to start when `FOREMAN_GAME_NETWORK_CONTAINER` is set, and the
   docs stop saying they shipped.

Orchestrator rulings: a save name found both at the top level and in
`saves/` refuses the import and names the file (fail safe, nothing is
guessed). A config file that is not valid JSON is still copied as is; the
pre-start check already reports a bad settings file.

## Tasks

| Task | Owner, model | Files | Done when |
|------|--------------|-------|-----------|
| A. Groups header | builder, Sonnet high | `internal/auth/headers.go`, `internal/auth/headers_test.go` | Groups split on `\|` and `,`; tests for Authentik's `a\|b` form, mixed, spaces, empty parts |
| B. Import layout and config | builder, Sonnet high | `internal/world/import.go`, `internal/world/import_test.go`, one new e2e spec under `e2e/tests/` | Rulings 1, 2 and the clash rule, with unit tests; the inbox count includes `saves/`; e2e imports a folder in the server layout; no config content is logged |
| C. Sidecar refusal | builder, Sonnet high | `internal/config/config.go`, `internal/config/config_test.go` | Startup fails with a message naming the setting and saying sidecars are not built yet |
| D. README compose, checked for real (wave 2, after A to C) | builder, Sonnet high | `README.md` | Compose adds `read_only` with a `/tmp` tmpfs, `cap_drop: [ALL]`, `no-new-privileges`, `depends_on: socket-proxy`; shows how the proxy's own compose joins `foreman-front` at the fixed address; status line updated. Brought up from the epic head under a scratch project with its own network names: healthy, a server-layout folder imported, game reaches InGame with the imported game password applied |
| F. Fold (last) | builder, Sonnet | `docs/SPEC.md` (5.5, 12.5 separators, 18: 4.13 moved to Epic 8), `docs/STATUS.md` (correct the sidecar lines, add this batch), `docs/adr/0006-*.md` status to Accepted, delete this plan | `Refs` lines; nothing else edited |

Wave 1: A, B, C in parallel (no shared files). Wave 2: D. Then
code-reviewer and security-reviewer (trusted headers, file paths), the
real-game check (`make test-game`, plus D's run), F, and the batch PR to
main.

Proposed for Todd: SPEC 4.13 moves to Epic 8 (live operations).
