package world

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The status strip names the live world from its metadata (DESIGN 6): the
// name it was given, the id when world.json has none, and not-exist for an
// id that is not a world or not a valid id.
func TestManagerName(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	if got, err := m.Name(id); err != nil || got != "Base" {
		t.Errorf("Name(%q) = %q, %v; want Base", id, got, err)
	}
	get, err := m.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Name(id); got != get.Name {
		t.Errorf("Name %q differs from Get's %q", got, get.Name)
	}

	if err := os.WriteFile(filepath.Join(m.dirs.Worlds, id, MetaFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := m.Name(id); err != nil || got != id {
		t.Errorf("unreadable world.json: Name = %q, %v; want the id", got, err)
	}

	for _, bad := range []string{"missing", "../etc", ""} {
		if _, err := m.Name(bad); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Name(%q) error %v, want not exist", bad, err)
		}
	}
}
