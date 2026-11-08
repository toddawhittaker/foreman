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

// The Mods page's update check is quiet too while the folder is swapped.
func TestUpdatesIsQuietWhileTheWorldFolderIsSwapped(t *testing.T) {
	f := newFixture(t, "visible-planets_1.5.5")
	dir := filepath.Join(f.m.Dirs().Worlds, f.id)
	if err := os.Rename(dir, dir+"-away"); err != nil {
		t.Fatal(err)
	}
	updates, err := f.planner.Updates(context.Background(), f.id)
	if err != nil || len(updates) != 0 {
		t.Errorf("Updates = %+v, %v; want nothing", updates, err)
	}
}
