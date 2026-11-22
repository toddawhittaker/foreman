package game

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
)

var updatePreviewGolden = flag.Bool("update-preview-golden", false, "rewrite testdata/preview-no-world.json")

// previewCapture is what a preview run gave the game: its container and the
// files of its work folder, with the data folder and the work folder's
// random name replaced, so two runs compare equal.
type previewCapture struct {
	Name  string
	Spec  docker.ContainerSpec
	Files map[string]string
}

// capturePreview starts run in the fake's hang mode, captures the container
// and its work folder while it runs, then ends the run.
func capturePreview(t *testing.T, r *rig, kind string, run func(ctx context.Context) error) previewCapture {
	t.Helper()
	return capturePreviewWith(t, r, kind, run, func(string) {})
}

// capturePreviewWith is capturePreview that also calls inspect with the
// work folder's path while the container runs.
func capturePreviewWith(t *testing.T, r *rig, kind string, run func(ctx context.Context) error, inspect func(dir string)) previewCapture {
	t.Helper()
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	var c dockertest.Container
	eventually(t, func() bool {
		for _, p := range r.previewContainers() {
			if p.Status == "running" {
				c = p
				return true
			}
		}
		return false
	}, "a running preview container")
	folder, ok := strings.CutPrefix(c.Name, "foreman-"+kind+"-")
	if !ok {
		t.Fatalf("container name %q", c.Name)
	}
	dir := filepath.Join(r.data, previewDir, folder)
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // the test's own work folder
		rel, _ := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	inspect(dir)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled run returned %v", err)
	}
	raw, err := json.Marshal(c.Spec)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(raw), folder, "FOLDER")
	s = strings.ReplaceAll(s, r.data, "DATA")
	var spec docker.ContainerSpec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		t.Fatal(err)
	}
	return previewCapture{Name: strings.ReplaceAll(c.Name, folder, "FOLDER"), Spec: spec, Files: files}
}

// SPEC 5.6, 14.3, ADR 0033: without a world, a preview, a territory run and
// a resource count make the containers and work folders they made before
// runs could load a world's mods. The golden file was written by the code
// before that change.
func TestPreviewRunsWithoutAWorldAreUnchanged(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	settings := func(req PreviewRequest) PreviewRequest {
		req.Preset = "island"
		req.MapGenSettings = []byte(`{"width": 500}`)
		req.MapSettings = []byte(`{"pollution": {}}`)
		return req
	}
	got := map[string]previewCapture{}
	preview := settings(previewRequest())
	preview.SpaceAge = true
	preview.Planets = []string{"nauvis", "vulcanus", "gleba"}
	got["preview"] = capturePreview(t, r, "preview", func(ctx context.Context) error {
		_, err := r.c.Preview(ctx, preview)
		return err
	})
	got["territories"] = capturePreview(t, r, "territories", func(ctx context.Context) error {
		_, err := r.c.Territories(ctx, settings(territoriesRequest()))
		return err
	})
	got["resources"] = capturePreview(t, r, "resources", func(ctx context.Context) error {
		_, err := r.c.Resources(ctx, settings(resourcesRequest()), "gleba")
		return err
	})
	out, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "preview-no-world.json")
	if *updatePreviewGolden {
		if err := os.WriteFile(golden, append(out, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden) //nolint:gosec // a fixed test file
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != string(out) {
		t.Errorf("runs without a world changed.\ngot:\n%s\nwant:\n%s", out, want)
	}
}
