package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// twoWorlds makes Alpha, which is active, and Beta.
func (r *rig) twoWorlds() (a, b string) {
	r.t.Helper()
	a, b = r.newWorld("Alpha"), r.newWorld("Beta")
	if got := r.active(); got != a {
		r.t.Fatalf("active world %q, want %q", got, a)
	}
	return a, b
}

func (r *rig) active() string {
	r.t.Helper()
	id, _, err := r.store.ActiveWorld(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

// keepRunState asks a switch to leave the game as it finds it: the new
// world starts only when the old one runs.
func keepRunState(c *Controller, from string) bool { return IsRunning(c.Status(from).State) }

func (r *rig) switchWorlds(from, to string) error {
	return r.c.Switch(context.Background(), from, to, admin, StopOptions{}, StartOptions{}, keepRunState(r.c, from))
}

// noRecord fails when a switch record is left behind (SPEC 7.10).
func (r *rig) noRecord() {
	r.t.Helper()
	list, err := r.store.ListSwitches(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	if len(list) != 0 {
		r.t.Errorf("switch records left: %+v", list)
	}
}

func (r *rig) switchSnapshots(id string) []world.Snapshot {
	r.t.Helper()
	all, err := r.worlds.Snapshots(id)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []world.Snapshot
	for _, s := range all {
		if s.Reason == world.ReasonSwitch {
			out = append(out, s)
		}
	}
	return out
}

// hasContainer reports whether the fake holds a container for the world.
func (r *rig) hasContainer(id string) bool {
	for _, cs := range r.game.State().Containers {
		if cs.Name == ContainerName(id) {
			return true
		}
	}
	return false
}

func stepState(op *Operation, name string) StepState {
	for _, s := range op.Steps {
		if s.Name == name {
			return s.State
		}
	}
	return ""
}

// SPEC 7.9: a running world's switch stops it, snapshots both worlds,
// makes the new one active and leaves it running (the game is left as it
// was found). Both worlds show the same operation, and it is audited with
// both worlds named (SPEC 12.9).
func TestSwitchRunningWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	if err := r.switchWorlds(a, b); err != nil {
		t.Fatal(err)
	}
	if got := r.active(); got != b {
		t.Errorf("active world %q, want %q", got, b)
	}
	if st := r.c.Status(b); st.State != StateRunning {
		t.Errorf("%s is %s, want running", b, st.State)
	}
	if cs := r.container(a); cs.Status != "exited" {
		t.Errorf("%s's container is %s, want exited", a, cs.Status)
	}
	if !slices.Contains(r.commands(a), "/server-save") {
		t.Error("the old world was not saved before it stopped")
	}
	for _, id := range []string{a, b} {
		snaps := r.switchSnapshots(id)
		if len(snaps) != 1 || snaps[0].Label != world.LabelBeforeSwitch || snaps[0].TakenBy != admin.Username {
			t.Errorf("%s has switch snapshots %+v, want one labelled %q", id, snaps, world.LabelBeforeSwitch)
		}
	}
	op := r.c.Status(b).Operation
	if op == nil || op.Action != AuditSwitch || !op.Done || op.Error != "" {
		t.Fatalf("operation %+v", op)
	}
	if op.Switch == nil || *op.Switch != (SwitchInfo{From: a, To: b, Presented: true}) {
		t.Errorf("switch info %+v", op.Switch)
	}
	for _, s := range op.Steps {
		// Nobody is online, so nobody is warned.
		if s.State != StepDone && (s.Name != StepWarn || s.State != StepSkipped) {
			t.Errorf("step %q is %s, want done", s.Name, s.State)
		}
	}
	if other := r.c.Status(a).Operation; other == nil || !slices.Equal(other.Steps, op.Steps) || other.Action != AuditSwitch {
		t.Errorf("%s shows %+v, want the same switch", a, other)
	}
	r.noRecord()
	entries := r.audit.list()
	if len(entries) != 2 || entries[1].Action != AuditSwitch || entries[1].WorldID != a ||
		entries[1].Detail != "from "+a+" to "+b {
		t.Errorf("audit %+v", entries)
	}
}

// SPEC 7.9: a stopped world's switch makes the new one active and leaves
// it stopped; no game is started.
func TestSwitchStoppedWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	if err := r.switchWorlds(a, b); err != nil {
		t.Fatal(err)
	}
	if got := r.active(); got != b {
		t.Errorf("active world %q, want %q", got, b)
	}
	if r.hasContainer(b) || len(r.calls("POST /containers/create")) != 0 {
		t.Error("a game container was created for a stopped switch")
	}
	op := r.c.Status(b).Operation
	for _, name := range stopSteps {
		if got := stepState(op, name); got != StepSkipped {
			t.Errorf("step %q is %s, want skipped", name, got)
		}
	}
	// Nothing starts, so the switch neither checks nor starts the new world
	// and lists neither (SPEC 7.9).
	for _, name := range append([]string{StepCheck, StepDownload}, launchSteps...) {
		if got := stepState(op, name); got != "" {
			t.Errorf("step %q is listed as %s", name, got)
		}
	}
	for _, name := range []string{StepSnapshotBoth, StepPresent} {
		if got := stepState(op, name); got != StepDone {
			t.Errorf("step %q is %s, want done", name, got)
		}
	}
	if len(r.switchSnapshots(a)) != 1 || len(r.switchSnapshots(b)) != 1 {
		t.Error("the switch did not snapshot both worlds")
	}
	r.noRecord()

	// Switching back is the same operation the other way.
	if err := r.switchWorlds(b, a); err != nil {
		t.Fatal(err)
	}
	if got := r.active(); got != a {
		t.Errorf("active world %q after switching back, want %q", got, a)
	}
	r.noRecord()
}

// A refused switch changes nothing: no stop, no snapshot, no record.
func TestSwitchRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig, a, b string) (from, to string)
		want  error
	}{
		{"same world", func(_ *rig, a, _ string) (string, string) { return a, a }, ErrSwitchSame},
		{"from not active", func(_ *rig, a, b string) (string, string) { return b, a }, ErrSwitchNotActive},
		{"to missing", func(_ *rig, a, _ string) (string, string) { return a, "gamma" }, ErrSwitchNoTarget},
		{"to already running", func(r *rig, a, b string) (string, string) {
			r.startAsActive(b)
			return a, b
		}, ErrSwitchTargetRunning},
		{"from busy", func(r *rig, a, b string) (string, string) {
			unlock, err := r.worlds.Lock(a)
			if err != nil {
				r.t.Fatal(err)
			}
			r.t.Cleanup(unlock)
			return a, b
		}, world.ErrBusy},
		{"to busy", func(r *rig, a, b string) (string, string) {
			unlock, err := r.worlds.Lock(b)
			if err != nil {
				r.t.Fatal(err)
			}
			r.t.Cleanup(unlock)
			return a, b
		}, world.ErrBusy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			a, b := r.twoWorlds()
			r.start(a)
			from, to := tc.setup(r, a, b)
			if err := r.switchWorlds(from, to); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if r.active() != a {
				t.Error("the active world changed")
			}
			if r.container(a).Status != "running" {
				t.Error("the running game was stopped")
			}
			if len(r.switchSnapshots(a))+len(r.switchSnapshots(b)) != 0 {
				t.Error("a refused switch took snapshots")
			}
			r.noRecord()
		})
	}
}

// SPEC 7.8, 7.9: a target the pre-start check refuses is refused before
// the running game is stopped.
func TestSwitchRefusedByTargetCheck(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	if err := r.worlds.SetLoadSave(b, "base.zip"); err != nil {
		t.Fatal(err)
	}
	r.writeFile(b, world.SavesDir+"/base.zip", []byte("now broken"))
	var pe *PrecheckError
	if err := r.switchWorlds(a, b); !errors.As(err, &pe) {
		t.Fatalf("got %v, want a pre-start refusal", err)
	}
	if r.container(a).Status != "running" || r.active() != a {
		t.Error("a refused switch stopped the game or changed the active world")
	}
	r.noRecord()
}

// SPEC 7.9, 7.10: a failure at any step before the new world is made
// active leaves the old world active and no game running a half-switched
// world; the result offers a start of the old world, not a switch back.
func TestSwitchFailureBeforePresentKeepsOldWorld(t *testing.T) {
	cases := []struct {
		name   string
		failAt string // the step shown failed
		setup  func(r *rig, a, b string)
	}{
		{"stop", StepWarn, func(r *rig, a, _ string) { r.game.SetRCONRefuse(ContainerName(a), true) }},
		{"record stopped", StepSnapshotBoth, func(r *rig, _, _ string) { r.store.setFail(store.StepStopped, false) }},
		{"snapshot", StepSnapshotBoth, func(r *rig, _, b string) {
			// A file where the target's snapshot folder goes.
			if err := os.WriteFile(filepath.Join(datadir.Snapshots(r.data), b), nil, 0o600); err != nil {
				r.t.Fatal(err)
			}
		}},
		{"record snapshotted", StepSnapshotBoth, func(r *rig, _, _ string) { r.store.setFail(store.StepSnapshotted, false) }},
		{"record checked", StepPresent, func(r *rig, _, _ string) { r.store.setFail(store.StepChecked, false) }},
		{"present", StepPresent, func(r *rig, _, _ string) { r.store.setFail("", true) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			a, b := r.twoWorlds()
			r.start(a)
			tc.setup(r, a, b)
			if err := r.switchWorlds(a, b); err == nil {
				t.Fatal("the switch worked")
			}
			if r.active() != a {
				t.Errorf("active world %q, want %q", r.active(), a)
			}
			if r.hasContainer(b) {
				t.Error("a game container was created for the new world")
			}
			op := r.c.Status(a).Operation
			if op.Error == "" || op.Switch == nil || op.Switch.Presented {
				t.Errorf("operation %+v, switch %+v: want a failure with the old world still active", op, op.Switch)
			}
			if got := stepState(op, tc.failAt); got != StepFailed {
				t.Errorf("step %q is %s, want failed", tc.failAt, got)
			}
			for _, s := range op.Steps {
				if s.State == StepWaiting || s.State == StepProgress {
					t.Errorf("step %q still reads %s", s.Name, s.State)
				}
			}
			r.noRecord()
			r.c.pokeMonitor()
			// The monitor takes the old world back once the switch has ended.
			if tc.name == "stop" {
				r.waitState(a, StateRunning)
			} else {
				r.waitState(a, StateStopped)
			}
		})
	}
}

// SPEC 7.9: a start that fails after the new world became active reports
// the step and offers a switch back.
func TestSwitchFailedStartOffersSwitchBack(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	r.game.SetRCONRefuse(ContainerName(b), true)
	err := r.switchWorlds(a, b)
	if !errors.Is(err, ErrStartFailed) {
		t.Fatalf("got %v, want ErrStartFailed", err)
	}
	if r.active() != b {
		t.Errorf("active world %q, want %q", r.active(), b)
	}
	op := r.c.Status(b).Operation
	if op.Switch == nil || !op.Switch.Presented || stepState(op, StepRCON) != StepFailed {
		t.Errorf("operation %+v, switch %+v", op, op.Switch)
	}
	if r.container(a).Status != "exited" {
		t.Error("the old world's game is running beside the new one")
	}
	r.noRecord()
}

// A switch is busy on both worlds: neither can be acted on, the monitor
// leaves both alone, and a forced stop on either interrupts it.
func TestSwitchBusyOnBothAndForcedStop(t *testing.T) {
	for _, target := range []string{"from", "to"} {
		t.Run("forced stop on "+target, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			a, b := r.twoWorlds()
			r.start(a)
			if err := r.game.Join(ContainerName(a), "bob"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				done <- r.c.Switch(context.Background(), a, b, admin, StopOptions{Countdown: MaxStopCountdown}, StartOptions{}, true)
			}()
			eventually(t, func() bool {
				op := r.c.Status(a).Operation
				return op != nil && op.Action == AuditSwitch && stepState(op, StepWarn) == StepProgress
			}, "the countdown to begin")
			for _, id := range []string{a, b} {
				if err := r.c.Start(context.Background(), id, admin, StartOptions{}); !errors.Is(err, world.ErrBusy) {
					t.Errorf("start of %s during the switch: %v, want ErrBusy", id, err)
				}
				if !r.c.busy(r.c.world(id)) {
					t.Errorf("the monitor would look after %s during the switch", id)
				}
				if op := r.c.Status(id).Operation; op == nil || op.Action != AuditSwitch {
					t.Errorf("%s shows %+v, want the switch", id, op)
				}
			}
			if st := r.c.Status(a); st.State != StateStopping {
				t.Errorf("%s is %s during the countdown", a, st.State)
			}
			id := a
			if target == "to" {
				id = b
			}
			if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, ErrCancelled) {
				t.Errorf("switch returned %v, want ErrCancelled", err)
			}
			if r.active() != a {
				t.Error("an interrupted switch changed the active world")
			}
			r.noRecord()
			// Whichever world was named, the forced stop acts on the game
			// the switch was stopping (SPEC 7.7), and says so.
			r.waitState(a, StateStopped)
			for _, id := range []string{a, b} {
				if op := r.c.Status(id).Operation; op == nil || !strings.Contains(op.Error, "Alpha") {
					t.Errorf("%s's operation result %+v does not name Alpha", id, op)
				}
			}
			if r.hasContainer(b) {
				t.Error("the new world was started")
			}
		})
	}
}

// SPEC 7.10, 3.8: startup reports each unfinished switch from its last
// step, clears the record, and never starts or stops a game.
func TestRecoverSwitches(t *testing.T) {
	cases := []struct {
		step      string
		presented bool
		text      string
	}{
		{store.StepBegun, false, "Switch from Alpha to Beta stopped at stopping Alpha. Alpha is still active and was not started again."},
		{store.StepStopped, false, "Switch from Alpha to Beta stopped at the snapshots. Alpha is still active and was not started again."},
		{store.StepSnapshotted, false, "Switch from Alpha to Beta stopped at the check of Beta. Alpha is still active and was not started again."},
		{store.StepChecked, false, "Switch from Alpha to Beta stopped at making Beta active. Alpha is still active and was not started again."},
		{store.StepPresented, true, "Beta is active; the switch did not confirm it running."},
		{store.StepStarted, true, "Beta is active; the switch did not confirm it running."},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			a, b := r.twoWorlds()
			r.start(a) // a game left running must stay running
			ctx := context.Background()
			if err := r.store.BeginSwitch(ctx, a, b, admin.Username, time.Now()); err != nil {
				t.Fatal(err)
			}
			if tc.presented {
				if err := r.store.ReplaceActiveWorldForSwitch(ctx, a, b, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if tc.step != store.StepBegun && tc.step != store.StepPresented {
				if err := r.store.Store.SetSwitchStep(ctx, a, tc.step); err != nil {
					t.Fatal(err)
				}
			}
			before := len(r.game.State().Calls)

			c := r.newController(rigOptions{})
			if err := c.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			r.noRecord()
			for _, call := range r.game.State().Calls[before:] {
				if strings.HasPrefix(call, "POST") || strings.HasPrefix(call, "DELETE") {
					t.Errorf("recovery called Docker: %s", call)
				}
			}
			if r.container(a).Status != "running" {
				t.Error("recovery stopped a game")
			}
			want, other := a, b
			if tc.presented {
				want, other = b, a
			}
			if r.active() != want {
				t.Errorf("active world %q, want %q", r.active(), want)
			}
			n := c.Status(want).Notice
			if n == nil {
				t.Fatalf("no notice on %s", want)
			}
			if n.Text != tc.text || n.Presented != tc.presented || n.From != a || n.To != b {
				t.Errorf("notice %+v", n)
			}
			if c.Status(other).Notice != nil {
				t.Errorf("notice on %s too", other)
			}
			// The next action on the world clears it, whatever its result:
			// this controller's monitor never ran, so it cannot reach RCON.
			_ = c.Stop(ctx, want, admin, StopOptions{})
			if c.Status(want).Notice != nil {
				t.Error("the notice outlived the next action")
			}
		})
	}
}

// Without a switch store a switch is refused and recovery has nothing to do.
func TestSwitchUnavailable(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.c.switches = nil
	if err := r.switchWorlds(a, b); !errors.Is(err, ErrSwitchUnavailable) {
		t.Fatalf("got %v", err)
	}
	if err := r.c.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// SPEC 7.10: a record left by an earlier switch from the same world, which
// recovery did not clear, does not block the next switch: the world's lock
// shows no switch from it is running.
func TestSwitchReplacesAStaleRecord(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	if err := r.store.BeginSwitch(context.Background(), a, b, "someone", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.switchWorlds(a, b); err != nil {
		t.Fatalf("switch = %v", err)
	}
	if r.active() != b {
		t.Errorf("active = %q", r.active())
	}
	r.noRecord()
}
