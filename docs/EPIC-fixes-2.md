# Fixes batch 2 plan

Branch `epic/fixes-2`, cut from main 08e20b7 on 2026-11-07. This plan lives
only on the batch branch; the fold moves its lasting rules into SPEC.md and
deletes it (WORKFLOW.md, "Epic plans").

Issues in scope: #265, #272, #274, #275, #276, #277, #278, #279, #280, #281,
#282, #283, #284, #93. Left out: #92, #131, #132, #206, #207.

## Rulings

Todd, 2026-11-07, before the plan:

1. Scope is the thirteen issues filed 2026-11-07 plus #93.
2. #93: both update (SPEC 10.3) and rollback (10.4) work on a world that is
   not active. Nothing starts or stops, because such a world never runs.
3. #280: the "cliffs off" keys come from the game's own data, proved by real
   games (below). A cross-check against Todd's exchange strings comes later.
4. #284: phones get the select of steps, as every other slider does. If a
   4096 picture exceeds the 8 MiB cap, the cap rises to the measured worst
   case plus a margin, with the reason in SPEC.
5. #282: the download with territories is drawn server side in Go. The
   overlay on screen is drawn in the page.

Orchestrator, 2026-11-07, while Todd was away (each is the architect's
recommendation; Todd may overturn any of them at review):

6. **Picture cap (#284).** Measured at 4096 on two seeds: Gleba 8.91 MB,
   Vulcanus 8.6 MB, all others under 6 MB. `MaxPreviewPNG` becomes 16 MiB.
   The preview container's 1 GiB memory stays (peak 553 MiB, slowest 4.5 s).
7. **Cliff keys (#280).** Proved on 1.1.110, 2.0.77 and 2.1.24 by counting
   cliffs over RCON: 1.1 Nauvis `cliff_settings.richness = 0`; 2.x
   `autoplace_controls.<planet>_cliff.size = 0` for nauvis, gleba and
   fulgora, each reaching only its own planet. Never write
   `cliff_settings.richness = 0` on 2.x: it reaches every planet. Vulcanus
   has no cliff control. Cliffs off disables no achievement on any line.
8. **C1, #93 progress:** the update runs in the background as now, and sends
   the person to the world page, which shows the busy note and then the
   result. One code path, reusing the busy note.
9. **C3, preview store:** the total rises from 48 MB to 96 MiB, so one Space
   Age user's set at 4096 plus territories is not evicted by a second user.
10. **C4, large-view zoom:** the most zoom is the larger of 8 times the fitted
    size and 8 CSS pixels per tile.
11. **C5, territory container:** no RCON (the container has no network, the
    image has no client, SPEC 14.3 forbids exec). A control-only mod in a
    `--create` run writes JSON with `helpers.write_file`; if demolishers
    appear only after ticks, a `--start-server` run with an `on_tick`
    handler instead. The spike decides.
12. **C5b:** if territories at 4096 do not fit in 60 s and 1 GiB, the
    checkbox is offered only up to the largest size that fits, with a hint.
13. **C6, PNG font:** pinned `golang.org/x/image` (Go Bold for Latin and
    Cyrillic) plus a committed subset of Noto Sans SC Bold (OFL) holding
    only the zh-Hans label glyphs, with a unit test that every label glyph
    exists. Reason for the dependency: ruling 5 needs text in a PNG in eight
    languages, and the standard library has no font rasteriser.
14. **Phones at 4096:** a hint that big sizes show their detail in the large
    view; nothing else.

The design below is the architect's, and it is the contract for every task.

## 0. Bugs and conflicts found while designing

1. **#278 root cause, almost certainly.** `internal/web/static/generate.js`, `reveal()` runs on `focusin` and sets `d.open = true` on
   every enclosing `details`. A first click on a planet `summary` focuses it, so `focusin` opens the row; then the click's own
   toggle closes it. On the second click the summary already has focus, so only the toggle runs. Fix: in `reveal()`, skip a
   `details` whose own `summary` is the focused element. Write the failing e2e test first.
2. **#277 cause.** `foreman.css` gives `.pictures img`, `.pictures .picture` and `.preview-placeholder__map` a
   `max-inline-size: 32rem`. DESIGN 8.8 says the same. Both change.
3. **#279 goes against DESIGN 8.7**, which already says "the slider has a line to itself". The flex-wrap rules in `foreman.css`
   (`.js-on .step`, at about line 1629) do not ensure it. A two-row grid (label and value, then the slider) is the fix.
4. **#280 is mostly there already.** On every line the cliff "Continuity" select already offers "None", and it writes exactly the
   keys the research found: `cliff_settings.richness = 0` on 1.1, and `autoplace_controls.<planet>_cliff.size = 0` on 2.x.
   So the checkbox is the resources' toggle pattern (`mgControl.toggle`, `data-disables`, the hidden "off" field,
   `f.resourceOn`). As with resources, the box replaces "None" in that select (mapgenform.go,
   `s.none && (!c.toggle || s.key != "size")`). No achievement warning.
   The one new write is for the 1.1 top-level cliff: when the box is off, `richness` must be 0.
5. **The preview store limits clash with 4096 (#284).** Space Age at 4096 is about 9 + 8.6 + 3 x 6 = about 35 MB for one user.
   SPEC 5.6 and `genpreview.go` allow 48 MB for all users. So one user with five planets fits, a second user evicts the first,
   and a territory result adds more on top. See choice C3.
6. **The large view's zoom range clashes with #284.** "100% to 800%" is relative to the fitted size. A 4096 picture fitted to
   about 1000 px reaches only 2 screen pixels per tile at 800%. See choice C4.
7. **Phones and 4096.** Five 4096 thumbnails decode to 64 MB each in the browser, 320 MB in all. A phone tab can crash.
   This is covered by #284's hint ("consider offering big sizes only in the large view"). I do not design it away.
   Recommended: the hint says so, and nothing else. Flag it for Todd.
8. **#274 conflicts with DESIGN 12.** DESIGN 12 defers "icons of any kind, except the map preview's pin" and "pop-overs".
   #274's arrows, X and download icon, and #276's info icon and toggletip, both need DESIGN 12 changed. The task that adds each
   one edits DESIGN 12 (see the ownership tables).
9. **#282's labels and Chinese.** A Go-drawn PNG needs a font with the glyphs of every language. Go's own fonts (Latin, Greek,
   Cyrillic) have no Chinese glyphs. See choice C6.
10. **#282 at 4096.** The territory run generates the whole picture's chunks. At 1280 that is 1,681 chunks in 1.7 s. At 4096
    it is about 16,900 chunks, so perhaps 15 to 20 s, with an unknown memory peak. The spike must measure it (choice C5b).
11. **#93: rollback already works on a world that is not active.** `game.Rollback` has no `checkActive`. `world.html` shows
    Rollback for any world. A stopped rollback runs inside the request (`handleRollback`). So rollback needs only tests and a
    SPEC line. Update is the real change.
12. **#275 and native exclusive accordions.** Within one `name` group the browser keeps only one `details` open. So "any row
    with an error is open" (DESIGN 8.7) becomes "the row with the first error is open". A focus or summary link reaching an
    error in a closed row opens it. That already happens through `reveal()`.

## 1. Per issue

### #265 Test port race
- Smallest fix, and the right shape too, since it is in a test harness and easy to undo: Foreman listens on port 0 and reports
  the port it got.
- `cmd/foreman/main.go`: replace `srv.ListenAndServe()` with `ln, err := net.Listen("tcp", cfg.Listen)`, then
  `slog.Info("listening", "addr", ln.Addr().String())`, then `srv.Serve(ln)`. The "listening" line is then logged after the
  bind, not before it as now. `config.validateListen` already accepts port 0.
- `e2e/tests/foreman.ts` `startForeman`: set `FOREMAN_LISTEN=127.0.0.1:0`. Read stdout line by line (`readline` is already
  imported) until the JSON line with `"msg":"listening"`, then take the port from `addr`. Keep the existing `/healthz` loop and
  the 10 s deadline. Delete `freePort()`.
- `e2e/game/phone.spec.ts` has its own copy of `freePort()`. Change it the same way if it binds on the host. Leave it if Docker
  networking needs a known port, and say so in the PR.
- Tests: a `cmd/foreman` unit test that `:0` logs a real port. Run `make e2e` twice with `--workers` raised.

### #272 make deploy
- Shell only. `Makefile` gets a `deploy` target and `-include local.mk`. `.gitignore` gets `/local.mk`.
- Steps: refuse when `DEPLOY_DIR` is unset; refuse a dirty checkout unless `FORCE=1`; `make image IMAGE=$(DEPLOY_IMAGE)`
  (default `foreman:dev`); `docker compose --project-directory $(DEPLOY_DIR) up -d $(DEPLOY_SERVICE)` (default `foreman`);
  wait for `docker inspect -f '{{.State.Health.Status}}'` to read `healthy`, up to `DEPLOY_TIMEOUT` (default 120 s, because
  the image's HEALTHCHECK interval is 30 s, Dockerfile line 17); print the image ID and `git rev-parse HEAD`.
- Docs: WORKFLOW.md "Local development", and STACK.md section 13.
- Test: run it by hand with and without `DEPLOY_DIR`, with a dirty tree, and with `FORCE=1`, and paste the output.

### #93 Update a world that is not active
- Game layer: `internal/game/update.go` drops `checkActive` from `Update`. The rest already copes. `planStop` gives
  `ErrNotRunning`, `stopLocked` is skipped, the snapshot and the version are set, and launch is skipped. The world lock
  (`c.worlds.Lock(id)`) keeps a switch out meanwhile.
- `precheckReadOnly` still runs. Keep it: the pre-start check of the new version is advice the person needs anyway.
- Web: `internal/web/update.go` `updatePage` loses the `!sum.Active` refusal. `world.html` line 74 shows Update for every
  world that is not Newer or Unreadable, and the comment at line 79 goes. Delete the `server.update.not_active` key.
- **Choice C1, where an update of a world that is not active reports its progress.**
  - (a) Smallest: run it in the background as now, but send the person to the world page (`beginTo(worldPath(id)+...)`). The
    world page already shows a busy note for the world's operation (`Busy` from `busyText`) with a link. Add the result
    (done, or the refusal or error) from the same `actionResult` the Server page reads. One template block plus one view field.
  - (b) Run it inside the request when the image is already held (seconds), and in the background with (a) only when it
    must download. Two code paths for one action.
  - (c) A progress page of its own, `/worlds/{id}/update/progress`, with the step list (`server-steps`) and live updates.
    It costs the most, and it adds a second step-list page.
  - Recommendation: (a). One path, reuses the busy note, and it is easy to undo.
- Rollback: tests only. Add a game unit test and an e2e test of rolling back a stopped world that is not active, and confirm
  that nothing starts.
- Tests: game unit tests (no stop or start, the snapshot taken, the version set, a switch to the world waits for the lock);
  an e2e test in `versions.spec.ts`; a `make test-game` update of a world that is not active on 2.0 to 2.1.

### #274 Large view as a picture viewer (with #277)
- Files: `generatepreview.html` (the `generate-large-view` define), `static/preview.js` (the large view part), `foreman.css`
  (the 8.8 large-view block), the locale files (worlds, js), `jsstrings.go`.
- Smallest shape: keep the scrolling frame (`.large-view__frame`, `--zoom`). Make zoom a continuous number: each wheel step
  or each + or - multiplies by 1.15, around the pointer (or around the centre from the keyboard), by changing `scrollLeft` and
  `scrollTop`. Dragging changes the scroll position (pointer events and `setPointerCapture`). Pinch tracks two pointers
  (`touch-action: none` on the frame). Arrows pan only while zoomed and focus is on the frame, as now.
  Because it scrolls, the pin and the overlay stay inside `.map` and keep their alignment with no maths.
  A rewrite to CSS `transform` was rejected: it would need its own clamping and its own pin and overlay maths, for no gain the
  issue asks for.
- Controls: `button`s placed over the frame. The edges get Previous and Next with the names "Previous planet: X" and
  "Next planet: X". The corner gets an X button named "Close". A download icon link sits beside the h2, named
  "Download X map". They show on `:hover`, `:focus-visible` and `@media (hover: none)`. Targets are 24 px, and 44 px with
  `(pointer: coarse)`. A `role=status` line gives the zoom as text after a keyboard zoom. With `prefers-reduced-motion`
  nothing animates. Icons are inline SVG with `aria-hidden`; DESIGN 12 changes.
- #277 in the same task: remove the `32rem` caps. For a square picture use
  `inline-size: min(100%, calc(100dvh - <controls>))` with `margin-inline: auto`, and the same rule on the held box.
  Keep `image-rendering: pixelated` on the thumbnail too. The "Preview map" grid of several pictures stays
  `repeat(auto-fit, minmax(16rem, 1fr))`.
- Interface for #275 (fixed in the plan): when the large view or the planet select changes the planet viewed, preview.js runs
  `document.dispatchEvent(new CustomEvent("preview:planet", {detail: {planet}}))`. preview.js starts `viewing` from the hidden
  field `#open-planet` when it is set (see #275).
- Tests: e2e for wheel steps, keyboard zoom and reset, drag pan, pinch (Playwright touch), arrows on hover, focus and touch,
  the X, the download icon, axe in both themes at desktop and phone widths, the bounding-box widths of #277 at 1280, 1920 and
  320, and the held box equal to the picture. Update `ui-fixes.spec.ts` and `generate-preview.spec.ts`.

### #275 One planet open at a time, shared across tabs, preview follows (with #278)
- Files: `mapgenform.go` (MapGenPlanet gets `Planet` and the open rule), `mapgenform.html` (`mapgen-planets`:
  `<details name="planet-{{tab}}" data-planet=".." data-said="Preview: Fulgora">`, and a hidden `open_planet` input inside
  `#map-settings`), `generate.go` or `mapgenform.go` (read `open_planet`; only a planet the line has counts), and
  `static/generate.js`.
- One `name` group per tab, not one for the whole form. A single group across tabs would close the other tab's row. The
  script mirrors the open planet into the other tab's group. Without script, each tab is an exclusive accordion, and the round
  trip keeps the planet through `open_planet`.
- Open rule on the server: the planet with the first error, else `open_planet`, else the first planet.
- generate.js: on `toggle` (open) of a planet row, set `#open-planet`, open the same planet in the other tabs, and, if
  `#preview-planet` exists and differs, set it and dispatch `change`. The existing preview.js handler then draws or reuses the
  picture. Announce `data-said` once through `#announcer`; a tab switch says nothing. On `change` of `#preview-planet` and on
  `preview:planet`, open that planet's row in the shown tab without moving focus. The #278 fix goes in `reveal()`.
- No preview.js edit in this task. The `preview:planet` event and reading `#open-planet` belong to the viewer task (V-a).
- Tests: e2e for exclusive open; carry-over between tabs and over an error round trip; the preview switching; a planet chosen
  in the preview opening its row; the #278 test (click once soon after load and after the first preview; the row is still open
  after 2 s); axe in both themes; keyboard only at 320 px.

### #276 Info tips on Enemy and Advanced, drop x 10^-n (with #279, #281)
- Files: `mapsettingsform.go` (a `Tip i18n.Msg` per field and group; drop `Unit` for the evolution and pollution factors),
  `mapgenform.go` (tips for the Enemy tab's controls, the starting area, No enemies, Peaceful, and Width and Height),
  `mapgenform.html` (one `toggletip` partial used by `ms-group`, `mapgen-select` and `mapgen-check`), a new
  `static/toggletip.js`, the script tag in `generate.html`, `foreman.css`, and mapgen locale. Delete `mapgen.unit.e5` and `e7`.
- Toggletip: `<button type="button" class="tip" aria-expanded aria-controls>` with an (i) SVG and a name such as
  "About Time factor". The text sits in `<p id="..-tip" class="tip__text">`, and the field's `aria-describedby` includes it.
  Without script the text is visible under the field (the class shows it unless `js-on`). The script toggles it on click, tap
  and focus, shows it on hover, closes it on Escape or an outside tap, and keeps it inside the viewport near the right edge.
  It is a new file, so generate.js stays out of it. DESIGN gets a component section, and the pop-overs line in 12 goes.
- Text: the game's own `*-description` tooltips from `core/locale/<lang>/core.cfg` in the 2.1.24 image, as I18N.md 2 does for
  labels. The builder reads them at build time with `docker run --rm --entrypoint cat`, not at runtime. Where the game has
  none, plain words, translated in the late translation task.
- #279: `.js-on .step` becomes a grid, `grid-template-columns: minmax(0,1fr) auto`, with areas "label value" and then
  "range range" and "ach ach". The value has a reserved `min-inline-size` in `ch` (tabular-nums), long enough for German and
  Russian "(default)".
- #281: diagnose first, in Chromium and Firefox. Native ranges with `accent-color` normally reach both ends. If the gap is
  real, the smallest fix is CSS-only: `appearance: none`, a thumb of known size, and a track whose fill is a gradient from
  `--fill`, which needs one line in `syncSlider`. That line is in generate.js, which this wave's task owns. The e2e test reads
  geometry from the thumb's known size, because pseudo-elements have no bounding box.
- Tests: e2e for hover, focus, tap and Escape on tips; no "x 10" text left; axe in both themes at both widths; slider box does
  not move while stepping at 1280 and 1920, in de and ru; first and last step of every slider kind in both themes and at 200%
  zoom.

### #280 Cliffs off
- Data (F): `internal/mapgen/cliffs.go`, `CliffToggle(line, planet string) bool`. True for Nauvis on every line, and for
  Gleba and Fulgora on 2.x with Space Age; never for Vulcanus. Its comment cites the research: 1.1
  `cliff_settings.richness = 0`; 2.x `autoplace_controls.<planet>_cliff.size = 0`; never `cliff_settings.richness` on 2.x,
  because it reaches every planet. Unit test per line and planet.
- Form (G): in `mgPlanets` and `controlFor`, set `toggle = true` on cliff controls where `CliffToggle` is true. The present
  code then gives the checkbox, the live disable, "[modified]", `knownFieldNames` and the 2.x write (`entry["size"] = 0`).
  Add the 1.1 top-level case: when the box is off, `cliff["richness"] = 0`. A posted or decoded continuity of 0 shows as
  "box off". The preview follows by itself, because it reads the same files.
- Tests: unit tests of the written keys per line and planet (G); e2e for the box, the disable and "[modified]" (G); real
  games on 1.1.110, 2.0.77 and 2.1.24 in the final real-game check.

### #282 Demolisher territories
Split: D builds the backend in wave 1; V-b builds the UI in wave 2.

**C5, the container: a control script, not RCON.** RCON is ruled out. The preview container has no network, so Foreman cannot
reach a port in it. The image has no RCON client, and exec is forbidden by SPEC 14.3. A script that runs inside the game is
the only way that keeps 14.3 as it is.
- (a) Recommended, if the spike shows it works: one `--create` with a small mod. Put `mods/foreman-territories_1.0.0/`
  (`info.json`, `control.lua`) in the work folder and add it to `mod-list.json`. `script.on_init` creates the Vulcanus
  surface, requests the chunks of the picture's area, forces them to generate, reads `get_territories()`, each territory's
  `get_chunks()` (clipped to the picture) and `get_segmented_units()[1].prototype.name`, and writes JSON with
  `helpers.write_file` into `script-output` on the tmpfs. The game saves and exits by itself. The shell then copies the file
  to `/out`. One process, nothing to poll or kill, no server settings, no auto-pause. It takes the same arguments as the
  preview (`--preset`, seed, both settings files), so it is the same map.
- (b) Fallback, if demolishers appear only after ticks: `--create`, then `--start-server` of that save with the same mod and a
  `server-settings.json` with `auto_pause: false`. `on_tick` at tick 2 or later writes the data and then a `done` file. The
  shell polls for `done`, copies the data and kills the server. More moving parts, still inside 14.3.
- (c) Rejected: `--start-server-load-scenario`. It is not proven to honour `--preset`, and the scenario folder would have to
  be copied onto the tmpfs.
- The spike's first job is to show (a) or (b) on 2.1.24 with seed 123456 and size 1280: 25 territories, 12 small and 13 medium.
  It then measures 4096: time, peak memory and the save's size in the work folder. A control-only mod adds no prototypes, so
  the map is unchanged; check this against the picture.
- **C5b, if 4096 is too slow or too big:** (i) limit the checkbox to sizes the spike shows fit in 60 s and 1 GiB, with a hint;
  or (ii) read territories only within 1280 at any size. Recommended: (i) if the spike finds a cut-off, otherwise nothing.

**Where it goes:**
- `internal/territory/` (new package, pure, unit-tested):
  - `Parse(b []byte, size int) (Result, error)` checks the game's JSON. The game container is untrusted (14.1). Limits: at
    most 2 MiB, at most 1,000 territories, chunks inside the picture, a known unit name or none. Out-of-picture chunks are
    clipped.
  - `Geometry(Result, size) Overlay` gives the cells per territory as row runs, the border segments between different owners
    (including the free start area), the label point (the mean chunk centre inside the picture) and the counts per size.
    Pixel = tile + size/2, and chunk c covers tiles 32c to 32c+31.
  - `DrawPNG(src []byte, ov Overlay, label func(size string) string) ([]byte, error)` draws the overlay into the picture.
- `internal/game/territories.go` (new): `(*Controller) Territories(ctx, PreviewRequest) ([]byte, error)`. It shares the one
  preview slot, the stale cleanup, the work folder and the container limits through helpers that E pulls out of preview.go
  (below). The Lua and `info.json` are embedded files beside it.
- `internal/gameenv/territories.go` (new file in the same package, so it does not clash with E's edit of `gameenv.go`):
  `TerritoryRadius` and `PreviewKind`.
- `internal/fakegame/territories.go` (new) writes a canned result. `instance.go` gets one dispatch line.
- The PNG renderer (C6 is the font choice): stripes are drawn per pixel. A pixel inside a territory with
  `((x + y) mod 56) < 28` is blended with rgb(200,20,20) at alpha 85. Borders are a 2 px run of rgb(255,30,30) along each
  segment. Labels are white bold text with a dark outline, made by drawing the text 8 times at offsets of plus or minus 1 or
  2 px in near-black, then once in white. Encode with `png.Encoder{CompressionLevel: png.BestSpeed}`. Decoding a 4096 picture
  takes 64 MB, so a one-slot semaphore lets only one render run at a time.
- Cache (`genpreview.go`, V-b): `previewEntry` gets `territories *territory.Result` under the same settings key, which now
  includes the size. Its byte count goes into `e.bytes`. New settings drop it with the pictures.
- Routes (V-b, new `internal/web/genterritories.go`):
  - `POST /generate/preview/territories` takes the same form, answers from the cache or runs the container, and returns the
    `generate-territories` fragment.
  - The download routes take `?territories=1` and draw the overlay PNG on request, without caching it. The zip draws it for
    Vulcanus only.
  - The rendered file can exceed 16 MiB. The cap is on what Foreman reads from the game, not on what it sends.
- Page (V-b):
  - The `territories` checkbox sits in `generatepreview.html` beside the planet select, shown only for Vulcanus on Space Age.
    It is a named field, so "Preview map" without script also draws territories after the pictures, in the same request.
  - With script, the territory request goes out after the picture's reply, and the newest wins as for pictures. It is
    cancelled when the settings change or the planet viewed moves away from Vulcanus.
  - The overlay is an inline `<svg viewBox="0 0 size size" preserveAspectRatio="none">` inside the `.map` span: a
    `<pattern>` 56 wide with `patternTransform="rotate(45)"`, the runs as rects, and the borders as one path with
    `vector-effect="non-scaling-stroke"`. Labels are HTML spans placed by percent at a fixed font size, like the pin, so they
    do not grow with zoom. The large view clones the overlay into its `.map`.
  - The list under the picture is a `<p>` such as "12 small, 13 medium".
  - The download link gets `?territories=1` while the box is on.
- Tests: unit tests (Parse limits, chunk to pixel, clipping, borders, label point, PNG pixels at known points); real game on
  2.1.24 and 2.0.77 (D); e2e for the box, the overlay after the picture, the list and the download URL (V-b); axe; sizes in
  words.

**C6, the font for PNG labels** (adding a dependency, so stated out loud):
- (a) Recommended: `golang.org/x/image` (the Go team's, pinned), with `font/opentype` and `font/gofont/gobold` for Latin and
  Cyrillic, plus a committed subset of Noto Sans SC Bold (OFL licence, a few KB) holding only the glyphs of the zh-Hans label
  strings. A unit test checks that every glyph of every language's label strings is in the fonts, so a translation change
  that needs a new glyph fails `make verify`. Labels stay in every language, and it adds about 1 MB to the binary.
- (b) No dependency: hand-drawn bitmap glyphs for the needed characters. Ugly, a lot of work across eight scripts, and
  fragile when a translation changes.
- (c) Draw size marks (1, 2 or 3 bars) instead of words in the PNG. No fonts, but the PNG then differs from the screen and
  needs a legend.
- (d) English labels in the PNG for zh-Hans only. Cheap, but a visible gap for one language.

### #283 Warn when a Nauvis resource the game needs is off
- Data (F): `internal/mapgen/needs.go`, `NeededResources(line string, spaceAge bool) []string`, the Nauvis control names:
  iron-ore, copper-ore, stone, crude-oil, and coal, because plastic needs coal on every line. Not uranium. Space Age keeps every
  warning, because reaching space needs them first; the code says why per resource. Unit tests per line and with and without
  Space Age.
- Form (G): on those toggles, `data-need` and a `<p class="ach" id="..-need">` (reusing the warning style). One
  `message--warning` summary next to `#ach-summary` in `generate.html`. In generate.js, `refresh()` shows and hides the
  warnings as it does for `data-ach`, and announces once each way. The server renders the same warnings on every page. The
  text is conditional: "...unless a mod provides it".
- Tests: e2e for the warning appearing and clearing live, and in the summary.

### #284 Preview size
- Ruled: the select of steps on phones; MaxPreviewPNG is 16 MiB (worst case measured: Gleba 8.91 MB at 4096); memory stays
  1 GiB (peak 553 MiB; slowest run 4.5 s).
- Backend (E):
  - `internal/game/preview.go`: `PreviewRequest.Size int`; `checkPreview` takes only 1024 to 4096 in steps of 128; the
    script reads `$FOREMAN_PREVIEW_SIZE` instead of the constant; `MaxPreviewPNG = 16 << 20` with the reason in a comment.
  - `gameenv.go`: `PreviewSize`.
  - `fakegame/create.go`: draw at that size.
  - `genpreview.go`: `settingsKey` includes Size; `PreviewView.Size` replaces the hard-coded `1280` in
    `generatepreview.html` (thumbnail and large view).
  - `generate.go`: read `preview_size` for kindPreview and keep it in `GenerateForm` for the round trip. A bad value is a field
    error.
  - E also pulls helpers out of preview.go for D, with these signatures fixed in the plan:
    `runOneOff(ctx, name string, spec docker.ContainerSpec) error` (today's poll loop) and
    `oneOffHostConfig(dir string) docker.HostConfig`. `previewSpec` uses both. D codes against them from E's head.
- UI (E): a `<div class="field step">` with a select of 25 options ("1280 × 1280 tiles (default)") in `generate.html`, inside
  `.gen-preview` but outside `#preview`, so a reply never replaces it mid-drag. generate.js's `addSliders` turns it into a
  slider on a wide panel by itself, and preview.js's `counts()` already redraws 0.8 s after it changes. No CSS. The hint says
  that big sizes show their detail in the large view.
- **C3, the store limits:**
  - (a) Raise the total to 96 MiB and refuse nothing for one user's set: 5 x 16 MiB plus the territories fits.
  - (b) Keep 48 MB, and at 3072 and above keep only the planet viewed.
  - (c) Keep 48 MB and accept that a second user evicts the first.
  - Recommendation: (a). It is easy to undo, and it is only memory for 15 minutes. SPEC 5.6 and 14.6 change at the fold.
- **C4, the zoom range** (#274 with #284): the most zoom is the larger of 8 x fit and 8 CSS px per tile, shown as a
  percentage of fit. The alternative, keeping 800% of fit, leaves a 4096 picture at about 2 px per tile. Recommended: the first.
- Tests: unit tests of the size argument, refusals off the steps, and the key including the size; e2e for changing the size,
  the redraw, and the round trip; real game at 4096 on the three lines in the final check.

## 2. Waves and ownership

A task that needs a shared file it does not own stops and reports. SPEC.md and STATUS.md are the fold's only. A task's DESIGN
text that it does not own goes in its PR body for the fold.

| Wave | foreman.css | layout.html | generate.html | generatepreview.html | preview.js | generate.js | jsstrings.go | Locale areas | DESIGN.md | e2e helpers (foreman.ts) |
|---|---|---|---|---|---|---|---|---|---|---|
| 1 | none | none | E | E | none | none | none | C: server. E: worlds. | E (8.8 size line) | A |
| 2 | V | none | G | V | V | G | V | G: mapgen. V: worlds, js. | V (8.8, 12) | V |
| 3 | I | none | I | none | none | I | I | I: mapgen | I (7.x new, 8.7, 12) | I |
| 4 | none | none | none | none | none | none | none | X: every area, all 8 languages, translated/ | none | none |

Other shared files: `genpreview.go` (E in wave 1, V in wave 2); `generate.go` (E in wave 1, G in wave 2); `mapgenform.go` and
`mapgenform.html` (G in wave 2, I in wave 3); `mapsettingsform.go` (I); `preview.go` (E); `gameenv.go` (E; D has its own
file); `fakegame/create.go` (E); `fakegame/instance.go` (D); `go.mod` and `go.sum` (D); `Makefile`, `.gitignore`,
WORKFLOW.md and STACK.md (B).

Tasks write English into all eight locale files and run `make i18n-hash`, so `make verify` passes. X writes the real
translations.

### Wave 1, from 08e20b7, six in parallel (D's game wiring starts from E's head)

| Task | Owner, model | Files | Issues | Done means |
|---|---|---|---|---|
| A: listen on port 0 | builder, Sonnet high | `cmd/foreman/main.go`, `main_test.go`, `e2e/tests/foreman.ts`, `e2e/game/phone.spec.ts` | #265 | the unit test; `freePort` gone; two full `make e2e` runs with no bind failure |
| B: make deploy | builder, Sonnet high | `Makefile`, `.gitignore`, WORKFLOW.md "Local development", STACK.md 13 | #272 | the four hand runs in the PR body |
| C: update a world that is not active | builder, Sonnet high once C1 is ruled (Opus low if not) | `internal/game/update.go` and tests, `internal/web/update.go`, `update_pages.go`, `worlds.go` (world page result), `templates/update.html`, `world.html`, server locale, `e2e/tests/versions.spec.ts` | #93 | game and web unit tests; e2e for update and rollback of a world that is not active; `make test-game TAG=2.1` |
| D: territories backend | builder, Opus medium (the spike first, report before building) | new `internal/territory/**`, new `internal/game/territories.go` with embedded Lua and info.json and tests, new `realgame2_territories_game_test.go`, new `internal/gameenv/territories.go`, new `internal/fakegame/territories.go`, `fakegame/instance.go`, `go.mod`, `go.sum`, the font file and its NOTICE | #282 (part) | the spike's report (C5 a or b; 4096 cost); unit tests of Parse, Geometry and DrawPNG; real game on 2.1.24 and 2.0.77 |
| E: preview size | builder, Sonnet high | `internal/game/preview.go` and tests, `internal/gameenv/gameenv.go`, `internal/fakegame/create.go`, `internal/web/genpreview.go`, `generate.go`, `templates/generate.html`, `generatepreview.html`, worlds locale, `e2e/tests/generate-preview.spec.ts`, DESIGN 8.8 (size line) | #284 | the unit tests, e2e and real-game 4096 run listed above; C3 applied as ruled; the helpers D needs, with the fixed signatures |
| F: mapgen tables | builder, Sonnet high | new `internal/mapgen/cliffs.go`, `needs.go` and their tests | #280 (part), #283 (part) | unit tests per line, planet and Space Age |

### Wave 2, from the batch head after wave 1

| Task | Owner, model | Files | Issues | Done means |
|---|---|---|---|---|
| G: planet rows, cliffs box, needed resources | ui-designer, Opus low | `internal/web/mapgenform.go`, `generate.go` (open_planet), their tests, `templates/mapgenform.html`, `generate.html` (needs summary), `static/generate.js`, mapgen locale, new `e2e/tests/planet-rows.spec.ts`, `generate.spec.ts` | #275, #278, #280, #283 | the e2e and unit tests above; axe; the #278 test fails before the fix |
| V: viewer, two PRs in order (V-a large view and width; V-b territories UI) | ui-designer, Opus medium | `static/preview.js`, `templates/generatepreview.html`, `foreman.css`, `jsstrings.go`, new `internal/web/genterritories.go`, `genpreview.go` (cache and view), worlds and js locale, `e2e/tests/generate-preview.spec.ts`, `ui-fixes.spec.ts`, new `e2e/tests/large-view.spec.ts` and `territories.spec.ts`, e2e helpers, DESIGN 8.8 and 12 | V-a: #274, #277. V-b: #282 (UI) | the e2e above; axe in both themes at both widths; the `preview:planet` event and `#open-planet` read |

G and V meet only through the event and hidden-field contract in section 1, #275. Whichever lands second runs both suites.

### Wave 3, from the batch head after wave 2

| Task | Owner, model | Files | Issues | Done means |
|---|---|---|---|---|
| I: tips and sliders | ui-designer, Opus low | `internal/web/mapsettingsform.go`, `mapgenform.go`, their tests, `templates/mapgenform.html`, `generate.html` (script tag), new `static/toggletip.js`, `static/generate.js` (`syncSlider` only, if #281 needs it), `foreman.css`, `jsstrings.go`, mapgen locale, new `e2e/tests/tips.spec.ts` and `sliders.spec.ts`, DESIGN 7.x, 8.7, 12 | #276, #279, #281 | the e2e above, in de and ru, both themes, 200% zoom |

### Wave 4: translation
- X: builder, Sonnet high. Every locale key added or changed in waves 1 to 3, in the seven other languages (I18N.md); game
  tooltip text from the game's own locale where it exists; `make i18n-hash`. Then a second agent (Opus low) reviews per
  SPEC 22.6, as in #263.

### Wave 5: checks and fold
- Reviews in parallel on the batch head:
  - code-reviewer (Opus).
  - security-reviewer (Opus): the territory container against 14.3, the parsing limits on the game's JSON, the render
    semaphore, the `?territories=1` routes, the update without the active-world check, and `make deploy` naming no deployment.
  - a11y-reviewer: the large view, the toggletips, the accordion, the sliders and the overlay.
- Fix round, then each reviewer confirms.
- Real-game check (tester, Sonnet high): cliffs off on 1.1.110, 2.0.77 and 2.1.24 per planet; 4096 previews on the three
  lines; territories on 2.1.24 and 2.0.77; an update and a rollback of a world that is not active.
- Fold:
  - SPEC 5.6: the size; one planet open; cliffs off; needed-resource warnings; territories; the store limit from C3; the zoom
    range from C4.
  - SPEC 10.3 and 10.4: a world that is not active can be updated and rolled back.
  - SPEC 14.3: the territory container and the reason it is not RCON.
  - SPEC 14.6: the 16 MiB picture cap and its reason.
  - SPEC 17: the toggletip, the viewer controls and the touch targets.
  - DESIGN text from G's PR body.
  - ADR 0011: territories by an in-game script, and the font choice.
  - STATUS; `Fixes #N` for each issue; delete the plan.

## 3. Left out
- A CSS `transform` large view (scroll keeps the pin and the overlay aligned for free).
- Caching the overlay PNG (it is drawn per download, one at a time).
- Detecting mods that supply a resource (#283 says not to).
- Any change to `--generate-map-preview` itself, or territories on 1.1 (it has none).
- Splitting `foreman.css` (DESIGN 11 says one file). This is why the CSS tasks run in separate waves.
