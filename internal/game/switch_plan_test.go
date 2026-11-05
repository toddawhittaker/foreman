package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// breakSave names b's save as the one to load and then breaks it, so b's
// pre-start check finds a problem that allows a start anyway (SPEC 7.4).
func (r *rig) breakSave(id string) {
	r.t.Helper()
	if err := r.worlds.SetLoadSave(id, "base.zip"); err != nil {
		r.t.Fatal(err)
	}
	r.writeFile(id, world.SavesDir+"/base.zip", []byte("now broken"))
}

// SPEC 7.9, 17.8: the plan the confirmation page shows says whether the
// game runs, which image the new world needs and whether it is missing, and
// the new world's pre-start problems, which do not refuse the plan.
func TestPlanSwitch(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	old := r.versionWorld("Old", "2.0.77", "2.0.77-no-space-age.zip")
	r.game.SetImage(image20, false)
	broken := r.newWorld("Broken")
	r.breakSave(broken)

	plan, err := r.c.PlanSwitch(context.Background(), a, b)
	if err != nil || plan.Running || plan.Download || plan.Image != image21 || !plan.Check.OK() {
		t.Errorf("stopped plan %+v, %v", plan, err)
	}
	r.start(a)
	if plan, err = r.c.PlanSwitch(context.Background(), a, b); err != nil || !plan.Running {
		t.Errorf("running plan %+v, %v", plan, err)
	}
	if plan, err = r.c.PlanSwitch(context.Background(), a, old); err != nil || !plan.Download || plan.Image != image20 {
		t.Errorf("plan for a missing image %+v, %v", plan, err)
	}
	plan, err = r.c.PlanSwitch(context.Background(), a, broken)
	if err != nil || plan.Check.OK() || !plan.Check.CanStartAnyway() {
		t.Errorf("plan for a world with problems %+v, %v", plan, err)
	}
	if n := len(r.calls("POST ")); n != 2 { // the create and start of a
		t.Errorf("planning made %d changing Docker calls: %v", n, r.calls("POST "))
	}
}

// SPEC 7.9, 17.8: the plan and the switch refuse for the same reasons, so
// the confirmation page never offers a switch the switch would refuse.
func TestPlanSwitchRefusesAsTheSwitchDoes(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig, a, b string) (from, to string)
		want  error
	}{
		{"the active world", func(_ *rig, a, _ string) (string, string) { return a, a }, ErrSwitchSame},
		{"from not active", func(_ *rig, a, b string) (string, string) { return b, a }, ErrSwitchNotActive},
		{"to missing", func(_ *rig, a, _ string) (string, string) { return a, "gamma" }, ErrSwitchNoTarget},
		{"to unreadable", func(r *rig, a, b string) (string, string) {
			r.writeFile(b, world.MetaFile, []byte("{"))
			return a, b
		}, ErrSwitchUnreadable},
		{"to from a newer Foreman", func(r *rig, a, b string) (string, string) {
			r.writeFile(b, world.MetaFile, []byte(`{"format_version": 99, "name": "Beta", "game_version": "2.1.24"}`))
			return a, b
		}, ErrSwitchNewer},
		{"to already running", func(r *rig, a, b string) (string, string) {
			r.startAsActive(b)
			return a, b
		}, ErrSwitchTargetRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			a, b := r.twoWorlds()
			r.start(a)
			from, to := tc.setup(r, a, b)
			if _, err := r.c.PlanSwitch(context.Background(), from, to); !errors.Is(err, tc.want) {
				t.Errorf("plan: got %v, want %v", err, tc.want)
			}
			if err := r.c.Switch(context.Background(), from, to, admin, StopOptions{}, StartOptions{}, true); !errors.Is(err, tc.want) {
				t.Errorf("switch: got %v, want %v", err, tc.want)
			}
			if r.active() != a || r.container(a).Status != "running" {
				t.Error("a refused switch changed the active world or stopped the game")
			}
		})
	}
}

// SPEC 7.9: while an operation runs on either world, the plan refuses, so
// the confirmation page says so before the admin confirms.
func TestPlanSwitchRefusesWhileBusy(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	_, o := r.c.beginOp(context.Background(), r.c.world(b), AuditStart, []string{StepCheck})
	if _, err := r.c.PlanSwitch(context.Background(), a, b); !errors.Is(err, ErrSwitchBusy) {
		t.Errorf("got %v, want %v", err, ErrSwitchBusy)
	}
	_ = o.end(nil)
	if _, err := r.c.PlanSwitch(context.Background(), a, b); err != nil {
		t.Errorf("after the operation: %v", err)
	}
}

// SPEC 3.7, 7.9: a switch to a world whose image is missing downloads it
// as its first step, before the old game is stopped and before the switch
// record is written, and the audit entry says so.
func TestSwitchDownloadsAMissingImageFirst(t *testing.T) {
	r := newRig(t, rigOptions{})
	a := r.newWorld("Alpha")
	old := r.versionWorld("Old", "2.0.77", "2.0.77-no-space-age.zip")
	r.game.SetImage(image20, false)
	r.start(a)
	tr := &recordWatch{SwitchStore: r.c.switches, pulls: func() int { return len(r.game.Daemon().Pulls()) }}
	r.c.switches = tr
	if err := r.c.Switch(context.Background(), a, old, admin, StopOptions{}, StartOptions{}, true); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.game.Daemon().Pulls(), image20) {
		t.Fatalf("pulls %v, want %s", r.game.Daemon().Pulls(), image20)
	}
	if tr.pullsAtBegin != 1 {
		t.Errorf("%d downloads before the switch record, want 1", tr.pullsAtBegin)
	}
	calls := r.game.State().Calls
	pull := slices.IndexFunc(calls, func(c string) bool { return strings.HasPrefix(c, "POST /images/create") })
	stop := slices.IndexFunc(calls, func(c string) bool {
		return strings.HasPrefix(c, "POST /containers/") && (strings.HasSuffix(c, "/stop") || strings.HasSuffix(c, "/kill"))
	})
	if pull < 0 || stop < 0 || pull > stop {
		t.Errorf("download at call %d, stop at call %d; want the download first", pull, stop)
	}
	op := r.c.Status(old).Operation
	if op.Steps[0].Name != StepDownload || op.Steps[0].State != StepDone {
		t.Errorf("first step %+v, want the download, done", op.Steps[0])
	}
	if r.container(old).Status != "running" || r.container(old).Image != image20 {
		t.Errorf("the new world's game is %+v", r.container(old))
	}
	if e := r.audit.list()[len(r.audit.list())-1]; e.Action != AuditSwitch || !strings.Contains(e.Detail, "download "+image20) {
		t.Errorf("audit entry %+v, want a switch that names the download", e)
	}
}

// recordWatch counts the downloads made before the switch record is begun.
type recordWatch struct {
	SwitchStore
	pulls        func() int
	pullsAtBegin int
}

func (w *recordWatch) BeginSwitch(ctx context.Context, from, to, by string, now time.Time) error {
	w.pullsAtBegin = w.pulls()
	return w.SwitchStore.BeginSwitch(ctx, from, to, by, now)
}

// SPEC 7.9 (fail safe): a failed download leaves the old world running and
// untouched: not saved, not stopped, not snapshotted, no record, still
// active. The step list shows the download failed and nothing else ran.
func TestSwitchFailedDownloadLeavesTheOldWorldRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	a := r.newWorld("Alpha")
	old := r.versionWorld("Old", "2.0.77", "2.0.77-no-space-age.zip")
	r.game.SetImage(image20, false)
	r.game.Daemon().FailPull(image20, "manifest unknown")
	r.start(a)
	err := r.c.Switch(context.Background(), a, old, admin, StopOptions{}, StartOptions{}, true)
	if err == nil || !strings.Contains(err.Error(), "download "+image20) {
		t.Fatalf("got %v, want the failed download", err)
	}
	if r.active() != a || r.container(a).Status != "running" {
		t.Error("the old world is no longer active and running")
	}
	if slices.Contains(r.commands(a), "/server-save") {
		t.Error("the old game was saved, so the stop had begun")
	}
	if len(r.switchSnapshots(a))+len(r.switchSnapshots(old)) != 0 {
		t.Error("a switch whose download failed took snapshots")
	}
	r.noRecord()
	op := r.c.Status(a).Operation
	if op == nil || op.Switch == nil || op.Switch.Presented || stepState(op, StepDownload) != StepFailed {
		t.Fatalf("operation %+v", op)
	}
	for _, s := range op.Steps[1:] {
		if s.State != StepSkipped {
			t.Errorf("step %q is %s, want skipped", s.Name, s.State)
		}
	}
}

// SPEC 7.9: a stopped game's switch starts the new world when asked to,
// which is what the confirmation page's ticked box asks.
func TestSwitchStoppedGameStartsWhenAsked(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	if err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}, true); err != nil {
		t.Fatal(err)
	}
	if r.active() != b || r.c.Status(b).State != StateRunning {
		t.Errorf("active %s, %s is %s; want it running", r.active(), b, r.c.Status(b).State)
	}
}

// SPEC 7.4, 7.9: the new world's start problems refuse only a switch that
// starts it. "Switch, leave stopped" goes ahead past them, stops the old
// game, starts nothing and takes no "Before start anyway" snapshot; the
// problems wait for the next Start.
func TestSwitchLeaveStoppedGoesAheadPastStartProblems(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.breakSave(b)
	r.start(a)
	var pe *PrecheckError
	if err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}, true); !errors.As(err, &pe) {
		t.Fatalf("a switch that starts: got %v, want a pre-start refusal", err)
	}
	if err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{Anyway: true}, false); err != nil {
		t.Fatal(err)
	}
	if r.active() != b || r.hasContainer(b) || r.container(a).Status == "running" {
		t.Errorf("active %s, new game made %v, old game %s", r.active(), r.hasContainer(b), r.container(a).Status)
	}
	snaps, err := r.worlds.Snapshots(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snaps {
		if s.Reason == ReasonStartAnyway {
			t.Error("a switch that starts nothing took the start anyway snapshot")
		}
	}
	if e := r.audit.list()[len(r.audit.list())-1]; e.Action != AuditSwitch || !strings.HasSuffix(e.Detail, ", leave stopped") {
		t.Errorf("audit entry %+v", e)
	}
	var refused *PrecheckError
	if err := r.c.Start(context.Background(), b, admin, StartOptions{}); !errors.As(err, &refused) {
		t.Errorf("the next start: got %v, want the problems", err)
	}
}
