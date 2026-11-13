# 0020. Language flags: one country each, bundled SVG files

- **Status**: Accepted
- **Date**: 2026-11-12
- **References**: SPEC.md section 22.2; STACK.md section 3; DESIGN.md section 6

## Context

The language row in the user menu is easier to find for someone who landed
in a language they cannot read when it shows a flag beside the language's
own name. A flag stands for a country, not a language, so each of the eight
languages needs one pick. Nothing may be fetched at build or start, and the
flags must look the same on every system.

## Decision

- **One country per language.** English shows the United Kingdom and
  Spanish shows Spain, the countries the languages are named for, chosen
  by the project owner over the United States and Mexico. The rest are
  plain: Germany, Russia, China, France, Poland and Czechia.
- **Bundled SVG files, not emoji.** Flag emoji draw as two letters on
  Windows and in some Linux fonts, and differ between systems. The files
  come from the MIT-licensed flag-icons project at an exact version, are
  stored with their checksums, and are checked by a test.
- **Decorative.** The flags have empty `alt` text; the language name, in the
  language's own words, is what a screen reader hears.

## Consequences

- The row looks the same everywhere and costs a closed menu nothing, since
  the listed flags load lazily.
- The files add about 85 KB to the repository, almost all of it Spain's
  coat of arms.
- A flag can offend or mislead (Spanish is spoken in many countries; English
  in many). That is accepted: the name beside it is the real label. If a
  better single picture is wanted later, only the eight files and the
  table in DESIGN 6 change.
