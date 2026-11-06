# Foreman: stack

docs/SPEC.md says what Foreman does. This file says what it is built with,
why, and what was rejected. Sections are numbered so prompts, commits and
pull requests can cite them. Changing a choice marked "hard to undo" needs
an ADR (decision record) in `docs/adr/`.

## 1. Versions

Every version is pinned exactly, and Foreman's own build, test and runtime
images are pinned by digest (CLAUDE.md, "Project rules"). The game images
the product runs are not pinned here: each world names an exact Factorio
version and runs `factoriotools/factorio:<version>` by tag (SPEC 4.2,
ADR 0005). The files named below are the source of truth; this list
is a copy and goes stale if they change without it.

- Go 1.27.3, set once as `GO_VERSION` in the `Makefile`. `go.mod` states
  only the language minimum, `go 1.27.0` (ADR 0001).
- Build image: `golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385`.
- golangci-lint: `golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481`.
- Runtime image: `gcr.io/distroless/static-debian13@sha256:448810357cc8e67b7208988abdba0f5ec3d3aebee0a5b13eb624979792cffc0f`,
  run as `USER 845:845`.
- Playwright image: `mcr.microsoft.com/playwright:v1.64.1-noble@sha256:2f05f8a734509a7b3f26a912639488ee08f58a7b292d14d62271bcff9038768f`.
- npm packages in `e2e/package.json`, with `e2e/package-lock.json`:
  `@playwright/test` 1.64.1, `@axe-core/playwright` 4.14.0, `typescript`
  7.0.2 and `@types/node` 26.6.6 (the test fixture starts the binary with
  Node's `child_process`).
- Node: the version inside the Playwright image. It runs only the
  end-to-end tests.
- htmx 2.0.11, vendored as `internal/web/static/vendor/htmx.min.js`; its
  SHA-256 is in `SHA256SUMS` beside it, and a test checks it.
- htmx-ext-sse 2.2.4 (the server-sent events extension for htmx 2.0.x),
  vendored as `internal/web/static/vendor/htmx-ext-sse.min.js`, with its
  SHA-256 in the same `SHA256SUMS`, checked by the same test.
- Go modules in `go.mod`: `modernc.org/sqlite` v1.61.1,
  `golang.org/x/crypto` v0.58.0 and `golang.org/x/sys` v0.49.0 (direct
  since Epic 2: hardlinks into a snapshot folder, which the standard
  library cannot make relative to an open folder).
- `golang.org/x/text`, direct from Epic 6, for language matching, plural
  rules and number formats (SPEC 22). The exact version is pinned in
  `go.mod`, and the Epic 6 fold copies it here.
- govulncheck v1.9.0, run by `make vuln`.
- CI: GitHub Actions on the `ubuntu-24.04` runner. Every action is
  pinned to a full commit: `actions/checkout` v7.0.1,
  `actions/setup-go` v7.0.0, `actions/cache` v6.1.0,
  `docker/setup-buildx-action` v4.4.1 and `docker/build-push-action`
  v7.4.0. CI reads `GO_VERSION` and `GO_IMAGE` from the Makefile, so the
  Go pin stays in one place. Dependabot proposes updates weekly
  (`.github/dependabot.yml`).
- Game test images, one per supported line (SPEC 10.8), pinned by digest.
  They are what `make test-game` and the end-to-end checks against a real
  game (`make e2e-game`) run, not what the product runs. The
  `GAME_IMAGE_1_1`, `GAME_IMAGE_2_0` and `GAME_IMAGE_2_1` variables in the
  `Makefile` are the source of truth:
  - `factoriotools/factorio:1.1.110@sha256:6fb9b7bfb12fb51bf668fce7e29cb1e98053f248201e64dfee870ef73b2f31cb`
    (the last 1.1 release);
  - `factoriotools/factorio:2.0.77@sha256:312341f29987b5a6b11f00ba8240192f7564936af9913384d13f7f4c535708ef`;
  - `factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28`;
  - `GAME_IMAGE_2_1_PREV`: `factoriotools/factorio:2.1.23@sha256:1e1773337f5be1d93f61d1d64757f18c19c8eb345c96acc57e0d3567a4fd4c3f`,
    the starting point of the same-line update test (SPEC 10.3).

  Foreman runs a world on the plain tag, so the tests run the plain tag too.
  Before a real-game run, `make test-game` and `make e2e-game` check that
  the plain tag on the host resolves to the pinned digest, and refuse with
  a `docker pull` and `docker tag` hint when it is missing or different.
  A tag can be rebuilt upstream (2.1.24 was), which is why the tests pin
  the digest, so a test run is repeatable. The product accepts that risk
  for a world's image (SPEC 4.2). Foreman never downloads an image at
  startup (SPEC 3.7).

## 2. Backend: Go

Hard to undo.

Go builds one static binary, gives an image of roughly 25 MB, and idles at
roughly 30 MB of memory. Its standard library covers HTTP, SQL, JSON, zip,
cryptography and structured logging, and handles hardlinks, file times and
atomic renames (SPEC 6.8, 6.9) and the save reader (SPEC 9.9) with no
extra dependencies.

Rejected:

- **TypeScript on Node**: one language for server and browser, but an
  image of 80 to 150 MB, more memory, and a large npm supply chain to pin.
- **Python with FastAPI**: an image of about 100 MB, more memory, and
  weaker type checking.
- **Rust**: the smallest at runtime, but slow builds and costlier changes
  for no need the spec has.

## 3. Frontend: server-rendered HTML with htmx

Costly to undo, but no user data depends on it.

Pages are rendered with Go's `html/template`. htmx updates parts of a page,
and server-sent events (SSE) drive the live pages (SPEC 16.3). htmx and its
SSE extension are vendored into the repository with their checksums and
embedded in the binary, so nothing loads from a CDN (SPEC 3.7). CSS is
hand-written and follows docs/DESIGN.md (SPEC 17.6). There is no build
step and no public JSON API.

Rejected:

- **A single-page app** (Svelte or Preact, built with Vite): a richer
  component model, but a Node build toolchain, an npm dependency tree, and
  a JSON API that would become a contract.
- **templ** (typed Go templates): the compiler would check templates, but
  it adds a code generator and generated files. A test that renders every
  template (section 9) covers most of the same ground.

## 4. Database: SQLite

The engine and schema are hard to undo; the driver is cheap to swap.

SPEC 3.1 (one container, no other service) leaves SQLite as the only
engine. It is used through plain `database/sql` with the pure-Go
`modernc.org/sqlite` driver, so the binary stays static, and with
write-ahead logging (WAL) on. Migrations are numbered SQL files embedded in
the binary and tracked with SQLite's `user_version`. There is no ORM
(object-relational mapper). The schema keeps room for several worlds and
for roles (SPEC 5.3, 12.1).

Rejected:

- **mattn/go-sqlite3**: faster, but needs a C compiler and makes a static
  build harder.
- **ncruces/go-sqlite3**: runs SQLite as WebAssembly; newer and less proven.
- **goose** for migrations: kept as the fallback if hand-written
  migrations grow past about 60 lines.

## 5. Docker client: hand-written

Cheap to change.

A small client speaks HTTP to the Docker socket or to a socket proxy, and
allows only the endpoints in SPEC 14.3. A test fails on any call outside
that list. It agrees an API version with the daemon through `GET /version`
rather than hard-coding one; recent Docker Engine releases refuse old API
versions.

The client has two HTTP clients. The first has a total timeout and serves
queries. The second has none, because a log follow, an image pull or a stop
can last minutes; each of those calls sets its own deadline. Both refuse
redirects. Calls carry query strings and JSON bodies, and only the path is
checked against SPEC 14.3. It reads Docker's framed log output into separate
standard output and error lines, and reports an error that Docker sends
inside a 200 reply while pulling an image.

Rejected: **the official moby client package**. It is complete, but brings
a heavy dependency tree and can reach endpoints outside SPEC 14.3 without
anyone noticing.

## 6. RCON client: hand-written

Cheap to change.

RCON (the game's remote console protocol) is small: about 150 lines with
our own timeouts, tested against a fake server. Escaping commands built
from user text (SPEC 14.9) lives in our code either way. The client runs
one command at a time and reads one reply packet per command, up to
4 MiB, because the real game answers every non-empty command with exactly
one packet of any length and never answers an empty one.

Rejected: **gorcon/rcon**. It is fine, but one more dependency for little
saved.

## 7. Sign-in and secrets

- **Password hash (SPEC 12.4)**: argon2id from `golang.org/x/crypto`, with
  at least OWASP's minimum settings (19 MiB of memory, 2 passes), stored as
  a self-describing hash string so settings can be raised at the next
  sign-in. Hard to undo. Rejected: bcrypt, which is simpler but weaker
  against attackers using graphics cards.
- **Sessions (SPEC 12.8)**: hand-written server-side sessions. The cookie
  holds a random token; the database holds only its hash.
- **Cross-site request forgery (CSRF)**: Go's `http.CrossOriginProtection`
  plus SameSite=Lax cookies. Rejected: gorilla/csrf form tokens, a
  dependency the standard library now makes unnecessary.
- **OIDC sign-in (SPEC 12.7)**: `coreos/go-oidc` v3 with
  `golang.org/x/oauth2`. Rejected: zitadel/oidc, which is larger and
  mostly server-side features Foreman does not need.
- **Stored portal credentials (SPEC 9.11)**: AES-GCM from the standard
  library, with the key derived through HKDF from `FOREMAN_SECRET_KEY`.
  Each stored value starts with a version byte so the format can change.
  Hard to undo.
- **Logging secrets (SPEC 14.8, 15.1)**: logs use `log/slog` in JSON.
  Secrets are held in a `Secret` type that always logs as "[redacted]".

## 8. MCP: the official Go SDK

The library is cheap to change; the `/mcp` path is hard to undo, because
AI clients are configured with it.

The MCP endpoint (SPEC 21) uses `modelcontextprotocol/go-sdk`, mounted at
`/mcp` on the main server, with bearer API tokens (SPEC 12.10).

Rejected:

- **mark3labs/mcp-go**: popular, but community-maintained.
- **Hand-written JSON-RPC**: no dependency, but we would track protocol
  changes ourselves.

## 9. Tests

Cheap to change.

- **Unit tests**: `go test -race`, with no assertion library. One test
  renders every template.
- **End-to-end tests**: Playwright Test in TypeScript, with
  `@axe-core/playwright` for the accessibility scan SPEC 17.1 requires,
  run at desktop width and at 320 px. They run inside the pinned
  `mcr.microsoft.com/playwright` image, so browsers are never downloaded
  during a job. Node is used only here. Rejected: playwright-go and
  chromedp, which keep one language but are community projects and need
  axe injected by hand.
- **Fakes**: a test-only fake Docker Engine (`internal/docker/dockertest`)
  that serves only the SPEC 14.3 endpoints and fails on anything else, and
  a fake RCON server (`internal/rcon/rcontest`) that answers like the real
  game. On top of them, `internal/fakegame` is a fake game: a start replays
  the real log lines kept in `internal/game/testdata`, serves RCON on the
  container's own loopback address, and follows `unless-stopped`. With
  these, ordinary tests and the end-to-end tests need no game. The fake
  game is built by `make e2e` and is never in the image. It is not the real
  Docker: its restart delay is capped at 3 seconds (Docker backs off to a
  minute) and the `on-failure` restart policy is not modelled.
- **Real game**: Go integration tests behind the `game` build tag run
  against a real `factoriotools/factorio` container (`make test-game`),
  and a Playwright scenario runs against the real 2.1 image (`make
  e2e-game`). They run on demand and before every epic pull request that
  touches server control, world switching, snapshots, mods or game versions
  (CLAUDE.md, "Testing rules"), not on every task, because the images are
  large and each start takes tens of seconds.
- **In CI**: parallel jobs that together are `make ci`, with `make
  verify` split into `vet`, `lint` and `test` so its slow parts do not
  wait on each other, since they grow with the code. The end-to-end tests
  run as four shards (`make e2e SHARD=1/4` and so on; an empty `SHARD`
  runs them all). Go modules and build output, the lint and npm
  caches, and the image's build layers are cached between runs. Each job
  gets a fresh runner, so nothing needs cleaning up afterwards.

## 10. Image

The base image is cheap to change; the default user is hard to undo.

The image is built in stages from a pinned `golang` image with
`CGO_ENABLED=0 -trimpath`, onto `gcr.io/distroless/static` (the Debian 13
variant if available), pinned by digest. It runs as `USER 845:845` (SPEC
3.6). There is no shell, so the health check is a `foreman healthcheck`
subcommand.

Rejected:

- **scratch**: smaller, but CA certificates, time zone data and the user
  file must be copied in by hand.
- **Alpine**: a shell for debugging, but more to patch.

## 11. Size targets

- Image under 30 MB compressed (SPEC 16.1).
- Under 50 MB of memory when idle (SPEC 16.2).

## 12. Smaller choices

- **Scheduled restarts (SPEC 7.12)**: a hand-written daily scheduler using
  `time.LoadLocation`. Rejected: robfig/cron, since the spec needs only
  "at HH:MM in time zone Z".
- **Languages (SPEC 22)**: a small in-repo package, `internal/i18n`, over
  JSON catalogues embedded in the binary. It uses `golang.org/x/text`,
  which the Go team maintains, for matching the browser's languages,
  CLDR (Common Locale Data Repository) plural rules and per-language
  number formats. Rejected: `gotext`, which cannot read templates and
  generates code; and `go-i18n`, a third-party copy of what `x/text`
  already does. Rules for writing and adding strings are in
  docs/I18N.md.
- **Linting**: golangci-lint v2.
- **Vulnerability scan**: govulncheck.

## 13. Make targets

| Target | What it does |
|---|---|
| `make verify` | `check-go`, the format check, `go vet`, golangci-lint, `make test`, and a type check of the end-to-end tests. Fast. The Stop hook and the merge gate run it. |
| `make check-go` | Fails when the local Go, or the Go in the build image, differs from `GO_VERSION` (ADR 0001). |
| `make test` | Unit tests with `-race`. Fails when statement coverage across `internal/` is below 70%. |
| `make lint` | golangci-lint, in its pinned image. |
| `make fmt-check` | Fails when a Go file is not formatted. This is the only place format is checked. |
| `make fmt` | Rewrite formatting. |
| `make build` | The static binary, with frontend files embedded. |
| `make image` | Build the container image with a local tag. |
| `make run` | Run Foreman from source against `./.dev-data` (also set as `FOREMAN_HOST_DATA_DIR`), with your own user and group as the game's. It contacts Docker at `DOCKER_HOST` (the default socket unless you set it) to show and control the game. If Docker cannot be reached the Server page shows "Unknown" with the reason and the other pages work. |
| `make e2e` | Playwright and axe against the built binary and the fake game, at desktop width and 320 px, in dark and light. A test that needs Docker points `DOCKER_HOST` at the fake game's Unix socket, so no real Docker or game is used. |
| `make test-game` | The game controller against the real Docker daemon and the game test images (`GAME_IMAGE_*`, section 1), one after another, because they share the game port. It first checks that each image's plain tag on the host is the pinned digest. It checks start to InGame with RCON, the newest-save and chosen-save rules, players, the console, graceful and forced stop, a corrupt save crash loop, no password in relayed lines, the save reader on a save the game wrote, and re-attach to a running game, and update and rollback with the real game: 2.0.77 to 2.1.24 and 2.1.23 to 2.1.24, each reaching InGame and rolling back to a byte-identical save. `TAG=1.1`, `2.0` or `2.1` runs one line, and checks only the images that line's tests use (`2.0` and `2.1` include the update tests). Containers are named `foreman-e3test-*` and removed afterwards. Nothing is downloaded: an image must already be on the host. |
| `make e2e-game` | The vision's phone scenario at 320 px against the real 2.1 image, and a switch from one world to another and back: Playwright in its pinned image, with the host's Docker socket, runs `bin/foreman`, which starts the game on a Docker network made for the run; the target removes the game container and the network on exit. It refuses, with a `docker pull` hint, when the image is missing or is not the pinned digest. For milestone gates and epic pull requests; run it only on a development machine (docs/WORKFLOW.md). |
| `make test-catalogue` | Created in Epic 9. Re-runs the crash reason catalogue's scenarios (SPEC 8.8) against `factoriotools/factorio:$(VERSION)`, for example `VERSION=2.1.22`. Downloads nothing: the image must already be on the host. |
| `make vuln` | govulncheck. It needs the network, so it stays out of `make verify`. |
| `make ci` | `verify`, then `e2e`, then `image`: everything CI checks, in one local run. CI runs the same checks as parallel jobs. |
| `make tidy` | `go mod tidy`. |

## 14. Left out on purpose

- No public REST API. The spec needs only the UI and MCP.
- No ORM, CSS framework or state-management library.
- No release tooling (such as GoReleaser) until Epic 11.
- Nothing for running several worlds at once, beyond the schema leaving
  room for it (SPEC 5.3).
