# Fixes batch 11 plan

Branch `epic/fixes-11`, cut from origin/main 4e0678e. Closes #599, #615,
#616, #618, #619, #620, #621, #622, #623, #624, #625 and #626. Serves SPEC
5.6, 5.11, 5.12, 6.10, 6.12, 7.11, 8.7, 9.1, 9.2, 9.6, 9.8, 9.14, 9.15,
11.1, 14.3, 14.6, 17 and 22, with DESIGN 8.11 and new DESIGN sections for
uploads and favorites.

## Rulings

Todd, 2026-11-15:
1. All twelve issues are in this batch.
2. Fork saving (`non_blocking_saving`) ships in the form. If the real-game
   check finds the snapshot does not wait for a forked save, that is a bug
   fixed in this batch.
3. Autosave slots: Foreman writes `autosave_slots: 20` whenever it creates
   `server-settings.json` (Generate, Import, a world's first settings
   save). An older world whose file lacks the key shows "Game default: 5"
   and a hint that Foreman's default for new worlds is 20.
4. Defaults show in every unset field of the game settings form. SPEC 17
   gets the rule; S2 files one issue listing every other form that breaks it.
5. An upload happens in place when its result is used on that page;
   otherwise a button (never a link) goes to the tab that owns the file.
6. Pages show titles; saved records (snapshot labels, audit entries, sent
   notifications) keep the slug they were written with.
7. The exchange string is offered on the world's page and the favorites
   list, for every world, imported ones too (ruling 19).
8. A favorite file is a zip of the favorite's folder plus a format version.
   `mod-settings.dat` travels with it, with a visible warning that a mod may
   keep a token in it. A missing mod or game version is still imported and
   shows as a conflict on review.
9. Generating from a favorite passes the favorite's id, not its mod names.
   The saved-picks cap rises from 50 to 500.
10. No long addresses anywhere: state travels by id or a server-side
    record, never as a list in the query string. SPEC 17 gets the rule.
11. Docker events are optional. Foreman uses them when the socket proxy
    allows them and polls as today otherwise; the operator guide recommends
    `EVENTS=1`.
12. An imported world's string: when reading the save does not match the
    game, a throwaway container is the fallback (ruling 19).
13. The mods review page keeps its answers in a server-side draft in
    memory (ruling 20).
14. Snapshot bulk delete posts its selection; the result shows a count, not
    the names (ruling 21).

Already ruled in the issues:
15. #623 is the rule; #619 and #620 apply it to two screens. A title comes
    from `info.json` for installed mods and from the portal otherwise, with
    the slug as fallback. It links to the portal in a new tab, except for
    built-in mods, folder mods and mods the portal lacks. Titles are
    untrusted: escaped, length-limited, `translate="no"`. On the Installed
    list the name is already the link (`mods.html:210`).
16. #626 form groups as the issue lists them. `auto_pause_when_players_connect`
    is hidden on 1.1, and the schema fix adds it to 2.0. Fork saving carries
    the note about the experimental flag and memory (SPEC 8.7), and is
    disabled off Linux.
17. #625: names are shown, not changed on disk. Downloads are named with
    the world-name slug plus the save time. #621: a Favorites button beside
    Generate and Import.

Orchestrator:
18. Docker's operating system comes from `GET /version`'s `Os` field, which
    Foreman already calls. SPEC 14.3 does not change for it. Shared template
    partials: `mod-name` in `templates/modportal.html`; the drop zone in a
    new `templates/dropzone.html` with a new `static/dropzone.js`, loaded
    through each page's `scripts` block. No one edits `layout.html`. Reuse
    `portalPath` (`mods.go:301`), the `portal-link` partial, `attachment()`
    and `previewFileName()` (`genpreview.go`), `time.js`, the `when`
    partial, `copy.js`, the favorite reader (`internal/world/favorite.go`),
    `internal/mapexchange`'s decoder, and `internal/savefile`'s zip and
    header code.
19. An imported world's string (X3): first read the save's `level-init.dat`
    and `level.dat0` in Go, checked against the game's own
    `LuaSurface.get_map_exchange_string()` on all three lines. If they do
    not match byte for byte, use a throwaway container in the territory-run
    pattern (SPEC 14.3) that loads a copy of the save and writes that call's
    result. RCON is never used: a Lua command turns achievements off for
    good.
20. Review page state (R1): answers live in a draft, in memory, under a
    random token tied to the user and the world, like SPEC 11.3's previews.
    An expired draft is rebuilt from the saved picks, and the page says so.
    Entry links carry only the kind and one name, `all` for Update all, or
    a favorite's id.
21. Snapshot lists (L2): the bulk-delete selection is sent as a POST, and
    the result says how many snapshots were deleted. The audit log still
    names each one.
22. #620's optional mods use the plan's portal lookups, then the cache, with
    no more lookups than the plan's cap; otherwise title and link only.
23. Tasks put proposed SPEC text in their PR body; the fold writes SPEC.
    Before the fold, U1 alone writes DESIGN's upload section and P1 alone
    writes its favorites section.

## Long addresses today (rule 10)

Fixed by R1: `mods.go:66-76` (`planPath`, `kindPlanPath`), `953-990`
(`planForm.query`), `993` (`answers`), `1056-1062` (remove links),
`1138-1143` (upload address), `1203` (account return), `1438, 1500, 1693`
(retry), `427` (update with a version); `modplanrows.go:119-122`;
`modplan.html:214` (no-script redraw); `modgone.go:29` (Update all);
`generate.go:405-417` (favorite to review); `modplanpanel.go:384`.

Fixed by L2: `snapshots.html:54`, `allsnapshots.html:17`,
`snapshots_bulk.go:301`, `snapshots_all.go:283, 298`.

Left as they are (one short value each): account and notify notes, the
favorite id, the player name, game version, search and page, the versions
filter.

## Hot files: who owns what

| File | Owner, in order |
|---|---|
| `foreman.css` | Wave 1: M1, U1, S2, T3, each in its own block. Then M2 and P1, then U2, then X2. M3, R1, L2, X1, X3, O1, S1 edit none. |
| mods locales (8 + `translated/mods.json`) | M1, then M2, then U2, then R1. M3 adds no `mods.*` keys; if it needs one, it stops and reports. |
| worlds locales | T3, then U2, then L2 |
| settings locales | S2 |
| common locales | U1 |
| server locales | M3 |
| favorites locales | P1, then X2, then R1 |
| mapgen locales | X2 |
| `web/mods.go`, `modplan.html`, `modplanrows.go` | M1, then M2, then U2, then R1 |
| `web/favorites.go`, `favorites.html` | P1, then X2 |
| `web/generate.go`, `modgone.go`, `modplanpanel.go` | M1 (panel), M3 (`modgone`), then R1 |
| `internal/world/favorite.go` | P1, then R1 (read only, if needed) |
| `web/worlds.go`, `world.html` | T3 (worlds.go only, if needed), then X2 |
| `internal/mapexchange/` | X1, then X3 (new files only) |
| `internal/docker/` | S2 (`client.go`); O1 (new `events.go`) |
| `internal/game/` | S1 (`live.go`); O1 (`monitor.go`); S2 at most one new file |
| `internal/store/modplans.go` | R1 |
| snapshots handlers and templates | L2 |
| `layout.html`, e2e helpers | nobody; a needed change stops the task and is reported |

## Invariants every task tests

- 320 px at 200% text in German and Russian; dark and light; axe clean.
- Keyboard, visible focus, a 44 px target on a phone, and a working page
  without script.
- Every new string is in all eight languages, with the second i18n review
  (SPEC 22.6).
- No page shows a slug where ruling 15 wants a title. No address Foreman
  builds is over 512 bytes (R1 and L2 each add a test for their pages).

## Tasks

| # | Issues | Owner, model | Files | Done when |
|---|---|---|---|---|
| M1 | #623 core, #619 | ui-designer, Opus low | `world/modversion.go` + test, `internal/mods/` where rows carry ModInfo, `web/mods.go`, `modhits.go`, `modplanpanel.go`, `mods.html`, `modportal.html`, css block, mods locales, `e2e/tests/mods-*.spec.ts` | Installed list, update lines, More menus, plan panel and phone bar show linked titles; fallback and exceptions unit-tested. |
| M2 | #620 | ui-designer, Opus low | `modplan.html`, `modplanrows.go`, `mods.go` (review), css block, mods locales, modplan e2e | Name is the link; version drop-down in the row; remove icon "Remove *title* from the plan"; optional, recommended and orphan boxes show title, summary, version and date; the link never toggles the tick box. Keeps the redraw in one function R1 can replace. |
| M3 | #623 other pages | builder, Sonnet high | `modsettings.html`, `modsettings.go`, `server.html` (`mods-notice`), `serverresults.go`, `failure.html`, `modgone.go`, server locales, their e2e | Those pages use `mod-name`; saved records per ruling 6. |
| U1 | #622 component | ui-designer, Opus low | new `dropzone.html`, new `dropzone.js`, `import.html`, `import.js`, css block, common locales, DESIGN upload section, new `dropzone.spec.ts`, import specs | Import uses the drop zone; drop, click, keyboard, wrong type, too big, clear and no-script tested; server checks unchanged (SPEC 14.6). |
| U2 | #622 everywhere | ui-designer, Sonnet high | `saves.html`, `mods.html`, `modplan.html`, `mods.go` (upload views), css, worlds and mods locales, their e2e | Every upload uses the drop zone and ruling 5; no bare links. |
| S1 | #626 save check | builder, Opus low, 90-min box, then fix | `game/live.go`, `game/*_test.go`, a new `*_game_test.go` | Real container on 2.0.77 and 2.1.24 with fork saving: findings in the PR body; any wait that falls short is fixed, with a test that fails first. |
| S2 | #624, #626 form | builder, Sonnet high | `settings.html`, `settingsform.go`, `settings.go`, `gamesettings/schema.go` + test, `docker/client.go` + test, one new file for the OS check, settings locales, css block, settings e2e, the world-creation paths that write `server-settings.json` | Rulings 3, 4, 16 and 18; issue filed for the other forms. |
| T3 | #625, #621 | builder, Sonnet high | `web/saves.go` + test, `saves.html`, `worlds.html`, `worlds.go` if needed, worlds locales, css block, saves and worlds e2e | Readable save names in the viewer's zone; slugged downloads; Favorites button. |
| X1 | #618 encoder | builder, Opus low | new `mapexchange/encode*.go` + tests | Encoders for 1.1, 2.0 and 2.1 from map files; encode then decode round-trips; real-game strings in testdata re-encode equal. |
| X3 | #618 from a save | builder, Opus low, 2-hour box on the Go route | new files in `internal/savefile/` or `internal/mapexchange/`, new testdata, a new `*_game_test.go`; the fallback container, if needed, in new files only | Ruling 19: the game's own strings for the testdata saves recorded as fixtures; the chosen route matches them on all three lines; route and reasons in the PR body. |
| X2 | #618 screens | ui-designer, Sonnet high | `world.html`, `worlds.go`, a new `web/mapexchangeexport.go` + test, `favorites.html`, `favorites.go` (button), css block, mapgen and favorites locales, a new e2e | Ruling 7; copy button; note that mods are not included. |
| P1 | #599, #623 on favorites | builder, Opus low | `world/favorite.go`, a new `world/favoritefile.go` + tests, `web/favorites.go`, `favorites.html`, css block, favorites locales, DESIGN favorites section, favorites e2e | Ruling 8; import through the drop zone validates (8 MiB cap, unknown fields, untrusted names and versions) before writing; lists show titles. |
| R1 | #615, rule 10 (mods) | builder, Opus low | `web/mods.go`, `modplanrows.go`, `modplan.html`, `modgone.go`, `modplanpanel.go` (return only), `generate.go`, a new `web/plandraft.go` + test, `store/modplans.go`, mods and favorites locales, modplan e2e, a new address-length test | Rulings 9 and 20; every address in the R1 list is under 512 bytes; a 120-mod favorite installs in one review; drafts expire safely. |
| L2 | rule 10 (snapshots) | builder, Sonnet high | `snapshots.html`, `allsnapshots.html`, `snapshots_bulk.go`, `snapshots_all.go` + tests, worlds locales, snapshot e2e | Ruling 21; works without script; snapshot addresses under 512 bytes. |
| O1 | #616 | builder, Opus low | new `docker/events.go` + test, `game/monitor.go` + test, `docs/OPERATOR.md` | Ruling 11; an out-of-memory kill on start is named every time with events on, and polling still works with `EVENTS=0`. |
| F | fold | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md`, `docs/DESIGN.md` 8.11, this plan (deleted) | STATUS from PR bodies; SPEC 5.6, 5.12, 6.12, 9.x, 11.1, 14.3, 17 updated; `Fixes` lines for all twelve. |

## Waves

1. From 4e0678e: M1, U1, S1, S2, T3, X1, O1.
2. M2 and M3 from M1's head; P1 once M1 and U1 land; X3 once X1 lands.
3. U2 once M2, T3 and U1 land; X2 once P1, X1, X3 and T3 land.
4. R1 and L2, in parallel, once U2 lands.
5. Integration review, then F.

Reviews: code-reviewer on every task; a11y-reviewer on every UI task;
security-reviewer on M1, U1, U2, P1, S2, O1, X3 (a save is untrusted
input), T3, X2 and R1 (draft tokens). The integration review covers
titles, uploads, favorites and short addresses. S1 and O1 are reviewed
together after both land, since both change how a start watches the game.

## Real-game checks

- S1: the fork-save wait. S2: a start on 1.1.110, 2.0.77 and 2.1.24
  reaches "InGame" with 20 slots and each new key set.
- O1: an out-of-memory kill on start is named in 20 of 20 runs, with
  events on and with them off.
- X3: the game's own strings for the testdata saves, and the fallback
  container if it is built.
- X1 and X2: Todd pastes one string into the game client at the test drive.
- Epic head: `make test-game`.
