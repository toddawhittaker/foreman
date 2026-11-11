# 0015. Counting resources in a one-off control-mod run

- **Status**: Accepted
- **Date**: 2026-11-09
- **References**: SPEC.md sections 5.6, 14.1, 14.3 and 14.6; ADR 0011

## Context

The large map view should label each resource patch with its exact amount,
which only the game knows. The preview container has no network, RCON
client or exec (14.3), and a 3072 tile Nauvis has very many resource tiles.

## Decision

- Copy ADR 0011: a third preview container (`foreman-resources-<folder>`)
  runs one `--create` with a control-only mod that writes JSON. Same slot,
  cleanup and limits; no new Docker call. The mod-running code is one
  `controlmod.go`, shared with territories.
- Group in Lua, one resource at a time, keeping a key, an amount and a
  parent per tile; the file is under 100 KB. Rejected: Go grouping of a
  dump of every tile (10 to 30 MB).
- Ores join within 2 tiles (a gap of one): touching only broke Fulgora's
  scrap into 3,301 patches. Wells are resources whose mineable product is
  a fluid, not those flagged infinite (lithium brine is finite), and join
  within 24 tiles: 12 split real fields.
- One planet per run, kept per planet with the preview (five would not fit
  60 s), and apart from territories.
- A held, cancellable request after the picture, with an indeterminate bar.
  Rejected: a background job with a true percentage (a job store, polling).
- Labels show the amount only, in compact form; a text list names the
  resource. Rejected: "Iron 1.2M" (twice as wide).

## Consequences

Every default run fits (at most 757 MiB and 33 s at 3072 on 1.1.110, 2.0.77
and 2.1.24), so there is no size cap and no lean run; both were the
fallbacks. Nauvis at 600% runs out of memory on 2.x and fails safe. The Lua
is proved only in a real game, against a scan of every tile; the file's map
colour lets that test check labels against the picture.

Downloads draw only counts already held and never start a run. Their
labels must read as the page's `Intl.NumberFormat` compact and percent
output, which Go has no equivalent for, so `internal/web/compact.go` holds
a table of the CLDR patterns for the eight languages, pinned by a test of
literals pasted from Node's `Intl` (the same ICU data). A new language needs a row
there, and a CLDR change in browsers can make the two drift until the
table is updated.
