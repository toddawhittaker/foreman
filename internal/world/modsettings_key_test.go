package world

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// pinTimes sets each path's times to t0, so only a file's own change can
// move the key.
func pinTimes(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.Chtimes(p, t0, t0); err != nil {
			t.Fatal(err)
		}
	}
}

// SPEC 9.6: the cache key covers what is inside a folder mod, so editing a
// file of an unpacked mod shows fresh definitions.
func TestModSettingsKeyChangesWithFolderModContents(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	mod := filepath.Join(dir, ModsDir, "mine")
	sub := filepath.Join(mod, "prototypes")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(sub, "settings.lua")
	writeFile(t, settings, "data:extend{}")
	folders := []string{filepath.Join(dir, ModsDir), mod, sub}
	pinTimes(t, folders...)
	before := settingsKey(t, m, id)

	writeFile(t, settings, "data:extend{{type='bool-setting'}}")
	pinTimes(t, folders...)
	edited := settingsKey(t, m, id)
	if edited == before {
		t.Fatal("editing a file inside a folder mod did not change the key")
	}
	writeFile(t, filepath.Join(sub, "more.lua"), "")
	pinTimes(t, folders...)
	if settingsKey(t, m, id) == edited {
		t.Fatal("adding a file inside a folder mod did not change the key")
	}
}

// A folder mod past the walk's limits gives an error, not a slow key.
func TestModSettingsKeyFolderModTooBig(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	mod := filepath.Join(dir, ModsDir, "mine")
	if err := os.MkdirAll(mod, 0o750); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		writeFile(t, filepath.Join(mod, fmt.Sprintf("f%d.lua", i)), "x")
	}
	defer SetLinkLimitsForTest(2, 1<<20)()
	if _, err := m.ModSettingsKey(id); !errors.Is(err, ErrModsTooBig) {
		t.Fatalf("got %v, want ErrModsTooBig", err)
	}
}
