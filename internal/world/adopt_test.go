package world

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// origFile is what adoption must leave alone in an original file.
type origFile struct {
	body  string
	ino   uint64
	mtime time.Time
	mode  fs.FileMode
}

// adoptRig is an adopted-mode layout: worlds inside the game folder,
// snapshots and the rest in the data folder (SPEC 4.8, 5.2).
type adoptRig struct {
	m    *Manager
	act  *fakeActive
	game string
	data string
}

// newAdoptRig makes a game folder as a real one looks before adoption.
func newAdoptRig(t *testing.T) *adoptRig {
	t.Helper()
	game, data := t.TempDir(), t.TempDir()
	seed := map[string]string{
		"saves/Family.zip":            "main save",
		"saves/_autosave1.zip":        "autosave one",
		"saves/_autosave2.zip":        "autosave two",
		"mods/mod-list.json":          `{"mods":[{"name":"base","enabled":true}]}`,
		"mods/mod-settings.dat":       "settings",
		"mods/flib_0.15.0.zip":        "flib",
		"mods/sub/extra_1.0.0.zip":    "nested mod",
		"config/server-settings.json": `{"name":"x"}`,
		"config/rconpw":               "hunter2\n",
	}
	for p, body := range seed {
		full := filepath.Join(game, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, "rconpw") {
			mode = 0o600
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil { //nolint:gosec // test files
			t.Fatal(err)
		}
		mt := t0.Add(-time.Duration(len(p)) * time.Hour).Add(987654321)
		if err := os.Chtimes(full, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	return newAdoptRigAt(t, game, data, filepath.Join(game, "worlds"))
}

func newAdoptRigAt(t *testing.T, game, data, worlds string) *adoptRig {
	t.Helper()
	act := &fakeActive{}
	m, err := NewManager(Dirs{
		Worlds:    worlds,
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, act, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	m.log = slog.New(slog.DiscardHandler)
	return &adoptRig{m: m, act: act, game: game, data: data}
}

// originals records every file under the game folder's three folders.
func (r *adoptRig) originals(t *testing.T) map[string]origFile {
	t.Helper()
	out := map[string]origFile{}
	for _, sub := range []string{SavesDir, ModsDir, ConfigDir} {
		err := filepath.WalkDir(filepath.Join(r.game, sub), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := os.Lstat(p)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p) //nolint:gosec // a test temp file
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(r.game, p)
			out[rel] = origFile{string(b), info.Sys().(*syscall.Stat_t).Ino, info.ModTime(), info.Mode()}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func (r *adoptRig) adopt(t *testing.T) (string, Snapshot, error) {
	t.Helper()
	id, err := r.m.ReserveID(t.Context(), "Family base")
	if err != nil {
		t.Fatal(err)
	}
	game, err := os.OpenRoot(r.game)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = game.Close() }()
	unlock, err := r.m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	snap, err := r.m.Adopt(t.Context(), game, id, "Family base", "2.0.72", true, "alice")
	return id, snap, err
}

func TestAdoptBuildsTheWorldAndKeepsTheOriginals(t *testing.T) {
	r := newAdoptRig(t)
	before := r.originals(t)

	id, snap, err := r.adopt(t)
	if err != nil {
		t.Fatal(err)
	}

	// The originals are the same bytes, inodes, times and modes (SPEC 4.8).
	after := r.originals(t)
	if len(after) != len(before) {
		t.Fatalf("file count %d, was %d", len(after), len(before))
	}
	for p, b := range before {
		if a := after[p]; a != b {
			t.Errorf("%s changed: %+v, was %+v", p, a, b)
		}
	}

	world := filepath.Join(r.m.dirs.Worlds, id)
	for p, orig := range before {
		w, err := os.Lstat(filepath.Join(world, p))
		if err != nil {
			t.Fatalf("%s missing from the world: %v", p, err)
		}
		body, _ := os.ReadFile(filepath.Join(world, p)) //nolint:gosec // a test temp file
		if string(body) != orig.body {
			t.Errorf("%s holds %q, want %q", p, body, orig.body)
		}
		if !w.ModTime().Equal(orig.mtime) {
			t.Errorf("%s time %v, want %v", p, w.ModTime(), orig.mtime)
		}
		sameInode := w.Sys().(*syscall.Stat_t).Ino == orig.ino
		if wantLink := strings.HasSuffix(p, ".zip"); sameInode != wantLink {
			t.Errorf("%s hard-linked is %v, want %v", p, sameInode, wantLink)
		}
	}
	if m := mode(t, filepath.Join(world, RCONPasswordFile)); m != 0o640 {
		t.Errorf("rconpw mode %o, want 640", m)
	}

	root, err := r.m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "Family base" || meta.GameVersion != "2.0.72" || !meta.SpaceAge || meta.FormatVersion != FormatVersion {
		t.Errorf("meta %+v", meta)
	}

	if snap.Reason != ReasonAdoption || snap.Label != LabelBeforeAdoption || snap.TakenBy != "alice" || !snap.Automatic() {
		t.Errorf("snapshot %+v", snap.Manifest)
	}
	listed, err := r.m.Snapshots(id)
	if err != nil || len(listed) != 1 || listed[0].Name != snap.Name || len(listed[0].Files) != len(before)+1 /* world.json */ {
		t.Fatalf("snapshots %v, %v", listed, err)
	}
	// Original, world and snapshot share one inode per zip.
	if st := statT(t, filepath.Join(r.game, "saves/Family.zip")); st.Nlink != 3 {
		t.Errorf("Family.zip has %d links, want 3", st.Nlink)
	}

	if r.act.id != "" {
		t.Errorf("adoption made %q active; the caller does that after the cut-over", r.act.id)
	}
	entries, _ := os.ReadDir(r.m.dirs.Worlds)
	if len(entries) != 1 || entries[0].Name() != id {
		t.Errorf("worlds folder holds %v, want only %s", entries, id)
	}
}

// SPEC 6.8, 6.11: when the world is on another filesystem the zips cannot
// be linked, so they are copied. The originals stay as they were.
func TestAdoptCopiesZipsAcrossFilesystems(t *testing.T) {
	shm, err := os.MkdirTemp("/dev/shm", "foreman-test-")
	if err != nil {
		t.Skip("no /dev/shm")
	}
	t.Cleanup(func() { _ = os.RemoveAll(shm) })
	src := newAdoptRig(t)
	if statT(t, shm).Dev == statT(t, src.game).Dev {
		t.Skip("/dev/shm is on the same filesystem")
	}
	r := newAdoptRigAt(t, src.game, src.data, filepath.Join(shm, "worlds"))
	before := r.originals(t)

	id, _, err := r.adopt(t)
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range r.originals(t) {
		if b != before[p] {
			t.Errorf("%s changed", p)
		}
	}
	w := filepath.Join(r.m.dirs.Worlds, id, "saves/Family.zip")
	body, err := os.ReadFile(w) //nolint:gosec // a test temp file
	if err != nil || string(body) != "main save" {
		t.Errorf("copied save %q, %v", body, err)
	}
	if statT(t, w).Ino == before["saves/Family.zip"].ino && statT(t, w).Dev == statT(t, src.game).Dev {
		t.Error("the zip was not copied")
	}
}

// SPEC 5.5: a build that stops partway leaves no world. Cancelling after
// the files are placed is such a stop.
func TestAdoptStoppedPartwayLeavesNoWorld(t *testing.T) {
	r := newAdoptRig(t)
	before := r.originals(t)
	id, err := r.m.ReserveID(t.Context(), "Family base")
	if err != nil {
		t.Fatal(err)
	}
	game, err := os.OpenRoot(r.game)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = game.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.m.Adopt(ctx, game, id, "Family base", "2.0.72", true, "alice"); err == nil {
		t.Fatal("a cancelled adoption succeeded")
	}
	entries, _ := os.ReadDir(r.m.dirs.Worlds)
	if len(entries) != 0 {
		t.Errorf("worlds folder holds %v", entries)
	}
	if snaps, _ := r.m.Snapshots(id); len(snaps) != 0 {
		t.Errorf("snapshots %v", snaps)
	}
	for p, b := range r.originals(t) {
		if b != before[p] {
			t.Errorf("%s changed", p)
		}
	}
}

// A crash leaves a staging folder. It never shows as a world, and startup
// recovery removes it (SPEC 5.5).
func TestAdoptCrashedStagingIsNotAWorldAndRecoveryClearsIt(t *testing.T) {
	r := newAdoptRig(t)
	staging := filepath.Join(r.m.dirs.Worlds, PrefixStaging+"new-0123456789abcdef")
	if err := os.MkdirAll(filepath.Join(staging, "saves"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "saves/half.zip"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := r.m.List(t.Context())
	if err != nil || len(list) != 0 {
		t.Fatalf("list %v, %v", list, err)
	}
	if err := r.m.Recover(slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(staging); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("staging folder survived recovery: %v", err)
	}
}

func TestAdoptRemovesTheWorldWhenTheSnapshotFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder modes")
	}
	r := newAdoptRig(t)
	before := r.originals(t)
	id, err := r.m.ReserveID(t.Context(), "Family base")
	if err != nil {
		t.Fatal(err)
	}
	snapDir := filepath.Join(r.m.dirs.Snapshots, id)
	if err := os.Chmod(snapDir, 0o500); err != nil { //nolint:gosec // a test folder
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(snapDir, 0o755) }) //nolint:gosec // a test folder
	game, err := os.OpenRoot(r.game)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = game.Close() }()
	if _, err := r.m.Adopt(t.Context(), game, id, "Family base", "2.0.72", true, "alice"); err == nil {
		t.Fatal("adoption succeeded without a snapshot")
	}
	entries, _ := os.ReadDir(r.m.dirs.Worlds)
	if len(entries) != 0 {
		t.Errorf("worlds folder holds %v", entries)
	}
	for p, b := range r.originals(t) {
		if b != before[p] {
			t.Errorf("%s changed", p)
		}
	}
}

func TestAdoptRefusedWhenDiskIsShort(t *testing.T) {
	r := newAdoptRig(t)
	t.Cleanup(SetSpaceReserveForTest(math.MaxUint64 / 2))
	_, _, err := r.adopt(t)
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("err %v, want ErrNoSpace", err)
	}
	entries, _ := os.ReadDir(r.m.dirs.Worlds)
	if len(entries) != 0 {
		t.Errorf("worlds folder holds %v", entries)
	}
}

// A link inside the folders cannot be snapshotted, so it is refused before
// anything is built (SPEC 6.3).
func TestAdoptRefusesALinkInTheGameFolders(t *testing.T) {
	r := newAdoptRig(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(r.game, "config/passwd")); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.adopt(t)
	if !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("err %v, want ErrUnsupportedFile", err)
	}
	entries, _ := os.ReadDir(r.m.dirs.Worlds)
	if len(entries) != 0 {
		t.Errorf("worlds folder holds %v", entries)
	}
}

func TestAdoptRefusesAFolderThatIsALink(t *testing.T) {
	r := newAdoptRig(t)
	if err := os.Rename(filepath.Join(r.game, "mods"), filepath.Join(r.data, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.data, "elsewhere"), filepath.Join(r.game, "mods")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.adopt(t); err == nil {
		t.Fatal("adopted a game folder whose mods is a link")
	}
}

func TestAdoptNeedsAReservedFreeID(t *testing.T) {
	r := newAdoptRig(t)
	game, err := os.OpenRoot(r.game)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = game.Close() }()
	if _, err := r.m.Adopt(t.Context(), game, "never-reserved", "N", "2.0.72", false, "a"); err == nil {
		t.Error("adopted under an id nobody reserved")
	}
	if _, err := r.m.Adopt(t.Context(), game, "../x", "N", "2.0.72", false, "a"); err == nil {
		t.Error("adopted under a bad id")
	}
	if _, err := r.m.Adopt(t.Context(), game, "ok", "N", "latest", false, "a"); !errors.Is(err, ErrBadGameVersion) {
		t.Errorf("err %v, want ErrBadGameVersion", err)
	}

	id, err := r.m.ReserveID(t.Context(), "Taken")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.m.dirs.Worlds, id), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Adopt(t.Context(), game, id, "Taken", "2.0.72", false, "a"); err == nil {
		t.Error("adopted over an existing folder")
	}
}

func TestReserveIDKeepsTheIDTaken(t *testing.T) {
	m, _ := newTestManager(t)
	a, err := m.ReserveID(t.Context(), "Base")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.ReserveID(t.Context(), "Base")
	if err != nil {
		t.Fatal(err)
	}
	if a != "base" || b != "base-2" {
		t.Errorf("ids %q and %q, want base and base-2", a, b)
	}
	if _, err := m.Create(t.Context(), "Base", "2.0.72", false); err != nil {
		t.Fatal(err)
	}
	deleted, err := m.Deleted()
	if err != nil || len(deleted) != 0 {
		t.Errorf("a reserved id shows as a deleted world: %v, %v", deleted, err)
	}
}

func TestNewManagerRefusesAWorldsFolderThatIsALink(t *testing.T) {
	data, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Chmod(elsewhere, 0o700); err != nil { //nolint:gosec // a test folder
		t.Fatal(err)
	}
	link := filepath.Join(data, "worlds")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	_, err := NewManager(Dirs{
		Worlds:    link,
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, &fakeActive{}, nil)
	if !errors.Is(err, ErrWorldsIsLink) {
		t.Fatalf("err %v, want ErrWorldsIsLink", err)
	}
	if m := mode(t, elsewhere); m != 0o700 {
		t.Errorf("the link's target was chmodded to %o", m)
	}
}
