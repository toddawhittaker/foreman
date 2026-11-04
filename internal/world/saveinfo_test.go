package world

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func copyFixture(t *testing.T, fixture, to string, mtime time.Time) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	write(t, to, string(b), mtime)
}

func TestInboxReadsTheNewestSave(t *testing.T) {
	m, _ := newTestManager(t)
	in := m.dirs.Imports
	copyFixture(t, "1.1.110-vanilla.zip", filepath.Join(in, "mixed", "old.zip"), t0)
	copyFixture(t, "2.1.24-space-age.zip", filepath.Join(in, "mixed", "new.zip"), t0.Add(time.Hour))
	copyFixture(t, "2.0.77-no-space-age.zip", filepath.Join(in, "plain", "a.zip"), t0)
	write(t, filepath.Join(in, "broken", "a.zip"), "not a zip", t0)
	got, err := m.Inbox()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]InboxFolder{}
	for _, f := range got {
		byName[f.Name] = f
	}
	if f := byName["mixed"]; !f.Read || f.GameVersion != "2.1.24" || !f.SpaceAge {
		t.Errorf("mixed: %+v", f)
	}
	if f := byName["plain"]; !f.Read || f.GameVersion != "2.0.77" || f.SpaceAge {
		t.Errorf("plain: %+v", f)
	}
	if f := byName["broken"]; f.Read || f.GameVersion != "" || f.SpaceAge || f.Saves != 1 {
		t.Errorf("broken: %+v", f)
	}
}
