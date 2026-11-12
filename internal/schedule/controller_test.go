package schedule

import (
	"context"
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
// online, and an empty server is restarted.
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
	x := &rig{t: t, s: s, clock: clk, ctx: ctx}
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
		t.Fatalf("with nobody online: %+v, want the restart begun", s.State())
	}
	// With nobody online the controller restarts without a warning.
	x.waitPhase(PhaseNone)
	got := c.Status(id)
	if got.State != game.StateRunning || !got.StartedAt.After(before) {
		t.Errorf("after the restart: state %s, started %v (before %v)", got.State, got.StartedAt, before)
	}
	if op := got.Operation; op == nil || op.Action != game.AuditRestart || op.Err != nil {
		t.Errorf("operation = %+v, want a restart that succeeded", op)
	}
}
