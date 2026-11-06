# 0009. Decode the map exchange string in Go

- **Status**: Accepted
- **Date**: 2026-11-06
- **References**: SPEC.md sections 5.6 and 14.6, STACK.md section 12

## Context

Generate (SPEC 5.6) takes a map exchange string. The game's `--create` step
reads only `map-gen-settings.json` and `map-settings.json`, so Foreman must
turn the string into those files, and refuse a bad one on the form before
any world exists. Wube does not document the binary layout. It changed
inside the 2.1 line, at 2.1.13 and again at 2.1.18.

## Decision

- **Foreman decodes the string in Go** (`internal/mapexchange`) for the 1.1,
  2.0 and 2.1 lines. It is a port of rfvgyhn/factorio-exchange-string-parser
  (MIT licence), whose notice stays in the package.
- It refuses what it cannot read exactly: a bad checksum, left-over bytes,
  an unknown line, an out-of-range value, and a settings layout that differs
  from the world's game version (SPEC 5.6).
- **Golden fixtures**: 98 maps made by the real game on 1.1.110, 2.0.66,
  2.0.77, 2.1.9 and 2.1.24, each with the game's own parse. `make test` runs
  them.
- Rejected: **a throwaway game container** that reads the string and prints
  the settings. The form could answer only once the image was downloaded,
  and each answer would cost a container start.

## Consequences

- A bad string is refused on the form with its version and the reason, with
  no Docker and no image.
- **Risk:** a new game patch can change the layout again. A change that
  breaks the length, checksum or a range is refused, but a silent one could
  read wrong values. Guards: only strings that use every byte are taken, the
  layout is checked against the world's version, and the fixtures are re-made
  when Foreman adopts a new game version. Strings from 2.1.13 to 2.1.17 have
  no real fixture.
- Foreman carries about 900 lines of ported code and a licence notice.
