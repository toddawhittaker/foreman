# Branching, review, and checks

The real-game test procedure is added here once the real-game tests
arrive (Epic 3).

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
pull request and every push to `main`. Its jobs run in parallel and
together check the same things as `make ci`, plus the image size limit:
`make verify` is split into `vet`, `lint` and `test` jobs, the type check
runs with `e2e`, and `image` builds the container. A final `ci` job passes
only when every other job passed. A ruleset on `main` blocks direct
pushes, force pushes and deletion, and requires a pull request whose
branch is up to date with `main` and whose `ci` job passed. Adding a job
never needs a ruleset change. Read a failing run's log with
`gh run view --log-failed`.

CI runs on GitHub-hosted `ubuntu-24.04` runners, so the job's operating
system changes only when we change it. A push to a pull request cancels
that pull request's older run, and each job stops after 15 minutes.

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
- `make test`: the unit tests and the coverage floor alone.
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
- `make vuln`: govulncheck. It needs the network, so it is not in
  `verify`.
- `make ci`: `verify`, `e2e` and `image` in turn: everything CI checks,
  in one local run.
- `make tidy`: `go mod tidy`.
- `make test-game`: run the game controller against the real Docker daemon
  and the three game test images (STACK.md, section 1), one after another.
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
switching, snapshots, mods or game versions.

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

## Dependencies

Versions are pinned exactly and the lockfile is committed. Each world
records its exact game version and runs the game image by that version tag
(ADR 0005), per the vision's "Pinned, never drifting" principle. The game
test images are pinned by digest (STACK.md, section 1).
