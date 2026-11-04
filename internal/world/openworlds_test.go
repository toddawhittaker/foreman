package world

import (
	"os"
	"testing"
)

// SPEC 14.5: the worlds folder is checked on every open, so a link swapped
// in for it is refused.
func TestOpenWorldsRefusesASwappedInLink(t *testing.T) {
	m, _ := newTestManager(t)
	root, err := m.OpenWorlds()
	if err != nil {
		t.Fatal(err)
	}
	_ = root.Close()

	elsewhere := t.TempDir()
	if err := os.Remove(m.Dirs().Worlds); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, m.Dirs().Worlds); err != nil {
		t.Fatal(err)
	}
	if root, err := m.OpenWorlds(); err == nil {
		_ = root.Close()
		t.Fatal("a link in place of the worlds folder was opened")
	}
}
