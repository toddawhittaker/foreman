package world

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedPtr(n uint32) *uint32 { return &n }

func readWorldMetaFile(t *testing.T, m *Manager, id string) Meta {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(m.Dirs().Worlds, id, MetaFile)) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	var meta Meta
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func TestGenerateMakesAWorldWithNoSaveAndNewMap(t *testing.T) {
	m, active := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{
		Name: "Fresh", GameVersion: "2.0.77", SpaceAge: true,
		Preset: "rich-resources", Seed: seedPtr(4294967295),
		MapGenSettings: []byte(`{"width":0}`), MapSettings: []byte(`{"pollution":{}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if active.id != id {
		t.Errorf("first world should become active: %q", active.id)
	}
	dir := filepath.Join(m.Dirs().Worlds, id)
	meta := readWorldMetaFile(t, m, id)
	if meta.Name != "Fresh" || meta.GameVersion != "2.0.77" || !meta.SpaceAge {
		t.Errorf("meta: %+v", meta)
	}
	if meta.NewMap == nil || meta.NewMap.Preset != "rich-resources" || meta.NewMap.Seed == nil || *meta.NewMap.Seed != 4294967295 {
		t.Errorf("new_map: %+v", meta.NewMap)
	}
	for f, want := range map[string]string{MapGenSettingsFile: `{"width":0}`, MapSettingsFile: `{"pollution":{}}`} {
		p := filepath.Join(dir, ConfigDir, f)
		b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
		if err != nil || string(b) != want {
			t.Errorf("%s: %q, %v", f, b, err)
		}
		if got := mode(t, p); got != 0o640 {
			t.Errorf("%s mode %o, want 640", f, got)
		}
	}
	if saves, _ := os.ReadDir(filepath.Join(dir, SavesDir)); len(saves) != 0 {
		t.Errorf("Generate wrote a save: %v", saves)
	}
	assertNoStaging(t, m)
}

func TestGenerateWithoutSettingsWritesNoConfigFilesAndKeepsNilSeed(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{Name: "Plain", GameVersion: "1.1.110"})
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(m.Dirs().Worlds, id, ConfigDir)); len(entries) != 0 {
		t.Errorf("config files written: %v", entries)
	}
	meta := readWorldMetaFile(t, m, id)
	if meta.NewMap == nil || meta.NewMap.Preset != "" || meta.NewMap.Seed != nil {
		t.Errorf("new_map %+v, want set with no preset and no seed", meta.NewMap)
	}
	b, _ := os.ReadFile(filepath.Join(m.Dirs().Worlds, id, MetaFile)) //nolint:gosec // a test temp folder
	if !strings.Contains(string(b), `"new_map"`) || strings.Contains(string(b), `"seed"`) {
		t.Errorf("world.json: %s", b)
	}
}

func TestGenerateRefusals(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	if _, err := m.Generate(ctx, NewWorld{Name: "Taken", GameVersion: "2.0.77"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		nw   NewWorld
		want error
	}{
		{"taken name, other case", NewWorld{Name: "tAKEN", GameVersion: "2.0.77"}, ErrNameTaken},
		{"blank name", NewWorld{Name: "  ", GameVersion: "2.0.77"}, ErrNameEmpty},
		{"unsupported line", NewWorld{Name: "A", GameVersion: "3.0.1"}, ErrBadGameVersion},
		{"not a version", NewWorld{Name: "B", GameVersion: "latest"}, ErrBadGameVersion},
		{"space age on 1.1", NewWorld{Name: "C", GameVersion: "1.1.110", SpaceAge: true}, ErrSpaceAgeOld},
		{"preset with a quote", NewWorld{Name: "D", GameVersion: "2.0.77", Preset: `x"; rm -rf /`}, ErrBadPreset},
		{"bad map-gen JSON", NewWorld{Name: "E", GameVersion: "2.0.77", MapGenSettings: []byte(`{`)}, ErrBadMapJSON},
		{"bad map settings JSON", NewWorld{Name: "F", GameVersion: "2.0.77", MapSettings: []byte(`nope`)}, ErrBadMapJSON},
		{"map-gen too big", NewWorld{Name: "G", GameVersion: "2.0.77", MapGenSettings: []byte(`"` + strings.Repeat("a", maxConfigFile) + `"`)}, ErrConfigTooBig},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := m.Generate(ctx, c.nw); !errors.Is(err, c.want) {
				t.Errorf("got %v, want %v", err, c.want)
			}
		})
	}
	worlds, _ := m.List(ctx)
	if len(worlds) != 1 {
		t.Errorf("a refused Generate left a world: %d worlds", len(worlds))
	}
	assertNoStaging(t, m)
}

func TestNewMapRoundTripsAndKeepsUnknownFields(t *testing.T) {
	in := `{"format_version":1,"name":"N","game_version":"2.0.77","space_age":false,"created_at":"2026-11-03T14:25:01Z",` +
		`"new_map":{"preset":"death-world","seed":7,"future":1},"later":{"x":true}}`
	var meta Meta
	if err := json.Unmarshal([]byte(in), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.NewMap == nil || meta.NewMap.Preset != "death-world" || *meta.NewMap.Seed != 7 {
		t.Fatalf("new_map %+v", meta.NewMap)
	}
	out, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if string(back["later"]) != `{"x":true}` {
		t.Errorf("unknown top-level field lost: %s", out)
	}
	var nm NewMap
	if err := json.Unmarshal(back["new_map"], &nm); err != nil || nm.Preset != "death-world" || *nm.Seed != 7 {
		t.Errorf("new_map after round trip: %s, %v", back["new_map"], err)
	}
	// A world with no new_map does not gain one.
	meta.NewMap = nil
	out, _ = json.Marshal(meta)
	if strings.Contains(string(out), "new_map") {
		t.Errorf("new_map written when nil: %s", out)
	}
}

func TestClearNewMap(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	id, err := m.Generate(ctx, NewWorld{Name: "Clear", GameVersion: "2.0.77", Seed: seedPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	// Fields a newer Foreman added must survive the rewrite.
	p := filepath.Join(m.Dirs().Worlds, id, MetaFile)
	b, _ := os.ReadFile(p) //nolint:gosec // a test temp folder
	b = []byte(strings.Replace(string(b), `"name"`, `"later": 5, "name"`, 1))
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := m.ClearNewMap(id); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p) //nolint:gosec // a test temp folder
	if strings.Contains(string(b), "new_map") || !strings.Contains(string(b), `"later": 5`) {
		t.Errorf("world.json: %s", b)
	}
	if err := m.ClearNewMap(id); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
}

func TestClearNewMapWaitsForTheLockAndRefusesWhenBusy(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{Name: "Busy", GameVersion: "2.0.77"})
	if err != nil {
		t.Fatal(err)
	}
	m.addSaveWait = 0
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ClearNewMap(id); !errors.Is(err, ErrBusy) {
		t.Errorf("got %v, want ErrBusy", err)
	}
	unlock()
	if readWorldMetaFile(t, m, id).NewMap == nil {
		t.Error("a refused clear changed world.json")
	}
}

func TestClearNewMapRefusesANewerWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{Name: "Future", GameVersion: "2.0.77"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(m.Dirs().Worlds, id, MetaFile)
	b, _ := os.ReadFile(p) //nolint:gosec // a test temp folder
	b = []byte(strings.Replace(string(b), `"format_version": 1`, `"format_version": 2`, 1))
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := m.ClearNewMap(id); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("got %v, want ErrNewerFormat", err)
	}
}

// SPEC 7.4: a world with no save is refused unless it is waiting to make
// its map, and a save that is there is checked as usual.
func TestPreCheckNewMapSkipsNoSaveOnlyWhileSavesHoldNoZip(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	id, err := m.Generate(ctx, NewWorld{Name: "Gen", GameVersion: "2.0.77"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	p := runCheck(t, m, id)
	wantKinds(t, p)
	if !p.OK() || p.Save != "" {
		t.Errorf("%+v", p)
	}

	// A zip appears (a start made the map, or one was uploaded): the usual check.
	dir := filepath.Join(m.Dirs().Worlds, id)
	putFixture(t, dir, "2.0.77-space-age.zip", "a.zip", t0)
	wantKinds(t, runCheck(t, m, id), ProblemSpaceAgeOff)

	// The same world without new_map has the problem.
	if err := os.Remove(filepath.Join(dir, SavesDir, "a.zip")); err != nil {
		t.Fatal(err)
	}
	wantKinds(t, runCheck(t, m, id))
	if err := m.ClearNewMap(id); err != nil {
		t.Fatal(err)
	}
	wantKinds(t, runCheck(t, m, id), ProblemNoSave)
}

func TestPreCheckNewMapDoesNotHideAMissingChosenSave(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	id, err := m.Generate(ctx, NewWorld{Name: "Gen", GameVersion: "2.0.77"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dirs().Worlds, id)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "gone.zip", t0)
	if err := m.SetLoadSave(id, "gone.zip"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, SavesDir, "gone.zip")); err != nil {
		t.Fatal(err)
	}
	wantKinds(t, runCheck(t, m, id), ProblemNoSave)
}

func TestSettingsSnapshotReasonIsAutomatic(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{Name: "Snap", GameVersion: "2.0.77"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := m.AutoSnapshot(id, ReasonSettings, LabelBeforeSettings, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if s.Reason != "settings" || s.Label != "Before settings change" || !s.Automatic() {
		t.Errorf("%+v", s)
	}
}
