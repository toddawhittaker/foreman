package world

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests attack the file-path trust boundary (SPEC 14.5): a name
// from a request or a link on disk must never reach outside the world,
// snapshot or inbox folder it names, and a refusal must change nothing.

// diskTree records every entry under dir, symlinks by their target and
// never followed, so a test can prove an attack changed nothing.
func diskTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = "link:" + target
		case d.IsDir():
			out[rel] = "dir"
		default:
			b, err := os.ReadFile(p) //nolint:gosec // a test temp file
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[rel] = "file:" + string(b) + "@" + info.ModTime().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameDisk(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: %s is %q, want %q", what, p, got[p], w)
		}
	}
	for p, g := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("%s: new entry %s (%q)", what, p, g)
		}
	}
}

// dataDir is the folder holding a test manager's worlds, snapshots and inbox.
func dataDir(m *Manager) string { return filepath.Dir(m.dirs.Worlds) }

// outsideSecret writes a file outside the data folder that no operation
// may read, link, change or delete.
func outsideSecret(t *testing.T) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "secret.zip")
	writeAt(t, file, "TOPSECRET", t0)
	return dir, file
}

var hostileNames = []string{
	"", ".", "..", "../", "../x", "../../etc/passwd", "/etc", "/etc/passwd",
	"a/../b", "a/b", "./a", "a/.", `..\..\x`, "%2e%2e", "..%2f..%2fx", "%2fetc",
	"a\x00b", "a\nb", "a\rb", "\x7f", "a\tb", strings.Repeat("a", 300),
}

// SPEC 14: a hostile world id, save name, snapshot name or inbox folder
// name is refused by every entry point of the Go API, and nothing on disk
// changes.
func TestHostileNamesAreRefusedEverywhere(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "good", "alice")
	if err != nil {
		t.Fatal(err)
	}
	seedInbox(t, m)
	_, secret := outsideSecret(t)
	secretBefore := diskTree(t, filepath.Dir(secret))
	before := diskTree(t, dataDir(m))
	ctx := t.Context()

	saveNames := append([]string{"../world.json", "../../world.json", "/etc/x.zip", "a/b.zip",
		"x\x00.zip", "x\n.zip", ".x.zip", `..\x.zip`, "../saves/base.zip", "../" + filepath.Base(secret)}, hostileNames...)
	snapNames := append([]string{"../../worlds/" + id, snap.Name + "/..", snap.Name + "/../..",
		snap.Name + "\x00", "/" + snap.Name, "../" + id + "/" + snap.Name}, hostileNames...)
	folders := append([]string{"../worlds", "../worlds/" + id, "saves-2025/..", "saves-2025/mods",
		".hidden", "../snapshots/" + id}, hostileNames...)

	for _, bad := range hostileNames {
		if _, err := m.OpenWorld(bad); err == nil {
			t.Errorf("OpenWorld(%q) opened", bad)
		}
		if _, err := m.TakeSnapshot(bad, "x", "x"); err == nil {
			t.Errorf("TakeSnapshot(%q) worked", bad)
		}
		if _, err := m.Restore(context.Background(), bad, snap.Name, "x"); err == nil {
			t.Errorf("Restore(%q, good) worked", bad)
		}
		if err := m.Rename(ctx, bad, "New name"); err == nil {
			t.Errorf("Rename(%q) worked", bad)
		}
		if _, err := m.Saves(bad); err == nil {
			t.Errorf("Saves(%q) worked", bad)
		}
		if _, _, err := m.NewestSave(bad); err == nil {
			t.Errorf("NewestSave(%q) worked", bad)
		}
		if err := m.SetLoadSave(bad, "base.zip"); err == nil {
			t.Errorf("SetLoadSave(%q) worked", bad)
		}
		if list, err := m.Snapshots(bad); err == nil && len(list) > 0 {
			t.Errorf("Snapshots(%q) listed %d", bad, len(list))
		}
	}
	for _, bad := range saveNames {
		if bad == "" {
			continue // the "newest save" choice, which rewrites world.json
		}
		if f, _, err := m.OpenSave(id, bad); err == nil {
			_ = f.Close()
			t.Errorf("OpenSave(%q) opened", bad)
		}
		// An empty name is the "newest save" choice, not a hostile one.
		if err := m.SetLoadSave(id, bad); bad != "" && err == nil {
			t.Errorf("SetLoadSave(id, %q) worked", bad)
		}
		temp := uploadTemp(t, m, id, "upload")
		if err := m.AddSave(id, bad, temp); !errors.Is(err, ErrSaveName) {
			t.Errorf("AddSave(%q) = %v, want ErrSaveName", bad, err)
		}
	}
	for _, bad := range snapNames {
		if _, err := m.Snapshot(id, bad); !errors.Is(err, ErrNoSnapshot) {
			t.Errorf("Snapshot(%q) = %v, want ErrNoSnapshot", bad, err)
		}
		if _, err := m.Restore(context.Background(), id, bad, "x"); err == nil {
			t.Errorf("Restore(id, %q) worked", bad)
		}
	}
	for _, bad := range folders {
		if _, err := m.ImportFolder(ctx, bad, "Imported "+strings.ToValidUTF8(bad, "?"), "2.0.72", false); err == nil {
			t.Errorf("ImportFolder(%q) worked", bad)
		}
	}

	sameDisk(t, "data folder", diskTree(t, dataDir(m)), before)
	sameDisk(t, "outside folder", diskTree(t, filepath.Dir(secret)), secretBefore)
}

// plantWorldLinks puts symlinks in world id: a save pointing outside the
// data folder, a save pointing into world other, and a mods folder entry
// pointing at an outside folder.
func plantWorldLinks(t *testing.T, m *Manager, id, other, secretDir, secret string) {
	t.Helper()
	w := filepath.Join(m.dirs.Worlds, id)
	links := map[string]string{
		"saves/evil.zip":  secret,
		"saves/peer.zip":  filepath.Join(m.dirs.Worlds, other, "saves", "other.zip"),
		"saves/rel.zip":   "../../" + other + "/saves/other.zip",
		"mods/outside":    secretDir,
		"config/peer-cfg": filepath.Join(m.dirs.Worlds, other, "world.json"),
	}
	for p, target := range links {
		if err := os.Symlink(target, filepath.Join(w, p)); err != nil {
			t.Fatal(err)
		}
	}
}

func otherWorld(t *testing.T, m *Manager) string {
	t.Helper()
	other, err := m.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(m.dirs.Worlds, other, "saves", "other.zip"), "OTHERSAVE", t0)
	return other
}

func noLinksUnder(t *testing.T, dir string) {
	t.Helper()
	for p, v := range diskTree(t, dir) {
		if strings.HasPrefix(v, "link:") {
			t.Errorf("symlink %s copied into %s", p, dir)
		}
		if strings.Contains(v, "TOPSECRET") || strings.Contains(v, "OTHERSAVE") {
			t.Errorf("%s in %s holds another folder's data", p, dir)
		}
	}
}

// SPEC 14.5: symlinks planted inside a world are never followed: not by the save
// list, download, load choice or upload, and a snapshot and a restore
// neither copy them nor touch what they point at.
func TestSymlinksInsideAWorldAreNeverFollowed(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	other := otherWorld(t, m)
	secretDir, secret := outsideSecret(t)
	plantWorldLinks(t, m, id, other, secretDir, secret)
	otherBefore := diskTree(t, filepath.Join(m.dirs.Worlds, other))
	outsideBefore := diskTree(t, secretDir)

	list, err := m.Saves(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list.Saves {
		if s.Name != "base.zip" {
			t.Errorf("save list shows %s", s.Name)
		}
	}
	for _, name := range []string{"evil.zip", "peer.zip", "rel.zip"} {
		if f, _, err := m.OpenSave(id, name); err == nil {
			b := make([]byte, 64)
			n, _ := f.Read(b)
			_ = f.Close()
			t.Errorf("OpenSave(%s) opened a symlink, read %q", name, b[:n])
		}
		if err := m.SetLoadSave(id, name); !errors.Is(err, ErrSaveNotFound) {
			t.Errorf("SetLoadSave(%s) = %v, want ErrSaveNotFound", name, err)
		}
	}
	if s, ok, err := m.NewestSave(id); err != nil || !ok || s.Name != "base.zip" {
		t.Errorf("NewestSave = %+v, %v, %v", s, ok, err)
	}

	// A snapshot would lose the links, so it refuses (fail safe), and so
	// would a restore or delete, which need one first.
	worldBefore := diskTree(t, filepath.Join(m.dirs.Worlds, id))
	if _, err := m.TakeSnapshot(id, "with links", "alice"); err == nil {
		t.Error("snapshot of a world with symlinks worked")
	}
	if dirs := snapshotDirs(t, m, id); len(dirs) != 0 {
		t.Errorf("a refused snapshot left %v", dirs)
	}
	if n := linkCount(t, secret); n != 1 {
		t.Errorf("outside file has %d links; a snapshot hard-linked it", n)
	}
	if n := linkCount(t, filepath.Join(m.dirs.Worlds, other, "saves", "other.zip")); n != 1 {
		t.Errorf("other world's save has %d links; a snapshot hard-linked it", n)
	}
	sameDisk(t, "world", diskTree(t, filepath.Join(m.dirs.Worlds, id)), worldBefore)
	sameDisk(t, "other world", diskTree(t, filepath.Join(m.dirs.Worlds, other)), otherBefore)
	sameDisk(t, "outside folder", diskTree(t, secretDir), outsideBefore)
}

// SPEC 14.5: a world whose saves folder is itself a symlink into another world
// cannot list, open, receive or snapshot that world's saves.
func TestSavesFolderSymlinkedToAnotherWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	other := otherWorld(t, m)
	saves := filepath.Join(m.dirs.Worlds, id, "saves")
	if err := os.RemoveAll(saves); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../"+other+"/saves", saves); err != nil {
		t.Fatal(err)
	}
	otherBefore := diskTree(t, filepath.Join(m.dirs.Worlds, other))

	if list, err := m.Saves(id); err == nil && len(list.Saves) > 0 {
		t.Errorf("listed another world's saves: %+v", list.Saves)
	}
	if f, _, err := m.OpenSave(id, "other.zip"); err == nil {
		_ = f.Close()
		t.Error("opened another world's save")
	}
	if err := m.SetLoadSave(id, "other.zip"); err == nil {
		t.Error("chose another world's save")
	}
	// An upload's temp file is made through the world's root, as the
	// upload helper does; it must not land in the other world.
	if root, err := m.OpenWorld(id); err == nil {
		if f, _, err := CreateTemp(root, SavesDir); err == nil {
			_ = f.Close()
			t.Error("an upload's temp file landed in another world")
		}
		_ = root.Close()
	}
	snap, err := m.TakeSnapshot(id, "x", "alice")
	if err == nil {
		noLinksUnder(t, filepath.Join(m.dirs.Snapshots, id, snap.Name))
	}
	sameDisk(t, "other world", diskTree(t, filepath.Join(m.dirs.Worlds, other)), otherBefore)
}

// snapPath is a file inside a snapshot folder.
func snapPath(m *Manager, id, snap, rel string) string {
	return filepath.Join(m.dirs.Snapshots, id, snap, rel)
}

// SPEC 6.5 and 14.5: restore refuses a snapshot with a symlink planted in
// it, before it takes the "Before restore" snapshot or touches the world,
// and never reads what the link points at.
func TestRestoreRefusesSymlinksPlantedInASnapshot(t *testing.T) {
	cases := map[string]func(t *testing.T, m *Manager, id, other, snap, secretDir string){
		"save replaced by link outside": func(t *testing.T, m *Manager, id, _, snap, secretDir string) {
			p := snapPath(m, id, snap, "saves/base.zip")
			writeAt(t, filepath.Join(secretDir, "same.zip"), "TOPSECRE", t0) // same size as "save one"
			_ = os.Remove(p)
			if err := os.Symlink(filepath.Join(secretDir, "same.zip"), p); err != nil {
				t.Fatal(err)
			}
		},
		"save replaced by link to other world": func(t *testing.T, m *Manager, id, other, snap, _ string) {
			p := snapPath(m, id, snap, "saves/base.zip")
			writeAt(t, filepath.Join(m.dirs.Worlds, other, "saves", "base.zip"), "OTHERSAV", t0)
			_ = os.Remove(p)
			if err := os.Symlink(filepath.Join(m.dirs.Worlds, other, "saves", "base.zip"), p); err != nil {
				t.Fatal(err)
			}
		},
		"config folder replaced by link outside": func(t *testing.T, m *Manager, id, _, snap, secretDir string) {
			p := snapPath(m, id, snap, "config")
			writeAt(t, filepath.Join(secretDir, "server.json"), `TOPSECRET`[:7], t0) // `{"a":1}` is 7 bytes
			_ = os.RemoveAll(p)
			if err := os.Symlink(secretDir, p); err != nil {
				t.Fatal(err)
			}
		},
		"snapshot folder replaced by link to other world's snapshot": func(t *testing.T, m *Manager, id, other, snap, _ string) {
			os2, err := m.TakeSnapshot(other, "theirs", "bob")
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(m.dirs.Snapshots, id, snap)
			_ = os.RemoveAll(p)
			if err := os.Symlink(filepath.Join(m.dirs.Snapshots, other, os2.Name), p); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, plant := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := newTestManager(t)
			id := seedWorld(t, m)
			other := otherWorld(t, m)
			snap, err := m.TakeSnapshot(id, "target", "alice")
			if err != nil {
				t.Fatal(err)
			}
			secretDir, _ := outsideSecret(t)
			plant(t, m, id, other, snap.Name, secretDir)
			worldBefore := diskTree(t, filepath.Join(m.dirs.Worlds, id))
			otherBefore := diskTree(t, filepath.Join(m.dirs.Worlds, other))
			outsideBefore := diskTree(t, secretDir)
			count := len(snapshotDirs(t, m, id))

			if _, err := m.Restore(context.Background(), id, snap.Name, "alice"); err == nil {
				t.Fatal("restore of a tampered snapshot worked")
			}
			sameDisk(t, "world", diskTree(t, filepath.Join(m.dirs.Worlds, id)), worldBefore)
			sameDisk(t, "other world", diskTree(t, filepath.Join(m.dirs.Worlds, other)), otherBefore)
			sameDisk(t, "outside folder", diskTree(t, secretDir), outsideBefore)
			if got := len(snapshotDirs(t, m, id)); got != count {
				t.Errorf("snapshots went from %d to %d for a refused restore", count, got)
			}
			noLeftovers(t, m)
		})
	}
}

// SPEC 14.5: a world's snapshots folder that is a symlink to another world's
// snapshots folder must not let a snapshot of one world land among the
// other's, nor a restore read the other's.
func TestSnapshotsFolderSymlinkedToAnotherWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	other := otherWorld(t, m)
	theirs, err := m.TakeSnapshot(other, "theirs", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(m.dirs.Snapshots, id)); err != nil {
		t.Fatal(err)
	}
	otherSnaps := filepath.Join(m.dirs.Snapshots, other)
	before := diskTree(t, otherSnaps)
	worldBefore := diskTree(t, filepath.Join(m.dirs.Worlds, id))

	if _, err := m.Restore(context.Background(), id, theirs.Name, "alice"); err == nil {
		t.Error("restored world " + id + " from another world's snapshot")
	}
	_, _ = m.TakeSnapshot(id, "mine", "alice")
	sameDisk(t, "other world's snapshots", diskTree(t, otherSnaps), before)
	sameDisk(t, "world", diskTree(t, filepath.Join(m.dirs.Worlds, id)), worldBefore)
}

// snapshotDirs lists a world's snapshot folders, leftovers included.
func snapshotDirs(t *testing.T, m *Manager, id string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(m.dirs.Snapshots, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// noLeftovers fails on any dot-named entry in the worlds folder or in any
// world's snapshots folder: unfinished work a failure left behind.
func noLeftovers(t *testing.T, m *Manager) {
	t.Helper()
	assertNoStaging(t, m)
	entries, _ := os.ReadDir(m.dirs.Snapshots)
	for _, e := range entries {
		for _, s := range snapshotDirs(t, m, e.Name()) {
			if strings.HasPrefix(s, ".") {
				t.Errorf("left behind snapshots/%s/%s", e.Name(), s)
			}
		}
	}
}

// editManifest rewrites one snapshot's snapshot.json with edit applied.
func editManifest(t *testing.T, m *Manager, id, snap string, edit func(*Manifest)) {
	t.Helper()
	p := snapPath(m, id, snap, ManifestFile)
	b, err := os.ReadFile(p) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	var man Manifest
	if err := json.Unmarshal(b, &man); err != nil {
		t.Fatal(err)
	}
	edit(&man)
	b, err = json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// SPEC 6.5, 14: a snapshot.json edited to name files outside the snapshot
// is refused before anything is written, and no file outside is read.
func TestRestoreRefusesManifestPointingOutside(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	other := otherWorld(t, m)
	snap, err := m.TakeSnapshot(id, "target", "alice")
	if err != nil {
		t.Fatal(err)
	}
	otherMeta := filepath.Join(m.dirs.Worlds, other, MetaFile)
	info, err := os.Stat(otherMeta)
	if err != nil {
		t.Fatal(err)
	}
	size := info.Size()
	bad := []string{
		"../../../worlds/" + other + "/" + MetaFile,
		"../" + other + "/whatever",
		"/etc/passwd",
		filepath.Join(m.dirs.Worlds, other, MetaFile),
		"saves/../../../../worlds/" + other + "/" + MetaFile,
		"saves/../" + ManifestFile,
		"./saves/base.zip",
		".",
		"",
		ManifestFile,
		"saves",
		"saves/base.zip\x00",
	}
	for _, p := range bad {
		t.Run(strings.ToValidUTF8(p, "?"), func(t *testing.T) {
			editManifest(t, m, id, snap.Name, func(man *Manifest) {
				man.Files = append(man.Files, ManifestEntry{Path: p, Size: size, ModTime: t0})
			})
			defer editManifest(t, m, id, snap.Name, func(man *Manifest) { man.Files = man.Files[:len(man.Files)-1] })
			worldBefore := diskTree(t, filepath.Join(m.dirs.Worlds, id))
			count := len(snapshotDirs(t, m, id))
			if _, err := m.Restore(context.Background(), id, snap.Name, "alice"); err == nil {
				t.Fatal("restore of a manifest naming an outside file worked")
			}
			sameDisk(t, "world", diskTree(t, filepath.Join(m.dirs.Worlds, id)), worldBefore)
			if got := len(snapshotDirs(t, m, id)); got != count {
				t.Errorf("a refused restore took a snapshot (%d to %d)", count, got)
			}
			noLeftovers(t, m)
		})
	}
	// The untouched snapshot still restores, so the refusals were for the
	// planted entries, not a broken fixture.
	if _, err := m.Restore(context.Background(), id, snap.Name, "alice"); err != nil {
		t.Errorf("clean restore: %v", err)
	}
}

// SPEC 5.5, 14.5: symlinks in the import inbox, whether they name a folder
// or a file inside one, and whether they point outside the data folder or
// at another world, are neither listed nor imported, and the source is not
// changed.
func TestInboxSymlinksAreNeverFollowed(t *testing.T) {
	m, _ := newTestManager(t)
	victim := seedWorld(t, m)
	secretDir, secret := outsideSecret(t)
	write(t, filepath.Join(secretDir, "mods", "evil.zip"), "TOPSECRET-MOD", t0)
	inbox := m.dirs.Imports
	real := filepath.Join(inbox, "real")
	write(t, filepath.Join(real, "ok.zip"), "fine", t0)
	links := map[string]string{
		filepath.Join(inbox, "outside"):       secretDir,
		filepath.Join(inbox, "peer"):          filepath.Join(m.dirs.Worlds, victim),
		filepath.Join(inbox, "peer-saves"):    filepath.Join(m.dirs.Worlds, victim, "saves"),
		filepath.Join(inbox, "rel"):           "../worlds/" + victim,
		filepath.Join(real, "evil.zip"):       secret,
		filepath.Join(real, "victim.zip"):     filepath.Join(m.dirs.Worlds, victim, "saves", "base.zip"),
		filepath.Join(real, "mods"):           filepath.Join(secretDir, "mods"),
		filepath.Join(inbox, "real-too.link"): real,
	}
	for at, target := range links {
		if err := os.Symlink(target, at); err != nil {
			t.Fatal(err)
		}
	}
	inboxBefore := diskTree(t, inbox)
	victimBefore := diskTree(t, filepath.Join(m.dirs.Worlds, victim))
	outsideBefore := diskTree(t, secretDir)

	folders, err := m.Inbox()
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Name != "real" || folders[0].Saves != 1 || folders[0].HasMods {
		t.Errorf("inbox lists %+v, want only real with one save and no mods", folders)
	}
	for _, f := range []string{"outside", "peer", "peer-saves", "rel", "real-too.link"} {
		if _, err := m.ImportFolder(t.Context(), f, "From "+f, "2.0.72", false); !errors.Is(err, ErrNoFolder) {
			t.Errorf("ImportFolder(%s) = %v, want ErrNoFolder", f, err)
		}
	}
	id, err := m.ImportFolder(t.Context(), "real", "Real", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	got := diskTree(t, filepath.Join(m.dirs.Worlds, id))
	noLinksUnder(t, filepath.Join(m.dirs.Worlds, id))
	for p := range got {
		if strings.Contains(p, "evil") || strings.Contains(p, "victim") {
			t.Errorf("imported symlink %s", p)
		}
	}
	if n := linkCount(t, secret); n != 1 {
		t.Errorf("outside file has %d links", n)
	}
	sameDisk(t, "inbox", diskTree(t, inbox), inboxBefore)
	sameDisk(t, "victim world", diskTree(t, filepath.Join(m.dirs.Worlds, victim)), victimBefore)
	sameDisk(t, "outside folder", diskTree(t, secretDir), outsideBefore)
}
