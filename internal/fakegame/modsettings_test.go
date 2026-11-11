package fakegame_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// modSettingsRun runs a definitions container in the rig's work folder and
// returns its state.
func modSettingsRun(t *testing.T, r *rig) docker.State {
	t.Helper()
	ctx := context.Background()
	id, err := r.docker.CreateContainer(ctx, "foreman-modsettings-t", docker.ContainerSpec{
		Image:  image21,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env:    []string{gameenv.PreviewKind + "=" + gameenv.KindModSettings, gameenv.Seed + "=9"},
		HostConfig: docker.HostConfig{
			Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	return r.inspect(id).State
}

type defsFile struct {
	Settings []map[string]any `json:"settings"`
}

func readDefs(t *testing.T, r *rig) defsFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.world, gameenv.ModSettingsDefsFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	var f defsFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("the definitions are not JSON: %v\n%s", err, b)
	}
	return f
}

// A definitions run writes the definitions of the test mod and the game's
// defaults file, and draws nothing.
func TestModSettingsRunWritesBothFiles(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	if st := modSettingsRun(t, r); st.Status != "exited" || st.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", st)
	}
	f := readDefs(t, r)
	if len(f.Settings) != 3*5+3 {
		t.Fatalf("%d settings, want 18", len(f.Settings))
	}
	byName := map[string]map[string]any{}
	for _, s := range f.Settings {
		for _, k := range []string{"name", "mod", "setting_type", "type", "hidden", "allow_blank", "auto_trim"} {
			if _, ok := s[k]; !ok {
				t.Errorf("%v lacks %q", s["name"], k)
			}
		}
		if s["mod"] != "fmtest" {
			t.Errorf("%v: mod %v", s["name"], s["mod"])
		}
		byName[s["name"].(string)] = s
	}

	if d := byName["fm-sta-int"]["default_value"]; d != 7.0 {
		t.Errorf("fm-sta-int default is %v, want the raised 7", d)
	}
	i := byName["fm-glo-int"]
	if i["setting_type"] != "runtime-global" || i["type"] != "int-setting" || i["order"] != "b" ||
		i["default_value"] != 5.0 || i["minimum_value"] != 1.0 || i["maximum_value"] != 10.0 {
		t.Errorf("fm-glo-int is %v", i)
	}
	if _, has := i["allowed_values"]; has {
		t.Errorf("fm-glo-int has allowed values: %v", i)
	}
	if b := byName["fm-usr-bool"]; b["default_value"] != true || b["minimum_value"] != nil {
		t.Errorf("fm-usr-bool is %v", b)
	}
	if s := byName["fm-sta-string"]; s["default_value"] != "red" || len(s["allowed_values"].([]any)) != 3 {
		t.Errorf("fm-sta-string is %v", s)
	}
	col, _ := byName["fm-sta-color"]["default_value"].(map[string]any)
	if col["r"] != 1.0 || col["g"] != 0.5 || col["b"] != 0.0 || col["a"] != 1.0 {
		t.Errorf("fm-sta-color default is %v", col)
	}
	if byName["fm-sta-hidden"]["hidden"] != true || byName["fm-sta-hidden"]["default_value"] != false {
		t.Errorf("fm-sta-hidden is %v", byName["fm-sta-hidden"])
	}
	if a := byName["fm-sta-forced"]["allowed_values"].([]any); len(a) != 1 || a[0] != 3.0 {
		t.Errorf("fm-sta-forced is %v", byName["fm-sta-forced"])
	}
	if w := byName["fm-sta-weird"]; w["type"] != "weird-setting" {
		t.Errorf("fm-sta-weird is %v", w)
	}

	got, err := os.ReadFile(filepath.Join(r.world, world.ModsDir, gameenv.ModSettingsDefaultsFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../modsettings/testdata/game-2.1.24.dat")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("the defaults are not the bytes the real game wrote")
	}
	if matches, _ := filepath.Glob(filepath.Join(r.world, "*.png")); len(matches) != 0 {
		t.Errorf("a mod settings run drew pictures: %v", matches)
	}
}

// The failure switches end a definitions run the way the real one can fail.
func TestModSettingsRunFailureModes(t *testing.T) {
	for _, tc := range []struct {
		mode   fakegame.PreviewMode
		status string
		code   int
		oom    bool
	}{
		{fakegame.PreviewOOM, "exited", 137, true},
		{fakegame.PreviewHangs, "running", 0, false}, // the run's time limit stops it
		{fakegame.PreviewFails, "exited", 1, false},
		{fakegame.PreviewBroken, "exited", 0, false},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			r := newRig(t, fakegame.Config{})
			if err := r.game.SetPreviewMode(tc.mode); err != nil {
				t.Fatal(err)
			}
			st := modSettingsRun(t, r)
			if st.Status != tc.status || st.ExitCode != tc.code || st.OOMKilled != tc.oom {
				t.Errorf("state %+v, want %s with %d (oom %v)", st, tc.status, tc.code, tc.oom)
			}
			b, err := os.ReadFile(filepath.Join(r.world, gameenv.ModSettingsDefsFile)) //nolint:gosec // a test's own temp folder
			if tc.mode == fakegame.PreviewBroken {
				if err != nil {
					t.Fatal(err)
				}
				if json.Valid(b) {
					t.Errorf("the broken definitions are valid JSON: %s", b)
				}
			} else if err == nil {
				t.Errorf("a %s run wrote definitions", tc.mode)
			}
		})
	}
}

// The broken switch leaves other kinds of preview run alone.
func TestBrokenModeOnlyBreaksModSettingsRuns(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	if err := r.game.SetPreviewMode(fakegame.PreviewBroken); err != nil {
		t.Fatal(err)
	}
	if _, err := resourceRun(t, r, 3, "nauvis"); err != nil {
		t.Fatal(err)
	}
}

// valueNode is the bytes the game writes after a setting's "value" key, as
// far as the canned types go: bool, int, double, string and color.
func valueNode(t *testing.T, typ string, v any) []byte {
	t.Helper()
	f64 := func(f float64) []byte { return binary.LittleEndian.AppendUint64(nil, math.Float64bits(f)) }
	switch typ {
	case "bool-setting":
		b := byte(0)
		if v.(bool) {
			b = 1
		}
		return []byte{0x01, 0x00, b}
	case "int-setting":
		return append([]byte{0x06, 0x00}, binary.LittleEndian.AppendUint64(nil, uint64(v.(float64)))...)
	case "double-setting":
		return append([]byte{0x02, 0x00}, f64(v.(float64))...)
	case "string-setting":
		s := v.(string)
		return append([]byte{0x03, 0x00, 0x00, byte(len(s))}, s...) //nolint:gosec // short canned strings
	case "color-setting":
		c := v.(map[string]any)
		out := []byte{0x05, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00}
		for _, k := range []string{"r", "g", "b", "a"} {
			out = append(out, 0x01, k[0], 0x02, 0x00)
			out = append(out, f64(c[k].(float64))...)
			if k != "a" {
				out = append(out, 0x00)
			}
		}
		return out
	}
	return nil
}

// Every editable canned definition is in the embedded defaults file with its
// default as its value, as the real game's run writes every setting. The
// check is a byte search for the setting's name, the dictionary header and
// the value node, so it does not depend on the codec.
func TestCannedDefinitionsMatchTheDefaultsFile(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	modSettingsRun(t, r)
	file, err := os.ReadFile(filepath.Join(r.world, world.ModsDir, gameenv.ModSettingsDefaultsFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	// After the name: a dictionary of one entry, "value".
	header := []byte{0x05, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x05, 'v', 'a', 'l', 'u', 'e'}
	var checked int
	for _, d := range readDefs(t, r).Settings {
		name, typ := d["name"].(string), d["type"].(string)
		want := valueNode(t, typ, d["default_value"])
		if want == nil {
			t.Logf("%s: type %s is not editable, so the game never writes it; skipped", name, typ)
			continue
		}
		prefix := append(append([]byte{byte(len(name))}, name...), header...) //nolint:gosec // short canned names
		at := bytes.Index(file, prefix)
		if at < 0 {
			t.Errorf("%s is not in the defaults file", name)
			continue
		}
		rest := file[at+len(prefix):]
		if !bytes.HasPrefix(rest, want) {
			t.Errorf("%s: the file has % x, the default is % x", name, rest[:min(len(rest), len(want))], want)
		}
		checked++
	}
	if checked != 3*5+2 {
		t.Errorf("%d settings checked, want 17", checked)
	}
}
