package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The schedule against the real controller on the fake game: players are
// counted with /players online, a refused RCON login counts as someone
// online, and the warning runs on an empty server, so a join during it is
// cancelled through the game's own Cancel stop and the wait begins again.
func TestWithTheController(t *testing.T) {
	fake := fakegame.New(t, fakegame.Config{Images: []string{game.ImageRef("2.1.24")}})
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	data := t.TempDir()
	worlds, err := world.NewManager(world.Dirs{
		Worlds: filepath.Join(data, "worlds"), Snapshots: filepath.Join(data, "snapshots"), Imports: filepath.Join(data, "import"),
	}, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	dc, err := docker.New(fake.Host())
	if err != nil {
		t.Fatal(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := game.New(game.Config{
		Docker: dc, Worlds: worlds, Audit: st, Switches: st,
		HostDataDir: data, GameUID: 845, GameGID: 845, Log: quiet,
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stopRun := context.WithCancel(ctx)
	ran := make(chan struct{})
	go func() { c.Run(runCtx); close(ran) }()
	t.Cleanup(func() { stopRun(); <-ran })

	id, err := worlds.Create(ctx, "Base", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetActiveWorld(ctx, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	save, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", "2.1.24-no-space-age.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worlds.Dirs().Worlds, id, world.SavesDir, "base.zip"), save, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := c.Start(ctx, id, game.Actor{Username: "todd"}, game.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if s := c.Status(id).State; s != game.StateRunning {
		t.Fatalf("state = %s after the start", s)
	}

	clk := &clock{t: at(3, 0)}
	if err := st.SetRestartSchedule(ctx, *nightly(1), clk.Now()); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, Config{
		Game: c, Store: st, Now: clk.Now, Log: quiet,
		Active: func(context.Context) (string, string, bool, error) { return id, "Base", true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	// The last countdown ends with the test, before the game and store close.
	stepCtx, stopSteps := context.WithCancel(ctx)
	t.Cleanup(func() { stopSteps(); s.restarts.Wait() })
	x := &rig{t: t, s: s, clock: clk, ctx: stepCtx}
	name := game.ContainerName(id)

	if err := fake.Join(name, "alice"); err != nil {
		t.Fatal(err)
	}
	x.stepAt(at(4, 0))
	if st := s.State(); st.Phase != PhaseWaiting || st.Unasked {
		t.Fatalf("with alice online: %+v, want waiting", st)
	}

	fake.SetRCONRefuse(name, true)
	x.stepAt(at(4, 1))
	if st := s.State(); st.Phase != PhaseWaiting || !st.Unasked {
		t.Fatalf("with RCON refused: %+v, want waiting with the game unasked", st)
	}
	fake.SetRCONRefuse(name, false)

	if err := fake.Leave(name, "alice"); err != nil {
		t.Fatal(err)
	}
	before := c.Status(id).StartedAt
	x.stepAt(at(4, 2))
	if s.State().Phase != PhaseCountdown {
		t.Fatalf("with nobody online: %+v, want the countdown", s.State())
	}
	// The warning runs although nobody is online.
	waitWarning(t, c, id)

	// Bob joins during it: the schedule presses Cancel stop and waits again.
	if err := fake.Join(name, "bob"); err != nil {
		t.Fatal(err)
	}
	x.stepAt(at(4, 3))
	if st := x.waitPhase(PhaseWaiting); st.Joined != "bob" || !st.Until.Equal(at(5, 0)) {
		t.Errorf("after bob joined: %+v, want waiting until 05:00", st)
	}
	got := c.Status(id)
	if got.State != game.StateRunning || !got.StartedAt.Equal(before) {
		t.Errorf("after the cancel: state %s, started %v, want the same run going on", got.State, got.StartedAt)
	}
	if op := got.Operation; op == nil || !errors.Is(op.Err, game.ErrStopCancelled) {
		t.Errorf("operation = %+v, want the restart cancelled", op)
	}

	// Bob leaves: the countdown begins again.
	if err := fake.Leave(name, "bob"); err != nil {
		t.Fatal(err)
	}
	x.stepAt(at(4, 10))
	if s.State().Phase != PhaseCountdown {
		t.Fatalf("after bob left: %+v, want the countdown again", s.State())
	}
	waitWarning(t, c, id)
}

// waitWarning waits until the game's restart counts down.
func waitWarning(t *testing.T, c *game.Controller, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if op := c.Status(id).Operation; op != nil && !op.Done {
			if _, ok := op.CountdownEnds(); ok {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the restart's countdown never began: %+v", c.Status(id).Operation)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
