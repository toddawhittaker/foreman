package game

import (
	"context"
	"errors"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// archiveInPlace sets a world's archived flag by writing world.json, which
// reaches a state SetArchived refuses: an archived world that is active.
func (r *rig) archiveInPlace(id string) {
	r.t.Helper()
	root, err := r.worlds.OpenWorld(id)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil {
		r.t.Fatal(err)
	}
	meta.Archived = true
	if err := world.WriteMeta(root, meta); err != nil {
		r.t.Fatal(err)
	}
}

func isArchivedRefusal(err error) bool {
	se, ok := errors.AsType[*StartError](err)
	return ok && se.Kind == StartArchived
}

// SPEC 5.9: an archived world cannot be started, by anyone. The refusal is
// made under the world's lock, before any container exists, and is audited
// as a refusal.
func TestStartAndRestartRefuseAnArchivedWorld(t *testing.T) {
	for _, action := range []string{AuditStart, AuditRestart} {
		t.Run(action, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			a := r.newWorld("Alpha")
			r.archiveInPlace(a)
			before := len(r.audit.list())
			var err error
			if action == AuditStart {
				err = r.c.Start(context.Background(), a, admin, StartOptions{})
			} else {
				err = r.c.Restart(context.Background(), a, admin, StopOptions{}, StartOptions{})
			}
			if !isArchivedRefusal(err) || !IsRefusal(err) {
				t.Fatalf("%s = %v, want the archived refusal", action, err)
			}
			if n := len(r.calls("POST /containers/create")); n != 0 {
				t.Errorf("%d containers created for an archived world", n)
			}
			added := r.audit.list()[before:]
			if len(added) != 1 || added[0].Action != action+"-refused" || added[0].WorldID != a {
				t.Errorf("audit entries %+v, want one %s-refused", added, action)
			}
		})
	}
}

// SPEC 5.9, 7.9: a switch to an archived world is refused, in the plan the
// confirmation page shows and in the switch itself, and changes nothing.
func TestSwitchRefusesAnArchivedWorld(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	if err := r.worlds.SetArchived(context.Background(), b, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.PlanSwitch(context.Background(), a, b); !isArchivedRefusal(err) {
		t.Errorf("plan: %v, want the archived refusal", err)
	}
	for _, start := range []bool{true, false} {
		before := len(r.audit.list())
		err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}, start)
		if !isArchivedRefusal(err) {
			t.Errorf("switch (start=%v): %v, want the archived refusal", start, err)
		}
		added := r.audit.list()[before:]
		if len(added) != 1 || added[0].Action != AuditSwitchRefused {
			t.Errorf("audit entries %+v, want one switch-refused", added)
		}
	}
	if r.active() != a || r.container(a).Status != "running" {
		t.Error("a refused switch changed the active world or stopped the game")
	}

	// Unarchived, the same switch works.
	if err := r.worlds.SetArchived(context.Background(), b, false); err != nil {
		t.Fatal(err)
	}
	if err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}, true); err != nil {
		t.Errorf("switch after unarchive: %v", err)
	}
}

// SPEC 5.9: the archived refusal reads as a start failure with its own words.
func TestArchivedStartErrorText(t *testing.T) {
	err := errArchived()
	if !errors.Is(err, ErrStartFailed) {
		t.Error("the archived refusal is not a start failure")
	}
	if got := err.Error(); got != "the game did not start: the world is archived. Unarchive it first" {
		t.Errorf("text %q", got)
	}
}
