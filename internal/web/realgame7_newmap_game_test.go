//go:build game

package web

// Step 3 of Generate with modded planets against the real 2.1.24 game
// (SPEC 5.6, 7.4, 18.12's gate): map settings saved through the page for a
// modded planet's control and for a control two planets share reach the
// map the first start makes; and a planet mod removed after the save makes
// the pre-start check refuse the start until step 3 is saved again. The
// five planet mods are read from the folder FOREMAN_TEST_PLANET_MODS names,
// with its mod-list.json; the test skips when it is unset, and nothing is
// downloaded. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mapexchange"
	"github.com/toddawhittaker/foreman/internal/world"
)

const newMapGameVersion = "2.1.24"

// newMapModList is the set's mod list, less the feasibility test's export
// mod, with the named mods turned off.
func newMapModList(t *testing.T, dir string, off ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, world.ModListFile)) //nolint:gosec // the tester names the folder
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	list.Mods = slices.DeleteFunc(list.Mods, func(m map[string]any) bool { return m["name"] == "autoplace-export" })
	for _, m := range list.Mods {
		if name, _ := m["name"].(string); slices.Contains(off, name) {
			m["enabled"] = false
		}
	}
	if b, err = json.Marshal(list); err != nil {
		t.Fatal(err)
	}
	return b
}

// planetMapRig is one data folder signed in through the page, with a world
// made by step 1 and the five mods put in by hand as step 2.
type planetMapRig struct {
	*worldFileEnv
	t      *testing.T
	cookie *http.Cookie
	id     string
	set    string // the folder of the planet mods
}

func newPlanetMapRig(t *testing.T, dc *docker.Client, set, name string) *planetMapRig {
	t.Helper()
	e := newWorldFileEnv(t, dc)
	// newWorldFileEnv's user has no password, so the page gets its own.
	e.hn.addUser("bob", testPassword)
	r := &planetMapRig{worldFileEnv: e, t: t, cookie: e.hn.signInAgain("bob"), set: set}
	gen := generateForm("name", name, "game_version_typed", newMapGameVersion, "map_key", "2.1|1")
	r.id = idFromLocation(t, e.hn.postLong(PathGenerate, gen, r.cookie))
	if !strings.HasPrefix(game.ContainerName(r.id), "foreman-"+name) {
		t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(r.id))
	}
	e.cleanup(t, dc, r.id)
	zips, err := filepath.Glob(filepath.Join(set, "*.zip"))
	if err != nil || len(zips) < 5 {
		t.Fatalf("want the planet mods' zips in %s, found %v (%v)", set, zips, err)
	}
	for _, z := range zips {
		b, err := os.ReadFile(z) //nolint:gosec // the tester names the folder
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.mods(), filepath.Base(z)), b, 0o640); err != nil { //nolint:gosec // the test's own world
			t.Fatal(err)
		}
	}
	r.writeList()
	return r
}

func (r *planetMapRig) mods() string { return filepath.Join(r.dir(r.id), world.ModsDir) }

// writeList writes the set's mod list with the named mods turned off.
func (r *planetMapRig) writeList(off ...string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.mods(), world.ModListFile), newMapModList(r.t, r.set, off...), 0o640); err != nil { //nolint:gosec // the test's own world
		r.t.Fatal(err)
	}
}

// save posts step 3's form as the page shows it, with set laid over it.
func (r *planetMapRig) save(set map[string]string) {
	r.t.Helper()
	form := newMapPageValues(r.t, r.hn, r.cookie, r.id)
	for k, v := range set {
		if !form.Has(k) {
			r.t.Fatalf("step 3 has no field %s", k)
		}
		form.Set(k, v)
	}
	rec := r.hn.postLong(newMapPath(r.id), form, r.cookie)
	if rec.Code != http.StatusSeeOther {
		r.t.Fatalf("step 3 save: status %d:\n%s", rec.Code, rec.Body.String())
	}
}

// savedControls is the autoplace_controls of the world's saved map settings.
func (r *planetMapRig) savedControls() map[string]map[string]float64 {
	r.t.Helper()
	cf, err := r.hn.worlds.ReadConfig(r.id, world.MapGenSettingsFile)
	if err != nil || !cf.Found {
		r.t.Fatalf("no saved map settings: %v", err)
	}
	return controlsOf(r.t, cf.Data)
}

func controlsOf(t *testing.T, mapGen []byte) map[string]map[string]float64 {
	t.Helper()
	var f struct {
		Controls map[string]map[string]float64 `json:"autoplace_controls"`
	}
	if err := json.Unmarshal(mapGen, &f); err != nil {
		t.Fatalf("map settings: %v\n%s", err, mapGen)
	}
	return f.Controls
}

// start starts the world, waits for InGame, and returns the autoplace
// controls of the map exchange string the running game reports.
func (r *planetMapRig) start() map[string]map[string]float64 {
	r.t.Helper()
	ctx := context.Background()
	begin := time.Now()
	if err := r.ctrl.Start(ctx, r.id, r.by, game.StartOptions{}); err != nil {
		r.t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(r.ctrl.Lines(r.id), "\n"))
	}
	waitUntil(r.t, 3*time.Minute, "InGame", func() bool { return r.inGames(r.id) > 0 })
	r.t.Logf("TIMING first start to InGame: %s", time.Since(begin).Round(100*time.Millisecond))
	cmd := "/silent-command rcon.print(game.get_map_exchange_string())"
	var reply string
	for range 2 {
		var err error
		if _, reply, err = r.ctrl.Console(ctx, r.id, r.by, cmd, cmd); err != nil {
			r.t.Fatalf("RCON: %v", err)
		}
		if strings.Contains(reply, ">>>") {
			break
		}
	}
	res, err := mapexchange.Decode(strings.TrimSpace(reply))
	if err != nil {
		r.t.Fatalf("the game's map exchange string does not decode: %v\n%s", err, reply)
	}
	if err := r.ctrl.Stop(ctx, r.id, r.by, game.StopOptions{}); err != nil {
		r.t.Fatalf("stop: %v", err)
	}
	return controlsOf(r.t, res.MapGenSettings)
}

func TestRealGameNewMapWithPlanetMods(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && !strings.HasPrefix(newMapGameVersion, only) {
		t.Skipf("runs on %s only", newMapGameVersion)
	}
	set := os.Getenv("FOREMAN_TEST_PLANET_MODS")
	if set == "" {
		t.Skip("FOREMAN_TEST_PLANET_MODS is not set")
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dc.InspectImage(context.Background(), game.ImageRef(newMapGameVersion)); err != nil {
		t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", newMapGameVersion, err)
	}
	// Moshine's own ore, and calcite, which Vulcanus shares, changed on
	// corrundum's tab only.
	changed := map[string]string{
		"mg.moshine.multi_ore.richness":  "200",
		"mg.moshine.multi_ore.size":      "50",
		"mg.corrundum.calcite.frequency": "300",
	}

	t.Run("saved settings reach the first map", func(t *testing.T) {
		r := newPlanetMapRig(t, dc, set, "newmap-realgame7-a")
		r.save(changed)
		saved := r.savedControls()
		t.Logf("saved: multi_ore %v, calcite %v, steam_geyser %v", saved["multi_ore"], saved["calcite"], saved["steam_geyser"])
		if !near(saved["multi_ore"]["richness"], 2.0) || !near(saved["multi_ore"]["size"], 0.5) || !near(saved["calcite"]["frequency"], 3.0) {
			t.Fatalf("the saved file does not hold the changes: %v", saved)
		}
		got := r.start()
		t.Logf("in the game: multi_ore %v, calcite %v, steam_geyser %v", got["multi_ore"], got["calcite"], got["steam_geyser"])
		for _, c := range []struct {
			control, setting string
			want             float64
		}{{"multi_ore", "richness", 2}, {"multi_ore", "size", 0.5}, {"calcite", "frequency", 3}} {
			if !near(got[c.control][c.setting], c.want) {
				t.Errorf("the game's %s %s is %v, want %v", c.control, c.setting, got[c.control][c.setting], c.want)
			}
		}
		if !r.logHas(r.id, "Moshine") {
			t.Error("the game's log never names Moshine, so the mod did not load")
		}
	})

	t.Run("a removed planet mod refuses the start until step 3 is saved again", func(t *testing.T) {
		r := newPlanetMapRig(t, dc, set, "newmap-realgame7-b")
		r.save(changed)
		r.writeList("Moshine", "Moshine-assets")
		err := r.ctrl.Start(context.Background(), r.id, r.by, game.StartOptions{})
		var pe *game.PrecheckError
		if !errors.As(err, &pe) {
			t.Fatalf("start error %v, want a pre-start refusal", err)
		}
		var stale []string
		for _, p := range pe.Check.Problems {
			if p.Kind == world.ProblemStaleControls {
				stale = p.Names
			}
		}
		if !slices.Equal(stale, []string{"multi_ore", "steam_geyser"}) {
			t.Errorf("stale controls %v, want multi_ore and steam_geyser; problems %+v", stale, pe.Check.Problems)
		}
		if r.inGames(r.id) != 0 {
			t.Fatal("the refused world reached InGame")
		}
		if _, found, _ := r.hn.worlds.LoadTarget(r.id); found {
			t.Error("the refused start left a save")
		}

		r.save(nil)
		if saved := r.savedControls(); saved["multi_ore"] != nil {
			t.Errorf("the second save kept Moshine's control: %v", saved)
		}
		got := r.start()
		if !near(got["calcite"]["frequency"], 3.0) {
			t.Errorf("calcite frequency %v after the second save, want 3 kept", got["calcite"]["frequency"])
		}
	})
}
