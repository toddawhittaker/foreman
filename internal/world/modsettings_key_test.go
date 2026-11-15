package world

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// A same-size edit moves only the file's modified time; that is enough.
func TestModSettingsKeyChangesWithFolderModFileTime(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	mod := filepath.Join(dir, ModsDir, "mine")
	if err := os.MkdirAll(mod, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(mod, "settings.lua")
	writeFile(t, file, "aaaa")
	pinTimes(t, filepath.Join(dir, ModsDir), mod, file)
	before := settingsKey(t, m, id)
	later := t0.Add(time.Second)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	if settingsKey(t, m, id) == before {
		t.Fatal("moving a folder mod file's modified time did not change the key")
	}
}

// The walk follows no link: a link inside a folder mod is hashed as a link,
// so its target, outside the world, does not reach the key.
func TestModSettingsKeyIgnoresLinkTargets(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	mod := filepath.Join(dir, ModsDir, "mine")
	if err := os.MkdirAll(mod, 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "target.lua")
	writeFile(t, outside, "x")
	if err := os.Symlink(outside, filepath.Join(mod, "link.lua")); err != nil {
		t.Fatal(err)
	}
	before := settingsKey(t, m, id)
	writeFile(t, outside, "a longer target")
	later := t0.Add(time.Hour)
	if err := os.Chtimes(outside, later, later); err != nil {
		t.Fatal(err)
	}
	if settingsKey(t, m, id) != before {
		t.Fatal("changing a link's target outside the world changed the key")
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
