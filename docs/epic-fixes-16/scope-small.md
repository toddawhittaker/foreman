# Fixes batch 16: scope for the three small issues (#781, #784, #785)

Base commit: 5b718314 (origin/main). All three can start in parallel from it.
No two of them edit the same file (see "Ownership and overlap").

---

## #781: Switch back link missing right after a switch

### Verdict: the race is in the page, not the test

`serverPage` (internal/web/game.go:322) builds the page from two reads taken
at different moments:

1. the game status, which the caller reads first and passes in
   (`handleServer`, game.go:396; the live stream, live.go:68);
2. the stored action result, `s.runs.result(sum.ID)`, read inside
   serverPage (game.go:324), after `s.stopSeconds(ctx)` has read a setting
   from the database.

A switch ends in this order: the controller marks its operation Done
(`opRun.end`, internal/game/controller.go:718), `Switch` returns, then the run
stores its result (`gameRuns.runOn`, game.go:159-184). If a page request reads
the status just before step one and the result just after the last step, it
gets a fresh result with a stale status that still says the switch is running.

With that mix, `Done()` (internal/web/serverresults.go:186) shows "The worlds
are switched, from Alpha to Bravo", because it looks only at the result. But
the Switch back link comes from `Undo()` (serverresults.go:429), which goes
through `switchBackTarget()` (serverresults.go:63) and returns nothing while
`p.Busy()` is true. So the page shows Done with no way back, and the stale
operation's step list ("Switching from Alpha to Bravo") as well.

The test is right: `waitPage` (internal/web/game_test.go:111) polls GET / and
returns the first body holding both strings, which can be that mixed page. In
a browser the live stream repairs it within a moment (its `results` case
rereads the status), so a person sees at most a brief flash. A plain page load
at that instant stays wrong until the stream's first send.

Evidence (run in a throwaway copy, not in the repo; diff saved beside this file
as race781-experiment.diff):

- Forcing the order (a 100 ms pause before `o.end` in Switch, and a 300 ms
  pause in serverPage when the status shows an unfinished switch) made
  `TestServerPageSwitchesAndSwitchesBack` fail 3 of 3 runs at line 435. The
  failing body had the Done message and the "Switching from Alpha to Bravo"
  step list together.
- Adding the fix below, with both pauses still in place, passed 5 of 5.

### What the fix task should do

Write the failing test first, then the fix.

- **Test (deterministic, no sleeps).** After a real switch in the game rig has
  finished and stored its result, call `serverPage` with a stale status: the
  switch's status with a copy of its operation set to `Done: false`. Assert
  that `p.Undo()` offers "Switch back to Alpha" and that `p.ShowOperation()` is
  false. A `&server{...}` built in the test is precedent (for example
  internal/web/appsettings_test.go:18); the builder picks the fields
  serverPage needs (game, runs, worlds, store, cfg, log, now).
- **Fix (game.go, serverPage).** Right after the result is read: if a result is
  stored and the status still shows an operation running, read the status
  again (`st = s.game.Status(sum.ID); p.Status = st`). A result is only stored
  after its operation has ended, so the second read is always consistent. Use
  the updated `st` for the rest of the function (cancelledByOperation, the
  crash-loop and running checks). Explain the reason in a two-line comment.
  This covers both callers, page load and live stream.
- Do not change `Done()`, `Undo()` or `Busy()`: the rule that the switch back
  is not offered while an operation runs is right.
- Also (one line, so #784 need not touch game.go): change the comment on
  `gameRuns.changed` (game.go:71) from "closed and replaced when a result is
  stored" to also say "or when a snapshot is taken or deleted outside a task".
- Check: the new test with `-count=20`, and
  `go test ./internal/web -run 'TestServerPageSwitchesAndSwitchesBack$' -count=50`
  (each run takes under a second). Both under 3 minutes.

| Item | Answer |
|---|---|
| Files edited | internal/web/game.go (serverPage, and the comment on `gameRuns.changed`) |
| Unit tests | internal/web/serverpage_hub_test.go: new deterministic test beside TestServerPageSwitchesAndSwitchesBack; the existing test stays as it is |
| End-to-end tests | None. No browser test can force the order, and the live stream already repairs the page. Existing specs that check the link: e2e/tests/dismiss-result.spec.ts, e2e/tests/click-budgets.spec.ts |
| SPEC sections | 7.9 (switch), 17.7 (the Server page is the hub), 17.9 (every result offers its next step), 17.10 (click budgets) |
| Owner | builder, Sonnet, high effort. The diagnosis and fix are settled here. |

---

## #784: snapshot card does not follow an inline take or delete

### How to notify the Server page's stream

The live stream (internal/web/live.go:31) already rereads the snapshot card in
two cases: when an operation ends, and when the `results` channel fires
(`s.runs.watch()`, live.go:49 and 97-99). That channel is the event bus the
background tasks use: `gameRuns.changed`, closed and replaced by
`changedLocked()` (game.go:216) whenever a run starts or stores its result.

The Snapshots page takes and deletes without a run, so nothing closes that
channel. Reuse it: add one method on `gameRuns` that closes it, and call it
after a successful take or delete.

```go
// snapshotsChanged wakes the live Server pages to read their snapshot card
// again, after a snapshot was taken or deleted outside a task (SPEC 6.1, 16.3).
func (g *gameRuns) snapshotsChanged() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.changedLocked()
}
```

- Put the method in internal/web/live.go, beside its only consumer, not in
  game.go. Every other `gameRuns` method is in game.go, but #781 owns that file
  in this batch; this keeps the two tasks apart.
- Call it in `handleTakeSnapshot` (internal/web/snapshots.go:236) when the take
  succeeded, before the redirect. That covers both branches, with and without
  server control; `s.runs` is always set (internal/web/server.go:184).
- Call it in `deleteSnapshot` (internal/web/snapshots_all.go:273) when the
  delete succeeded. Every delete goes through there: the single delete
  (snapshots_all.go:292), the bulk delete (snapshots_bulk.go:301), and the MCP
  `delete_snapshots` tool (mcpadmin_snapshots.go:301), which the REST API
  also reaches. A bulk delete wakes the stream once per snapshot, which is
  cheap.
- Update the comment at live.go:54-56 to name inline takes and deletes.
- Nothing else is missing. The MCP snapshot tool and the Server page's own
  snapshot button go through `runs.runOn`, which already fires.
- Rejected: a change notice in the world package (`world.Manager`). It would
  catch every writer, but it needs a new channel and a subscriber API for a
  need the run channel already meets.

| Item | Answer |
|---|---|
| Files edited | internal/web/live.go (new method and comment), internal/web/snapshots.go (handleTakeSnapshot), internal/web/snapshots_all.go (deleteSnapshot) |
| Unit tests | internal/web/game_test.go: a new test beside TestSnapshotCardFollowsAnEndedOperation (from #767), using its `openEvents` and `readEvent` pattern. Two cases: POST to the Snapshots page's take (PathWorldSnapshots) makes a `snaps` event with the new snapshot; the confirmed single delete (PathSnapshotDelete) makes one without it. Write it first; it fails today. |
| End-to-end tests | e2e/tests/server-hub.spec.ts: two tests in the "with the fake game, a snapshot meanwhile" block. In a second tab, take a snapshot on the Snapshots page, then delete it; the first tab's `.recent-snaps` card follows each without a reload. End with `checkPage(page)` as the existing test does. |
| SPEC sections | 6.1 (the snapshot card), 6.3 (taking a snapshot by hand), 6.12 (deleting snapshots), 16.3 (live updates), 17.7 (Server page hub) |
| Owner | builder, Sonnet, high effort |

---

## #785: crash wording for `mod-error`

The message key is `crash.kind.mod_error`. Its only caller is `crashCause`
(internal/web/gametext.go:225). It shows in the crash box and is also added by
`withCause` after "The game did not start..." lines (gametext.go:138-155),
where the new sentence is still true. Webhooks send the stable id `mod-error`,
not this text, so docs/OPERATOR.md and SPEC 8.7's list need no change.

The glossary's "stop" (docs/I18N.md, section 3) means the orderly stop. A crash
is not that, so the translations follow the existing
`crash.kind.out_of_memory` verbs in each language. The orchestrator gives the
builder these values; a second agent still reviews them against
docs/I18N.md (SPEC 22.6).

| Language | New value |
|---|---|
| en | A mod stopped the game. |
| de | Eine Mod hat das Spiel beendet. |
| fr | Un mod a arrêté le jeu. |
| es | Un mod detuvo el juego. |
| ru | Мод остановил игру. |
| pl | Modyfikacja zatrzymała grę. |
| cs | Mod zastavil hru. |
| zh-Hans | 某个模组导致游戏停止。 |

Steps: edit the eight files, run `make i18n-hash` (refreshes the hash in
internal/i18n/translated/crash.json), update the two tests, run `make verify`.

| Item | Answer |
|---|---|
| Files edited | internal/i18n/locales/{en,de,fr,es,ru,pl,cs,zh-Hans}/crash.json, internal/i18n/translated/crash.json (by `make i18n-hash` only) |
| Unit tests | internal/web/render_test.go:1074 and internal/web/serverpage_messages_test.go:58: change the expected English text |
| End-to-end tests | None. No spec names this text, and reaching it in a browser needs a crashing mod in the fake game. The render tests pin it. |
| SPEC sections | 8.7 (crash detection and reasons), 22.5 (every string in all eight languages), 22.6 (translations reviewed against the glossary) |
| Owner | builder, Haiku: every value is written out above. Then a Sonnet reviewer checks the seven translations (SPEC 22.6). |

---

## Ownership and overlap

The three file lists do not overlap.

| File | Owner |
|---|---|
| internal/web/game.go | #781 only (including the comment on `gameRuns.changed`) |
| internal/web/serverpage_hub_test.go | #781 |
| internal/web/live.go | #784 |
| internal/web/snapshots.go, internal/web/snapshots_all.go | #784 |
| internal/web/game_test.go | #784 |
| e2e/tests/server-hub.spec.ts | #784 |
| internal/i18n/locales/*/crash.json, internal/i18n/translated/crash.json | #785 |
| internal/web/render_test.go, internal/web/serverpage_messages_test.go | #785 |

The one near miss: the fix for #781 and the method for #784 would both sit in
game.go if the usual pattern were followed. The method goes in live.go instead,
and #781 updates the shared comment. Land them in either order.

Overlap with the larger issues (none has a design yet, so these are the likely
files, not certain ones):

| Issue | Likely files shared with this batch | Effect |
|---|---|---|
| #769 player list and play time | game.go `serverPage` (new fields), live.go (a new region in `liveRegions` if play time updates live), serverpage_hub_test.go, e2e/tests/server-hub.spec.ts, templates/server.html | Small conflicts with #781 and #784. Land #781 and #784 first; #769 starts from their merged head. #785's files are untouched. |
| #773 factory map picture | the world page, a new renderer, the game package's copy-and-run path shared with #769, locales worlds.json | No overlap with this batch. |
| #774 world export and import ZIP | the world package, internal/web/import.go, the world and Worlds pages, locales worlds.json | No overlap. An import adds a new world, so it needs no notice to the Server page's snapshot card. |

None of the three small tasks touches templates/server.html, serverpage.go,
the store, a migration, the HTTP API or the on-disk layout. All three are easy
to undo.

## Choices left for Todd

None blocks the work.

- #785's wording comes from the issue ("A mod stopped the game."). It reads
  true after the "The game did not start" lines too. Todd may want to see the
  seven translations; the SPEC 22.6 reviewer checks them either way.
- #781 changes page code, not the test. The issue left that open; the
  evidence above settles it.

## Left out

- A test seam or hook to pause the switch: the stale-status test reproduces the
  race without one.
- Changing `waitPage` to wait for a stable page: that would hide a real page
  race.
- A per-world snapshot change channel: the run channel is enough.
- docs/STATUS.md: its #784 and #785 gap lines (around line 4667) are the
  fold's job, not a task's.
