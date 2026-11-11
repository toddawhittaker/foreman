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
- Ores join within 6 tiles: touching only broke Fulgora's scrap into 3,301
  patches, and at 2 tiles Fulgora still had 946 ore patches at 3072 (578
  under 10 entities) against 296 (48) at 6, measured on 2.1.24 at seed
  123456. On Nauvis no two separate fields join at any gap up to 16. The
  gain levels off at 5 to 6; 8 barely helps Fulgora and joins more
  Vulcanus pieces. Cost at 3072: Vulcanus 10.7 s to 16.4 s, the others
  within 3 s, peak memory unchanged. Wells are resources whose mineable product is
  a fluid, not those flagged infinite (lithium brine is finite), and join
  within 24 tiles: 12 split real fields.
- Fulgora's scrap alone joins within 16 tiles, from a table in the Lua
  keyed by planet and resource. Measured on 2.1.24 at 3072 tiles with scrap
  frequency 600%, seeds 123456 and 977648065 (134 and 150 islands): a gap
  of 2 gave 2,234 and 2,436 scrap patches, up to 80 on one island; 12 gave
  352 and 369, with a quarter of islands still over three labels; 16 gave
  296 and 303, with 84 to 86% of islands at one to three; 24 gave 209 and
  204 but took 36 to 38 s. Any gap from 4 up lets a few labels join
  neighbouring islands across thin channels (4 to 6 of about 300 at 16).
  Gaps above 4 are bucketed by cells as wells are, which keeps 16 at 20 s
  and 675 MiB with the same patches. Rejected: 16 for every ore (it would
  merge real, separate patches on the other planets); grouping scrap by
  island (exact, but the Lua would have to read every ground tile); 12
  (too many labels left per island).
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
