package world

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// SPEC 5.11: a busy world must not throw away a finished upload; AddSave
// waits for the world's lock.
func TestAddSaveWaitsForABusyWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	temp := uploadTemp(t, m, id, "new save")
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		unlock()
	}()
	if err := m.AddSave(id, "new.zip", temp); err != nil {
		t.Fatalf("AddSave while the world was briefly busy: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Worlds, id, SavesDir, "new.zip")); err != nil {
		t.Errorf("the save was not added: %v", err)
	}
}

// When the world stays busy past the wait, the upload is refused and its
// temp file is removed.
func TestAddSaveGivesUpOnAWorldThatStaysBusy(t *testing.T) {
	m, _ := newTestManager(t)
	m.addSaveWait = 100 * time.Millisecond
	id := seedWorld(t, m)
	temp := uploadTemp(t, m, id, "new save")
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	start := time.Now()
	if err := m.AddSave(id, "new.zip", temp); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v, want ErrBusy", err)
	}
	if time.Since(start) < m.addSaveWait {
		t.Error("gave up before the wait was over")
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Worlds, id, temp)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temp file was left behind: %v", err)
	}
}

// SPEC 5.4: a world whose saves folder cannot be read is still listed, with
// its last played time unknown.
func TestListShowsLastPlayedUnknown(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	m, _ := newTestManager(t)
	bad := seedWorld(t, m)
	good, err := m.Create(t.Context(), "Zed", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	saves := filepath.Join(m.dirs.Worlds, bad, SavesDir)
	if err := os.Chmod(saves, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(saves, 0o755) }) //nolint:gosec // a test folder

	list, err := m.List(t.Context())
	if err != nil {
		t.Fatalf("one unreadable saves folder broke the list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("listed %d worlds, want 2", len(list))
	}
	for _, s := range list {
		switch s.ID {
		case bad:
			if !s.LastPlayedUnknown {
				t.Errorf("%s: last played should be unknown", s.ID)
			}
		case good:
			if s.LastPlayedUnknown {
				t.Errorf("%s: last played is known", s.ID)
			}
		}
	}
	got, err := m.Get(t.Context(), bad)
	if err != nil || !got.LastPlayedUnknown {
		t.Errorf("Get: %+v, %v; want last played unknown", got, err)
	}
}

// The web layer shows this wording as it is.
func TestErrWorldChangedWording(t *testing.T) {
	const want = "This world was changed or deleted while your upload was running. Nothing was saved; upload it again."
	if got := ErrWorldChanged.Error(); got != want {
		t.Errorf("got %q", got)
	}
}

// Tests that lower the reserve must be race-safe next to code that reads it.
func TestSpaceReserveCanBeChangedWhileInUse(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				restore := SetSpaceReserveForTest(0)
				restore()
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				if release, err := Reserve(dir, 1); err == nil {
					release()
				}
			}
		}()
	}
	wg.Wait()
}

// SPEC 5.10, fail safe: a world whose world.json cannot be read is never
// deleted, with or without a final snapshot, and nothing is written.
func TestDeleteRefusesAWorldWithUnreadableMeta(t *testing.T) {
	for _, final := range []bool{true, false} {
		m, _ := newTestManager(t)
		seedWorld(t, m)
		id := seedSecond(t, m)
		rewriteMeta(t, filepath.Join(m.dirs.Worlds, id), "{not json")
		before := diskTree(t, dataDir(m))
		if err := m.Delete(t.Context(), id, id, final, "a"); !errors.Is(err, ErrUnreadable) {
			t.Fatalf("final snapshot %v: got %v, want ErrUnreadable", final, err)
		}
		sameDisk(t, "data folder", diskTree(t, dataDir(m)), before)
	}
}
