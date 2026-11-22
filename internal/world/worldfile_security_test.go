package world

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Attack tests for world file imports (SPEC 5.14, 14.5, 14.6; design-774
// section 1.5). Every hostile file must be refused as a whole, by both the
// preview and the import, with nothing written outside the staging folder
// and no world made.

var secLimits = WorldFileLimits{Save: 1 << 30, Mod: 1 << 30}

type zipEntry struct {
	name string
	data []byte
	mode fs.FileMode // 0 means a regular file
	// method is zip.Store unless set.
	method uint16
	// lieSize, when >= 0, is written as the header's sizes instead of the
	// real ones (a raw entry).
	lieSize int64
}

func wfEntry(name, data string) zipEntry {
	return zipEntry{name: name, data: []byte(data), lieSize: -1}
}

func wfFormat() zipEntry {
	return wfEntry(WorldFileFormatFile, `{"format_version":1,"exported_at":"2026-01-01T00:00:00Z","foreman_version":"test","saves":"all","game_password_set":false}`)
}

func wfMeta() zipEntry {
	return wfEntry(MetaFile, `{"format_version":1,"name":"Imported","game_version":"2.0.77","space_age":false,"load_save":"a.zip","created_at":"2026-01-01T00:00:00Z"}`)
}

func wfSave() zipEntry { return wfEntry("saves/a.zip", "PK-not-really-a-save") }

func wfEntries() []zipEntry { return []zipEntry{wfFormat(), wfMeta(), wfSave()} }

func buildWorldZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		if e.lieSize >= 0 {
			// A raw stored entry whose header states a size it does not have.
			h.Method = zip.Store
			h.CompressedSize64 = uint64(len(e.data))
			h.UncompressedSize64 = uint64(e.lieSize)
			h.CRC32 = 0
			fw, err := w.CreateRaw(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fw.Write(e.data); err != nil {
				t.Fatal(err)
			}
			continue
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stage writes data as a staged world file and returns its name.
func stageWorldFile(t *testing.T, m *Manager, data []byte) string {
	t.Helper()
	f, name, err := m.CreateWorldFileTemp()
	if err != nil {
		t.Fatalf("CreateWorldFileTemp: %v", err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

// wfTree lists every path under dir with its size.
func wfTree(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out[p] = info.Size()
		}
		return nil
	})
	return out
}

// assertWorldFileRefused checks that both the preview and the import refuse data
// with want, and that the import made no world and wrote nothing new
// anywhere under the data folder (other than its own staged file).
func assertWorldFileRefused(t *testing.T, data []byte, want error) {
	t.Helper()
	m, _ := newTestManager(t)
	ctx := context.Background()
	root := filepath.Dir(m.dirs.Worlds)

	temp := stageWorldFile(t, m, data)
	if _, err := m.ReadWorldFile(temp, secLimits); !errors.Is(err, want) {
		t.Errorf("ReadWorldFile: got %v, want %v", err, want)
	}
	before := wfTree(t, root)
	id, err := m.ImportWorldFile(ctx, temp, "Attack", secLimits)
	if !errors.Is(err, want) {
		t.Errorf("ImportWorldFile: got id %q, err %v; want %v", id, err, want)
	}
	if id != "" {
		t.Errorf("ImportWorldFile returned id %q on a refusal", id)
	}
	if list, err := m.List(ctx); err == nil && len(list) != 0 {
		t.Errorf("a refused import left %d worlds", len(list))
	}
	for p, size := range wfTree(t, root) {
		if _, ok := before[p]; !ok {
			t.Errorf("a refused import left a new path %s (%d bytes)", p, size)
		}
	}
}

// The control: a minimal honest world file is read and imported. Without it
// every refusal test could pass against an import that refuses everything.
func TestWorldFileSecurityControlAccepted(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	data := buildWorldZip(t, wfEntries())
	temp := stageWorldFile(t, m, data)
	info, err := m.ReadWorldFile(temp, secLimits)
	if err != nil {
		t.Fatalf("ReadWorldFile of a good file: %v", err)
	}
	if info.Saves != 1 || info.GameVersion != "2.0.77" {
		t.Errorf("info %+v", info)
	}
	id, err := m.ImportWorldFile(ctx, temp, "Imported", secLimits)
	if err != nil {
		t.Fatalf("ImportWorldFile of a good file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.dirs.Worlds, id, SavesDir, "a.zip")); err != nil {
		t.Errorf("save not imported: %v", err)
	}
}

func TestWorldFileSecurityZipSlip(t *testing.T) {
	for _, name := range []string{
		"../escape.txt",
		"saves/../../escape.txt",
		"mods/../../../escape.txt",
		"mods/a/../../../escape.txt",
		`mods\..\..\escape.txt`,
		"./world.json",
	} {
		t.Run(name, func(t *testing.T) {
			assertWorldFileRefused(t, buildWorldZip(t, append(wfEntries(), wfEntry(name, "x"))), ErrWorldFileBad)
		})
	}
}

func TestWorldFileSecurityAbsolutePaths(t *testing.T) {
	for _, name := range []string{"/tmp/fixes-16-t5-escape", "/etc/passwd", "C:/escape.txt", `C:\escape.txt`, "//server/share/x"} {
		t.Run(name, func(t *testing.T) {
			assertWorldFileRefused(t, buildWorldZip(t, append(wfEntries(), wfEntry(name, "x"))), ErrWorldFileBad)
		})
	}
	if _, err := os.Stat("/tmp/fixes-16-t5-escape"); err == nil {
		t.Error("an absolute entry was written outside the data folder")
	}
}

func TestWorldFileSecuritySymlinkEntries(t *testing.T) {
	cases := map[string][]zipEntry{
		"mod link to /etc": {
			{name: "mods/evil", data: []byte("/etc"), mode: fs.ModeSymlink | 0o777, lieSize: -1},
		},
		"save link": {
			{name: "saves/b.zip", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777, lieSize: -1},
		},
		"link then write through it": {
			{name: "mods/dir", data: []byte("../../.."), mode: fs.ModeSymlink | 0o777, lieSize: -1},
			wfEntry("mods/dir/escape.txt", "x"),
		},
		"device": {
			{name: "mods/dev", data: nil, mode: fs.ModeDevice | 0o666, lieSize: -1},
		},
		"named pipe": {
			{name: "mods/fifo", data: nil, mode: fs.ModeNamedPipe | 0o666, lieSize: -1},
		},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			assertWorldFileRefused(t, buildWorldZip(t, append(wfEntries(), extra...)), ErrWorldFileBad)
		})
	}
}

func TestWorldFileSecurityDuplicateNames(t *testing.T) {
	cases := map[string][]zipEntry{
		"world.json twice": {wfEntry(MetaFile, `{"format_version":1,"name":"Other","game_version":"2.0.77","created_at":"2026-01-01T00:00:00Z"}`)},
		"format twice":     {wfFormat()},
		"save twice":       {wfEntry("saves/a.zip", "other")},
		"mod twice":        {wfEntry("mods/x_1.0.0.zip", "a"), wfEntry("mods/x_1.0.0.zip", "b")},
		"config twice":     {wfEntry("config/server-settings.json", "{}"), wfEntry("config/server-settings.json", "{}")},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			assertWorldFileRefused(t, buildWorldZip(t, append(wfEntries(), extra...)), ErrWorldFileBad)
		})
	}
}

// config/rconpw is never read from a file: the new host makes its own.
func TestWorldFileSecurityRconPassword(t *testing.T) {
	assertWorldFileRefused(t, buildWorldZip(t, append(wfEntries(), wfEntry(RCONPasswordFile, "stolen"))), ErrWorldFileBad)
}

func TestWorldFileSecurityTooManyEntries(t *testing.T) {
	entries := wfEntries()
	for i := 0; len(entries) <= maxWorldFileEntries; i++ {
		entries = append(entries, zipEntry{name: fmt.Sprintf("mods/d%d/", i), lieSize: -1})
	}
	assertWorldFileRefused(t, buildWorldZip(t, entries), ErrWorldFileBad)
}

func TestWorldFileSecurityLyingSizes(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 4096)
	cases := map[string]zipEntry{
		// The header says 10 bytes; 4 KiB follow.
		"mod larger than stated": {name: "mods/x_1.0.0.zip", data: big, lieSize: 10},
		// The header says 1 byte for a config file; 2 MiB follow, over
		// the 1 MiB cap.
		"config larger than stated": {name: "config/map-settings.json", data: bytes.Repeat([]byte(" "), 2<<20), lieSize: 1},
		// The header claims more than is there.
		"mod smaller than stated": {name: "mods/y_1.0.0.zip", data: []byte("short"), lieSize: 1 << 20},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			assertWorldFileRefused(t, buildWorldZip(t, append(wfEntries(), e)), ErrWorldFileBad)
		})
	}
}

// Honest headers over a per-entry limit are refused too.
func TestWorldFileSecurityEntryOverLimit(t *testing.T) {
	cases := map[string]zipEntry{
		"format file over 1 KiB": wfEntry(WorldFileFormatFile, `{"format_version":1,"pad":"`+string(bytes.Repeat([]byte("x"), 2048))+`"}`),
		"config over 1 MiB":      {name: "config/map-gen-settings.json", data: bytes.Repeat([]byte(" "), (1<<20)+10), lieSize: -1},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			entries := wfEntries()
			if e.name == WorldFileFormatFile {
				entries[0] = e
			} else {
				entries = append(entries, e)
			}
			assertWorldFileRefused(t, buildWorldZip(t, entries), ErrWorldFileBad)
		})
	}
	t.Run("save over the save limit", func(t *testing.T) {
		m, _ := newTestManager(t)
		temp := stageWorldFile(t, m, buildWorldZip(t, append(wfEntries(), zipEntry{name: "saves/b.zip", data: make([]byte, 4096), lieSize: -1})))
		lim := WorldFileLimits{Save: 1024, Mod: 1 << 30}
		if _, err := m.ImportWorldFile(context.Background(), temp, "Big", lim); !errors.Is(err, ErrWorldFileBad) {
			t.Errorf("got %v, want ErrWorldFileBad", err)
		}
	})
}

// A deflated entry that unpacks to more than 50 times the file's size.
func TestWorldFileSecurityRatioBomb(t *testing.T) {
	zeros := make([]byte, 32<<20)
	entries := append(wfEntries(), zipEntry{name: "mods/bomb_1.0.0.zip", data: zeros, method: zip.Deflate, lieSize: -1})
	data := buildWorldZip(t, entries)
	if int64(len(zeros)) <= 50*int64(len(data)) {
		t.Fatalf("test file not a bomb: %d into %d", len(zeros), len(data))
	}
	assertWorldFileRefused(t, data, ErrWorldFileBad)
}

// Too little disk for the unpacked world refuses with ErrNoSpace, writes
// nothing, and keeps the staged file so the form can try again (1.5 rule 8).
func TestWorldFileSecurityDiskShortfall(t *testing.T) {
	restore := SetSpaceReserveForTest(1 << 62)
	defer restore()
	m, _ := newTestManager(t)
	temp := stageWorldFile(t, m, buildWorldZip(t, wfEntries()))
	before := wfTree(t, m.dirs.Worlds)
	_, err := m.ImportWorldFile(context.Background(), temp, "Full", secLimits)
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("got %v, want ErrNoSpace", err)
	}
	after := wfTree(t, m.dirs.Worlds)
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("new path %s", p)
		}
	}
	if len(after) != len(before) {
		t.Errorf("paths changed: %d before, %d after (the staged file must be kept)", len(before), len(after))
	}
}
