package fakegame_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/rcon"
	"github.com/toddawhittaker/foreman/internal/secret"
)

const network = "foreman-games"

// gameFolder makes an adopted game folder the way SPEC 4.8 lays it out:
// worlds a and b, current linked to a, and saves, mods and config linked
// through current.
func gameFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, w := range []string{"a", "b"} {
		for _, sub := range []string{"saves", "mods", "config"} {
			if err := os.MkdirAll(filepath.Join(dir, "worlds", w, sub), 0o750); err != nil {
				t.Fatal(err)
			}
		}
		write(t, filepath.Join(dir, "worlds", w, "config", "rconpw"), "pw-"+w+"\n")
	}
	link(t, filepath.Join("worlds", "a"), filepath.Join(dir, "current"))
	for _, sub := range []string{"saves", "mods", "config"} {
		link(t, filepath.Join("current", sub), filepath.Join(dir, sub))
	}
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

// save writes a save file whose modification time is age ago.
func save(t *testing.T, dir, world, name string, age time.Duration) {
	t.Helper()
	p := filepath.Join(dir, "worlds", world, "saves", name)
	write(t, p, "zip")
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func adoptedRig(t *testing.T, a fakegame.Adopted) *rig {
	t.Helper()
	g := fakegame.New(t, fakegame.Config{SelfNetwork: network, Adopted: &a})
	c, err := docker.New(g.Host())
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, game: g, docker: c, world: a.GameDir}
}

func (r *rig) stop(id string) {
	r.t.Helper()
	if err := r.docker.StopContainer(context.Background(), id, 5); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) begin(id string) {
	r.t.Helper()
	if err := r.docker.StartContainer(context.Background(), id); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) adoptedRCON(id, pw string) *rcon.Client {
	r.t.Helper()
	ip := r.inspect(id).Networks[network].IPAddress
	if !strings.HasPrefix(ip, "127.") {
		r.t.Fatalf("container address %q is not a loopback address", ip)
	}
	c := rcon.New(ip+":27015", secret.New(pw))
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}

func (r *rig) adoptedID(name string) string {
	r.t.Helper()
	return r.inspect(name).ID
}

func TestAdoptedSeedsTheContainerAsDockerReportsIt(t *testing.T) {
	dir := gameFolder(t)
	r := adoptedRig(t, fakegame.Adopted{
		Name: "factorio", GameDir: dir, User: "845:845", RCONHostPort: "27016",
		Env: []string{"DLC_SPACE_AGE=false", "TZ=UTC"},
	})
	c := r.inspect("factorio")
	if c.State.Status != "exited" || c.Name != "factorio" || c.User != "845:845" {
		t.Errorf("container = %+v", c)
	}
	if c.Image != "factoriotools/factorio:2.1.24" {
		t.Errorf("image = %q", c.Image)
	}
	if len(c.Mounts) != 1 || c.Mounts[0].Source != dir || c.Mounts[0].Destination != "/factorio" {
		t.Errorf("mounts = %+v", c.Mounts)
	}
	if _, ok := c.Networks[network]; !ok || c.NetworkMode != network {
		t.Errorf("networks = %+v, mode %q", c.Networks, c.NetworkMode)
	}
	if !c.PublishesTCP(27015) {
		t.Errorf("RCON port bindings = %+v", c.PortBindings)
	}
	for name, want := range map[string]string{
		"PORT": "34197", "RCON_PORT": "27015", "SAVES": "/factorio/saves", "CONFIG": "/factorio/config",
		"MODS": "/factorio/mods", "SCENARIOS": "/factorio/scenarios", "SCRIPTOUTPUT": "/factorio/script-output",
		"PUID": "845", "PGID": "845", "VERSION": "2.1.24",
		"DLC_SPACE_AGE": "false", "TZ": "UTC",
	} {
		if got, ok := c.EnvValue(name); !ok || got != want {
			t.Errorf("%s = %q (set %v), want %q", name, got, ok, want)
		}
	}
	if got, ok := c.EnvValue("PRESET"); !ok || got != "" {
		t.Errorf("PRESET = %q (set %v), want set and empty", got, ok)
	}
	if list, err := r.docker.ListContainersOnNetwork(context.Background(), network); err != nil || len(list) == 0 {
		t.Errorf("not on the network: %v %v", list, err)
	}
}

func TestAdoptedRCONPortBindingIsOptional(t *testing.T) {
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: gameFolder(t)})
	if c := r.inspect("factorio"); c.PublishesTCP(27015) || len(c.PortBindings) != 0 {
		t.Errorf("port bindings = %+v", c.PortBindings)
	}
}

func TestAdoptedRefusesCreateAndRemove(t *testing.T) {
	g, err := fakegame.Start(fakegame.Config{
		Dir: t.TempDir(), SelfNetwork: network,
		Adopted: &fakegame.Adopted{Name: "factorio", GameDir: gameFolder(t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	c, err := docker.New(g.Host())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.CreateContainer(ctx, "foreman-w1", docker.ContainerSpec{Image: "x"}); err == nil {
		t.Error("a create was allowed")
	}
	if err := c.RemoveContainer(ctx, "factorio", true); err == nil {
		t.Error("a remove was allowed")
	}
	if _, err := c.InspectContainer(ctx, "factorio"); err != nil {
		t.Errorf("the container is gone: %v", err)
	}
}

func TestAdoptedConfigMistakesAreRefused(t *testing.T) {
	dir := gameFolder(t)
	for name, cfg := range map[string]fakegame.Config{
		"no network": {Adopted: &fakegame.Adopted{Name: "f", GameDir: dir}},
		"no name":    {SelfNetwork: network, Adopted: &fakegame.Adopted{GameDir: dir}},
		"relative":   {SelfNetwork: network, Adopted: &fakegame.Adopted{Name: "f", GameDir: "game"}},
		"bad env":    {SelfNetwork: network, Adopted: &fakegame.Adopted{Name: "f", GameDir: dir, Env: []string{"TZ"}}},
	} {
		cfg.Dir = t.TempDir()
		if g, err := fakegame.Start(cfg); err == nil {
			g.Close()
			t.Errorf("%s: started", name)
		}
	}
}

func TestAdoptedStartFollowsTheLinksAndLogsThePassword(t *testing.T) {
	dir := gameFolder(t)
	save(t, dir, "a", "old.zip", 3*time.Hour)
	save(t, dir, "a", "new.zip", time.Hour)
	save(t, dir, "a", "broken.tmp.zip", time.Minute)
	save(t, dir, "b", "other.zip", time.Hour)
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir})
	id := r.adoptedID("factorio")

	r.begin(id)
	log := r.log(id)
	if !strings.Contains(log, "Loading map /factorio/worlds/a/saves/new.zip: ") {
		t.Errorf("world a loads the wrong save:\n%s", log)
	}
	// Shaped like the last line of the real image's trace.
	const wantExec = "+ exec /opt/factorio/bin/x64/factorio --server-settings /factorio/config/server-settings.json" +
		" --server-banlist /factorio/config/server-banlist.json --rcon-port 27015" +
		" --server-whitelist /factorio/config/server-whitelist.json --use-server-whitelist" +
		" --server-adminlist /factorio/config/server-adminlist.json --rcon-password pw-a" +
		" --server-id /factorio/config/server-id.json --mod-directory /factorio/mods" +
		" --port 34197 --start-server-load-latest"
	if !strings.Contains(log, wantExec+"\n") {
		t.Errorf("the exec line is not the entrypoint's:\n%s", log)
	}
	if got := r.exec2(id, "pw-a", "/players online"); got != "Online players (0):\n" {
		t.Errorf("/players online = %q", got)
	}

	// Switching current changes the save, the password and the log's new run.
	r.stop(id)
	if err := os.Remove(filepath.Join(dir, "current")); err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join("worlds", "b"), filepath.Join(dir, "current"))
	r.begin(id)
	log = r.log(id)
	if !strings.Contains(log, "Loading map /factorio/worlds/b/saves/other.zip: ") {
		t.Errorf("world b loads the wrong save:\n%s", log)
	}
	if got := r.exec2(id, "pw-b", "/players online"); got != "Online players (0):\n" {
		t.Errorf("/players online on b = %q", got)
	}
	if strings.Count(log, "--rcon-password pw-a") != 1 || strings.Count(log, "--rcon-password pw-b") != 1 {
		t.Errorf("each run should log its own password once:\n%s", log)
	}
}

func (r *rig) exec2(id, pw, command string) string {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.adoptedRCON(id, pw).Exec(ctx, command)
	if err != nil {
		r.t.Fatalf("%s: %v", command, err)
	}
	return out
}

func TestAdoptedWithoutASaveGetsTheEntrypointsFirstAutosave(t *testing.T) {
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: gameFolder(t)})
	id := r.adoptedID("factorio")
	r.begin(id)
	if log := r.log(id); !strings.Contains(log, "Loading map /factorio/worlds/a/saves/_autosave1.zip: ") {
		t.Errorf("log:\n%s", log)
	}
}

func TestAdoptedMakesTheRCONPasswordWhenThereIsNone(t *testing.T) {
	dir := gameFolder(t)
	pw := filepath.Join(dir, "worlds", "a", "config", "rconpw")
	if err := os.Remove(pw); err != nil {
		t.Fatal(err)
	}
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir})
	id := r.adoptedID("factorio")
	r.begin(id)
	b, err := os.ReadFile(pw) //nolint:gosec // a path in the test's own folder
	if err != nil || len(strings.TrimSpace(string(b))) != 15 {
		t.Fatalf("rconpw = %q, %v", b, err)
	}
	if got := r.exec2(id, strings.TrimSpace(string(b)), "/players online"); got != "Online players (0):\n" {
		t.Errorf("/players online = %q", got)
	}
}

func TestAdoptedAppliesDLCSpaceAgeToTheModList(t *testing.T) {
	list := `{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":false},` +
		`{"name":"quality","enabled":true},{"name":"flib","enabled":true,"version":"0.16.0"}]}`
	for _, tc := range []struct {
		setting string
		want    map[string]bool // mod name to enabled; absent means not listed
	}{
		// "true" turns on the entries that exist and adds none.
		{"true", map[string]bool{"base": true, "space-age": true, "quality": true, "flib": true}},
		{"false", map[string]bool{
			"base": true, "space-age": false, "quality": false, "flib": true,
			"elevated-rails": false, "recycler": false,
		}},
		// A listed mod is enabled where the list has it, and not added.
		{"quality recycler", map[string]bool{
			"base": true, "space-age": false, "quality": true, "flib": true,
			"elevated-rails": false,
		}},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			dir := gameFolder(t)
			write(t, filepath.Join(dir, "worlds", "a", "mods", "mod-list.json"), list)
			r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir, Env: []string{"DLC_SPACE_AGE=" + tc.setting}})
			id := r.adoptedID("factorio")
			r.begin(id)

			b, err := os.ReadFile(filepath.Join(dir, "worlds", "a", "mods", "mod-list.json")) //nolint:gosec // a path in the test's own folder
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Mods []struct {
					Name    string
					Enabled bool
					Version string
				}
			}
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("%v: %s", err, b)
			}
			have := map[string]bool{}
			for _, m := range got.Mods {
				have[m.Name] = m.Enabled
				if m.Name == "flib" && m.Version != "0.16.0" {
					t.Errorf("flib lost its version: %s", b)
				}
			}
			if len(have) != len(tc.want) {
				t.Errorf("mods = %v, want %v", have, tc.want)
			}
			for name, on := range tc.want {
				if v, ok := have[name]; !ok || v != on {
					t.Errorf("%s = %v (listed %v), want %v", name, v, ok, on)
				}
			}
		})
	}
}

func TestAdoptedCreatesTheModListAndPrintsWhatTheScriptDoes(t *testing.T) {
	dir := gameFolder(t)
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir, Env: []string{"DLC_SPACE_AGE=false"}})
	id := r.adoptedID("factorio")
	r.begin(id)
	b, err := os.ReadFile(filepath.Join(dir, "worlds", "a", "mods", "mod-list.json")) //nolint:gosec // a path in the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"base"`, `"elevated-rails"`, `"recycler"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("mod-list.json lacks %s:\n%s", want, b)
		}
	}
	if log := r.log(id); !strings.Contains(log, "Disable mod space-age for DLC") {
		t.Errorf("log:\n%s", log)
	}
}

func TestAdoptedUsesTheContainersRCONPort(t *testing.T) {
	dir := gameFolder(t)
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir, Env: []string{"RCON_PORT=27020"}})
	id := r.adoptedID("factorio")
	r.begin(id)
	if !strings.Contains(r.log(id), " --rcon-port 27020 ") {
		t.Errorf("log:\n%s", r.log(id))
	}
	ip := r.inspect(id).Networks[network].IPAddress
	c := rcon.New(ip+":27020", secret.New("pw-a"))
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Exec(ctx, "/players online"); err != nil {
		t.Errorf("RCON on the container's port: %v", err)
	}
}

func TestAdoptedRunningSeedsAStartedGame(t *testing.T) {
	dir := gameFolder(t)
	save(t, dir, "a", "w.zip", time.Hour)
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir, Running: true})
	if st := r.status("factorio"); st != "running" {
		t.Fatalf("status = %q", st)
	}
	id := r.adoptedID("factorio")
	if log := r.log(id); !strings.Contains(log, "Loading map /factorio/worlds/a/saves/w.zip: ") {
		t.Errorf("log:\n%s", log)
	}
	if got := r.exec2(id, "pw-a", "/players online"); got != "Online players (0):\n" {
		t.Errorf("/players online = %q", got)
	}
	if n := len(r.game.State().Containers); n != 1 {
		t.Errorf("State lists %d containers, want the game only", n)
	}
}

func TestAdoptedStopsLikeAnyGame(t *testing.T) {
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: gameFolder(t), Running: true})
	id := r.adoptedID("factorio")
	r.stop(id)
	if st := r.status(id); st != "exited" {
		t.Errorf("status after stop = %q", st)
	}
	time.Sleep(300 * time.Millisecond)
	if st := r.status(id); st != "exited" {
		t.Errorf("a stopped game came back: %q", st)
	}
}

func TestAdoptedDanglingLinkStopsTheScript(t *testing.T) {
	dir := gameFolder(t)
	if err := os.Remove(filepath.Join(dir, "saves")); err != nil {
		t.Fatal(err)
	}
	link(t, filepath.Join("current", "nowhere", "saves"), filepath.Join(dir, "saves"))
	r := adoptedRig(t, fakegame.Adopted{Name: "factorio", GameDir: dir})
	id := r.adoptedID("factorio")
	r.begin(id)
	eventually(t, "the script to stop", func() bool { return r.inspect(id).State.ExitCode == 1 })
	if log := r.log(id); !strings.Contains(log, "mkdir: cannot create directory '/factorio/saves'") ||
		strings.Contains(log, "Loading map") || strings.Contains(log, dir) {
		t.Errorf("log:\n%s", log)
	}
}
