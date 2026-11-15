# Fixes batch 9 plan

Branch `epic/fixes-9`, cut from main 9804cc0 on 2026-11-14. Todd approved
running it straight through to a green epic pull request into main, with
no stop after this plan.

## Rulings

1. **Search order (#549, SPEC 9.1).** Each word typed is matched on its
   own, in any order, against a mod's name and title. A mod whose name is
   exactly the typed text (ignoring case) comes first; every other match
   is ordered by downloads, most first, then by name.
2. **As you type (#549).** The search starts at 2 characters, after a
   300 ms pause in typing. Only the results are replaced; focus stays in
   the search box; the address bar does not change while typing. A screen
   reader hears the result count once typing pauses, through the shared
   announcer. Without script the form works as it does today.
3. **Pasted link (#551, SPEC 9.1, 9.11).** A mods.factorio.com link (with
   or without scheme or `www.`, with a sub-page, a query or a trailing
   slash) is read for the name after `/mod/`. The mod is looked up by
   exact name in the cached list, then with the existing `/full` call, and
   shown as one result row. "No such mod" and "no release for this game
   line" are told apart. Any other text is ordinary search. Nothing is
   fetched from the link.
4. **Portal link (#552).** A plain link to
   `https://mods.factorio.com/browse/downloaded?factorio_version=<line>`
   opens in a new tab and says so in its name. It sits on the search page
   and the Mods tab's Find a mod form, with a hint that a pasted link works.
5. **#206** is deferred. **#207** is closed as won't build. **#547** waits
   for its own feature epic. **#92**, **#132** and **#353** are out.

## Tasks

| Task | What | Issues and gaps | Agent, model | Files it may touch |
|---|---|---|---|---|
| M1 | Word matching and the order in ruling 1 | #549 | builder, Sonnet high | `internal/modportal/search.go`, `search_test.go`, e2e specs that assert the old order |
| M2 | Pasted link (ruling 3); Mods tab no longer says "0 up to date" while a world is briefly away | #551, STATUS fixes batch 5 gap | builder, Sonnet high | new `internal/modportal/link.go` and test, `internal/web/mods.go` and tests, `mods.html` (that wording only), locale files, `cmd/fakeportal` |
| M3 | As you type (ruling 2) and the portal link (ruling 4); starts from M2's verified head | #549, #552 | ui-designer, Opus low | `modsearch.html`, `mods.html`, new `static/modsearch.js`, locale files, `mods-search.spec.ts`, DESIGN search section |
| G1 | Large map view: the settings hint is reached by Tab, Close comes earlier in Tab order, arrow keys do not end a preparation, Try again after a failed territory request | STATUS batches 5, 6, 7 gaps | ui-designer, Opus low | `internal/web/static/preview.js`, the specs that open the large view |
| G2 | The error summary marks English setting names `lang="en"` | STATUS mod settings gap | builder, Sonnet high | `layout.html` (summary template), `internal/web/render.go` and tests |
| G3 | A scheduled restart sends no extra start webhook; Start no longer reports an earlier run's failed load | STATUS Epic 9 and batch 5 gaps | builder, Opus low | `internal/schedule`, `internal/notify`, `internal/game` (start and crash reasons) |
| G4 | The mod settings reader mounts world mods read-only; editing a folder mod refreshes its cached definitions | STATUS mod settings gaps | builder, Opus low | `internal/modsettings`, the container mount code it uses |
| F | Fold: STATUS section, SPEC 9.1 text, DESIGN 8.6 and 8.8 preparation block, delete this plan, `Fixes` lines | | builder, Sonnet | docs only |

M1, M2, G1, G2, G3 and G4 start together. M3 chains on M2 (both touch
`mods.html` and the mod search handler). G2 touches `layout.html`, which
every page uses, so its check runs the whole browser suite.

## Checks

Each task PR: the orchestrator's full `make verify` and the browser specs
for every touched file, then merge. Code review on every task;
a11y review on M2, M3, G1 and G2; security review on G4. M2, M3 and G4
also get a real `factoriotools/factorio` container check before the epic
pull request. After the last task: an integration review, then the fold,
then the epic pull request into main with CI green.
