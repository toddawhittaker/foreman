# Epic 7: Generating worlds and game settings

Generate (SPEC 5.6), Clone (5.7) and Game settings (section 11), as SPEC
18.7 lists them. Branch `epic/7-generate`, cut from main 8d60dc8. The
architect designed it on 2026-11-06; the design is summarised in the
rulings and tasks below.

## Rulings

Todd, 2026-11-06:

1. **D9.** Foreman decodes a map exchange string itself, in Go, for every
   game line it supports (1.1, 2.0, 2.1). The decoder is a port of
   rfvgyhn/factorio-exchange-string-parser (`exchangeString.mjs`, MIT
   licence), which already handles 1.1 to 2.1.13 and later. The port keeps
   that project's copyright notice in the package. Presets and seeds go to
   the game's own `--preset` and `--map-gen-seed` options.
2. **A newer patch.** A string from a patch newer than any the decoder
   was tested against is accepted when it is on a known line, is not
   newer than the world's game version, uses every byte exactly, has a
   matching checksum, and every value passes its range check. Anything
   else is refused with the version and the reason.
3. **The map is made at the world's first start.** Generate writes only
   the world folder and its settings files; the start script creates the
   save when the world has none.
4. **Scope is P0 and P1.** Generate, Clone, the server-settings form, the
   raw JSON editors with line-and-reason errors (11.2), and the map-gen
   form inside Generate. Editing map-settings after creation (P2) is issue
   #207.
5. **Admins.** A stopped world writes `server-adminlist.json`. A running
   world sends `/promote` and `/demote` over RCON (escaped per 14.9) and
   lets the game write the file.
6. **Visibility** offers LAN and hidden only. Public listing needs
   factorio.com credentials and is issue #206.
7. **The map-gen form has full resource controls**: every autoplace
   control's frequency, size and richness, per game line and, on 2.x, per
   planet, plus preset, seed, width and height, starting area, cliffs,
   water and peaceful mode. The raw editor covers the rest.
8. **Map preview** (P1). Generate offers a "Preview map" step that shows
   a picture of the map, one per planet the world will have (Nauvis only
   without Space Age, and on 1.1). Previewing is optional; Generate works
   without it.

Orchestrator rulings on the preview (Todd asked for it 2026-11-06; the
orchestrator checked the game's options the same day):

- The picture comes from the game's own `--generate-map-preview` option
  (with `--map-preview-planet` on 2.x), run in a one-off container from
  the world's game image. It takes about one second per planet and starts
  no game. Screenshots were rejected: the headless server cannot render.
- The preview is made from exactly the settings Generate would write. A
  blank seed is filled with a random one at the preview, and the form keeps
  it, so the world matches the picture.
- The one-off container runs as the game's user, with no network, a
  read-only root, all capabilities dropped, a memory limit, a 60-second
  limit and only a fresh temporary folder mounted. One preview runs at a
  time. It uses only an image already downloaded; otherwise the page says
  the image must be downloaded first.
- Pictures are held on the server under a random token tied to the user,
  like a pending settings change, and deleted after 15 minutes.

Orchestrator rulings (from the architect's design):

- `world.json` gains `new_map` (`{"preset": "...", "seed": 123}`, either
  may be empty). Its presence lets the next start create a map; it is
  cleared once that start reaches InGame. The pre-start "no save" refusal
  (7.4) is skipped only when `new_map` is set and `saves/` has no zip.
- Foreman passes `FOREMAN_NEW_MAP`, `FOREMAN_PRESET` and `FOREMAN_SEED`
  in the container environment. The start script runs `--create
  saves/<world-id>.zip` only when `FOREMAN_NEW_MAP` is set and no zip
  exists, so a Docker restart can never overwrite a save. A failed create
  exits and uses the existing failed-start reporting.
- An exchange string is decoded at Generate, so a bad string is refused on
  the form. The decoder's output is written as `map-gen-settings.json` and
  `map-settings.json`. Limits: 64 KB of string, 1 MB after decompression.
- Clone copies `mods/` and the config files (never `rconpw`), the game
  version and the Space Age flag. "Keep the save" copies only the save
  that would load; "fresh map" sets `new_map` with a random seed.
- A settings save goes edit, then a preview with the diff, then confirm.
  The pending change is held on the server under a random token tied to
  the user and the world, for 15 minutes, so no secret ever sits in a
  hidden field. Confirm takes the world lock, saves a running game, takes
  a snapshot with the new reason `settings`, checks the file has not
  changed since the preview (if it has, shows the new diff), writes the
  file atomically, and records the audit entry `settings-save`.
- The game password is write-only (11.4): the form shows only whether
  one is set; the raw editor leaves the key out and puts it back on save;
  the diff says "will be set", "changed" or "removed". The same applies to
  the factorio.com `username`, `password` and `token` keys, which the
  game's log redaction (14.8) also learns.
- Raw JSON: at most 1 MB; must be one JSON object; known keys are
  type-checked per game line; unknown keys are kept and listed in the
  diff; each error names its line and the reason.
- Every change to a running world's `server-settings.json` says it takes
  effect after a restart and links to Restart.
- Routes and contract stubs are added by this plan PR so wave-two tasks
  compile apart: `generateRoutes`, `cloneRoutes` and `settingsRoutes` in
  `internal/web/server.go`, each in its own stub file, and the map-gen
  form stub `internal/web/mapgenform.go`. T9 owns the shared partial and the one line in `render.go` that loads it with each page.

## Tasks

Every task follows the shared rules file it is given, writes tests with
its change, puts its proposed STATUS line in its PR body, and adds every
new string in all eight languages (SPEC 22).

### Wave 1, from the plan PR's merge

| Task | Owner and model | Files | Delivers |
|---|---|---|---|
| T1 World layer | builder, Sonnet high | `internal/world/`: `meta.go`, `world.go`, `import.go` (call site), new `generate.go`, new `clone.go`, `configfiles.go`, `precheck.go`, their tests | `new_map`; `Generate(ctx, NewWorld) (id, error)`; `Clone(ctx, srcID, name, keepSave)`; `WriteConfigHeld(id, file, data)`; `ReasonSettings`; the 7.4 change |
| T2 Game data and fixtures | tester, Sonnet high | new `internal/mapgen/` (embedded per-line data: presets, autoplace controls per planet, default map-gen and map settings, read from each image), `internal/mapexchange/testdata/`, `internal/mapexchange/golden_test.go`, Makefile target `mapexchange-golden` | Data the form, decoder and preview use (including each line's planets); strings made by 1.1.110, 2.0.66, 2.0.77, 2.1.9 and 2.1.24 with the game's own parsed JSON |
| T3 Settings rules | builder, Sonnet high | new `internal/gamesettings/` | `Validate(file, line, b) []FieldError{Line, Reason}`, `SecretKeys`, `Diff`, `StripSecrets`, `MergeSecrets`, admin-list diff |
| T4 Design | ui-designer, Opus low | `docs/DESIGN.md` | Patterns for the diff view, the raw editor, the Generate mode choice, the write-only field, and the full map-gen form at phone width |
| T5 Decoder | builder, Opus low, time-boxed | `internal/mapexchange/*.go` except `golden_test.go` | `Decode(s) (Result, error)` with `*UnsupportedError{Version, Reason}`; ruling 2; unit tests |

### Wave 2, from the wave-1 head

| Task | Owner and model | Files | Delivers |
|---|---|---|---|
| T6 Game layer | builder, Sonnet high | `internal/game/container.go`, `launch.go`, new `settings.go`, the secrets list, their tests; `internal/fakegame/` | Create step and environment; clearing `new_map`; `SaveSettings` with snapshot and admins over RCON; new `preview.go`: `Preview(ctx, version, files, seed, planets) ([]PNG, error)` in a locked-down one-off container (ruling 8); fake game models `--create` and the preview |
| T7 Generate and Clone pages | ui-designer, Sonnet high | `internal/web/generate.go`, `clone.go`, `templates/generate.html`, `clone.html`, `worlds.html`, `world.html` (links, including Settings), `locales/*/worlds.json` | Both flows, using the map-gen form partial; the Preview map step and its images |
| T8 Settings pages | ui-designer, Sonnet high | `internal/web/settings.go`, new `pending.go`, `templates/settings*.html`, new area `locales/*/settings.json` | Form, raw editors, preview with diff, confirm, restart note |
| T9 Map-gen form | ui-designer, Sonnet high | `internal/web/mapgenform.go`, `templates/mapgenform.html`, the partial-loading line in `render.go`, new area `locales/*/mapgen.json` | Full resource controls from `internal/mapgen`; parses the form into map-gen JSON with field errors |

### Wave 3, from the wave-2 head

| Task | Owner and model | Delivers |
|---|---|---|
| T10 End-to-end tests | tester, Sonnet high | Generate in each mode on each line, then start; preview on each line; Clone both ways; settings diff, snapshot, restart note; no secret in any page |
| Reviews | code-reviewer, security-reviewer, a11y-reviewer; translation review | Findings, then a fix round |
| Real-game check | tester, Sonnet high | Each mode on 1.1, 2.0 and 2.1 reaches InGame with the expected map, and the map matches its preview; confirm the "first start writes example config files" gap is gone |
| Fold | builder, Sonnet high | SPEC, STATUS, I18N.md, ADR for D9; delete this plan; `Fixes` lines |

## Spec edits for the fold

5.6 records D9, rulings 1 to 3 and the preview (ruling 8) with each line's presets and planets; 14.3 the preview container; 5.1 adds
`new_map`; 4.3 the create step; 7.4 the reworded exception; 5.7 what Clone
copies; 6.7 the `settings` reason; 11.1 admins live in
`server-adminlist.json`, visibility is LAN or hidden; 11.3 the restart
rule; 14.6 the size limits; 14.8 the factorio.com keys; table 20 drops D9.
I18N.md adds the `settings` and `mapgen` areas and the new reason label.

## Left out

Editing map-settings after creation (#207); public listing (#206);
whitelist enforcement; checking a string's planets against the world's
mods (the game's create step reports that).
