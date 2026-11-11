# Mod settings epic plan

Branch `epic/mod-settings`, cut from main 6249fad (Gate B prep merged).
Todd started this epic on 2026-11-11, ahead of Epic 9; Gate B (SPEC 19.2)
is skipped for now and still owed before Gate C. The epic pull request into
main is opened and left for Todd to merge.

## Scope

- #339: edit mod settings in the UI, on a page of their own linked from
  the world's Mods tab (SPEC 9.6).
  - **Startup settings** are written to `mod-settings.dat` through the
    existing mod-change operation (9.13): on a running world, warn, stop,
    snapshot, write, check, start.
  - **Map settings** (Factorio's `runtime-global`) are edited in the file
    only for a world with no save yet, where the file seeds the new map.
    Once a save exists they live in the save, and an admin changes them in
    the game (Settings, Mod settings, Map). Foreman does not change them
    and runs no Lua for them.
  - **Per-player settings** (`runtime-per-user`) are not shown. Their bytes
    in the file are kept exactly.
- Left out on purpose: see "Left out" at the end.

## What a real test showed (2026-11-11, 1.1.110, 2.0.77, 2.1.24)

1. On a world with a save, the save's map values win over the file's.
   Stopping, editing the file and starting changes nothing (2.1.24: file
   42, save 4, the mod saw 4).
2. The only ways to change a map value in a save are a Lua console command
   in the owning mod's context, which ends the save's achievements for good,
   or a player admin in the game's Map tab, which does not. The game's Map
   tab sends a player input action over the lockstep multiplayer protocol,
   which Foreman cannot speak without running the whole game.
3. `--dump-data` output has no owning mod for a setting.
4. The game rewrites `mod-settings.dat` when the server starts, not when it
   exits.

Test files (the `fmtest` mod, game-written files) are in the
orchestrator's scratchpad; T1 and T6 get the path in their prompts.

## Rulings

- R1. Todd: Foreman does not change map settings on a world that has a
  save. The page lists them read-only with their current file values marked
  "values for a new map", and one line saying an admin changes them in the
  game under Settings, Mod settings, Map. Rejected: setting them over RCON
  with Lua (ends local achievements), patching the save (undocumented,
  version-specific format), joining as a game client (lockstep protocol).
- R2. A world with no save yet edits its map settings in the same form as
  its startup settings, as file writes.
- R3. Todd: the definitions run has a 4 GiB memory limit and a 5-minute time
  limit. The orchestrator measures one heavy mod pack by hand before T3
  fixes the numbers, and brings Todd any change.
- R4. Todd: editing is P1 in SPEC 9.6. Upload and download stay P0.
- R5. Definitions come from a one-off control-mod run of the world's own
  image: a fourth kind of preview container through `previewRun`, same
  label, endpoints, slot, cleanup and host settings as SPEC 14.3. The work
  folder gets the world's mods by hard link (copy when linking fails), never
  the world's own folder or its `mod-settings.dat`. The run also leaves a
  defaults `mod-settings.dat` written by the world's own image. Rejected:
  `--dump-data` (no owner, a 14 to 44 MB file the 64 MB tmpfs cannot hold).
- R6. The definitions cache is in memory, one entry per world, keyed by the
  game version plus the mods fingerprint without `mod-settings.dat`. The key
  is read before and after the run; a result is kept only when both match.
  Lost on restart. Rejected: a disk cache (new files to save 1 to 2 s).
- R7. Names and descriptions come from each enabled mod's
  `locale/<code>/*.cfg` (`[mod-setting-name]`, `[mod-setting-description]`),
  in the page's language, else English, else the setting name, marked with
  the `lang` of the text shown. Factorio codes `en de ru zh-CN fr es-ES pl
  cs` map to Foreman's eight.
- R8. An edit decodes the current file, changes only the edited entries (a
  missing entry copies its node from the defaults file), and re-encodes.
  Per-player and unknown entries, the header and the flag byte keep their
  bytes. It then goes through `game.ApplyMods` as the upload does, with one
  new guard: `ModChange.SettingsSeen`, the SHA-256 of the file the form was
  drawn from; `ApplyModsHeld` refuses with `ErrModsChanged` when the file no
  longer matches. The upload leaves it empty. No new snapshot reason or
  audit action; the audit entry names settings, never values.
- R9. Foreman checks every changed value itself, because the game silently
  resets a bad value in the file: the setting exists with the right setting
  type; hidden settings are refused; a setting with one allowed value is
  shown locked; bool, int (whole, within limits), double (finite, within
  limits), string (UTF-8, no NUL, at most 10,000 bytes, `auto_trim` first,
  blank only with `allow_blank`), allowed values, and color (four
  components, 0 to 1). An untouched value is never checked or rewritten.
- R10. Untrusted input limits (SPEC 14.6): definitions file at most 2 MiB,
  10,000 settings, 1,000 allowed values each, unknown fields ignored,
  unknown types shown as not editable; `.cfg` files 1 MiB each and 8 MiB per
  language.
- R11. The page is `GET /worlds/{id}/mods/settings`, linked as "Edit mod
  settings" first in the Mods tab's "Mod settings" panel. The form loads
  after the page (htmx `load`, as `PathModStatus` does) because a cache miss
  runs the game. One form: startup settings (Save, plus restart and
  countdown when running), map settings editable only with no save.
  Settings grouped by owning mod, in Factorio's order. Controls: checkbox,
  number with min/max/step, select for allowed values, text, color plus
  alpha. Single column at 320 px. Click budget from the Server page: 4 for a
  startup change (SPEC 17.10).
- R12. SPEC 9.6, 9.13, 9.15, 14.3, 14.6, 17.10 and 22.3 and a new ADR 0017
  are written by the fold, from the design's proposed wording with these
  rulings applied. Task PRs do not edit them.
- R13. Machine load: every heavy run waits for the load gate (load under 20,
  12 GB free), uses at most 4 browser workers, and at most two full
  `make e2e` runs go at once.

## Tasks

Shared files and their one owner: `internal/game/controlmod.go`,
`preview.go` and `controller.go` belong to T3. `internal/fakegame/*` belongs
to T4. `internal/web/mods.go` (one route call), `templates/mods.html` (the
link) and the `mods.json` locale files belong to T5. There are no database
migrations.

| Task | Issues | Owner, model | Files it may touch | Wave |
|---|---|---|---|---|
| T1 Codec, definitions parser, value checks, `.cfg` parser, fuzz tests, game-written test files for 1.1, 2.0 and 2.1 | #339 | builder, Sonnet high | `internal/modsettings/**` (new), `internal/gameenv/modsettings.go` (new) | 1 |
| T2 World layer: `ModSettingsKey`, `LinkModsInto`, `ModLocaleFiles`, `ModChange.SettingsSeen` checked in `ApplyModsHeld` and `CheckModChange` | #339 | builder, Sonnet high | `internal/world/modsettings.go` (new), `modtypes.go`, `modchange.go`, `mods.go`, their tests | 1, beside T1 |
| T3 Definitions run and cache, `Controller.ModSettingDefs` | #339 | builder, Sonnet high | `internal/game/modsettings.go` (new), `internal/game/modsettings/control.lua` (new), `controlmod.go`, `preview.go`, `controller.go`, their tests | 2, after T1 and T2 |
| T4 Fake game: the run kind writes a canned definitions file and defaults file | #339 | builder, Sonnet high | `internal/fakegame/modsettings.go` (new), `territories.go`, their tests | 2, after T1 |
| T5 Page, handlers, template, CSS, eight languages, e2e specs, e2e test mod | #339 | ui-designer, Opus | `internal/web/modsettings.go` (new) and its test, `internal/web/mods.go` (one route call), `templates/modsettings.html` (new), `templates/mods.html` (the link), `foreman.css` (a new block), `internal/i18n/locales/*/mods.json`, `internal/i18n/translated/mods.json`, `e2e/tests/mods-settings.spec.ts` (new), `e2e/tests/click-budgets.spec.ts` (one row), e2e fixtures for the test mod | 3, after T3 and T4 |
| T6 Real-game tests on 1.1, 2.0 and 2.1 | #339 | tester, Sonnet high | `internal/game/realgame2_modsettings_game_test.go` (new), `internal/game/testdata/modsettings/**` (new) | 3, beside T5 |
| Fold | #339 | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md`, `docs/adr/0017-mod-settings.md` (new), delete this plan | 4 |

T6 checks, each on all three lines: the run's definitions match `fmtest`
(types, owners, limits, hidden, one allowed value, the
`settings-updates.lua` default); a game-written file round-trips
byte-identical; a startup edit through `ApplyMods` takes effect at the next
start with per-player bytes unchanged and a 300-byte string intact; a map
value edited on a world with no save seeds the new map; on a world with a
save the save's map value wins over the file; the game does not rewrite the
file at exit; a value outside the allowed values written to the file is
reset by the game.

## Checks

- Each task: the orchestrator runs `make verify` and the browser specs for
  every file the task touched before the squash merge.
- After each wave: `make verify` and the full `make e2e` on the epic head.
- Reviews per task as it lands: code-reviewer always; security-reviewer on
  T1 (parsers), T2 (hard links, file paths), T3 (the container, untrusted
  output) and T5 (form input); a11y-reviewer on T5; a second-pass
  translation check (SPEC 22.6) on T5's strings.
- At the end: one integration review (code-reviewer across tasks and dead
  code, a11y-reviewer on the whole flow), then `make test-game` and
  `make e2e-game` on the epic head.

## Left out

- Per-player settings (Todd's scope ruling).
- Changing map settings on a world that has a save (R1).
- A "Settings" link on each mod's row (needs the definitions, so a run, on
  every Mods tab open), a link from the Server page, and a search or filter.
- Mod titles in group headings; rich text, `__1__` parameters and locale
  references in mod text (shown as written).
- A disk cache, and an MCP tool (SPEC 21.5 forbids mod changes).
- Flagging stored values the game will reset.
