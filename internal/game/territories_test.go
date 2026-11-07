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
	"github.com/toddawhittaker/foreman/internal/game/territories"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/territory"
)

func territoriesRequest() PreviewRequest {
	return PreviewRequest{GameVersion: "2.1.24", SpaceAge: true, Seed: 7}
}

// SPEC 5.6: a territory run reads the territories the game's script wrote,
// clipped to the picture; the container and the work folder are gone after.
func TestTerritoriesReadsTheGamesFile(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	res, err := r.c.Territories(context.Background(), territoriesRequest())
	if err != nil {
		t.Fatal(err)
	}
	// The fake game writes a small, a medium and a big territory in three corners.
	var sizes []string
	for _, tt := range res.Territories {
		sizes = append(sizes, tt.Size)
	}
	if len(sizes) != 3 || !slices.Contains(sizes, "small") || !slices.Contains(sizes, "medium") || !slices.Contains(sizes, "big") {
		t.Errorf("territories %v", sizes)
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d containers left", len(left))
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 5.6: the radius reaches the game as half the picture in chunks.
func TestTerritoriesRadiusFollowsTheSize(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	for size, want := range map[int]string{0: "20", 1024: "16", 3072: "48"} {
		req := territoriesRequest()
		req.Size = size
		env := r.c.territoriesSpec(image21, "p", req).Env
		if w := gameenv.TerritoryRadius + "=" + want; !slices.Contains(env, w) {
			t.Errorf("size %d: environment %v lacks %s", size, env, w)
		}
	}
}

// SPEC 5.6, 14.3: the work folder holds the config, a mod list that enables
// the territory mod beside Space Age, the mod for the game's own line, and
// the settings files a preview gets; the container is a labelled preview
// container with the territory environment.
func TestTerritoriesWorkFolderAndContainer(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	req := territoriesRequest()
	req.Preset = "island"
	req.Seed = 123456
	req.MapGenSettings = []byte(`{"width": 500}`)
	req.MapSettings = []byte(`{"pollution": {}}`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.c.Territories(ctx, req)
		done <- err
	}()
	c := r.waitPreviewContainer()
	folder, ok := strings.CutPrefix(c.Name, "foreman-territories-")
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
	if !enabled["base"] || !enabled["space-age"] || !enabled[territories.ModName] {
		t.Errorf("enabled mods %v", enabled)
	}
	if string(read("mods/"+territories.ModDir+"/info.json")) != string(territories.InfoJSON("2.1")) ||
		string(read("mods/"+territories.ModDir+"/control.lua")) != string(territories.Control) {
		t.Error("the mod folder is not info.json for 2.1 and control.lua")
	}
	if string(read("map-gen-settings.json")) != `{"width": 500}` || string(read("map-settings.json")) != `{"pollution": {}}` ||
		string(read("config.ini")) != previewConfig {
		t.Error("config or settings files differ from the preview's")
	}
	for _, want := range []string{
		gameenv.PreviewKind + "=territories", gameenv.TerritoryRadius + "=20",
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
}

// SPEC 14.3: the territory container has the preview container's limits, and
// no value of the request is built into its script.
func TestTerritoriesSpecHasThePreviewsLimits(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	req := territoriesRequest()
	req.Preset = "death-world"
	got := r.c.territoriesSpec(image21, "p-abc", req)
	want := r.c.previewSpec(image21, "p-abc", "2.1", previewRequest())
	if !reflect.DeepEqual(got.HostConfig, want.HostConfig) {
		t.Errorf("host config %+v, want the preview's %+v", got.HostConfig, want.HostConfig)
	}
	if got.User != want.User || got.Image != want.Image || !reflect.DeepEqual(got.Labels, want.Labels) ||
		!reflect.DeepEqual(got.Entrypoint, want.Entrypoint) {
		t.Errorf("container %+v differs from the preview's %+v", got, want)
	}
	if strings.Contains(strings.Join(got.Cmd, " "), "death-world") {
		t.Error("a value is built into the script")
	}
	for _, need := range []string{
		`--create /out/territories.zip`, `--map-gen-seed "$` + gameenv.Seed + `"`,
		`--preset "$` + gameenv.Preset + `"`, `--map-gen-settings`, `--map-settings`,
		`cp /tmp/factorio/script-output/territories.json /out/territories.json`,
		`> /out/mods/foreman-territories_1.0.0/radius.lua`,
	} {
		if !strings.Contains(territoriesScript, need) {
			t.Errorf("the script lacks %s:\n%s", need, territoriesScript)
		}
	}
	if strings.Contains(territoriesScript, "--start-server") {
		t.Error("the territory run starts a server")
	}
}

// SPEC 5.6: territories exist only on Vulcanus, with Space Age, up to
// territory.MaxSize; anything else is refused before Docker is called.
func TestTerritoriesRefusesWhatHasNone(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	cases := map[string]func(*PreviewRequest){
		"without Space Age":      func(q *PreviewRequest) { q.SpaceAge = false },
		"on 1.1":                 func(q *PreviewRequest) { q.GameVersion = "1.1.110" },
		"on a line, not a patch": func(q *PreviewRequest) { q.GameVersion = "2.1" },
		"above the most":         func(q *PreviewRequest) { q.Size = territory.MaxSize + PreviewSizeStep },
		"at the preview's most":  func(q *PreviewRequest) { q.Size = PreviewSizeMax },
		"off the steps":          func(q *PreviewRequest) { q.Size = 1100 },
		"an unknown preset":      func(q *PreviewRequest) { q.Preset = "x; touch y" },
		"settings not JSON":      func(q *PreviewRequest) { q.MapGenSettings = []byte(`{"a":`) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := territoriesRequest()
			mutate(&req)
			if _, err := r.c.Territories(context.Background(), req); !errors.Is(err, ErrPreviewInvalid) {
				t.Errorf("error %v, want ErrPreviewInvalid", err)
			}
		})
	}
	if len(r.game.State().Calls) != 0 {
		t.Errorf("Docker was called: %v", r.game.State().Calls)
	}
	// The largest size offered is read.
	req := territoriesRequest()
	req.Size = territory.MaxSize
	if _, err := r.c.Territories(context.Background(), req); err != nil {
		t.Errorf("size %d: %v", territory.MaxSize, err)
	}
}

// SPEC 14.3: a territory run and a map preview share the one slot, in both
// orders, and each frees it when it ends.
func TestTerritoriesShareThePreviewSlot(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.previewWait = 100 * time.Millisecond
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() {
		_, err := r.c.Territories(ctx, territoriesRequest())
		first <- err
	}()
	r.waitPreviewContainer()
	if _, err := r.c.Preview(context.Background(), previewRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("preview during a territory run: %v, want ErrPreviewBusy", err)
	}
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("second territory run: %v, want ErrPreviewBusy", err)
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
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); err != nil {
		t.Errorf("the slot was not freed: %v", err)
	}

	// And the other way: a hanging map preview holds it against a territory run.
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		_, err := r.c.Preview(ctx2, previewRequest())
		second <- err
	}()
	r.waitPreviewContainer()
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("territory run during a preview: %v, want ErrPreviewBusy", err)
	}
	cancel2()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Errorf("preview: %v, want context.Canceled", err)
	}
}

// SPEC 14.3: a caller who goes away mid-run stops the container and removes
// it and the folder; a run past the limit is ErrPreviewTimeout.
func TestTerritoriesCancelAndTimeoutCleanUp(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.c.Territories(ctx, territoriesRequest())
		done <- err
	}()
	r.waitPreviewContainer()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	r.c.previewLimit = 300 * time.Millisecond
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); !errors.Is(err, ErrPreviewTimeout) {
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
func TestTerritoriesFailureAndMissingImage(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewFails); err != nil {
		t.Fatal(err)
	}
	var pe *PreviewError
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); !errors.As(err, &pe) || pe.ExitCode != 1 {
		t.Errorf("error %v, want a *PreviewError with exit 1", err)
	}
	if len(r.previewContainers()) != 0 || len(r.previewFolders()) != 0 {
		t.Error("the container or folder was left")
	}
	r.game.SetImage(image21, false)
	if _, err := r.c.Territories(context.Background(), territoriesRequest()); !errors.Is(err, ErrPreviewImageMissing) {
		t.Errorf("error %v, want ErrPreviewImageMissing", err)
	}
	if len(r.calls("POST /images/create")) != 0 {
		t.Error("an image was downloaded")
	}
}

// SPEC 14.1: the game's file is untrusted. A missing, malformed or too big
// file is refused.
func TestReadTerritoriesRefusesWhatIsNotRight(t *testing.T) {
	base, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	if err := base.Mkdir("p", 0o750); err != nil {
		t.Fatal(err)
	}
	put := func(b []byte) {
		if err := base.WriteFile(filepath.Join("p", gameenv.TerritoryFile), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readTerritories(base, "p", 1280); err == nil {
		t.Error("a missing file was accepted")
	}
	put([]byte(`{"territories": [{"unit": "small-demolisher", "chunks": [[0, 0]]}]}`))
	if res, err := readTerritories(base, "p", 1280); err != nil || len(res.Territories) != 1 {
		t.Errorf("a good file: %v, %v", res, err)
	}
	// A territory wholly outside the picture is dropped.
	put([]byte(`{"territories": [{"unit": "small-demolisher", "chunks": [[500, 500]]}]}`))
	if res, err := readTerritories(base, "p", 1280); err != nil || len(res.Territories) != 0 {
		t.Errorf("an outside territory: %v, %v", res, err)
	}
	for name, b := range map[string][]byte{
		"not JSON":       []byte(`{"territories": [`),
		"unknown unit":   []byte(`{"territories": [{"unit": "worm", "chunks": [[0, 0]]}]}`),
		"unknown field":  []byte(`{"territories": [], "x": 1}`),
		"too big":        []byte(`{"territories": [],"pad":"` + strings.Repeat("a", territory.MaxJSON) + `"}`),
		"trailing bytes": []byte(`{"territories": []} {}`),
	} {
		put(b)
		if _, err := readTerritories(base, "p", 1280); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
