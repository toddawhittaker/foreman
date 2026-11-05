# Server page hub fix batch

The first of two fix batches before i18n (#87). It makes the Server page
the hub for the active world (SPEC 17.7 to 17.10, umbrella #157) and
fixes the switch and stop flows. Branch `epic/hub-fixes`, cut from main
f26eb0c. No design step (fix batch); the architect grouped the issues.

## Rulings (Todd, 2026-11-05)

1. Two batches. This one is the Server page hub. The second (import,
   snapshots, sign-in and times) follows on its own branch.
2. A switch to a world whose start checks find problems (#145, #128)
   offers two buttons on the confirmation page: "Switch and start
   anyway" and "Switch, leave stopped". With "leave stopped", the
   problems are shown at the next Start. Nothing asks twice.
3. Stopping (#143): "Cancel stop" while the countdown runs, audited as
   `stop-cancel`. Cancelling a restart, switch, restore or update ends
   the whole action with nothing changed; a finished download stays. Of
   the two "no countdown" controls, keep the seconds field (0 means no
   warning, SPEC 7.6) and drop the checkbox.
4. The Server page lists each other world with its own "Switch to X"
   button (#141).

Orchestrator rulings:

- The switch downloads a missing image before the switch record begins
  and before the stop, as the update flow does (`game/update.go`). A
  failed download leaves the old world running and untouched (fail
  safe). The SPEC 7.10 step record is unchanged.
- The switch refusal rules live in one controller method that both the
  confirmation page and the switch call (closes Gate A review S3).
- #129 is fixed by #128's flow; it still gets its own regression test.
- After a crash loop (#147), "Switch back" uses the in-memory switch
  result. After a Foreman restart only Restore is offered; no schema
  change.
- New routes are keyed by world id, never "the active world", so
  several running worlds stay possible.

## Tasks

| Task | Owner, model | Files it may touch | Closes |
|------|--------------|--------------------|--------|
| T1. Switch flow | builder, Opus | `internal/game/switch.go` and tests; new `internal/game/image.go`; `internal/releases` (image size); `internal/web/switch.go`, `serverpage.go`, `game.go`, `server.go` (`Deps` gets the host data folder); `cmd/foreman/main.go`; `server.html`, `switch.html`; `e2e/tests/foreman.ts`; new `e2e/tests/switch-flow.spec.ts`; existing `switch-page.spec.ts`, `server.spec.ts` | #128, #129, #125 (Server page), #138 (switch half), #136, #145, #142 (refusal point) |
| T2. Game layer | builder, Sonnet high | `internal/game/actions.go` (`CancelStop`, countdown end time, players during countdown), `controller.go`, `container.go`, `players.go`, `monitor_test.go`; `internal/docker/client.go`; Go tests only | #126, #144, #109, #143 (game side) |
| T3. World pages | builder, Sonnet high | `internal/web/worlds.go` (world page, list, rename, delete only), `render.go` (`WorldsPage`), `world.html`, `worlds.html`, `delete.html`, `foreman.css`; `worlds.spec.ts`, `delete.spec.ts`, new `worlds-hub.spec.ts` | #141 (world page and list), #146, #138 (delete half), #154 (rename Done message) |
| T4. Server page results, controls and stopping (wave 2) | builder, Opus | `serverpage.go`, `web/game.go`, `web/switch.go`, `web/snapshots.go` (snapshot from the Server page), `web/live.go`, `server.html`, `world.html` (image notice), `stop.html`, `forcestop.html`, `restore.html`, `switch.html`, `layout.html` (`countdown` block only), `foreman.css`, `foreman.ts`, new e2e spec | #140, #147, #152, #137, #141 (Server page), #143 (web side), #125 (world page) |
| T5. Phone layout (wave 3) | ui-designer, Opus | `layout.html` (header), `foreman.css`, `server.html` (order and step list only; no button names change), `console.html`, `log.js`, `worlds.html` (compact at 320 px), DESIGN.md 6 and 7.6 | #142, #153, #155 (Worlds list and Force stop points) |
| T6. Click budgets (wave 3) | tester, Sonnet high | new `e2e/tests/click-budgets.spec.ts` and `e2e/tests/clicks.ts` only | #157 (all eight SPEC 17.10 budgets; the mod-change row waits for Epic 6) |
| F. Fold (last) | builder, Sonnet high | `docs/SPEC.md` (3.7, 7.6, 7.9, and 14 if needed), `docs/STATUS.md` (this batch; closes the "switch anyway has no countdown field" gap), `docs/WORKFLOW.md` (the auto-merge line under "Pull requests"), delete this plan | `Fixes #N` for each issue |

Contracts between tasks:

- T1 does not edit `game/actions.go`, `game/controller.go` or
  `foreman.css`. It may call `gameImage`, `checkLocked` and
  `StepDownload` without changing them, and keeps `s.switchBusy`,
  `switchPath` and `s.game.Status` (T3 uses them).
- T2 builds the stop cancel inside `stopLocked`, returning a distinct
  error before `/server-save`, so switch, restart and update callers need
  no change.

## Waves and checks

Wave 1: T1, T2, T3 in parallel. Wave 2: T4, after T2 lands (it uses
T2's new API). Wave 3: T5 and T6 (T6 uses role and name selectors only;
T5 keeps every button name). Then code-reviewer and a11y-reviewer on the
epic head, the real-game check (`make test-game`, `make e2e-game`) on a
switch that downloads an image, F, and the batch PR to main.
