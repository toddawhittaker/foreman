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
| [0012](0012-mod-portal-client-and-search-source.md) | The mod portal client uses a fixed base, a checked download (2 redirects to `.factorio.com` only, no `Referer`, no token in errors), and searches the per-line mod list locally, because the portal's search needs credentials. |
| [0013](0013-sealed-stored-secrets.md) | Secrets typed into the UI are sealed with AES-GCM under an HKDF key from `FOREMAN_SECRET_KEY`, with a version byte and the row name as authenticated data, in a keyed `stored_secrets` table. |
| [0014](0014-one-snapshot-per-run-of-stopped-mod-changes.md) | On a stopped world, a run of mod changes shares the snapshot from before its first change; a launch, any other file change or any doubt ends the run and the next change snapshots again. |
| [0015](0015-resource-amounts-by-one-off-count-run.md) | Resource amounts come from a one-off control-mod count run copied from the territory run, grouped in Lua (ores at a gap of 6, or 2 in a dense count over 400,000 ore tiles, Fulgora's scrap at 16, fluid-product wells within 24 tiles), one planet per run, with the control-mod code shared in `controlmod.go`. |
| [0016](0016-run-state-against-stale-looks-and-early-stops.md) | The monitor drops a Docker look taken before or during an operation (a per-world operation count), and Start reads the start's container log to tell a failed load from a plain stop when the container check sees the stop first. |
| [0017](0017-mod-settings.md) | Mod setting definitions come from a one-off control-mod run of the world's own image, cached in memory; map settings on a world with a save are changed in the game, not by Foreman. |
| [0018](0018-webhook-storage-and-body.md) | Webhook addresses are sealed like the factorio.com account, the generic body is versioned from the start, and every target has its own queue so a hung one delays only itself; `http://` is allowed with a warning. |
| [0019](0019-the-scheduler.md) | The restart scheduler is a leaf package that waits for an empty server, then runs a 60-second countdown that always runs and never starts a stopped game; a missed restart is not made up. |
| [0020](0020-language-flags.md) | Each language shows one country's flag in the user menu (the United Kingdom for English, Spain for Spanish), as bundled flag-icons SVG files rather than emoji, decorative with the language's own name as the label. |
| [0021](0021-mod-plan-picks-in-the-database.md) | The mods picked on the Mods tab are saved as names in the app database, not the world folder, so no snapshot is needed and no snapshot, clone or restore carries a plan. |
| [0022](0022-mods-review-answers-in-a-memory-draft.md) | The mods review page keeps its answers in an in-memory draft under a random token tied to the user and the world (20 per user, 200 in all, 2 hours), not in the address or a database table; a lost draft is rebuilt from the saved picks. |
| [0023](0023-a-stop-catches-up-before-it-plans.md) | A stop, restart or any other operation that stops a game first takes the monitor's missed look at a restart Docker made, then asks RCON whether the game is in game, so a game back in game is warned and saved; reading the log alone was rejected because the save has no log follower to wait on. |
| [0024](0024-api-token-format-and-storage.md) | An API token secret is `fmn_` plus 32 random bytes, stored only as its SHA-256 and looked up by hash; renewing keeps the secret, so a leaked token is revoked, never renewed. |
| [0025](0025-mcp-inside-web-one-server-per-scope.md) | MCP lives in package `web` and calls the page handlers' methods, with one SDK server per scope in stateless JSON mode, and any `Origin` on `/mcp` refused unless it is exactly `FOREMAN_PUBLIC_URL`'s. |
| [0026](0026-mcp-confirm-codes.md) | MCP confirm codes are kept in memory, bound to the token, the tool, the hash of the arguments and the hash of the state the preview saw, spent by any use including a refusal, and checked again on confirm; a client-echoed preview hash and a generic wrapper were rejected. |
| [0027](0027-oidc-users-by-subject.md) | An OIDC user is matched by issuer and subject, never by name; a header-made user with no password and no link is linked once by name on first sign-in, an accepted risk (operators turn off renaming at the provider), and "never link" and "admin approval" were rejected. |
| [0028](0028-one-navigation-pattern.md) | One navigation pattern: a product rail on the left lists the four areas and, under the current one, its own pages; no page has an in-page section list, and a page too long to read whole is split into sub-pages there; top tabs and per-page section rails were rejected. |
| [0029](0029-rest-api-translates-mcp.md) | The REST API hands each call, in process, to the same per-scope MCP SDK server, with shared token checks and limits, its own on/off switch, 422 for every tool refusal and a v1 compatibility promise; a registry wrapper, resource-style routes and one call route were rejected. |
| [0030](0030-reading-a-save-in-a-benchmark-run.md) | A save is read (its players and its factory map) by running the world's image in benchmark mode for 8 ticks in a one-off container with no network, the save hard-linked read-only and a control-only helper mod writing JSON; a throwaway server with RCON, a killed server and reading `level.dat` were rejected. |
| [0031](0031-world-file-format.md) | A world file is one ZIP of the 5.1 world layout plus `foreman-world.json`, written at the lowest format version that holds it, with `rconpw`, game and factorio.com secrets, `imported_from`, `archived` and `players.json` left out. |
| [0032](0032-where-save-reads-keep-their-results.md) | The players record is the world file `players.json`, so snapshots, restores and clones carry it; factory map pictures live in `factorymaps/<world-id>/` outside the world; the read's game writes only to a 1 GiB in-memory folder; the database, the world folder and an on-disk write folder were rejected. |
