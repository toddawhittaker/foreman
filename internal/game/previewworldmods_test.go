package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/world"
)

// newModdedWorld makes a 2.1 world with Space Age and no save, a mod zip, a
// mod list that enables it, and a mod-settings.dat.
func newModdedWorld(t *testing.T, r *rig) string {
	t.Helper()
	id, err := r.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.worldDir(id), world.ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	r.writeFile(id, filepath.Join(world.ModsDir, "planet_1.0.0.zip"), []byte("PK zip"))
	r.writeFile(id, filepath.Join(world.ModsDir, world.ModListFile),
		[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":true},{"name":"planet","enabled":true}]}`))
	r.writeFile(id, filepath.Join(world.ModsDir, world.ModSettingsFile), []byte("settings"))
	return id
}

// worldRequest is a preview of Nauvis and the modded planet "cerys" on
// world id, whose planet list also has Vulcanus.
func worldRequest(id string) PreviewRequest {
	return PreviewRequest{
		GameVersion: "2.1.24", SpaceAge: true, Seed: 7, World: id,
		Planets:      []string{"nauvis", "cerys"},
		KnownPlanets: []string{"nauvis", "vulcanus", "cerys"},
	}
}

// SPEC 14.3, ADR 0033: with a world, each run's work folder holds the
// world's mod zips as hard links, mounted read-only over themselves, and
// copies of its mod-list.json and mod-settings.dat; the one bind mount is
// still the work folder, never the world's own folder.
func TestPreviewRunsLoadTheWorldsMods(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	worldMods := filepath.Join(r.worldDir(id), world.ModsDir)
	runs := map[string]func(ctx context.Context) error{
		"preview": func(ctx context.Context) error {
			_, err := r.c.Preview(ctx, worldRequest(id))
			return err
		},
		"territories": func(ctx context.Context) error {
			_, err := r.c.Territories(ctx, worldRequest(id))
			return err
		},
		"resources": func(ctx context.Context) error {
			_, err := r.c.Resources(ctx, worldRequest(id), "cerys")
			return err
		},
	}
	same := func(a, b string) (linked bool, ok bool) {
		x, err := os.Stat(a)
		y, err2 := os.Stat(b)
		return err == nil && err2 == nil && os.SameFile(x, y), err == nil && err2 == nil
	}
	for kind, run := range runs {
		t.Run(kind, func(t *testing.T) {
			var dir string
			var zipLinked, settingsLinked, settingsThere bool
			got := capturePreviewWith(t, r, kind, run, func(work string) {
				dir = work
				zipLinked, _ = same(filepath.Join(work, world.ModsDir, "planet_1.0.0.zip"), filepath.Join(worldMods, "planet_1.0.0.zip"))
				settingsLinked, settingsThere = same(filepath.Join(work, world.ModsDir, world.ModSettingsFile), filepath.Join(worldMods, world.ModSettingsFile))
			})
			if !zipLinked {
				t.Error("the world's mod zip is not hard-linked into the work folder")
			}
			if !settingsThere || settingsLinked || got.Files["mods/"+world.ModSettingsFile] != "settings" {
				t.Errorf("mod-settings.dat is not a copy of the world's: %q", got.Files["mods/"+world.ModSettingsFile])
			}
			if list := got.Files["mods/"+world.ModListFile]; !strings.Contains(list, `"planet"`) {
				t.Errorf("mod list %s is not the world's", list)
			}
			h := got.Spec.HostConfig
			if len(h.Binds) != 1 || h.Binds[0] != "DATA/previews/FOLDER:/out" {
				t.Errorf("binds %v", h.Binds)
			}
			want := []docker.BindMount{{Type: "bind", Source: "DATA/previews/FOLDER/mods/planet_1.0.0.zip",
				Target: "/out/mods/planet_1.0.0.zip", ReadOnly: true}}
			if !reflect.DeepEqual(h.Mounts, want) {
				t.Errorf("mounts %+v, want %+v", h.Mounts, want)
			}
			if h.Memory != previewMemory {
				t.Errorf("memory %d, want %d", h.Memory, previewMemory)
			}
			if _, err := os.Stat(dir); err == nil {
				t.Error("the work folder is left behind")
			}
		})
	}
	if b, _ := os.ReadFile(filepath.Join(worldMods, world.ModSettingsFile)); string(b) != "settings" { //nolint:gosec // the test's own world
		t.Errorf("the world's mod-settings.dat is now %q", b)
	}
}

// SPEC 14.3: a world mod with the name of a run's control-only mod refuses
// the run before any container is made.
func TestPreviewRunsRefuseAWorldModNamedLikeTheControlMod(t *testing.T) {
	for kind, mod := range map[string]string{"territories": territoryMod.dir, "resources": resourceMod.dir} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t, rigOptions{noRun: true})
			id := newModdedWorld(t, r)
			r.writeFile(id, filepath.Join(world.ModsDir, mod+".zip"), []byte("PK"))
			var err error
			if kind == "territories" {
				_, err = r.c.Territories(context.Background(), worldRequest(id))
			} else {
				_, err = r.c.Resources(context.Background(), worldRequest(id), "nauvis")
			}
			if !errors.Is(err, ErrControlModTaken) {
				t.Fatalf("error %v, want ErrControlModTaken", err)
			}
			if n := len(r.calls("POST /containers/create")); n != 0 || len(r.previewFolders()) != 0 {
				t.Errorf("%d containers made, folders %v", n, r.previewFolders())
			}
		})
	}
}

// SPEC 5.6, 14.3: with a world, planets come from the known list the caller
// passes, not the built-in one, and every name has the safe shape; without
// a world, the built-in list still decides.
func TestPreviewRunsCheckPlanetsAgainstTheKnownList(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	for name, change := range map[string]func(*PreviewRequest){
		"a planet the world lacks": func(q *PreviewRequest) { q.Planets = []string{"gleba"} },
		"a bad name in the list": func(q *PreviewRequest) {
			q.Planets = []string{`x" ; rm`}
			q.KnownPlanets = q.Planets
		},
		"no known list":                 func(q *PreviewRequest) { q.KnownPlanets = nil },
		"a world that is not an id":     func(q *PreviewRequest) { q.World = "../" + id },
		"a modded planet with no world": func(q *PreviewRequest) { q.World = "" },
	} {
		q := worldRequest(id)
		change(&q)
		if _, err := r.c.Preview(context.Background(), q); !errors.Is(err, ErrPreviewInvalid) {
			t.Errorf("preview with %s: error %v, want ErrPreviewInvalid", name, err)
		}
	}
	q := worldRequest(id)
	if _, err := r.c.Resources(context.Background(), q, "gleba"); !errors.Is(err, ErrPreviewInvalid) {
		t.Errorf("count of a planet the world lacks: error %v", err)
	}
	q.KnownPlanets = []string{"nauvis", "cerys"}
	if _, err := r.c.Territories(context.Background(), q); !errors.Is(err, ErrPreviewInvalid) {
		t.Errorf("territories of a world without Vulcanus: error %v", err)
	}
	if n := len(r.calls("POST /containers/create")); n != 0 {
		t.Errorf("%d containers made", n)
	}
	pngs, err := r.c.Preview(context.Background(), worldRequest(id))
	if err != nil || len(pngs) != 2 || pngs[1].Planet != "cerys" {
		t.Fatalf("a modded planet: %v, %d pictures", err, len(pngs))
	}
}

// SPEC 14.3: with a world's mods the time limit is the preview's for each
// planet drawn.
func TestPreviewWithAWorldHasALimitPerPlanet(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	r.c.previewLimit = 200 * time.Millisecond
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	q := worldRequest(id)
	q.Planets = []string{"nauvis", "vulcanus", "cerys"}
	began := time.Now()
	_, err := r.c.Preview(context.Background(), q)
	if !errors.Is(err, ErrPreviewTimeout) {
		t.Fatalf("error %v, want ErrPreviewTimeout", err)
	}
	if took := time.Since(began); took < 3*r.c.previewLimit {
		t.Errorf("stopped after %v, want at least %v for three planets", took, 3*r.c.previewLimit)
	}
}

// SPEC 14.3: a world that server control holds, a request for another game
// version or Space Age than the world's, and mods past the link caps refuse
// the run before any container is made, and leave no folder.
func TestPreviewWithAWorldRefusals(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	unlock, err := r.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.Preview(context.Background(), worldRequest(id)); !errors.Is(err, world.ErrBusy) {
		t.Errorf("a held world: error %v, want world.ErrBusy", err)
	}
	unlock()
	q := worldRequest(id)
	q.SpaceAge = false
	q.Planets = []string{"nauvis"}
	if _, err := r.c.Preview(context.Background(), q); !errors.Is(err, ErrPreviewInvalid) {
		t.Errorf("another Space Age: error %v, want ErrPreviewInvalid", err)
	}
	q = worldRequest(id)
	q.GameVersion = "2.0.77"
	if _, err := r.c.Preview(context.Background(), q); !errors.Is(err, ErrPreviewInvalid) {
		t.Errorf("another game version: error %v, want ErrPreviewInvalid", err)
	}
	restore := world.SetLinkLimitsForTest(1, 1<<20)
	defer restore()
	if _, err := r.c.Preview(context.Background(), worldRequest(id)); !errors.Is(err, world.ErrModsTooBig) {
		t.Errorf("too many mod files: error %v, want world.ErrModsTooBig", err)
	}
	if n := len(r.calls("POST /containers/create")); n != 0 || len(r.previewFolders()) != 0 {
		t.Errorf("%d containers made, folders %v", n, r.previewFolders())
	}
}
