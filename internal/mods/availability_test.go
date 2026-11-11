package mods

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A restore swaps the world folder out for a moment. The availability
// check run then has nothing to say: no flags and no error to log.
func TestAvailabilityIsQuietWhileTheWorldFolderIsSwapped(t *testing.T) {
	f := newFixture(t, "visible-planets_1.5.5")
	dir := filepath.Join(f.m.Dirs().Worlds, f.id)
	if err := os.Rename(dir, dir+"-away"); err != nil {
		t.Fatal(err)
	}
	flags, err := f.planner.Availability(context.Background(), f.id)
	if err != nil || len(flags) != 0 {
		t.Errorf("Availability = %+v, %v; want nothing", flags, err)
	}
}

// The Mods tab's status is quiet too while the folder is swapped: no
// updates, flags or Change version, and no error.
func TestStatusIsQuietWhileTheWorldFolderIsSwapped(t *testing.T) {
	f := newFixture(t, "visible-planets_1.5.5")
	dir := filepath.Join(f.m.Dirs().Worlds, f.id)
	if err := os.Rename(dir, dir+"-away"); err != nil {
		t.Fatal(err)
	}
	st, err := f.planner.Status(context.Background(), f.id)
	if err != nil || len(st.Updates) != 0 || len(st.Flags) != 0 || len(st.Changeable) != 0 || len(st.Installed) != 0 {
		t.Errorf("Status = %+v, %v; want nothing", st, err)
	}
}
