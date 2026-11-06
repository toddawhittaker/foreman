package game

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
)

func previewRequest() PreviewRequest {
	return PreviewRequest{GameVersion: "2.1.24", Seed: 7, Planets: []string{"nauvis"}}
}

// previewContainers lists the preview containers the fake Docker holds.
func (r *rig) previewContainers() []dockertest.Container {
	var out []dockertest.Container
	for _, c := range r.game.Daemon().Containers() {
		if _, ok := c.Spec.Labels[gameenv.LabelPreview]; ok {
			out = append(out, c)
		}
	}
	return out
}

func (r *rig) previewFolders() []string {
	entries, err := os.ReadDir(filepath.Join(r.data, previewDir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// waitPreviewContainer waits until a preview container exists.
func (r *rig) waitPreviewContainer() dockertest.Container {
	r.t.Helper()
	var found []dockertest.Container
	eventually(r.t, func() bool {
		found = r.previewContainers()
		return len(found) > 0
	}, "a preview container")
	return found[0]
}

// SPEC 5.6: a preview draws one picture per planet, in order; the container
// and the work folder are gone afterwards.
func TestPreviewDrawsEachPlanet(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	req := previewRequest()
	req.SpaceAge = true
	req.Planets = []string{"nauvis", "vulcanus", "gleba"}
	req.Preset = "island"
	req.MapGenSettings = []byte(`{"width": 500}`)
	req.MapSettings = []byte(`{"pollution": {}}`)

	pngs, err := r.c.Preview(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range pngs {
		got = append(got, p.Planet)
		if _, err := png.Decode(bytes.NewReader(p.Data)); err != nil {
			t.Errorf("%s: not a PNG: %v", p.Planet, err)
		}
	}
	if !slices.Equal(got, req.Planets) {
		t.Errorf("planets %v, want %v", got, req.Planets)
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d preview containers left", len(left))
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 5.6: the same settings give the same pictures, another seed another.
func TestPreviewFollowsTheSeed(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	draw := func(seed uint32) []byte {
		req := previewRequest()
		req.Seed = seed
		pngs, err := r.c.Preview(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return pngs[0].Data
	}
	if !bytes.Equal(draw(1), draw(1)) || bytes.Equal(draw(1), draw(2)) {
		t.Error("the picture does not follow the seed")
	}
}

// SPEC 14.3, 14.4: the preview container's fixed shape: the game's user, no
// network, a read-only root, nothing added, a memory limit, no restart, and
// one bind mount, a folder under the data directory.
func TestPreviewSpecIsLockedDown(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	req := previewRequest()
	req.Preset = "death-world"
	s := r.c.previewSpec(image21, "p-abc", "2.1", req)
	h := s.HostConfig
	switch {
	case s.Image != image21 || s.User != "845:845":
		t.Errorf("image %q, user %q", s.Image, s.User)
	case h.NetworkMode != "none":
		t.Errorf("network %q", h.NetworkMode)
	case !h.ReadonlyRootfs:
		t.Error("the root filesystem is writable")
	case !slices.Equal(h.CapDrop, []string{"ALL"}) || !slices.Equal(h.SecurityOpt, []string{"no-new-privileges"}):
		t.Errorf("capabilities %v, security %v", h.CapDrop, h.SecurityOpt)
	case h.Memory <= 0 || h.MemorySwap != h.Memory:
		t.Errorf("memory %d with swap limit %d", h.Memory, h.MemorySwap)
	case h.RestartPolicy.Name != "no" && h.RestartPolicy.Name != "":
		t.Errorf("restart policy %q", h.RestartPolicy.Name)
	case len(h.PortBindings) != 0 || len(s.ExposedPorts) != 0:
		t.Error("a port is published")
	case len(h.Binds) != 1 || h.Binds[0] != filepath.Join(r.data, "previews", "p-abc")+":/out":
		t.Errorf("binds %v", h.Binds)
	}
	if _, ok := s.Labels[gameenv.LabelPreview]; !ok {
		t.Error("the container is not labelled as a preview")
	}
	if !slices.Contains(s.Env, gameenv.PreviewPlanetFlag+"=1") || !slices.Contains(s.Env, gameenv.Preset+"=death-world") ||
		!slices.Contains(s.Env, gameenv.Seed+"=7") || !slices.Contains(s.Env, gameenv.PreviewPlanets+"=nauvis") {
		t.Errorf("environment %v", s.Env)
	}
	// The script holds no value of the request.
	if strings.Contains(strings.Join(s.Cmd, " "), "death-world") {
		t.Error("a value is built into the script")
	}
}

// SPEC 5.6: --map-preview-planet exists on 2.x only, so 1.1 is not given it.
func TestPreviewPlanetFlagIsFor2xOnly(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	flag := gameenv.PreviewPlanetFlag + "=1"
	for line, want := range map[string]bool{"1.1": false, "2.0": true, "2.1": true} {
		if got := slices.Contains(r.c.previewSpec(image21, "p", line, previewRequest()).Env, flag); got != want {
			t.Errorf("line %s: flag given is %t, want %t", line, got, want)
		}
	}
}

// SPEC 14.3: the real container is made from the request's image, with the
// lock-down, and goes when the 60-second limit passes: it is killed and
// removed, and the folder goes with it.
func TestPreviewTimesOutAndCleansUp(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.previewLimit = 300 * time.Millisecond
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.c.Preview(context.Background(), previewRequest())
		done <- err
	}()
	c := r.waitPreviewContainer()
	if c.Spec.Image != image21 || c.Spec.HostConfig.NetworkMode != "none" || !c.Spec.HostConfig.ReadonlyRootfs {
		t.Errorf("the container made is %+v", c.Spec)
	}
	if err := <-done; !errors.Is(err, ErrPreviewTimeout) {
		t.Fatalf("error %v, want ErrPreviewTimeout", err)
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d preview containers left", len(left))
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 14.3: one preview runs at a time across Foreman; a second caller is
// told so at once and no second container is made.
func TestPreviewIsOneAtATime(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := r.c.Preview(ctx, previewRequest())
		first <- err
	}()
	r.waitPreviewContainer()
	if _, err := r.c.Preview(context.Background(), previewRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("second preview: %v, want ErrPreviewBusy", err)
	}
	if n := len(r.previewContainers()); n != 1 {
		t.Errorf("%d preview containers", n)
	}
	// A caller who goes away does not leave the container behind.
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("first preview: %v, want context.Canceled", err)
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d preview containers left", len(left))
	}
	if err := r.game.SetPreviewMode(fakegame.PreviewOK); err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.Preview(context.Background(), previewRequest()); err != nil {
		t.Errorf("the preview slot was not freed: %v", err)
	}
}

// SPEC 3.7: a preview never downloads an image.
func TestPreviewNeedsTheImageAlreadyDownloaded(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.game.SetImage(image21, false)
	_, err := r.c.Preview(context.Background(), previewRequest())
	if !errors.Is(err, ErrPreviewImageMissing) {
		t.Fatalf("error %v, want ErrPreviewImageMissing", err)
	}
	if len(r.calls("POST /images/create")) != 0 || len(r.calls("POST /containers/create")) != 0 {
		t.Errorf("Docker was asked for more: %v", r.game.State().Calls)
	}
}

// SPEC 5.6: a game that fails is a *PreviewError with its own words; the
// container and folder are still removed.
func TestPreviewReportsAFailedGame(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewFails); err != nil {
		t.Fatal(err)
	}
	_, err := r.c.Preview(context.Background(), previewRequest())
	var pe *PreviewError
	if !errors.As(err, &pe) || pe.ExitCode != 1 || !strings.Contains(pe.Log, "told to fail") {
		t.Fatalf("error %v, want a *PreviewError with exit 1 and the game's words", err)
	}
	if len(r.previewContainers()) != 0 || len(r.previewFolders()) != 0 {
		t.Error("the container or folder was left")
	}
}

// SPEC 5.6, 14.3: planet and preset names go to a script, so only the game
// line's own names pass, and nothing reaches Docker for a bad request.
func TestPreviewRefusesRequestsThatTheLineCannotDraw(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	cases := map[string]func(*PreviewRequest){
		"a Space Age planet without Space Age": func(q *PreviewRequest) { q.Planets = []string{"nauvis", "vulcanus"} },
		"a planet 1.1 lacks": func(q *PreviewRequest) {
			q.GameVersion = "1.1.110"
			q.SpaceAge = true
			q.Planets = []string{"vulcanus"}
		},
		"a made-up planet":                   func(q *PreviewRequest) { q.Planets = []string{"nauvis; rm -rf /"} },
		"the same planet twice":              func(q *PreviewRequest) { q.Planets = []string{"nauvis", "nauvis"} },
		"no planets":                         func(q *PreviewRequest) { q.Planets = nil },
		"an unknown preset":                  func(q *PreviewRequest) { q.Preset = "x; touch y" },
		"a game version that is a line":      func(q *PreviewRequest) { q.GameVersion = "2.1" },
		"an unsupported version":             func(q *PreviewRequest) { q.GameVersion = "3.0.1" },
		"map-gen settings that are not JSON": func(q *PreviewRequest) { q.MapGenSettings = []byte(`{"a":`) },
		"map settings that are too big":      func(q *PreviewRequest) { q.MapSettings = append([]byte(`"`), make([]byte, 2<<20)...) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := previewRequest()
			mutate(&req)
			if _, err := r.c.Preview(context.Background(), req); !errors.Is(err, ErrPreviewInvalid) {
				t.Errorf("error %v, want ErrPreviewInvalid", err)
			}
		})
	}
	if len(r.game.State().Calls) != 0 {
		t.Errorf("Docker was called: %v", r.game.State().Calls)
	}
}

// SPEC 4.3, 14.3: what an earlier preview left behind after a crash is
// removed by the next one: containers with the preview label (this install's
// only) and work folders.
func TestPreviewRemovesLeftovers(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.game.Daemon().Seed(dockertest.Container{
		Name: "foreman-preview-old", Status: "running",
		Spec: docker.ContainerSpec{Labels: map[string]string{gameenv.LabelPreview: r.data}},
	})
	r.game.Daemon().Seed(dockertest.Container{
		Name: "foreman-preview-other-install", Status: "exited",
		Spec: docker.ContainerSpec{Labels: map[string]string{gameenv.LabelPreview: "/another/data"}},
	})
	stale := filepath.Join(r.data, previewDir, "p-crashed")
	if err := os.MkdirAll(stale, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.Preview(context.Background(), previewRequest()); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range r.previewContainers() {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"foreman-preview-other-install"}) {
		t.Errorf("containers left: %v, want only the other install's", names)
	}
	if len(r.previewFolders()) != 0 {
		t.Errorf("folders left: %v", r.previewFolders())
	}
}

// SPEC 5.6: a picture over the size cap, or one that is not a PNG, is
// refused rather than passed on.
func TestReadPreviewsRefusesWhatIsNotAPicture(t *testing.T) {
	base, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	if err := base.Mkdir("p", 0o750); err != nil {
		t.Fatal(err)
	}
	put := func(name string, b []byte) {
		if err := base.WriteFile(filepath.Join("p", name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("nauvis.png", []byte("not a png"))
	if _, err := readPreviews(base, "p", []string{"nauvis"}); err == nil {
		t.Error("a file that is not a PNG was accepted")
	}
	put("nauvis.png", append(append([]byte{}, pngSignature...), make([]byte, maxPreviewPNG)...))
	if _, err := readPreviews(base, "p", []string{"nauvis"}); err == nil {
		t.Error("a picture over the cap was accepted")
	}
	if _, err := readPreviews(base, "p", []string{"gleba"}); err == nil {
		t.Error("a missing picture was accepted")
	}
}
