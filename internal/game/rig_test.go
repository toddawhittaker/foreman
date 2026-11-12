package game

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The images the worlds in these tests run: the exact tag of the world's
// game version (SPEC 10.1).
const (
	image11 = "factoriotools/factorio:1.1.110"
	image20 = "factoriotools/factorio:2.0.77"
	image21 = "factoriotools/factorio:2.1.24"
)

var admin = Actor{UserID: 7, Username: "alice"}

// clock is the injected clock: real time plus an offset tests can move.
type clock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (k *clock) Now() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return time.Now().Add(k.offset)
}

func (k *clock) Advance(d time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.offset += d
}

// memAudit is an audit log in memory that can be made to fail.
type memAudit struct {
	mu      sync.Mutex
	entries []store.AuditEntry
	fail    bool
}

func (a *memAudit) AddAudit(_ context.Context, e store.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return errors.New("the database is gone")
	}
	a.entries = append(a.entries, e)
	return nil
}

func (a *memAudit) list() []store.AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.entries)
}

func (a *memAudit) setFail(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fail = on
}

type noActive struct {
	mu sync.Mutex
	id string
}

func (n *noActive) ActiveWorld(context.Context) (string, bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.id, n.id != "", nil
}

func (n *noActive) SetActiveWorld(_ context.Context, id string, _ time.Time) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.id = id
	return nil
}

// rigStore is the app database, with failures a test can inject into the
// switch record.
type rigStore struct {
	*store.Store
	mu          sync.Mutex
	failStep    string // SetSwitchStep to this step fails
	failPresent bool   // ReplaceActiveWorldForSwitch fails
}

var errDatabase = errors.New("the database is gone")

func (s *rigStore) SetSwitchStep(ctx context.Context, from, step string) error {
	s.mu.Lock()
	fail := s.failStep == step
	s.mu.Unlock()
	if fail {
		return errDatabase
	}
	return s.Store.SetSwitchStep(ctx, from, step)
}

func (s *rigStore) ReplaceActiveWorldForSwitch(ctx context.Context, from, to string, now time.Time) error {
	s.mu.Lock()
	fail := s.failPresent
	s.mu.Unlock()
	if fail {
		return errDatabase
	}
	return s.Store.ReplaceActiveWorldForSwitch(ctx, from, to, now)
}

func (s *rigStore) setFail(step string, present bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failStep, s.failPresent = step, present
}

type rig struct {
	t      *testing.T
	game   *fakegame.Game
	docker *docker.Client
	worlds *world.Manager
	audit  *memAudit
	store  *rigStore
	clock  *clock
	data   string
	c      *Controller
	stop   func() // ends the controller's Run
}

type rigOptions struct {
	fake    fakegame.Config
	network string
	selfID  string
	noRun   bool
}

func newRig(t *testing.T, opts rigOptions) *rig {
	t.Helper()
	if opts.fake.Images == nil {
		opts.fake.Images = []string{image11, image20, image21}
	}
	g := fakegame.New(t, opts.fake)
	dc, err := docker.New(g.Host())
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rs := &rigStore{Store: st}
	m, err := world.NewManager(world.Dirs{
		Worlds:    datadir.Worlds(data),
		Snapshots: datadir.Snapshots(data),
		Imports:   datadir.Imports(data),
	}, rs, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, game: g, docker: dc, worlds: m, audit: &memAudit{}, store: rs, clock: &clock{}, data: data}
	r.c = r.newController(opts)
	if !opts.noRun {
		r.stop = r.run(r.c)
	}
	return r
}

// seemInContainer creates the marker file the rig's controllers look for,
// so Foreman appears to run in a container. It changes no controller field,
// which the running monitor reads.
func (r *rig) seemInContainer() {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.data, "no-dockerenv"), nil, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// newController makes a controller on the rig's Docker and data, with
// short limits.
func (r *rig) newController(opts rigOptions) *Controller {
	r.t.Helper()
	cfg := Config{
		Docker: r.docker, Worlds: r.worlds, Audit: r.audit, Switches: r.store,
		HostDataDir: r.data, GameUID: 845, GameGID: 845,
		Network: opts.network, SelfID: opts.selfID,
		Now: r.clock.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	c, err := New(cfg)
	if err != nil {
		r.t.Fatal(err)
	}
	// The test host's own /.dockerenv and DNS must not decide the game network.
	c.containerEnvs = []string{filepath.Join(r.data, "no-dockerenv")}
	c.lookupHost = func(context.Context, string) ([]string, error) { return nil, errors.New("no DNS in tests") }
	c.pollInterval = 20 * time.Millisecond
	c.startTimeout = 5 * time.Second
	c.saveTimeout = time.Second
	c.rconRetry = 20 * time.Millisecond
	c.rconCallLimit = 2 * time.Second
	return c
}

// run starts c's monitor and returns a function that ends it and waits.
func (r *rig) run(c *Controller) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
	r.t.Cleanup(stop)
	return stop
}

// newWorld makes a 2.1 world without Space Age, with one save that the
// pre-start check accepts.
func (r *rig) newWorld(name string) string {
	r.t.Helper()
	id, err := r.worlds.Create(context.Background(), name, "2.1.24", false)
	if err != nil {
		r.t.Fatal(err)
	}
	r.putSave(id, "2.1.24-no-space-age.zip", "base.zip", time.Now().Add(-time.Hour))
	return id
}

func (r *rig) worldDir(id string) string { return filepath.Join(r.worlds.Dirs().Worlds, id) }

func (r *rig) putSave(id, fixture, name string, at time.Time) {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		r.t.Fatal(err)
	}
	r.writeFile(id, filepath.Join(world.SavesDir, name), b)
	if err := os.Chtimes(filepath.Join(r.worldDir(id), world.SavesDir, name), at, at); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) writeFile(id, rel string, b []byte) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.worldDir(id), rel), b, 0o600); err != nil { //nolint:gosec // a test's own temp folder
		r.t.Fatal(err)
	}
}

func (r *rig) start(id string) {
	r.t.Helper()
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); err != nil {
		r.t.Fatalf("start: %v", err)
	}
}

// startAsActive starts a world that is not the active one, by making it
// active for the start only: a state an older Foreman, or a game started
// before a switch was refused, can leave (SPEC 5.3).
func (r *rig) startAsActive(id string) {
	r.t.Helper()
	ctx := context.Background()
	was, _, err := r.store.ActiveWorld(ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.store.SetActiveWorld(ctx, id, time.Now()); err != nil {
		r.t.Fatal(err)
	}
	r.start(id)
	if err := r.store.SetActiveWorld(ctx, was, time.Now()); err != nil {
		r.t.Fatal(err)
	}
}

// container returns the fake's record of the world's container.
func (r *rig) container(id string) fakegame.ContainerState {
	r.t.Helper()
	for _, cs := range r.game.State().Containers {
		if cs.Name == ContainerName(id) {
			return cs
		}
	}
	r.t.Fatalf("no container for %s", id)
	return fakegame.ContainerState{}
}

func (r *rig) commands(id string) []string {
	for _, cs := range r.game.State().Containers {
		if cs.Name == ContainerName(id) {
			return cs.Commands
		}
	}
	return nil
}

func (r *rig) calls(prefix string) []string {
	var out []string
	for _, c := range r.game.State().Calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (r *rig) actions() []string {
	var out []string
	for _, e := range r.audit.list() {
		out = append(out, e.Action)
	}
	return out
}

func (r *rig) waitState(id string, want State) Status {
	r.t.Helper()
	var st Status
	eventually(r.t, func() bool {
		st = r.c.Status(id)
		return st.State == want
	}, "state %s, last %s (%s)", want, func() any { return st.State }, func() any { return st.Reason })
	return st
}

// waitReason waits for the world's state to be want with a reason that
// contains reason.
func (r *rig) waitReason(id string, want State, reason string) Status {
	r.t.Helper()
	var st Status
	eventually(r.t, func() bool {
		st = r.c.Status(id)
		return st.State == want && strings.Contains(st.Reason, reason)
	}, "state %s with reason %q, last %s (%s)", want, reason, func() any { return st.State }, func() any { return st.Reason })
	return st
}

// eventually polls cond for up to five seconds.
func eventually(t *testing.T, cond func() bool, format string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			for i, a := range args {
				if f, ok := a.(func() any); ok {
					args[i] = f()
				}
			}
			t.Fatalf("timed out waiting for "+format, args...)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *rig) crashCount(id string) int {
	w := r.c.world(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.crashes)
}
