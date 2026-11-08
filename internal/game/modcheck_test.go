package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 7.4: a start, restart or switch refuses a world whose enabled mods
// include a version the mod portal no longer offers, since players could
// not join; "start anyway" stays open. A portal that cannot be asked, or
// answers too slowly, never blocks the start (fail safe for availability).
// Folder mods and mods not on the portal stay notes, not refusals.

// withPortal gives the rig's controller the mod check on a fake portal.
func (r *rig) withPortal() *modportaltest.Portal {
	r.t.Helper()
	p := modportaltest.New(r.t)
	r.c.SetModCheck(&mods.Planner{Worlds: r.worlds, Portal: modportal.NewForTest(p.URL)})
	return p
}

func goneOf(t *testing.T, err error) world.Problem {
	t.Helper()
	var pe *PrecheckError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want a pre-start refusal", err)
	}
	for _, p := range pe.Check.Problems {
		if p.Kind == world.ProblemModGone {
			return p
		}
	}
	t.Fatalf("no mod-gone problem in %+v", pe.Check.Problems)
	return world.Problem{}
}

func TestStartRefusesAModVersionThePortalNoLongerOffers(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	id := r.newWorld("Gone")
	r.putModZip(id, "fp-gone", "2.1")
	p.AddZipFor("2.1", "fp-gone", "1.1.0")

	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	gone := goneOf(t, err)
	if !gone.StartAnyway {
		t.Error("the refusal does not offer start anyway")
	}
	want := world.GoneMod{Name: "fp-gone", Version: "1.0.0", Update: "1.1.0"}
	if len(gone.Gone) != 1 || gone.Gone[0] != want || len(gone.Mods) != 1 || gone.Mods[0] != "fp-gone" {
		t.Errorf("problem %+v, want %+v", gone, want)
	}
	if r.hasContainer(id) {
		t.Error("a refused start created the game container")
	}

	// Start anyway confirms it and starts.
	var pe *PrecheckError
	errors.As(err, &pe)
	seen := AnywayFingerprint(pe.Check, false)
	if err := r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true, Seen: seen}); err != nil {
		t.Fatalf("start anyway: %v", err)
	}
	if st := r.c.Status(id); st.State != StateRunning || st.Operation.PortalUnchecked {
		t.Errorf("state %s, operation %+v", st.State, st.Operation)
	}
}

func TestStartGoesAheadWhenThePortalIsDown(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	id := r.newWorld("Offline")
	r.putModZip(id, "fp-gone", "2.1")
	p.AddZipFor("2.1", "fp-gone", "1.1.0")
	p.FailAPI(503)

	r.start(id)
	st := r.c.Status(id)
	if st.State != StateRunning {
		t.Fatalf("state %s", st.State)
	}
	if !st.Operation.PortalUnchecked {
		t.Error("the start does not say the portal could not be checked")
	}
}

func TestStartGoesAheadWhenThePortalIsTooSlow(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	r.c.modCheckLimit = 100 * time.Millisecond
	p.FullDelay = 10 * time.Second
	id := r.newWorld("Slow")
	r.putModZip(id, "fp-gone", "2.1")
	p.AddZipFor("2.1", "fp-gone", "1.1.0")

	began := time.Now()
	r.start(id)
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("the start waited %s on the portal", took)
	}
	if !r.c.Status(id).Operation.PortalUnchecked {
		t.Error("the start does not say the portal could not be checked")
	}
}

func TestStartIsNotRefusedForFolderModsOrModsNotOnThePortal(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.withPortal()
	id := r.newWorld("Local")
	r.putModZip(id, "fp-nowhere", "2.1")
	dir := filepath.Join(r.worldDir(id), world.ModsDir, "fp-folder")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	info := `{"name":"fp-folder","version":"1.0.0","factorio_version":"2.1"}`
	if err := os.WriteFile(filepath.Join(dir, "info.json"), []byte(info), 0o600); err != nil {
		t.Fatal(err)
	}
	r.start(id)
	if st := r.c.Status(id); st.State != StateRunning || st.Operation.PortalUnchecked {
		t.Fatalf("state %s, operation %+v", st.State, st.Operation)
	}
}

// SPEC 7.8: the check runs before the stop, so a restart the portal check
// refuses leaves the game running.
func TestRestartRefusedForAGoneModBeforeStopping(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	id := r.newWorld("Keep")
	r.putModZip(id, "fp-gone", "2.1")
	p.FailAPI(503)
	r.start(id)

	p.FailAPI(0)
	p.AddZipFor("2.1", "fp-gone", "1.1.0")
	goneOf(t, r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}))
	if r.container(id).Status != "running" {
		t.Error("the game was stopped although the start would be refused")
	}
}

// SPEC 7.9: the new world's portal problem refuses the switch before the
// running game stops, and shows in the plan the confirmation page reads.
func TestSwitchRefusedForAGoneModBeforeStopping(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	a, b := r.twoWorlds()
	r.start(a)
	r.putModZip(b, "fp-gone", "2.1")
	p.AddZipFor("2.1", "fp-gone", "1.1.0")

	plan, err := r.c.PlanSwitch(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Check.OK() || plan.Check.Problems[0].Kind != world.ProblemModGone {
		t.Errorf("plan check %+v", plan.Check)
	}
	goneOf(t, r.switchWorlds(a, b))
	if r.container(a).Status != "running" || r.active() != a {
		t.Error("a refused switch stopped the game or changed the active world")
	}
	r.noRecord()
}

// A switch with the portal down goes ahead, and its result says the portal
// could not be checked.
func TestSwitchGoesAheadWhenThePortalIsDown(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	a, b := r.twoWorlds()
	r.start(a)
	r.putModZip(b, "fp-gone", "2.1")
	p.AddZipFor("2.1", "fp-gone", "1.1.0")
	p.FailAPI(503)
	if err := r.switchWorlds(a, b); err != nil {
		t.Fatalf("switch: %v", err)
	}
	st := r.c.Status(b)
	if st.State != StateRunning || !st.Operation.PortalUnchecked {
		t.Errorf("state %s, operation %+v", st.State, st.Operation)
	}
}

// uncachedPortal asks the fake portal through a new client each time, so a
// count of calls shows how often an action asks, not what a cache kept.
type uncachedPortal struct{ url string }

func (u uncachedPortal) Mod(ctx context.Context, name string) (modportal.Mod, error) {
	return modportal.NewForTest(u.url).Mod(ctx, name)
}

func (u uncachedPortal) Mods(ctx context.Context, names []string) map[string]modportal.ModResult {
	return modportal.NewForTest(u.url).Mods(ctx, names)
}

// SPEC 7.4: a restart checks twice, before the stop and before the start,
// but asks the portal once.
func TestRestartAsksThePortalOncePerAction(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := modportaltest.New(t)
	r.c.SetModCheck(&mods.Planner{Worlds: r.worlds, Portal: uncachedPortal{p.URL}})
	id := r.newWorld("Once")
	r.putModZip(id, "fp-here", "2.1")
	p.AddZipFor("2.1", "fp-here", "1.0.0")

	r.start(id)
	before := p.FullCalls("fp-here")
	if err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if n := p.FullCalls("fp-here") - before; n != 1 {
		t.Errorf("the restart asked the portal %d times for fp-here, want once", n)
	}
	if r.c.Status(id).Operation.PortalUnchecked {
		t.Error("an answered check says the portal could not be checked")
	}
}

// SPEC 7.9: a switch that leaves the new world stopped does not ask the
// portal, since a gone mod only matters to a start.
func TestSwitchWithoutStartDoesNotAskThePortal(t *testing.T) {
	r := newRig(t, rigOptions{})
	p := r.withPortal()
	a, b := r.twoWorlds()
	r.putModZip(b, "fp-gone", "2.1")
	p.AddZipFor("2.1", "fp-gone", "1.1.0")

	if err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}, false); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if n := p.FullCalls("fp-gone"); n != 0 {
		t.Errorf("the switch asked the portal %d times, want none", n)
	}
	if r.active() != b {
		t.Error("the switch did not make the new world active")
	}
}
