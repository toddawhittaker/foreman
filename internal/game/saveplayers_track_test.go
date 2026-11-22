package game

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// needRecords skips a test until the players record can be written and read.
func needRecords(t *testing.T) {
	t.Helper()
	b, err := saveplayers.Record{FormatVersion: saveplayers.FormatVersion}.Marshal()
	if err == nil {
		_, err = saveplayers.Parse(b)
	}
	if err != nil {
		t.Skipf("the players record is not built yet: %v", err)
	}
}

// putRecord writes a record for the save the world loads, true at asOf.
func (r *rig) putPlayersRecord(id string, asOf time.Time) saveplayers.Record {
	r.t.Helper()
	b, ok := r.c.loadTarget(id)
	if !ok {
		r.t.Fatal("the world has no save to load")
	}
	rec := saveplayers.Record{FormatVersion: saveplayers.FormatVersion, Basis: b, AsOf: asOf, TrackedSince: asOf,
		Seed: saveplayers.SeedInfo{At: asOf, Save: b.Save, Result: saveplayers.ResultOK}}
	r.writePlayersRecord(id, rec)
	return rec
}

func (r *rig) writePlayersRecord(id string, rec saveplayers.Record) {
	r.t.Helper()
	data, err := rec.Marshal()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.worlds.WritePlayersRecord(id, data); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) playersRecord(id string) saveplayers.Record {
	r.t.Helper()
	b, err := r.worlds.ReadPlayersRecord(id)
	if err != nil {
		r.t.Fatal(err)
	}
	rec, err := saveplayers.Parse(b)
	if err != nil {
		r.t.Fatal(err)
	}
	return rec
}

func trackedOf(rec saveplayers.Record, name string) (saveplayers.Player, bool) {
	i := slices.IndexFunc(rec.Players, func(p saveplayers.Player) bool { return p.Name == name })
	if i < 0 {
		return saveplayers.Player{}, false
	}
	return rec.Players[i], true
}

func joinAt(name string, at time.Time) Event {
	return Event{Kind: KindPlayerJoined, Player: name, Time: at}
}

func leaveAt(name string, at time.Time) Event {
	return Event{Kind: KindPlayerLeft, Player: name, Time: at}
}

var trackT0 = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

// SPEC 8.14: two players on at once each get their whole time.
func TestTrackOverlappingSessions(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Overlap")
	r.putPlayersRecord(id, trackT0)
	r.c.trackLine(id, joinAt("ann", trackT0.Add(time.Minute)), "", true)
	r.c.trackLine(id, joinAt("bob", trackT0.Add(10*time.Minute)), "", true)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(31*time.Minute)), "", true)
	r.c.trackLine(id, leaveAt("bob", trackT0.Add(40*time.Minute)), "", true)

	rec := r.playersRecord(id)
	for name, want := range map[string]int64{"ann": 30 * 60, "bob": 30 * 60} {
		p, ok := trackedOf(rec, name)
		if !ok || p.TrackedSeconds != want {
			t.Errorf("%s: %+v (found %v), want %d tracked seconds", name, p, ok, want)
		}
	}
	if p, _ := trackedOf(rec, "bob"); p.LastSeen == nil || !p.LastSeen.Equal(trackT0.Add(40*time.Minute)) {
		t.Errorf("bob last seen %v, want the leave time", p.LastSeen)
	}
	if !rec.AsOf.Equal(trackT0.Add(40 * time.Minute)) {
		t.Errorf("as of %v, want the last leave", rec.AsOf)
	}
}

// SPEC 8.14: replaying the log after a re-attach never counts a session
// twice, and still counts the part Foreman missed.
func TestTrackReplayCountsOnce(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Replay")
	r.putPlayersRecord(id, trackT0)
	r.c.trackLine(id, joinAt("ann", trackT0.Add(time.Minute)), "", true)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(11*time.Minute)), "", true)
	// The same lines again, as a re-attach replays them, then a session
	// whose lines Foreman read only from the replay.
	r.c.trackLine(id, joinAt("ann", trackT0.Add(time.Minute)), "", false)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(11*time.Minute)), "", false)
	r.c.trackLine(id, joinAt("ann", trackT0.Add(20*time.Minute)), "", false)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(25*time.Minute)), "", false)

	if p, _ := trackedOf(r.playersRecord(id), "ann"); p.TrackedSeconds != 15*60 {
		t.Errorf("ann tracked %d seconds, want %d", p.TrackedSeconds, 15*60)
	}
}

// SPEC 8.14: time before the record's moment was in the save already.
func TestTrackCountsOnlyAfterTheRecord(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("After")
	r.putPlayersRecord(id, trackT0)
	r.c.trackLine(id, joinAt("ann", trackT0.Add(-time.Hour)), "", true)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(5*time.Minute)), "", true)
	if p, _ := trackedOf(r.playersRecord(id), "ann"); p.TrackedSeconds != 5*60 {
		t.Errorf("ann tracked %d seconds, want %d", p.TrackedSeconds, 5*60)
	}
}

// SPEC 8.14: without a record nothing is written; the list starts from a
// read of the save.
func TestTrackWithoutRecordWritesNothing(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("None")
	r.c.trackLine(id, joinAt("ann", trackT0), "", true)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(time.Minute)), "", true)
	r.c.trackRunEnd(id, runEnd{at: trackT0.Add(2 * time.Minute), unseen: true})
	r.c.trackReattach(id)
	if _, err := r.worlds.ReadPlayersRecord(id); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a record appeared: %v", err)
	}
}

// SPEC 8.14: a new run (a load line) ends the sessions of the run before.
func TestTrackLoadEndsOpenSessions(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Load")
	r.putPlayersRecord(id, trackT0)
	r.c.trackLine(id, joinAt("ann", trackT0), "", true)
	r.c.trackLine(id, Event{Kind: KindLoadStarted, Time: trackT0.Add(3 * time.Minute)}, "base.zip", true)
	r.c.trackLine(id, leaveAt("ann", trackT0.Add(time.Hour)), "", true)
	if p, _ := trackedOf(r.playersRecord(id), "ann"); p.TrackedSeconds != 3*60 {
		t.Errorf("ann tracked %d seconds, want %d", p.TrackedSeconds, 3*60)
	}
}

// SPEC 8.14: a stop closes open sessions when the game ended; a crash also
// marks the record inexact (rule D7).
func TestTrackRunEnd(t *testing.T) {
	needRecords(t)
	for _, crash := range []bool{false, true} {
		r := newRig(t, rigOptions{noRun: true})
		id := r.newWorld("End")
		r.putPlayersRecord(id, trackT0)
		r.c.trackLine(id, Event{Kind: KindLoadStarted, Time: trackT0.Add(time.Second)}, "base.zip", true)
		r.c.trackLine(id, joinAt("ann", trackT0.Add(time.Minute)), "", true)
		r.c.trackRunEnd(id, runEnd{at: trackT0.Add(21 * time.Minute), started: trackT0, unseen: crash})
		// A later poll of the same stopped container changes nothing.
		r.c.trackRunEnd(id, runEnd{at: trackT0.Add(21 * time.Minute), started: trackT0, unseen: crash})
		rec := r.playersRecord(id)
		if p, _ := trackedOf(rec, "ann"); p.TrackedSeconds != 20*60 {
			t.Errorf("crash %v: ann tracked %d seconds, want %d", crash, p.TrackedSeconds, 20*60)
		}
		if rec.Inexact != crash {
			t.Errorf("crash %v: inexact %v", crash, rec.Inexact)
		}
		// The sessions are closed: a later leave line adds nothing.
		r.c.trackLine(id, leaveAt("ann", trackT0.Add(time.Hour)), "", false)
		if p, _ := trackedOf(r.playersRecord(id), "ann"); p.TrackedSeconds != 20*60 {
			t.Errorf("crash %v: a leave after the end counted, %d seconds", crash, p.TrackedSeconds)
		}
	}
}

// SPEC 8.14 (rule D7): a run that began after the record's moment and ended
// with nothing tracked ran while Foreman was down.
func TestTrackRunWhileForemanDown(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Down")
	r.putPlayersRecord(id, trackT0)
	r.c.trackRunEnd(id, runEnd{at: trackT0.Add(2 * time.Hour), started: trackT0.Add(time.Hour)})
	if !r.playersRecord(id).Inexact {
		t.Error("a run Foreman never saw left the record exact")
	}

	id2 := r.newWorld("Seen")
	r.putPlayersRecord(id2, trackT0.Add(2*time.Hour))
	r.c.trackRunEnd(id2, runEnd{at: trackT0.Add(2 * time.Hour), started: trackT0.Add(time.Hour)})
	if r.playersRecord(id2).Inexact {
		t.Error("a run that ended before the record was made marked it inexact")
	}
}

// SPEC 8.14: a save the game finished moves the record to it, so the record
// still matches the save that loads next; but only from the save the run
// went on from, and not once the record is inexact.
func TestTrackSaveRebases(t *testing.T) {
	needRecords(t)
	cases := []struct {
		name       string
		live       bool
		loaded     string
		inexact    bool
		wantRebase bool
	}{
		{"own run", true, "base.zip", false, true},
		{"replayed load", false, "base.zip", false, false},
		{"other save loaded", true, "older.zip", false, false},
		{"inexact", true, "base.zip", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOptions{noRun: true})
			id := r.newWorld("Rebase")
			rec := r.putPlayersRecord(id, trackT0)
			if tc.inexact {
				rec.Inexact = true
				r.writePlayersRecord(id, rec)
			}
			r.c.trackLine(id, Event{Kind: KindLoadStarted, Time: trackT0.Add(time.Second)}, tc.loaded, tc.live)
			r.putSave(id, "2.1.24-no-space-age.zip", "_autosave1.zip", time.Now())
			r.c.trackLine(id, Event{Kind: KindSaveFinished, Time: trackT0.Add(5 * time.Minute)}, "", tc.live)
			got := r.playersRecord(id).Basis.Save
			if rebased := got == "_autosave1.zip"; rebased != tc.wantRebase {
				t.Errorf("basis %s, rebased %v, want %v", got, rebased, tc.wantRebase)
			}
		})
	}
}

// A crash is seen by the monitor and marks the record inexact (SPEC 8.14).
func TestCrashMarksRecordInexact(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{})
	id := r.newWorld("Crash record")
	r.putPlayersRecord(id, trackT0)
	r.start(id)
	if err := r.game.Join(ContainerName(id), "dave"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { _, ok := trackedOf(r.playersRecord(id), "dave"); return ok }, "the join to reach the record")
	if r.playersRecord(id).Inexact {
		t.Fatal("inexact before the crash")
	}
	if err := r.game.Crash(ContainerName(id), 0, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.playersRecord(id).Inexact }, "the crash to mark the record inexact")
}

// A stop closes the open sessions, and a game Foreman takes on after a
// restart marks the record inexact (SPEC 8.14).
func TestStopAndReattachTrackRecord(t *testing.T) {
	needRecords(t)
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stop record")
	r.putPlayersRecord(id, trackT0)
	r.start(id)
	if err := r.game.Join(ContainerName(id), "erin"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { _, ok := trackedOf(r.playersRecord(id), "erin"); return ok }, "the join to reach the record")

	r.stop()
	if r.playersRecord(id).Inexact {
		t.Fatal("inexact before Foreman restarted")
	}
	c2 := r.newController(rigOptions{})
	r.run(c2)
	eventually(t, func() bool { return r.playersRecord(id).Inexact }, "the re-attach to mark the record inexact")

	joined := r.waitTracked(c2, id, "erin")
	if err := c2.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		p, _ := trackedOf(r.playersRecord(id), "erin")
		return p.LastSeen != nil && p.LastSeen.After(joined)
	}, "the stop to close erin's session")
	if n := len(c2.tracked(id).open); n != 0 {
		t.Errorf("%d sessions open after the stop", n)
	}
}

// tracked waits until c knows player is on, and returns their join time.
func (r *rig) waitTracked(c *Controller, id, player string) time.Time {
	r.t.Helper()
	var at time.Time
	eventually(r.t, func() bool {
		tw := c.tracked(id)
		tw.mu.Lock()
		defer tw.mu.Unlock()
		var ok bool
		at, ok = tw.open[player]
		return ok
	}, "%s to be tracked as on", player)
	return at
}

// SPEC 8.15: a page makes Foreman ask /time at most once a minute.
func TestGameTimeAskedOnceAMinute(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Time")
	r.start(id)
	asks := func() int {
		return len(slices.DeleteFunc(r.commands(id), func(c string) bool { return c != "/time" }))
	}
	r.c.liveGameTime(id)
	eventually(t, func() bool { return asks() == 1 }, "one /time")
	eventually(t, func() bool {
		tw := r.c.tracked(id)
		tw.mu.Lock()
		defer tw.mu.Unlock()
		return !tw.asking
	}, "the ask to end")
	r.c.liveGameTime(id)
	r.clock.Advance(30 * time.Second)
	r.c.liveGameTime(id)
	time.Sleep(50 * time.Millisecond)
	if n := asks(); n != 1 {
		t.Errorf("%d /time commands within a minute, want 1", n)
	}
	r.clock.Advance(31 * time.Second)
	r.c.liveGameTime(id)
	eventually(t, func() bool { return asks() == 2 }, "a second /time after a minute")
}

// SPEC 8.15: the /time answer becomes the record's game time.
func TestGameTimeReachesRecord(t *testing.T) {
	needRecords(t)
	if _, err := ParseTimeReply("1 minute"); err != nil {
		t.Skipf("the /time parser is not built yet: %v", err)
	}
	r := newRig(t, rigOptions{})
	id := r.newWorld("Time record")
	r.putPlayersRecord(id, trackT0)
	r.start(id)
	var d time.Duration
	eventually(t, func() bool {
		var ok bool
		d, _, ok = r.c.liveGameTime(id)
		return ok
	}, "a /time reading")
	if got, want := r.playersRecord(id).GameTicks, int64(d/time.Second)*ticksPerSecond; got != want {
		t.Errorf("game ticks %d, want %d from %v", got, want, d)
	}
}

// updatePlayersRecord gives a zero record for a missing or bad file, and
// writes only a record with a format version (SPEC 8.14, 14.6).
func TestUpdatePlayersRecord(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Update")
	called := false
	err := r.c.updatePlayersRecord(id, func(rec *saveplayers.Record) error {
		called = true
		if rec.FormatVersion != 0 {
			t.Errorf("a missing record read as %+v", rec)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err %v, called %v", err, called)
	}
	if _, err := r.worlds.ReadPlayersRecord(id); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a zero record was written: %v", err)
	}
	r.writeFile(id, world.PlayersFile, []byte("not json"))
	want := errors.New("refused")
	if err := r.c.updatePlayersRecord(id, func(rec *saveplayers.Record) error {
		if rec.FormatVersion != 0 {
			t.Errorf("a bad record read as %+v", rec)
		}
		return want
	}); !errors.Is(err, want) {
		t.Errorf("f's error came back as %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(r.worldDir(id), world.PlayersFile)); string(b) != "not json" { //nolint:gosec // the test's own folder
		t.Errorf("a refused update wrote %q", b)
	}
}
