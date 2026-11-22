package fakegame_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// runCount makes each run's container name new: a fake keeps its containers.
var runCount atomic.Int64

type seedOut struct {
	Format      int                              `json:"format"`
	GameVersion string                           `json:"game_version"`
	Tick        int64                            `json:"tick"`
	ActiveMods  []struct{ Name, Version string } `json:"active_mods"`
	Players     []fakegame.SavePlayer            `json:"players"`
}

// savePlayersRun does what Foreman's copy run does for the fake: a work
// folder with a mod list and the mods the list names, the save bound
// read-only, then a preview container of the players kind. It returns the
// finished container and the work folder.
func savePlayersRun(t *testing.T, r *rig, image string, list string, mods []string, withSave bool) (docker.Container, string) {
	t.Helper()
	work := t.TempDir()
	modsDir := filepath.Join(work, world.ModsDir)
	if err := os.MkdirAll(modsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modsDir, world.ModListFile), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, m := range mods {
		if err := os.WriteFile(filepath.Join(modsDir, m), []byte("zip"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	binds := []string{work + ":" + gameenv.PreviewMount}
	if withSave {
		save := filepath.Join(t.TempDir(), "save.zip")
		if err := os.WriteFile(save, []byte("save"), 0o600); err != nil {
			t.Fatal(err)
		}
		binds = append(binds, save+":"+gameenv.SaveCopyMount+":ro")
	}
	ctx := context.Background()
	id, err := r.docker.CreateContainer(ctx, fmt.Sprintf("foreman-saveplayers-%d", runCount.Add(1)), docker.ContainerSpec{
		Image:      image,
		Labels:     map[string]string{gameenv.LabelPreview: "/data"},
		Env:        []string{gameenv.PreviewKind + "=" + gameenv.KindSavePlayers},
		HostConfig: docker.HostConfig{Binds: binds, NetworkMode: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	return r.inspect(id), work
}

func readSeed(t *testing.T, work string) seedOut {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(work, gameenv.SavePlayersFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	var out seedOut
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // the contract: unknown fields are refused (SPEC 14.6)
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("the output is not the helper's format: %v\n%s", err, b)
	}
	return out
}

func modNames(o seedOut) []string {
	var names []string
	for _, m := range o.ActiveMods {
		names = append(names, m.Name+"@"+m.Version)
	}
	return names
}

const listOf2 = `{"mods":[{"name":"base","enabled":true},{"name":"quality","enabled":true},{"name":"space-age","enabled":false},` +
	`{"name":"foreman-saveplayers","enabled":true},{"name":"alpha","enabled":true},{"name":"gone","enabled":true}]}`

// A read writes the helper's format 1 with the canned players, the game's
// version from the image, and active_mods as the game would load them: the
// built-in mods at the game's version, mods with files at the version in
// the file name (the newest of several), and the helper. A mod with no file
// is dropped silently, as the real game does, and a disabled one is absent.
func TestSavePlayersRunWritesTheHelpersOutput(t *testing.T) {
	fakegame.ResetSavePlayers()
	r := newRig(t, fakegame.Config{})
	c, work := savePlayersRun(t, r, image21, listOf2, []string{"alpha_1.2.3.zip", "alpha_1.10.0.zip", "foreman-saveplayers_1.0.0"}, true)
	if c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	out := readSeed(t, work)
	def := fakegame.DefaultSavePlayers()
	if out.Format != 1 || out.GameVersion != "2.1.24" || out.Tick != def.Tick {
		t.Errorf("format %d, version %q, tick %d", out.Format, out.GameVersion, out.Tick)
	}
	if !slices.Equal(out.Players, def.Players) {
		t.Errorf("players %v, want %v", out.Players, def.Players)
	}
	want := []string{"base@2.1.24", "quality@2.1.24", "foreman-saveplayers@1.0.0", "alpha@1.10.0"}
	if got := modNames(out); !slices.Equal(got, want) {
		t.Errorf("active mods %v, want %v", got, want)
	}
}

// The configured output replaces the canned one, drop_mod removes a mod from
// active_mods (the mods-differ case), and reset restores the canned output.
func TestSavePlayersRunFollowsTheConfiguration(t *testing.T) {
	t.Cleanup(fakegame.ResetSavePlayers)
	fakegame.SetSavePlayers(fakegame.SavePlayersOutput{
		Tick:    123,
		Players: []fakegame.SavePlayer{{Name: "carol", OnlineTicks: 7}},
		DropMod: "alpha",
	})
	r := newRig(t, fakegame.Config{})
	_, work := savePlayersRun(t, r, image21, listOf2, []string{"alpha_1.2.3.zip"}, true)
	out := readSeed(t, work)
	if out.Tick != 123 || !slices.Equal(out.Players, []fakegame.SavePlayer{{Name: "carol", OnlineTicks: 7}}) {
		t.Errorf("tick %d players %v", out.Tick, out.Players)
	}
	if got := modNames(out); slices.Contains(got, "alpha@1.2.3") {
		t.Errorf("active mods %v still hold the dropped mod", got)
	}

	fakegame.ResetSavePlayers()
	_, work = savePlayersRun(t, r, image21, listOf2, nil, true)
	if out := readSeed(t, work); out.Tick != fakegame.DefaultSavePlayers().Tick {
		t.Errorf("after reset the tick is %d", out.Tick)
	}
}

// No players is an empty list, not null; raw bytes go out as they are.
func TestSavePlayersRunEmptyAndRaw(t *testing.T) {
	t.Cleanup(fakegame.ResetSavePlayers)
	r := newRig(t, fakegame.Config{})
	fakegame.SetSavePlayers(fakegame.SavePlayersOutput{Tick: 5})
	_, work := savePlayersRun(t, r, image21, listOf2, nil, true)
	b, _ := os.ReadFile(filepath.Join(work, gameenv.SavePlayersFile)) //nolint:gosec // a test's own temp folder
	if !bytes.Contains(b, []byte(`"players":[]`)) {
		t.Errorf("no players is not an empty list: %s", b)
	}
	fakegame.SetSavePlayers(fakegame.SavePlayersOutput{Raw: "not json"})
	_, work = savePlayersRun(t, r, image21, listOf2, nil, true)
	if b, _ := os.ReadFile(filepath.Join(work, gameenv.SavePlayersFile)); string(b) != "not json" { //nolint:gosec // a test's own temp folder
		t.Errorf("raw output was %q", b)
	}
}

// NoOutput ends the run with 0 and no file.
func TestSavePlayersRunNoOutput(t *testing.T) {
	t.Cleanup(fakegame.ResetSavePlayers)
	fakegame.SetSavePlayers(fakegame.SavePlayersOutput{NoOutput: true})
	r := newRig(t, fakegame.Config{})
	c, work := savePlayersRun(t, r, image21, listOf2, nil, true)
	if c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	if _, err := os.Stat(filepath.Join(work, gameenv.SavePlayersFile)); err == nil {
		t.Error("a no-output run left a file")
	}
	h := r.game.HookHandler()
	if w := hook(t, h, http.MethodPost, "/save-players", `{"no_output":true}`); w.Code != http.StatusNoContent {
		t.Errorf("the hook answered %d", w.Code)
	}
}

// The preview modes still end a save read: a failure leaves no file.
func TestSavePlayersRunHonoursPreviewMode(t *testing.T) {
	fakegame.ResetSavePlayers()
	r := newRig(t, fakegame.Config{})
	if err := r.game.SetPreviewMode(fakegame.PreviewOOM); err != nil {
		t.Fatal(err)
	}
	c, work := savePlayersRun(t, r, image21, listOf2, nil, true)
	if c.State.ExitCode != 137 || !c.State.OOMKilled {
		t.Errorf("state %+v, want killed for memory", c.State)
	}
	if _, err := os.Stat(filepath.Join(work, gameenv.SavePlayersFile)); err == nil {
		t.Error("a killed read left a file")
	}
}

// A read with no save bound at the save mount fails with 1: the fake checks
// that Foreman set the run up as SPEC 8.16 says, and reports the misuse. It
// uses Start, because New would fail the test on that report.
func TestSavePlayersRunNeedsTheSave(t *testing.T) {
	fakegame.ResetSavePlayers()
	g, err := fakegame.Start(fakegame.Config{Images: []string{image21}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	c, err := docker.New(g.Host())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, game: g, docker: c}
	ct, work := savePlayersRun(t, r, image21, listOf2, nil, false)
	if ct.State.ExitCode != 1 {
		t.Errorf("state %+v, want exit 1", ct.State)
	}
	if _, err := os.Stat(filepath.Join(work, gameenv.SavePlayersFile)); err == nil {
		t.Error("a read without a save left a file")
	}
}

// The hook sets the output over HTTP and reset restores it; an unknown field
// is refused.
func TestSavePlayersHook(t *testing.T) {
	t.Cleanup(fakegame.ResetSavePlayers)
	r := newRig(t, fakegame.Config{})
	h := r.game.HookHandler()
	body := `{"tick":99,"players":[{"name":"dave","online_ticks":3}],"drop_mod":"alpha"}`
	if w := hook(t, h, http.MethodPost, "/save-players", body); w.Code != http.StatusNoContent {
		t.Fatalf("set answered %d: %s", w.Code, w.Body)
	}
	_, work := savePlayersRun(t, r, image21, listOf2, nil, true)
	if out := readSeed(t, work); out.Tick != 99 || len(out.Players) != 1 || out.Players[0].Name != "dave" {
		t.Errorf("the hook's output was not used: %+v", out)
	}
	if w := hook(t, h, http.MethodPost, "/save-players", `{"reset":true}`); w.Code != http.StatusNoContent {
		t.Fatalf("reset answered %d", w.Code)
	}
	_, work = savePlayersRun(t, r, image21, listOf2, nil, true)
	if out := readSeed(t, work); out.Tick != fakegame.DefaultSavePlayers().Tick {
		t.Errorf("after reset the tick is %d", out.Tick)
	}
	if w := hook(t, h, http.MethodPost, "/save-players", `{"nonsense":1}`); w.Code != http.StatusBadRequest {
		t.Errorf("an unknown field answered %d", w.Code)
	}
}
