package world

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// SPEC 5.5: one folder is described by name, and a name that is not a
// folder directly in the inbox is ErrNoFolder.
func TestInboxEntry(t *testing.T) {
	m, _ := newTestManager(t)
	in := m.dirs.Imports
	copyFixture(t, "2.0.77-no-space-age.zip", filepath.Join(in, "plain", "a.zip"), t0)
	f, err := m.InboxEntry("plain")
	if err != nil || !f.Read || f.GameVersion != "2.0.77" || f.Saves != 1 {
		t.Errorf("plain: %+v, %v", f, err)
	}
	for _, name := range []string{"", "missing", "..", "../x", "plain/a.zip", ".hidden"} {
		if _, err := m.InboxEntry(name); !errors.Is(err, ErrNoFolder) {
			t.Errorf("%q: got %v, want ErrNoFolder", name, err)
		}
	}
}

// SPEC 9.7: only space-age itself turns the flag on.
func TestUsesSpaceAge(t *testing.T) {
	cases := []struct {
		mods []string
		want bool
	}{
		{[]string{"base", "space-age", "quality", "elevated-rails"}, true},
		{[]string{"base", "quality"}, false},
		{[]string{"base", "elevated-rails", "quality"}, false},
		{[]string{"base"}, false},
	}
	for _, c := range cases {
		var mods []savefile.Mod
		for _, n := range c.mods {
			mods = append(mods, savefile.Mod{Name: n})
		}
		if got := usesSpaceAge(mods); got != c.want {
			t.Errorf("%v: got %v, want %v", c.mods, got, c.want)
		}
	}
}

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
