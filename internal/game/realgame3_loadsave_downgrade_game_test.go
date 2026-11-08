//go:build game

package game

// The save chosen to load next, and a mod held to an older release, each
// checked against the real game. Run by `make test-game`, never by `make
// verify`.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameChosenSaveLoads(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runChosenSave(t, g.tag, g.fixture, g.version)
		})
	}
}

// A chosen older save is the one the game loads, and clearing the choice
// loads the newest again (SPEC 7.11).
func (e *gameEnv) runChosenSave(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.newWorld(t, tag, version, "chosen", fixture, map[string]time.Duration{
		"older.zip": 3 * time.Hour, "newer.zip": 2 * time.Hour,
	})
	stopOpts := StopOptions{Countdown: 2 * time.Second}
	// startLoads starts the game, checks it loaded want, stops it, and
	// returns when that run started.
	startLoads := func(want string, prev time.Time) time.Time {
		t.Helper()
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		e.requireBackInGame(t, c, id, version, prev)
		st := c.Status(id)
		if st.Save != want {
			t.Fatalf("loaded %q, want %s", st.Save, want)
		}
		if !anyLineHas(currentRun(c, id), "Loading map /factorio/saves/"+want) {
			t.Fatalf("the game's log has no line for loading %s", want)
		}
		if err := c.Stop(ctx, id, admin, stopOpts); err != nil {
			t.Fatalf("stop: %v", err)
		}
		return st.StartedAt
	}

	if err := e.worlds.SetLoadSave(id, "older.zip"); err != nil {
		t.Fatal(err)
	}
	prev := startLoads("older.zip", time.Time{})

	// The stop saved over older.zip, which made it the newest; make newer.zip
	// the newest again so a cleared choice is told apart from a kept one.
	e.putSave(t, id, "newer.zip", e.readSave(t, id, "newer.zip"), time.Now().Add(time.Minute))
	if err := e.worlds.SetLoadSave(id, ""); err != nil {
		t.Fatal(err)
	}
	startLoads("newer.zip", prev)
}

func (e *gameEnv) readSave(t *testing.T, id, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.worldDir(id), world.SavesDir, name)) //nolint:gosec // a path inside the test's own data folder
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRealGameModHeldToOlderRelease(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runHeldToOlder(t, g.tag, g.fixture, g.version)
		})
	}
}

// Changing an installed mod to an older release takes a snapshot first, the
// game loads the older release, and Undo brings the newer one back (SPEC
// 6.2, 9.2, 9.3, 9.4).
func (e *gameEnv) runHeldToOlder(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	fetch := newFetcher()
	c.SetModFetcher(fetch)
	portal := probePortal(t, fetch, version, "1.0.0", "2.0.0")
	id := e.newWorld(t, tag, version, "older release", fixture, map[string]time.Duration{"main.zip": time.Hour})
	stopOpts := StopOptions{Countdown: 2 * time.Second}
	startLoads := func(want string, prev time.Time) time.Time {
		t.Helper()
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		e.requireBackInGame(t, c, id, version, prev)
		if !anyLineHas(currentRun(c, id), probeLoaded(want)) {
			t.Fatalf("the game did not load %s %s; mods %s", probeMod, want, e.modsSummary(t, id))
		}
		if zips := e.zipsInMods(t, id); !slices.Equal(zips, []string{probeFile(want)}) {
			t.Fatalf("the mods folder holds %v, want only %s", zips, probeFile(want))
		}
		started := c.Status(id).StartedAt
		if err := c.Stop(ctx, id, admin, stopOpts); err != nil {
			t.Fatalf("stop: %v", err)
		}
		return started
	}

	// An install takes the newest release.
	op, _ := e.planOp(t, portal, id, mods.Request{Kind: mods.KindInstall, Names: []string{probeMod}}, false, false)
	if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
		t.Fatalf("install: %v", err)
	}
	prev := startLoads("2.0.0", time.Time{})
	n := len(e.modsSnapshots(t, id))

	op, plan := e.planOp(t, portal, id, mods.Request{
		Kind: mods.KindUpdate, Names: []string{probeMod}, Versions: map[string]string{probeMod: "1.0.0"},
	}, false, false)
	i := slices.IndexFunc(plan.Changes, func(ch mods.Change) bool { return ch.Name == probeMod })
	if i < 0 || plan.Changes[i].Action != mods.ActDowngrade {
		t.Fatalf("the plan does not downgrade %s: %+v", probeMod, plan.Changes)
	}
	if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
		t.Fatalf("downgrade: %v", err)
	}
	snaps := e.modsSnapshots(t, id)
	if len(snaps) != n+1 {
		t.Fatalf("%d \"Before mod change\" snapshots after the downgrade, want %d", len(snaps), n+1)
	}
	before := snaps[len(snaps)-1]
	if !snapshotHas(before, "mods/"+probeFile("2.0.0")) || snapshotHas(before, "mods/"+probeFile("1.0.0")) {
		t.Fatalf("the downgrade's snapshot should hold the world with 2.0.0: %+v", before.Files)
	}
	prev = startLoads("1.0.0", prev)

	if _, err := c.Restore(ctx, id, admin, before.Name, stopOpts); err != nil {
		t.Fatalf("undo: %v", err)
	}
	startLoads("2.0.0", prev)
}
