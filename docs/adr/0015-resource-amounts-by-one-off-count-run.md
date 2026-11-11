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
- Ores join within 6 tiles. Measured on 2.1.24 at 3072 tiles and seed
  123456, 2 tiles left many crumbs: Nauvis 138 ore patches (22 under 10
  entities) against 112 (4) at 6, Vulcanus 338 (118) against 228 (45),
  Gleba 280 (68) against 235 (28). On Nauvis no two separate fields join
  at any gap up to 16. The gain levels off at 6: 8 gave 110 (2), 213 (33)
  and 234 (27) but took Nauvis to 42 s and Vulcanus to 46 s. Cost of 6 against
  2: Vulcanus 27.5 s to 32.9 s, Nauvis and Gleba within 3 s, peak memory
  unchanged. Aquilo has only wells and Fulgora only scrap, so the ore gap
  does not change them. Wells are resources whose mineable product is
  a fluid, not those flagged infinite (lithium brine is finite), and join
  within 24 tiles: 12 split real fields.
- A dense count, with more than 400,000 ore tiles in the picture (the
  ores that would join at 6, counted with `count_entities_filtered` before
  any grouping), joins its ores within 2 tiles instead, so its patch list
  is finer (Todd's decision, 2026-11-11). Measured at 3072 tiles, seed
  123456, on 1.1.110, 2.0.77 and 2.1.24: the most ore tiles of a default
  map are Vulcanus's 141,912, and Nauvis with every resource at 600% has
  1,097,357 to 1,133,683; 400,000 is 2.8 times the one and 2.7 times
  below the other; TestDenseOreTiles keeps both at 2.5 times or more. Between them, Nauvis at 200% has about 250,000 and counts
  at 6 in 38 s; at 300% (about 450,000) it took 53 to 57 s at 6, at 400%
  (about 690,000) and 600% it ran past the 60 s limit at 6 while memory was
  still under the cap, so the page said to try again. With the fallback,
  600% runs out of memory at 28 to 32 s on 2.x and the page says to choose
  a smaller size, as SPEC 5.6 promises; 300% to 500% finish in 26 to 38 s
  at up to 988 MiB (300% gives 512 patches, against 344 at 6), and on
  1.1.110 even 600% finishes, in 37 s at 1,014 MiB. Rejected: a lower limit for every count
  (the default maps would lose the gap of 6); keeping 6 and calling a
  timeout out of memory (a slow server times out on a map that fits).
- Fulgora's scrap alone joins within 16 tiles, from a table in the Lua
  keyed by planet and resource, so the ore gap never applies to it. Scrap
  is Fulgora's only resource: at the defaults and 3072 tiles Fulgora has
  240 patches (23 under 10 entities) on 2.1.24 and 237 (18) on 2.0.77 at any
  ore gap. Touching only broke scrap into 3,301 patches. Measured on 2.1.24 at 3072 tiles with scrap
  frequency 600%, seeds 123456 and 977648065 (134 and 150 islands): a gap
  of 2 gave 2,234 and 2,436 scrap patches, up to 80 on one island; 12 gave
  352 and 369, with a quarter of islands still over three labels; 16 gave
  296 and 303 (296 and 306 on 2.0.77), with 84 to 86% of islands at one to three; 24 gave 209 and
  204 but took 36 to 38 s. Any gap from 4 up lets a few labels join
  neighbouring islands across thin channels (4 to 6 of about 300 at 16).
  Gaps above 6 are bucketed by cells as wells are. Per-tile offsets look
  up every position in reach, 84 a tile at 6 and 544 at 16, while cells
  compare only the tiles present, about 160 a tile in a dense field at 6
  but few in sparse scrap; cells keep 16 at 33 s and 675 MiB with the same patches. Rejected: 16 for every ore (it would
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

Every default run fits (at most 760 MiB and 36 s at 3072 on 1.1.110, 2.0.77
and 2.1.24), so there is no size cap and no lean run; both were the
fallbacks. Nauvis at 600% is a dense count: it runs out of memory on 2.x
within about 30 s and fails safe, and on 1.1 it fits with about 10 MiB to
spare. A dense count's patch list is finer than a default map's. The Lua
is proved only in a real game, against a scan of every tile; the file's map
colour lets that test check labels against the picture.

Downloads draw only counts already held and never start a run. Their
labels must read as the page's `Intl.NumberFormat` compact and percent
output, which Go has no equivalent for, so `internal/web/compact.go` holds
a table of the CLDR patterns for the eight languages, pinned by a test of
literals pasted from Node's `Intl` (the same ICU data). A new language needs a row
there, and a CLDR change in browsers can make the two drift until the
table is updated.
