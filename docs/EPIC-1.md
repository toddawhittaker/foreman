# Epic 1: Foundation (plan)

This plan lives only on `epic/1-foundation` and is deleted by the fold
task (T10). Code, tests and other docs cite SPEC.md, STACK.md or ADRs,
never this file.

Scope: SPEC 18.1. Base commit: `7714ec4` on `main`.

## Rulings

Todd settled these on 2026-11-02. T10 folds each lasting rule into the
SPEC section named.

| # | Question | Ruling | Folds into |
|---|---|---|---|
| R1 | Where the toolchain runs | Go 1.27.3 is installed locally and pinned. golangci-lint and Playwright run in pinned images. `make verify` fails if the local Go version differs from the Go image named in the Dockerfile and `go.mod`. CI installs the same Go version and checks its checksum. | STACK 1, 9, 13; ADR |
| R2 | Turning sign-in methods on | `FOREMAN_AUTH_METHODS`, a comma list of `local` and `headers` (default `local`). `oidc` joins it in Epic 10. | SPEC 3.2, 12.6 |
| R3 | Docker address variable | `DOCKER_HOST`, default `unix:///var/run/docker.sock` | SPEC 3.2 |
| R4 | First-run setup page | Open until an admin exists, exactly as SPEC 12.3 says. No setup code. When `local` is not in the methods list there is no setup page; the first header user becomes the admin. | SPEC 12.3 |
| R5 | Secure cookie | Set when `FOREMAN_PUBLIC_URL` starts with `https://`. | SPEC 12.8 |
| R6 | Coverage floor | 70% of statements across `internal/`, checked inside `make verify`. | STACK 13 |
| R7 | Foreman's user differs from the game's | Files are created 0664 and folders 0775. Foreman refuses to start only when neither its user ID nor its group ID matches the game's. | SPEC 3.6 |
| R8 | Session idle default | 7 days (`FOREMAN_SESSION_IDLE`, a Go duration). | SPEC 12.8 |

## Environment variables

T3 loads exactly these. Any variable marked secret also accepts `_FILE`
(SPEC 3.2).

| Variable | Default | Meaning |
|---|---|---|
| `FOREMAN_MODE` | `managed` | `managed` or `adopted` (SPEC 4) |
| `FOREMAN_DATA_DIR` | `/data` | The data directory inside the container (3.3) |
| `FOREMAN_HOST_DATA_DIR` | discovered | The same directory's path on the host (3.5) |
| `FOREMAN_GAME_UID`, `FOREMAN_GAME_GID` | `845` | The game's user and group (3.6) |
| `FOREMAN_LISTEN` | `:8080` | Listen address |
| `FOREMAN_PUBLIC_URL` | none | The URL users reach; `https://` makes cookies Secure (R5) |
| `FOREMAN_AUTH_METHODS` | `local` | `local`, `headers`, or both (R2) |
| `FOREMAN_AUTH_USER_HEADER` | none | Required when `headers` is on (12.5) |
| `FOREMAN_AUTH_GROUPS_HEADER` | none | Optional (12.5) |
| `FOREMAN_AUTH_REQUIRED_GROUP` | none | Optional (12.5) |
| `FOREMAN_TRUSTED_PROXIES` | none | Address ranges; required when `headers` is on (12.6) |
| `FOREMAN_SESSION_IDLE` | `168h` | Idle expiry (R8) |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket or socket proxy (R3) |

No secret variable exists yet in this epic; `_FILE` support is built and
tested generically so Epic 2 onward can mark variables secret.

## Pinned versions

Checked on 2026-11-02. T10 records them in STACK 1.

- Go 1.27.3; image `golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385`
- golangci-lint `v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481`
- `gcr.io/distroless/static-debian13@sha256:448810357cc8e67b7208988abdba0f5ec3d3aebee0a5b13eb624979792cffc0f`, with `USER 845:845`
- `mcr.microsoft.com/playwright:v1.64.1-noble@sha256:2f05f8a734509a7b3f26a912639488ee08f58a7b292d14d62271bcff9038768f`
- npm: `@playwright/test` 1.64.1, `@axe-core/playwright` 4.14.0, TypeScript 7.0.2 (fall back to the newest 6.x if 7 cannot type-check Playwright's types, and say so)
- htmx 2.0.11, `dist/htmx.min.js` sha256 `d6fdc75f204e6bdefa99b69bf1e6d4ac69b8a364f77929f45c13476b4000f717`
- Go modules: `modernc.org/sqlite` v1.61.1, `golang.org/x/crypto` v0.58.0; govulncheck v1.9.0

## Layout

Module `git.thewhittakers.org/todd/foreman`.

```
cmd/foreman/              main: serve (default) and healthcheck; startup order
internal/config/          environment and _FILE loading and validation
internal/secret/          Secret type that always logs as "[redacted]"
internal/datadir/         data directory layout, host path (3.5), ownership check (3.6)
internal/docker/          minimal client: version agreement, 14.3 allowlist
internal/store/           SQLite (WAL), embedded migrations, user and session queries
internal/auth/            password hash, rate limiter, sessions, trusted headers, client address
internal/web/             server, routes, handlers, cross-origin protection
internal/web/templates/   html/template files, embedded
internal/web/static/      CSS and vendored htmx with SHA256SUMS, embedded
e2e/                      Playwright tests, package.json, lockfile
```

Schema `0001_init.sql` (owned by T4):

- `users`: integer id; `username` unique, case-insensitive;
  `password_hash` (empty for header users); `role` not null, default
  `admin`, checked against a list (12.1); `created_at`.
- `sessions`: `token_hash` primary key (SHA-256 of the token); `user_id`;
  `created_at`; `last_seen_at`.

The Docker client ships only `GET /_ping`, `GET /version` and
`GET /containers/{id}/json`, used for 3.5 discovery. Foreman finds its
own container ID from `/proc/self/mountinfo`, falling back to the
hostname.

## Tasks

| Task | Wave | Owner, model | Files it may touch | SPEC / STACK | Done looks like |
|---|---|---|---|---|---|
| T1 Skeleton | 1 | builder, Opus | `go.mod`, `go.sum`, `Makefile`, `.golangci.yml`, `Dockerfile`, `.dockerignore`, `.gitignore`, `.forgejo/workflows/ci.yml`, `cmd/foreman/`, `internal/web/server.go` (`/healthz` only), `e2e/` scaffold with one smoke test plus axe, WORKFLOW.md "Local development" | SPEC 3.1, 14.11, 16.1, 17.1; STACK 9, 10, 13 | Every STACK 13 target exists; `test-game` and `e2e-game` are stubs naming Epic 3. The Go version guard (R1) works. `make verify`, `make e2e` and `make image` pass locally, and the image is under 30 MB. CI on this PR is green on the shared runner, and `make ci-clean` leaves nothing behind. |
| T2 DESIGN.md | 1 | ui-designer, Opus | `docs/DESIGN.md` | SPEC 17.2 to 17.6 | Type, colour tokens for both themes with contrast ratios, spacing, components (button, field, error, confirmation, state badge with a word), and the 320 px rules. |
| T3 Config | 2 | builder, Sonnet high | `internal/config/`, `internal/secret/` | SPEC 3.2, 4.1, 12.5, 12.6, 14.8 | Every variable above loads with its default. Setting `X` and `X_FILE` together is refused, naming which to remove. `headers` without trusted proxies or without a user header is refused. Table-driven tests. |
| T4 Store | 2 | builder, Sonnet high | `internal/store/` | SPEC 3.4, 12.1, 12.8 | WAL, foreign keys and busy timeout on. Migrations idempotent. User and session queries. Creating the first admin is atomic when two requests race. |
| T5 Data dir and Docker | 2 | builder, Opus low | `internal/datadir/`, `internal/docker/` | SPEC 3.3, 3.5, 3.6, 14.3 | Host path from the variable or mount discovery, or a plain-words refusal. Writability and ownership probe per R7. Tests use a fake daemon, including one that fails on any endpoint outside 14.3. |
| T6 UI shell | 2 | ui-designer, Opus | `internal/web/templates/`, `internal/web/static/`, `internal/web/render.go` and its test | SPEC 17.1 to 17.4, 3.7 | Layout, sign-in, setup, signed-out and placeholder home pages following DESIGN.md. htmx vendored, with a test that checks its checksum. One test renders every template. |
| T7 Auth core | 2 | builder, Sonnet high | `internal/auth/` | SPEC 12.4, 12.5, 12.8 | argon2id in a self-describing format, rehashed at sign-in when settings rise. Rate limiter per username and per address. Client address: the rightmost untrusted `X-Forwarded-For` entry, only when the direct peer is a trusted proxy. Header identity. Session create and check behind a small store interface. |
| T8 Wiring | 3 | builder, Sonnet high | `cmd/foreman/`, `internal/web/*.go` except `render.go` | SPEC 12.3 to 12.8, 14.7, 15.1 | Startup order: config, data directory check, store, server. Setup open until an admin exists (R4). Sign-in, sign-out, idle expiry, header sign-in, cross-origin protection, Secure cookie per R5, JSON logs. Handler tests. |
| T9 End-to-end and security tests | 4 | tester, Opus | `e2e/tests/`, new `*_test.go` files only | SPEC 12.3 to 12.8, 14.7, 14.8, 17.1, 17.2 | Setup, sign-in, sign-out and expiry at desktop and 320 px, with axe. Spoofed headers from an untrusted address are ignored. A second setup is refused. The rate limit works. No secret appears in logs. |
| T10 Fold | 5 | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md`, `docs/STACK.md`, `docs/adr/`, delete this file | All | STATUS section from the task PR bodies. Rulings and the variable table folded into the SPEC sections named. Versions in STACK 1. A toolchain ADR for R1. |

## Waves and shared files

- Wave 1: T1 and T2, in parallel. Wave 2 starts from the epic head once
  both land.
- Wave 2: T3 to T7, in parallel. T4 and T7 both add Go modules, so
  `go.mod` and `go.sum` will conflict; the merger resolves them with
  `go mod tidy` and nothing else.
- Only T1 creates `internal/web/server.go`; from wave 3, T8 owns it.
- T6 edits no Go file except `render.go` and its test.
- Migration number 0001 belongs to T4.
- Wave 3: T8. Wave 4: T9. Wave 5: T10.

## Checks before the epic pull request

- `make verify`, `make e2e` and `make image` on the epic head, after
  merging `main` in.
- CI green on the epic pull request.
- Reviewers on the epic head: code-reviewer, security-reviewer (sign-in,
  headers, sessions, Docker API, file paths), a11y-reviewer (UI).
- No real-game check: this epic does not touch server control, worlds,
  snapshots, mods or versions.

## Risks

- **CI on the shared runner is the long pole.** T1 must prove that the
  job can reach Docker, that `--volumes-from` works, and that cleanup by
  name works. Every later wave waits on T1, so its debugging is
  time-boxed.
- **First builds are slow.** The SQLite package is large, and CI starts
  with a cold cache each run.
- **T8 is the bottleneck**, since it needs T3 to T7. Keeping T7's store
  interface small keeps T8 to wiring.

## Left out on purpose

The audit and app-settings tables, the SSE extension, the fake Docker
command and fake RCON server (Epic 3), OIDC (Epic 10), a persistent CI
cache, role logic beyond the column, and linking header users to local
users beyond matching username.
