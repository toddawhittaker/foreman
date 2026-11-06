# Epic 6: Languages

Foreman's web UI in eight languages, with a check that keeps new
English-only text out (issue #87). Branch `epic/6-i18n`, cut from main
42fed1c. The architect designed it on 2026-11-05; the design is
summarised in the rulings below.

## Rulings

Todd, 2026-11-04 (in #87):

1. Languages: English, German, Russian, Simplified Chinese, French,
   Spanish, Polish and Czech.
2. Translations are made by AI, with a glossary so terms such as world,
   snapshot and mod stay consistent.
3. Numbers, dates and plurals follow each language's CLDR (Common Locale
   Data Repository) rules. A message that counts something has every
   plural form its language needs.
4. Text relayed from Factorio stays as the game wrote it.

Todd, 2026-11-05:

5. The language is the best match for the browser's Accept-Language
   header. A language menu on every page overrides it with a cookie.
   Nothing is stored per user.
6. Everything the web UI shows is translated, including messages built in
   Go (pre-start problems, refusals, failure texts). Server logs, audit
   entries as stored, and API and MCP (Model Context Protocol) error text
   stay English.
7. Every new user-facing string goes through the catalogue, and
   `make verify` enforces it.
8. SPEC 18 is renumbered: 6 Languages, 7 Generate and game settings,
   8 Mods, 9 Live operations, 10 AI access, 11 Release.
9. The library is a small in-repo package, `internal/i18n`, over embedded
   JSON catalogues, using the Go team's `golang.org/x/text` for language
   matching, plural rules and number formats. `gotext` was rejected
   because it cannot read templates and generates code; `go-i18n` because
   it is a third-party copy of what `x/text` already does.
10. Each language addresses the user the way software in that language
    usually does (for example du in German, vous in French, вы in
    Russian, neutral wording in Polish and Czech). The glossary records
    the rule per language.
11. The Languages requirements are P0.

Orchestrator rulings:

- Gate B (19.2) needs three portal mods, so it moves to after Epic 8.
  Gate C moves to after Epic 11. Decision D10 moves to Epic 10.
- Catalogue keys are stable dotted ids (`server.start.button`), not
  English sentences. Values hold named placeholders (`{world}`); a plural
  value is an object of CLDR categories using the `count` argument.
  Catalogues never hold HTML. Files are
  `internal/i18n/locales/<lang>/<area>.json`, with the areas `common`,
  `accounts`, `server`, `worlds` and `js`, so each task owns its own
  area files in all eight languages.
- `translated.json` records a short hash of the English text each
  translation was made from; a test fails when English has changed since.
- The cookie is `foreman_lang`, holding a BCP 47 language tag (for
  example `zh-Hans`): path `/`, SameSite Lax, HttpOnly, one year. The
  menu posts to `/language`, which accepts only the eight tags and
  returns through the existing return-path check (SPEC 14.17).
- View models carry `i18n.Msg{Key, Args}` instead of English strings, so
  a raw string cannot reach a page without a compile error. The domain
  packages (`internal/world`, `internal/game`) keep English `Error()`
  text for logs and gain typed errors or fields where the web needs
  data. They never import `internal/i18n`.
- During the epic, `i18n.Raw(s)` wraps a string not yet converted. W1a
  adds it so the shared types can change in wave one; wave two removes
  every use; the fold deletes it.
- Sizes and durations are formatted per language on the server. Times
  stay UTC without JavaScript (SPEC 17.11); `time.js` formats them in the
  page's `lang`. JavaScript gets its strings from a JSON block that
  `layout.html` renders.
- Text that is the game's own (log pane, console output, crash reason)
  is marked `lang="en"`. Names people typed are shown as written.
- Automatic snapshot labels are shown translated from their stored
  reason code; a manual snapshot shows the user's label.
- End-to-end tests run with the browser locale fixed to `en-US`. The
  existing English selectors stay; they prove English is unchanged. New
  and touched tests look up text through `e2e/tests/i18n.ts`.

## Draft SPEC text

Task D adds this as SPEC section 22.

> 22.1. Foreman's web UI is available in English, German, Russian,
> Simplified Chinese, French, Spanish, Polish and Czech. (P0)
>
> 22.2. The language is the best match for the browser's
> Accept-Language, else English. A language menu on every page,
> including sign-in, overrides it for that browser with a cookie;
> nothing is stored per user. The page's `lang` attribute names the
> language shown. (P0)
>
> 22.3. Everything the web UI shows is translated, including problems,
> refusals, failures and results built by the server, and text shown by
> the page's scripts. Server logs, audit entries as stored, and API and
> MCP errors stay English. Text relayed from Factorio and anything a
> person typed (world names, snapshot labels, player names) is shown as
> written. (P0)
>
> 22.4. Numbers, sizes, dates and plurals follow each language's CLDR
> rules. A message that counts something has every plural form its
> language needs. A message is never assembled from translated
> fragments. (P0)
>
> 22.5. Every new user-facing string goes into the catalogue in all
> languages in the same change. `make verify` fails on hard-coded text in
> a template, script or stylesheet; a key missing from any language; an
> unused key; mismatched placeholders or plural forms; and a translation
> made from English text that has since changed. (P0)
>
> 22.6. Translations are machine-made with the glossary in docs/I18N.md,
> so terms stay consistent and follow Factorio's own translations where
> it has them. A second pass reviews each before its epic merges. (P0)
>
> 22.7. Every page meets 17.1 and 17.2 in every language, including at
> 320 px in the languages with the longest text. (P0)

## Tasks

| Task | Owner, model | Files it may touch | Closes |
|------|--------------|--------------------|--------|
| D. Docs (wave 1) | builder, Sonnet high | `docs/SPEC.md` (new 22; 18 renumbered; 4.13, 19.2, 19.3, 20); `docs/VISION.md`, `docs/DESIGN.md` (epic numbers; a paragraph in 9 pointing to I18N.md), `docs/STACK.md` (epic numbers; `x/text` in 1 and 12), `docs/adr/0006-*.md` status lines, `README.md` (epic number; translations are AI-made, corrections welcome), `docs/STATUS.md` (epic numbers, renumbering note), `e2e/tests/click-budgets.spec.ts` (comments only), `CLAUDE.md` (one line for I18N.md); new `docs/I18N.md` (glossary, address rule per language, do-not-translate list, writing rules, how to add a string) | rulings 8, 10, 11 |
| W1a. Foundation (wave 1) | builder, Opus low | new `internal/i18n/` (package, `locales/*/common.json` and `accounts.json`, `translated.json`, checks); `go.mod`, `go.sum`; `internal/web/render.go` (per-language templates, functions `t`, `tLinked`, `msg`, `bytes`, `num`, `duration`, `lang`; shared `Problem` takes `i18n.Msg`; `WorldsPage` moves unchanged to `worlds.go`); `server.go` (locale middleware, `/language`); `layout.html` (lang, menu, JSON block, skip link, title); `signin.html`, `setup.html`, `signedout.html`, `error.html`, `accounts.go`, `identity.go`; elsewhere only wrapping with `i18n.Raw` and the move; their tests; `Makefile` (hash refresh target) | rulings 5, 7, 9 |
| W2a. Server pages (wave 2) | builder, Opus low | `server.html`, `stop.html`, `forcestop.html`, `switch.html`, `update.html`, `rollback.html`, `restore.html`, `console.html`, `versions.html`; `serverpage.go`, `serverresults.go`, `game.go`, `switch.go`, `update.go`, `update_pages.go`, `console.go`, `live.go`; typed errors in `internal/game/*.go` and `internal/world/precheck.go`; `locales/*/server.json`; their Go tests | ruling 6 (server area) |
| W2b. Worlds and snapshots (wave 2) | builder, Sonnet high | `worlds.html`, `world.html`, `saves.html`, `snapshots.html`, `allsnapshots.html`, `snapshotdelete.html`, `snapshotbulkdelete.html`, `delete.html`, `deleted.html`, `import.html`; `worlds.go`, `snapshots.go`, `snapshots_all.go`, `snapshots_bulk.go`, `saves.go`, `import.go`, `upload.go`, `errors.go`, `sweep.go`; other domain errors in `internal/world/*.go` (not `precheck.go`); `foreman.css` (`content` labels become `attr(data-label)`); `locales/*/worlds.json`; their Go tests | ruling 6 (worlds area), CSS labels gap |
| W2c. Scripts (wave 2) | builder, Sonnet high | `internal/web/static/*.js`, new `internal/web/jsstrings.go`, `locales/*/js.json` | ruling 6 (scripts) |
| W2d. Language tests (wave 2) | tester, Sonnet high | new `e2e/tests/i18n.spec.ts`, `e2e/tests/i18n.ts`; `e2e/playwright.config.ts` (locale) | SPEC 22.2, 22.4, 22.7 |
| W3. Translation review and fixes (wave 3) | two reviewers, Opus low (de fr es pl; ru cs zh-Hans), then builder Sonnet high for `locales/*`; ui-designer Sonnet high for `foreman.css` if long words break a layout | `internal/i18n/locales/*`, `foreman.css` | SPEC 22.6, 22.7 |
| F. Fold (last) | builder, Sonnet high | `docs/SPEC.md`, `docs/STATUS.md` (this epic), `docs/STACK.md` (the pinned `x/text` version), an ADR for the catalogue design, delete `i18n.Raw` and this plan | `Fixes #87` |

W1a's checks (SPEC 22.5) start with a pending list of templates and Go
files not yet converted. Each wave-two task empties its part of the list;
the fold confirms it is empty.

## Waves and checks

Wave 1: D and W1a in parallel. Wave 2: W2a, W2b, W2c and W2d in
parallel from W1a's verified head (W2d's long-word checks run against
whatever is converted and are re-run after wave 2 lands). Then
code-reviewer, security-reviewer (the cookie, `/language`, the return
path) and a11y-reviewer on the epic head, alongside W3. The real-game
check (`make test-game`, `make e2e-game`) runs because W2a changes how
game failures are returned: a failed start, switch and update must
still report the right step, in English and in one other language.
Then F and the epic PR to main.

## Left out

- Right-to-left layouts (none of the eight languages needs them).
- A per-user stored language (ruling 5 chose a cookie).
- Translating docs, the README and the operator guide.
- Rewriting the existing English end-to-end selectors.
- Server-side time formatting per language (the browser does it).
- Translated audit entries; the future audit viewer (SPEC 12.9) shows
  them in English, by ruling 6.
