package world

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// SPEC 5.16: a world with no factory_map setting is manual with skip-online
// on, and the default is what a reader of world.json sees.
func TestFactoryMapDefaultIsManualSkipOnline(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)

	got := readMetaOf(t, m, id).FactoryMapSetting()
	if got != (FactoryMapMode{Mode: FactoryMapManual, SkipOnline: true}) {
		t.Errorf("default setting %+v, want manual with skip-online on", got)
	}
}

// SPEC 5.16: each of the three modes is stored in world.json as given, and
// skip_online is kept both ways, so a false is not lost to omitempty.
func TestSetFactoryMapModeStoresEveryMode(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)

	cases := []FactoryMapMode{
		{Mode: FactoryMapAutomatic, SkipOnline: true},
		{Mode: FactoryMapOff, SkipOnline: false},
		{Mode: FactoryMapManual, SkipOnline: false},
	}
	for _, want := range cases {
		if err := m.SetFactoryMapMode(id, want); err != nil {
			t.Fatalf("SetFactoryMapMode(%+v): %v", want, err)
		}
		meta := readMetaOf(t, m, id)
		if meta.FactoryMap == nil {
			t.Fatalf("world.json has no factory_map after setting %+v", want)
		}
		if got := *meta.FactoryMap; got != want {
			t.Errorf("stored %+v, want %+v", got, want)
		}
	}
}

// SPEC 5.16: an unknown mode is refused and world.json keeps its old value.
func TestSetFactoryMapModeRefusesUnknownMode(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	if err := m.SetFactoryMapMode(id, FactoryMapMode{Mode: FactoryMapAutomatic, SkipOnline: true}); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"", "Automatic", "always", "on"} {
		err := m.SetFactoryMapMode(id, FactoryMapMode{Mode: bad, SkipOnline: false})
		if !errors.Is(err, ErrFactoryMapMode) {
			t.Errorf("mode %q: error %v, want ErrFactoryMapMode", bad, err)
		}
	}
	if got := readMetaOf(t, m, id).FactoryMapSetting(); got != (FactoryMapMode{Mode: FactoryMapAutomatic, SkipOnline: true}) {
		t.Errorf("after refused modes the setting is %+v, want the old automatic one", got)
	}
}

// SPEC 5.16: the setting takes no snapshot and changes no file but
// world.json.
func TestSetFactoryMapModeTakesNoSnapshot(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	dir := filepath.Join(m.dirs.Worlds, id)
	before := fileTree(t, dir)

	if err := m.SetFactoryMapMode(id, FactoryMapMode{Mode: FactoryMapOff, SkipOnline: true}); err != nil {
		t.Fatal(err)
	}
	if snaps, _ := m.Snapshots(id); len(snaps) != 0 {
		t.Errorf("setting the mode took a snapshot: %v", snaps)
	}
	after := fileTree(t, dir)
	for p := range after {
		if _, ok := before[p]; !ok && p != MetaFile {
			t.Errorf("new file %q after setting the mode", p)
		}
	}
	for p, f := range before {
		if p == MetaFile {
			continue
		}
		if after[p] != f {
			t.Errorf("file %q changed by setting the mode", p)
		}
	}
}

// SPEC 5.1: a world a newer Foreman wrote is not changed by an older one.
func TestSetFactoryMapModeRefusesNewerFormat(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	w := filepath.Join(m.dirs.Worlds, id, MetaFile)
	if err := os.WriteFile(w, []byte(`{"format_version":9,"name":"Future","created_at":"2026-11-01T10:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := m.SetFactoryMapMode(id, FactoryMapMode{Mode: FactoryMapOff, SkipOnline: true})
	if !errors.Is(err, ErrNewerFormat) {
		t.Errorf("error %v, want ErrNewerFormat", err)
	}
}

// writeFactoryMapJSON replaces world.json's factory_map with raw, keeping the
// rest of the file as the seeded world wrote it.
func writeFactoryMapJSON(t *testing.T, m *Manager, id, raw string) {
	t.Helper()
	p := filepath.Join(m.dirs.Worlds, id, MetaFile)
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	doc["factory_map"] = json.RawMessage(raw)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Decoding fails safe (orchestrator ruling): a factory_map with no
// skip_online decodes as skip-online on, and so does one with only a mode.
func TestFactoryMapMissingSkipOnlineDecodesTrue(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)

	for _, raw := range []string{
		`{"mode":"automatic"}`,
		`{"mode":"off"}`,
		`{}`,
	} {
		writeFactoryMapJSON(t, m, id, raw)
		got := readMetaOf(t, m, id).FactoryMapSetting()
		if !got.SkipOnline {
			t.Errorf("%s decoded skip_online false, want true", raw)
		}
	}
}

// Decoding: a missing factory_map is manual with skip-online on.
func TestMissingFactoryMapDecodesManualSkipOnline(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	if err := m.SetFactoryMapMode(id, FactoryMapMode{Mode: FactoryMapOff, SkipOnline: false}); err != nil {
		t.Fatal(err)
	}
	// Drop the key entirely.
	p := filepath.Join(m.dirs.Worlds, id, MetaFile)
	b, _ := os.ReadFile(filepath.Clean(p))
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(b, &doc)
	delete(doc, "factory_map")
	out, _ := json.Marshal(doc)
	_ = os.WriteFile(p, out, 0o600)

	got := readMetaOf(t, m, id).FactoryMapSetting()
	if got != (FactoryMapMode{Mode: FactoryMapManual, SkipOnline: true}) {
		t.Errorf("missing factory_map decoded %+v, want manual with skip-online on", got)
	}
}

// Decoding: a mode outside the three is read as manual, and SetFactoryMapMode
// refuses to store it.
func TestUnknownStoredModeReadsManual(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	writeFactoryMapJSON(t, m, id, `{"mode":"always","skip_online":false}`)

	got := readMetaOf(t, m, id).FactoryMapSetting()
	if got.Mode != FactoryMapManual {
		t.Errorf("mode %q read, want manual", got.Mode)
	}
}
