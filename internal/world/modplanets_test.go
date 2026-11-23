package world

import (
	"os"
	"path/filepath"
	"testing"
)

func planetsFile(t *testing.T, m *Manager, id string) (string, []byte) {
	t.Helper()
	k, data, err := m.ModPlanetsFile(id)
	if err != nil {
		t.Fatal(err)
	}
	return k, data
}

// The planets key follows the world's mod-settings.dat as well as its mods,
// and a world with no file has none (SPEC 9.6).
func TestModPlanetsFileFollowsTheSettingsFile(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	putModList(t, dir, "base", "flib")
	if k, data := planetsFile(t, m, id); k != "" || data != nil {
		t.Fatalf("no file: key %q, data %q; want none", k, data)
	}
	putSettings(t, dir, "first")
	k1, data := planetsFile(t, m, id)
	if k1 == "" || string(data) != "first" {
		t.Fatalf("key %q, data %q", k1, data)
	}
	putSettings(t, dir, "second")
	k2, _ := planetsFile(t, m, id)
	if k2 == k1 {
		t.Fatal("changing mod-settings.dat kept the planets key")
	}
	putModFor(t, dir, "other", "1.0.0", "2.0")
	putModList(t, dir, "base", "flib", "other")
	if k3, _ := planetsFile(t, m, id); k3 == k2 {
		t.Fatal("changing the mods kept the planets key")
	}
}

func TestModPlanetsFileRefusesALink(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putSettings(t, dir, "x")
	p := filepath.Join(dir, ModsDir, ModSettingsFile)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.ModPlanetsFile(id); err == nil {
		t.Fatal("a linked mod-settings.dat was read")
	}
}
