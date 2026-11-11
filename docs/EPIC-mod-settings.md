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
  - **Map settings** (Factorio's `runtime-global`) are written to the file
    for a world with no save yet, set live over RCON on a running world,
    and shown read-only on a stopped world with a save.
  - **Per-player settings** (`runtime-per-user`) are not shown. Their bytes
    in the file are kept exactly.
- Left out on purpose: see "Left out" at the end.

## What a real test showed (2026-11-11, 1.1.110, 2.0.77, 2.1.24)

The same on all three lines:

1. A plain `/c settings.global[...] = ...` is refused: "Settings can only be
   changed by the owning player or the mod that made the setting."
2. Running the command in the owning mod's context works:
   `/sc __<mod>__ settings.global["<name>"] = {value = ...}`. The value
   survives a save and a restart. A bad value gets "Value is invalid".
3. On a save that has never run Lua, the first Lua command over RCON is
   held, not run: the reply is empty and the game log asks to repeat it.
   The second one runs, and the save remembers from then on.
4. Only the running game knows which mod owns a setting.
   `--dump-data` output has no owner.
5. The game rewrites `mod-settings.dat` when the server starts, not when it
   exits. Live map changes never reach the file; the file's map values only
   seed a new map.

Test files (the `fmtest` mod, game-written files, `rcon.py`) are in the
orchestrator's scratchpad; T1 and T7 get the path in their prompts.

## Rulings

- R1. Todd: map settings and Steam achievements. Any non-built-in mod
  already stops Steam achievements, and every world with mod settings has
  one. A Lua command also ends the game's own achievement tracking for
  that save, which Todd accepts. So there is **no confirmation step**:
  Foreman sends a harmless probe (`/sc rcon.print("foreman-lua-ok")`), sends
  it once more if the reply is empty, then reads. The map section shows one
  static line saying live changes end achievements for the save.
- R2. Todd: a stopped world with a save shows its map settings read-only,
  with "Start the world to change them" and the Start button (the active
  world) or the Switch link (another world). No queue, no automatic start.
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
  `--dump-data` (no owner, a 14 to 44 MB file the 64 MB tmpfs cannot hold);
  reading definitions over RCON (works only while running).
- R6. The definitions cache is in memory, one entry per world, keyed by the
  game version plus the mods fingerprint without `mod-settings.dat`. The key
  is read before and after the run; a result is kept only when both match.
  Lost on restart. Rejected: a disk cache (new files to save 1 to 2 s).
- R7. Names and descriptions come from each enabled mod's
  `locale/<code>/*.cfg` (`[mod-setting-name]`, `[mod-setting-description]`),
  in the page's language, else English, else the setting name, marked with
  the `lang` of the text shown. Factorio codes `en de ru zh-CN fr es-ES pl
  cs` map to Foreman's eight.
- R8. A startup edit decodes the current file, changes only the edited
  entries (a missing entry copies its node from the defaults file), and
  re-encodes. Per-player and unknown entries, the header and the flag byte
  keep their bytes. It then goes through `game.ApplyMods` as the upload does,
  with one new guard: `ModChange.SettingsSeen`, the SHA-256 of the file the
  form was drawn from; `ApplyModsHeld` refuses with `ErrModsChanged` when the
  file no longer matches. The upload leaves it empty.
- R9. A live map change takes the world lock, saves (`saveRunning`), takes a
  "Before mod change" snapshot, writes one `mods` audit entry naming the
  settings (never values), then sends one command per owning mod ending in
  `rcon.print("foreman-set-ok")`. Success is exactly that marker. The form
  carries a hash of the values it showed; if the game's values differ at
  post time, nothing is applied and the new values are shown. No new
  snapshot reason or audit action.
- R10. No injection (SPEC 14.9). Foreman writes every bool, number and color
  literal itself; every byte of a string value or setting name becomes a
  `\ddd` escape; the `__<mod>__` name comes only from the game's own reply,
  must be an installed mod and match `^[A-Za-z0-9_-]{1,100}$`, else that
  mod's settings show "cannot be changed while running". Ints stay within
  ±2^53; NaN and infinities are refused. The builder is fuzzed.
- R11. Foreman checks every changed value itself, because the game silently
  resets a bad value in the file: the setting exists with the right setting
  type; hidden settings are refused; a setting with one allowed value is
  shown locked; bool, int (whole, within limits), double (finite, within
  limits), string (UTF-8, no NUL, at most 10,000 bytes, `auto_trim` first,
  blank only with `allow_blank`), allowed values, and color (four
  components, 0 to 1). An untouched value is never checked or rewritten.
- R12. Untrusted input limits (SPEC 14.6): definitions file at most 2 MiB,
  10,000 settings, 1,000 allowed values each, unknown fields ignored,
  unknown types shown as not editable; live read reply at most 1 MiB with
  only names from the definitions; `.cfg` files 1 MiB each and 8 MiB per
  language.
- R13. The page is `GET /worlds/{id}/mods/settings`, linked as "Edit mod
  settings" first in the Mods tab's "Mod settings" panel. The form loads
  after the page (htmx `load`, as `PathModStatus` does) because a cache miss
  runs the game. Two forms: startup (Save, plus restart and countdown when
  running) and map (live, file, or the start note); one form on a world with
  no save. Settings grouped by owning mod, in Factorio's order. Controls:
  checkbox, number with min/max/step, select for allowed values, text,
  color plus alpha. Single column at 320 px. Click budget from the Server
  page: 4 for a startup change and 4 for a live map change (SPEC 17.10).
- R14. SPEC 9.6, 9.13, 9.15, 14.3, 14.6, 14.9, 17.10 and 22.3 and a new ADR
  0017 are written by the fold, from the design's proposed wording with R1
  to R4 applied. Task PRs do not edit them.
- R15. Machine load: every heavy run waits for the load gate (load under 20,
  12 GB free), uses at most 4 browser workers, and at most two full
  `make e2e` runs go at once.

## Tasks

Shared files and their one owner: `internal/game/controlmod.go`,
`preview.go` and `controller.go` belong to T3. `internal/fakegame/*` belongs
to T5. `internal/web/mods.go` (one route call), `templates/mods.html` (the
link) and the `mods.json` locale files belong to T6. There are no database
migrations.

| Task | Issues | Owner, model | Files it may touch | Wave |
|---|---|---|---|---|
| T1 Codec, definitions parser, value checks, Lua builder and reply parser, `.cfg` parser, fuzz tests, game-written test files for 1.1, 2.0 and 2.1 | #339 | builder, Sonnet high | `internal/modsettings/**` (new), `internal/gameenv/modsettings.go` (new) | 1 |
| T2 World layer: `ModSettingsKey`, `LinkModsInto`, `ModLocaleFiles`, `ModChange.SettingsSeen` checked in `ApplyModsHeld` and `CheckModChange` | #339 | builder, Sonnet high | `internal/world/modsettings.go` (new), `modtypes.go`, `modchange.go`, `mods.go`, their tests | 1, beside T1 |
| T3 Definitions run and cache, `Controller.ModSettingDefs` | #339 | builder, Sonnet high | `internal/game/modsettings.go` (new), `internal/game/modsettings/control.lua` (new), `controlmod.go`, `preview.go`, `controller.go`, their tests | 2, after T1 and T2 |
| T4 Live map settings: `ProbeLua`, `LiveModSettings`, `SetLiveModSettings` | #339 | builder, Sonnet high | `internal/game/modsettingslive.go` (new) and its test | 2, after T1 |
| T5 Fake game: the run kind and the RCON replies (held first Lua, read, `__mod__` set, owner error, "Value is invalid") | #339 | builder, Sonnet high | `internal/fakegame/modsettings.go` (new), `territories.go`, `instance.go`, their tests | 2, after T1 |
| T6 Page, handlers, template, CSS, eight languages, e2e specs, e2e test mod | #339 | ui-designer, Opus | `internal/web/modsettings.go` (new) and its test, `internal/web/mods.go` (one route call), `templates/modsettings.html` (new), `templates/mods.html` (the link), `foreman.css` (a new block), `internal/i18n/locales/*/mods.json`, `internal/i18n/translated/mods.json`, `e2e/tests/mods-settings.spec.ts` (new), `e2e/tests/click-budgets.spec.ts` (two rows), e2e fixtures for the test mod | 3, after T3, T4 and T5 |
| T7 Real-game tests on 1.1, 2.0 and 2.1 | #339 | tester, Sonnet high | `internal/game/realgame2_modsettings_game_test.go` (new), `internal/game/testdata/modsettings/**` (new) | 3, beside T6 |
| Fold | #339 | builder, Sonnet high | `docs/STATUS.md`, `docs/SPEC.md`, `docs/adr/0017-mod-settings.md` (new), delete this plan | 4 |

If T6 runs long it splits into a chain: T6a the page and startup form, then
T6b the map section (same template, so not in parallel).

T7 checks, each on all three lines: the run's definitions match `fmtest`
(types, owners, limits, hidden, one allowed value, the
`settings-updates.lua` default); a game-written file round-trips
byte-identical; a startup edit through `ApplyMods` takes effect at the next
start with per-player bytes unchanged and a 300-byte string intact; probe,
read, set and restart keep the value; over-maximum gets "Value is invalid";
without `__mod__` the owner error comes back; the game does not rewrite the
file at exit; a value outside the allowed values written to the file is
reset by the game.

## Checks

- Each task: the orchestrator runs `make verify` and the browser specs for
  every file the task touched before the squash merge.
- After each wave: `make verify` and the full `make e2e` on the epic head.
- Reviews per task as it lands: code-reviewer always; security-reviewer on
  T1 (Lua builder, parsers), T2 (hard links, file paths), T3 (the container,
  untrusted output), T4 (RCON) and T6 (form input); a11y-reviewer on T6; a
  second-pass translation check (SPEC 22.6) on T6's strings.
- At the end: one integration review (code-reviewer across tasks and dead
  code, a11y-reviewer on the whole flow), then `make test-game` and
  `make e2e-game` on the epic head.

## Left out

- Per-player settings (Todd's scope ruling).
- Changing the file's map values on a world that already has a save; they
  seed only a new map, and the page says so.
- A "Settings" link on each mod's row (needs the definitions, so a run, on
  every Mods tab open), a link from the Server page, and a search or filter.
- Mod titles in group headings; rich text, `__1__` parameters and locale
  references in mod text (shown as written).
- A disk cache, an MCP tool (SPEC 21.5 forbids mod changes), and a queue of
  changes for a stopped world.
- Flagging stored values the game will reset.
