package world

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readMetaOf(t *testing.T, m *Manager, id string) Meta {
	t.Helper()
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

// SPEC 5.9: archive hides a world without touching its files, and
// unarchive brings it back; no snapshot is taken for either.
func TestSetArchivedRoundTrips(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	seedWorld(t, m)
	id := seedSecond(t, m)
	files := fileTree(t, filepath.Join(m.dirs.Worlds, id))

	if _, err := m.SetArchived(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if !readMetaOf(t, m, id).Archived {
		t.Error("world.json does not say archived")
	}
	sum, err := m.Get(ctx, id)
	if err != nil || !sum.Archived {
		t.Errorf("Get: %+v, %v", sum, err)
	}
	list, _ := m.List(ctx)
	flagged := 0
	for _, s := range list {
		if s.Archived {
			flagged++
		}
	}
	if flagged != 1 {
		t.Errorf("%d archived rows, want 1", flagged)
	}
	if snaps, _ := m.Snapshots(id); len(snaps) != 0 {
		t.Errorf("archiving took a snapshot: %v", snaps)
	}
	// Only world.json changed.
	after := fileTree(t, filepath.Join(m.dirs.Worlds, id))
	for p, f := range files {
		if p != MetaFile && (after[p].body != f.body || !after[p].modTime.Equal(f.modTime)) {
			t.Errorf("%s changed", p)
		}
	}

	if _, err := m.SetArchived(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if readMetaOf(t, m, id).Archived {
		t.Error("still archived")
	}
	b, _ := os.ReadFile(filepath.Join(m.dirs.Worlds, id, MetaFile)) //nolint:gosec // a test temp file
	if strings.Contains(string(b), "archived") {
		t.Errorf("a false flag is left out of world.json: %s", b)
	}
}

// SPEC 5.9: the active world cannot be archived, and the refusal comes
// before anything is written.
func TestSetArchivedRefusals(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	active := seedWorld(t, m)
	other := seedSecond(t, m)

	if _, err := m.SetArchived(ctx, active, true); !errors.Is(err, ErrArchiveActive) {
		t.Errorf("active: %v", err)
	}
	if readMetaOf(t, m, active).Archived {
		t.Error("the active world was archived")
	}
	// Unarchiving the active world is harmless and allowed.
	if _, err := m.SetArchived(ctx, active, false); err != nil {
		t.Errorf("unarchive active: %v", err)
	}
	if _, err := m.SetArchived(ctx, "missing", true); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	unlock, _ := m.Lock(other)
	if _, err := m.SetArchived(ctx, other, true); !errors.Is(err, ErrBusy) {
		t.Errorf("locked: %v", err)
	}
	unlock()

	w := filepath.Join(m.dirs.Worlds, other, MetaFile)
	if err := os.WriteFile(w, []byte(`{"format_version":9,"name":"Future","created_at":"2026-11-01T10:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetArchived(ctx, other, true); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer: %v", err)
	}
}

// SPEC 5.1: a world.json field this Foreman does not know survives.
func TestArchivedKeepsUnknownFields(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	w := filepath.Join(m.dirs.Worlds, id, MetaFile)
	b, _ := os.ReadFile(w) //nolint:gosec // a test temp file
	patched := strings.Replace(string(b), "{", `{"future_field": 7,`, 1)
	if err := os.WriteFile(w, []byte(patched), 0o600); err != nil { //nolint:gosec // a test temp file
		t.Fatal(err)
	}
	if _, err := m.SetArchived(t.Context(), id, true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(w) //nolint:gosec // a test temp file
	if !strings.Contains(string(b), `"future_field": 7`) || !strings.Contains(string(b), `"archived": true`) {
		t.Errorf("world.json: %s", b)
	}
}

// SPEC 5.9, 6.5: a restore keeps the world's current archive state,
// whatever the snapshot's world.json says.
func TestRestoreKeepsCurrentArchivedState(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		snapArchived, liveArchived bool
	}{
		{"snapshot archived, live not", true, false},
		{"snapshot not archived, live archived", false, true},
		{"both archived", true, true},
		{"neither", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestManager(t)
			ctx := context.Background()
			seedWorld(t, m)
			id := seedSecond(t, m)
			if _, err := m.SetArchived(ctx, id, tc.snapArchived); err != nil {
				t.Fatal(err)
			}
			snap, err := m.TakeSnapshot(id, "", "alice")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.SetArchived(ctx, id, tc.liveArchived); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Restore(ctx, id, snap.Name, "alice"); err != nil {
				t.Fatal(err)
			}
			if got := readMetaOf(t, m, id).Archived; got != tc.liveArchived {
				t.Errorf("archived = %v after restore, want %v", got, tc.liveArchived)
			}
		})
	}
}

// A restore that changes nothing about the flag gives back world.json
// exactly, time included (SPEC 6.9).
func TestRestoreStaysExactWhenFlagMatches(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	want := fileTree(t, filepath.Join(m.dirs.Worlds, id))
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "alice"); err != nil {
		t.Fatal(err)
	}
	sameTree(t, fileTree(t, filepath.Join(m.dirs.Worlds, id)), want)
}

// A deleted world has no current state: it comes back unarchived, so an
// archived world can never become the active one.
func TestRestoreDeletedWorldComesBackUnarchived(t *testing.T) {
	m, active := newTestManager(t)
	ctx := context.Background()
	id := seedWorld(t, m)
	other := seedSecond(t, m)
	if _, err := m.SetArchived(ctx, other, true); err != nil {
		t.Fatal(err)
	}
	active.id = ""
	if err := m.Delete(ctx, other, "Second", true, "a"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, id, "Base", true, "a"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.Snapshots(other)
	if _, err := m.Restore(ctx, other, list[0].Name, "a"); err != nil {
		t.Fatal(err)
	}
	if readMetaOf(t, m, other).Archived {
		t.Error("a restored deleted world is still archived")
	}
	if active.id != other {
		t.Errorf("active = %q, want the restored world", active.id)
	}
}

// SPEC 12.9: SetArchived says whether it changed anything, so a repeat is not
// recorded twice.
func TestSetArchivedReportsAChange(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	for i, want := range []struct {
		on, changed bool
	}{{true, true}, {true, false}, {false, true}, {false, false}} {
		changed, err := m.SetArchived(t.Context(), id, want.on)
		if err != nil || changed != want.changed {
			t.Errorf("call %d (on=%v) = %v, %v; want changed=%v", i, want.on, changed, err, want.changed)
		}
	}
}
