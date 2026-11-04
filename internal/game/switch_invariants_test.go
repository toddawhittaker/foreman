package game

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// These tests state what a switch must hold at every instant and after any
// interruption (SPEC 6.5, 7.9, 7.10, 3.8), not what the code happens to do:
//
//   - exactly one world is active, and no game container runs a world that
//     is not the active one;
//   - no save of either world is lost or changed, and the "Before switch"
//     snapshots exist once the step after them was reached;
//   - a refused switch changes nothing;
//   - a running game ends up running, a stopped one ends up stopped;
//   - startup recovery never starts or stops a game.

// invRig is a rig with several worlds, a view of the app database that
// counts active rows, and a check of the instant invariants.
type invRig struct {
	*rig
	ids    []string
	byName map[string]string // container name to world id
	db     *sql.DB
}

func newInvRig(t *testing.T, names ...string) *invRig {
	t.Helper()
	r := newRig(t, rigOptions{})
	v := &invRig{rig: r, byName: map[string]string{}}
	for _, n := range names {
		id := r.newWorld(n)
		v.ids = append(v.ids, id)
		v.byName[ContainerName(id)] = id
		// A second save with content of its own, so that a mix-up between
		// worlds or a rewritten file shows.
		v.addSave(id, "extra.zip", "extra save of "+id, 2*time.Hour)
	}
	v.openDB()
	return v
}

func (v *invRig) addSave(id, name, content string, age time.Duration) {
	v.t.Helper()
	v.writeFile(id, filepath.Join(world.SavesDir, name), []byte(content))
	at := time.Now().Add(-age)
	if err := os.Chtimes(filepath.Join(v.worldDir(id), world.SavesDir, name), at, at); err != nil {
		v.t.Fatal(err)
	}
}

// openDB opens a second, read-only view of the rig's database. The store
// only exposes the first active row, and "exactly one" needs a count.
func (v *invRig) openDB() {
	v.t.Helper()
	// The rig makes the data folder, then the database folder, with
	// t.TempDir, which numbers its folders in order.
	n, err := strconv.Atoi(filepath.Base(v.data))
	if err != nil {
		v.t.Fatal(err)
	}
	dbPath := filepath.Join(filepath.Dir(v.data), fmt.Sprintf("%03d", n+1), "foreman.db")
	if _, err := os.Stat(dbPath); err != nil {
		v.t.Fatalf("cannot find the rig's database: %v", err)
	}
	u := url.URL{Scheme: "file", Path: dbPath}
	q := url.Values{}
	q.Add("mode", "ro")
	q.Add("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		v.t.Fatal(err)
	}
	v.t.Cleanup(func() { _ = db.Close() })
	v.db = db
}

func (v *invRig) activeRows() ([]string, error) {
	rows, err := v.db.QueryContext(context.Background(), "SELECT world_id FROM active_worlds ORDER BY world_id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// violations checks, at this instant, that exactly one world is active and
// that every running game container belongs to it (SPEC 7.10). The active
// row is read before and after the containers, so a switch that completes
// between the reads is not mistaken for a breach: a game running a world
// that was never active during the look is.
func (v *invRig) violations() []string {
	ctx := context.Background()
	var out []string
	first, _, err := v.store.ActiveWorld(ctx)
	if err != nil {
		return []string{"cannot read the active world: " + err.Error()}
	}
	rows, err := v.activeRows()
	if err != nil {
		return []string{"cannot count the active worlds: " + err.Error()}
	}
	if len(rows) != 1 || !slices.Contains(v.ids, rows[0]) {
		out = append(out, fmt.Sprintf("active worlds %v, want exactly one of %v", rows, v.ids))
	}
	st := v.game.State()
	second, _, err := v.store.ActiveWorld(ctx)
	if err != nil {
		return append(out, "cannot read the active world: "+err.Error())
	}
	for _, cs := range st.Containers {
		id, ours := v.byName[cs.Name]
		if ours && cs.Status == "running" && id != first && id != second {
			out = append(out, fmt.Sprintf("%s's game is running but %s/%s is active", id, first, second))
		}
	}
	return out
}

// assertSafe reports any breach of the instant invariants now.
func (v *invRig) assertSafe(label string) {
	v.t.Helper()
	for _, m := range v.violations() {
		v.t.Errorf("%s: %s", label, m)
	}
}

// watch checks the instant invariants every few milliseconds until the
// returned function is called, which fails the test on any breach.
func (v *invRig) watch() (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]bool{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			for _, m := range v.violations() {
				mu.Lock()
				seen[m] = true
				mu.Unlock()
			}
			select {
			case <-done:
				return
			case <-time.After(3 * time.Millisecond):
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			mu.Lock()
			defer mu.Unlock()
			for m := range seen {
				v.t.Errorf("while the switch ran: %s", m)
			}
		})
	}
}

// hashDir maps each file under dir, by slash path, to its SHA-256.
func hashDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a test's own temp folder
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (v *invRig) saves(id string) map[string]string {
	v.t.Helper()
	return hashDir(v.t, filepath.Join(v.worldDir(id), world.SavesDir))
}

// prior holds each world's saves before a switch.
type prior map[string]map[string]string

func (v *invRig) prior() prior {
	p := prior{}
	for _, id := range v.ids {
		p[id] = v.saves(id)
	}
	return p
}

// assertSaves fails when any save of any world is lost, changed or added
// (SPEC 6.5: nothing is written to a save by a switch, failed or not).
func (v *invRig) assertSaves(p prior, label string) {
	v.t.Helper()
	for id, want := range p {
		got := v.saves(id)
		for name, h := range want {
			switch g, ok := got[name]; {
			case !ok:
				v.t.Errorf("%s: %s lost save %s", label, id, name)
			case g != h:
				v.t.Errorf("%s: %s's save %s changed", label, id, name)
			}
		}
		for name := range got {
			if _, ok := want[name]; !ok {
				v.t.Errorf("%s: %s gained file %s", label, id, name)
			}
		}
	}
}

// stepOrder lists the steps a switch record passes through.
var stepOrder = []string{"", store.StepBegun, store.StepStopped, store.StepSnapshotted, store.StepChecked, store.StepPresented, store.StepStarted}

func reached(last, step string) bool {
	return slices.Index(stepOrder, last) >= slices.Index(stepOrder, step)
}

// assertSnapshots fails when the step after the snapshots was reached but
// either world has no "Before switch" snapshot (SPEC 7.9, 6.2).
func (v *invRig) assertSnapshots(a, b, lastStep string) {
	v.t.Helper()
	if !reached(lastStep, store.StepSnapshotted) {
		return
	}
	for _, id := range []string{a, b} {
		snaps := v.switchSnapshots(id)
		if len(snaps) == 0 {
			v.t.Errorf("the switch reached %q but %s has no snapshot", lastStep, id)
			continue
		}
		if snaps[0].Label != world.LabelBeforeSwitch {
			v.t.Errorf("%s's switch snapshot is labelled %q, want %q", id, snaps[0].Label, world.LabelBeforeSwitch)
		}
	}
}

// crashSignal is what a simulated crash panics with: the switch goroutine
// stops where it is, and nothing after that point runs, as when the process
// dies.
type crashSignal struct{}

// tracer is the switch store with a look at every write: before and after
// each, it checks the instant invariants and calls act, which a test uses to
// crash, cancel or damage something at that exact point.
type tracer struct {
	inner SwitchStore
	v     *invRig

	mu        sync.Mutex
	events    []string
	lastStep  string // the last step a write recorded
	presented bool   // the active row was replaced
	found     []string

	act func(idx int, name string)
}

func newTracer(v *invRig, act func(idx int, name string)) *tracer {
	return &tracer{inner: v.store, v: v, act: act}
}

func (tr *tracer) fire(name string) {
	tr.mu.Lock()
	idx := len(tr.events)
	tr.events = append(tr.events, name)
	tr.mu.Unlock()
	for _, m := range tr.v.violations() {
		tr.mu.Lock()
		tr.found = append(tr.found, name+": "+m)
		tr.mu.Unlock()
	}
	if tr.act != nil {
		tr.act(idx, name)
	}
}

func (tr *tracer) write(name string, call func() error, record func()) error {
	tr.fire("before " + name)
	if err := call(); err != nil {
		return err
	}
	tr.mu.Lock()
	record()
	tr.mu.Unlock()
	tr.fire("after " + name)
	return nil
}

func (tr *tracer) step() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.lastStep
}

func (tr *tracer) wasPresented() bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.presented
}

func (tr *tracer) assertNoBreach(t *testing.T) {
	t.Helper()
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, m := range tr.found {
		t.Errorf("at %s", m)
	}
}

func (tr *tracer) ActiveWorld(ctx context.Context) (string, bool, error) {
	return tr.inner.ActiveWorld(ctx)
}

func (tr *tracer) ListSwitches(ctx context.Context) ([]store.Switch, error) {
	return tr.inner.ListSwitches(ctx)
}

func (tr *tracer) BeginSwitch(ctx context.Context, from, to, byUser string, now time.Time) error {
	return tr.write("begin", func() error { return tr.inner.BeginSwitch(ctx, from, to, byUser, now) },
		func() { tr.lastStep = store.StepBegun })
}

func (tr *tracer) SetSwitchStep(ctx context.Context, from, step string) error {
	return tr.write(step, func() error { return tr.inner.SetSwitchStep(ctx, from, step) },
		func() { tr.lastStep = step })
}

func (tr *tracer) ReplaceActiveWorldForSwitch(ctx context.Context, from, to string, now time.Time) error {
	return tr.write(store.StepPresented, func() error { return tr.inner.ReplaceActiveWorldForSwitch(ctx, from, to, now) },
		func() { tr.lastStep, tr.presented = store.StepPresented, true })
}

func (tr *tracer) EndSwitch(ctx context.Context, from string) error {
	tr.fire("before end")
	return tr.inner.EndSwitch(ctx, from)
}

// runSwitch runs the switch in its own goroutine, so a simulated crash
// ends only that goroutine.
func runSwitch(ctx context.Context, c *Controller, from, to string) (err error, crashed bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if p := recover(); p != nil {
				if _, ok := p.(crashSignal); !ok {
					panic(p)
				}
				crashed = true
			}
		}()
		err = c.Switch(ctx, from, to, admin, StopOptions{}, StartOptions{})
	}()
	<-done
	return err, crashed
}

// eventsOfASwitch runs one clean switch and returns the points a crash can
// fall on: before and after each write of the switch record.
func eventsOfASwitch(t *testing.T, running bool) []string {
	t.Helper()
	v := newInvRig(t, "Alpha", "Beta")
	a, b := v.ids[0], v.ids[1]
	if running {
		v.start(a)
	}
	tr := newTracer(v, nil)
	v.c.switches = tr
	if err := v.switchWorlds(a, b); err != nil {
		t.Fatal(err)
	}
	tr.assertNoBreach(t)
	if len(tr.events) < 10 {
		t.Fatalf("a switch made only %d record writes: %v", len(tr.events), tr.events)
	}
	return tr.events
}

// settle waits until the controller has looked at every world's container
// and shows it as running, or as not running.
func (v *invRig) settle(c *Controller) {
	v.t.Helper()
	for _, id := range v.ids {
		running := false
		for _, cs := range v.game.State().Containers {
			if cs.Name == ContainerName(id) && cs.Status == "running" {
				running = true
			}
		}
		eventually(v.t, func() bool {
			s := c.Status(id).State
			if running {
				return s == StateRunning
			}
			return s == StateStopped || s == StateCrashed
		}, "%s to settle (running %v), last %s", id, running, func() any { return c.Status(id).State })
	}
}

func (v *invRig) isRunning(id string) bool {
	for _, cs := range v.game.State().Containers {
		if cs.Name == ContainerName(id) && cs.Status == "running" {
			return true
		}
	}
	return false
}

// mutations returns the Docker calls that change something.
func mutations(calls []string) []string {
	var out []string
	for _, c := range calls {
		if !strings.HasPrefix(c, "GET ") && !strings.HasPrefix(c, "HEAD ") {
			out = append(out, c)
		}
	}
	return out
}

func (v *invRig) statuses() map[string]string {
	out := map[string]string{}
	for _, cs := range v.game.State().Containers {
		out[cs.Name] = cs.Status
	}
	return out
}

func (v *invRig) allCommands() map[string][]string {
	out := map[string][]string{}
	for _, cs := range v.game.State().Containers {
		out[cs.Name] = slices.Clone(cs.Commands)
	}
	return out
}

func (v *invRig) records() []store.Switch {
	v.t.Helper()
	list, err := v.store.ListSwitches(context.Background())
	if err != nil {
		v.t.Fatal(err)
	}
	return list
}

// recoverOnFreshProcess builds a new world manager and controller on the
// same database, data folder and Docker, as a restart does, and runs
// recovery before the monitor (SPEC 3.8). It asserts that recovery made no
// Docker call that changes anything and sent no RCON command.
func (v *invRig) recoverOnFreshProcess() *Controller {
	v.t.Helper()
	m, err := world.NewManager(v.worlds.Dirs(), v.store, nil)
	if err != nil {
		v.t.Fatal(err)
	}
	v.worlds = m
	c := v.newController(rigOptions{})
	callsBefore := len(v.game.State().Calls)
	statusBefore, commandsBefore := v.statuses(), v.allCommands()
	if err := c.Recover(context.Background()); err != nil {
		v.t.Fatal(err)
	}
	if m := mutations(v.game.State().Calls[callsBefore:]); len(m) != 0 {
		v.t.Errorf("recovery changed things in Docker: %v", m)
	}
	if got := v.statuses(); !equalMaps(got, statusBefore) {
		v.t.Errorf("recovery started or stopped a game: %v became %v", statusBefore, got)
	}
	if got := v.allCommands(); fmt.Sprint(got) != fmt.Sprint(commandsBefore) {
		v.t.Errorf("recovery sent RCON commands: %v became %v", commandsBefore, got)
	}
	if left := v.records(); len(left) != 0 {
		v.t.Errorf("records left after recovery: %+v", left)
	}
	return c
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, x := range a {
		if y, ok := b[k]; !ok || x != y {
			return false
		}
	}
	return true
}

// useController makes c the rig's controller and starts its monitor.
func (v *invRig) useController(c *Controller) {
	v.c = c
	v.stop = v.run(c)
}

// SPEC 7.10, 3.8, 6.5, 7.9: if the process dies right before or right after
// any write of the switch record, whatever the game was doing, startup
// leaves exactly one world active, runs no game for a world that is not
// active, keeps every save and the snapshots taken, starts and stops
// nothing, and the admin can still switch.
func TestInvariantCrashAtEveryPointOfASwitch(t *testing.T) {
	for _, running := range []bool{true, false} {
		for i, name := range eventsOfASwitch(t, running) {
			t.Run(fmt.Sprintf("running=%v/%02d %s", running, i, name), func(t *testing.T) {
				crashAndRecover(t, running, i)
			})
		}
	}
}

func crashAndRecover(t *testing.T, running bool, at int) {
	v := newInvRig(t, "Alpha", "Beta")
	a, b := v.ids[0], v.ids[1]
	if running {
		v.start(a)
	}
	pre := v.prior()
	stopWatch := v.watch()
	defer stopWatch()

	tr := newTracer(v, func(idx int, _ string) {
		if idx == at {
			panic(crashSignal{})
		}
	})
	v.c.switches = tr
	if _, crashed := runSwitch(context.Background(), v.c, a, b); !crashed {
		t.Fatal("the switch ended before the crash point")
	}
	v.stop() // the dead process's monitor goes with it; the games keep running
	tr.assertNoBreach(t)
	v.assertSafe("after the crash")

	active := a
	if tr.wasPresented() {
		active = b
	}
	if got := v.active(); got != active {
		t.Errorf("active world %q after the crash, want %q (record at %q)", got, active, tr.step())
	}
	recs := v.records()
	switch last := tr.step(); {
	case last == "" && len(recs) != 0:
		t.Errorf("a record exists although none was written: %+v", recs)
	case last != "" && (len(recs) != 1 || recs[0].Step != last || recs[0].From != a || recs[0].To != b):
		t.Errorf("record %+v, want one at %q from %s to %s", recs, last, a, b)
	}
	v.assertSaves(pre, "after the crash")
	v.assertSnapshots(a, b, tr.step())

	c2 := v.recoverOnFreshProcess()
	if got := v.active(); got != active {
		t.Errorf("recovery changed the active world to %q, want %q", got, active)
	}
	v.assertSafe("after recovery")
	n := c2.Status(active).Notice
	switch {
	case tr.step() == "" && n != nil:
		t.Errorf("notice %+v although no switch began", n)
	case tr.step() != "" && n == nil:
		t.Error("an unfinished switch was not reported")
	case n != nil && n.Presented != tr.wasPresented():
		t.Errorf("notice %+v, want presented=%v at %q", n, tr.wasPresented(), tr.step())
	case n != nil && !n.Presented && !strings.Contains(n.Text, unfinishedStep(tr.step(), c2.worldName(a), c2.worldName(b))):
		t.Errorf("notice %q does not name the step after %q", n.Text, tr.step())
	}
	other := b
	if active == b {
		other = a
	}
	if n := c2.Status(other).Notice; n != nil {
		t.Errorf("notice on %s, which is not active: %+v", other, n)
	}

	// The admin's way out: the monitor takes the games as they are, and a
	// switch from the active world works whatever step the crash fell on.
	v.useController(c2)
	v.settle(c2)
	v.assertSafe("after the monitor took over")
	if err := v.switchWorlds(active, other); err != nil {
		t.Fatalf("switch after recovery: %v", err)
	}
	if got := v.active(); got != other {
		t.Errorf("active world %q after the switch, want %q", got, other)
	}
	v.assertSnapshots(a, b, store.StepStarted)
	v.noRecord()
	v.assertSaves(pre, "after recovery and a switch")
	stopWatch()
}

// SPEC 7.10, 7.9: a switch cancelled (a forced stop, a closing request) at
// any point, including a write refused because the context ended, leaves one
// active world and no game for the other, keeps every save, clears its
// record, and can be walked back.
func TestInvariantCancelAtEveryPointOfASwitch(t *testing.T) {
	for _, running := range []bool{true, false} {
		for i, name := range eventsOfASwitch(t, running) {
			t.Run(fmt.Sprintf("running=%v/%02d %s", running, i, name), func(t *testing.T) {
				v := newInvRig(t, "Alpha", "Beta")
				a, b := v.ids[0], v.ids[1]
				if running {
					v.start(a)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				tr := newTracer(v, func(idx int, _ string) {
					if idx == i {
						cancel()
					}
				})
				v.c.switches = tr
				pre := v.prior()
				stopWatch := v.watch()
				defer stopWatch()
				err, _ := runSwitch(ctx, v.c, a, b)
				_ = err // the very last points leave nothing to cancel
				v.afterFailedSwitch(a, b, tr, pre)
				stopWatch()
			})
		}
	}
}

// afterFailedSwitch checks the state a failed or interrupted switch leaves,
// then takes the way back the page offers (SPEC 7.9): a switch back when
// the new world was made active, otherwise a start of the old one. Both
// worlds' saves must be exactly as before.
func (v *invRig) afterFailedSwitch(a, b string, tr *tracer, pre prior) {
	v.t.Helper()
	tr.assertNoBreach(v.t)
	v.assertSafe("after the switch")
	v.noRecord()
	active := a
	if tr.wasPresented() {
		active = b
	}
	if got := v.active(); got != active {
		v.t.Errorf("active world %q, want %q (record reached %q)", got, active, tr.step())
	}
	v.assertSaves(pre, "after the failed switch")
	v.assertSnapshots(a, b, tr.step())

	// The monitor looks at the games again once the switch has ended.
	v.c.pokeMonitor()
	time.Sleep(100 * time.Millisecond)
	v.assertSafe("after the monitor looked")

	if active == b {
		if err := v.switchWorlds(b, a); err != nil {
			v.t.Fatalf("switch back: %v", err)
		}
	}
	if got := v.active(); got != a {
		v.t.Errorf("active world %q after the way back, want %q", got, a)
	}
	if !v.isRunning(a) && v.c.Status(a).State != StateRunning {
		if err := v.c.Start(context.Background(), a, admin, StartOptions{}); err != nil {
			v.t.Fatalf("start of the old world: %v", err)
		}
	}
	v.assertSafe("after the way back")
	v.noRecord()
	v.assertSaves(pre, "after the way back")
}

type failureCase struct {
	name string
	// setup injects the fault and returns how to remove it. It runs with
	// the old world running or stopped, as the case allows.
	setup func(v *invRig, a, b string) (undo func())
	// onlyRunning skips the stopped variant, when the fault is in the stop.
	onlyRunning bool
	// afterSnapshots runs once both snapshots are taken, and may change the
	// world: the saves it leaves are the new baseline.
	afterSnapshots func(v *invRig, a, b string)
}

func failureCases() []failureCase {
	blockSnapshots := func(which int) func(v *invRig, a, b string) func() {
		return func(v *invRig, a, b string) func() {
			path := filepath.Join(v.worlds.Dirs().Snapshots, []string{a, b}[which])
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				v.t.Fatal(err)
			}
			return func() { _ = os.Remove(path) }
		}
	}
	failRecord := func(step string) func(v *invRig, a, b string) func() {
		return func(v *invRig, _, _ string) func() {
			v.store.setFail(step, false)
			return func() { v.store.setFail("", false) }
		}
	}
	callFails := func(action string) func(v *invRig, a, b string) func() {
		return func(v *invRig, _, _ string) func() {
			v.game.Daemon().SetCallFailure("POST", action, 500, "daemon busy")
			return func() { v.game.Daemon().SetCallFailure("POST", action, 0, "") }
		}
	}
	return []failureCase{
		{name: "RCON of the old game unreachable", onlyRunning: true, setup: func(v *invRig, a, _ string) func() {
			v.game.SetRCONRefuse(ContainerName(a), true)
			return func() { v.game.SetRCONRefuse(ContainerName(a), false) }
		}},
		{name: "Docker refuses the stop", onlyRunning: true, setup: callFails("stop")},
		{name: "record stopped", setup: failRecord(store.StepStopped)},
		{name: "snapshot of the old world", setup: blockSnapshots(0)},
		{name: "snapshot of the new world", setup: blockSnapshots(1)},
		{name: "record snapshotted", setup: failRecord(store.StepSnapshotted)},
		{name: "record checked", setup: failRecord(store.StepChecked)},
		{name: "making the new world active", setup: func(v *invRig, _, _ string) func() {
			v.store.setFail("", true)
			return func() { v.store.setFail("", false) }
		}},
		{name: "new world's RCON unreachable", onlyRunning: true, setup: func(v *invRig, _, b string) func() {
			v.game.SetRCONRefuse(ContainerName(b), true)
			return func() { v.game.SetRCONRefuse(ContainerName(b), false) }
		}},
		{name: "Docker refuses to start the new game", onlyRunning: true, setup: callFails("start")},
		{name: "new world's save breaks after the snapshots", afterSnapshots: func(v *invRig, _, b string) {
			v.writeFile(b, filepath.Join(world.SavesDir, "base.zip"), []byte("now broken"))
		}, setup: func(v *invRig, _, b string) func() {
			// Named before the switch, so the earlier check still passes.
			if err := v.worlds.SetLoadSave(b, "base.zip"); err != nil {
				v.t.Fatal(err)
			}
			return func() {}
		}},
	}
}

// SPEC 7.9, 7.10: a failure at any step leaves one active world, no game
// for the other, every save as it was, and a way back that works.
func TestInvariantFailedSwitchKeepsEverything(t *testing.T) {
	for _, fc := range failureCases() {
		for _, running := range []bool{true, false} {
			if !running && fc.onlyRunning {
				continue
			}
			t.Run(fmt.Sprintf("%s/running=%v", fc.name, running), func(t *testing.T) {
				v := newInvRig(t, "Alpha", "Beta")
				a, b := v.ids[0], v.ids[1]
				if running {
					v.start(a)
				}
				undo := fc.setup(v, a, b)
				pre := v.prior()
				tr := newTracer(v, func(_ int, name string) {
					if name == "after "+store.StepSnapshotted && fc.afterSnapshots != nil {
						fc.afterSnapshots(v, a, b)
					}
				})
				v.c.switches = tr
				stopWatch := v.watch()
				defer stopWatch()
				if err := v.switchWorlds(a, b); err == nil {
					t.Fatal("the switch worked although a step was made to fail")
				}
				undo()
				v.store.setFail("", false)
				if fc.afterSnapshots != nil {
					pre = v.prior() // the world was changed on purpose
				}
				v.afterFailedSwitch(a, b, tr, pre)
				stopWatch()
			})
		}
	}
}

// observation is what a refused switch must leave untouched.
type observation struct {
	mutations []string
	commands  string
	statuses  map[string]string
	snapshots map[string]int
	records   []store.Switch
	active    string
	audit     int
	saves     prior
}

func (v *invRig) observe() observation {
	v.t.Helper()
	o := observation{
		mutations: mutations(v.game.State().Calls),
		commands:  fmt.Sprint(v.allCommands()),
		statuses:  v.statuses(),
		snapshots: map[string]int{},
		records:   v.records(),
		active:    v.active(),
		audit:     len(v.audit.list()),
		saves:     v.prior(),
	}
	for _, id := range v.ids {
		all, err := v.worlds.Snapshots(id)
		if err != nil {
			v.t.Fatal(err)
		}
		o.snapshots[id] = len(all)
	}
	return o
}

// SPEC 7.8, 7.9, 12.9: a switch that would be refused changes nothing
// before the checks pass: the old game keeps running and is sent nothing,
// no snapshot is taken, no record is written, the active world stays. A
// refusal before the audit entry leaves no entry; one after it leaves the
// one entry for the attempt, as a refused restart does.
func TestInvariantRefusedSwitchChangesNothing(t *testing.T) {
	lockWorld := func(which int) func(v *invRig, a, b string) (string, string) {
		return func(v *invRig, a, b string) (string, string) {
			unlock, err := v.worlds.Lock([]string{a, b}[which])
			if err != nil {
				v.t.Fatal(err)
			}
			v.t.Cleanup(unlock)
			return a, b
		}
	}
	cases := []struct {
		name    string
		audited int // audit entries the attempt may leave
		stop    StopOptions
		setup   func(v *invRig, a, b string) (from, to string)
		wantErr error
		// othersGameRuns is set when the case starts the new world's game by
		// hand, which is itself a state the invariant forbids.
		othersGameRuns bool
	}{
		{name: "same world", setup: func(_ *invRig, a, _ string) (string, string) { return a, a }, wantErr: ErrSwitchSame},
		{name: "countdown out of range", stop: StopOptions{Countdown: MaxStopCountdown + time.Second},
			setup: func(_ *invRig, a, b string) (string, string) { return a, b }, wantErr: ErrCountdown},
		{name: "old world busy", setup: lockWorld(0), wantErr: world.ErrBusy},
		{name: "new world busy", setup: lockWorld(1), wantErr: world.ErrBusy},
		{name: "no switch store", setup: func(v *invRig, a, b string) (string, string) {
			v.c.switches = nil
			return a, b
		}, wantErr: ErrSwitchUnavailable},
		{name: "audit log cannot be written", setup: func(v *invRig, a, b string) (string, string) {
			v.audit.setFail(true)
			return a, b
		}},
		{name: "from is not active", audited: 1, setup: func(_ *invRig, a, b string) (string, string) { return b, a }, wantErr: ErrSwitchNotActive},
		{name: "new world missing", audited: 1, setup: func(_ *invRig, a, _ string) (string, string) { return a, "gamma" }, wantErr: ErrSwitchNoTarget},
		{name: "image missing", audited: 1, setup: func(v *invRig, a, b string) (string, string) {
			v.game.SetImage(image21, false)
			return a, b
		}, wantErr: ErrImageMissing},
		{name: "new world already running", audited: 1, othersGameRuns: true, setup: func(v *invRig, a, b string) (string, string) {
			v.startAsActive(b)
			return a, b
		}, wantErr: ErrSwitchTargetRunning},
		{name: "pre-start check refuses the new world", audited: 1, setup: func(v *invRig, a, b string) (string, string) {
			if err := v.worlds.SetLoadSave(b, "base.zip"); err != nil {
				v.t.Fatal(err)
			}
			v.writeFile(b, filepath.Join(world.SavesDir, "base.zip"), []byte("now broken"))
			return a, b
		}},
		{name: "game network refused", audited: 1, setup: func(v *invRig, a, b string) (string, string) {
			// Inside a container, and no way to tell which networks it is on.
			v.seemInContainer()
			return a, b
		}, wantErr: ErrNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newInvRig(t, "Alpha", "Beta")
			a, b := v.ids[0], v.ids[1]
			v.start(a)
			from, to := tc.setup(v, a, b)
			before := v.observe()
			err := v.c.Switch(context.Background(), from, to, admin, tc.stop, StartOptions{})
			switch {
			case err == nil:
				t.Fatal("the switch was not refused")
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			after := v.observe()
			if len(after.mutations) != len(before.mutations) {
				t.Errorf("Docker was changed: %v", after.mutations[len(before.mutations):])
			}
			if after.commands != before.commands {
				t.Error("the running game was sent RCON commands")
			}
			if !equalMaps(after.statuses, before.statuses) || after.statuses[ContainerName(a)] != "running" {
				t.Errorf("containers were %v, now %v", before.statuses, after.statuses)
			}
			if fmt.Sprint(after.snapshots) != fmt.Sprint(before.snapshots) {
				t.Errorf("snapshots were %v, now %v", before.snapshots, after.snapshots)
			}
			if fmt.Sprint(after.records) != fmt.Sprint(before.records) {
				t.Errorf("switch records were %+v, now %+v", before.records, after.records)
			}
			if after.active != before.active {
				t.Errorf("the active world changed to %q", after.active)
			}
			if added := after.audit - before.audit; added != tc.audited {
				t.Errorf("%d audit entries added, want %d: %v", added, tc.audited, v.actions())
			}
			if tc.audited == 1 {
				// SPEC 12.9: a refusal is its own action, with the reason.
				if e := v.audit.list()[after.audit-1]; e.Action != AuditSwitchRefused || e.WorldID != from ||
					!strings.HasPrefix(e.Detail, "from "+from+" to "+to+": ") || !strings.Contains(e.Detail, err.Error()) {
					t.Errorf("audit entry %+v, want a refused switch from %s with the reason", e, from)
				}
			}
			v.assertSaves(before.saves, "after the refusal")
			if !tc.othersGameRuns {
				v.assertSafe("after the refusal")
			}
			if tc.name != "no switch store" {
				v.noRecord()
			}
		})
	}
}

// SPEC 7.9: a switch leaves the game as it found it. A running game ends running on the new world, a
// stopped one ends stopped with the new world active, over a round trip, and
// each switch snapshots both worlds.
func TestInvariantSwitchKeepsRunState(t *testing.T) {
	cases := []struct {
		name  string
		setup func(v *invRig, a string)
		run   bool
	}{
		{"running", func(v *invRig, a string) { v.start(a) }, true},
		{"never started", func(*invRig, string) {}, false},
		{"started then stopped", func(v *invRig, a string) {
			v.start(a)
			if err := v.c.Stop(context.Background(), a, admin, StopOptions{}); err != nil {
				v.t.Fatal(err)
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newInvRig(t, "Alpha", "Beta")
			a, b := v.ids[0], v.ids[1]
			tc.setup(v, a)
			pre := v.prior()
			stopWatch := v.watch()
			defer stopWatch()
			from, to := a, b
			for trip := 1; trip <= 3; trip++ {
				if err := v.switchWorlds(from, to); err != nil {
					t.Fatalf("switch %d: %v", trip, err)
				}
				if got := v.active(); got != to {
					t.Fatalf("switch %d: active %q, want %q", trip, got, to)
				}
				if tc.run {
					v.waitState(to, StateRunning)
				}
				if got := v.isRunning(to); got != tc.run {
					t.Errorf("switch %d: new world running %v, want %v", trip, got, tc.run)
				}
				if v.isRunning(from) {
					t.Errorf("switch %d: the old world is still running", trip)
				}
				if st := v.c.Status(to).State; !tc.run && st == StateRunning || tc.run && st != StateRunning {
					t.Errorf("switch %d: state %s with running=%v", trip, st, tc.run)
				}
				for _, id := range []string{a, b} {
					if n := len(v.switchSnapshots(id)); n < 1 {
						t.Errorf("switch %d: %s has no switch snapshot", trip, id)
					}
				}
				v.noRecord()
				from, to = to, from
			}
			v.assertSaves(pre, "after the round trips")
			stopWatch()
		})
	}
}

// A seeded random walk over three worlds: start, stop, switch and a new
// autosave in the active world, in any order. After every step the instant
// invariants hold, a switch kept the run state, and no save ever vanished or
// changed.
func TestInvariantRandomWalk(t *testing.T) {
	for _, seed := range []int64{1, 2, 3} {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rnd := rand.New(rand.NewSource(seed)) //nolint:gosec // a repeatable test sequence
			v := newInvRig(t, "Alpha", "Beta", "Gamma")
			ctx := context.Background()
			known := v.prior()
			stopWatch := v.watch()
			defer stopWatch()
			for step := 0; step < 14; step++ {
				active := v.active()
				running := v.isRunning(active)
				switch op := rnd.Intn(5); op {
				case 0:
					if !running {
						v.start(active)
					}
				case 1:
					if running {
						if err := v.c.Stop(ctx, active, admin, StopOptions{}); err != nil {
							t.Fatalf("step %d: stop: %v", step, err)
						}
					}
				case 2:
					name := fmt.Sprintf("autosave-%d.zip", step)
					v.addSave(active, name, fmt.Sprintf("autosave %d of %s", step, active), time.Duration(step+3)*time.Hour)
					known[active] = v.saves(active)
				default:
					var others []string
					for _, id := range v.ids {
						if id != active {
							others = append(others, id)
						}
					}
					to := others[rnd.Intn(len(others))]
					if err := v.switchWorlds(active, to); err != nil {
						t.Fatalf("step %d: switch %s to %s: %v", step, active, to, err)
					}
					if got := v.active(); got != to {
						t.Fatalf("step %d: active %q, want %q", step, got, to)
					}
					if running {
						v.waitState(to, StateRunning)
					}
					if got := v.isRunning(to); got != running {
						t.Errorf("step %d: new world running %v, want %v", step, got, running)
					}
					if v.isRunning(active) {
						t.Errorf("step %d: the old world still runs", step)
					}
				}
				for _, id := range v.ids {
					if id != v.active() && v.isRunning(id) {
						t.Errorf("step %d: %s runs but %s is active", step, id, v.active())
					}
				}
				v.assertSaves(known, fmt.Sprintf("step %d", step))
				v.noRecord()
			}
			stopWatch()
		})
	}
}

// SPEC 3.8, 7.10: startup recovery never starts, stops, creates or removes a
// game, and sends it nothing, whatever step the record holds and whichever
// games are up. Running the monitor afterwards changes nothing either.
func TestInvariantRecoveryNeverTouchesAGame(t *testing.T) {
	steps := []string{store.StepBegun, store.StepStopped, store.StepSnapshotted, store.StepChecked, store.StepPresented, store.StepStarted}
	layouts := []struct {
		name  string
		setup func(v *invRig, a, b string)
	}{
		{"nothing ever started", func(*invRig, string, string) {}},
		{"old world running", func(v *invRig, a, _ string) { v.start(a) }},
		{"new world running", func(v *invRig, _, b string) { v.startAsActive(b) }},
		{"both stopped", func(v *invRig, a, b string) {
			for _, id := range []string{a, b} {
				v.startAsActive(id)
				if err := v.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
					v.t.Fatal(err)
				}
			}
		}},
	}
	for _, l := range layouts {
		for _, step := range steps {
			t.Run(l.name+"/"+step, func(t *testing.T) {
				v := newInvRig(t, "Alpha", "Beta")
				a, b := v.ids[0], v.ids[1]
				l.setup(v, a, b)
				ctx := context.Background()
				if err := v.store.BeginSwitch(ctx, a, b, admin.Username, time.Now()); err != nil {
					t.Fatal(err)
				}
				if reached(step, store.StepPresented) {
					if err := v.store.ReplaceActiveWorldForSwitch(ctx, a, b, time.Now()); err != nil {
						t.Fatal(err)
					}
				} else if step != store.StepBegun {
					if err := v.store.Store.SetSwitchStep(ctx, a, step); err != nil {
						t.Fatal(err)
					}
				}
				if step == store.StepStarted {
					if err := v.store.Store.SetSwitchStep(ctx, a, step); err != nil {
						t.Fatal(err)
					}
				}
				pre := v.prior()
				statusBefore := v.statuses()
				c := v.recoverOnFreshProcess()
				// A second pass finds nothing, and does nothing.
				if err := c.Recover(ctx); err != nil {
					t.Fatal(err)
				}
				want := a
				if reached(step, store.StepPresented) {
					want = b
				}
				if got := v.active(); got != want {
					t.Errorf("active world %q, want %q", got, want)
				}
				// Some layouts are built by hand into states a real switch never
				// reaches; recovery must leave those alone, not judge them.
				built := false
				for _, id := range v.ids {
					if id != want && statusBefore[ContainerName(id)] == "running" {
						built = true
					}
				}
				if !built {
					v.assertSafe("after recovery")
				}

				// The monitor's first looks do not change a game either.
				callsBefore := len(v.game.State().Calls)
				v.useController(c)
				time.Sleep(200 * time.Millisecond)
				if m := mutations(v.game.State().Calls[callsBefore:]); len(m) != 0 {
					t.Errorf("the monitor changed things in Docker after recovery: %v", m)
				}
				if got := v.statuses(); !equalMaps(got, statusBefore) {
					t.Errorf("games went from %v to %v", statusBefore, got)
				}
				v.assertSaves(pre, "after recovery")
			})
		}
	}
}

// SPEC 7.9, 7.10: two switches started at once from the same world cannot
// both go ahead. Whatever order they land in, the end state is one active
// world, one game at most and it is the active one, no record, and every
// save as it was.
func TestInvariantConcurrentSwitches(t *testing.T) {
	pairs := []struct {
		name string
		// each is from, to as an index into the three worlds
		first, second [2]int
		exactlyOne    bool
	}{
		{"two targets from one world", [2]int{0, 1}, [2]int{0, 2}, true},
		{"a chain through the middle world", [2]int{0, 1}, [2]int{1, 2}, false},
		{"opposite directions", [2]int{0, 1}, [2]int{1, 0}, false},
	}
	for _, p := range pairs {
		for _, running := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/running=%v", p.name, running), func(t *testing.T) {
				v := newInvRig(t, "Alpha", "Beta", "Gamma")
				if running {
					v.start(v.ids[0])
				}
				pre := v.prior()
				stopWatch := v.watch()
				defer stopWatch()
				var wg sync.WaitGroup
				errs := make([]error, 2)
				gate := make(chan struct{})
				for i, pair := range [][2]int{p.first, p.second} {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-gate
						errs[i] = v.switchWorlds(v.ids[pair[0]], v.ids[pair[1]])
					}()
				}
				close(gate)
				wg.Wait()
				ok := 0
				for _, err := range errs {
					if err == nil {
						ok++
					}
				}
				// Two switches that need the same middle world may both be
				// refused; that is safe and changes nothing.
				if p.exactlyOne && ok != 1 {
					t.Errorf("%d switches worked, errors %v", ok, errs)
				}
				v.noRecord()
				v.assertSaves(pre, "after the concurrent switches")
				v.assertSafe("after the concurrent switches")
				n := 0
				for _, id := range v.ids {
					if v.isRunning(id) {
						n++
						if id != v.active() {
							t.Errorf("%s runs but %s is active", id, v.active())
						}
					}
				}
				if n > 1 || running && n != 1 || !running && n != 0 {
					t.Errorf("%d games running, started with running=%v", n, running)
				}
				stopWatch()
			})
		}
	}
}

// SPEC 6.5, 7.9: the "Before switch" snapshot of each world puts it back
// exactly as it was when the switch began.
func TestInvariantSwitchSnapshotRestoresExactly(t *testing.T) {
	for _, running := range []bool{true, false} {
		t.Run(fmt.Sprintf("running=%v", running), func(t *testing.T) {
			v := newInvRig(t, "Alpha", "Beta")
			a, b := v.ids[0], v.ids[1]
			if running {
				v.start(a)
			}
			trees := map[string]map[string]string{}
			for _, id := range v.ids {
				// The switch's check makes a missing mod list; have it already,
				// so the tree before is the tree the snapshot holds.
				if err := v.worlds.EnsureModList(id); err != nil {
					t.Fatal(err)
				}
				trees[id] = hashDir(t, v.worldDir(id))
			}
			if err := v.switchWorlds(a, b); err != nil {
				t.Fatal(err)
			}
			if running {
				v.waitState(b, StateRunning)
				if err := v.c.Stop(context.Background(), b, admin, StopOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			for _, id := range v.ids {
				snaps := v.switchSnapshots(id)
				if len(snaps) != 1 {
					t.Fatalf("%s has %d switch snapshots, want 1", id, len(snaps))
				}
				// Damage the world in the ways a bad day can.
				// A save is replaced, never rewritten in place (SPEC 6.8).
				if err := os.Remove(filepath.Join(v.worldDir(id), world.SavesDir, "extra.zip")); err != nil {
					t.Fatal(err)
				}
				v.writeFile(id, filepath.Join(world.SavesDir, "extra.zip"), []byte("damaged"))
				v.writeFile(id, filepath.Join(world.SavesDir, "stray.zip"), []byte("not in the snapshot"))
				if err := os.Remove(filepath.Join(v.worldDir(id), world.SavesDir, "base.zip")); err != nil {
					t.Fatal(err)
				}
				if _, err := v.worlds.Restore(context.Background(), id, snaps[0].Name, admin.Username); err != nil {
					t.Fatalf("restore %s: %v", id, err)
				}
				got := hashDir(t, v.worldDir(id))
				if !equalMaps(got, trees[id]) {
					t.Errorf("%s restored to %v, was %v", id, got, trees[id])
				}
			}
		})
	}
}

// SPEC 6.2, 7.8, 7.9: a switch that is refused writes no file in either
// world, because nothing may change a world before the snapshot of a switch
// that goes ahead. The target's pre-start check is the one thing that runs
// first, and it must be read-only.
func TestInvariantRefusedSwitchWritesNoWorldFile(t *testing.T) {
	cases := []struct {
		name  string
		setup func(v *invRig, a, b string) (from, to string)
	}{
		{"from is not active", func(_ *invRig, a, b string) (string, string) { return b, a }},
		{"image missing", func(v *invRig, a, b string) (string, string) {
			v.game.SetImage(image21, false)
			return a, b
		}},
		{"the game network is refused", func(v *invRig, a, b string) (string, string) {
			// Foreman looks as if it runs in a container it cannot find (SPEC 4.4).
			v.seemInContainer()
			return a, b
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newInvRig(t, "Alpha", "Beta")
			a, b := v.ids[0], v.ids[1]
			v.start(a)
			from, to := tc.setup(v, a, b)
			before := map[string]map[string]string{}
			for _, id := range v.ids {
				before[id] = hashDir(t, v.worldDir(id))
			}
			if err := v.switchWorlds(from, to); err == nil {
				t.Fatal("the switch was not refused")
			}
			for _, id := range v.ids {
				if got := hashDir(t, v.worldDir(id)); !equalMaps(got, before[id]) {
					t.Errorf("%s's files were %v, now %v", id, before[id], got)
				}
			}
		})
	}
}

// SPEC 7.3, 7.9: a start or restart of a world that is not
// the active one is refused before anything runs, so a container only ever
// runs the active world.
func TestInvariantStartOfANonActiveWorldRunsNoContainer(t *testing.T) {
	for _, action := range []string{"start", "restart"} {
		t.Run(action, func(t *testing.T) {
			v := newInvRig(t, "Alpha", "Beta")
			b := v.ids[1]
			var err error
			if action == "start" {
				err = v.c.Start(context.Background(), b, admin, StartOptions{})
			} else {
				err = v.c.Restart(context.Background(), b, admin, StopOptions{}, StartOptions{})
			}
			if !errors.Is(err, ErrWorldNotActive) || err.Error() != "Beta is not the active world. Switch to it first" {
				t.Fatalf("%s = %v, want the not-active refusal", action, err)
			}
			if n := len(v.calls("POST /containers/create")); n != 0 {
				t.Errorf("%d container creations after a refusal", n)
			}
			if v.hasContainer(b) {
				t.Error("a container exists for the world that is not active")
			}
		})
	}
}
