package game

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game/resources"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/resource"
)

func resourcesRequest() PreviewRequest {
	return PreviewRequest{GameVersion: "2.1.24", SpaceAge: true, Seed: 7}
}

// SPEC 5.6, 14.1: a count returns the patches the game's script wrote,
// parsed for the picture's size and planet, largest box first; the
// container and the work folder are gone after.
func TestResourcesReadsTheGamesFile(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	for _, planet := range []string{"nauvis", "vulcanus", "aquilo"} {
		res, err := r.c.Resources(context.Background(), resourcesRequest(), planet)
		if err != nil {
			t.Fatalf("%s: %v", planet, err)
		}
		// The fake game writes seven patches of three resources, one infinite.
		if res.Planet != planet || len(res.Patches) != 7 || len(res.Resources) != 3 {
			t.Errorf("%s: planet %q, %d patches, %d resources", planet, res.Planet, len(res.Patches), len(res.Resources))
		}
		var yields int
		for i, p := range res.Patches {
			if _, ok := resource.Yield(p); ok {
				yields++
			}
			if i > 0 {
				a, b := res.Patches[i-1].Box, p.Box
				if (a[2]-a[0]+1)*(a[3]-a[1]+1) < (b[2]-b[0]+1)*(b[3]-b[1]+1) {
					t.Errorf("%s: patch %d has a larger box than the one before it", planet, i)
				}
			}
		}
		if yields != 2 {
			t.Errorf("%s: %d patches with a yield, want the 2 of the infinite resource", planet, yields)
		}
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d containers left", len(left))
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 5.6: the radius reaches the game as half the picture in chunks, and
// the planet as itself.
func TestResourcesEnvFollowsTheSizeAndPlanet(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	for size, want := range map[int]string{0: "20", 1024: "16", 3072: "48"} {
		req := resourcesRequest()
		req.Size = size
		env := r.c.controlModSpec(image21, "p", req, resourceMod, resourcesEnv(req, "gleba")).Env
		for _, w := range []string{gameenv.ResourceRadius + "=" + want, gameenv.ResourcePlanet + "=gleba"} {
			if !slices.Contains(env, w) {
				t.Errorf("size %d: environment %v lacks %s", size, env, w)
			}
		}
	}
}

// SPEC 5.6, 14.3: the work folder holds the config, a mod list that enables
// the count mod beside the map's own mods, the mod for the game's own line,
// and the settings files a preview gets; the container is a labelled
// preview container with the count's environment.
func TestResourcesWorkFolderAndContainer(t *testing.T) {
	for _, tc := range []struct {
		version, line string
		spaceAge      bool
		planet        string
	}{
		{"2.1.24", "2.1", true, "vulcanus"},
		{"1.1.110", "1.1", false, "nauvis"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			r := newRig(t, rigOptions{noRun: true})
			if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
				t.Fatal(err)
			}
			req := PreviewRequest{GameVersion: tc.version, SpaceAge: tc.spaceAge, Seed: 123456, Preset: "island"}
			req.MapGenSettings = []byte(`{"width": 500}`)
			req.MapSettings = []byte(`{"pollution": {}}`)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				_, err := r.c.Resources(ctx, req, tc.planet)
				done <- err
			}()
			c := r.waitPreviewContainer()
			folder, ok := strings.CutPrefix(c.Name, "foreman-resources-")
			if !ok {
				t.Fatalf("container name %q", c.Name)
			}
			dir := filepath.Join(r.data, previewDir, folder)
			read := func(rel string) []byte {
				b, err := os.ReadFile(filepath.Join(dir, rel)) //nolint:gosec // the test's own work folder
				if err != nil {
					t.Fatal(err)
				}
				return b
			}
			var list struct {
				Mods []modEntry `json:"mods"`
			}
			if err := json.Unmarshal(read("mods/mod-list.json"), &list); err != nil {
				t.Fatal(err)
			}
			enabled := map[string]bool{}
			for _, m := range list.Mods {
				enabled[m.Name] = m.Enabled
			}
			if !enabled["base"] || enabled["space-age"] != tc.spaceAge || !enabled[resources.ModName] {
				t.Errorf("enabled mods %v", enabled)
			}
			if string(read("mods/"+resources.ModDir+"/info.json")) != string(resources.InfoJSON(tc.line, tc.spaceAge)) ||
				string(read("mods/"+resources.ModDir+"/control.lua")) != string(resources.Control) {
				t.Errorf("the mod folder is not info.json for %s and control.lua", tc.line)
			}
			if string(read("map-gen-settings.json")) != `{"width": 500}` || string(read("map-settings.json")) != `{"pollution": {}}` ||
				string(read("config.ini")) != previewConfig {
				t.Error("config or settings files differ from the preview's")
			}
			for _, want := range []string{
				gameenv.PreviewKind + "=resources", gameenv.ResourceRadius + "=20", gameenv.ResourcePlanet + "=" + tc.planet,
				gameenv.Seed + "=123456", gameenv.Preset + "=island",
			} {
				if !slices.Contains(c.Spec.Env, want) {
					t.Errorf("environment %v lacks %s", c.Spec.Env, want)
				}
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Errorf("error %v, want context.Canceled", err)
			}
		})
	}
}

// SPEC 14.3: the count container has the preview container's limits, and no
// value of the request is built into its script.
func TestResourcesSpecHasThePreviewsLimits(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	req := resourcesRequest()
	req.Preset = "death-world"
	got := r.c.controlModSpec(image21, "p-abc", req, resourceMod, resourcesEnv(req, "fulgora"))
	want := r.c.previewSpec(image21, "p-abc", "2.1", previewRequest())
	if !reflect.DeepEqual(got.HostConfig, want.HostConfig) {
		t.Errorf("host config %+v, want the preview's %+v", got.HostConfig, want.HostConfig)
	}
	if got.User != want.User || got.Image != want.Image || !reflect.DeepEqual(got.Labels, want.Labels) ||
		!reflect.DeepEqual(got.Entrypoint, want.Entrypoint) {
		t.Errorf("container %+v differs from the preview's %+v", got, want)
	}
	script := strings.Join(got.Cmd, " ")
	if strings.Contains(script, "death-world") || strings.Contains(script, "fulgora") {
		t.Error("a value is built into the script")
	}
	for _, need := range []string{
		`--create /out/resources.zip`, `--map-gen-seed "$` + gameenv.Seed + `"`,
		`--preset "$` + gameenv.Preset + `"`, `--map-gen-settings`, `--map-settings`,
		`cp /tmp/factorio/script-output/resources.json /out/resources.json`,
		`"$` + gameenv.ResourceRadius + `" "$` + gameenv.ResourcePlanet + `" > /out/mods/foreman-resources_1.0.0/params.lua`,
	} {
		if !strings.Contains(script, need) {
			t.Errorf("the script lacks %s:\n%s", need, script)
		}
	}
	if strings.Contains(script, "--start-server") {
		t.Error("the count run starts a server")
	}
}

// SPEC 5.6: a count is for one planet the map has; anything else is refused
// before Docker is called. 1.1 has Nauvis only, and a 2.x map without Space
// Age has Nauvis only.
func TestResourcesRefusesWhatHasNone(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	cases := map[string]struct {
		mutate func(*PreviewRequest)
		planet string
	}{
		"an unknown planet":          {func(*PreviewRequest) {}, "mars"},
		"a planet in a Lua string":   {func(*PreviewRequest) {}, `nauvis"}os.exit()--`},
		"no planet":                  {func(*PreviewRequest) {}, ""},
		"Vulcanus without Space Age": {func(q *PreviewRequest) { q.SpaceAge = false }, "vulcanus"},
		"Vulcanus on 1.1":            {func(q *PreviewRequest) { q.GameVersion, q.SpaceAge = "1.1.110", false }, "vulcanus"},
		"on a line, not a patch":     {func(q *PreviewRequest) { q.GameVersion = "2.1" }, "nauvis"},
		"above the most":             {func(q *PreviewRequest) { q.Size = PreviewSizeMax + PreviewSizeStep }, "nauvis"},
		"off the steps":              {func(q *PreviewRequest) { q.Size = 1100 }, "nauvis"},
		"an unknown preset":          {func(q *PreviewRequest) { q.Preset = "x; touch y" }, "nauvis"},
		"settings not JSON":          {func(q *PreviewRequest) { q.MapGenSettings = []byte(`{"a":`) }, "nauvis"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := resourcesRequest()
			tc.mutate(&req)
			if _, err := r.c.Resources(context.Background(), req, tc.planet); !errors.Is(err, ErrPreviewInvalid) {
				t.Errorf("error %v, want ErrPreviewInvalid", err)
			}
		})
	}
	if len(r.game.State().Calls) != 0 {
		t.Errorf("Docker was called: %v", r.game.State().Calls)
	}
	// Every planet the map has, on every line, is counted; req.Planets plays
	// no part.
	for _, ok := range []struct {
		version  string
		spaceAge bool
		planet   string
	}{
		{"1.1.110", false, "nauvis"}, {"2.0.77", false, "nauvis"}, {"2.1.24", true, "aquilo"},
	} {
		req := PreviewRequest{GameVersion: ok.version, SpaceAge: ok.spaceAge, Planets: []string{"x"}, Size: PreviewSizeMax}
		if res, err := r.c.Resources(context.Background(), req, ok.planet); err != nil || res.Planet != ok.planet {
			t.Errorf("%s %s: %v, %v", ok.version, ok.planet, res.Planet, err)
		}
	}
}

// SPEC 14.3: a count, a territory run and a map preview share the one slot,
// and a count frees it when it ends.
func TestResourcesShareThePreviewSlot(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.previewWait = 100 * time.Millisecond
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := r.c.Resources(ctx, resourcesRequest(), "nauvis")
		first <- err
	}()
	r.waitPreviewContainer()
	if _, err := r.c.Preview(context.Background(), previewRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("preview during a count: %v, want ErrPreviewBusy", err)
	}
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("territory run during a count: %v, want ErrPreviewBusy", err)
	}
	if _, err := r.c.Resources(context.Background(), resourcesRequest(), "nauvis"); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("second count: %v, want ErrPreviewBusy", err)
	}
	if n := len(r.previewContainers()); n != 1 {
		t.Errorf("%d containers, want the running one only", n)
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Errorf("first run: %v, want context.Canceled", err)
	}
	if err := r.game.SetPreviewMode(fakegame.PreviewOK); err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.Preview(context.Background(), previewRequest()); err != nil {
		t.Errorf("the slot was not freed: %v", err)
	}
}

// SPEC 14.3: a caller who goes away mid-run stops the container and removes
// it and the folder; a run past the limit is ErrPreviewTimeout.
func TestResourcesCancelAndTimeoutCleanUp(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.c.Resources(ctx, resourcesRequest(), "nauvis")
		done <- err
	}()
	r.waitPreviewContainer()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	r.c.previewLimit = 300 * time.Millisecond
	if _, err := r.c.Resources(context.Background(), resourcesRequest(), "nauvis"); !errors.Is(err, ErrPreviewTimeout) {
		t.Fatalf("error %v, want ErrPreviewTimeout", err)
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d containers left", len(left))
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 5.6: a game that fails is a *PreviewError; a missing image is
// ErrPreviewImageMissing and nothing is downloaded (SPEC 3.7).
func TestResourcesFailureAndMissingImage(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewFails); err != nil {
		t.Fatal(err)
	}
	var pe *PreviewError
	if _, err := r.c.Resources(context.Background(), resourcesRequest(), "nauvis"); !errors.As(err, &pe) || pe.ExitCode != 1 {
		t.Errorf("error %v, want a *PreviewError with exit 1", err)
	}
	if len(r.previewContainers()) != 0 || len(r.previewFolders()) != 0 {
		t.Error("the container or folder was left")
	}
	r.game.SetImage(image21, false)
	if _, err := r.c.Resources(context.Background(), resourcesRequest(), "nauvis"); !errors.Is(err, ErrPreviewImageMissing) {
		t.Errorf("error %v, want ErrPreviewImageMissing", err)
	}
	if len(r.calls("POST /images/create")) != 0 {
		t.Error("an image was downloaded")
	}
}

// SPEC 14.1: the game's file is untrusted. A missing or too big file is
// refused, and the bytes of one within the limit come back as they are for
// the parser.
func TestReadResourcesRefusesWhatIsNotRight(t *testing.T) {
	base, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	if err := base.Mkdir("p", 0o750); err != nil {
		t.Fatal(err)
	}
	put := func(b []byte) {
		if err := base.WriteFile(filepath.Join("p", gameenv.ResourceFile), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readControlModOutput(base, "p", resourceMod); err == nil || !strings.Contains(err.Error(), "no usable resource file") {
		t.Errorf("a missing file: %v", err)
	}
	good := []byte(`{"planet": "nauvis", "resources": {}, "patches": []}`)
	put(good)
	if b, err := readControlModOutput(base, "p", resourceMod); err != nil || string(b) != string(good) {
		t.Errorf("a good file: %q, %v", b, err)
	}
	put([]byte(strings.Repeat("a", resource.MaxJSON)))
	if _, err := readControlModOutput(base, "p", resourceMod); err != nil {
		t.Errorf("a file of exactly the limit: %v", err)
	}
	put([]byte(strings.Repeat("a", resource.MaxJSON+1)))
	if _, err := readControlModOutput(base, "p", resourceMod); err == nil {
		t.Error("a file over the limit was accepted")
	}
}

// SPEC 14.1: a named pipe where the count's file should be is refused at
// once, so it cannot hold the preview slot.
func TestReadResourcesRefusesANamedPipe(t *testing.T) {
	base := fifoAt(t, gameenv.ResourceFile)
	err := failsPromptly(t, func() error {
		_, err := readControlModOutput(base, "p", resourceMod)
		return err
	})
	if err == nil {
		t.Error("a named pipe was accepted")
	}
}
