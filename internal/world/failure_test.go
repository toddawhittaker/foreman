package world

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// These tests break the disk and the process partway through an operation
// and check the vision's "never lose a save": a failed snapshot means the
// operation does not run (SPEC 6.2), a failure leaves nothing half-done,
// and startup recovery settles a crash (R9).

// tinyFSEnv marks a test process already running on a small filesystem.
const tinyFSEnv = "FOREMAN_TEST_TINY_FS"

// onTinyFS runs the calling test again in a child process inside a new user
// and mount namespace, with TMPDIR on a fresh tmpfs mounted with opts, so the
// test can fill a real disk without root. It returns true in the child,
// where the test goes on; in the parent it reports the child's result and
// returns false.
func onTinyFS(t *testing.T, opts string) bool {
	t.Helper()
	if os.Getenv(tinyFSEnv) != "" {
		return true
	}
	mnt := t.TempDir()
	probe := exec.Command("unshare", "-rm", "sh", "-c", `mount -t tmpfs -o "$1" tmpfs "$2"`, "sh", opts, mnt) //nolint:gosec // fixed test command
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("cannot mount a tmpfs in a user namespace here (%v: %s); this test needs unshare and unprivileged user namespaces", err, out)
	}
	cmd := exec.Command("unshare", "-rm", "sh", "-c", //nolint:gosec // fixed test command
		`mount -t tmpfs -o "$1" tmpfs "$2" && exec "$3" -test.run "$4" -test.v`,
		"sh", opts, mnt, os.Args[0], "^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), tinyFSEnv+"=1", "TMPDIR="+mnt)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("on a tiny filesystem: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "--- SKIP") {
		t.Skipf("skipped on the tiny filesystem:\n%s", out)
	}
	t.Logf("on a tiny filesystem:\n%s", out)
	return false
}

// inodeFiller uses up a filesystem's free inodes so the next file or folder
// made fails with ENOSPC, while statfs still shows free bytes: the disk fills
// partway through an operation that checked its space first.
type inodeFiller struct {
	t     *testing.T
	dir   string
	files []string
}

func newInodeFiller(t *testing.T) *inodeFiller {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "filler")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return &inodeFiller{t: t, dir: dir}
}

// leave fills the disk's inodes, then frees n of them.
func (f *inodeFiller) leave(n int) {
	f.t.Helper()
	for {
		p := filepath.Join(f.dir, fmt.Sprint(len(f.files)))
		fd, err := os.Create(p) //nolint:gosec // a test temp file
		if errors.Is(err, syscall.ENOSPC) {
			break
		}
		if err != nil {
			f.t.Fatal(err)
		}
		_ = fd.Close()
		f.files = append(f.files, p)
	}
	for range n {
		f.pop()
	}
}

func (f *inodeFiller) pop() {
	last := f.files[len(f.files)-1]
	f.files = f.files[:len(f.files)-1]
	if err := os.Remove(last); err != nil {
		f.t.Fatal(err)
	}
}

func (f *inodeFiller) release() {
	for len(f.files) > 0 {
		f.pop()
	}
}

// seedBusyWorld adds files to a seeded world so a snapshot or restore makes
// many folders and copies: one more inode each.
func seedBusyWorld(t *testing.T, m *Manager) string {
	t.Helper()
	id := seedWorld(t, m)
	w := filepath.Join(m.dirs.Worlds, id)
	for i := range 6 {
		write(t, filepath.Join(w, "config", fmt.Sprintf("sub%d", i), "c.json"), fmt.Sprintf(`{"n":%d}`, i), t0)
	}
	return id
}

// SPEC 6.2: a disk that fills partway through a snapshot fails it cleanly:
// no snapshot, no partial folder, the world untouched. Every point of
// failure is tried, from the first folder made to the last file.
func TestDiskFullMidSnapshot(t *testing.T) {
	if !onTinyFS(t, "size=8m,nr_inodes=400") {
		return
	}
	m, _ := newTestManager(t)
	id := seedBusyWorld(t, m)
	before := diskTree(t, filepath.Join(m.dirs.Worlds, id))
	fill := newInodeFiller(t)
	failures := 0
	for free := 0; ; free++ {
		fill.leave(free)
		_, err := m.TakeSnapshot(id, "x", "alice")
		fill.release()
		if err == nil {
			break
		}
		failures++
		if !errors.Is(err, syscall.ENOSPC) {
			t.Errorf("with %d inodes free: want ENOSPC, got %v", free, err)
		}
		if list, _ := m.Snapshots(id); len(list) != 0 {
			t.Fatalf("with %d inodes free: a failed snapshot is listed: %+v", free, list)
		}
		noLeftovers(t, m)
		sameDisk(t, fmt.Sprintf("world with %d inodes free", free), diskTree(t, filepath.Join(m.dirs.Worlds, id)), before)
		if free > 200 {
			t.Fatal("snapshot never succeeded")
		}
	}
	if failures < 5 {
		t.Errorf("only %d failure points tried", failures)
	}
}

// SPEC 6.2, 6.5, R9: a disk that fills partway through a restore leaves the
// world either exactly as it was or exactly as the snapshot recorded it,
// with no leftovers; and the world never changes unless the "Before
// restore" snapshot exists first.
func TestDiskFullMidRestore(t *testing.T) {
	if !onTinyFS(t, "size=8m,nr_inodes=400") {
		return
	}
	m, _ := newTestManager(t)
	id := seedBusyWorld(t, m)
	w := filepath.Join(m.dirs.Worlds, id)
	snap, err := m.TakeSnapshot(id, "target", "alice")
	if err != nil {
		t.Fatal(err)
	}
	restored := diskTree(t, w)
	write(t, filepath.Join(w, "saves", "later.zip"), "played on", t0.Add(48*3600e9))
	write(t, filepath.Join(w, "config", "sub0", "c.json"), `{"n":"changed"}`, t0.Add(48*3600e9))
	live := diskTree(t, w)

	fill := newInodeFiller(t)
	failures := 0
	for free := 0; ; free++ {
		fill.leave(free)
		_, err := m.Restore(context.Background(), id, snap.Name, "alice")
		fill.release()
		if err == nil {
			break
		}
		failures++
		noLeftovers(t, m)
		sameDisk(t, fmt.Sprintf("world with %d inodes free", free), diskTree(t, w), live)
		for _, s := range snapshotDirs(t, m, id) {
			if _, err := m.Snapshot(id, s); err != nil {
				t.Errorf("with %d inodes free: snapshot %s unreadable: %v", free, s, err)
			}
			// Each failure point must start from the same disk, so the
			// Before restore snapshot an attempt left is cleared.
			if s != snap.Name {
				if err := os.RemoveAll(filepath.Join(m.dirs.Snapshots, id, s)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if free > 200 {
			t.Fatal("restore never succeeded")
		}
	}
	if failures < 5 {
		t.Errorf("only %d failure points tried", failures)
	}
	got := diskTree(t, w)
	delete(got, MetaFile)
	delete(restored, MetaFile)
	sameDisk(t, "restored world", got, restored)
	var before int
	list, _ := m.Snapshots(id)
	for _, s := range list {
		if s.Reason == ReasonBeforeRestore {
			before++
		}
	}
	if before == 0 {
		t.Error("the restore that worked left no Before restore snapshot")
	}
}

// SPEC 5.5, R19: a disk that fills partway through an import leaves no
// world, no staging folder, and the inbox folder as it was.
func TestDiskFullMidImport(t *testing.T) {
	if !onTinyFS(t, "size=8m,nr_inodes=400") {
		return
	}
	m, _ := newTestManager(t)
	src := filepath.Join(m.dirs.Imports, "old-server")
	write(t, filepath.Join(src, "a.zip"), "save a", t0)
	write(t, filepath.Join(src, "b.zip"), "save b", t0)
	for i := range 6 {
		write(t, filepath.Join(src, "mods", fmt.Sprintf("d%d", i), "m.zip"), "mod", t0)
	}
	inbox := diskTree(t, m.dirs.Imports)
	fill := newInodeFiller(t)
	failures := 0
	for free := 0; ; free++ {
		fill.leave(free)
		id, err := m.ImportFolder(t.Context(), "old-server", "Imported", "2.0.72", false)
		fill.release()
		if err == nil {
			if id == "" {
				t.Fatal("import worked with no id")
			}
			break
		}
		failures++
		list, lerr := m.List(t.Context())
		if lerr != nil || len(list) != 0 {
			t.Fatalf("with %d inodes free: failed import left worlds %+v (%v)", free, list, lerr)
		}
		noLeftovers(t, m)
		sameDisk(t, "inbox", diskTree(t, m.dirs.Imports), inbox)
		if free > 200 {
			t.Fatal("import never succeeded")
		}
	}
	if failures < 5 {
		t.Errorf("only %d failure points tried", failures)
	}
}

// copyTree copies a snapshot's files, without its manifest, as a staged
// restore holds them.
func copyTree(t *testing.T, from, to string) map[string]string {
	t.Helper()
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(to, ManifestFile))
	return diskTree(t, to)
}

// R9: a crash at each step of a restore is settled by Recover: before the
// swap the world is as it was, after it the world is the snapshot, and
// nothing half-done is left. Recover run twice changes nothing more.
func TestCrashAtEachRestoreStepIsRecovered(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	type setup struct {
		m        *Manager
		id       string
		world    string
		snapDir  string
		staging  string
		old      string
		live     map[string]string
		restored map[string]string
	}
	steps := []struct {
		name   string
		crash  func(t *testing.T, s *setup)
		wantOK func(s *setup) map[string]string
	}{
		{"during the Before restore snapshot", func(t *testing.T, s *setup) {
			p := filepath.Join(s.m.dirs.Snapshots, s.id, PrefixPartial+"0123456789abcdef")
			write(t, filepath.Join(p, "saves", "half.zip"), "half", t0)
		}, func(s *setup) map[string]string { return s.live }},
		{"while staging", func(t *testing.T, s *setup) {
			write(t, filepath.Join(s.staging, "saves", "base.zip"), "half", t0)
		}, func(s *setup) map[string]string { return s.live }},
		{"with staging complete", func(t *testing.T, s *setup) {
			copyTree(t, s.snapDir, s.staging)
		}, func(s *setup) map[string]string { return s.live }},
		{"after the live world moved aside", func(t *testing.T, s *setup) {
			copyTree(t, s.snapDir, s.staging)
			if err := os.Rename(s.world, s.old); err != nil {
				t.Fatal(err)
			}
		}, func(s *setup) map[string]string { return s.live }},
		{"after the staged world moved in", func(t *testing.T, s *setup) {
			s.restored = copyTree(t, s.snapDir, s.staging)
			if err := os.Rename(s.world, s.old); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(s.staging, s.world); err != nil {
				t.Fatal(err)
			}
		}, func(s *setup) map[string]string { return s.restored }},
		{"while removing the old world", func(t *testing.T, s *setup) {
			s.restored = copyTree(t, s.snapDir, s.staging)
			if err := os.Rename(s.world, s.old); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(s.staging, s.world); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(s.old, "saves")); err != nil {
				t.Fatal(err)
			}
		}, func(s *setup) map[string]string { return s.restored }},
	}
	for _, step := range steps {
		// A hyphenated id checks that recovery reads the id back whole.
		for _, name := range []string{"Base", "My old base"} {
			t.Run(step.name+"/"+name, func(t *testing.T) {
				m, _ := newTestManager(t)
				id, err := m.Create(t.Context(), name, "2.0.72", true)
				if err != nil {
					t.Fatal(err)
				}
				w := filepath.Join(m.dirs.Worlds, id)
				writeAt(t, filepath.Join(w, "saves", "base.zip"), "save one", t0)
				writeAt(t, filepath.Join(w, "config", "server.json"), "{}", t0)
				snap, err := m.TakeSnapshot(id, "target", "alice")
				if err != nil {
					t.Fatal(err)
				}
				s := &setup{m: m, id: id, world: w, snapDir: filepath.Join(m.dirs.Snapshots, id, snap.Name),
					staging: filepath.Join(m.dirs.Worlds, PrefixStaging+id+"-0123456789abcdef"),
					old:     filepath.Join(m.dirs.Worlds, PrefixRestoreOld+id+"-fedcba9876543210")}
				writeAt(t, filepath.Join(w, "saves", "later.zip"), "played on", t0)
				s.live = diskTree(t, w)
				step.crash(t, s)

				if err := m.Recover(quiet); err != nil {
					t.Fatal(err)
				}
				sameDisk(t, "world after recovery", diskTree(t, w), step.wantOK(s))
				noLeftovers(t, m)
				list, err := m.List(t.Context())
				if err != nil || len(list) != 1 || list[0].ID != id {
					t.Errorf("worlds after recovery: %+v, %v", list, err)
				}
				if snaps, err := m.Snapshots(id); err != nil || len(snaps) != 1 {
					t.Errorf("snapshots after recovery: %+v, %v", snaps, err)
				}
				again := diskTree(t, dataDir(m))
				if err := m.Recover(quiet); err != nil {
					t.Fatal(err)
				}
				sameDisk(t, "second recovery", diskTree(t, dataDir(m)), again)
				if _, err := m.Restore(context.Background(), id, snap.Name, "alice"); err != nil {
					t.Errorf("restore after recovery: %v", err)
				}
			})
		}
	}
}

// R15: while one admin's operation holds a world, every other change to it
// is refused at once with the busy error and changes nothing.
func TestBusyWorldRefusesEveryChange(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "target", "alice")
	if err != nil {
		t.Fatal(err)
	}
	temp := uploadTemp(t, m, id, "upload")
	before := diskTree(t, dataDir(m))
	delete(before, filepath.Join("worlds", id, temp))
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]func() error{
		"rename":    func() error { return m.Rename(t.Context(), id, "Taken over") },
		"restore":   func() error { _, err := m.Restore(context.Background(), id, snap.Name, "bob"); return err },
		"snapshot":  func() error { _, err := m.TakeSnapshot(id, "x", "bob"); return err },
		"load save": func() error { return m.SetLoadSave(id, "base.zip") },
		"add save":  func() error { return m.AddSave(id, "new.zip", temp) },
	}
	for name, op := range ops {
		if err := op(); !errors.Is(err, ErrBusy) {
			t.Errorf("%s while busy = %v, want ErrBusy", name, err)
		}
	}
	unlock()
	sameDisk(t, "data folder", diskTree(t, dataDir(m)), before)
}

// R15 and "never lose a save": two admins hammering one world at once get
// either success or the busy error, and the world is whole afterwards, with
// every snapshot restorable.
func TestConcurrentChangesNeverCorruptAWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "target", "alice")
	if err != nil {
		t.Fatal(err)
	}
	for round := range 20 {
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make(chan error, 64)
		run := func(name string, op func() error) {
			wg.Go(func() {
				<-start
				if err := op(); err != nil && !errors.Is(err, ErrBusy) {
					errs <- fmt.Errorf("round %d, %s: %w", round, name, err)
				}
			})
		}
		for i := range 3 {
			run("restore", func() error { _, err := m.Restore(context.Background(), id, snap.Name, "a"); return err })
			run("rename", func() error { return m.Rename(t.Context(), id, fmt.Sprintf("Name %d-%d", round, i)) })
			run("snapshot", func() error { _, err := m.TakeSnapshot(id, "x", "b"); return err })
			run("load save", func() error { return m.SetLoadSave(id, "base.zip") })
			temp := uploadTemp(t, m, id, "u")
			run("add save", func() error {
				err := m.AddSave(id, fmt.Sprintf("u-%d-%d.zip", round, i), temp)
				if errors.Is(err, ErrSaveExists) {
					return nil
				}
				return err
			})
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		noLeftovers(t, m)
		w, err := m.OpenWorld(id)
		if err != nil {
			t.Fatalf("round %d: world gone: %v", round, err)
		}
		if _, err := ReadMeta(w); err != nil {
			t.Errorf("round %d: world.json unreadable: %v", round, err)
		}
		_ = w.Close()
		if s, ok, err := m.NewestSave(id); err != nil || !ok {
			t.Errorf("round %d: no save left: %+v %v %v", round, s, ok, err)
		}
		// Partial uploads that lost the race stay in saves/ until AddSave
		// removes them; each was removed whatever happened.
		entries, _ := os.ReadDir(filepath.Join(m.dirs.Worlds, id, SavesDir))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), PrefixPartial) {
				t.Errorf("round %d: upload temp %s left behind", round, e.Name())
			}
		}
	}
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if err := m.checkFiles(id, s); err != nil {
			t.Errorf("snapshot %s damaged: %v", s.Name, err)
		}
	}
}
