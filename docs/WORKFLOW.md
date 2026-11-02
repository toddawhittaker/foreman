# Branching, review, and checks

Local development commands and the real-game test procedure are added
here once docs/STACK.md settles the stack.

## Where the code lives

The repository is on Forgejo: `origin` is
`ssh://git@git.thewhittakers.org/todd/foreman.git` (SSH port 2222, set in
`~/.ssh/config`). Use `tea` (login `forgejo`, the default) for pull
requests, issues and releases. Add `--output json` for machine-readable
output, and use `tea api /repos/todd/foreman/...` for anything `tea` has
no command for, such as a PR's merge state or changed files.

## Checks

`make verify` run locally on a PR's head commit is the check that gates
every merge, and the orchestrator or the merger agent runs it before
merging. Once the stack epic adds a CI workflow under `.forgejo/workflows/`,
it runs the same `make` targets on the server and a green run is required
as well.

The Forgejo server has one shared runner, `forgejo-runner`, with the
labels `ubuntu-latest`, `ubuntu-24.04` and `docker`. Use `ubuntu-latest`.
Jobs run as root inside a Debian container with Node and `apt`. Jobs can
reach a Docker daemon: they can run and build images, run
`factoriotools/factorio`, and reach containers they start by name on a
network they create.

That daemon does not see the job container's files. A bind mount of a
path inside the job's workspace arrives empty. This is the same host-path
trap the vision describes for Foreman itself, so tests that need files in
a container copy them in (`docker cp`) or use a named volume. `tea` cannot
read job logs on this Forgejo version; read them in the web interface.

## Branches

- `main` is always releasable. It changes only through a pull request.
- Each epic in the SPEC.md epic list gets a long-lived branch named
  `epic/<n>-<slug>`, for example `epic/1-server-control`. It also changes
  only through a pull request.
- Work happens on short-lived task branches cut from the epic branch,
  named `<epic-slug>/<task>`. A task branch is merged into its epic by
  pull request, squash merge. The orchestrator merges it with `tea pr
  merge --style squash` once it has checked the work and `make verify`
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
`.forgejo/pull_request_template.md`, asks for both. `make verify` must
pass. An epic pull request must be up to date with `main` before it is
merged. Task pull requests into an epic need not be; they are updated only
when Forgejo reports a conflict.

There are two kinds of pull request, merged by different people:

- A **task pull request**, from a task branch into its epic, is squash
  merged by the orchestrator once it has checked the work and `make verify`
  passes. Review happens once, later, over the whole epic.
- An **epic pull request**, from an epic branch into `main`, is opened
  only after every task pull request has landed, the reviewers named in
  the `epic-run` skill have run over the epic head, every required finding
  is fixed or explicitly deferred, and the full local checks are green.
  Only the user merges an epic pull request into `main`.

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

Versions are pinned exactly and the lockfile is committed. Game images are
pinned by exact tag in each world, per the vision's "Pinned, never
drifting" principle.
