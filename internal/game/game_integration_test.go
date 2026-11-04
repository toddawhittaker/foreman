//go:build game

package game

// These tests run the real controller against the real Docker daemon and the
// three pinned factoriotools/factorio images (SPEC 7, 8.1, 8.6, 8.7, 8.9,
// R25 to R35). They are run by `make test-game`, never by `make verify`.
// No game client can join, so a player's join and leave are not covered.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// gameTags are the game versions under test, with the save made by the real
// game for each (internal/savefile/testdata) and the version its image runs.
var gameTags = []struct {
	tag, fixture, version string
	env, image            string // the Makefile variable that pins the image, and its fallback
}{
	{"1.1", "1.1.110-vanilla.zip", "1.1.110", "GAME_IMAGE_1_1", image11},
	{"2.0", "2.0.77-no-space-age.zip", "2.0.77", "GAME_IMAGE_2_0", image20},
	{"2.1", "2.1.24-no-space-age.zip", "2.1.24", "GAME_IMAGE_2_1", image21},
}

// Only one game can hold the host's UDP port 34197, so the images run one
// after another, and one container at a time.
func TestRealGame(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	// The Makefile passes its pinned images, so the pins are written down once (R32).
	e.images = map[string]string{}
	for _, g := range gameTags {
		e.images[g.tag] = g.image
		if v := os.Getenv(g.env); v != "" {
			e.images[g.tag] = v
		}
	}
	total := time.Now()
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			start := time.Now()
			e.requireImage(t, e.images[g.tag])
			e.runImage(t, g.tag, g.fixture, g.version)
			t.Logf("TIMING %s: %s", g.tag, time.Since(start).Round(time.Second))
		})
	}
	t.Logf("TIMING total: %s", time.Since(total).Round(time.Second))
}

type gameEnv struct {
	data   string
	docker *docker.Client
	worlds *world.Manager
	audit  *memAudit
	images map[string]string // by major.minor
	run    string            // random, so no world id clashes with an earlier run
	uid    int
	gid    int
}

func newGameEnv(t *testing.T) *gameEnv {
	t.Helper()
	root := repoRoot(t)
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dc.ListContainers(context.Background(), false, nil); err != nil {
		t.Fatalf("cannot reach Docker at %s: %v", host, err)
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	run := fmt.Sprintf("%x", b)
	// Under the repository's .cache so the path Foreman uses is the host's path.
	base := filepath.Join(root, ".cache", "test-game")
	if err := os.MkdirAll(base, 0o750); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(base, run)
	for _, d := range []string{datadir.Worlds(data), datadir.Snapshots(data), datadir.Imports(data)} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(data) })
	m, err := world.NewManager(world.Dirs{
		Worlds: datadir.Worlds(data), Snapshots: datadir.Snapshots(data), Imports: datadir.Imports(data),
	}, &noActive{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &gameEnv{data: data, docker: dc, worlds: m, audit: &memAudit{}, run: run, uid: os.Getuid(), gid: os.Getgid()}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test's folder")
		}
		dir = parent
	}
}

// requireImage fails when the pinned image is not already on this host;
// Foreman never downloads at start (SPEC 3.7), so neither does this test.
func (e *gameEnv) requireImage(t *testing.T, ref string) {
	t.Helper()
	if _, err := e.docker.InspectImage(context.Background(), ref); err != nil {
		t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", ref, err)
	}
}

// controller makes a Controller on the real Docker and starts its monitor.
// The returned function ends the monitor and waits; the games keep running.
func (e *gameEnv) controller(t *testing.T) (*Controller, func()) {
	t.Helper()
	c, err := New(Config{
		Docker: e.docker, Worlds: e.worlds, Audit: e.audit, HostDataDir: e.data,
		GameUID: e.uid, GameGID: e.gid,
		Images: e.images,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.pollInterval = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return c, stop
}

// newWorld makes a world of the given version with the named saves, each a
// copy of the fixture with the given age, and removes its container at the
// end of the test, whatever the result.
func (e *gameEnv) newWorld(t *testing.T, tag, version, label, fixture string, ages map[string]time.Duration) string {
	t.Helper()
	id, err := e.worlds.Create(context.Background(), fmt.Sprintf("e3test %s %s %s", tag, label, e.run), version, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ContainerName(id), "foreman-e3test-") {
		t.Fatalf("container name %s would not be recognisable as a test container", ContainerName(id))
	}
	t.Cleanup(func() {
		err := e.docker.RemoveContainer(context.Background(), ContainerName(id), true)
		if err != nil && !errors.Is(err, docker.ErrNotFound) {
			t.Errorf("could not remove test container %s: %v", ContainerName(id), err)
		}
	})
	if fixture != "" {
		b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
		if err != nil {
			t.Fatal(err)
		}
		for name, age := range ages {
			e.putSave(t, id, name, b, time.Now().Add(-age))
		}
	}
	return id
}

func (e *gameEnv) worldDir(id string) string { return filepath.Join(datadir.Worlds(e.data), id) }

func (e *gameEnv) putSave(t *testing.T, id, name string, b []byte, at time.Time) {
	t.Helper()
	p := filepath.Join(e.worldDir(id), world.SavesDir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a path inside the test's own data folder
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func (e *gameEnv) saveTime(t *testing.T, id, name string) time.Time {
	t.Helper()
	info, err := os.Stat(filepath.Join(e.worldDir(id), world.SavesDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func (e *gameEnv) inspect(t *testing.T, id string) docker.Container {
	t.Helper()
	cont, err := e.docker.InspectContainer(context.Background(), ContainerName(id))
	if err != nil {
		t.Fatalf("inspect %s: %v", ContainerName(id), err)
	}
	return cont
}

func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", limit, what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitState(t *testing.T, c *Controller, id string, want State, limit time.Duration) Status {
	t.Helper()
	var st Status
	waitFor(t, limit, "state "+string(want), func() bool {
		st = c.Status(id)
		return st.State == want
	})
	return st
}

func logLines(c *Controller, id string) []string {
	v, _, stop := c.Watch(id)
	stop()
	return v.Lines
}

// logHas reports whether a relayed line contains text.
func logHas(c *Controller, id, text string) bool {
	for _, l := range logLines(c, id) {
		if strings.Contains(l, text) {
			return true
		}
	}
	return false
}

func waitLog(t *testing.T, c *Controller, id, text string) {
	t.Helper()
	waitFor(t, 20*time.Second, "log line containing "+text, func() bool { return logHas(c, id, text) })
}

// noSecretInLog checks the controller's buffer for the RCON password (SPEC 8.6).
func (e *gameEnv) noSecretInLog(t *testing.T, c *Controller, id string) {
	t.Helper()
	pw, err := e.worlds.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	lines := logLines(c, id)
	if len(lines) == 0 {
		t.Fatal("the controller relayed no log lines, so the scan proved nothing")
	}
	for i, l := range lines {
		if strings.Contains(l, pw.Reveal()) {
			t.Fatalf("log line %d of %d holds the RCON password", i+1, len(lines)) // the line is not printed
		}
	}
}

// runImage runs every check against one image. The checks share a world
// where one follows from the other, and each is timed.
func (e *gameEnv) runImage(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, stopMonitor := e.controller(t)

	// Two saves; the newer one is not named like any fixed name (R33).
	main := e.newWorld(t, tag, version, "main", fixture, map[string]time.Duration{
		"older.zip": 3 * time.Hour, "newer.zip": 2 * time.Hour,
	})

	step := func(name string, f func(t *testing.T)) {
		t.Helper()
		begin := time.Now()
		ok := t.Run(name, f)
		t.Logf("TIMING %s %s: %s", tag, name, time.Since(begin).Round(100*time.Millisecond))
		if !ok {
			t.FailNow() // later steps depend on this one
		}
	}

	step("1 start reaches InGame and RCON answers", func(t *testing.T) {
		if err := c.Start(ctx, main, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		st := c.Status(main)
		if st.State != StateRunning {
			t.Fatalf("state %s (%s), want running", st.State, st.Reason)
		}
		if st.StartedAt.IsZero() || st.GameVersion != version {
			t.Fatalf("started at %v, game version %q, want a start time and %q", st.StartedAt, st.GameVersion, version)
		}
		if cont := e.inspect(t, main); !cont.State.Running || cont.RestartCount != 0 {
			t.Fatalf("container status %s, restarts %d", cont.State.Status, cont.RestartCount)
		}
		if _, _, err := c.Console(ctx, main, admin, "/version", "/version"); err != nil {
			t.Fatalf("RCON did not answer: %v", err)
		}
	})

	step("2a latest-save start loads the newest save (R33)", func(t *testing.T) {
		st := c.Status(main)
		if st.Save != "newer.zip" {
			t.Fatalf("loaded %q, want newer.zip", st.Save)
		}
		if !logHas(c, main, "Loading map /factorio/saves/newer.zip") {
			t.Fatal("the game's log has no line for loading newer.zip")
		}
	})

	step("3 players online parses with nobody on", func(t *testing.T) {
		t.Log("no game client can join, so only the empty list is checked")
		names, err := c.Players(ctx, main)
		if err != nil || len(names) != 0 {
			t.Fatalf("players = %v, err = %v, want none", names, err)
		}
	})

	step("4 console reply and the stop warning as chat", func(t *testing.T) {
		// The game answers /time to the console with an empty line; record it.
		_, reply, err := c.Console(ctx, main, admin, "/time", "/time")
		if err != nil {
			t.Fatalf("/time: %v", err)
		}
		t.Logf("/time replied: %q", reply)
		// The first /c on a fresh save may reply with nothing (SPEC R25 evidence);
		// the second must answer.
		lua := "/c rcon.print(1 + 1)"
		for range 2 {
			if _, reply, err = c.Console(ctx, main, admin, lua, lua); err != nil || strings.TrimSpace(reply) != "" {
				break
			}
		}
		if err != nil || strings.TrimSpace(reply) != "2" {
			t.Fatalf("a Lua command replied %q, err %v, want 2", reply, err)
		}
		_, reply, err = c.Console(ctx, main, admin, "/version", "/version")
		if err != nil || !strings.Contains(reply, version) {
			t.Fatalf("/version reply %q, err %v, want it to name %s", reply, err, version)
		}
		// Evidence for the stop warning: how the game logs a plain RCON message.
		warning := warningText(30 * time.Second)
		before := len(logLines(c, main))
		if _, _, err := c.Console(ctx, main, admin, warning, "(stop warning)"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
		found := false
		for _, l := range logLines(c, main)[min(before, len(logLines(c, main))):] {
			if strings.Contains(l, "The server will save") {
				found = true
				t.Logf("EVIDENCE the warning is logged as: %q", l)
			}
		}
		if !found {
			t.Logf("EVIDENCE the warning text did not appear in the game's log at all (%d new lines)", len(logLines(c, main))-before)
		}
	})

	step("8 no RCON password in the relayed log", func(t *testing.T) { e.noSecretInLog(t, c, main) })

	step("5 graceful stop saves and ends stopped, not crashed", func(t *testing.T) {
		before := e.saveTime(t, main, "newer.zip")
		began := time.Now()
		if err := c.Stop(ctx, main, admin, StopOptions{Countdown: 2 * time.Second}); err != nil {
			t.Fatalf("stop: %v", err)
		}
		t.Logf("graceful stop took %s", time.Since(began).Round(100*time.Millisecond))
		if st := c.Status(main); st.State != StateStopped {
			t.Fatalf("state %s (%s), want stopped", st.State, st.Reason)
		}
		cont := e.inspect(t, main)
		if cont.State.Running || cont.RestartCount != 0 {
			t.Fatalf("container status %s, restarts %d", cont.State.Status, cont.RestartCount)
		}
		if after := e.saveTime(t, main, "newer.zip"); !after.After(before) || after.Before(began.Add(-time.Second)) {
			t.Fatalf("save time %v, before the stop %v, stop began %v: the save was not written by the stop", after, before, began)
		}
		// The monitor must not call this a crash, then or later.
		time.Sleep(3 * time.Second)
		if st := c.Status(main); st.State != StateStopped || c.crashesIn(main) != 0 {
			t.Fatalf("after waiting: state %s, crashes %d", st.State, c.crashesIn(main))
		}
		waitLog(t, c, main, "Goodbye")
		if !logHas(c, main, "Saving map as /factorio/saves/newer.zip") {
			t.Error("the game's log has no final save line")
		}
	})

	step("9 the save reader reads the save the game just wrote", func(t *testing.T) {
		f, err := os.Open(filepath.Join(e.worldDir(main), world.SavesDir, "newer.zip")) //nolint:gosec // the test's own folder
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		s, err := savefile.Read(f, info.Size())
		if err != nil {
			t.Fatalf("savefile.Read: %v", err)
		}
		if s.Version.String() != version {
			t.Fatalf("save version %s, want %s", s.Version, version)
		}
	})

	step("2b a chosen save loads, then the choice is cleared (R4)", func(t *testing.T) {
		if err := e.worlds.SetLoadSave(main, "older.zip"); err != nil {
			t.Fatal(err)
		}
		if err := c.Start(ctx, main, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		if st := c.Status(main); st.State != StateRunning || st.Save != "older.zip" {
			t.Fatalf("state %s, loaded %q, want running with older.zip", st.State, st.Save)
		}
		list, err := e.worlds.Saves(main)
		if err != nil {
			t.Fatal(err)
		}
		if list.LoadSave != "" {
			t.Fatalf("load_save is still %q after the game loaded", list.LoadSave)
		}
	})

	step("6 forced stop leaves it stopped", func(t *testing.T) {
		if err := c.ForceStop(ctx, main, admin); err != nil {
			t.Fatalf("force stop: %v", err)
		}
		time.Sleep(4 * time.Second) // long enough for Docker to restart it, and for the monitor to look
		cont := e.inspect(t, main)
		if cont.State.Running || cont.RestartCount != 0 {
			t.Fatalf("container status %s, restarts %d", cont.State.Status, cont.RestartCount)
		}
		if st := c.Status(main); st.State != StateStopped || c.crashesIn(main) != 0 {
			t.Fatalf("state %s (%s), crashes %d, want stopped", st.State, st.Reason, c.crashesIn(main))
		}
	})

	step("10 a new controller re-attaches without restarting the game", func(t *testing.T) {
		if err := c.Start(ctx, main, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		first := e.inspect(t, main)
		stopMonitor() // as if Foreman stopped; the game must keep running (SPEC 3.8)
		time.Sleep(2 * time.Second)
		if cont := e.inspect(t, main); !cont.State.Running {
			t.Fatalf("the game stopped when the controller did: %s", cont.State.Status)
		}
		c2, stop2 := e.controller(t)
		st := waitState(t, c2, main, StateRunning, 60*time.Second)
		cont := e.inspect(t, main)
		if cont.ID != first.ID || !cont.State.StartedAt.Equal(first.State.StartedAt) || cont.RestartCount != 0 {
			t.Fatalf("the game was restarted: id %s to %s, started %v to %v, restarts %d",
				first.ID, cont.ID, first.State.StartedAt, cont.State.StartedAt, cont.RestartCount)
		}
		if st.GameVersion != version {
			t.Fatalf("game version %q, want %q", st.GameVersion, version)
		}
		if _, err := c2.Players(ctx, main); err != nil {
			t.Fatalf("players through the new controller: %v", err)
		}
		e.noSecretInLog(t, c2, main)
		stop2()
		if cont := e.inspect(t, main); !cont.State.Running {
			t.Fatalf("the game stopped when the second controller did: %s", cont.State.Status)
		}
		// Only one game can hold the host port, so end this one before the next step.
		if err := c2.ForceStop(ctx, main, admin); err != nil {
			t.Fatalf("force stop: %v", err)
		}
	})

	step("7 a corrupt save is a crash loop and ends stopped", func(t *testing.T) {
		c3, _ := e.controller(t)
		bad := e.newWorld(t, tag, version, "corrupt", "", nil)
		junk := make([]byte, 4096)
		_, _ = rand.Read(junk)
		e.putSave(t, bad, "corrupt.zip", junk, time.Now().Add(-time.Hour))
		err := c3.Start(ctx, bad, admin, StartOptions{Anyway: true})
		if !errors.Is(err, ErrStartFailed) {
			t.Fatalf("start error = %v, want ErrStartFailed", err)
		}
		t.Logf("start said: %v", err)
		st := waitState(t, c3, bad, StateCrashLooping, 90*time.Second)
		if len(st.CrashLines) == 0 || len(st.CrashLines) > CrashLines {
			t.Fatalf("crash lines = %d, want 1 to %d", len(st.CrashLines), CrashLines)
		}
		waitFor(t, 60*time.Second, "the crash-looping container to be stopped", func() bool {
			return !e.inspect(t, bad).State.Running && e.inspect(t, bad).State.Status != "restarting"
		})
		restarts := e.inspect(t, bad).RestartCount
		time.Sleep(5 * time.Second)
		cont := e.inspect(t, bad)
		if cont.State.Running || cont.State.Status == "restarting" || cont.RestartCount != restarts {
			t.Fatalf("after the stop: status %s, restarts %d then %d", cont.State.Status, restarts, cont.RestartCount)
		}
		if st := c3.Status(bad); st.State != StateCrashLooping {
			t.Fatalf("state %s, want crash-looping to stay shown", st.State)
		}
		t.Logf("restarts before Foreman stopped it: %d", restarts)
		e.noSecretInLog(t, c3, bad)
	})
}

// crashesIn is how many crashes the monitor holds for the world.
func (c *Controller) crashesIn(id string) int {
	w := c.world(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.crashes)
}
