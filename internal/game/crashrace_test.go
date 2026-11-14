package game

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

// The orderings below are the ones the real game gave in a crash loop
// (SPEC 8.7, 8.9): each run ends within half a second and Docker restarts
// it within a few hundred milliseconds, so one poll can see the restart
// count rise by three at once, after Docker has already cleared the
// out-of-memory flag and while no follower read the runs in between. The
// monitor does not run on its own here; each test takes its looks by hand.

// drainPoke empties the monitor's wake-up channel, so the next value on it
// comes from what the test does next.
func drainPoke(c *Controller) {
	select {
	case <-c.poke:
	default:
	}
}

// awaitPoke waits for the log follower to end and wake the monitor, which
// it does once it has finished with the exit it saw.
func awaitPoke(t *testing.T, c *Controller) {
	t.Helper()
	select {
	case <-c.poke:
	case <-time.After(5 * time.Second):
		t.Fatal("the log follower never ended")
	}
}

// SPEC 8.7: an out-of-memory kill is named from Docker's flag, which Docker
// clears when it restarts the game. The exit the follower saw had the flag;
// the poll that found the crash loop came after three more restarts, each
// of which cleared it. The flag seen at that exit is the newest Docker
// evidence of the loop, so it names the loop, and Foreman's own stop of the
// loop does not undo it.
func TestCrashLoopKeepsOutOfMemoryThePollMissed(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("OOM missed")
	r.start(id)
	ctx := context.Background()
	d := r.game.Daemon()
	cid := r.container(id).ID

	drainPoke(r.c)
	d.Exit(cid, 137, true)
	awaitPoke(t, r.c)
	for range CrashLoopCount - 1 {
		d.Restart(cid) // Docker clears the flag
		d.Exit(cid, 137, true)
	}
	d.Restart(cid)
	if cs, _ := d.Container(cid); cs.Status != "running" || cs.OOMKilled || cs.RestartCount != CrashLoopCount {
		t.Fatalf("setup: %s, flag %v, %d restarts", cs.Status, cs.OOMKilled, cs.RestartCount)
	}

	r.c.tick(ctx)
	st := r.c.Status(id)
	if st.State != StateCrashLooping || st.Crash.Kind != crashreasons.OutOfMemory {
		t.Fatalf("on the look that found the loop: state %s, kind %q; want %s, %q",
			st.State, st.Crash.Kind, StateCrashLooping, crashreasons.OutOfMemory)
	}
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the crash loop's stop")
	r.c.tick(ctx)
	if st := r.c.Status(id); st.State != StateCrashLooping || st.Crash.Kind != crashreasons.OutOfMemory {
		t.Errorf("after Foreman's own stop: state %s, kind %q", st.State, st.Crash.Kind)
	}
}

// SPEC 8.7: out of memory is never inferred from the exit code. A run the
// kernel may have killed (exit 137) without Docker's flag stays unknown,
// whatever the polls missed.
func TestCrashLoopWithoutTheFlagIsNotOutOfMemory(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("No flag")
	r.start(id)
	d := r.game.Daemon()
	cid := r.container(id).ID

	drainPoke(r.c)
	d.Exit(cid, 137, false)
	awaitPoke(t, r.c)
	for range CrashLoopCount - 1 {
		d.Restart(cid)
		d.Exit(cid, 137, false)
	}
	d.Restart(cid)
	r.c.tick(context.Background())
	if st := r.c.Status(id); st.State != StateCrashLooping || st.Crash.Kind != "" {
		t.Errorf("state %s, kind %q; want %s, unknown", st.State, st.Crash.Kind, StateCrashLooping)
	}
}

// SPEC 8.7: a newer exit that Docker says was not out of memory overrides
// an older one that was; only an exit nobody saw keeps the older flag.
func TestOutOfMemoryClearedByANewerExitWithoutTheFlag(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Contradicted")
	r.start(id)
	ctx := context.Background()
	d := r.game.Daemon()
	cid := r.container(id).ID

	drainPoke(r.c)
	d.Exit(cid, 137, true)
	awaitPoke(t, r.c)
	d.Restart(cid)
	r.c.tick(ctx) // sees the crash and follows the new run
	if st := r.c.Status(id); st.State != StateCrashed || st.Crash.Kind != crashreasons.OutOfMemory {
		t.Fatalf("first crash: state %s, kind %q", st.State, st.Crash.Kind)
	}
	eventually(t, func() bool {
		w := r.c.world(id)
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.following
	}, "the new run to be followed")
	drainPoke(r.c)
	d.Exit(cid, 1, false)
	awaitPoke(t, r.c)
	d.Restart(cid)
	r.c.tick(ctx)
	if st := r.c.Status(id); st.State != StateCrashed || st.Crash.Kind != "" {
		t.Errorf("a newer crash Docker says was not out of memory: state %s, kind %q", st.State, st.Crash.Kind)
	}
}

// SPEC 8.7: the game's own words name a crash even when no follower read
// the runs that said them. After the first exit, Docker ran the save that
// cannot be read three times before the poll that found the loop, and the
// newest run had only begun to load when Foreman stopped it. The reason is
// the newest finished run's, masked as every log line is (SPEC 8.6, 14.8).
func TestCrashLoopNamesTheReasonOfRunsNoFollowerRead(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Bad save")
	r.start(id)
	ctx := context.Background()
	d := r.game.Daemon()
	cid := r.container(id).ID
	w := r.c.world(id)
	w.mu.Lock()
	if len(w.secrets) == 0 {
		w.mu.Unlock()
		t.Fatal("the world has no secrets to mask")
	}
	pw := w.secrets[0].Reveal()
	w.mu.Unlock()
	save := "/factorio/saves/" + pw + ".zip"
	run := func(failed bool) {
		lines := []string{"   0.344 Loading map " + save + ": 4096 bytes."}
		if failed {
			lines = append(lines,
				`   0.344 Error ServerMultiplayerManager.cpp:92: MultiplayerManager failed: "Opening zip `+save+` failed: Bad zip file"`,
				"   0.344 Info ServerMultiplayerManager.cpp:814: updateTick(4294967295) changing state from(CreatingGame) to(InitializationFailed)",
				"   0.364 Goodbye")
		}
		for _, l := range lines {
			if !d.EmitLog(cid, docker.Stdout, l) {
				t.Fatal("no container to log to")
			}
		}
	}

	drainPoke(r.c)
	d.Exit(cid, 0, false) // the game quit, and Docker restarts it
	awaitPoke(t, r.c)
	for range CrashLoopCount {
		d.Restart(cid)
		run(true)
		d.Exit(cid, 1, false)
	}
	d.Restart(cid)
	run(false)

	r.c.tick(ctx)
	st := r.c.Status(id)
	if st.State != StateCrashLooping || st.Crash.Kind != crashreasons.SaveUnreadable {
		t.Fatalf("on the look that found the loop: state %s, kind %q (%s); want %s, %q",
			st.State, st.Crash.Kind, st.CrashReason, StateCrashLooping, crashreasons.SaveUnreadable)
	}
	if strings.Contains(st.CrashReason, pw) || !strings.Contains(st.CrashReason, "Bad zip file") {
		t.Errorf("crash reason %q: want the game's words with the password masked", st.CrashReason)
	}
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the crash loop's stop")
	r.c.tick(ctx)
	if st := r.c.Status(id); st.Crash.Kind != crashreasons.SaveUnreadable {
		t.Errorf("after Foreman stopped the run that was loading: kind %q (%s)", st.Crash.Kind, st.CrashReason)
	}
}

// SPEC 8.7: a run still loading when the reason is read has not said why
// it would stop, so the reason of the newest earlier run that did is kept.
// A run that finished loading, or failed with no known reason, did say:
// that is the reason then, and an older one is never shown.
func TestCrashReasonKeepsTheNewestFinishedRuns(t *testing.T) {
	loading := "   0.5 Loading map /factorio/saves/a.zip: 10 bytes."
	badZip := `   0.5 Error ServerMultiplayerManager.cpp:92: MultiplayerManager failed: "Opening zip /factorio/saves/a.zip failed: Bad zip file"`
	other := `   0.5 Error ServerMultiplayerManager.cpp:92: MultiplayerManager failed: "something new"`
	failed := "   0.5 Info ServerMultiplayerManager.cpp:814: updateTick(4294967295) changing state from(CreatingGame) to(InitializationFailed)"
	inGame := "   0.6 Info ServerMultiplayerManager.cpp:814: updateTick(0) changing state from(CreatingGame) to(InGame)"
	for _, tc := range []struct {
		name  string
		lines []string
		want  string
	}{
		{"a run still loading", []string{loading, badZip, failed, loading}, crashreasons.SaveUnreadable},
		{"runs that ended unheard", []string{loading, badZip, failed, loading, loading}, crashreasons.SaveUnreadable},
		{"a newer failed run with another reason", []string{loading, badZip, failed, loading, other, failed, loading}, ""},
		{"a run that reached the game", []string{loading, badZip, failed, loading, inGame}, ""},
		{"a run that reached the game, then one loading", []string{loading, badZip, failed, loading, inGame, loading}, ""},
		{"the newest run's own reason", []string{loading, other, failed, loading, badZip}, crashreasons.SaveUnreadable},
	} {
		f := readAll(tc.lines)
		if got := f.crashReason(masking()).Kind; got != tc.want {
			t.Errorf("%s: kind %q, want %q", tc.name, got, tc.want)
		}
	}
	// A start reads the log after its game stopped, so its newest run did
	// end, unheard; that run's unknown stands (TestLoadReasonKindIsTheNewestRuns).
	start := readAll([]string{loading, badZip, failed, loading})
	if got := start.reason(masking()).Kind; got != "" {
		t.Errorf("a start's reason took an older run's kind %q", got)
	}
}
