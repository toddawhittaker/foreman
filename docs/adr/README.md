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
| [0004](0004-adopted-game-folder.md) | Superseded by 0005. Adopted mode: a link layout switched by one rename, originals kept, records in the database, strict container rules, a network warning, header refusal by range. |
| [0005](0005-managed-only-versions-by-tag.md) | Managed mode only; a world runs any patch release of a supported line, by version tag. |
| [0006](0006-vpn-sidecar-network.md) | Optionally, the game shares a VPN sidecar's network, with RCON bound to the game-network address. |
| [0007](0007-imported-from-in-world-json.md) | Where a world was imported from is kept in `world.json` and matched by the save's name, size and modification time, not a hash. |
| [0008](0008-message-catalogue.md) | Languages: an in-repo `internal/i18n` over `x/text`, dotted keys with a hash check, message ids in view models, one catalogue file per area. |
| [0009](0009-map-exchange-string-in-go.md) | Generate decodes a map exchange string in Go, a port of rfvgyhn's parser (MIT), refusing what it cannot read exactly; golden strings from the real game keep it honest. |
| [0010](0010-full-map-settings-files.md) | A changed Generate form writes both map files complete and passes no `--preset`; an unchanged preset passes `--preset` alone. |
| [0011](0011-demolisher-territories-by-control-mod.md) | Demolisher territories come from a control-only mod in a one-off `--create` run (not RCON), drawn into the PNG in Go with `x/image` and a Noto subset, offered up to 3072 tiles. |
