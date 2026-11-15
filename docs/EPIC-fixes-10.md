# Fixes batch 10 plan

Branch `epic/fixes-10`, cut from main 80e1bb7 on 2026-11-15. It covers
#593, #595, #596, #597, #598, #547 (favorites), and the removal of
orphaned dependencies Todd asked for while planning. #132 is out (it
needs a vision amendment). Export and import of favorites is #599, not
built here.

## Rulings (Todd, 2026-11-15, unless marked)

### Mod search and the plan panel (#595, #596, #597)

1. The design is the approved prototype on branch `prototype/mod-search`
   (never merged), with the rulings in #597's body, changed by the
   rulings below.
2. **The panel shows a tree.** Each mod picked with "+" is listed with
   its required dependencies indented under it, clearly labelled as
   needed by that mod. A dependency shared by two picked mods appears
   under each; the "Review and install N mods" count counts each
   distinct mod once.
3. **Only changes show.** Built-in mods are always hidden. A dependency
   already installed at a version that fits is hidden; one that must be
   updated shows "will update to *version*".
4. **Problems show in the panel.** A dependency that cannot be had (gone
   from the portal, no release for this game line) or that conflicts
   with an installed mod shows a warning icon and a short reason on its
   line. Review still opens; the review page offers today's fixes.
5. **Only picked mods are saved.** Foreman saves, per world, only the
   names of the mods picked with "+". The tree is worked out afresh each
   time the panel is drawn, so removing a parent also drops every
   dependency nothing else in the plan needs. A dependency has no remove
   button of its own.
6. **A result that is in the plan only as a dependency** shows "Needed by
   *mod*" and no button.
7. **The review page loses "Add another mod".** Adding happens only on
   the Mods tab. The review page keeps versions, optional mods, the
   hazard confirmation and Install; its Back link goes to the Mods tab.
8. **Click budgets rise by one** (SPEC 17.10): searching for a mod and
   installing it is 4 clicks, a second mod in the same change is 5.
9. **Each "+" takes a moment**, since the tree needs the portal; the row
   shows "Adding…" until the panel updates.
10. *(Orchestrator)* The saved plan is cleared when "Install N mods" is
    accepted. The failure page's "Try again" still carries the names.
11. *(Orchestrator)* The saved plan is a database table, not a file in
    the world folder: every world-folder write needs a snapshot, and
    snapshots, clones and restores must not carry a plan along. SPEC 3.4
    gains it.

### Orphaned dependencies on removal (new, SPEC 9.4)

12. Removing an installed mod lists, on its review page, each installed
    mod that no remaining enabled mod requires, as "Only needed by
    *mod*", ticked to remove too. Built-in and local (folder) mods are
    never offered. Foreman keeps no record of which mods were picked; the
    removal is snapshotted, so it can be undone.

### Favorites (#547)

13. A favorite is made only from a world: "Make a favorite from this
    world" on the world's page. Generate offers "Start from a favorite".
14. It pins the exact game version, Space Age, and each portal mod's
    version; Generate's version box stays changeable. On another game
    line the mods fall back to "Newest that fits", and a missing release
    shows as a conflict on the review page.
15. It holds the map and map-generation settings (when the world has
    them; an imported world has none, so the favorite gets the default
    map and the page says so), the server settings with every secret
    removed (game password, factorio.com username and token), the
    portal mods with versions (uploaded and folder mods are left out,
    with a note), and `mod-settings.dat`.
16. Each favorite is a folder under the data directory,
    `favorites/<id>/`, holding `favorite.json`, the settings files and
    `mod-settings.dat`. Every signed-in user sees and uses every
    favorite. A favorite can be deleted; it is not edited (make a new one
    from a world instead).
17. Starting from a favorite fills Generate in; the person can change
    anything before generating. After the world is made, its server
    settings and `mod-settings.dat` are written, and the person lands on
    the new world's review page with the favorite's mods and pinned
    versions, so one "Install N mods" goes through the checks, checksums
    and snapshot of SPEC 9.13. There is no second download path.
18. No export or import (#599).

## Tasks

| Task | What | Issues | Agent, model | Files it may touch |
|---|---|---|---|---|
| T1 | Saved plan storage: list, add (refused past 50 names), remove, clear; cleared when a world is deleted | #597 | builder, Sonnet high | new `internal/store/migrations/0008_mod_plan.sql`, new `internal/store/modplans.go` and test |
| T2 | Each search hit carries its latest release date | #596 | builder, Sonnet high | `internal/modportal/types.go`, `search.go`, `mod.go`, `lookup.go`, their tests, `internal/modportaltest/testdata/*` |
| T3 | Mods tab: always-shown results list, paging top and bottom with a page box, compact rows, "+"/"−", badges, the plan panel with its tree and warnings, phone bar; add, remove and clear routes; starts from T1 and T2's verified heads | #595, #596, #597 | ui-designer, Opus medium | new `internal/web/modplanpanel.go` and test, the search parts of `internal/web/mods.go`, `mods.html`, `modsearch.html`, `modportal.html`, `static/modsearch.js`, `static/foreman.css`, `locales/*/mods.json`, `translated/mods.json`, `internal/web/worlds.go` (delete cleanup only), e2e `mods-search`, `mods-tab` and `click-budgets` specs |
| T4 | Review page: remove "Add another mod", Back to the Mods tab, clear the saved plan on Install; chains on T3 | #597 | builder, Sonnet high | the plan parts of `internal/web/mods.go`, `modplan.html`, `locales/*/mods.json`, plan web tests, `mods-plan.spec.ts` |
| T5 | Mod settings use each setting's `localised_name` and `localised_description`; nesting stops at 20 levels and 1,000 characters | #598 | builder, Opus low | `internal/game/modsettings/control.lua`, `internal/modsettings/definitions.go`, `locale.go`, new `localised.go`, their tests, `internal/game/modsettings.go`, `internal/fakegame/modsettings.go`, `testdata/modsettings/fmtest_1.0.0/*`, `realgame2_modsettings_game_test.go` |
| T6 | Fix the flaky focus test | #593 | tester, Sonnet high | `e2e/tests/mods-prestart.spec.ts` |
| T7 | Orphaned dependencies offered on removal (ruling 12); chains on T4 | new | builder, Opus low | the removal plan code in `internal/mods` and `internal/web/mods.go`, `modplan.html`, `locales/*/mods.json`, their tests, `mods-plan` or `mods-remove` specs |
| F1 | Favorites core: folder storage, make from a world with secrets removed, list, delete, apply to a new world (ruling 13-17) | #547 | builder, Opus low | new `internal/world/favorite.go` and test, the new fields in `internal/world/generate.go`, data directory layout code |
| F2 | Favorites screens: world page button, Generate "Start from a favorite", the favorites list with delete, landing on the review page; starts from F1's head, and after T3 lands (CSS, `worlds.go`) | #547 | ui-designer, Opus low | new `internal/web/favorites.go` and test, new `templates/favorites.html`, `internal/web/generate.go`, `generate.html`, `world.html`, `internal/web/worlds.go`, new `locales/*/favorites.json`, `static/foreman.css`, new e2e `favorites.spec.ts` |
| Fold | STATUS section, the SPEC text below, delete this plan, `Fixes` lines | | builder, Sonnet | docs only |

**Waves.** T1, T2, T5, T6 and F1 start together. T3 starts from T1 and
T2's verified heads. T4 chains on T3, T7 on T4 (shared `mods.go`,
`modplan.html` and `mods.json`). F2 starts from F1's head once T3 has
landed.

## SPEC changes (the fold writes them)

- **3.4** adds "each world's unfinished mod plan (picked mod names
  only)".
- **New 5.12 Favorites** from rulings 13-18.
- **9.1** replaces the table and tick-box text: ten results at a time in
  a list saying how many match and which are shown; paging above and
  below (Previous, a page box with Go, Next), replacing only the list
  with script; the "Mods matching" panel always on the Mods tab; each
  row with the title linking to the portal in a new tab, summary, latest
  version, downloads, latest release date and a "+" named "Add *mod* to
  the plan" (44 px on a phone); "Installed", "In plan ✓" with "−", and
  "Needed by *mod*".
- **9.2** replaces "Add to plan … Add another mod" with the panel of
  rulings 2-7 and 9.
- **9.4** adds ruling 12.
- **9.6** replaces where names and descriptions come from: a setting's
  `localised_name`, resolved against every section of the mods' locale
  files as the game does (`__1__` parameters, nested keys, `""` joining
  its parameters), else `[mod-setting-name]`, else its own name; the
  same for descriptions, else none; the page's language, else English;
  20 levels and 1,000 characters at most.
- **9.14** becomes "The plan is worked out, not stored", with ruling 5
  and 10 as its first sentences.
- **17.10** sets the two rows to 4 and 5 clicks.

## Checks

Each task PR: the orchestrator's full `make verify` and the browser specs
for every touched file (T3 and F2 touch `foreman.css`, so the whole
suite), then merge. Code review on every task. Security review on T1, T5
(untrusted locale text), F1 and F2 (secrets removed, file paths). a11y
review on T3, T4, T7 and F2. `make test-game` on T5 and F1, and on the
epic head before the epic pull request (mods, install and new worlds).
After the last task: an integration review, then the fold, then the epic
pull request into main with CI green.
