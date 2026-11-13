package game

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 8.7: an exit nobody asked for is a crash, found by the
// restart count, even when the exit code is 0 (as 1.1 gives).
func TestCrashDetected(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Crash")
	r.start(id)
	if err := r.game.Crash(ContainerName(id), 0, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return !r.c.Status(id).LastCrash.IsZero() }, "the crash to be seen")
	if n := r.crashCount(id); n != 1 {
		t.Errorf("%d crashes counted, want 1", n)
	}
	r.waitState(id, StateRunning) // Docker brought it back
	if st := r.c.Status(id); st.State == StateCrashLooping {
		t.Error("one crash made a crash loop")
	}
}

// SPEC 8.7: while Docker restarts a crashed game, it shows as crashed.
func TestCrashedWhileLoadingAgain(t *testing.T) {
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 300 * time.Millisecond}})
	id := r.newWorld("Crash slow")
	r.start(id)
	_, ch, stop := r.c.Watch(id)
	defer stop()
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	seen := map[State]bool{}
	deadline := time.After(5 * time.Second)
	for !seen[StateCrashed] {
		select {
		case u := <-ch:
			if u.Kind == UpdateStatus {
				seen[u.Status.State] = true
			}
		case <-deadline:
			t.Fatalf("never shown crashed; saw %v", seen)
		}
	}
	r.waitState(id, StateRunning)
}

// SPEC 8.9: three crashes within ten minutes stop the game and mark it
// crash-looping; it stays stopped until started by hand.
func TestCrashLoopStopsGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Loop")
	r.start(id)
	r.game.SetCrashLoop(ContainerName(id), true)
	r.waitState(id, StateCrashLooping)
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the container to stop")
	n := r.container(id).RestartCount
	time.Sleep(500 * time.Millisecond)
	if cs := r.container(id); cs.Status != "exited" || cs.RestartCount != n {
		t.Fatalf("Docker restarted the crash-looping game: %s, %d restarts", cs.Status, cs.RestartCount)
	}
	if st := r.c.Status(id); st.State != StateCrashLooping {
		t.Errorf("state %s after the stop", st.State)
	}
	if len(r.calls("POST /containers/"+r.container(id).ID+"/kill")) != 0 {
		t.Error("the crash loop used kill, which is for forced stops only (SPEC 14.3)")
	}

	r.game.SetCrashLoop(ContainerName(id), false)
	r.start(id)
	if st := r.c.Status(id); st.State != StateRunning {
		t.Errorf("state %s after starting again", st.State)
	}
}

// SPEC 8.9: crashes further apart than ten minutes are not a loop.
func TestCrashLoopWindow(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Window")
	r.start(id)
	crash := func(want int) {
		t.Helper()
		r.waitState(id, StateRunning)
		if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { return r.crashCount(id) == want }, "%d crashes", want)
	}
	crash(1)
	crash(2)
	r.clock.Advance(CrashLoopWindow + time.Minute)
	crash(1) // the first two fell out of the window
	crash(2)
	r.waitState(id, StateRunning)
	if st := r.c.Status(id); st.State == StateCrashLooping {
		t.Fatal("crashes ten minutes apart made a loop")
	}
	r.waitState(id, StateRunning)
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	r.waitState(id, StateCrashLooping)
}

// SPEC 3.8: Foreman stopping leaves the game running, and a new Foreman
// finds it by its labels and carries on.
func TestReattachAfterForemanRestart(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Survive")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "carol"); err != nil {
		t.Fatal(err)
	}
	cid := r.container(id).ID
	before := len(r.game.State().Calls)
	r.stop()

	for _, call := range r.game.State().Calls[before:] {
		if strings.HasPrefix(call, "POST") || strings.HasPrefix(call, "DELETE") {
			t.Errorf("Foreman's shutdown called %s", call)
		}
	}
	if r.container(id).Status != "running" {
		t.Fatal("the game stopped with Foreman")
	}

	c2 := r.newController(rigOptions{})
	if st := c2.Status(id); st.State != StateUnknown {
		t.Errorf("state %s before the first look, want unknown", st.State)
	}
	start := len(r.game.State().Calls)
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateRunning }, "re-attach")
	for _, call := range r.game.State().Calls[start:] {
		if strings.HasPrefix(call, "POST") || strings.HasPrefix(call, "DELETE") {
			t.Errorf("Foreman's start called %s", call)
		}
	}
	if r.container(id).ID != cid {
		t.Error("the container was replaced")
	}
	players, err := c2.Players(context.Background(), id)
	if err != nil || len(players) != 1 || players[0].Name != "carol" {
		t.Errorf("players %+v, %v", players, err)
	}
	// The follower reads the log on its own goroutine, so its lines may
	// arrive a moment after the state reads running.
	eventually(t, func() bool { return len(c2.Lines(id)) > 0 }, "the log to be followed after re-attaching")
	if err := c2.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("the re-attached game cannot be stopped: %v", err)
	}
}

// SPEC 4.3: containers of another install are not this Foreman's.
func TestOtherInstallIgnored(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.game.Daemon().Seed(dockertest.Container{Name: "foreman-elsewhere", Status: "running",
		Spec: docker.ContainerSpec{Labels: map[string]string{LabelWorld: "elsewhere", LabelInstall: "/srv/other"}}})
	eventually(t, func() bool { return r.c.Status("elsewhere").State == StateStopped }, "the first look")
}

// SPEC 7.1: when Docker cannot be reached the state is unknown, with the reason.
func TestDockerUnreachable(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Lost")
	r.start(id)
	r.game.Daemon().SetFailure(500, "daemon on fire")
	// A check already past its listing may first fail to inspect the
	// container and give the inspect reason; the next check replaces it
	// (TestDockerDownAfterInspectFailure), so wait for the SPEC 7.1 reason.
	r.waitReason(id, StateUnknown, "cannot reach Docker")
	r.game.Daemon().SetFailure(0, "")
	r.waitState(id, StateRunning)
}

// SPEC 7.1: when a check lists the containers and then fails to inspect
// one, and Docker then stays down, the next check gives the reason that
// Docker cannot be reached, not the inspect reason.
func TestDockerDownAfterInspectFailure(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.pollInterval = time.Hour // only pokes make the monitor look
	r.run(r.c)
	id := r.newWorld("Lost")
	r.start(id)
	d := r.game.Daemon()
	d.SetCallFailure("GET", "json", 500, "inspect on fire")
	r.c.pokeMonitor()
	r.waitReason(id, StateUnknown, "cannot read the game container")
	d.SetFailure(500, "daemon on fire")
	r.c.pokeMonitor()
	r.waitReason(id, StateUnknown, "cannot reach Docker")
	d.SetFailure(0, "")
	d.SetCallFailure("GET", "json", 0, "")
	r.c.pokeMonitor()
	r.waitState(id, StateRunning)
}

// SPEC 8.6, 14.8: the RCON and game passwords never reach a page.
func TestLogRedacted(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Secret")
	r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"), []byte(`{"game_password":"hunter2-game"}`))
	r.start(id)
	pw, err := os.ReadFile(filepath.Join(r.worldDir(id), world.RCONPasswordFile))
	if err != nil {
		t.Fatal(err)
	}
	rconPW := strings.TrimSpace(string(pw))
	_, ch, stop := r.c.Watch(id)
	defer stop()
	r.game.Daemon().EmitLog(r.container(id).ID, docker.Stdout, "leak "+rconPW+" and hunter2-game")
	for {
		u := <-ch
		if u.Kind != UpdateLine {
			continue
		}
		if strings.Contains(u.Line, rconPW) || strings.Contains(u.Line, "hunter2-game") {
			t.Fatalf("a secret reached the page: %q", u.Line)
		}
		if strings.HasPrefix(u.Line, "leak ") {
			if u.Line != "leak "+Mask+" and "+Mask {
				t.Errorf("line %q", u.Line)
			}
			break
		}
	}
	v, _, stop2 := r.c.Watch(id)
	defer stop2()
	for _, l := range v.Lines {
		if strings.Contains(l, rconPW) || strings.Contains(l, "hunter2-game") {
			t.Fatalf("a secret is in the kept log: %q", l)
		}
	}
}

// SPEC 8.6: a page gets the log so far and then every new line, with no
// gap and no repeat, however the two race.
func TestWatchHasNoGapsOrRepeats(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stream")
	r.start(id)
	cid := r.container(id).ID
	const n = 200
	go func() {
		for i := range n {
			r.game.Daemon().EmitLog(cid, docker.Stdout, fmt.Sprintf("line %d", i))
		}
	}()
	time.Sleep(time.Millisecond)
	v, ch, stop := r.c.Watch(id)
	defer stop()
	var got []int
	add := func(l string) {
		if s, ok := strings.CutPrefix(l, "line "); ok {
			k, _ := strconv.Atoi(s)
			got = append(got, k)
		}
	}
	for _, l := range v.Lines {
		add(l)
	}
	deadline := time.After(5 * time.Second)
	for len(got) == 0 || got[len(got)-1] != n-1 {
		select {
		case u, ok := <-ch:
			if !ok {
				t.Fatal("dropped as a slow watcher")
			}
			if u.Kind == UpdateLine {
				add(u.Line)
			}
		case <-deadline:
			t.Fatalf("timed out; got %d lines", len(got))
		}
	}
	if got[0] != 0 {
		t.Fatalf("the first line seen is line %d", got[0])
	}
	for i := 1; i < len(got); i++ {
		if got[i] != got[i-1]+1 {
			t.Fatalf("lines %d then %d: a gap or a repeat", got[i-1], got[i])
		}
	}
}

// SPEC 8.1: RCON decides who is on; the log gives since when.
func TestPlayers(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Players")
	if _, err := r.c.Players(context.Background(), id); !errors.Is(err, ErrNotRunning) {
		t.Errorf("players of a stopped game: %v", err)
	}
	r.start(id)
	if err := r.game.Join(ContainerName(id), "dave"); err != nil {
		t.Fatal(err)
	}
	cid := r.container(id).ID
	// A chat line cannot fake a join.
	r.game.Daemon().EmitLog(cid, docker.Stdout, "2026-11-03 10:00:00 [CHAT] eve: 2026-11-03 10:00:00 [JOIN] eve joined the game")
	var players []Player
	eventually(t, func() bool {
		var err error
		players, err = r.c.Players(context.Background(), id)
		return err == nil && len(players) == 1 && !players[0].Joined.IsZero()
	}, "dave with a join time, got %v", func() any { return players })
	if players[0].Name != "dave" || players[0].Duration < 0 {
		t.Errorf("players %+v", players)
	}
	r.clock.Advance(10 * time.Minute)
	later, err := r.c.Players(context.Background(), id)
	if err != nil || later[0].Duration < 10*time.Minute {
		t.Errorf("players %+v, %v: duration does not follow the clock", later, err)
	}
	if err := r.game.Leave(ContainerName(id), "dave"); err != nil {
		t.Fatal(err)
	}
	if p, err := r.c.Players(context.Background(), id); err != nil || len(p) != 0 {
		t.Errorf("players %+v, %v after leaving", p, err)
	}
}

// SPEC 8.4, 12.9: each console command is audited, with the text the
// caller gives, before it is sent.
func TestConsole(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Console")
	if _, _, err := r.c.Console(context.Background(), id, admin, "/help", "/help"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("console of a stopped game: %v", err)
	}
	r.start(id)
	_, reply, err := r.c.Console(context.Background(), id, admin, "/players online", "/players online")
	if err != nil || !strings.HasPrefix(reply, "Online players") {
		t.Fatalf("reply %q, %v", reply, err)
	}
	if _, _, err := r.c.Console(context.Background(), id, admin, "/config set password s3cret", "/config set password "+Mask); err != nil {
		t.Fatal(err)
	}
	entries := r.audit.list()
	last := entries[len(entries)-1]
	if last.Action != AuditConsole || last.Detail != "/config set password "+Mask || last.WorldID != id {
		t.Errorf("audit %+v", last)
	}
	for _, e := range entries {
		if strings.Contains(e.Detail, "s3cret") {
			t.Error("the audit log holds the secret")
		}
	}

	r.audit.setFail(true)
	if _, _, err := r.c.Console(context.Background(), id, admin, "/unaudited", "/unaudited"); err == nil {
		t.Error("a command ran without an audit entry")
	}
	if slices.Contains(r.commands(id), "/unaudited") {
		t.Error("an unaudited command reached the game")
	}
}

// SPEC 7.2: CPU and memory use are read while running.
func TestUsage(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Busy")
	r.start(id)
	var st Status
	eventually(t, func() bool { st = r.c.Status(id); return st.HaveUsage }, "a usage reading")
	s := dockertest.SampleStats
	if st.MemoryBytes != s.MemoryUsage-s.MemoryInactive || st.CPUPercent <= 0 {
		t.Errorf("usage %v %% and %d bytes", st.CPUPercent, st.MemoryBytes)
	}
}

func TestUsageFrom(t *testing.T) {
	u := usageFrom(docker.Stats{CPUTotal: 300, PreCPUTotal: 100, CPUSystem: 2000, PreCPUSystem: 1000,
		OnlineCPUs: 4, MemoryUsage: 1000, MemoryInactive: 200})
	if u.cpu != 80 || u.memory != 800 {
		t.Errorf("usage %+v", u)
	}
	if u := usageFrom(docker.Stats{MemoryUsage: 10, MemoryInactive: 20}); u.cpu != 0 || u.memory != 10 {
		t.Errorf("usage %+v", u)
	}
}

// SPEC 16.3, DESIGN 7.6: a watching page sees the start's steps and states.
func TestStartIsPublished(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Watch")
	_, ch, stop := r.c.Watch(id)
	defer stop()
	r.start(id)
	var states []State
	steps := map[string]bool{}
	for {
		select {
		case u := <-ch:
			if u.Kind != UpdateStatus {
				continue
			}
			if len(states) == 0 || states[len(states)-1] != u.Status.State {
				states = append(states, u.Status.State)
			}
			if op := u.Status.Operation; op != nil {
				for _, s := range op.Steps {
					if s.State == StepProgress {
						steps[s.Name] = true
					}
				}
			}
			continue
		default:
		}
		break
	}
	if !slices.Contains(states, StateStarting) || states[len(states)-1] != StateRunning {
		t.Errorf("states %v", states)
	}
	for _, s := range []string{StepCheck, StepImage, StepCreate, StepStartContainer, StepLoad, StepRCON} {
		if !steps[s] {
			t.Errorf("step %q never shown in progress", s)
		}
	}
}

func TestLoadedSave(t *testing.T) {
	if got := loadedSave("   0.900 Loading map /factorio/saves/my base.zip: 123 bytes."); got != "my base.zip" {
		t.Errorf("got %q", got)
	}
	if got := loadedSave("nothing here"); got != "" {
		t.Errorf("got %q", got)
	}
}

// SPEC 3.8: re-attaching writes nothing into the world, not even a missing
// RCON password file; such a game's log is not relayed unredacted.
func TestReattachWritesNoWorldFile(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("No password")
	r.start(id)
	r.stop()
	if err := os.Remove(filepath.Join(r.worldDir(id), world.RCONPasswordFile)); err != nil {
		t.Fatal(err)
	}
	c2 := r.newController(rigOptions{})
	r.run(c2)
	eventually(t, func() bool { return strings.Contains(c2.Status(id).Reason, "RCON password") }, "unknown, saying why")
	if _, err := os.Lstat(filepath.Join(r.worldDir(id), world.RCONPasswordFile)); err == nil {
		t.Error("re-attaching created the RCON password file")
	}
	if v, _, stop := c2.Watch(id); len(v.Lines) != 0 {
		t.Errorf("relayed %d log lines without the password to redact", len(v.Lines))
	} else {
		stop()
	}
}

// SPEC 3.8: a re-attached game is in game when it answers over RCON, even
// when Docker's log rotation dropped the line that said so.
func TestReattachAfterLogRotated(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Rotated")
	r.start(id)
	r.stop()
	r.game.Daemon().DropLog(r.container(id).ID)
	c2 := r.newController(rigOptions{})
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateRunning }, "the re-attached game to be running, got %s",
		func() any { return c2.Status(id).State })
}

// SPEC 8.6: a log stream that breaks while the game runs resumes where it
// stopped, by Docker's own time for the last line, not Foreman's clock:
// nothing is lost or repeated, and no line shows Docker's timestamp.
func TestLogResumesByDockerTime(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Resume")
	r.start(id)
	r.clock.Advance(time.Hour) // Foreman's clock is ahead of Docker's
	cid := r.container(id).ID
	r.game.Daemon().EmitLog(cid, docker.Stdout, "before the break")
	eventually(t, func() bool { return slices.Contains(r.c.Lines(id), "before the break") }, "the first line")

	w := r.c.world(id)
	w.mu.Lock()
	w.followCancel()
	w.mu.Unlock()
	r.game.Daemon().EmitLog(cid, docker.Stdout, "after the break")
	eventually(t, func() bool { return slices.Contains(r.c.Lines(id), "after the break") }, "the line written during the break")
	time.Sleep(100 * time.Millisecond)
	count := map[string]int{}
	for _, l := range r.c.Lines(id) {
		count[l]++
		if _, err := time.Parse(time.RFC3339Nano, strings.SplitN(l, " ", 2)[0]); err == nil {
			t.Errorf("line %q carries Docker's timestamp", l)
		}
	}
	for l, n := range count {
		if n > 1 && strings.Contains(l, "break") {
			t.Errorf("line %q relayed %d times", l, n)
		}
	}
}

// Closing an RCON connection can wait for a command in flight; pages must
// not wait for it.
func TestDroppingContainerDoesNotBlockStatus(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			accepted <- conn // held open, never answered
		}
	}()
	rc := rcon.New(ln.Addr().String(), secret.New("pw"))
	w := r.c.world("held")
	w.mu.Lock()
	w.containerID, w.dockerStatus, w.rc = "cid", "running", rc
	w.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _, _ = rc.Exec(ctx, "/hang") }()
	conn := <-accepted
	defer func() { _ = conn.Close() }()

	go r.c.observe(w, nil, 0)
	time.Sleep(100 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		r.c.Status("held")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Status waited for the RCON connection to close")
	}
}

// A log follow that fails at once is retried at the next poll, not in a
// tight loop.
func TestFailingLogFollowBacksOff(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.pollInterval = time.Hour // only pokes make the monitor look
	r.run(r.c)
	id := r.newWorld("Backoff")
	r.start(id)
	cid := r.container(id).ID
	r.game.Daemon().SetCallFailure("GET", "logs", 500, "log driver broken")
	w := r.c.world(id)
	w.mu.Lock()
	w.followCancel()
	w.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	if n := len(r.calls("GET /containers/" + cid + "/logs")); n > 3 {
		t.Errorf("%d log calls in 300 ms: the follow retries in a tight loop", n)
	}
}

// SPEC 8.6, 8.7: a game password that also appears in Docker's timestamp
// or the game's seconds prefix is hidden from pages, yet the lines are
// still understood: the game is seen in game and its saves are seen.
func TestPasswordInTimestampStillUnderstood(t *testing.T) {
	year := strconv.Itoa(time.Now().UTC().Year())
	for _, pw := range []string{"0", "2", "11", year, ".", ":", "T"} {
		t.Run(pw, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			id := r.newWorld("Digits")
			r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"), []byte(`{"game_password":"`+pw+`"}`))
			r.start(id)
			if st := r.c.Status(id); st.State != StateRunning {
				t.Fatalf("state %s after start", st.State)
			}
			if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
				t.Fatalf("stop: %v", err)
			}
			v, _, stop := r.c.Watch(id)
			defer stop()
			for _, l := range v.Lines {
				if strings.Contains(l, pw) {
					t.Fatalf("the game password is in the kept log: %q", l)
				}
			}
		})
	}
}

// SPEC 3.8: after a re-attach the follower replays the run's log from its
// start. A replayed "Loading map" line that arrives after the RCON probe
// marked the game in game must not undo that.
func TestReplayedLoadAfterProbeKeepsInGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Replay")
	r.start(id)
	cid := r.container(id).ID
	w := r.c.world(id)
	r.c.handleLine(w, cid, "2026-01-01T00:00:00.000000000Z    0.900 Loading map /factorio/saves/replay.zip: 123 bytes.")
	w.mu.Lock()
	inGame, save := w.run.inGame, w.run.save
	w.mu.Unlock()
	if !inGame {
		t.Error("a replayed load line cleared in game")
	}
	if save != "replay.zip" {
		t.Errorf("save %q, want replay.zip", save)
	}
	if _, err := r.c.Players(context.Background(), id); err != nil {
		t.Errorf("players refused after the replayed line: %v", err)
	}
}

// SPEC 8.7: a `docker restart` by hand starts a new run without raising the
// restart count; the old run's "in game" must not carry over to it.
func TestManualRestartResetsRun(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("ByHand")
	r.start(id)
	if st := r.c.Status(id); st.State != StateRunning {
		t.Fatalf("state %s after start", st.State)
	}
	r.game.Daemon().RestartByHand(ContainerName(id))
	eventually(t, func() bool {
		w := r.c.world(id)
		w.mu.Lock()
		defer w.mu.Unlock()
		return !w.run.inGame
	}, "the new run to be seen as not yet in game")
}

// SPEC 8.6: the save name comes from the raw line, so a short password
// that appears inside it does not mask it; a name that is a password is.
func TestLoadedSaveNotMaskedByShortPassword(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Short")
	r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"), []byte(`{"game_password":"1"}`))
	r.start(id)
	cid := r.container(id).ID
	w := r.c.world(id)
	save := func(name string) string {
		w.mu.Lock()
		w.run = runState{}
		w.mu.Unlock()
		r.c.handleLine(w, cid, "2026-01-01T00:00:00.000000000Z    0.900 Loading map /factorio/saves/"+name+": 123 bytes.")
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.run.save
	}
	if got := save("base1.zip"); got != "base1.zip" {
		t.Errorf("save %q, want base1.zip", got)
	}
	if got := save("1"); got == "1" {
		t.Error("a save named as the password was shown")
	}
}

// SPEC 8.7: a look at Docker taken before a start must not land after it.
// The monitor reads Docker first and takes the world's lock later; a start
// that runs in between would otherwise show its game as stopped, and a
// switch that keeps the run state would then leave the new world stopped.
func TestMonitorDropsALookTakenBeforeAnOperation(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Stale")
	r.start(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	w := r.c.world(id)
	w.mu.Lock()
	ops := w.ops
	w.mu.Unlock()
	stale, err := r.docker.InspectContainer(context.Background(), r.container(id).ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.State.Status == "running" {
		t.Fatalf("the look before the start says %q", stale.State.Status)
	}
	r.start(id)
	if got := r.c.Status(id).State; got != StateRunning {
		t.Fatalf("after the start: %s", got)
	}
	r.c.observeInspect(context.Background(), w, stale, nil, ops)
	if got := r.c.Status(id).State; got != StateRunning {
		t.Errorf("a look from before the start made the running game %s", got)
	}
	r.c.observe(w, nil, ops)
	if got := r.c.Status(id).State; got != StateRunning {
		t.Errorf("an empty list from before the start made the running game %s", got)
	}
}

// SPEC 8.7: a look taken while an operation runs is dropped once it ends,
// since that operation may have changed what the look saw.
func TestMonitorDropsALookTakenDuringAnOperation(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("During")
	r.start(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	w := r.c.world(id)
	// A start whose game is launched after the monitor's look.
	ctx, o := r.c.beginOp(context.Background(), w, AuditStart, startSteps(StartOptions{}))
	w.mu.Lock()
	ops := w.ops
	w.mu.Unlock()
	stale, err := r.docker.InspectContainer(context.Background(), r.container(id).ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.end(r.c.launchLocked(ctx, o)); err != nil {
		t.Fatal(err)
	}
	if got := r.c.Status(id).State; got != StateRunning {
		t.Fatalf("after the start: %s", got)
	}
	r.c.observeInspect(context.Background(), w, stale, nil, ops)
	if got := r.c.Status(id).State; got != StateRunning {
		t.Errorf("a look from during the start made the running game %s", got)
	}
	r.c.observe(w, nil, ops)
	if got := r.c.Status(id).State; got != StateRunning {
		t.Errorf("an empty list from during the start made the running game %s", got)
	}
}

// SPEC 3.8, 8.7: taking on a container from an old look must not undo a
// start that ran since. A fresh process has not taken on the world's
// container yet, so its monitor's look would attach to it; a start then
// replaces that container before the attach runs.
func TestAttachDropsALookTakenBeforeAnOperation(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Attach")
	r.start(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	c := r.newController(rigOptions{noRun: true})
	w := c.world(id)
	w.mu.Lock()
	ops := w.ops
	w.mu.Unlock()
	stale, err := r.docker.InspectContainer(context.Background(), r.container(id).ID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.needsAttach(w, stale, ops) {
		t.Fatal("a fresh process does not want to take on the stopped container")
	}
	if err := c.Start(context.Background(), id, admin, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	type held struct {
		containerID string
		following   bool
		restarts    int
		crashes     int
		startedAt   time.Time
	}
	look := func() held {
		w.mu.Lock()
		defer w.mu.Unlock()
		return held{w.containerID, w.following, w.seenRestarts, len(w.crashes), w.startedAt}
	}
	before := look()
	if before.containerID == stale.ID || before.containerID == "" {
		t.Fatalf("the start did not replace the container: %q", before.containerID)
	}
	if c.needsAttach(w, stale, ops) {
		t.Error("an old look still asks to take on the replaced container")
	}
	c.attach(w, stale, ops)
	if got := look(); got != before {
		t.Errorf("an old look's attach changed the world from %+v to %+v", before, got)
	}
	if got := c.Status(id).State; got != StateRunning {
		t.Errorf("an old look's attach made the running game %s", got)
	}
}
