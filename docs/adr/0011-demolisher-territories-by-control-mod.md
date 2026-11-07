# 0011. Demolisher territories from an in-game control mod

- **Status**: Accepted
- **Date**: 2026-11-07
- **References**: SPEC.md sections 5.6, 14.1, 14.3 and 14.6; STACK.md section 1

## Context

The map preview of Vulcanus should show the demolisher territories, as the
game does. `--generate-map-preview` does not draw them. Only the game's
Lua API knows them (`get_territories()`). The preview container has no
network, the image has no RCON client, and SPEC 14.3 forbids exec, so
nothing outside the game can ask it. The downloaded PNG needs the labels
in all eight languages, and the Go standard library cannot draw text.

## Decision

- A one-off preview container runs one `--create` with a small mod that
  holds only `control.lua`. Its `on_init` makes the picture's chunks, reads
  each territory's chunks and demolisher, and writes JSON with
  `helpers.write_file`. Each territory already holds its demolisher at
  `on_init`, so no ticks and no server run are needed. `info.json` is built
  per game line, because 2.1 refuses a mod whose `factorio_version` is 2.0.
  A scenario was rejected: `--start-server-load-scenario` may ignore `--preset`.
- Foreman parses the file under limits, as untrusted input. The page draws
  the overlay as SVG; the server draws it into the PNG in Go, with
  `golang.org/x/image`, Go Bold for Latin and Cyrillic, and a committed
  Noto Sans CJK SC Bold subset for the Chinese labels. A unit test checks
  that every language's label glyphs are in the fonts.
- Territories are offered up to 3072 tiles. 4096 ran out of the 1 GiB
  limit on 2.1.24; 3072 peaked at 750 MiB and 27 seconds.

## Consequences

The map is the picture's map: a control-only mod adds no prototypes. The run costs a few seconds more per request, so it starts only
after the picture arrives. One dependency and a 2.5 KB font file are added;
a translation that needs a new Chinese glyph fails `make verify` until the
subset is remade. Generating only every fourth chunk fitted 4096 in 685 MiB
on one seed, but it relies on every territory covering such a chunk, which
is not proven, so it is not built.
