package savefile

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapexchange"
)

// payload is the inflated body of an exchange string: what the game means,
// whatever compressor wrote it.
func payload(t testing.TB, s string) []byte {
	t.Helper()
	s = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s), ">>>"), "<<<")
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// editInit rewrites a save's level-init.dat; edit returns nil to drop it.
func editInit(t testing.TB, src []byte, edit func(raw []byte) []byte) []byte {
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
		if path.Base(f.Name) == levelInitName {
			if data = edit(data); data == nil {
				continue
			}
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func exchangeBytes(b []byte) (string, error) {
	return ExchangeString(bytes.NewReader(b), int64(len(b)))
}

// The game's own LuaSurface.get_map_exchange_string() for each test save
// (testdata/mxstrings, made by gen/mxgen.sh) is the expected answer.
func TestExchangeStringMatchesTheGame(t *testing.T) {
	saves, err := filepath.Glob("testdata/*.zip")
	if err != nil || len(saves) != 8 {
		t.Fatalf("want the 8 test saves, got %d (%v)", len(saves), err)
	}
	for _, p := range saves {
		name := strings.TrimSuffix(filepath.Base(p), ".zip")
		t.Run(name, func(t *testing.T) {
			want := readFixture(t, "mxstrings/"+name+".txt")
			got, err := exchangeBytes(readFixture(t, name+".zip"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload(t, got), payload(t, string(want))) {
				t.Fatal("payload differs from the game's string")
			}
			g, err := mapexchange.Decode(got)
			if err != nil {
				t.Fatal(err)
			}
			w, err := mapexchange.Decode(string(want))
			if err != nil {
				t.Fatal(err)
			}
			if g.Version != w.Version || !bytes.Equal(g.MapGenSettings, w.MapGenSettings) || !bytes.Equal(g.MapSettings, w.MapSettings) {
				t.Fatal("decoded settings differ from the game's string")
			}
		})
	}
}

func TestExchangeStringRefusesBadSaves(t *testing.T) {
	good := readFixture(t, "2.1.24-space-age.zip")
	cut := func(n int) func([]byte) []byte { return func(raw []byte) []byte { return raw[:min(len(raw), n)] } }
	garbage := func(raw []byte) []byte {
		g := bytes.Repeat([]byte{0xff}, len(raw))
		copy(g, raw[:9])
		return g
	}
	compressed := func(raw []byte) []byte {
		var b bytes.Buffer
		zw := zlib.NewWriter(&b)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		return b.Bytes()
	}
	t.Run("compressed level-init.dat", func(t *testing.T) {
		want, err := exchangeBytes(good)
		if err != nil {
			t.Fatal(err)
		}
		got, err := exchangeBytes(editInit(t, good, compressed))
		if err != nil || got != want {
			t.Fatalf("got %v, want the same string as uncompressed", err)
		}
	})
	cases := []struct {
		name string
		save []byte
		want error
	}{
		{"not a zip", []byte("hello"), ErrNotZip},
		{"no level-init.dat", editInit(t, good, func([]byte) []byte { return nil }), ErrNoLevelInit},
		{"header only", editInit(t, good, cut(9)), ErrTruncated},
		{"cut before the map settings", editInit(t, good, cut(250)), ErrNoMapSettings},
		{"cut before the surface", editInit(t, good, cut(100000)), ErrNoMapSettings},
		{"garbage after the header", editInit(t, good, garbage), ErrFormat},
		{"one byte", editInit(t, good, cut(1)), ErrTruncated},
		{"compressed stream cut short", editInit(t, good, func(raw []byte) []byte {
			c := compressed(raw)
			return c[:len(c)/2]
		}), ErrFormat},
		{"bad compressed header", editInit(t, good, func([]byte) []byte { return []byte{0x78, 0x01, 0xff, 0xff} }), ErrFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := exchangeBytes(c.save)
			if !errors.Is(err, c.want) {
				t.Fatalf("got %q, %v; want %v", s, err, c.want)
			}
		})
	}
}

func TestExchangeStringRefusesUnknownVersion(t *testing.T) {
	z := editInit(t, readFixture(t, "2.1.24-space-age.zip"), func(raw []byte) []byte {
		raw = bytes.Clone(raw)
		raw[2] = 9
		return raw
	})
	var uv *UnsupportedVersionError
	if _, err := exchangeBytes(z); !errors.As(err, &uv) {
		t.Fatalf("err %v, want *UnsupportedVersionError", err)
	}
}

// FuzzExchangeString checks that no input panics and that anything accepted
// is a string the decoder reads.
func FuzzExchangeString(f *testing.F) {
	for _, n := range []string{"1.1.110-vanilla.zip", "2.0.77-space-age.zip", "2.1.24-no-space-age.zip"} {
		b := readFixture(f, n)
		f.Add(b)
		f.Add(editInit(f, b, func(raw []byte) []byte { return raw[:min(len(raw), 2048)] }))
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := exchangeBytes(b)
		if err != nil {
			return
		}
		if _, err := mapexchange.Decode(s); err != nil {
			t.Fatalf("accepted a string the decoder refuses: %v", err)
		}
	})
}
