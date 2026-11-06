package game

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

func seedOf(n uint32) *uint32 { return &n }

// generate makes a world with no save, as the Generate page does.
func (r *rig) generate(nw world.NewWorld) string {
	r.t.Helper()
	id, err := r.worlds.Generate(context.Background(), nw)
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *rig) newMapSet(id string) bool {
	r.t.Helper()
	meta, err := r.c.readMeta(id)
	if err != nil {
		r.t.Fatal(err)
	}
	return meta.NewMap != nil
}

// SPEC 4.3, 5.6: a generated world's first start makes the map. The container
// is told so by its environment, the save appears in the world's folder, the
// game reaches InGame, and new_map is then cleared, so later starts carry no
// create variables.
func TestFirstStartOfAGeneratedWorldCreatesTheMap(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.generate(world.NewWorld{
		Name: "Fresh", GameVersion: "2.1.24", Preset: "rich-resources", Seed: seedOf(4294967295),
		MapGenSettings: []byte(`{"width": 1000}`),
	})
	r.start(id)

	if st := r.c.Status(id); st.State != StateRunning {
		t.Fatalf("state %s, want running", st.State)
	}
	spec, ok := r.game.Daemon().Container(ContainerName(id))
	if !ok {
		t.Fatal("no container")
	}
	want := []string{
		gameenv.NewMap + "=1", gameenv.WorldID + "=" + id,
		gameenv.Preset + "=rich-resources", gameenv.Seed + "=4294967295",
	}
	if !slices.Equal(spec.Spec.Env, want) {
		t.Errorf("environment %v, want %v", spec.Spec.Env, want)
	}
	save := filepath.Join(r.worldDir(id), world.SavesDir, id+".zip")
	b, err := os.ReadFile(save) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatalf("the first start made no save: %v", err)
	}
	read, err := savefile.Read(bytes.NewReader(b), int64(len(b)))
	if err != nil || read.Version.String() != "2.1.24" {
		t.Errorf("the save reads as %+v, %v", read, err)
	}
	if r.newMapSet(id) {
		t.Error("new_map is still set after the game reached InGame")
	}

	// The next start carries no create variables, and the save is untouched.
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	r.start(id)
	spec, _ = r.game.Daemon().Container(ContainerName(id))
	if len(spec.Spec.Env) != 0 {
		t.Errorf("a later start has environment %v", spec.Spec.Env)
	}
	if after, _ := os.ReadFile(save); !bytes.Equal(after, b) { //nolint:gosec // a test's own temp folder
		t.Error("the later start changed the save")
	}
}

// SPEC 4.3: only the variables that are set are given.
func TestCreateEnvOnlyHasWhatIsSet(t *testing.T) {
	cases := []struct {
		name string
		in   *world.NewMap
		want []string
	}{
		{"an ordinary world", nil, nil},
		{"nothing chosen", &world.NewMap{}, []string{"FOREMAN_NEW_MAP=1", "FOREMAN_WORLD_ID=w"}},
		{"a seed of zero", &world.NewMap{Seed: seedOf(0)}, []string{"FOREMAN_NEW_MAP=1", "FOREMAN_WORLD_ID=w", "FOREMAN_SEED=0"}},
		{"a preset", &world.NewMap{Preset: "island"}, []string{"FOREMAN_NEW_MAP=1", "FOREMAN_WORLD_ID=w", "FOREMAN_PRESET=island"}},
	}
	for _, c := range cases {
		if got := createEnv("w", c.in); !slices.Equal(got, c.want) {
			t.Errorf("%s: environment %v, want %v", c.name, got, c.want)
		}
	}
}

// SPEC 4.3: an ordinary world's container has no create variables.
func TestOrdinaryStartHasNoCreateEnvironment(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Plain")
	r.start(id)
	c, _ := r.game.Daemon().Container(ContainerName(id))
	if len(c.Spec.Env) != 0 {
		t.Errorf("environment %v", c.Spec.Env)
	}
}

// SPEC 7.4: a Space Age world made by Generate starts, and the save the fake
// makes names the mods its mod list turns on.
func TestGeneratedSpaceAgeWorldStarts(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.generate(world.NewWorld{Name: "Planets", GameVersion: "2.0.77", SpaceAge: true})
	r.start(id)
	b, err := os.ReadFile(filepath.Join(r.worldDir(id), world.SavesDir, id+".zip")) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	read, err := savefile.Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range read.Mods {
		names = append(names, m.Name)
	}
	if !slices.Contains(names, "space-age") || read.Version.String() != "2.0.77" {
		t.Errorf("save is %s with mods %v", read.Version, names)
	}
}

// SPEC 14.8: every secret in server-settings.json is known to the log
// redaction, not only the game password: the factorio.com username,
// password and token too.
func TestSettingsSecretsAreAllRedacted(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Keys")
	r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"),
		[]byte(`{"game_password":"gp-1","username":"user-2","password":"pw-3","token":"tok-4","name":"Public name"}`))
	rconPW, all, err := r.c.secrets(id)
	if err != nil {
		t.Fatal(err)
	}
	line := "leak " + rconPW.Reveal() + " gp-1 user-2 pw-3 tok-4 Public name"
	got := Redact(line, all...)
	for _, s := range []string{rconPW.Reveal(), "gp-1", "user-2", "pw-3", "tok-4"} {
		if strings.Contains(got, s) {
			t.Errorf("%q is still in %q", s, got)
		}
	}
	if !strings.Contains(got, "Public name") {
		t.Errorf("a public setting was redacted: %q", got)
	}
}

// SPEC 14.8: empty or non-string values are not secrets, so they mask
// nothing.
func TestSettingsSecretsIgnoreEmptyAndNonStrings(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Empty keys")
	r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"),
		[]byte(`{"game_password":"","username":null,"password":5,"token":false}`))
	_, all, err := r.c.secrets(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Errorf("%d secrets, want only the RCON password", len(all))
	}
}
