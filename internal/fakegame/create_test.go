package fakegame_test

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// createRig is a rig whose world folder has the saves and mods folders the
// start script's create step uses.
func createRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, fakegame.Config{})
	for _, d := range []string{world.SavesDir, world.ModsDir} {
		if err := os.MkdirAll(filepath.Join(r.world, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// startWithEnv creates and starts a container with the given environment.
func (r *rig) startWithEnv(name, img string, env ...string) string {
	r.t.Helper()
	ctx := context.Background()
	spec := r.spec(img, "no")
	spec.Env = env
	id, err := r.docker.CreateContainer(ctx, name, spec)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *rig) readSave(name string) savefile.Save {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.world, world.SavesDir, name)) //nolint:gosec // a test's own temp folder
	if err != nil {
		r.t.Fatal(err)
	}
	s, err := savefile.Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		r.t.Fatalf("the save does not read: %v", err)
	}
	return s
}

// SPEC 4.3: the create step makes a save the save reader accepts, at the
// version of the container's image, with the mods the world's mod list
// enables.
func TestCreateStepMakesAReadableSave(t *testing.T) {
	for _, c := range []struct{ image, version string }{
		{image21, "2.1.24"}, {image11, "1.1.110"},
	} {
		t.Run(c.version, func(t *testing.T) {
			r := createRig(t)
			if err := os.WriteFile(filepath.Join(r.world, world.ModsDir, world.ModListFile),
				[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"quality","enabled":false},{"name":"space-age","enabled":true}]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			r.startWithEnv("foreman-w", c.image, gameenv.NewMap+"=1", gameenv.WorldID+"=my-world")
			s := r.readSave("my-world.zip")
			if s.Version.String() != c.version {
				t.Errorf("save version %s, want %s", s.Version, c.version)
			}
			var names []string
			for _, m := range s.Mods {
				names = append(names, m.Name)
			}
			if len(names) != 2 || names[0] != "base" || names[1] != "space-age" {
				t.Errorf("mods %v, want base and space-age", names)
			}
		})
	}
}

// SPEC 4.3: the create step never replaces a save, and an ordinary start
// makes none.
func TestCreateStepLeavesSavesAlone(t *testing.T) {
	r := createRig(t)
	existing := filepath.Join(r.world, world.SavesDir, "mine.zip")
	if err := os.WriteFile(existing, []byte("a person's save"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.startWithEnv("foreman-a", image21, gameenv.NewMap+"=1", gameenv.WorldID+"=my-world")
	if b, _ := os.ReadFile(existing); string(b) != "a person's save" { //nolint:gosec // a test's own temp folder
		t.Error("the save was changed")
	}
	if _, err := os.Stat(filepath.Join(r.world, world.SavesDir, "my-world.zip")); err == nil {
		t.Error("a second save was made beside the first")
	}

	r2 := createRig(t)
	r2.startWithEnv("foreman-b", image21)
	if entries, _ := os.ReadDir(filepath.Join(r2.world, world.SavesDir)); len(entries) != 0 {
		t.Errorf("an ordinary start made saves: %v", entries)
	}
}

// SPEC 5.6: a preview container draws a picture for each planet into the
// folder bound at /out and exits 0, the way --generate-map-preview does.
func TestPreviewContainerDrawsPictures(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	ctx := context.Background()
	spec := docker.ContainerSpec{
		Image:  image21,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env: []string{
			gameenv.PreviewPlanets + "=nauvis vulcanus", gameenv.Seed + "=9", gameenv.PreviewSize + "=1536",
		},
		HostConfig: docker.HostConfig{Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none"},
	}
	id, err := r.docker.CreateContainer(ctx, "foreman-preview-x", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if c := r.inspect(id); c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	for _, p := range []string{"nauvis", "vulcanus"} {
		b, err := os.ReadFile(filepath.Join(r.world, p+".png")) //nolint:gosec // a test's own temp folder
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			t.Errorf("%s is not a PNG: %v", p, err)
		} else if cfg.Width != 1536 || cfg.Height != 1536 {
			t.Errorf("%s is %dx%d, want 1536x1536", p, cfg.Width, cfg.Height)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(r.world, world.ConfigDir)); len(entries) != 1 {
		t.Errorf("a preview wrote outside its folder: %v", entries)
	}
}

// The preview modes end a preview container the way the real game can:
// failing, or never finishing.
func TestPreviewModes(t *testing.T) {
	for mode, want := range map[fakegame.PreviewMode]struct {
		status string
		code   int
	}{
		fakegame.PreviewFails: {"exited", 1},
		fakegame.PreviewHangs: {"running", 0},
	} {
		r := newRig(t, fakegame.Config{})
		if err := r.game.SetPreviewMode(mode); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		id, err := r.docker.CreateContainer(ctx, "foreman-preview-x", docker.ContainerSpec{
			Image: image21, Labels: map[string]string{gameenv.LabelPreview: "/data"},
			Env:        []string{gameenv.PreviewPlanets + "=nauvis"},
			HostConfig: docker.HostConfig{Binds: []string{r.world + ":" + gameenv.PreviewMount}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.docker.StartContainer(ctx, id); err != nil {
			t.Fatal(err)
		}
		if c := r.inspect(id); c.State.Status != want.status || c.State.ExitCode != want.code {
			t.Errorf("mode %s: state %+v, want %s with %d", mode, c.State, want.status, want.code)
		}
	}
	if err := (&fakegame.Game{}).SetPreviewMode("nonsense"); err == nil {
		t.Error("an unknown mode was accepted")
	}
}
