# Branching, review, and checks

## Where the code lives

The repository is on GitHub: `origin` is
`https://github.com/toddawhittaker/foreman` (public). Use `gh` for pull
requests, issues and releases. Add `--json` with a field list for
machine-readable output, and use `gh api repos/toddawhittaker/foreman/...`
for anything `gh` has no command for, such as a PR's changed files.

## Checks

`make verify` run locally on a PR's head commit is the check that gates
every merge, and the orchestrator or the merger agent runs it before
merging. The CI workflow in `.github/workflows/ci.yml` runs on every
pull request into `main`, every push to `main`, and every push to an
`epic/` branch. A task pull request into an epic branch runs no CI; the
push its merge makes does, and a failure there becomes a fix task at
once. Its jobs run in parallel and
together check the same things as `make ci`, plus the image size limit:
`make verify` is split into `vet`, `lint` and `test` jobs, the type check
runs with `e2e`, and `image` builds the container. A final `ci` job passes
only when every other job passed or was skipped for a reason below. A ruleset on `main` blocks direct
pushes, force pushes and deletion, and requires a pull request whose
branch is up to date with `main` and whose `ci` job passed. Adding a job
never needs a ruleset change. Read a failing run's log with
`gh run view --log-failed`.

A first `changes` job decides whether the build and test jobs (`verify`,
`e2e` and `image`) run. It skips them in two cases. First, when a pull
request or a push to an epic branch changes only files that nothing in the build, the tests or CI
reads: anything under `docs/`, `.claude/` or `.githooks/`, and Markdown
files anywhere outside `.github/`. Second, when an earlier green run
already tested the same git tree, on a pull request or a push. So the
epic pull request into `main` reuses the run on the epic branch's head.
Every full green run records its tree as an artifact named
`tested-tree-<tree hash>`, kept 30 days, so a tree is re-tested on a fresh
runner now and then. Markers from forks' pull requests are ignored. A push
to `main` is never skipped for being documentation only. If the lookup
fails, everything runs; if `changes` itself fails, `ci` fails.

A `trailers` job runs on every pull request into `main`, even when the rest is
skipped. It fails when a commit in the pull request, or its title or body,
credits an AI assistant as co-author, and names the commit. Editing the
title or body does not rerun CI, so an edit is checked at the next push.
GitHub lists
every co-author as a contributor, and a squash merge copies those lines
onto `main`. The pattern lives in `.github/ai-trailer-check`, which the
commit-msg hook also runs (see "Secret scanning").

CI runs on GitHub-hosted `ubuntu-24.04` runners, so the job's operating
system changes only when we change it. A push to a pull request cancels
that pull request's older run. Each job has a time limit: 25 minutes for
the `verify` checks and 15 or fewer for the rest.

The repository is public. CI runs whatever code a pull request brings,
so a pull request from anyone outside the repository waits for a
maintainer's approval before its CI runs. The workflow's token can only
read, and pull requests from forks get no secrets.

Dependabot opens weekly pull requests for GitHub Actions, Go modules,
the end-to-end npm packages and the Dockerfile's base image. The
Makefile's tool images, and `@playwright/test` with them, are updated by
hand. GitHub's secret scanning and push protection are on, beside the
local gitleaks hook.

## Local development

You need Go 1.27.3, Docker and `make`. golangci-lint and Playwright run
in pinned images, so you do not install them. `make verify` refuses to
run when your Go differs from `GO_VERSION` in the Makefile, which also
names the build image, so install exactly that version:

```sh
curl -fsSLo /tmp/go.tgz https://go.dev/dl/go1.27.3.linux-amd64.tar.gz
echo "65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40  /tmp/go.tgz" | sha256sum -c -
mkdir -p ~/.local/go1.27.3 && tar -C ~/.local/go1.27.3 --strip-components=1 -xzf /tmp/go.tgz
export PATH="$HOME/.local/go1.27.3/bin:$PATH"   # add this line to your shell profile
```

The Makefile sets `GOTOOLCHAIN=local`, so Go never downloads another
toolchain behind your back. Tool caches live in `.cache/` and the
end-to-end packages in `e2e/node_modules/`; both are ignored.

The targets (STACK.md, section 13):

- `make verify`: the Go version check, the format check, `go vet`,
  golangci-lint, the unit tests with the race detector and the 70%
  coverage floor across `internal/`, and a type check of `e2e/`. Run it
  before every commit.
- `make test`: the unit tests and the coverage floor alone. Locally it
  runs the web, game and world packages' tests as small batches on
  `TEST_SHARDS` worker processes (default: one per CPU, limited by available
  memory at 1.5 GiB each; `TEST_SHARDS=1` runs one process). `make
  test-shards` shows the count without testing.
  CI sets `CI`, so it always runs one.
- `make lint` and `make fmt`: check or rewrite formatting, and lint.
- `make build`: the static binary at `bin/foreman`.
- `make run`: serve locally on port 8080, with data in `.dev-data/`. It
  runs with `FOREMAN_HOST_DATA_DIR` set, and your own user
  and group IDs as the game's. It talks to Docker through `DOCKER_HOST`
  (the default socket if unset), so you need a Docker socket you can use to
  start a game. Without one the Server page shows "Unknown" with the
  reason, and the other pages work.
- `make e2e`: build the binary and the fake game, and run the Playwright
  and axe tests in the Playwright image. No real Docker or game is used.
- `make image`: build the container image as `foreman:dev`.
- `make deploy`: build the image from your checkout and restart only the
  Foreman service in a local Docker Compose stack. Nothing in the
  repository names a stack, so set `DEPLOY_DIR` to its folder, either as
  `make deploy DEPLOY_DIR=/path/to/stack` or once in `local.mk` in the
  repository root (git ignores that file):

  ```make
  DEPLOY_DIR = /path/to/stack
  ```

  The service is `foreman` and the image `foreman:dev` unless you set
  `DEPLOY_SERVICE` or `DEPLOY_IMAGE` there too. Only Foreman is
  recreated, so the proxy and a running game keep going. The target waits
  up to `DEPLOY_TIMEOUT` seconds (120) for the container to report
  healthy, fails if it does not, and prints the image ID and the commit.
  It refuses a checkout with uncommitted changes unless you pass
  `FORCE=1`, so what runs matches a commit.
- `make vuln`: govulncheck. It needs the network, so it is not in
  `verify`.
- `make ci`: `verify`, `e2e` and `image` in turn: everything CI checks,
  in one local run.
- `make tidy`: `go mod tidy`.
- `make test-game`: run the real-game tests in `./internal/game/` and
  `./internal/web/` against the real Docker daemon and the game test
  images (STACK.md, section 1), one package at a time (`-p 1`), because
  both bind the game's UDP port.
  `TAG=1.1`, `2.0` or `2.1` runs one. It does not download the game
  images: pull each one first with the `docker pull` command in the error
  it prints. It starts containers named `foreman-e3test-*` and removes
  them, with their data under `.cache/test-game/`.
- `make e2e-game`: run the phone scenario against the real 2.1 game image.
  It needs that image on the host (it prints the `docker pull` to run) and
  a Docker socket (`DOCKER_SOCK`, `/var/run/docker.sock` by default).
  **Run it only on a development machine.** It gives the Playwright
  container the host's Docker socket, which is root on that host, and it
  runs on a Docker network of its own that it creates and removes. Do not
  run it in CI or on a shared or production host.

The first `make verify` or `make e2e` pulls the pinned images and runs
`npm ci` in `e2e/`; later runs reuse them. Run `make test-game` and `make
e2e-game` before an epic pull request that touches server control, world
creation (Generate, Clone), game settings, world switching, snapshots, mods
or game versions.

Every end-to-end test fails when its Foreman process writes an ERROR log
line the test did not declare. A test that causes an error on purpose
declares the line with `foreman.expectError(text or regexp)`, and a
declaration that matches no line fails the test, so declarations cannot go
stale. Foreman stops when the test ends and the check then reads its whole
output, but an error written by background work after the last assertion
can still be missed, so a test waits for what its action sets running
(a download, a switch) to finish before it ends.

## Branches

- `main` is always releasable. It changes only through a pull request.
- Each epic in the SPEC.md epic list gets a long-lived branch named
  `epic/<n>-<slug>`, for example `epic/1-server-control`. It also changes
  only through a pull request.
- Work happens on short-lived task branches cut from the epic branch,
  named `<epic-slug>/<task>`. A task branch is merged into its epic by
  pull request, squash merge. The orchestrator merges it with `gh pr
  merge --squash` once it has checked the work and `make verify`
  passes; no person reviews a task pull request.
- When an epic's acceptance criteria are met and its epic-level review is
  done, the epic branch is merged into `main` by pull request with a merge
  commit, so the epic's history is kept.
- To bring `main` into an epic branch, cut a short-lived branch from the
  epic, merge `main` into it, and open a pull request back into the epic.
- Branches are deleted on merge.

## Epic plans

An epic's detailed plan, `docs/EPIC-<n>.md` (its rulings, task table and
check steps), lives only on the epic branch, so every agent working from a
checkout can read it.

- Code comments, tests and other docs cite SPEC.md sections or ADRs, never
  the plan, because the plan goes away.
- The epic's last task folds the plan's lasting rules into SPEC.md in a
  sentence or two each, puts any "why" a later reader will ask into an
  ADR, and deletes the plan.
- Parallel task pull requests do not edit `docs/STATUS.md`, because they
  would all conflict on it. Each one puts its proposed STATUS line (what it
  delivered, gaps left) in its pull request body instead. The fold task
  writes the epic's STATUS section once, from those bodies.
- So `main` never holds a plan at a merge. Git history keeps it.

Before a feature epic's plan is written, the architect agent designs it
(not for small fix batches). It reads wide: docs/VISION.md, the epics
still to come and the open issues for the area. For each significant
choice it gives the smallest change and the shape the area should have,
recommends the smallest change unless the choice is hard to undo, and
brings large gaps to the user to decide.

At each milestone gate in SPEC.md, the architect also reviews the whole
system, one area per run, for layers and boundaries that local decisions
have bent. The user picks which findings become an epic.

## Code style

- Comments say why, never history. Do not write issue or PR numbers, epic
  or task names, review codes, "ruling N", or pointers into epic plans.
  Cite a SPEC.md section or an ADR instead.
- Do not copy a helper. Search for one that does the job and import it; if
  two places need the same code, move it into a shared module.
- A value that must match in two files or processes is one exported
  constant, not two values and a comment.

## Pull requests

Every pull request cites the SPEC.md and STACK.md sections it serves and
says how it was verified. The template, in
`.github/pull_request_template.md`, asks for both. `make verify` must
pass. An epic pull request must be up to date with `main` before it is
merged. Task pull requests into an epic need not be; they are updated only
when GitHub reports a conflict.

There are two kinds of pull request, merged by different people:

- A **task pull request**, from a task branch into its epic, is squash
  merged by the orchestrator once it has checked the work and `make verify`
  passes. Review happens once, later, over the whole epic.
- An **epic pull request**, from an epic branch into `main`, is opened
  only after every task pull request has landed, the reviewers named in
  the `epic-run` skill have run over the epic head, every required finding
  is fixed or explicitly deferred, and the full local checks are green.
  Only the user merges an epic pull request into `main`.

Group small related changes into one pull request, or put them on the
current epic branch. Auto-merge is on for the repository, so merge on
green is `gh pr merge <n> --auto --squash --delete-branch`, after
`gh pr update-branch` when `main` has moved.

The merger agent lands a list of task pull requests, or one that
conflicts. It reruns `make verify` up to twice when a failure looks like a
flake, and escalates anything else. It never edits code to make a check
pass.

## Secret scanning

A pre-commit hook runs gitleaks on staged changes. Enable it once per
clone:

```sh
git config core.hooksPath .githooks
```

Install gitleaks from https://github.com/gitleaks/gitleaks/releases or
your package manager. Files that legitimately contain secret-shaped
strings are allowlisted in `.gitleaks.toml`.

The same hooks folder holds a commit-msg hook. It refuses a commit message
that names an AI assistant as a co-author (a `Co-authored-by:` line for
Claude or Anthropic, or a "Generated with Claude Code" line). Agents write
commit messages themselves, so Claude Code's own attribution setting cannot
stop such a line; this hook does. CI runs the same check on every pull
request (see "Checks").

## Dependencies

Versions are pinned exactly and the lockfile is committed. Each world
records its exact game version and runs the game image by that version tag
(ADR 0005), per the vision's "Pinned, never drifting" principle. The game
test images are pinned by digest (STACK.md, section 1).
