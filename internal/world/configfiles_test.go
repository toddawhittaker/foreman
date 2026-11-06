package world

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfigHeldWritesAtomicallyAt0640(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	p := filepath.Join(dir, ConfigDir, ServerSettingsFile)
	if err := m.WriteConfigHeld(id, ServerSettingsFile, []byte(`{"name":"one"}`)); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteConfigHeld(id, ServerSettingsFile, []byte(`{"name":"two"}`)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
	if err != nil || string(b) != `{"name":"two"}` {
		t.Errorf("%q, %v", b, err)
	}
	if got := mode(t, p); got != 0o640 {
		t.Errorf("mode %o, want 640", got)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ConfigDir))
	if len(entries) != 1 {
		t.Errorf("temp files left in config/: %v", entries)
	}
}

func TestWriteConfigHeldRefusesOtherNames(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	for _, name := range []string{"rconpw", "../world.json", "config/server-settings.json", "", "extra.json", "server-settings.json/"} {
		if err := m.WriteConfigHeld(id, name, []byte("{}")); !errors.Is(err, ErrConfigFile) {
			t.Errorf("%q: got %v, want ErrConfigFile", name, err)
		}
	}
	if err := m.WriteConfigHeld(id, ServerSettingsFile, make([]byte, maxConfigFile+1)); !errors.Is(err, ErrConfigTooBig) {
		t.Errorf("too big: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ConfigDir))
	if len(entries) != 0 {
		t.Errorf("a refused write left files: %v", entries)
	}
}

func TestWriteConfigHeldRefusesANewerWorld(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	body := `{"format_version":2,"name":"Check","game_version":"2.0.77","space_age":false,"created_at":"2026-11-03T14:25:01Z"}`
	if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteConfigHeld(id, ServerSettingsFile, []byte("{}")); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("got %v, want ErrNewerFormat", err)
	}
}

func TestReadConfigReturnsBytesAndHash(t *testing.T) {
	m, id, _ := checkWorld(t, "2.0.77", false)
	got, err := m.ReadConfig(id, ServerSettingsFile)
	if err != nil || got.Found || got.Hash != "" || got.Data != nil {
		t.Fatalf("missing file: %+v, %v", got, err)
	}
	if err := m.WriteConfigHeld(id, ServerSettingsFile, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err = m.ReadConfig(id, ServerSettingsFile)
	sum := sha256.Sum256([]byte(`{"a":1}`))
	if err != nil || !got.Found || string(got.Data) != `{"a":1}` || got.Hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("%+v, %v", got, err)
	}
	// An empty file is found, and differs from a missing one.
	if err := m.WriteConfigHeld(id, ServerSettingsFile, nil); err != nil {
		t.Fatal(err)
	}
	empty, err := m.ReadConfig(id, ServerSettingsFile)
	if err != nil || !empty.Found || empty.Hash == "" || empty.Hash == got.Hash {
		t.Errorf("empty file: %+v, %v", empty, err)
	}
	// A change shows as a different hash.
	if err := m.WriteConfigHeld(id, ServerSettingsFile, []byte(`{"a":2}`)); err != nil {
		t.Fatal(err)
	}
	if again, _ := m.ReadConfig(id, ServerSettingsFile); again.Hash == got.Hash {
		t.Error("hash did not change")
	}
}

func TestReadConfigRefusals(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	for _, name := range []string{"rconpw", "../world.json", "x"} {
		if _, err := m.ReadConfig(id, name); !errors.Is(err, ErrConfigFile) {
			t.Errorf("%q: got %v, want ErrConfigFile", name, err)
		}
	}
	// A link in place of the file is not followed (SPEC 14.5).
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ConfigDir, ServerSettingsFile)); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ReadConfig(id, ServerSettingsFile); err == nil || strings.Contains(string(got.Data), "secret") {
		t.Errorf("link followed: %+v, %v", got, err)
	}
	if _, err := m.ReadConfig("missing", ServerSettingsFile); err == nil {
		t.Error("unknown world accepted")
	}
}
