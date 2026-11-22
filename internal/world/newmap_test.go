package world

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newUnfinished(t *testing.T, m *Manager, name string) string {
	t.Helper()
	id, err := m.Generate(context.Background(), NewWorld{
		Name: name, GameVersion: "2.0.77", SpaceAge: true,
		Preset: "death-world", Seed: seedPtr(7),
		MapGenSettings: []byte(`{"width":0}`), MapSettings: []byte(`{"pollution":{}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func addSave(t *testing.T, m *Manager, id string) {
	t.Helper()
	p := filepath.Join(m.Dirs().Worlds, id, SavesDir, "map.zip")
	if err := os.WriteFile(p, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func configFile(m *Manager, id, name string) string {
	return filepath.Join(m.Dirs().Worlds, id, ConfigDir, name)
}

func TestSetNewMapWritesBothFilesAndNewMapWithoutASnapshot(t *testing.T) {
	m, _ := newTestManager(t)
	id := newUnfinished(t, m, "Fresh")
	err := m.SetNewMap(context.Background(), id, NewMapFiles{
		Preset: "rich-resources", Seed: seedPtr(99),
		MapGenSettings: []byte(`{"width":1}`), MapSettings: []byte(`{"enemy_evolution":{}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{MapGenSettingsFile: `{"width":1}`, MapSettingsFile: `{"enemy_evolution":{}}`} {
		b, err := os.ReadFile(configFile(m, id, name)) //nolint:gosec // a test temp folder
		if err != nil || string(b) != want {
			t.Errorf("%s: %q, %v", name, b, err)
		}
		if got := mode(t, configFile(m, id, name)); got != 0o640 {
			t.Errorf("%s mode %o, want 640", name, got)
		}
	}
	meta := readWorldMetaFile(t, m, id)
	if meta.NewMap == nil || meta.NewMap.Preset != "rich-resources" || meta.NewMap.Seed == nil || *meta.NewMap.Seed != 99 {
		t.Errorf("new_map: %+v", meta.NewMap)
	}
	if meta.Name != "Fresh" || meta.GameVersion != "2.0.77" || !meta.SpaceAge {
		t.Errorf("other meta fields changed: %+v", meta)
	}
	if snaps, _ := m.Snapshots(id); len(snaps) != 0 {
		t.Errorf("SetNewMap took a snapshot: %v", snaps)
	}
	// The server settings Generate wrote are left alone.
	if _, err := os.Stat(configFile(m, id, ServerSettingsFile)); err != nil {
		t.Error(err)
	}
	assertNoStaging(t, m)
}

func TestSetNewMapUnchangedPresetRemovesTheFilesAndClearsTheSeed(t *testing.T) {
	m, _ := newTestManager(t)
	id := newUnfinished(t, m, "Fresh")
	if err := m.SetNewMap(context.Background(), id, NewMapFiles{Preset: "default"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{MapGenSettingsFile, MapSettingsFile} {
		if _, err := os.Stat(configFile(m, id, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still there: %v", name, err)
		}
	}
	meta := readWorldMetaFile(t, m, id)
	if meta.NewMap == nil || meta.NewMap.Preset != "default" || meta.NewMap.Seed != nil {
		t.Errorf("new_map: %+v", meta.NewMap)
	}
	// Saving twice is fine.
	if err := m.SetNewMap(context.Background(), id, NewMapFiles{Preset: "default"}); err != nil {
		t.Fatal(err)
	}
}

func TestSetNewMapKeepsUnknownWorldJSONFields(t *testing.T) {
	m, _ := newTestManager(t)
	id := newUnfinished(t, m, "Fresh")
	p := filepath.Join(m.Dirs().Worlds, id, MetaFile)
	b, _ := os.ReadFile(p) //nolint:gosec // a test temp folder
	b = append(b[:len(b)-2], []byte(",\"future_field\": 5\n}\n")...)
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := m.SetNewMap(context.Background(), id, NewMapFiles{Preset: "x"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p) //nolint:gosec // a test temp folder
	if !strings.Contains(string(after), `"future_field": 5`) {
		t.Errorf("unknown field lost: %s", after)
	}
}

func TestSetNewMapRefusalsChangeNothing(t *testing.T) {
	ctx := context.Background()
	files := NewMapFiles{Preset: "other", Seed: seedPtr(1), MapGenSettings: []byte(`{"a":1}`), MapSettings: []byte(`{"b":2}`)}
	tests := []struct {
		name  string
		setup func(t *testing.T, m *Manager) string
		want  error
	}{
		{"has a save", func(t *testing.T, m *Manager) string {
			id := newUnfinished(t, m, "Saved")
			addSave(t, m, id)
			return id
		}, ErrMapMade},
		{"no new_map", func(t *testing.T, m *Manager) string {
			id, err := m.Create(ctx, "Plain", "2.0.77", false)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}, ErrNotNewMap},
		{"archived", func(t *testing.T, m *Manager) string {
			seedWorld(t, m)
			id := newUnfinished(t, m, "Hidden")
			if _, err := m.SetArchived(ctx, id, true); err != nil {
				t.Fatal(err)
			}
			return id
		}, ErrWorldArchived},
		{"newer Foreman", func(t *testing.T, m *Manager) string {
			id := newUnfinished(t, m, "Newer")
			p := filepath.Join(m.Dirs().Worlds, id, MetaFile)
			meta := readWorldMetaFile(t, m, id)
			meta.FormatVersion = FormatVersion + 1
			b, err := meta.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o600); err != nil {
				t.Fatal(err)
			}
			return id
		}, ErrNewerFormat},
		{"busy", func(t *testing.T, m *Manager) string {
			id := newUnfinished(t, m, "Busy")
			unlock, err := m.Lock(id)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(unlock)
			return id
		}, ErrBusy},
		{"missing", func(*testing.T, *Manager) string { return "no-such-world" }, os.ErrNotExist},
		{"bad id", func(*testing.T, *Manager) string { return "../x" }, os.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestManager(t)
			id := tt.setup(t, m)
			var before map[string]fileState
			if tt.name != "missing" && tt.name != "bad id" {
				before = fileTree(t, filepath.Join(m.Dirs().Worlds, id))
			}
			err := m.SetNewMap(ctx, id, files)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if before != nil {
				sameTree(t, fileTree(t, filepath.Join(m.Dirs().Worlds, id)), before)
			}
		})
	}
}

func TestSetNewMapRefusesBadInputBeforeTouchingTheWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id := newUnfinished(t, m, "Fresh")
	before := fileTree(t, filepath.Join(m.Dirs().Worlds, id))
	big := make([]byte, maxConfigFile+1)
	for i := range big {
		big[i] = ' '
	}
	for name, tc := range map[string]struct {
		f    NewMapFiles
		want error
	}{
		"preset":     {NewMapFiles{Preset: "Bad Preset"}, ErrBadPreset},
		"gen json":   {NewMapFiles{MapGenSettings: []byte(`{`)}, ErrBadMapJSON},
		"set json":   {NewMapFiles{MapSettings: []byte(`nope`)}, ErrBadMapJSON},
		"gen big":    {NewMapFiles{MapGenSettings: big}, ErrConfigTooBig},
		"set big":    {NewMapFiles{MapSettings: big}, ErrConfigTooBig},
		"long name":  {NewMapFiles{Preset: string(make([]byte, 41))}, ErrBadPreset},
		"path chars": {NewMapFiles{Preset: "../x"}, ErrBadPreset},
	} {
		if err := m.SetNewMap(context.Background(), id, tc.f); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
	sameTree(t, fileTree(t, filepath.Join(m.Dirs().Worlds, id)), before)
}

func TestSetNewMapAfterTheMapIsMadeIsRefusedEvenWithNewMapStillSet(t *testing.T) {
	// A save with new_map not yet cleared is a world that is not Unfinished.
	m, _ := newTestManager(t)
	id := newUnfinished(t, m, "Racing")
	addSave(t, m, id)
	if err := m.SetNewMap(context.Background(), id, NewMapFiles{Preset: "x"}); !errors.Is(err, ErrMapMade) {
		t.Errorf("err = %v, want ErrMapMade", err)
	}
}

func TestSummaryUnfinishedIsNewMapWithNoSave(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	fresh := newUnfinished(t, m, "Fresh")
	plain, err := m.Create(ctx, "Plain", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	made := newUnfinished(t, m, "Made")
	addSave(t, m, made)

	want := map[string]bool{fresh: true, plain: false, made: false}
	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Unfinished != want[s.ID] {
			t.Errorf("List %s: Unfinished = %v, want %v", s.ID, s.Unfinished, want[s.ID])
		}
	}
	for id, w := range want {
		s, err := m.Get(ctx, id)
		if err != nil || s.Unfinished != w {
			t.Errorf("Get %s: Unfinished = %v, %v; want %v", id, s.Unfinished, err, w)
		}
	}
	// Clearing new_map (the first start made the map) ends it.
	unlock, _ := m.Lock(fresh)
	if err := m.ClearNewMapHeld(fresh); err != nil {
		t.Fatal(err)
	}
	unlock()
	if s, _ := m.Get(ctx, fresh); s.Unfinished {
		t.Error("Unfinished after new_map was cleared")
	}
	// A save added by a first start ends it too.
	other := newUnfinished(t, m, "Other")
	addSave(t, m, other)
	if s, _ := m.Get(ctx, other); s.Unfinished {
		t.Error("Unfinished with a save")
	}
}

func TestSetNewMapThenGenerateFilesMatchOneWriter(t *testing.T) {
	// Generate and SetNewMap write byte-identical files for the same input.
	m, _ := newTestManager(t)
	in := NewMapFiles{Preset: "p", MapGenSettings: []byte(`{"seed":1}`), MapSettings: []byte(`{"x":{}}`)}
	a, err := m.Generate(context.Background(), NewWorld{Name: "A", GameVersion: "2.0.77", Preset: in.Preset, MapGenSettings: in.MapGenSettings, MapSettings: in.MapSettings})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Generate(context.Background(), NewWorld{Name: "B", GameVersion: "2.0.77"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetNewMap(context.Background(), b, in); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{MapGenSettingsFile, MapSettingsFile} {
		x, _ := os.ReadFile(configFile(m, a, name)) //nolint:gosec // a test temp folder
		y, _ := os.ReadFile(configFile(m, b, name)) //nolint:gosec // a test temp folder
		if string(x) != string(y) || mode(t, configFile(m, a, name)) != mode(t, configFile(m, b, name)) {
			t.Errorf("%s differs: %q vs %q", name, x, y)
		}
	}
}
