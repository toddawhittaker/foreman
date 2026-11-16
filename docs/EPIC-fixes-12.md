# Fixes batch 12 plan

Branch `epic/fixes-12`, cut from main 5d7153cb. Closes #636, #646, #656,
#657, #658 and #659. Serves SPEC 5.12, 8.7, 14.6 and 17.16, with DESIGN
7.1, 7.8, 7.10, 7.11, 8.3 and the token tables.

## Rulings

From the issues (Todd, 2026-11-16), followed as written:
1. #656: a link to another tab ends in a chevron drawn in CSS. A link that
   leaves Foreman ends in `↗`, also drawn in CSS, so neither is read aloud.
   The batch 11 rule stands: an upload happens in place only where its
   result is used. Mod titles that link to the portal are links out, so
   they get `↗` too.
2. #657: one foldout style, as the issue's reference CSS sets out. Left as
   they are: `.player-actions`, the row menus and the header menu. For
   `.plan-panel__list`, see question Q1.
3. #658: the closed row, the Details order, the Generate button (worlds
   icon with a plus badge, the word "Generate", and the accessible name
   "Generate a world from {name}"), and Rename with the same name rules and
   errors as Make.
4. #659: the file name goes on its own line under the label, and the label
   uses tabular numbers.

Orchestrator:
5. The CSS class stays `disclosure`. F1 restyles it rather than renaming
   it, so the 16 templates and the shared e2e helpers keep their
   selectors. The Generate planet rows and the raw JSON foldout
   (`mapgenform.html`) gain the class.
6. The `↗` comes from one rule on `a[target="_blank"]`. Every link out
   already opens in a new tab, which covers `extLink`, `portal-link`,
   `mod-title`, `portal-browse` and the search hits. The only exception
   today is the changelog link in `versions.html`, which L1 changes to open
   in a new tab with the hidden "opens in a new tab" text. The chevron is a
   class, `link-tab`, drawn with `--i-chevron` as `.plan-panel__list` draws
   it now. A Go test makes sure every `target="_blank"` link points off
   the site.
7. The chevron goes on the three batch 11 "another tab's upload" links:
   `mods.html:83` (Saves tab), `modsettings.html:64` (Mods tab) and
   `worlds.html:62` (Import page). Each loses `class="button"`. The last
   one gets a new `worlds.*` key, because `worlds.saves.upload.heading` is
   also the heading on Import and Saves.
8. Emergency actions keeps its danger-coloured label. Only the marker is
   orange.
9. A favorite's rename takes the favorites lock (`makeMu`). It refuses a
   name another favorite already has, ignoring case, but allows a change of
   case to the favorite's own name. It rewrites only `favorite.json`, with
   `WriteFileAtomic`. After a rename the page redirects to
   `/favorites?renamed=<id>`, and the message names that favorite's current
   name, as `renameDone` does for worlds. Favorites stay out of the audit
   log, as Make and Delete are today.
10. #646 must end with two guarantees. A Stop or Restart of a game that is
    in game always warns players and saves. A game that is not in game is
    never refused a Stop. The refusal must not tell the user to "stop it",
    as `ErrNotReady` does today. The approach is G1's choice (see its row).
11. Tasks put their proposed SPEC text and STATUS line in the PR body, and
    the fold writes them. F1 alone writes DESIGN 7.8 and the token rows in
    wave 1. P1 alone writes DESIGN 7.11 in wave 2. L1 puts its DESIGN text
    in the PR body.

Todd (2026-11-16):
12. Q1: `.plan-panel__list` gets the orange triangle marker only. It keeps
    its bar layout and 44 px line; the compact size and hover do not apply.
    F1 does it.
13. Q2: add a "Mod default: X" hint, shown only under a mod setting whose
    value differs from the mod's default (bool, number, text, choice and
    colour). L1 does it.

## #636 checked (no real gaps)

| Field | Result | Why |
|---|---|---|
| `modsettings.html` text, number, decimal, choice, checkbox, colour | Already compliant | Every control is filled with the value in effect: the world's file, else the defaults file, else the mod's default (`currentValue`). A text box is empty only when "" is the value, and a setting that cannot be blank is required. A "mod default" note is a separate choice (Q2). |
| `schedule.html` on/off, time, limit | Already compliant | A new schedule's form is filled with Foreman's defaults, and every field is required. No field is ever unset. |
| `notificationform.html` events, language | Already compliant | A new webhook starts with every event ticked and the admin's language chosen. No field is ever unset. |
| `account.html` username, token | Exempt | Write-only secrets (SPEC 11.4), the exception 17.16 already names. |
| `generate.html` seed | Already compliant | Its hint says "Leave blank for a random seed." |
| `mapgenform.html` width, height, map settings | Already compliant | Filled from the preset. An empty box is refused with an error naming the range. |
| `generate.html`, `import.html`, `update.html` version box | Already compliant | The hint says a typed version is used instead of the choice above. |
| `settingsraw.html` | Exempt | It edits the file as text; adding keys the file lacks would change the file. |

## Hot files: who owns what

| File | Owner |
|---|---|
| `foreman.css` | F1: every rule for `details`, `summary` and `.disclosure` (including `.mode-panel details`, `.snap-details`, `.mod-rail`, `.emergency`, `.disclosure--add`, `.console-past`, `.settings-jump`, `.mod-settings-will`, and `.plan-panel__list` per Q1). L1: a new link block directly after `.portal-link` (about line 4560), plus `.save-name` and `.save-file`. P1 (after F1 lands): the favorites block (about line 9265). Nobody appends at the end of the file. |
| `mods.html`, `modsettings.html` (and `web/modsettings.go`), `worlds.html`, `versions.html`, `saves.html` | L1. F1 changes their foldouts through CSS only. |
| `mapgenform.html`, `generate.html`, `design.html`, other foldout templates | F1, only if a class must change |
| `favorites.html`, `web/favorites.go`, `world/favorite.go` | P1 |
| `internal/game/` | G1 |
| mods and worlds locales | L1 |
| favorites locales | P1 |
| other locale namespaces, `layout.html`, `e2e/tests/foreman.ts` | Nobody; a task that needs one stops and reports. |
| `docs/DESIGN.md` | F1 (7.8, tokens), then P1 (7.11), then the fold |

## Invariants every UI task tests

- 320 px at 200% text in German and Russian; dark and light; axe clean.
- Keyboard use and a visible focus ring. 44 px targets under touch
  emulation (`hasTouch: true, isMobile: true`; first assert that
  `matchMedia("(pointer: coarse)")` matches). The phone projects are
  Desktop Chrome at 320 px, so they use the compact mouse sizes.
- The page works without script. Every new string is in all eight
  languages, with the second i18n review (SPEC 22.6).

## Tasks

| # | Issues | Owner, model | Files | Done when |
|---|---|---|---|---|
| F1 | #657 | ui-designer, Opus low | `foreman.css` (its region), `mapgenform.html`, `generate.html`, `design.html`, other foldout templates only to drop an old modifier class, DESIGN 7.8 and token rows, new `e2e/tests/foldouts.spec.ts`, and the specs named here | Every foldout listed in #657 except Favorites uses the one style; the old button look is deleted. `details.disclosure` selectors that now match several elements on Generate (`generate.spec.ts:31`, `large-view.spec.ts:468`, `ui-fixes.spec.ts:483, 523`) are made exact. Existing 44 px summary checks move to the touch context, not weakened. New spec: a lone foldout has no top line; stacked planets have a line between them; the marker is `--color-primary`; opening turns the marker. Run: foldouts, generate, ui-fixes, large-view, planet-rows, resources, generate-preview, account, mods-tab, mods-files, mods-settings, mods-plan, mods-search, server, server-hub, phone, settings, settings-defaults, console, snapshots, snapshots-tab, snapshots-select, refusal-layout, design, click-budgets. |
| L1 | #656, #659, #636 (Q2) | ui-designer, Sonnet high | `foreman.css` (its region), `mods.html`, `modsettings.html`, `worlds.html`, `versions.html`, `saves.html`, `web/modsettings.go` + test (Q2 only), new `web/linkmarks_test.go`, mods and worlds locales, new `e2e/tests/links.spec.ts`, `saves.spec.ts`, `uploads.spec.ts`, `mods-settings.spec.ts` | Rulings 6 and 7. The #656 e2e check (link not button, chevron, opens Saves; one external link shows `↗`; neither marker in an accessible name). The #659 check (every `.save-file` starts at the link's left edge, desktop and 320 px). Q2 per Todd's answer, failing unit test first. Also run: mods-tab, mods-search, mods-plan, account, versions, about, worlds, import, dropzone, favorites, i18n. |
| G1 | #646 | builder, Opus low, 90-minute box | `internal/game/actions.go`, `game/*_test.go`; `monitor.go` only if needed, without changing its behaviour | The issue's unit test is written first and fails on 5d7153cb: the stop plan with a restart count above `seenRestarts` before any look at the container, with the new run in game and not in game. Ruling 10 holds. Approach (a) refresh this world's view inside the stop, reconnecting RCON, then plan; or (b) read the log since `State.StartedAt` and treat the game as running only once RCON reaches the new run. Choice and reasons in the PR body. `TestRestartReachesAGameBackFromACrash` and the crash-loop tests still pass. |
| P1 | #658 | ui-designer, Opus low; starts from F1's verified head | `favorites.html`, `web/favorites.go` + test, `world/favorite.go` + test, `foreman.css` (favorites block), favorites locales, DESIGN 7.11, `favorites.spec.ts`, `map-exchange.spec.ts` | The approved layout from #658, export warning once. Rename per ruling 9, route `POST /favorites/{id}/rename`, a refusal re-renders with that favorite's Details open and the field error. Unit tests: accepted, empty, over 100 characters, another favorite's name, case-only change, missing favorite. E2e: closed row with Generate visible; Details holds facts, mods, exchange string, Rename, Export, Delete; a rename shows in the list and in the export. Also run: worlds, generate, click-budgets. |
| FD | fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md`, `docs/DESIGN.md` (7.1, 7.10, 8.3), this plan (deleted) | STATUS from the PR bodies. SPEC 14.6: "a button goes to" becomes "a link marked with a chevron goes to". SPEC 5.12: favorites can be renamed; nothing else changes. SPEC 17.16: drop the #636 note and name the exemptions above. SPEC 8.7: a stop before Foreman has seen a restart, if G1 changed it. DESIGN 7.1 link rule, 7.10 upload rule, 8.3 saves second line. `Fixes` lines for all six issues. |

## Waves

1. From 5d7153cb: F1, L1 and G1 in parallel.
2. P1, from F1's verified head.
3. Integration review, then full e2e and `make test-game` at the epic head,
   then FD.

Reviews: code-reviewer on every task; a11y-reviewer on F1, L1 and P1;
security-reviewer on P1 (a new POST that writes under `favorites/`).

## Real-game checks

#646 changes stop and restart after a crash, so the epic head runs
`make test-game` before the epic PR. G1's unit test pins the window itself,
which is shorter than one poll.
