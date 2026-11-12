package game

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	modsettingsmod "github.com/toddawhittaker/foreman/internal/game/modsettings"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// defsJSON is a definitions file as the control mod writes it.
const defsJSON = `{"settings": [
  {"name": "fm-sta-int", "mod": "fmtest", "setting_type": "startup", "type": "int-setting", "order": "b",
   "default_value": 5, "minimum_value": 1, "maximum_value": 10, "allowed_values": [1, 5],
   "hidden": false, "allow_blank": false, "auto_trim": false},
  {"name": "fm-run-bool", "mod": "fmtest", "setting_type": "runtime-global", "type": "bool-setting", "order": "a",
   "default_value": true}
]}`

// modSettingsRig is a rig whose preview containers stay up until the test
// ends them, and a world to read the settings of.
type modSettingsRig struct {
	*rig
	id string
}

func newModSettingsRig(t *testing.T) *modSettingsRig {
	t.Helper()
	r := newRig(t, rigOptions{noRun: true})
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	return &modSettingsRig{rig: r, id: r.newWorld("Mods")}
}

// defaultsDat is a real mod-settings.dat the game wrote.
func defaultsDat(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "modsettings", "testdata", "game-2.1.24.dat"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// waitRunning waits for the run's container to be started. The fake takes an
// exit only from a container that runs.
func (r *modSettingsRig) waitRunning() dockertest.Container {
	r.t.Helper()
	var found dockertest.Container
	eventually(r.t, func() bool {
		for _, c := range r.previewContainers() {
			if c.Status == "running" {
				found = c
				return true
			}
		}
		return false
	}, "a running preview container")
	return found
}

// workDir is the container's work folder on this machine.
func (r *modSettingsRig) workDir(c dockertest.Container) string {
	r.t.Helper()
	folder, ok := strings.CutPrefix(c.Name, "foreman-"+gameenv.KindModSettings+"-")
	if !ok {
		r.t.Fatalf("container name %q", c.Name)
	}
	return filepath.Join(r.data, previewDir, folder)
}

// finish writes the files a good run leaves and ends the container.
func (r *modSettingsRig) finish(c dockertest.Container, defs string, dat []byte) {
	r.t.Helper()
	dir := r.workDir(c)
	if err := os.WriteFile(filepath.Join(dir, gameenv.ModSettingsDefsFile), []byte(defs), 0o600); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, world.ModsDir, world.ModSettingsFile), dat, 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.game.Daemon().Exit(c.ID, 0, false)
}

type defsResult struct {
	defs *ModSettingDefs
	err  error
}

// ask calls ModSettingDefs in the background.
func (r *modSettingsRig) ask(ctx context.Context) <-chan defsResult {
	ch := make(chan defsResult, 1)
	go func() {
		d, err := r.c.ModSettingDefs(ctx, r.id)
		ch <- defsResult{d, err}
	}()
	return ch
}

func (r *modSettingsRig) result(ch <-chan defsResult) defsResult {
	r.t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(10 * time.Second):
		r.t.Fatal("ModSettingDefs did not return")
		return defsResult{}
	}
}

// run asks once and ends the container well.
func (r *modSettingsRig) run() *ModSettingDefs {
	r.t.Helper()
	ch := r.ask(context.Background())
	r.finish(r.waitRunning(), defsJSON, defaultsDat(r.t))
	res := r.result(ch)
	if res.err != nil {
		r.t.Fatal(res.err)
	}
	eventually(r.t, func() bool { return len(r.previewContainers()) == 0 }, "the container to be removed")
	return res.defs
}

func (r *modSettingsRig) creates() int {
	return len(r.calls("POST /containers/create"))
}

// changeMods makes the world's mod list differ, as a mod upload does.
func (r *modSettingsRig) changeMods(entry string) {
	r.t.Helper()
	p := filepath.Join(r.worldDir(r.id), world.ModsDir, world.ModListFile)
	b, err := os.ReadFile(p) //nolint:gosec // the test's own world folder
	if errors.Is(err, os.ErrNotExist) {
		b, err = []byte(`{"mods":[]}`), nil
	}
	if err != nil {
		r.t.Fatal(err)
	}
	var list map[string]json.RawMessage
	if err := json.Unmarshal(b, &list); err != nil {
		r.t.Fatal(err)
	}
	var mods []json.RawMessage
	if err := json.Unmarshal(list["mods"], &mods); err != nil {
		r.t.Fatal(err)
	}
	list["mods"], _ = json.Marshal(append(mods, json.RawMessage(entry)))
	out, _ := json.Marshal(list)
	if err := os.WriteFile(p, out, 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// SPEC 9.6, 14.3: the run is a labelled preview container of kind
// modsettings from the world's own image, with the preview's lockdown and
// the definitions run's own limits (SPEC 14.3), and no value built into it.
func TestModSettingDefsContainer(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	defer r.finish(c, defsJSON, defaultsDat(t))

	if !strings.HasPrefix(c.Name, "foreman-modsettings-p-") {
		t.Errorf("container name %q", c.Name)
	}
	if c.Spec.Image != image21 {
		t.Errorf("image %q, want the world's %q", c.Spec.Image, image21)
	}
	if !reflect.DeepEqual(c.Spec.Labels, r.c.previewSpec(image21, "x", "2.1", previewRequest()).Labels) {
		t.Errorf("labels %v", c.Spec.Labels)
	}
	hc := c.Spec.HostConfig
	want := r.c.oneOffHostConfig(strings.TrimPrefix(c.Name, "foreman-modsettings-"))
	want.Memory, want.MemorySwap = 4<<30, 4<<30
	if !reflect.DeepEqual(hc, want) {
		t.Errorf("host config %+v, want %+v", hc, want)
	}
	if ModSettingsMemory != 4<<30 || ModSettingsLimit != 5*time.Minute {
		t.Errorf("limits %d bytes, %v", ModSettingsMemory, ModSettingsLimit)
	}
	if r.c.modSettingsLimit != ModSettingsLimit {
		t.Errorf("the controller's limit is %v", r.c.modSettingsLimit)
	}
	found := false
	for _, e := range c.Spec.Env {
		if e == gameenv.PreviewKind+"="+gameenv.KindModSettings {
			found = true
		}
	}
	if !found {
		t.Errorf("environment %v lacks the kind", c.Spec.Env)
	}
	script := strings.Join(c.Spec.Cmd, " ")
	for _, need := range []string{
		`--create /out/modsettings.zip`, `--mod-directory /out/mods`, `--config /out/config.ini`,
		`cp /tmp/factorio/script-output/mod-settings-defs.json /out/mod-settings-defs.json`,
	} {
		if !strings.Contains(script, need) {
			t.Errorf("the script lacks %s:\n%s", need, script)
		}
	}
	if strings.Contains(script, "--start-server") {
		t.Error("the run starts a server")
	}
	_ = ch
}

// SPEC 9.6: the work folder holds the config, the world's own mod list with
// the reader enabled beside its mods and entries kept as they were, and the
// reader's mod for the game's line. It never holds a mod-settings.dat.
func TestModSettingDefsWorkFolder(t *testing.T) {
	r := newModSettingsRig(t)
	r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModListFile),
		[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"fmtest","enabled":true,"version":"1.0.0"},{"name":"off","enabled":false}]}`))
	if err := os.WriteFile(filepath.Join(r.worldDir(r.id), world.ModsDir, "mod-settings.dat"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := r.ask(context.Background())
	c := r.waitRunning()
	dir := r.workDir(c)
	read := func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, rel)) //nolint:gosec // the test's own work folder
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var list struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(read("mods/mod-list.json"), &list); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, m := range list.Mods {
		got[m["name"].(string)] = m
	}
	if len(list.Mods) != 4 || got["fmtest"]["version"] != "1.0.0" || got["off"]["enabled"] != false ||
		got[modsettingsmod.ModName]["enabled"] != true || got["base"]["enabled"] != true {
		t.Errorf("mod list %v", list.Mods)
	}
	if string(read("mods/"+modsettingsmod.ModDir+"/info.json")) != string(modSettingsInfo("2.1")) ||
		string(read("mods/"+modsettingsmod.ModDir+"/control.lua")) != string(modsettingsmod.Control) {
		t.Error("the mod folder is not info.json for 2.1 and control.lua")
	}
	if string(read("config.ini")) != previewConfig {
		t.Error("config.ini differs from the preview's")
	}
	if _, err := os.Lstat(filepath.Join(dir, world.ModsDir, "mod-settings.dat")); err == nil {
		t.Error("the world's mod-settings.dat was put in the work folder")
	}
	r.finish(c, defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil {
		t.Fatal(res.err)
	}
}

// SPEC 9.6: a run returns the definitions, the defaults file as the game
// wrote it, and leaves no container or folder.
func TestModSettingDefsReadsTheGamesFiles(t *testing.T) {
	r := newModSettingsRig(t)
	got := r.run()
	if len(got.Defs) != 2 || got.Defs[0].Name != "fm-sta-int" || got.Defs[1].Name != "fm-run-bool" {
		t.Errorf("definitions %+v, want the file's two in its order", got.Defs)
	}
	if string(got.Defaults) != string(defaultsDat(t)) {
		t.Error("the defaults differ from the game's file")
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 14.6: the result is kept; asking again does not run the game, and a
// change of the mods does.
func TestModSettingDefsCaches(t *testing.T) {
	r := newModSettingsRig(t)
	first := r.run()
	again, err := r.c.ModSettingDefs(context.Background(), r.id)
	if err != nil || again != first || r.creates() != 1 {
		t.Fatalf("second call: %v, same %v, %d runs", err, again == first, r.creates())
	}
	r.changeMods(`{"name":"fmtest","enabled":true}`)
	r.run()
	if r.creates() != 2 {
		t.Errorf("%d runs after a change of mods, want 2", r.creates())
	}
	if _, err := r.c.ModSettingDefs(context.Background(), r.id); err != nil || r.creates() != 2 {
		t.Errorf("the new result was not kept: %v, %d runs", err, r.creates())
	}
}

// SPEC 14.6: another world has its own entry.
func TestModSettingDefsCachesPerWorld(t *testing.T) {
	r := newModSettingsRig(t)
	r.run()
	other := &modSettingsRig{rig: r.rig, id: r.newWorld("Other")}
	other.run()
	for _, id := range []string{r.id, other.id} {
		if _, err := r.c.ModSettingDefs(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if r.creates() != 2 {
		t.Errorf("%d runs for two worlds, want 2", r.creates())
	}
}

// SPEC 14.6: a key that changed during the run is a result for mods that are
// gone, so it is returned but not kept.
func TestModSettingDefsNotCachedWhenKeyChangesDuringRun(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	r.changeMods(`{"name":"late","enabled":true}`)
	r.finish(c, defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil || res.defs == nil {
		t.Fatalf("the run: %v", res.err)
	}
	eventually(t, func() bool { return len(r.previewContainers()) == 0 }, "the container to be removed")
	r.run()
	if r.creates() != 2 {
		t.Errorf("%d runs, want the second call to run again", r.creates())
	}
}

// SPEC 14.6: callers who ask during a run share it; one who comes after it hits
// the cache. Either way the game runs once.
func TestModSettingDefsCallersShareARun(t *testing.T) {
	r := newModSettingsRig(t)
	const n = 4
	var wg sync.WaitGroup
	results := make([]defsResult, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := r.c.ModSettingDefs(context.Background(), r.id)
			results[i] = defsResult{d, err}
		}()
	}
	c := r.waitRunning()
	time.Sleep(100 * time.Millisecond)
	r.finish(c, defsJSON, defaultsDat(t))
	wg.Wait()
	for i, res := range results {
		if res.err != nil || res.defs != results[0].defs {
			t.Errorf("caller %d: %v, shared %v", i, res.err, res.defs == results[0].defs)
		}
	}
	if r.creates() != 1 {
		t.Errorf("%d runs for %d callers, want 1", r.creates(), n)
	}
}

// Callers who go away get their own context's error, and the run goes on to
// its end and is cached, so a reload during a long run finds the result.
func TestModSettingDefsRunOutlivesItsCallers(t *testing.T) {
	r := newModSettingsRig(t)
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	first, second := r.ask(ctx1), r.ask(ctx2)
	c := r.waitRunning()
	cancel1()
	if res := r.result(first); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("first caller: %v, want context.Canceled", res.err)
	}
	cancel2()
	if res := r.result(second); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("second caller: %v, want context.Canceled", res.err)
	}
	if len(r.previewContainers()) != 1 {
		t.Fatal("the run ended when its callers left")
	}
	r.finish(c, defsJSON, defaultsDat(t))
	eventually(t, func() bool {
		r.c.modSettings.mu.Lock()
		defer r.c.modSettings.mu.Unlock()
		return len(r.c.modSettings.entries) == 1
	}, "the result to be cached")
	if _, err := r.c.ModSettingDefs(context.Background(), r.id); err != nil || r.creates() != 1 {
		t.Errorf("a call after the run: %v, %d runs, want the cache and 1 run", err, r.creates())
	}
	eventually(t, func() bool { return len(r.previewContainers()) == 0 && len(r.previewFolders()) == 0 }, "the container and folder to go")
}

// SPEC 9.6: each way the run fails is one of the page's errors, with
// nothing left behind.
func TestModSettingDefsErrors(t *testing.T) {
	good := defaultsDat(t)
	var pe *PreviewError
	cases := []struct {
		name string
		run  func(r *modSettingsRig, c dockertest.Container)
		want error
		more func(t *testing.T, err error)
	}{
		{"out of memory", func(r *modSettingsRig, c dockertest.Container) { r.game.Daemon().Exit(c.ID, 137, true) },
			ErrModSettingsOutOfMemory, func(t *testing.T, err error) {
				if !errors.As(err, &pe) || !pe.OOM {
					t.Errorf("%v is not an OOM *PreviewError", err)
				}
			}},
		{"the game fails", func(r *modSettingsRig, c dockertest.Container) { r.game.Daemon().Exit(c.ID, 1, false) },
			ErrModSettingsBadOutput, func(t *testing.T, err error) {
				if !errors.As(err, &pe) || pe.ExitCode != 1 {
					t.Errorf("%v does not carry the exit", err)
				}
			}},
		{"no files", func(r *modSettingsRig, c dockertest.Container) { r.game.Daemon().Exit(c.ID, 0, false) },
			ErrModSettingsBadOutput, nil},
		{"definitions that are not JSON", func(r *modSettingsRig, c dockertest.Container) { r.finish(c, `{"settings":`, good) },
			ErrModSettingsBadOutput, nil},
		{"a defaults file that is not one", func(r *modSettingsRig, c dockertest.Container) { r.finish(c, defsJSON, []byte("nope")) },
			ErrModSettingsBadOutput, nil},
		{"a defaults file that is empty", func(r *modSettingsRig, c dockertest.Container) { r.finish(c, defsJSON, nil) },
			ErrModSettingsBadOutput, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newModSettingsRig(t)
			ch := r.ask(context.Background())
			tc.run(r, r.waitRunning())
			res := r.result(ch)
			if !errors.Is(res.err, tc.want) || res.defs != nil {
				t.Fatalf("got %v, %v; want %v", res.defs, res.err, tc.want)
			}
			if tc.more != nil {
				tc.more(t, res.err)
			}
			eventually(t, func() bool { return len(r.previewContainers()) == 0 && len(r.previewFolders()) == 0 }, "the container and folder to go")
			// A failure is not kept.
			ch = r.ask(context.Background())
			r.finish(r.waitRunning(), defsJSON, good)
			if res := r.result(ch); res.err != nil {
				t.Errorf("the next call: %v", res.err)
			}
		})
	}
}

// SPEC 14.3: a run past its limit is killed and is ErrModSettingsTimeout.
func TestModSettingDefsTimeout(t *testing.T) {
	r := newModSettingsRig(t)
	r.c.modSettingsLimit = 300 * time.Millisecond
	_, err := r.c.ModSettingDefs(context.Background(), r.id)
	if !errors.Is(err, ErrModSettingsTimeout) {
		t.Fatalf("error %v, want ErrModSettingsTimeout", err)
	}
	if len(r.previewContainers()) != 0 || len(r.previewFolders()) != 0 {
		t.Error("the container or folder was left")
	}
}

// SPEC 3.7: a missing image is its own error and nothing is downloaded.
func TestModSettingDefsMissingImage(t *testing.T) {
	r := newModSettingsRig(t)
	r.game.SetImage(image21, false)
	if _, err := r.c.ModSettingDefs(context.Background(), r.id); !errors.Is(err, ErrModSettingsImageMissing) {
		t.Errorf("error %v, want ErrModSettingsImageMissing", err)
	}
	if len(r.calls("POST /images/create")) != 0 {
		t.Error("an image was downloaded")
	}
	if len(r.previewFolders()) != 0 {
		t.Error("a work folder was left")
	}
}

// SPEC 14.3: the run takes the one preview slot, so a preview waits and a
// definitions run behind a preview is ErrPreviewBusy.
func TestModSettingDefsShareThePreviewSlot(t *testing.T) {
	r := newModSettingsRig(t)
	r.c.previewWait = 100 * time.Millisecond
	ch := r.ask(context.Background())
	c := r.waitRunning()
	if _, err := r.c.Preview(context.Background(), previewRequest()); !errors.Is(err, ErrPreviewBusy) {
		t.Errorf("preview during the run: %v, want ErrPreviewBusy", err)
	}
	r.finish(c, defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil {
		t.Fatal(res.err)
	}
}

// The world is unknown: no Docker call.
func TestModSettingDefsUnknownWorld(t *testing.T) {
	r := newModSettingsRig(t)
	if _, err := r.c.ModSettingDefs(context.Background(), "nope"); err == nil {
		t.Error("an unknown world was read")
	}
	if len(r.game.State().Calls) != 0 {
		t.Errorf("Docker was called: %v", r.game.State().Calls)
	}
}

// SPEC 22.3: text comes from each enabled mod's locale files, per language,
// merged in mod-name order; a language without files has no entry.
func TestModSettingDefsLocale(t *testing.T) {
	r := newModSettingsRig(t)
	put := func(rel, text string) {
		p := filepath.Join(r.worldDir(r.id), world.ModsDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mod := range []string{"fmtest", "zmod"} {
		put(mod+"/info.json", `{"name":"`+mod+`","version":"1.0.0","title":"x","author":"x","factorio_version":"2.1"}`)
	}
	put("fmtest/locale/en/a.cfg", "[mod-setting-name]\nfm-sta-int=Integer\n[mod-setting-description]\nfm-sta-int=An integer\n")
	put("fmtest/locale/zh-CN/a.cfg", "[mod-setting-name]\nfm-sta-int=整数\n")
	put("zmod/locale/en/b.cfg", "[mod-setting-name]\nz-set=Zed\n")
	put("fmtest/locale/ru/bad.cfg", "[mod-setting-name\nbroken")
	r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModListFile),
		[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"fmtest","enabled":true},{"name":"zmod","enabled":true}]}`))
	got := r.run().Locale
	want := modsettings.Locale{
		"en":      {"fm-sta-int": {Name: "Integer", Description: "An integer"}, "z-set": {Name: "Zed"}},
		"zh-Hans": {"fm-sta-int": {Name: "整数"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("locale %+v, want %+v", got, want)
	}
}

// SPEC 14.1, 14.6: the game's files are untrusted. Each must be a regular
// file within its limit; a link or a named pipe is refused at once.
func TestReadModSettingsOutput(t *testing.T) {
	dat := defaultsDat(t)
	newDir := func(t *testing.T) (*os.Root, string) {
		t.Helper()
		dir := t.TempDir()
		base, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = base.Close() })
		if err := os.MkdirAll(filepath.Join(dir, "p", world.ModsDir), 0o750); err != nil {
			t.Fatal(err)
		}
		return base, filepath.Join(dir, "p")
	}
	put := func(t *testing.T, p string, b []byte) {
		t.Helper()
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defsAt := func(p string) string { return filepath.Join(p, gameenv.ModSettingsDefsFile) }
	datAt := func(p string) string { return filepath.Join(p, world.ModsDir, world.ModSettingsFile) }

	t.Run("good", func(t *testing.T) {
		base, p := newDir(t)
		put(t, defsAt(p), []byte(defsJSON))
		put(t, datAt(p), dat)
		d, b, err := readModSettingsOutput(base, "p")
		if err != nil || string(d) != defsJSON || string(b) != string(dat) {
			t.Errorf("%v", err)
		}
	})
	t.Run("the limits exactly", func(t *testing.T) {
		base, p := newDir(t)
		put(t, defsAt(p), []byte(strings.Repeat("a", modsettings.MaxDefinitionsBytes)))
		put(t, datAt(p), []byte(strings.Repeat("a", world.MaxModSettings)))
		if _, _, err := readModSettingsOutput(base, "p"); err != nil {
			t.Error(err)
		}
	})
	for name, f := range map[string]func(t *testing.T, p string){
		"definitions missing": func(t *testing.T, p string) { put(t, datAt(p), dat) },
		"defaults missing":    func(t *testing.T, p string) { put(t, defsAt(p), []byte(defsJSON)) },
		"definitions too big": func(t *testing.T, p string) {
			put(t, defsAt(p), []byte(strings.Repeat("a", modsettings.MaxDefinitionsBytes+1)))
			put(t, datAt(p), dat)
		},
		"defaults too big": func(t *testing.T, p string) {
			put(t, defsAt(p), []byte(defsJSON))
			put(t, datAt(p), []byte(strings.Repeat("a", world.MaxModSettings+1)))
		},
		"definitions a link": func(t *testing.T, p string) {
			put(t, filepath.Join(p, "real.json"), []byte(defsJSON))
			if err := os.Symlink("real.json", defsAt(p)); err != nil {
				t.Fatal(err)
			}
			put(t, datAt(p), dat)
		},
		"defaults a link": func(t *testing.T, p string) {
			put(t, defsAt(p), []byte(defsJSON))
			put(t, filepath.Join(p, "real.dat"), dat)
			if err := os.Symlink("../real.dat", datAt(p)); err != nil {
				t.Fatal(err)
			}
		},
		"definitions a link out of the folder": func(t *testing.T, p string) {
			if err := os.Symlink("/etc/passwd", defsAt(p)); err != nil {
				t.Fatal(err)
			}
			put(t, datAt(p), dat)
		},
		"definitions a named pipe": func(t *testing.T, p string) {
			if err := syscall.Mkfifo(defsAt(p), 0o600); err != nil {
				t.Fatal(err)
			}
			put(t, datAt(p), dat)
		},
		"defaults a named pipe": func(t *testing.T, p string) {
			put(t, defsAt(p), []byte(defsJSON))
			if err := syscall.Mkfifo(datAt(p), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"the mods folder a link": func(t *testing.T, p string) {
			put(t, defsAt(p), []byte(defsJSON))
			if err := os.Remove(filepath.Join(p, world.ModsDir)); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(p, "elsewhere"), 0o750); err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(p, "elsewhere", world.ModSettingsFile), dat)
			if err := os.Symlink("elsewhere", filepath.Join(p, world.ModsDir)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			base, p := newDir(t)
			f(t, p)
			err := failsPromptly(t, func() error {
				_, _, err := readModSettingsOutput(base, "p")
				return err
			})
			if !errors.Is(err, ErrModSettingsBadOutput) {
				t.Errorf("error %v, want ErrModSettingsBadOutput", err)
			}
		})
	}
}

// A work folder's mod list, which makePreviewDir and LinkModsInto write, gets
// the control mod appended with every other entry and field kept.
func TestAddControlModKeepsAnExistingList(t *testing.T) {
	dir := t.TempDir()
	base, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()
	if err := os.MkdirAll(filepath.Join(dir, "p", world.ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	listFile := filepath.Join(dir, "p", world.ModsDir, world.ModListFile)
	existing := `{"mods":[{"name":"base","enabled":false},{"name":"x","enabled":true,"version":"2"}],"extra":1}`
	if err := os.WriteFile(listFile, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addControlMod(base, "p", resourceMod, resourceInfo("2.1", true)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(listFile) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Mods  []map[string]any `json:"mods"`
		Extra int              `json:"extra"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Mods) != 3 || got.Extra != 1 || got.Mods[0]["enabled"] != false || got.Mods[1]["version"] != "2" ||
		got.Mods[2]["name"] != resourceMod.name || got.Mods[2]["enabled"] != true {
		t.Errorf("mod list %s", b)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "p", world.ModsDir, world.PrefixPartial+"*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
	// A list that is not JSON, or one that is missing, stops the run.
	if err := os.WriteFile(listFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := addControlMod(base, "p", resourceMod, nil); err == nil {
		t.Error("a broken mod list was accepted")
	}
	if err := os.Remove(listFile); err != nil {
		t.Fatal(err)
	}
	if err := addControlMod(base, "p", resourceMod, nil); err == nil {
		t.Error("a missing mod list was accepted")
	}
}

// A mod of the control mod's name in the work folder, as a zip, a folder or
// a list entry, is refused before anything is written.
func TestAddControlModRefusesATakenName(t *testing.T) {
	for name, setup := range map[string]func(mods string){
		"a zip":          func(mods string) { writeTestFile(t, filepath.Join(mods, resourceMod.dir+".zip"), "PK") },
		"a zip of other": func(mods string) { writeTestFile(t, filepath.Join(mods, resourceMod.name+"_2.0.0.zip"), "PK") },
		"a folder": func(mods string) {
			if err := os.MkdirAll(filepath.Join(mods, resourceMod.name), 0o750); err != nil {
				t.Fatal(err)
			}
		},
		"a list entry": func(mods string) {
			writeTestFile(t, filepath.Join(mods, world.ModListFile), `{"mods":[{"name":"`+resourceMod.name+`","enabled":false}]}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			base, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = base.Close() }()
			mods := filepath.Join(dir, "p", world.ModsDir)
			if err := os.MkdirAll(mods, 0o750); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(mods, world.ModListFile)); err != nil {
				writeTestFile(t, filepath.Join(mods, world.ModListFile), `{"mods":[]}`)
			}
			setup(mods)
			if err := addControlMod(base, "p", resourceMod, nil); !errors.Is(err, errControlModTaken) {
				t.Errorf("error %v, want errControlModTaken", err)
			}
			if _, err := os.Stat(filepath.Join(mods, resourceMod.dir)); err == nil {
				t.Error("the control mod was written")
			}
		})
	}
}

func writeTestFile(t *testing.T, p, text string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The reader works on every game line and the lua file is the one the
// package embeds.
func TestModSettingsModFiles(t *testing.T) {
	for _, line := range []string{"1.1", "2.0", "2.1"} {
		var info map[string]any
		if err := json.Unmarshal(modSettingsInfo(line), &info); err != nil {
			t.Fatal(err)
		}
		if info["factorio_version"] != line || info["name"] != modsettingsmod.ModName {
			t.Errorf("info for %s: %v", line, info)
		}
		if !reflect.DeepEqual(info["dependencies"], []any{"base"}) {
			t.Errorf("dependencies %v", info["dependencies"])
		}
	}
	if modSettingsMod.output != gameenv.ModSettingsDefsFile || modSettingsMod.maxOutput != modsettings.MaxDefinitionsBytes {
		t.Errorf("output %q, max %d", modSettingsMod.output, modSettingsMod.maxOutput)
	}
}

// The world's lock is held only while the world's mods are read, never while
// the game runs: a start can take it during a run. A miss on a locked world is
// world.ErrBusy, matchable by the caller, and leaves no container or folder.
func TestModSettingDefsWorldLock(t *testing.T) {
	r := newModSettingsRig(t)
	unlock, err := r.worlds.Lock(r.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.ModSettingDefs(context.Background(), r.id); !errors.Is(err, world.ErrBusy) {
		t.Errorf("a locked world: %v, want world.ErrBusy", err)
	}
	if r.creates() != 0 || len(r.previewFolders()) != 0 {
		t.Errorf("%d containers created, folders %v", r.creates(), r.previewFolders())
	}
	unlock()

	ch := r.ask(context.Background())
	c := r.waitRunning()
	unlock, err = r.worlds.Lock(r.id)
	if err != nil {
		t.Fatalf("the world is locked during the game run: %v", err)
	}
	unlock()
	r.finish(c, defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil {
		t.Fatal(res.err)
	}
	unlock, err = r.worlds.Lock(r.id)
	if err != nil {
		t.Fatalf("the world is still locked after the run: %v", err)
	}
	unlock()
}

// A cache hit needs no lock: it works while the world starts or stops.
func TestModSettingDefsHitWhileLocked(t *testing.T) {
	r := newModSettingsRig(t)
	first := r.run()
	unlock, err := r.worlds.Lock(r.id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	got, err := r.c.ModSettingDefs(context.Background(), r.id)
	if err != nil || got != first || r.creates() != 1 {
		t.Errorf("a hit on a locked world: %v, same %v, %d runs", err, got == first, r.creates())
	}
}

// The key is read again after the run without the lock, so a world that is
// busy then still has its result cached.
func TestModSettingDefsCachesWhileWorldIsBusyAfterTheRun(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	unlock, err := r.worlds.Lock(r.id)
	if err != nil {
		t.Fatal(err)
	}
	r.finish(c, defsJSON, defaultsDat(t))
	res := r.result(ch)
	if res.err != nil {
		t.Fatal(res.err)
	}
	// The world is still locked, and the hit works.
	got, err := r.c.ModSettingDefs(context.Background(), r.id)
	unlock()
	if err != nil || got != res.defs || r.creates() != 1 {
		t.Errorf("after the run: %v, same %v, %d runs, want the cached result", err, got == res.defs, r.creates())
	}
}

// A mods folder with more files than a run takes is world.ErrModsTooBig,
// which the caller can match, and no container or folder is left. The limit
// is lowered, so the test needs few files.
func TestModSettingDefsModsTooBig(t *testing.T) {
	r := newModSettingsRig(t)
	const limit = 10
	dir := filepath.Join(r.worldDir(r.id), world.ModsDir, "big_1.0.0")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for i := range limit + 1 {
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	restore := world.SetLinkLimitsForTest(limit, 1<<20)
	defer restore()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := r.c.ModSettingDefs(ctx, r.id); !errors.Is(err, world.ErrModsTooBig) {
		t.Fatalf("error %v, want world.ErrModsTooBig", err)
	}
	if r.creates() != 0 || len(r.previewFolders()) != 0 {
		t.Errorf("%d containers created, folders %v", r.creates(), r.previewFolders())
	}
}

// A world mod named like Foreman's reader could stand in for it, so the run
// is refused, however the mod is there: a list entry, a zip or a folder.
func TestModSettingDefsRefusesAModNamedLikeTheReader(t *testing.T) {
	list := `{"mods":[{"name":"base","enabled":true},{"name":"` + modsettingsmod.ModName + `","enabled":true}]}`
	for name, setup := range map[string]func(r *modSettingsRig){
		"a list entry": func(r *modSettingsRig) {
			r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModListFile), []byte(list))
		},
		"a zip": func(r *modSettingsRig) {
			r.writeFile(r.id, filepath.Join(world.ModsDir, modsettingsmod.ModDir+".zip"), []byte("PK"))
		},
		"a folder": func(r *modSettingsRig) {
			dir := filepath.Join(r.worldDir(r.id), world.ModsDir, modsettingsmod.ModName+"_9.9.9")
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newModSettingsRig(t)
			setup(r)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := r.c.ModSettingDefs(ctx, r.id)
			if !errors.Is(err, ErrModSettingsBadOutput) || !errors.Is(err, errControlModTaken) {
				t.Fatalf("error %v, want ErrModSettingsBadOutput for a taken name", err)
			}
			if r.creates() != 0 || len(r.previewFolders()) != 0 {
				t.Errorf("%d containers created, folders %v", r.creates(), r.previewFolders())
			}
		})
	}
}

// SPEC 14.6: the cache keeps at most maxModSettingsEntries worlds, dropping
// the least recently used; a hit counts as a use.
func TestModSettingsCacheKeepsTheMostRecentlyUsed(t *testing.T) {
	var c modSettingsCache
	defs := &ModSettingDefs{}
	for i := range maxModSettingsEntries {
		c.put("w"+string(rune('a'+i)), "k", defs)
	}
	if _, ok := c.get("wa", "k"); !ok {
		t.Fatal("the first world is missing")
	}
	c.put("wnew", "k", defs)
	if len(c.entries) != maxModSettingsEntries {
		t.Errorf("%d entries, want %d", len(c.entries), maxModSettingsEntries)
	}
	if _, ok := c.get("wb", "k"); ok {
		t.Error("the least recently used world was kept")
	}
	for _, id := range []string{"wa", "wc", "wnew"} {
		if _, ok := c.get(id, "k"); !ok {
			t.Errorf("world %s was dropped", id)
		}
	}
	// A new key for the same world replaces its entry, and a stale key is a miss.
	c.put("wa", "k2", defs)
	if _, ok := c.get("wa", "k"); ok || len(c.entries) != maxModSettingsEntries {
		t.Errorf("the old key still hits, or the entry count is %d", len(c.entries))
	}
}
