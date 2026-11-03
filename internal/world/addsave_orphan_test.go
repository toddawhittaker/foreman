package world

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SPEC 5.11 and 6.6: when a restore swaps the world out while an upload
// waits for the lock, and the old folder survives with the temp file in it,
// AddSave must still refuse. Filing the save in the old folder would lose it
// at the next startup cleanup.
func TestAddSaveRefusesAnOrphanedFolderThatKeptItsTempFile(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	temp := uploadTemp(t, m, id, "new save")
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.AddSave(id, "new.zip", temp) }()
	time.Sleep(150 * time.Millisecond) // AddSave has opened the folder and waits

	world := filepath.Join(m.dirs.Worlds, id)
	old := filepath.Join(m.dirs.Worlds, PrefixRestoreOld+id)
	if err := os.Rename(world, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(world, SavesDir), 0o750); err != nil {
		t.Fatal(err)
	}
	unlock()

	if err := <-done; !errors.Is(err, ErrWorldChanged) {
		t.Fatalf("got %v, want ErrWorldChanged", err)
	}
	for _, dir := range []string{world, old} {
		if _, err := os.Lstat(filepath.Join(dir, SavesDir, "new.zip")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the save was filed in %s: %v", dir, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(old, temp)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp file was left behind: %v", err)
	}
}
