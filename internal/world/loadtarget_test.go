package world

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// LoadTarget names the save PreStartCheckAt reads: the newest, or the one
// world.json chooses, and reports none where the check reports no save.
func TestLoadTargetMatchesPreStartCheck(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModList(t, dir, "base")

	// No save: both say so.
	if _, found, err := m.LoadTarget(id); err != nil || found {
		t.Fatalf("no save: found %v, err %v", found, err)
	}
	wantKinds(t, runCheck(t, m, id), ProblemNoSave)

	putFixture(t, dir, "2.0.77-no-space-age.zip", "old.zip", t0)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "new.zip", t0.Add(time.Hour))

	// The newest by default.
	target, found, err := m.LoadTarget(id)
	if err != nil || !found {
		t.Fatalf("newest: found %v, err %v", found, err)
	}
	if check := runCheck(t, m, id); target.Name != "new.zip" || check.Save != target.Name {
		t.Fatalf("newest: target %q, check %q", target.Name, check.Save)
	}
	if target.Size == 0 || !target.ModTime.Equal(t0.Add(time.Hour)) {
		t.Fatalf("newest: size %d, modified %v", target.Size, target.ModTime)
	}

	// The chosen save when set.
	if err := m.SetLoadSave(id, "old.zip"); err != nil {
		t.Fatal(err)
	}
	target, found, err = m.LoadTarget(id)
	if err != nil || !found {
		t.Fatalf("chosen: found %v, err %v", found, err)
	}
	if check := runCheck(t, m, id); target.Name != "old.zip" || check.Save != target.Name {
		t.Fatalf("chosen: target %q, check %q", target.Name, check.Save)
	}
	if !target.ModTime.Equal(t0) {
		t.Fatalf("chosen: modified %v", target.ModTime)
	}
}

// A chosen save that is gone is no target, as the check refuses it.
func TestLoadTargetChosenSaveGone(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModList(t, dir, "base")
	putFixture(t, dir, "2.0.77-no-space-age.zip", "a.zip", t0)
	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, SavesDir, "a.zip")); err != nil {
		t.Fatal(err)
	}
	putFixture(t, dir, "2.0.77-no-space-age.zip", "b.zip", t0)
	if _, found, err := m.LoadTarget(id); err != nil || found {
		t.Fatalf("found %v, err %v", found, err)
	}
	wantKinds(t, runCheck(t, m, id), ProblemNoSave)
}

// A world that does not exist is an error, not "no save".
func TestLoadTargetNoWorld(t *testing.T) {
	m, _ := newTestManager(t)
	if _, _, err := m.LoadTarget("nope"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err %v", err)
	}
}
