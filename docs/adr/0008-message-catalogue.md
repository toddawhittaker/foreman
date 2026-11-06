# 0008. Message catalogue for the eight languages

- **Status**: Accepted
- **Date**: 2026-11-05
- **References**: STACK.md sections 1, 12 and 13, SPEC.md section 22

## Context

The UI is shown in eight languages (SPEC 22.1), with text from templates,
scripts and Go. Translations are AI-made (SPEC 22.6), so reworded English
must not leave an old translation looking current. Tasks run in parallel,
so they must not all edit one file.

## Decision

- **A small in-repo package, `internal/i18n`, over `golang.org/x/text`.**
  JSON catalogues are embedded in the binary; `x/text` matches languages
  and gives plural rules and number formats. Rejected: `gotext` (cannot
  read templates, generates code) and `go-i18n` (a third-party copy of
  what `x/text` does).
- **Dotted keys, not English as the key.** `server.start.button` survives
  rewording. The cost is that a key can hide a stale translation, so
  `internal/i18n/translated/<area>.json` records a short hash of the
  English each translation came from. A test fails when English has
  changed; `make i18n-hash` refreshes it.
- **Message ids in view models.** A page field holds `i18n.Msg` (key and
  named arguments), never a string, so a raw string cannot reach a page
  without a compile error. `internal/world` and `internal/game` keep
  English `Error()` text for logs, never import the package, and carry
  facts in typed errors that the web layer words.
- **One catalogue file per area** (`common`, `accounts`, `server`,
  `worlds`, `js`) per language, so parallel tasks edit different files.

## Consequences

- Adding a string is one change in eight languages plus a hash refresh;
  the checks in SPEC 22.5 fail otherwise.
- A hash shows English did not change, not that a translation is good.
  Quality rests on the glossary and the review (SPEC 22.6).
- Keys in Go must be string literals so the unused-key check sees them.
