package game

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
	"github.com/toddawhittaker/foreman/internal/notify"
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
		if got, _, _ := f.crashReason(masking()); got.Kind != tc.want {
			t.Errorf("%s: kind %q, want %q", tc.name, got.Kind, tc.want)
		}
	}
	// A start reads the log after its game stopped, so its newest run did
	// end, unheard; that run's unknown stands (TestLoadReasonKindIsTheNewestRuns).
	start := readAll([]string{loading, badZip, failed, loading})
	if got := start.reason(masking()).Kind; got != "" {
		t.Errorf("a start's reason took an older run's kind %q", got)
	}
}

// Lines of the game's log in a crash loop: the start of a run, the words
// that name a save that cannot be read, and the end of a failed load.
const (
	runLoad    = "   0.344 Loading map /factorio/saves/base.zip: 4096 bytes."
	runBadZip  = `   0.344 Error ServerMultiplayerManager.cpp:92: MultiplayerManager failed: "Opening zip /factorio/saves/base.zip failed: Bad zip file"`
	runOther   = `   0.344 Error ServerMultiplayerManager.cpp:92: MultiplayerManager failed: "something new"`
	runFailed  = "   0.344 Info ServerMultiplayerManager.cpp:814: updateTick(4294967295) changing state from(CreatingGame) to(InitializationFailed)"
	runGoodbye = "   0.364 Goodbye"
)

// emitLines writes lines to the container's log, as the game would.
func emitLines(t *testing.T, d *dockertest.Daemon, cid string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !d.EmitLog(cid, docker.Stdout, l) {
			t.Fatal("no container to log to")
		}
	}
}

// lookAt reads the container as the poll would, before anything below
// changes it, and the world's operation count with it.
func lookAt(t *testing.T, r *rig, w *worldRun, cid string) (docker.Container, uint64) {
	t.Helper()
	w.mu.Lock()
	ops := w.ops
	w.mu.Unlock()
	cont, err := r.docker.InspectContainer(context.Background(), cid)
	if err != nil {
		t.Fatal(err)
	}
	return cont, ops
}

func oomOf(w *worldRun) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.oomKilled
}

// SPEC 8.7: the poll's inspect can be seconds older than the one taken when
// the log ended (its log read takes up to loadFailureReadLimit), so the
// two race. Docker's FinishedAt orders them: a look at an older exit never
// replaces what a look at a newer one said, in either direction.
func TestOlderExitLookDoesNotOverruleANewerOne(t *testing.T) {
	for _, tc := range []struct {
		name       string
		older, oom bool // whether the older exit and the newer were out of memory
	}{
		{"a stale exit without the flag after a newer out-of-memory kill", false, true},
		{"a stale out-of-memory kill after a newer exit without the flag", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOptions{noRun: true})
			id := r.newWorld("Race")
			r.start(id)
			d := r.game.Daemon()
			cid := r.container(id).ID
			w := r.c.world(id)

			drainPoke(r.c)
			d.Exit(cid, 1, tc.older)
			awaitPoke(t, r.c) // the follower ended and looked at this exit
			stale, ops := lookAt(t, r, w, cid)
			d.Restart(cid)
			d.Exit(cid, 1, tc.oom)
			r.c.noteExit(w, cid) // the newer exit, seen first
			if got := oomOf(w); got != tc.oom {
				t.Fatalf("after the newer exit's look: out of memory %v, want %v", got, tc.oom)
			}
			if !r.c.observe(w, &stale, ops, nil) {
				t.Fatal("the poll's look was dropped")
			}
			if got := oomOf(w); got != tc.oom {
				t.Errorf("the older look made it out of memory %v, want %v", got, tc.oom)
			}
			d.Restart(cid)
			r.c.tick(context.Background())
			want := ""
			if tc.oom {
				want = crashreasons.OutOfMemory
			}
			if st := r.c.Status(id); st.State != StateCrashed || st.Crash.Kind != want {
				t.Errorf("state %s, kind %q; want %s, %q", st.State, st.Crash.Kind, StateCrashed, want)
			}
		})
	}
}

// SPEC 8.7: a look at the exit it already took in, or one with no exit
// time, changes nothing; a running game's flag is not an exit's.
func TestExitSeenOnlyFromANewerExit(t *testing.T) {
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	w := &worldRun{}
	if exitSeenLocked(w, docker.State{Status: "exited", OOMKilled: true}) || w.oomKilled {
		t.Error("an exit with no time was taken in")
	}
	if !exitSeenLocked(w, docker.State{Status: "exited", OOMKilled: true, FinishedAt: at}) || !w.oomKilled {
		t.Fatal("the first exit was not taken in")
	}
	if exitSeenLocked(w, docker.State{Status: "exited", FinishedAt: at}) || !w.oomKilled {
		t.Error("a look at the same exit changed the flag")
	}
	if exitSeenLocked(w, docker.State{Status: "exited", FinishedAt: at.Add(-time.Second)}) || !w.oomKilled {
		t.Error("an older exit changed the flag")
	}
	w.stopping = true
	if exitSeenLocked(w, docker.State{Status: "exited", FinishedAt: at.Add(time.Second)}) || !w.oomKilled {
		t.Error("Foreman's own stop cleared the flag")
	}
	w.stopping = false
	if !exitSeenLocked(w, docker.State{Status: "exited", FinishedAt: at.Add(2 * time.Second)}) || w.oomKilled {
		t.Error("a newer exit without the flag did not clear it")
	}
	for _, s := range []string{"running", "paused", "created"} {
		if exited(s) {
			t.Errorf("%s counted as an exit", s)
		}
	}
}

// SPEC 8.7: an out-of-memory kill names the crash until a newer run names
// another reason. Here the game ran out of memory, Docker restarted it,
// and that run failed to load its save before any look saw it, so the
// save names the crash. A run Docker began after the kill is told by
// Docker's own time of its first line.
func TestNewerRunsReasonOverrulesAnOlderOutOfMemoryKill(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("OOM then bad save")
	r.start(id)
	d := r.game.Daemon()
	cid := r.container(id).ID

	drainPoke(r.c)
	d.Exit(cid, 137, true)
	awaitPoke(t, r.c)
	if !oomOf(r.c.world(id)) {
		t.Fatal("setup: the kill was not seen")
	}
	d.Restart(cid)
	emitLines(t, d, cid, runLoad, runBadZip, runFailed, runGoodbye)
	d.Exit(cid, 1, false) // no look sees this exit
	d.Restart(cid)
	emitLines(t, d, cid, runLoad)

	r.c.tick(context.Background())
	if st := r.c.Status(id); st.State != StateCrashed || st.Crash.Kind != crashreasons.SaveUnreadable {
		t.Errorf("state %s, kind %q; want %s, %q", st.State, st.Crash.Kind, StateCrashed, crashreasons.SaveUnreadable)
	}
}

// SPEC 8.7: a read taken after an out-of-memory kill holds the killed run's
// words too, and the flag outranks them: they were written before the
// kill, so they are not a newer crash.
func TestOutOfMemoryOutranksTheKilledRunsOwnWords(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("OOM with words")
	r.start(id)
	d := r.game.Daemon()
	cid := r.container(id).ID

	drainPoke(r.c)
	emitLines(t, d, cid, runLoad, runBadZip)
	d.Exit(cid, 137, true)
	awaitPoke(t, r.c)
	d.Restart(cid)
	emitLines(t, d, cid, runLoad)
	d.Exit(cid, 1, false)
	d.Restart(cid)

	r.c.tick(context.Background())
	if st := r.c.Status(id); st.State != StateCrashed || st.Crash.Kind != crashreasons.OutOfMemory {
		t.Errorf("state %s, kind %q; want %s, %q", st.State, st.Crash.Kind, StateCrashed, crashreasons.OutOfMemory)
	}
}

// takeEvents empties the controller's event queue; the tests below run no
// monitor, so nothing else delivers them.
func takeEvents(c *Controller) []notify.Message {
	var out []notify.Message
	for {
		select {
		case m := <-c.events:
			out = append(out, m)
		default:
			return out
		}
	}
}

// eventsOf lists the crash and crash-loop events in ms, with their reasons.
func crashEventsOf(ms []notify.Message) []string {
	var out []string
	for _, m := range ms {
		if m.Event == notify.Crash || m.Event == notify.CrashLoop {
			out = append(out, string(m.Event)+":"+m.Reason)
		}
	}
	return out
}

// ADR 0016, SPEC 8.7, 8.10: when the poll's read of the log is the only
// way the runs it found were seen (each follower is held, as a stalled
// stream is), the crash and the crash loop it finds are each sent at once
// with the reason that read names.
func TestCrashEventsNameThePollsRead(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.crashSettle = time.Hour
	id := r.newWorld("Read only")
	r.start(id)
	ctx := context.Background()
	d := r.game.Daemon()
	cid := r.container(id).ID
	d.HoldFollowers(true)
	t.Cleanup(func() { d.HoldFollowers(false) })
	takeEvents(r.c)

	d.Exit(cid, 1, false)
	d.Restart(cid)
	emitLines(t, d, cid, runLoad, runBadZip, runFailed)
	d.Exit(cid, 1, false)
	d.Restart(cid)
	emitLines(t, d, cid, runLoad)
	r.c.tick(ctx)
	if got := crashEventsOf(takeEvents(r.c)); !slices.Equal(got, []string{"crash:" + crashreasons.SaveUnreadable}) {
		t.Fatalf("after the look that found the crash: %v", got)
	}

	emitLines(t, d, cid, `   0.300 Error Util.cpp:81: Failed to load mod "brokenlua": __brokenlua__/data.lua:1: boom`)
	d.Exit(cid, 1, false)
	d.Restart(cid)
	r.c.tick(ctx)
	if got := crashEventsOf(takeEvents(r.c)); !slices.Equal(got, []string{"crash-loop:" + crashreasons.ModError}) {
		t.Errorf("after the look that found the loop: %v", got)
	}
}

// SPEC 8.7, 8.9, 8.10: an earlier run's reason that only the follower saw
// is the last one seen, not the crash's: the run that crashed may not
// have been read yet. The crash-loop event waits for that run's words
// (here a failure the catalogue does not know) instead of naming the
// earlier save, while the Server page shows the last reason seen.
func TestCrashLoopEventWaitsForTheCrashedRunsWords(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.crashSettle = 5 * time.Second
	id := r.newWorld("Lagging follower")
	r.start(id)
	ctx := context.Background()
	d := r.game.Daemon()
	d.RefuseLogReads(true) // the poll's read never helps here
	t.Cleanup(func() { d.RefuseLogReads(false); d.HoldFollowers(false) })
	cid := r.container(id).ID
	w := r.c.world(id)
	following := func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.following
	}
	crashKind := func() string { return r.c.Status(id).Crash.Kind }
	takeEvents(r.c)

	// The first crash: its run said nothing, the next failed on its save.
	drainPoke(r.c)
	d.Exit(cid, 1, false)
	awaitPoke(t, r.c)
	d.Restart(cid)
	r.c.tick(ctx)
	eventually(t, following, "the second run to be followed")
	emitLines(t, d, cid, runLoad, runBadZip, runFailed)
	eventually(t, func() bool { return crashKind() == crashreasons.SaveUnreadable }, "the save's failure to be read")
	if got := crashEventsOf(takeEvents(r.c)); !slices.Equal(got, []string{"crash:" + crashreasons.SaveUnreadable}) {
		t.Fatalf("the first crash: %v", got)
	}

	// The third run begins and the follower reads that, then stalls.
	drainPoke(r.c)
	d.Exit(cid, 1, false)
	awaitPoke(t, r.c)
	d.Restart(cid)
	r.c.tick(ctx)
	eventually(t, following, "the third run to be followed")
	emitLines(t, d, cid, runLoad)
	eventually(t, func() bool {
		lines := w.logs.Lines()
		return len(lines) > 0 && strings.HasSuffix(lines[len(lines)-1], "bytes.")
	}, "the third run's start to be read")
	d.HoldFollowers(true)
	emitLines(t, d, cid, runOther, runFailed)
	d.Exit(cid, 1, false)
	d.Restart(cid)
	r.c.tick(ctx)
	if st := r.c.Status(id); st.State != StateCrashLooping || st.Crash.Kind != crashreasons.SaveUnreadable {
		t.Fatalf("the page: state %s, kind %q; want %s showing the last reason seen", st.State, st.Crash.Kind, StateCrashLooping)
	}
	if got := crashEventsOf(takeEvents(r.c)); len(got) != 0 {
		t.Fatalf("sent %v before the run that crashed was read", got)
	}

	d.HoldFollowers(false)
	eventually(t, func() bool { return crashKind() == "" }, "the third run's words to be read")
	r.clock.Advance(6 * time.Second)
	r.c.tick(ctx)
	if got := crashEventsOf(takeEvents(r.c)); !slices.Equal(got, []string{"crash-loop:"}) {
		t.Errorf("after the third run was read and the wait ended: %v, want the loop sent as unknown", got)
	}
}

// SPEC 8.6, 8.7: the poll reads the log only for a restart it has not
// seen, only when the world's secrets are known to mask what it reads,
// and keeps the read only when it reached the log's end.
func TestReadCrashLogLimits(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Limits")
	r.start(id)
	ctx := context.Background()
	d := r.game.Daemon()
	cid := r.container(id).ID
	w := r.c.world(id)
	emitLines(t, d, cid, runLoad, runBadZip, runFailed)
	d.Exit(cid, 1, false)
	d.Restart(cid)
	cont, ops := lookAt(t, r, w, cid)
	reads := func() int {
		n := 0
		for _, c := range d.Calls() {
			if strings.HasSuffix(c.Path, "/logs") && c.Query.Get("follow") != "true" {
				n++
			}
		}
		return n
	}

	before := reads()
	w.mu.Lock()
	w.noSecrets = true
	w.mu.Unlock()
	if got := r.c.readCrashLog(ctx, w, cont, ops); got != nil || reads() != before {
		t.Errorf("read without the secrets to mask it: %v, %d reads", got, reads()-before)
	}
	w.mu.Lock()
	w.noSecrets = false
	w.mu.Unlock()

	seen := cont
	seen.RestartCount = 0
	if got := r.c.readCrashLog(ctx, w, seen, ops); got != nil || reads() != before {
		t.Errorf("read with no new restart: %v", got)
	}

	d.CutLogReads(1)
	if got := r.c.readCrashLog(ctx, w, cont, ops); got != nil {
		t.Errorf("a read cut short was kept: %+v", got)
	}
	d.CutLogReads(0)
	d.RefuseLogReads(true)
	if got := r.c.readCrashLog(ctx, w, cont, ops); got != nil {
		t.Errorf("a refused read was kept: %+v", got)
	}
	d.RefuseLogReads(false)

	got := r.c.readCrashLog(ctx, w, cont, ops)
	if got == nil {
		t.Fatal("a whole read was not kept")
	}
	if r, _, final := got.f.crashReason(masking()); r.Kind != crashreasons.SaveUnreadable || !final {
		t.Errorf("the whole read: kind %q, final %v", r.Kind, final)
	}
}
