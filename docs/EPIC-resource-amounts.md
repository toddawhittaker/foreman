# Resource amounts epic plan

Branch `epic/resource-amounts`, cut from main b981c7f (fixes batch 4
merged). Todd approved the scope on 2026-11-08. The epic pull request into
main is opened and left for Todd to merge. Gate B (SPEC 19.2) is still to
run after this epic.

## Scope

- #400: in the Generate page's large map view, a "Show resource amounts"
  check box, off by default, labels each resource patch with its exact
  amount, counted from the real map in a headless game (SPEC 5.6).
- #423: each map-generator slider's 100% default sits at the middle of its
  track (SPEC 5.6, DESIGN 8.7).
- #422: Foreman's own dialog when leaving Generate with unsaved settings by
  any in-app route; the browser's `beforeunload` question stays as the
  fallback for reload, closing the tab, the address bar and Back (SPEC 5.6,
  17).
- Left out on purpose: #419 and #424 (other areas, for a later fixes
  batch), and the items in "Left out" in the design below.

## Rulings

- R1. The count copies the demolisher territory run (ADR 0011): a third
  kind of preview container through the existing `previewRun`, with the
  same slot, cleanup, work folder and limits (1 GiB, 60 s, no network). The
  Docker API calls stay the SPEC 14.3 list.
- R2. Todd: labels show the amount only, in short form ("1.2M"), through
  `Intl.NumberFormat` compact notation. The text list beside the map names
  each resource.
- R3. Todd: progress is an indeterminate bar and a line saying the count can
  take up to a minute. One held, cancellable request; no background job.
- R4. The run-a-control-mod code that territories and resources share
  (about 120 lines) moves into one `internal/game/controlmod.go` instead of
  being copied. The territory real-game test covers the move.
- R5. T2a is a measuring spike. If 3072 tiles at default settings goes over
  1 GiB or 60 s on any planet, Todd rules O1 (design section 5) before T2b
  starts. The orchestrator runs `make test-game` for it.
- R6. Infinite resources (crude oil, sulfuric acid, lithium brine,
  fluorine) show yield, 100 × sum(amount) ÷ normal_resource_amount. Ores
  join when they touch, corners included (YARM's rule); wells of the same
  resource join within 12 tiles, a number the spike checks.
- R7. SPEC 5.6, 14.3 and 14.6, DESIGN 7.4, 8.7, 8.8 and 12, and a new
  ADR 0015 are written by the fold, using the spike's measured numbers.
  Task PRs do not edit them.
- R8. Machine load: every heavy run waits for the load gate (load under 20,
  12 GB free), uses at most 4 browser workers, and at most two full
  `make e2e` runs go at once.

## Tasks

Shared files and their one owner: `generate.js` and `generate.html` belong
to T1. `preview.js`, `generatepreview.html` and `genpreview.go` belong to
T3. `foreman.css` and the `worlds.json` locale files (eight languages plus
`translated/worlds.json`) belong to T1 and then T3, chained. The territory
files belong to T2. The real-game test file is created by T2a and extended
by T4. There are no migrations.

| Task | Issues | Owner, model | Files it may touch | Order |
|---|---|---|---|---|
| T1 Sliders and leave dialog | #423, #422 | builder, Sonnet high (two commits, one PR) | generate.js, generate.html, foreman.css (new `.leave-dialog` block only), locales/*/worlds.json and translated/worlds.json (`worlds.generate.leave.*` only), e2e/tests/sliders.spec.ts, e2e/tests/generate-leave.spec.ts (new) | Wave 1 |
| T2a Spike: count mod and measurements | part of #400 | builder, Opus medium | internal/game/controlmod.go (new), territories.go, territories/territories.go, resources.go (new), resources/control.lua and resources/resources.go (new), internal/gameenv/territories.go, a new internal/game real-game resources test (timing and patch counts per planet and line) | Wave 1, beside T1; stops and reports |
| T2b Parser, limits, fake game | part of #400 | builder, Sonnet high | internal/resource/ (new), internal/game/resources*.go and their unit tests, internal/fakegame/territories.go (`makePreview`), internal/fakegame/resources.go (new) and its test | After T2a and any O1 ruling |
| T3 Web and large-view UI | #400 | ui-designer, Opus medium | internal/web/genresources.go and its test (new), genpreview.go, generatepreview.html, preview.js, foreman.css (resource block, shared label selector), locales/*/worlds.json and translated/worlds.json (`worlds.generate.resources.*`), e2e/tests/resources.spec.ts (new) | From T1's and T2b's verified heads |
| T4 Real-game tests | part of #400 | tester, Sonnet high | the real-game file from T2a; optionally internal/web/genresources_game_test.go (new) | After T2b, beside T3 |
| Fold | all | builder, Sonnet high | docs/STATUS.md, SPEC.md, DESIGN.md, ADR 0015, delete this plan | Last |

Each task's prompt names its exact file list; a task that must touch a file
outside it says so in its report.

## Checks

- Each task: the orchestrator runs `make verify` and the browser specs for
  every file the task touched before the squash merge.
- After each wave: `make verify` and the full `make e2e` on the epic head.
- Reviews per task, started as each task PR merges: code-reviewer always;
  security-reviewer on T2a, T2b and T3 (the container, the parser, the
  endpoint); a11y-reviewer on T1 (the dialog) and T3 (labels, the list,
  focus).
- At the end: one integration review (code-reviewer across tasks and dead
  code, a11y-reviewer for flows that cross tasks), and a second-agent
  translation review of the new strings (SPEC 22.6).
- Real-game check: T4's invariants (listed under "T4 invariants" below) against real
  `factoriotools/factorio` containers on 1.1.110, 2.0.77 and 2.1.24 (Space
  Age planets on 2.x only), run by the orchestrator with `make test-game`
  before the epic pull request.
- A task PR stuck for more than 30 minutes gets its own time-boxed fix
  agent; its lane continues from the last verified head.

## Open choices

- O1 waits on T2a's numbers (design section 5). O2 and O3 are settled as
  R2 and R3.

---

## Design (architect, 2026-11-08): resource amounts (#400), centred sliders (#423), leave dialog (#422)

Base: main b981c7f. Sections served: SPEC 5.6, 14.1, 14.3, 14.6, 17 (17.1, 17.12, 17.14);
DESIGN 7.4, 8.7, 8.8, 12; ADR 0011 (the pattern #400 copies).

Undo test, once for the whole epic: nothing here is hard to undo. No on-disk world
layout, snapshot format, database schema or public API changes, and the set of Docker
API calls stays exactly the SPEC 14.3 list. The new endpoint is a page-fragment
endpoint like `/generate/preview/territories`, not part of the API. So every choice
below takes the smallest change, except one small refactor where the duplication is
real (choice B2).

---

### 1. #400 Resource amounts in the large view

#### 1.1 How the count runs (reuse the territory run)

Today a territory run (internal/game/territories.go, ADR 0011) is a one-off preview
container that runs one `--create` with a control-only mod. Its `on_init` generates
the picture's chunks, reads them and writes JSON with `helpers.write_file`. Foreman
then reads the file under limits. Resource counting is the same shape. Only the Lua
and the parser differ.

| Part | Design |
|---|---|
| Container | A third kind of preview container, `foreman-resources-<folder>`, made by `c.previewRun(ctx, req, "resources", ...)` (internal/game/preview.go). It gets the same slot, stale cleanup by label, work folder, `oneOffHostConfig` (no network, read-only root, caps dropped, 1 GiB, 64m tmpfs) and the 60 s limit. No new Docker endpoint. |
| Mod | `internal/game/resources/` with `control.lua` embedded, a sibling of `internal/game/territories/`. Mod name `foreman-resources` 1.0.0. `params.lua`, written for each run like `radius.lua`, returns `{radius = N, planet = "<name>"}`. The planet comes only from `mapgen.Planets(line, spaceAge)`, never from user text. |
| One planet per run | The large view shows one planet, so one run counts one planet, and each planet is cached on its own. Five planets in one run would not fit 60 s. |
| Lua, step 1 | Get the surface: `game.planets[p].surface or game.planets[p].create_surface()` on 2.x, `game.surfaces["nauvis"]` on 1.1. Then `surface.request_to_generate_chunks({0,0}, radius)` and `surface.force_generate_chunk_requests()`, as territories does. |
| Lua, step 2 | For each chunk inside the picture, call `find_entities_filtered{area = chunk, type = "resource"}`. Going one chunk at a time keeps the number of LuaEntity handles small; one call for the whole area could hold 400k of them. Keep only tiles inside `[-size/2, size/2)`. Per tile, record the resource name, `amount`, `prototype.infinite_resource` and `prototype.normal_resource_amount`. |
| Lua, step 3 | Group tiles into patches (1.2) and write `resources.json` with one entry per patch: `name, x, y` (the label tile), `amount` (sum), `n` (entity count), `infinite`, `normal`, and the bounding box. Output is a few KB to a few hundred KB, never the raw tiles. |
| 1.1 vs 2.x | Use `local h = helpers or game` for `write_file` and `table_to_json` (1.1 has no `helpers`). `info.json` gets `factorio_version` per line, as now, and depends on `base`, plus `space-age` only when the world has it. Everything else used (`request_to_generate_chunks`, `force_generate_chunk_requests`, `find_entities_filtered`, `amount`, `infinite_resource`, `normal_resource_amount`) exists on 1.1, 2.0 and 2.1. Not yet proven: that 1.1's `--create` runs a mod's `on_init`. Territories never ran on 1.1. The spike (T2a) checks it first. |
| Result path | `Controller.Resources(ctx, req, planet) (resource.Result, error)`, with the same errors as Territories: ErrPreviewInvalid, Busy, ImageMissing, Timeout, or *PreviewError with OOM. |
| Parsing | A new package `internal/resource` (a sibling of `internal/territory`) holds `Result`, `Parse(b, size)`, limits and yield. The game container is untrusted (14.1), so the file must be a regular file read with `world.ReadSmallFile`. Limits: at most 1 MiB and at most 5,000 patches (set the final numbers after the spike). Names must match `^[a-z0-9_-]{1,64}$`. Numbers must be finite, ≥ 0 and ≤ 2^53. The label tile must be inside the picture. `normal` must be > 0 when `infinite` is set. Nothing from the file goes into a log line; only counts do. |
| Oil as yield | For `infinite` patches, Go works out yield % = 100 × sum(amount) / normal. That is the game's own per-well yield, summed, the way YARM shows oil. The same rule covers every infinite resource: crude oil, Vulcanus's sulfuric acid geyser, and Aquilo's crude oil, lithium brine and fluorine vent. Ores show the summed amount. |
| Resource names | No new strings. Map the entity name to the planet's existing control label in `mapgen.json`: drop a `<planet>_` prefix and turn `_` into `-` (so `vulcanus_coal` becomes `coal`, `gleba_stone` becomes `stone`, `aquilo_crude_oil` becomes `crude-oil`). An entity name with no matching control shows as it is. It has already passed the name check, and html/template escapes it. |

The patches cover what lies inside the picture. A patch the edge cuts counts only its
part inside. One hint sentence says so.

#### 1.2 Grouping tiles into patches

What existing mods do:
- YARM (`resmon.lua`, `scan_current_site`) grows a site from one ore entity. It
  looks one tile away in every direction and adds any neighbour of the same
  resource name. That is 8-connected flood fill. Oil cannot be grouped this way, so
  YARM makes the player select an oil field by hand.
- "Where dat is!?" and Resource Map Label Marker tag one label per patch at its
  middle, with the amount.

Design:
- **Ores:** connected-component labelling over the tile grid. Two tiles of the same
  resource that touch, including corners (8-connected), are in one patch. This is
  YARM's rule, run as a two-pass union-find in Lua: O(n), with one integer key per
  tile.
- **Infinite resources (wells):** wells of the same resource whose distance on
  either axis is at most D tiles are in one patch. Start with D = 12 and check it
  in the spike against the picture.
- **Label point:** the patch tile nearest its centroid. A centroid alone can fall
  outside a C-shaped patch.
- **Spike checks:** do low-coverage ores break into tiny fragments? If they do,
  allow a gap of one tile (distance ≤ 2) for ores. Does D merge two real oil
  fields, or split one?

Where the grouping lives: in Lua (recommended) or in Go.

| Design | Gains | Costs |
|---|---|---|
| A. Group in Lua, output patches (recommended) | The output is small and bounded, so Foreman's own memory is untouched and the 64m tmpfs is never at risk | The algorithm can only be tested in a real game, not by `make verify` |
| B. Lua dumps every tile, Go groups | Go unit tests cover the grouping | 3072 tiles on Nauvis means a few hundred thousand tiles: a 10 to 30 MB file in the container's tmpfs and in Foreman's memory, with bigger caps on both sides |

Both are easy to undo: the format is internal to Foreman's own Lua and Go. Take A.
The real-game test (T4) can still prove it. Lua also writes per-resource totals,
summed separately, and the parser checks that the patch sums equal them.

#### 1.3 Bounds: area, time, memory

- **Area:** the preview's own picture, 1024 to 3072 tiles. That is the same area and
  the same cap as territories.
- **Time and memory:** today's limits (60 s, 1 GiB). Territories at 3072 on
  Vulcanus peaked at 750 MiB in 27 s. Nauvis and Gleba make far more entities per
  chunk (trees, rocks, spawners, plants), so the headroom is unknown. **The spike
  measures before anything else is built:** every planet on 1.1.110, 2.0.77 and
  2.1.24, at 1280 and 3072. It also runs the worst case: Nauvis 3072 with resource
  frequency and size at 600%. If a run goes over, the person sees the existing
  timeout or out-of-memory error with "choose a smaller preview size". That fails
  safe.
- **If 3072 at default settings does not fit,** Todd decides (choice O1).

#### 1.4 Cache, cancellation, progress

- **Cache key:** the existing `settingsKey(req)` (version, preset, Space Age, seed,
  size, both settings files) plus the planet. The results live in the user's
  `previewEntry` as `resources map[string]*resource.Result`, counted in `bytes`
  like territories. `joinLocked` must clone the map, following its copy-on-write
  comment. Turning the box off and on, or going back to a counted planet, is
  instant. A settings change makes a new entry, so a recount is needed. That is
  correct.
- **Request:** `POST /generate/preview/resources` takes the form and a `planet`
  field. The page sends it only after the picture has arrived, so the picture never
  waits. The handler copies `handleGenerateTerritories` and `territories()` in
  internal/web/genterritories.go, including the write deadline (`previewDeadline`).
  This is what "off the request path" means here: a separate request after the
  picture, not a background job (choice O3).
- **Cancellation:** the browser aborts its fetch, the request context ends, and the
  existing `runOneOff` stops and deletes the container and its folder (SPEC 14.3,
  already tested by realgame2_previewcancel). The page aborts when the view closes,
  a setting changes (`drop()` in preview.js), the view moves to another planet, or
  the box is unticked.
- **After a settings change** (#399): stale labels are removed at once. The recount
  is sent only when the box is on, and only after the redraw's picture lands, in
  `draw()`'s success path, where `territories(false)` is called today.
- **Progress:** a visible line in the view, "Counting resources in the map… This
  can take up to a minute.", with an indeterminate `progress` bar. It reuses
  `.wait progress`, which is hidden under reduced motion. Like the drawing line, it
  is not read out. The status line then says "Resource amounts ready." or the
  error (choice O3).
- **Errors:** busy, timeout, out of memory, image missing, failed and invalid, each
  with Try again where a retry can work. This copies `territoryFailure` and the
  `territory-result` template.

#### 1.5 Labels

- **Where:** only in the large view. The box "Show resource amounts" (no `name`, so
  it is a view, not a setting) sits in the view's bar, so it is there both with the
  settings column and in map-only views. It is off when the page loads and stays as
  set while the page lives. It belongs to every planet.
- **Markup:** the server returns `<span class="resources" aria-hidden="true">`. Each
  label is a span placed by percent of the picture, as `territoryView` places them.
  It carries `data-amount` and `data-kind="amount|yield"`, and the server sorts the
  labels by bounding-box area, largest first. The span sits in `.map`, beside the
  territory overlay.
- **Short form:** the script writes the text with
  `Intl.NumberFormat(lang, {notation: "compact", maximumFractionDigits: 1})`.
  English gets "1.2M", German "1,2 Mio.", Chinese "120万". Yield uses
  `{style: "percent", maximumFractionDigits: 0}`. Both are correct in all eight
  languages with no new strings. The large view needs script anyway.
- **Readable at every zoom:** a fixed type size with the territory labels' white
  face and dark ring. Share the `.territories__label` rule by adding a second
  selector; do not copy it.
- **No overlap:** a greedy pass in preview.js runs on show, after each zoom
  (throttled to one per animation frame) and on resize. It does this:
  - Measure each label once, since its size is fixed.
  - Work out its box from the map's current scale and clamp the box inside the map
    rectangle. That also fixes the "sticking out" problem of #321 for these labels.
  - Place labels in the server's order. Skip, with `hidden`, any label whose box
    meets one already placed, a demolisher label or the pin. Demolisher labels and
    the pin are always kept.
  - Zooming in has room for more labels. A few hundred boxes checked against each
    other cost nothing.
- **Label content:** the amount only (choice O2).

#### 1.6 Text for keyboard and screen-reader users (SPEC 17)

- A `details` element in the view, collapsed: "Resource amounts as text (N
  patches)".
- Inside, one list per resource with its total, then each patch's amount and its
  position in the game's own coordinates, as "(x, y)" from the start. It is sorted
  by amount. It uses lists, not a table, so the 17.12 reflow rules do not apply.
- The same server fragment holds it. It is visible to everyone and inside the
  dialog, because the page outside is inert.
- The ui-designer decides where it sits in the bar or in the settings column, as
  long as both wide and narrow views have it.

#### 1.7 Security (SPEC 14)

| Rule | How it is met |
|---|---|
| 14.3 Docker surface | Same endpoints, host settings, slot and cleanup. 14.3 gains one paragraph for the third kind of container, next to the territory paragraph. |
| 14.1 game is untrusted | `resource.Parse` checks under limits. The file must be a regular file (no named pipe). No file content goes in logs or errors. |
| Foreman's own script | The Lua is embedded in the binary. The only per-run inputs are a whole number (radius) and a planet from a fixed list. |
| 14.6 | Add the resource file's cap next to the territory file's 2 MiB. |
| 14.7 | A POST behind `requireUser` through `handleUpload`, like territories, so the same-origin check applies. |
| Abuse | One slot for all of Foreman, at most 8 waiting, one current preview per user. A user who keeps asking blocks others for at most 60 s at a time, as previews and territories already can. |

#### 1.8 Smallest change and long-term shape

| Choice | Smallest | Long-term shape | Gap | Recommendation |
|---|---|---|---|---|
| B1 Request model | Held request, like territories | A background job with a status endpoint and real progress | Large: a job store, polling, a status endpoint | Smallest. Easy to undo. |
| B2 Run code | Copy territories.go into resources.go | One `controlmod.go` in internal/game that writes the mod, `mod-list.json`, `info.json` and the params file, plus the script, spec and output read, used by both kinds | About 120 lines duplicated if copied | Share it. The duplication is real (two consumers, the same steps), the refactor stays inside one package, and the territory real-game test covers it. |
| B3 Grouping | Lua | Go | 1.2 | Lua |
| B4 One run for territories and resources on Vulcanus | Separate runs | One run doing both | Saves about 27 s when both boxes are on, Vulcanus only | Separate runs. Left out. |

---

### 2. #423 Sliders centred on 100%

**Cause:** the range input's value is the option index (`generate.js` `addSliders`
and `syncSlider`), so the thumb sits at index / (n - 1). The steps are not even
around 100%:
- With "None" (sizes, richness, starting area): 6 steps below, 5 above.
- The terrain scale: 5 below, 6 above.
- A select can also hold a preset-only extra step (75, 133 or 150).
- Frequency (5 and 5) and bias (10 and 10) are already even.

**Smallest change, in generate.js only:**
- Map each option index to a track position so that the default sits at the middle.
  `d` is the index of the option whose value is `"100"` (bias: `"0"`), and
  `L = lcm(d, n-1-d)`. The range runs from `min=0` to `max=2L` with `step=1`.
  Index `i` sits at `i·L/d` below the default and at `L + (i-d)·L/(n-1-d)` above it.
  When the select has no default option, or the default is the first or last
  option, keep today's index mapping.
- `syncSlider` sets `range.value` to the index's position.
- In the `input` handler, map the value back to an index. While a pointer is down
  on the slider, take the nearest index. Otherwise (keys, or a screen reader's
  increment) take the next index in the direction of the change when the nearest is
  still the current one, so a one-unit arrow step never sticks. Home, End and Page
  keys then work natively.
- Keep `aria-valuetext` as the step's words.
- Nothing changes in the server, the select, the posted values or the labels.
- The `sliders.spec.ts` tests for both ends at the first and last step still hold
  (index 0 sits at 0, the last at `2L`).
- Add one test: for every kind of slider, at its default, the thumb's centre is at
  the track's middle within 1 px, at 1280 px and at 200% zoom.
- Rejected: a CSS-only track drawing. The thumb's place follows the value, so CSS
  cannot move it.

### 3. #422 Foreman's own leave dialog

**Smallest change:**
- **generate.html:** add `<dialog id="leave-dialog" aria-labelledby aria-describedby>`.
  - Heading (a question that names the thing, DESIGN 7.4): "Leave without saving
    these map settings?"
  - One sentence: "The new world's settings are not saved. If you leave, they are
    lost."
  - Buttons: "Stay on this page" (primary, focused on open) and "Leave without
    saving" (secondary).
  - Four new keys under `worlds.generate.leave.*` in eight languages.
- **generate.js:** reuse the existing `isDirty()` and `submitting`.
  - Catch any `a[href]` click in the capture phase, but only a plain one: main
    button, no modifier key, no `download`, no `target`, and not a same-page `#`
    link (the skip link and the error-summary links).
  - Also catch the `submit` of any form other than `#generate-form`: the header's
    language and sign-out forms.
  - When `isDirty()`, call `preventDefault()` and `showModal()`, and remember the
    link or the form and its submitter.
  - Stay, or Escape (the native `cancel` event): close and return focus to the link
    or button that was used.
  - Leave: set `leaving = true`, so `beforeunload` stays quiet, then
    `location.assign(href)` or `form.requestSubmit(submitter)`.
  - `pageshow` resets `leaving` along with `submitting`.
- **beforeunload:** keep it exactly as now. It is the fallback for reload, closing
  the tab, the address bar and browser Back.
- **foreman.css:** a small `.leave-dialog` block. Copy the confirmation box look of
  DESIGN 7.4 (`surface` face, `--radius`, padding `--space-4`) and add a
  `::backdrop`. Check it at 320 px.
- **DESIGN:** 12 ("Modal dialogs other than the large view") and 7.4 ("no modal
  dialog") each gain this one exception. The orchestrator writes it in the plan
  commit.
- **Tests:** generate-leave.spec.ts changes. Cancel, header links, tabs and the
  language menu now show Foreman's dialog, so assert no `page.on("dialog")`. Keep
  one reload test for `beforeunload`. Add an accessibility scan of the open dialog
  (a known gap in STATUS fixes batch 4).

---


### T4 invariants

- The same request gives the same patches, and another seed gives others.
- Every resource of each planet is found on 1.1.110, 2.0.77 and 2.1.24. Space Age
  planets are checked on 2.x only.
- A resource switched off has no patches.
- Richness 200% gives about twice the ore of 100%, within a band.
- Infinite resources come back as yield, and ores never do.
- Patch sums equal the per-resource totals.
- Alignment: at each large patch's label tile, the preview PNG's pixel matches the
  resource's colour (Lua can also return `prototype.map_color`). This catches a
  flipped or shifted overlay.
- At 3072 on every planet, the run stays within 1 GiB and 60 s. Log the peaks for
  SPEC 5.6.
- No container or folder is left after a cancelled count.

---

### 5. Open choices for Todd

**O1. Only if the spike shows 3072 at default settings goes over 1 GiB or 60 s on
some planet.** The options, in the order recommended:
1. **Lean count run (recommended, if it holds).** The count run turns trees,
   decoratives and enemy bases off, but only once real games on all three lines
   show the patches are exactly the same with and without them. This is how cliffs
   off was proven. Gains: no cap, and the limits stay. Costs: a proof to maintain,
   and a game patch could break it, which the real-game test catches.
2. **Cap.** Offer amounts only up to the measured size. Above it the box is
   disabled with a hint. Gains: simple and safe. Costs: the kind of "turned off"
   state that #317 removed.
3. **Raise this run's limits** (for example 2 GiB and 120 s), with SPEC 14.3
   changed. Gains: no cap and no proof. Costs: twice the host memory, and a slot
   that blocks others twice as long.

**O2. Label content.**
1. **The amount only, "1.2M" (recommended).** The colour on the map shows the
   resource, and the text list names it. Fewer labels collide.
2. **Short name plus amount, "Iron 1.2M".** It is clear without the colour, but
   labels are about twice as wide, so fewer show at each zoom, and it needs short
   names in eight languages.

**O3. Progress.**
1. **Indeterminate bar and a line saying it can take up to a minute
   (recommended).** No new mechanism.
2. **Real percentage.** Lua generates in bands and logs progress, Foreman polls the
   container log, and the count becomes a background job with a status endpoint.
   Gains: a true percentage. Costs: a job store, polling and two more endpoints.
   It is easy to add later if the wait feels long.

Settled here, not asked:
- Large view only, off by default, exact counts from the real map (the issue).
- Amounts cover the picture's area only.
- No labels in the PNG download or the thumbnail.
- One planet per run.
- Lua grouping.
- The ore and well rules in 1.2.

---

### 6. Left out, and why

- **Amounts in the downloaded PNG and the zip, and labels on the page's thumbnail.**
  The issue asks for the large view only.
- **A no-script path.** The large view itself needs script.
- **One run for both territories and resources on Vulcanus.** It is an
  optimisation with no need yet.
- **A "+" mark on patches the edge cuts.** The hint sentence covers it.
- **Depletion times, per-patch ore counts, or searching beyond the picture** (as
  "Where dat is!?" does). None are asked for.
- **Caching across settings changes, or for more than the current preview.** The
  issue's key is the current settings, and one entry per user is the store's rule.
- **Mods' resources.** The preview never loads the world's mods (ADR 0011), so the
  map has only the base game's and Space Age's resources.
