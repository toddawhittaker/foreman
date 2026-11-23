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
that Fixes batch 17 left for mod icons.
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
