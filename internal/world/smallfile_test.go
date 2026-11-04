package world

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const planted = "hunter2-the-secret-password"

type smallFileTarget struct {
	rel  string
	read func(*os.Root) error
}

// smallFileTargets are the files the game can write that Foreman reads
// through ReadSmallFile, with the function that reads each.
func smallFileTargets() map[string]smallFileTarget {
	return map[string]smallFileTarget{
		"world.json": {MetaFile, func(r *os.Root) error { _, err := ReadMeta(r); return err }},
		"mod-list.json": {filepath.Join(ModsDir, ModListFile), func(r *os.Root) error {
			_, err := readModList(r)
			return err
		}},
	}
}

// withTimeout fails the test when read does not return promptly.
func withTimeout(t *testing.T, read func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- read() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the read hangs")
		return nil
	}
}

func openTestWorld(t *testing.T) (*os.Root, string) {
	t.Helper()
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

// SPEC 14.5, 14.8: a named pipe in place of a game-writable file is
// refused at once, not waited on.
func TestSmallFilesRefuseAPipe(t *testing.T) {
	for name, tg := range smallFileTargets() {
		t.Run(name, func(t *testing.T) {
			root, dir := openTestWorld(t)
			p := filepath.Join(dir, tg.rel)
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(p, 0o640); err != nil {
				t.Fatal(err)
			}
			if err := withTimeout(t, func() error { return tg.read(root) }); err == nil {
				t.Error("a pipe was accepted")
			}
		})
	}
}

// SPEC 14.5: a huge file is refused from its size, without reading it.
func TestSmallFilesRefuseAHugeSparseFile(t *testing.T) {
	for name, tg := range smallFileTargets() {
		t.Run(name, func(t *testing.T) {
			root, dir := openTestWorld(t)
			f, err := os.OpenFile(filepath.Join(dir, tg.rel), os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // a test path
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Truncate(1 << 40); err != nil {
				t.Skipf("no sparse files here: %v", err)
			}
			_ = f.Close()
			err = withTimeout(t, func() error { return tg.read(root) })
			if err == nil || !strings.Contains(err.Error(), "too big") {
				t.Errorf("a huge file was not refused as too big: %v", err)
			}
		})
	}
}

// SPEC 14.8: a link to the RCON password is refused and the error never
// carries the password.
func TestSmallFilesRefuseALinkToThePassword(t *testing.T) {
	for name, tg := range smallFileTargets() {
		t.Run(name, func(t *testing.T) {
			root, dir := openTestWorld(t)
			pw := filepath.Join(dir, RCONPasswordFile)
			if err := os.MkdirAll(filepath.Dir(pw), 0o750); err != nil {
				t.Fatal(err)
			}
			writeFile(t, pw, planted+"\n")
			p := filepath.Join(dir, tg.rel)
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.Symlink(pw, p); err != nil {
				t.Fatal(err)
			}
			err := tg.read(root)
			if err == nil {
				t.Fatal("a link was followed")
			}
			if strings.Contains(err.Error(), planted) {
				t.Errorf("the error carries the password: %v", err)
			}
		})
	}
}

// A file that is not what is expected must not have its text quoted back
// in the error.
func TestSmallFileJSONErrorNeverQuotesContents(t *testing.T) {
	bodies := []string{
		planted,
		`{"mods": "` + planted + `"}`,
		`{"format_version": "` + planted + `"}`,
		`{"name": 1, "x": "` + planted + `"`,
	}
	for name, tg := range smallFileTargets() {
		t.Run(name, func(t *testing.T) {
			root, dir := openTestWorld(t)
			for _, body := range bodies {
				writeFile(t, filepath.Join(dir, tg.rel), body)
				err := tg.read(root)
				// Some bodies are fine for one file and not the other.
				if err != nil && strings.Contains(err.Error(), "hunter") {
					t.Errorf("the error quotes the file: %v", err)
				}
			}
		})
	}
}

func TestReadSmallFileLimit(t *testing.T) {
	root, dir := openTestWorld(t)
	writeFile(t, filepath.Join(dir, "x"), "12345")
	if b, err := ReadSmallFile(root, "x", 5); err != nil || string(b) != "12345" {
		t.Errorf("a file at the limit: %q, %v", b, err)
	}
	if _, err := ReadSmallFile(root, "x", 4); err == nil {
		t.Error("a file over the limit was read")
	}
	if _, err := ReadSmallFile(root, "missing", 4); !os.IsNotExist(err) {
		t.Errorf("a missing file: %v", err)
	}
}

// SPEC 14.5: a pipe at a save's name is refused, not waited on.
func TestOpenSaveRefusesAPipe(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	if err := syscall.Mkfifo(filepath.Join(dir, SavesDir, "a.zip"), 0o640); err != nil {
		t.Fatal(err)
	}
	err := withTimeout(t, func() error {
		f, _, err := m.OpenSave(id, "a.zip")
		if f != nil {
			_ = f.Close()
		}
		return err
	})
	if err == nil {
		t.Error("a pipe was served as a save")
	}
}
