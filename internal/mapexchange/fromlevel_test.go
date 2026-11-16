package mapexchange

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"path"
	"path/filepath"
	"testing"
	"time"
)

// levelInits returns each test save's level-init.dat.
func levelInits(t testing.TB) [][]byte {
	t.Helper()
	saves, err := filepath.Glob("../savefile/testdata/*.zip")
	if err != nil || len(saves) == 0 {
		t.Fatalf("no test saves: %v", err)
	}
	var out [][]byte
	for _, s := range saves {
		zr, err := zip.OpenReader(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if path.Base(f.Name) != "level-init.dat" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, b)
		}
		_ = zr.Close()
	}
	return out
}

// A crafted level whose counts claim the rest of the file, with decoy
// surface names, must be refused quickly. Before the count checks it took
// about 5.6 s; the bound leaves room for a slow machine and the race
// detector while still catching that.
func TestFromLevelRefusesHostileCountsQuickly(t *testing.T) {
	lv := []byte{2, 0, 1, 0, 24, 0, 1, 0}
	unit := []byte{0x00, 0x00, 0xff, 0x60, 0xea, 0x00, 0x00}
	lv = append(lv, bytes.Repeat(unit, (4<<20)/len(unit))...)
	for i := range 64 {
		copy(lv[1000+i*50000:], "\x06nauvis\x00")
	}
	start := time.Now()
	_, err := FromLevel(lv, 8)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("took %v, want well under 2s", el)
	}
	if !errors.Is(err, ErrNoSettings) {
		t.Fatalf("err %v, want ErrNoSettings", err)
	}
}

// FuzzFromLevel feeds level bytes straight to FromLevel: no input panics,
// and anything accepted decodes.
func FuzzFromLevel(f *testing.F) {
	for _, b := range levelInits(f) {
		f.Add(b, 51)
		f.Add(b[:min(len(b), 4096)], 8)
	}
	f.Add([]byte{1, 0, 1, 0, 110, 0, 0, 0}, 8)
	f.Fuzz(func(t *testing.T, b []byte, from int) {
		s, err := FromLevel(b, from)
		if err != nil {
			return
		}
		if _, err := Decode(s); err != nil {
			t.Fatalf("accepted a string the decoder refuses: %v", err)
		}
	})
}
