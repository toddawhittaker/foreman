package savefile

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(os.DirFS("testdata"), name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readBytes(b []byte) (Save, error) {
	return Read(bytes.NewReader(b), int64(len(b)))
}

func m(name string, v [3]uint16) Mod {
	return Mod{Name: name, Version: Version{Major: v[0], Minor: v[1], Patch: v[2]}}
}

func mods(m ...Mod) []Mod { return m }

// Expected values come from the mod-list each fixture was made with
// (testdata/README) and the image's game version.
func TestFixtures(t *testing.T) {
	v11 := [3]uint16{1, 1, 110}
	v20 := [3]uint16{2, 0, 77}
	v21 := [3]uint16{2, 1, 24}
	test := [3]uint16{0, 1, 0}
	cases := []struct {
		file    string
		version string
		mods    []Mod
	}{
		{"1.1.110-vanilla.zip", "1.1.110", mods(m("base", v11))},
		{"1.1.110-testmod.zip", "1.1.110", mods(m("base", v11), m("foreman-test", test))},
		{"2.0.77-no-space-age.zip", "2.0.77", mods(m("base", v20))},
		{"2.0.77-space-age.zip", "2.0.77", mods(m("base", v20), m("elevated-rails", v20), m("quality", v20), m("space-age", v20))},
		{"2.1.24-no-space-age.zip", "2.1.24", mods(m("base", v21), m("recycler", v21))},
		{"2.1.24-space-age.zip", "2.1.24", mods(m("base", v21), m("elevated-rails", v21), m("recycler", v21), m("quality", v21), m("space-age", v21))},
		{"2.1.24-space-age-testmod.zip", "2.1.24", mods(m("base", v21), m("elevated-rails", v21), m("foreman-test", test), m("recycler", v21), m("quality", v21), m("space-age", v21))},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			s, err := readBytes(readFixture(t, c.file))
			if err != nil {
				t.Fatal(err)
			}
			if s.Version.String() != c.version {
				t.Errorf("version %s, want %s", s.Version, c.version)
			}
			if !reflect.DeepEqual(s.Mods, c.mods) {
				t.Errorf("mods %+v, want %+v", s.Mods, c.mods)
			}
		})
	}
}

// rezip copies a fixture, letting edit change level.dat0's name and its
// inflated bytes, and say whether to compress them again. Returning nil
// bytes drops the entry.
func rezip(t testing.TB, src []byte, edit func(name string, raw []byte) (string, []byte, bool)) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		name := f.Name
		if filepath.Base(name) == "level.dat0" {
			raw, err := inflate(data)
			if err != nil {
				t.Fatal(err)
			}
			var compress bool
			name, raw, compress = edit(name, raw)
			if raw == nil {
				continue
			}
			data = raw
			if compress {
				data = deflate(raw)
			}
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func inflate(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func deflate(b []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

func TestUncompressedLevelData(t *testing.T) {
	z := rezip(t, readFixture(t, "2.1.24-space-age.zip"), func(n string, raw []byte) (string, []byte, bool) { return n, raw, false })
	s, err := readBytes(z)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Mods) != 5 || s.Version.String() != "2.1.24" {
		t.Fatalf("got %+v", s)
	}
}

func TestOlderLevelDatName(t *testing.T) {
	z := rezip(t, readFixture(t, "1.1.110-vanilla.zip"), func(n string, raw []byte) (string, []byte, bool) {
		return filepath.Dir(n) + "/level.dat", raw, true
	})
	s, err := readBytes(z)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version.String() != "1.1.110" {
		t.Fatalf("got %+v", s)
	}
}

func TestErrors(t *testing.T) {
	fix := readFixture(t, "2.0.77-space-age.zip")
	cut := func(n int) func(string, []byte) (string, []byte, bool) {
		return func(name string, raw []byte) (string, []byte, bool) { return name, raw[:n], true }
	}
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrNotZip},
		{"not a zip", []byte("hello, this is not a zip file"), ErrNotZip},
		{"zip cut short", fix[:len(fix)/2], ErrNotZip},
		{"no level data", rezip(t, fix, func(string, []byte) (string, []byte, bool) { return "", nil, false }), ErrNoLevelData},
		{"level data empty", rezip(t, fix, cut(0)), ErrTruncated},
		{"header cut in version", rezip(t, fix, cut(5)), ErrTruncated},
		{"header cut in mod list", rezip(t, fix, cut(60)), ErrTruncated},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readBytes(c.data)
			if !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
		})
	}
}

func TestCompressedStreamCutShort(t *testing.T) {
	z := rezip(t, readFixture(t, "2.0.77-space-age.zip"), func(n string, raw []byte) (string, []byte, bool) {
		return n, raw, true
	})
	// Rebuild with the compressed level data cut to 20 bytes, stored as is.
	zr, _ := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if filepath.Base(f.Name) != "level.dat0" {
			continue
		}
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		w, _ := zw.Create(f.Name)
		_, _ = w.Write(data[:20])
	}
	_ = zw.Close()
	_, err := readBytes(out.Bytes())
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("err %v, want ErrTruncated", err)
	}
}

func TestUnsupportedVersion(t *testing.T) {
	z := rezip(t, readFixture(t, "2.1.24-space-age.zip"), func(n string, raw []byte) (string, []byte, bool) {
		raw = bytes.Clone(raw)
		raw[2] = 2 // minor 2
		return n, raw, true
	})
	_, err := readBytes(z)
	var uv *UnsupportedVersionError
	if !errors.As(err, &uv) || uv.Version.String() != "2.2.24" {
		t.Fatalf("err %v, want UnsupportedVersionError for 2.2.24", err)
	}
}

func TestLayoutMismatchIsRefused(t *testing.T) {
	// A 2.x header read with the 1.1 layout leaves the mod list misaligned;
	// the reader must refuse rather than return garbage.
	z := rezip(t, readFixture(t, "2.1.24-space-age.zip"), func(n string, raw []byte) (string, []byte, bool) {
		raw = bytes.Clone(raw)
		raw[0], raw[2] = 1, 1
		return n, raw, true
	})
	_, err := readBytes(z)
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("err %v, want ErrFormat", err)
	}
}

func TestHugeCountsAreRefused(t *testing.T) {
	hdr := []byte{2, 0, 1, 0, 24, 0, 1, 0, 0}
	// Campaign name claims a 4 GiB length.
	raw := append(bytes.Clone(hdr), 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	z := rezip(t, readFixture(t, "2.1.24-space-age.zip"), func(n string, _ []byte) (string, []byte, bool) { return n, raw, false })
	if _, err := readBytes(z); !errors.Is(err, ErrFormat) {
		t.Fatalf("err %v, want ErrFormat", err)
	}
}

// FuzzRead checks that no input panics. Its seeds (every fixture, plus
// their uncompressed level data) run in every `go test`.
func FuzzRead(f *testing.F) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".zip" {
			continue
		}
		b := readFixture(f, e.Name())
		f.Add(b)
		f.Add(rezip(f, b, func(n string, raw []byte) (string, []byte, bool) { return n, raw[:min(len(raw), 512)], false }))
	}
	f.Add([]byte{})
	f.Add([]byte("PK\x03\x04"))
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := readBytes(b)
		if err == nil && (len(s.Mods) == 0 || s.Mods[0].Name != "base") {
			t.Fatalf("accepted a save without base first: %+v", s)
		}
	})
}
