package world

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 3, 14, 25, 1, 0, time.UTC)

type fakeActive struct {
	mu sync.Mutex
	id string
}

func (f *fakeActive) ActiveWorld(context.Context) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.id, f.id != "", nil
}

func (f *fakeActive) SetActiveWorld(_ context.Context, id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.id = id
	return nil
}

func newTestManager(t *testing.T) (*Manager, *fakeActive) {
	t.Helper()
	data := t.TempDir()
	active := &fakeActive{}
	m, err := NewManager(Dirs{
		Worlds:    filepath.Join(data, "worlds"),
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, active, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	return m, active
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestIDFromName(t *testing.T) {
	cases := map[string]string{
		"Family base 2025":                   "family-base-2025",
		"  --Hello,  World!--  ":             "hello-world",
		"Ünïcode café":                       "n-code-caf",
		"???":                                "world",
		"":                                   "world",
		"../../etc/passwd":                   "etc-passwd",
		strings.Repeat("ab ", 30):            strings.Repeat("ab-", 13) + "a",
		strings.Repeat("x", 50):              strings.Repeat("x", 40),
		"Space Age: Gleba run #2":            "space-age-gleba-run-2",
		"ALLCAPS":                            "allcaps",
		"a" + strings.Repeat("b", 38) + " c": "a" + strings.Repeat("b", 38),
	}
	for name, want := range cases {
		got := IDFromName(name)
		if got != want {
			t.Errorf("IDFromName(%q) = %q, want %q", name, got, want)
		}
		if !ValidID(got) {
			t.Errorf("IDFromName(%q) = %q is not a valid id", name, got)
		}
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"", "-a", "a-", "A", "a/b", "..", ".staging-a", strings.Repeat("a", 41), "a b"} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
	for _, id := range []string{"a", "a-2", strings.Repeat("a", 40)} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
}

// An id is never reused: one taken by a world or by a snapshot folder
// (a deleted world's) gets the next suffix.
func TestNewIDSkipsWorldsAndSnapshots(t *testing.T) {
	m, _ := newTestManager(t)
	if err := os.Mkdir(filepath.Join(m.dirs.Worlds, "base"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(m.dirs.Snapshots, "base-2"), 0o750); err != nil {
		t.Fatal(err)
	}
	id, err := m.newID("Base")
	if err != nil || id != "base-3" {
		t.Fatalf("newID = %q, %v; want base-3", id, err)
	}

	long := strings.Repeat("y", 40)
	if err := os.Mkdir(filepath.Join(m.dirs.Worlds, long), 0o750); err != nil {
		t.Fatal(err)
	}
	id, err = m.newID(long)
	if err != nil || id != strings.Repeat("y", 38)+"-2" {
		t.Fatalf("newID = %q, %v; want 38 y and -2", id, err)
	}
}

func TestMetaRoundTripKeepsUnknownFields(t *testing.T) {
	in := `{"format_version":1,"name":"Base","game_version":"2.0.72","space_age":true,` +
		`"created_at":"2026-11-03T14:25:01Z","future_field":{"a":[1,2]},"other":"x"}`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	m, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "Base" || m.GameVersion != "2.0.72" || !m.SpaceAge || m.LoadSave != "" || !m.CreatedAt.Equal(t0) || m.Newer() {
		t.Fatalf("read %+v", m)
	}
	m.Name = "Renamed"
	m.LoadSave = "autosave1.zip"
	if err := WriteMeta(root, m); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Renamed" || got["load_save"] != "autosave1.zip" || got["other"] != "x" || got["format_version"] != 1.0 {
		t.Errorf("written: %s", b)
	}
	if ff, ok := got["future_field"].(map[string]any); !ok || len(ff["a"].([]any)) != 2 {
		t.Errorf("future_field lost: %s", b)
	}
	if mode(t, filepath.Join(dir, MetaFile)) != FileMode {
		t.Errorf("mode %v, want %v", mode(t, filepath.Join(dir, MetaFile)), FileMode)
	}
	// A missing load_save means the newest save, so it is left out when empty.
	m.LoadSave = ""
	if err := WriteMeta(root, m); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp file
	if strings.Contains(string(b), "load_save") {
		t.Errorf("empty load_save written: %s", b)
	}
}

func TestMetaNewerFormatIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	in := `{"format_version":2,"name":"Future","game_version":"3.0.0","space_age":false,"created_at":"2026-11-03T14:25:01Z"}`
	if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	m, err := ReadMeta(root)
	if err != nil || !m.Newer() || m.Name != "Future" {
		t.Fatalf("read %+v, %v", m, err)
	}
	if err := WriteMeta(root, m); !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("WriteMeta = %v, want ErrNewerFormat", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp file
	if string(b) != in {
		t.Error("world.json changed")
	}
}

func TestReadMetaRefusesBadFiles(t *testing.T) {
	for _, body := range []string{"", "{", `{"name":"no version"}`, `[]`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		root, _ := os.OpenRoot(dir)
		if _, err := ReadMeta(root); err == nil {
			t.Errorf("ReadMeta(%q) succeeded", body)
		}
		_ = root.Close()
	}
}

func TestWriteFileAtomicModeAndNoTemp(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir := t.TempDir()
	root, _ := os.OpenRoot(dir)
	defer func() { _ = root.Close() }()
	if err := WriteFileAtomic(root, "f.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(root, "f.txt", []byte("two")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "f.txt")) //nolint:gosec // a test temp file
	if string(b) != "two" {
		t.Errorf("content %q", b)
	}
	if got := mode(t, filepath.Join(dir, "f.txt")); got != FileMode {
		t.Errorf("mode %v under umask 077, want %v", got, FileMode)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
	// Writing outside the root is refused.
	if err := WriteFileAtomic(root, "../escape.txt", []byte("x")); err == nil {
		t.Error("wrote outside the root")
	}
}

func TestCreateAndList(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	m, active := newTestManager(t)
	ctx := context.Background()

	id, err := m.Create(ctx, "  Zeta base  ", "2.0.72", true)
	if err != nil || id != "zeta-base" {
		t.Fatalf("Create = %q, %v", id, err)
	}
	// The first world becomes active so there is always one to start.
	if active.id != "zeta-base" {
		t.Errorf("active = %q, want zeta-base", active.id)
	}
	for _, sub := range []string{"", SavesDir, ModsDir, ConfigDir} {
		if got := mode(t, filepath.Join(m.dirs.Worlds, id, sub)); got != DirMode {
			t.Errorf("%q mode %v, want %v", sub, got, DirMode)
		}
	}
	if _, err := m.Create(ctx, "alpha", "1.1.110", false); err != nil {
		t.Fatal(err)
	}
	if active.id != "zeta-base" {
		t.Errorf("second world changed the active one to %q", active.id)
	}
	if _, err := m.Create(ctx, "ZETA BASE", "2.0.72", false); !errors.Is(err, ErrNameTaken) {
		t.Errorf("duplicate name: %v, want ErrNameTaken", err)
	}
	if _, err := m.Create(ctx, "   ", "2.0.72", false); !errors.Is(err, ErrNameEmpty) {
		t.Errorf("blank name: %v, want ErrNameEmpty", err)
	}

	// A save makes last played and disk use show; leftovers and stray files are not worlds.
	save := filepath.Join(m.dirs.Worlds, "alpha", SavesDir, "a.zip")
	if err := os.WriteFile(save, make([]byte, 1500), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(save, t0, t0); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".staging-x", "Not-An-Id"} {
		if err := os.Mkdir(filepath.Join(m.dirs.Worlds, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.dirs.Worlds, "stray"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A world without a readable world.json is still listed, by id.
	if err := os.Mkdir(filepath.Join(m.dirs.Worlds, "broken"), 0o750); err != nil {
		t.Fatal(err)
	}

	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range list {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "zeta-base,alpha,broken" {
		t.Fatalf("order %v, want active first then by name", ids)
	}
	z, a, b := list[0], list[1], list[2]
	if !z.Active || z.Name != "Zeta base" || z.GameVersion != "2.0.72" || !z.SpaceAge || !z.LastPlayed.IsZero() {
		t.Errorf("zeta %+v", z)
	}
	if a.Active || !a.LastPlayed.Equal(t0) || a.DiskBytes < 1500 || a.SpaceAge {
		t.Errorf("alpha %+v", a)
	}
	if !b.Unreadable || b.Name != "broken" {
		t.Errorf("broken %+v", b)
	}
}

func TestLock(t *testing.T) {
	m, _ := newTestManager(t)
	unlock, err := m.Lock("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Lock("a"); !errors.Is(err, ErrBusy) {
		t.Errorf("second lock: %v, want ErrBusy", err)
	}
	unlockB, err := m.Lock("b")
	if err != nil {
		t.Errorf("another world: %v", err)
	}
	unlockB()
	unlock()
	unlock() // releasing twice must not release someone else's lock
	again, err := m.Lock("a")
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	unlock()
	if _, err := m.Lock("a"); !errors.Is(err, ErrBusy) {
		t.Error("a stale unlock released the new holder's lock")
	}
	again()
}

func TestRecoverRemovesLeftovers(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, "Base", "2.0.72", false); err != nil {
		t.Fatal(err)
	}
	keep := []string{
		filepath.Join(m.dirs.Worlds, "base", SavesDir, "real.zip"),
		filepath.Join(m.dirs.Snapshots, "base", "20261103T142501Z", "snapshot.json"),
		filepath.Join(m.dirs.Imports, ".partial-not-ours"),
	}
	gone := []string{
		filepath.Join(m.dirs.Worlds, ".staging-other-1234", SavesDir, "x.zip"),
		filepath.Join(m.dirs.Worlds, "base", SavesDir, ".partial-abcd"),
		filepath.Join(m.dirs.Worlds, ".deleting-old-1234", "world.json"),
		filepath.Join(m.dirs.Snapshots, "base", ".staging-20261103T150000Z", "snapshot.json"),
	}
	for _, p := range append(append([]string{}, keep...), gone...) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Recover(slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("removed %s: %v", p, err)
		}
	}
	for _, p := range gone {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("left %s", p)
		}
	}
	for _, d := range []string{".staging-other-1234", ".deleting-old-1234"} {
		if _, err := os.Stat(filepath.Join(m.dirs.Worlds, d)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("left folder %s", d)
		}
	}
	if _, err := m.OpenWorld("base"); err != nil {
		t.Errorf("real world harmed: %v", err)
	}
}

func TestCheckSpace(t *testing.T) {
	dir := t.TempDir()
	if err := CheckSpace(dir, 0); err != nil {
		t.Errorf("zero bytes: %v", err)
	}
	err := CheckSpace(dir, math.MaxUint64/2)
	if !errors.Is(err, ErrNoSpace) || !strings.Contains(err.Error(), "free") {
		t.Errorf("huge need: %v", err)
	}
	if _, err := FreeBytes(filepath.Join(dir, "missing")); err == nil {
		t.Error("FreeBytes on a missing folder succeeded")
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[uint64]string{0: "0 B", 999: "999 B", 1000: "1.0 kB", 2_100_000_000: "2.1 GB", 1_550_000: "1.6 MB"}
	for n, want := range cases {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestOpenWorldRefusesBadIDs(t *testing.T) {
	m, _ := newTestManager(t)
	for _, id := range []string{"..", "../worlds", "", "a/b", ".staging-x"} {
		if r, err := m.OpenWorld(id); err == nil {
			_ = r.Close()
			t.Errorf("OpenWorld(%q) succeeded", id)
		}
	}
}
