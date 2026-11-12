# Epic 9 plan: Live operations

Branch `epic/9-live-ops`, cut from origin/main 0bdb6f5. Serves SPEC 7.12,
8.2, 8.3, 8.5, 8.7 (naming the reason), 8.8 and 8.10 to 8.13, with 14.8,
14.9, 14.10, 17 and 22. VPN sidecars (4.13, ADR 0006) move to their own
epic (ruling 1).

## Rulings (Todd, 2026-11-12)

1. **Sidecars split out.** VPN sidecars become their own epic, listed in
   SPEC 18 as "Epic 9b: VPN sidecars" so Epics 10 and 11 keep their numbers.
   Foreman keeps refusing to start when `FOREMAN_GAME_NETWORK_CONTAINER`
   is set. The fold updates SPEC 4.13, 18.9 and the ADR 0006 status line.
2. **Scheduled restart flow.** At the set time Foreman waits until nobody
   is online (counted by `/players online`, as in 8.1), then restarts after
   a 60-second countdown. Someone who joins during the countdown triggers
   the existing Cancel stop, which tells them in chat, and the wait starts
   again.
3. **Skip limit.** One hour by default, settable from 0 to 12 hours; 0
   means skip at once if anyone is online.
4. **One schedule for all of Foreman.** A single schedule, on a Foreman
   settings page beside the factorio.com account, applies to whichever
   world is active. The Server page shows the next restart time.
5. **Webhook URLs are sealed** with `FOREMAN_SECRET_KEY`, as the stored
   factorio.com account is (ADR 0013). Without the key, webhooks are
   unavailable. Pages show only the preset and the host; changing a URL
   means typing it again. URLs join the secrets list in 14.8.
6. **Low disk** warns below 4 GB free (twice the 6.11 reserve) and clears
   only above 4.5 GB, so it does not flap. Fixed, not a setting.
7. **Notification language** is chosen per webhook and defaults to the
   language of the admin who adds it.
8. **Whitelist enforcement** is decided after task 1 captures whether an
   in-game `/whitelist enable` survives a restart. If it does, the page
   says the admin turns it on in game. If not, the result goes back to
   Todd before task 4 builds the whitelist editor.
9. **No snapshot for live player actions.** A kick, ban, promote or
   whitelist change sent over RCON to a running game takes no snapshot,
   because the game writes the file itself. Editing the lists of a stopped
   world takes the usual settings snapshot (11.3).
10. **Confirm kick and ban only.** Promote, demote, unban and whitelist
    changes act at once and offer Undo (17.9).
11. **Chat has its own tab**, beside Overview and Console.

Design choices made by the orchestrator, from the architect's
recommendations:

- Webhooks belong to Foreman as a whole, and every event names its world.
- The generic webhook body is versioned from the start:
  `{"version":1,"event":"<stable id>","time":"<RFC 3339>","world":{"id","name"},...}`,
  documented in the operator guide, with a golden-file test.
- `internal/notify` is a leaf package that does not import `game`; the
  controller gets it through `SetNotifier` and emits without blocking.
- The scheduler is a leaf package `internal/schedule` that uses only the
  controller's public methods.
- "Out of memory" is named only from Docker's `OOMKilled` flag. The game
  container has no memory limit, so a host-wide kill shows as an unknown
  crash; STATUS records this gap.
- "Port in use" is named from Docker's start error ("port is already
  allocated"), not from the log.
- Not built: a "send test notification" button, signed webhook bodies,
  days of the week for a schedule, "skip tonight", keeping undelivered
  webhooks across a Foreman restart.

## Invariants every task tests

- A player name with a space, a control or invisible character, or over
  64 bytes is refused, never escaped (14.9). A reason or chat message is
  one line with no NUL, CR or LF.
- Every player action and chat message is audited before it is sent; if
  the audit fails, nothing is sent.
- A chat message starting with `/`, after any spaces, is never run as a
  command.
- Emitting an event never blocks or delays a server operation, even when
  a target hangs or the queue is full (8.12). A failed delivery is logged
  and retried at most twice; no redirect is followed.
- After a Foreman restart no event is sent again for something that
  happened before it.
- Discord messages set `allowed_mentions` to none.
- A scheduled restart never starts a stopped, crashed or crash-looping
  game, never uses "start anyway", treats an RCON failure as "someone is
  online", and does not catch up a restart missed while Foreman was down.
  A daylight-saving gap runs at the next valid minute; a repeated hour
  runs once.
- Crash patterns match message text, never `File.cpp:NN`; every
  catalogue entry has a fixture captured from a real game; no match means
  "unknown".

## Tasks

Base for wave 0 is the plan's merge commit on the epic branch. Each later
task starts from the verified head named in its prompt.

| # | Task | Owner / model | Files it may touch | Starts after |
|---|------|---------------|--------------------|--------------|
| 1 | Real-game capture: `[CHAT]` lines from a client and the server; replies to `/kick`, `/ban`, `/unban`, `/promote`, `/demote`, `/whitelist add/remove/enable`; which list files the game writes and when; whether `/whitelist enable` survives a restart; whether chat starting with `/` or ` /c` runs; a mod that fails to load; Docker's port-in-use start error; `OOMKilled` under a test memory limit. On 2.1.24, plus 2.0.77 and 1.1.110 where formats may differ | tester, Opus medium | `internal/game/testdata/**` and its README | plan |
| 2 | Design sections: player row actions, the list editors, the Chat tab, the schedule form and its Server page state, the notifications page, the low-disk notice, 17.10 click-budget rows | ui-designer, Opus | `docs/DESIGN.md` | plan |
| 3 | Scaffold: empty route files and lines in `server.go`; `main.go` wiring with no-op `notify` and `schedule` stubs; Chat tab and the Notifications and Restart schedule links; empty `players`, `chat`, `crash`, `notify`, `schedule` locale areas in all eight languages; area list in `I18N.md` | builder, Sonnet high | `internal/web/server.go`, new empty route files, `cmd/foreman/main.go`, `layout.html`, `common.json`, new locale files, `docs/I18N.md` | plan |
| 4 | Fakes: RCON replies and `[CHAT]` lines from task 1's transcripts; ban list and whitelist file writes | builder, Sonnet high | `internal/fakegame/**`, `internal/docker/dockertest/**` | 1 |
| 5 | Crash catalogue: `internal/game/crashreasons/` with embedded `catalogue.json` and matcher; `LoadReason.Kind`; a pure classifier for Docker's start error (not wired in); `make test-catalogue VERSION=x.y.z` | builder, Opus | `internal/game/crashreasons/**`, `loadfailure.go`, `controller.go`, `Makefile`, `realgame*_catalogue_game_test.go` | 1, 3 |
| 6 | Notify: presets (Discord, ntfy, generic), queue, two retries, URL checks, sealed storage, notifications page | builder, Opus | `internal/notify/**`, `migrations/0006_webhooks.sql`, `store/webhooks.go`, `web/notify.go`, `notifications.html`, `notify` locale area | 2, 3 |
| 7 | Players and lists: player actions over RCON, ban list and whitelist parsing and editing beside the admins on the Settings tab | builder, Opus | new `gamesettings/` files, `game/settings.go`, `game/playeractions.go`, `web/settings*.go`, `settings*.html`, `web/players.go`, `playeraction.html`, `players` area, `settings.json` | 2, 4 (and ruling 8) |
| 8 | Chat: `ChatLines` and `SendChat` in `game`, the Chat tab | builder, Sonnet high | `game/chat.go`, `web/chat.go`, `chat.html`, `chat` area, chat keys in `js.json` | 2, 4 |
| 9 | Events and low disk: `SetNotifier` and non-blocking `emit` at start, stop, crash, crash loop, join, leave; low-disk check in the monitor poll; wire the start-error classifier | builder, Opus | `game/events.go`, `monitor.go`, `actions.go`, `launch.go`, `controller.go` | 5, 6 |
| 10 | Scheduled restart: one schedule, its settings page, the wait, countdown and skip; emits the scheduled-restart event | builder, Opus | `internal/schedule/**`, `migrations/0007_*.sql`, `store/schedules.go`, `web/schedule.go`, `schedule.html`, `schedule` area | 6, 9 |
| 11 | Server page: crash reason words, player row actions, low-disk notice, scheduled-restart waiting state and next time, and the epic's `foreman.css` changes | ui-designer, Opus | `server.html`, `serverpage.go`, `serverresults.go`, `live.go`, `gametext.go`, `server.json`, `foreman.css`, server keys in `js.json` | 5, 7, 9, 10 |
| 12 | Operator guide: webhooks (with the generic body format), the schedule, low disk | builder, Sonnet | `docs/OPERATOR.md`, `README.md` | 6, 10 |
| 13 | Fold: STATUS section, SPEC rulings (7.12, 8.2, 8.3, 8.5, 8.8, 8.10 to 8.13, 14.8, 4.13, 18), ADRs (webhook storage and body, scheduler), delete this plan | builder, Sonnet | docs | all |

Tasks before task 11 use existing CSS classes only and list any CSS they
need in their pull request body. Each task writes its own end-to-end spec
file. Migration 0006 lands before 0007 is written.

## Waves

- **Wave 0:** tasks 1, 2 and 3 in parallel; task 4 as soon as task 1
  lands.
- **Wave 1:** task 5 and task 6 once tasks 1 to 3 land; tasks 7 and 8 once
  task 4 lands.
- **Wave 2:** task 9, then task 10.
- **Wave 3:** task 11 and task 12, then reviews, the real-game check and
  task 13.

## Real-game checks

- Task 1 captures every format listed above.
- Task 5: `make test-catalogue` on all three pinned images names each
  reason from its scenario.
- Task 7: each action changes the game's own file; a stopped-world edit
  takes effect at the next start. Todd checks with a client that a banned
  player is refused.
- Task 8: a client's chat appears, a server message arrives, and a
  message starting with `/` does not run (achievements stay enabled).
- Task 9: a real join, leave and corrupt-save crash each send one
  webhook; a Foreman restart sends no duplicates.
- Task 10: with nobody online the restart runs; with Todd online it waits
  and warns, then runs after Todd leaves; with a short limit it is skipped.
