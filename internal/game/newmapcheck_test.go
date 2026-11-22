package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// moonMapGen names the moon planet's control, which only the moon mod has.
const moonMapGen = `{"autoplace_controls": {"iron-ore": {"frequency": 1, "size": 1, "richness": 1},
  "moon-rock": {"frequency": 1, "size": 1, "richness": 150}}}`

// unfinishedRig is a mod settings rig whose world is Unfinished, with
// mapGen as its saved map settings.
func unfinishedRig(t *testing.T, mapGen string) *modSettingsRig {
	t.Helper()
	r := newModSettingsRig(t)
	r.id = r.generate(world.NewWorld{Name: "Fresh", GameVersion: "2.1.24", MapGenSettings: []byte(mapGen)})
	return r
}

// staleCheck runs the check in the background, so a test can answer the
// planet run it starts.
func (r *modSettingsRig) staleCheck() <-chan world.PreCheck {
	ch := make(chan world.PreCheck, 1)
	go func() {
		check, err := r.c.staleControlsCheck(context.Background(), r.id, world.PreCheck{})
		if err != nil {
			r.t.Error(err)
		}
		ch <- check
	}()
	return ch
}

func (r *modSettingsRig) staleResult(ch <-chan world.PreCheck) world.PreCheck {
	r.t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(10 * time.Second):
		r.t.Fatal("the check did not return")
		return world.PreCheck{}
	}
}

// SPEC 7.4: an Unfinished world whose map settings name a control the
// world's planets lack is refused, with no start anyway.
func TestStaleControlRefusesTheStart(t *testing.T) {
	r := unfinishedRig(t, moonMapGen)
	_, err := r.c.precheck(context.Background(), r.id, StartOptions{})
	pe, ok := errors.AsType[*PrecheckError](err)
	if !ok {
		t.Fatalf("error %v, want a refusal", err)
	}
	var found *world.Problem
	for i, p := range pe.Check.Problems {
		if p.Kind == world.ProblemStaleControls {
			found = &pe.Check.Problems[i]
		}
	}
	if found == nil {
		t.Fatalf("problems %+v, want stale controls", pe.Check.Problems)
	}
	if !slices.Equal(found.Names, []string{"moon-rock"}) {
		t.Errorf("names %v, want moon-rock only", found.Names)
	}
	if found.StartAnyway || pe.Check.CanStartAnyway() {
		t.Error("the refusal offers start anyway")
	}
	if _, err := r.c.precheck(context.Background(), r.id, StartOptions{Anyway: true}); err == nil {
		t.Error("start anyway got past the refusal")
	}
}

// SPEC 7.4: the read-only check a switch runs refuses too.
func TestStaleControlRefusesTheSwitchCheck(t *testing.T) {
	r := unfinishedRig(t, moonMapGen)
	if _, err := r.c.precheckReadOnly(context.Background(), r.id, "", StartOptions{}); err == nil {
		t.Error("the switch check let a stale control through")
	}
}

// SPEC 5.6, 7.4: every control in the game's own default map settings is in
// the built-in planets, so a fresh Generate is never refused.
func TestStaleControlDefaultsPass(t *testing.T) {
	for _, line := range []string{"2.0", "2.1"} {
		r := unfinishedRig(t, string(mapgen.DefaultMapGen(line)))
		if c := r.staleResult(r.staleCheck()); len(c.Problems) != 0 {
			t.Errorf("%s: problems %+v", line, c.Problems)
		}
	}
}

// SPEC 7.4: a world with a save, or without the file, is not checked.
func TestStaleControlOnlyUnfinishedWithTheFile(t *testing.T) {
	t.Run("has a save", func(t *testing.T) {
		r := unfinishedRig(t, moonMapGen)
		r.putSave(r.id, "2.1.24-no-space-age.zip", "base.zip", time.Now())
		if c := r.staleResult(r.staleCheck()); len(c.Problems) != 0 {
			t.Errorf("problems %+v", c.Problems)
		}
	})
	t.Run("no file", func(t *testing.T) {
		r := unfinishedRig(t, "")
		if err := os.Remove(filepath.Join(r.worldDir(r.id), world.ConfigDir, world.MapGenSettingsFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if c := r.staleResult(r.staleCheck()); len(c.Problems) != 0 {
			t.Errorf("problems %+v", c.Problems)
		}
	})
}

// SPEC 5.6: a control the world's planet mod still has passes.
func TestStaleControlModStillThere(t *testing.T) {
	r := unfinishedRig(t, moonMapGen)
	r.writeFile(r.id, filepath.Join(world.ModsDir, "moon_1.0.0.zip"), []byte("zip"))
	ch := r.staleCheck()
	c := r.waitRunning()
	if err := os.WriteFile(filepath.Join(r.workDir(c), gameenv.PlanetsFile), []byte(planetsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	r.finish(c, defsJSON, defaultsDat(t))
	if got := r.staleResult(ch); len(got.Problems) != 0 {
		t.Errorf("problems %+v", got.Problems)
	}
}

// SPEC 7.4: when the planet run fails, the check does not refuse; the
// create then fails without writing a save, and its error says why.
func TestStaleControlFailedRunDoesNotRefuse(t *testing.T) {
	r := unfinishedRig(t, moonMapGen)
	r.writeFile(r.id, filepath.Join(world.ModsDir, "moon_1.0.0.zip"), []byte("zip"))
	ch := r.staleCheck()
	r.game.Daemon().Exit(r.waitRunning().ID, 1, false)
	if got := r.staleResult(ch); len(got.Problems) != 0 {
		t.Errorf("problems %+v", got.Problems)
	}
}
