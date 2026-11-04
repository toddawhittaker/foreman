# Architecture decision records

An architecture decision record (ADR) is a short note about one decision
that shapes the system: what we chose, why, and what it costs. We keep
them so that a new contributor or agent can find the reasoning behind a
choice without reading the whole history of the repository.

## Format

One file per decision, named `NNNN-short-title.md`, numbered in the order
the records are written. Copy `0000-template.md` to start a new one. Each
record has four parts:

- **Status**: Proposed, Accepted, Superseded by NNNN, or Deprecated.
- **Context**: the situation that forced a choice.
- **Decision**: what we decided, in the present tense.
- **Consequences**: what this makes easy and what it makes hard.

## Rules

- Keep a record under 40 lines. If it needs more, it is a document, not an
  ADR.
- Cite the `docs/SPEC.md` and `docs/STACK.md` sections the decision serves.
  Those files remain authoritative; an ADR records the decision, it does
  not replace the specification.
- Records are immutable once accepted. To change a decision, write a new
  record and mark the old one "Superseded by NNNN".

## Records

| Record | Decision |
|---|---|
| [0001](0001-toolchain.md) | Go runs locally and is pinned in the Makefile; the linter and Playwright run in pinned images. |
| [0002](0002-world-and-snapshot-formats.md) | World and snapshot formats on disk: ids, `world.json`, `snapshot.json` with hashes, restore by swap, hardlinks kept. |
| [0003](0003-game-container.md) | The game container: binary run directly, labelled name, fresh per start, polling, load-latest, game network rules. |
