package world

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// SPEC 6.12: deleting a snapshot removes only its folder. Zips it shares by
// hard link with the world and other snapshots stay, and the world is not
// touched.
func TestDeleteSnapshotRemovesOnlyThatSnapshot(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	first, err := m.TakeSnapshot(id, "one", "a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.TakeSnapshot(id, "two", "a")
	if err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(m.dirs.Worlds, id)
	before := diskTree(t, worldDir)

	if err := m.DeleteSnapshot(id, first.Name, noHook); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Snapshots, id, first.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("snapshot folder still there: %v", err)
	}
	sameDisk(t, "world", diskTree(t, worldDir), before)
	if b, err := os.ReadFile(filepath.Join(m.dirs.Snapshots, id, second.Name, "saves/base.zip")); err != nil || string(b) != "save one" { //nolint:gosec // a test temp file
		t.Errorf("other snapshot's linked zip: %q %v", b, err)
	}
	list, err := m.Snapshots(id)
	if err != nil || len(list) != 1 || list[0].Name != second.Name {
		t.Errorf("left %+v %v", list, err)
	}
	if err := m.DeleteSnapshot(id, first.Name, noHook); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("deleting twice: %v", err)
	}
	if _, err := m.Restore(t.Context(), id, second.Name, "a"); err != nil {
		t.Errorf("the other snapshot no longer restores: %v", err)
	}
}

func TestDeleteSnapshotRefusesBadNamesAndBusyWorlds(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	s, err := m.TakeSnapshot(id, "one", "a")
	if err != nil {
		t.Fatal(err)
	}
	snapsBefore := diskTree(t, m.dirs.Snapshots)
	worldsBefore := diskTree(t, m.dirs.Worlds)
	for _, bad := range []struct{ id, name string }{
		{id, "../" + s.Name}, {id, ".."}, {id, "."}, {id, ""}, {id, s.Name + "/saves"},
		{"..", id}, {"../worlds", id}, {id, "x" + s.Name}, {"nope", s.Name},
		{id, "20261103T142501Z-99"},
	} {
		if err := m.DeleteSnapshot(bad.id, bad.name, noHook); !errors.Is(err, ErrNoSnapshot) {
			t.Errorf("DeleteSnapshot(%q, %q) = %v, want ErrNoSnapshot", bad.id, bad.name, err)
		}
	}
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteSnapshot(id, s.Name, noHook); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	unlock()
	stop := errors.New("audit failed")
	if err := m.DeleteSnapshot(id, s.Name, func(got Snapshot) error {
		if got.Label != "one" {
			t.Errorf("hook saw %+v", got)
		}
		return stop
	}); !errors.Is(err, stop) {
		t.Errorf("a failing hook: %v", err)
	}
	sameDisk(t, "snapshots", diskTree(t, m.dirs.Snapshots), snapsBefore)
	sameDisk(t, "worlds", diskTree(t, m.dirs.Worlds), worldsBefore)
}

// SPEC 6.12: a zip the live world also holds by hard link frees nothing, so
// the snapshots add only the files they alone hold.
func TestSnapshotsAddLeavesOutFilesTheWorldHolds(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	a, err := m.TakeSnapshot(id, "one", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.TakeSnapshot(id, "two", "a")
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.CountSnapshots()
	if err != nil || len(c.Snapshots()) != 2 {
		t.Fatalf("count %+v %v", c, err)
	}
	// The zips (8 + 3 bytes) are shared with the world; the copied small
	// files are the snapshots' own.
	// Each snapshot.json is its own, so it frees with its snapshot.
	own := map[string]uint64{
		a.Name: a.Size() - 11 + manifestSize(t, m, id, a.Name),
		b.Name: b.Size() - 11 + manifestSize(t, m, id, b.Name),
	}
	for _, u := range c.Snapshots() {
		if u.WorldID != id || u.Frees != own[u.Name] {
			t.Errorf("%s: world %q frees %d, want %d", u.Name, u.WorldID, u.Frees, own[u.Name])
		}
	}
	both := []SnapshotRef{{id, a.Name}, {id, b.Name}}
	want := own[a.Name] + own[b.Name]
	if got := c.Frees(both); got != want {
		t.Errorf("both free %d, want %d", got, want)
	}
	if got := c.Total(); got != want {
		t.Errorf("total %d, want %d", got, want)
	}
}

// seedPlayedOn makes a world with three snapshots, then plays on. The save
// "save one" is held by the first snapshot alone, "save two!" by the second
// and third together, and the world holds a newer save; the mod zip is
// shared with the world throughout.
func seedPlayedOn(t *testing.T) (m *Manager, id string, snaps []Snapshot) {
	t.Helper()
	m, _ = newTestManager(t)
	id = seedWorld(t, m)
	save := filepath.Join(m.dirs.Worlds, id, "saves/base.zip")
	replace := func(body string) {
		if err := os.Remove(save); err != nil {
			t.Fatal(err)
		}
		writeAt(t, save, body, t0)
	}
	take := func() {
		s, err := m.TakeSnapshot(id, "snap", "a")
		if err != nil {
			t.Fatal(err)
		}
		snaps = append(snaps, s)
	}
	take()
	replace("save two!")
	take()
	take()
	replace("the world has moved on")
	return m, id, snaps
}

// manifestSize is the size of a snapshot's snapshot.json on disk.
func manifestSize(t *testing.T, m *Manager, id, snap string) uint64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(m.dirs.Snapshots, id, snap, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	return uint64(info.Size()) //nolint:gosec // a size is never negative
}

// distinctBytes is the disk the files under roots take, each inode once.
// A snapshot.json counts like any file (SPEC 6.12).
func distinctBytes(t *testing.T, roots ...string) uint64 {
	t.Helper()
	seen := map[inodeKey]bool{}
	var n uint64
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			st := info.Sys().(*syscall.Stat_t)    //nolint:forcetypeassert // Linux only
			k := inodeKey{uint64(st.Dev), st.Ino} //nolint:gosec,unconvert // types differ by platform
			if !seen[k] {
				seen[k] = true
				n += uint64(info.Size()) //nolint:gosec // a file size is never negative
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return n
}

// SPEC 6.12: what a selection says it frees is what deleting it frees, for
// every selection, and a file two snapshots share frees only when both go.
func TestFreesMatchesTheDiskForEverySelection(t *testing.T) {
	for mask := 1; mask < 8; mask++ {
		t.Run(fmt.Sprintf("selection %03b", mask), func(t *testing.T) {
			m, id, snaps := seedPlayedOn(t)
			c, err := m.CountSnapshots()
			if err != nil {
				t.Fatal(err)
			}
			var picked []SnapshotRef
			for i, s := range snaps {
				if mask&(1<<i) != 0 {
					picked = append(picked, SnapshotRef{id, s.Name})
				}
			}
			want := c.Frees(picked)
			before := distinctBytes(t, m.dirs.Snapshots, m.dirs.Worlds)
			for _, ref := range picked {
				if err := m.DeleteSnapshot(ref.WorldID, ref.Name, noHook); err != nil {
					t.Fatal(err)
				}
			}
			if got := before - distinctBytes(t, m.dirs.Snapshots, m.dirs.Worlds); got != want {
				t.Errorf("disk fell by %d, the selection said %d", got, want)
			}
		})
	}
}

// The selection, each world's column and the total come from one pass, so
// they agree.
func TestSnapshotFiguresAgree(t *testing.T) {
	m, id, snaps := seedPlayedOn(t)
	c, err := m.CountSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	// What each snapshot holds alone: its small files and its snapshot.json.
	own := func(i int) uint64 {
		zips := uint64(8 + 3) // "save one" and the mod zip
		if i > 0 {
			zips = 9 + 3 // "save two!" and the mod zip
		}
		return snaps[i].Size() - zips + manifestSize(t, m, id, snaps[i].Name)
	}
	alone := map[string]uint64{}
	var all []SnapshotRef
	for _, u := range c.Snapshots() {
		if want := u.Size() + manifestSize(t, m, id, u.Name); u.DiskSize() != want {
			t.Errorf("%s size %d, want %d with its snapshot.json", u.Name, u.DiskSize(), want)
		}
		alone[u.Name] = u.Frees
		all = append(all, SnapshotRef{id, u.Name})
	}
	// The two that share "save two!" free only their copies on their own.
	want := map[string]uint64{snaps[0].Name: own(0) + 8, snaps[1].Name: own(1), snaps[2].Name: own(2)}
	for name, w := range want {
		if alone[name] != w {
			t.Errorf("%s alone frees %d, want %d", name, alone[name], w)
		}
	}
	if got := c.Frees([]SnapshotRef{{id, snaps[1].Name}, {id, snaps[2].Name}}); got != own(1)+own(2)+9 {
		t.Errorf("the pair frees %d, want %d", got, own(1)+own(2)+9)
	}
	total := own(0) + own(1) + own(2) + 8 + 9
	if got := c.Total(); got != total {
		t.Errorf("total %d, want %d", got, total)
	}
	if got := c.Frees(all); got != total {
		t.Errorf("selecting everything frees %d, total is %d", got, total)
	}
	if got := c.WorldAdds()[id]; got != total {
		t.Errorf("the world adds %d, total is %d", got, total)
	}
	one, err := m.CountWorldSnapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	if one.Total() != total || one.Frees(all) != total || one.WorldAdds()[id] != total {
		t.Error("the one-world count differs from the full count")
	}
}

// SPEC 6.12: each world's column is its own snapshots' share, and with no
// file shared across worlds the columns add up to the total.
func TestWorldAddsSumToTotal(t *testing.T) {
	m, _ := newTestManager(t)
	var ids []string
	for _, name := range []string{"Alpha", "Beta"} {
		id, err := m.Create(t.Context(), name, "2.0.72", true)
		if err != nil {
			t.Fatal(err)
		}
		save := filepath.Join(m.dirs.Worlds, id, "saves/base.zip")
		writeAt(t, save, "first "+name, t0)
		if _, err := m.TakeSnapshot(id, "x", "a"); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(save); err != nil {
			t.Fatal(err)
		}
		writeAt(t, save, "later "+name, t0)
		ids = append(ids, id)
	}
	c, err := m.CountSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	adds := c.WorldAdds()
	if len(adds) != 2 || adds[ids[0]] == 0 || adds[ids[0]]+adds[ids[1]] != c.Total() {
		t.Errorf("adds %v, total %d", adds, c.Total())
	}
	for _, id := range ids {
		one, err := m.CountWorldSnapshots(id)
		if err != nil || one.Total() != adds[id] {
			t.Errorf("%s alone adds %d, want %d (%v)", id, one.Total(), adds[id], err)
		}
	}
	if none, err := m.CountWorldSnapshots("nope"); err != nil || len(none.Snapshots()) != 0 || none.Total() != 0 {
		t.Errorf("unknown world: %+v %v", none, err)
	}
}

func noHook(Snapshot) error { return nil }
