//go:build game

package game

// The autosave tidy on the real game (SPEC 11.6). A world with 20
// autosaves, the newest by time being 17, has its slots lowered to 10. The
// start keeps files 17 down to 8 as slots 7 to 1 then 10 to 8, removes the
// rest, and the game loads the newest, now _autosave7.zip. After a stop,
// Foreman remembers that the run loaded it.
// Run by `make test-game`, never by `make verify`.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameAutosaveTidy(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runAutosaveTidy(t, g.tag, g.fixture, g.version)
		})
	}
}

func autosaveFile(n int) string { return "_autosave" + strconv.Itoa(n) + ".zip" }

func (e *gameEnv) runAutosaveTidy(t *testing.T, tag, fixture, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	// Written 18, 19, 20, then 1 to 17: 17 is the newest.
	order := []int{18, 19, 20, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
	ages := map[string]time.Duration{}
	for i, n := range order {
		ages[autosaveFile(n)] = time.Duration(len(order)-i) * time.Minute
	}
	id := e.newWorld(t, tag, version, "tidy", fixture, ages)
	settings, err := gamesettings.SetFields(gamesettings.NewServerSettings("tidy"), []gamesettings.Field{
		{Key: "autosave_slots", JSON: []byte("10")},
		{Key: "autosave_interval", JSON: []byte("1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.worldDir(id), world.ConfigDir, world.ServerSettingsFile), settings, 0o600); err != nil { //nolint:gosec // the test's own world folder
		t.Fatal(err)
	}
	times := map[string]time.Time{}
	for n := 1; n <= 20; n++ {
		times[autosaveFile(n)] = e.saveTime(t, id, autosaveFile(n))
	}

	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	if st := c.Status(id); st.State != StateRunning || st.Save != autosaveFile(7) {
		t.Errorf("state %s, save %q; want running with %s", st.State, st.Save, autosaveFile(7))
	}
	entries, err := os.ReadDir(filepath.Join(e.worldDir(id), world.SavesDir))
	if err != nil {
		t.Fatal(err)
	}
	var got, want []string
	for _, en := range entries {
		if strings.HasPrefix(en.Name(), "_autosave") {
			got = append(got, en.Name())
		}
	}
	for n := 1; n <= 10; n++ {
		want = append(want, autosaveFile(n))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("autosaves after the tidy: %v", got)
	}
	// The game pauses with no players, so it writes no autosave here; each
	// kept file keeps its time under its new name.
	for from, to := range map[int]int{8: 8, 11: 1, 16: 6} {
		if at := e.saveTime(t, id, autosaveFile(to)); !at.Equal(times[autosaveFile(from)]) {
			t.Errorf("%s has time %v, want file %d's %v", autosaveFile(to), at, from, times[autosaveFile(from)])
		}
	}

	if err := c.Stop(ctx, id, admin, StopOptions{Countdown: time.Second}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	w := c.world(id)
	w.mu.Lock()
	loaded := w.lastLoaded
	w.mu.Unlock()
	if loaded != autosaveFile(7) {
		t.Errorf("remembered %q as loaded, want %s", loaded, autosaveFile(7))
	}
}
