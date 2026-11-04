package world

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modWorldWithSave is modCheckWorld with the save fixture chosen.
func modWorldWithSave(t *testing.T, version, fixture string, mods map[string]string) (*Manager, string, string) {
	t.Helper()
	m, id, dir := checkWorld(t, version, false)
	putFixture(t, dir, fixture, "a.zip", t0)
	enabled := []string{"base"}
	if strings.HasPrefix(fixture, "2.1") {
		enabled = append(enabled, "recycler")
	}
	for name, factorio := range mods {
		putModFor(t, dir, name, "1.0.0", factorio)
		enabled = append(enabled, name)
	}
	putModList(t, dir, enabled...)
	return m, id, dir
}

func modText(t *testing.T, m *Manager, id string) string {
	t.Helper()
	got := modVersionProblems(checkWith(t, m, id))
	if len(got) != 1 {
		t.Fatalf("mod version problems %+v", got)
	}
	return got[0].Text
}

// The game loads a mod zip that mod-list.json does not mention and turns it
// on, so only a mod listed as off is left out of the check.
func TestPreCheckModVersionCountsUnlistedZips(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.0.77", nil)
	putModFor(t, dir, "unlisted", "1.0.0", "1.1")
	putModListOff(t, dir, []string{"base"})
	if got := modVersionProblems(checkWith(t, m, id)); len(got) != 1 {
		t.Fatalf("unlisted zip: %+v", got)
	}
	putModListOff(t, dir, []string{"base"}, "unlisted")
	if got := modVersionProblems(checkWith(t, m, id)); len(got) != 0 {
		t.Fatalf("zip listed as off: %+v", got)
	}
}

func TestPreCheckModZipWithTooManyEntriesIsLeftUnread(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.0.77", nil)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("many_1.0.0/info.json")
	_, _ = w.Write([]byte(`{"factorio_version":"1.1"}`))
	for i := range maxModZipEntries {
		if _, err := zw.Create(fmt.Sprintf("many_1.0.0/f%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ModsDir, "many_1.0.0.zip"), buf.String())
	putModList(t, dir, "base", "many")
	if got := modVersionProblems(checkWith(t, m, id)); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// A valid zip behind 201 MiB of leading bytes, which the zip reader would
// accept: it must be skipped on size before the reader is made.
func TestPreCheckModZipOverTheSizeLimitIsLeftUnread(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.0.77", nil)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("big_1.0.0/info.json")
	_, _ = w.Write([]byte(`{"factorio_version":"1.1"}`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, ModsDir, "big_1.0.0.zip")
	f, err := os.Create(name) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxModZip + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(buf.Bytes(), maxModZip+1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	putModList(t, dir, "base", "big")
	if got := modVersionProblems(checkWith(t, m, id)); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

// 2.10 is not 2.1.
func TestPreCheckModVersionComparesMinorNumbersWhole(t *testing.T) {
	m, id, _ := modCheckWorld(t, "2.1.24", map[string]string{"future": "2.10"})
	if got := modText(t, m, id); !strings.Contains(got, "made for Factorio 2.10") {
		t.Fatalf("2.1 game, 2.10 mod: %s", got)
	}
}

func TestPreCheckModVersionAdviceInManagedMode(t *testing.T) {
	cases := []struct {
		name, world, fixture string
		mod                  string
		want                 string
	}{
		{"an older supported game that still loads the save", "2.1.24", "2.0.77-no-space-age.zip", "2.0",
			"Set the world's game version to 2.0.x, or update its mods."},
		{"an older game cannot load a newer save", "2.1.24", "2.1.24-no-space-age.zip", "2.0",
			"Update its mods to versions made for Factorio 2.1."},
		{"a version Foreman does not run", "2.1.24", "2.0.77-no-space-age.zip", "0.17",
			"Update its mods to versions made for Factorio 2.1."},
		{"a newer version than the save", "2.0.77", "2.0.77-no-space-age.zip", "2.1",
			"Set the world's game version to 2.1.x, or update its mods."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, id, _ := modWorldWithSave(t, c.world, c.fixture, map[string]string{"x": c.mod})
			if got := modText(t, m, id); !strings.HasSuffix(got, c.want) {
				t.Fatalf("got %q, want it to end %q", got, c.want)
			}
		})
	}
}

func TestPreCheckModVersionAdviceWithAnUnreadableSave(t *testing.T) {
	m, id, dir := modCheckWorld(t, "2.1.24", map[string]string{"x": "2.0"})
	if err := os.Remove(filepath.Join(dir, SavesDir, "a.zip")); err != nil {
		t.Fatal(err)
	}
	putSave(t, dir, "a.zip", "not a zip", t0)
	if got := modText(t, m, id); !strings.HasSuffix(got, "Update its mods to versions made for Factorio 2.1.") {
		t.Fatalf("got %q", got)
	}
}

func TestPreCheckLongModNamesAreCut(t *testing.T) {
	name := strings.Repeat("a", 150)
	m, id, _ := modCheckWorld(t, "2.1.24", map[string]string{name: "2.0"})
	got := modText(t, m, id)
	if !strings.Contains(got, strings.Repeat("a", maxShownModName)+"…") || strings.Contains(got, strings.Repeat("a", maxShownModName+1)) {
		t.Fatalf("got %q", got)
	}
}
