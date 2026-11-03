# Foreman

[![ci](https://github.com/toddawhittaker/foreman/actions/workflows/ci.yml/badge.svg)](https://github.com/toddawhittaker/foreman/actions/workflows/ci.yml)

Foreman (a working name) is a small, self-hosted web app that runs
Factorio servers through Docker. It manages **worlds**: a world is a map
with its own mods, settings and pinned game version. You switch between
worlds with one click, and every risky change takes a snapshot first so
a save is never lost.

## Status

Early development; there is nothing to install yet. The foundation is
built: configuration, the app database, sign-in, sessions and the page
layout. Server control, worlds, snapshots, mods and game versions come in
later epics. See [docs/STATUS.md](docs/STATUS.md) for what each epic has
delivered, and [docs/SPEC.md](docs/SPEC.md), section 18, for the epic
order.

## Documentation

- [docs/VISION.md](docs/VISION.md): what Foreman is for, its principles
  and its non-goals.
- [docs/SPEC.md](docs/SPEC.md): the numbered requirements.
- [docs/STACK.md](docs/STACK.md): the technology choices and why.
- [docs/DESIGN.md](docs/DESIGN.md): the user interface design rules.
- [docs/WORKFLOW.md](docs/WORKFLOW.md): branches, pull requests, checks
  and local development.
- [docs/adr/](docs/adr/): decision records.

## Development

You need Go 1.27.3 (exactly; see WORKFLOW.md, "Local development"),
Docker and `make`. The linter and the browser tests run in pinned
containers, so you do not install them.

```sh
make verify   # format, vet, lint, unit tests with coverage, e2e type check
make run      # serve locally on http://localhost:8080
make e2e      # browser and accessibility tests
make image    # build the container image as foreman:dev
```

CI runs `make ci` on every pull request and on every push to `main`.

## License

[MIT](LICENSE).
