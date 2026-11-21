package world

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The error texts name the supported lines. They are built from mapgen.Lines,
// so this pins today's wording and fails when a line is added, as a reminder
// to look at the texts and the translations that repeat them.
func TestSupportedLinesTexts(t *testing.T) {
	if got := mapgen.Lines; len(got) != 3 || got[0] != "1.1" || got[1] != "2.0" || got[2] != "2.1" {
		t.Fatalf("mapgen.Lines = %v: update the texts below and the locale files", got)
	}
	if got, want := ErrBadGameVersion.Error(), "enter the game version as three numbers from a supported line (1.1, 2.0 or 2.1), such as 2.0.77"; got != want {
		t.Errorf("ErrBadGameVersion = %q, want %q", got, want)
	}
	if got, want := ErrVersionUnsupported.Error(), "enter an exact Factorio release of 1.1, 2.0 or 2.1, such as 2.0.72"; got != want {
		t.Errorf("ErrVersionUnsupported = %q, want %q", got, want)
	}
}

func TestPreCheckUnsupportedTexts(t *testing.T) {
	t.Run("world version", func(t *testing.T) {
		m, id, dir := checkWorld(t, "1.0.0", false)
		putFixture(t, dir, "1.1.110-vanilla.zip", "a.zip", t0)
		p := runCheck(t, m, id)
		wantKinds(t, p, ProblemGameVersion)
		if got, want := p.Problems[0].Text, "The world is set to Factorio 1.0.0. Foreman runs only Factorio 1.1, 2.0 and 2.1."; got != want {
			t.Errorf("text %q, want %q", got, want)
		}
	})
	t.Run("save version", func(t *testing.T) {
		m, id, dir := checkWorld(t, "2.1.24", false)
		putFixture(t, dir, "2.1.24-no-space-age.zip", "a.zip", t0)
		patchSaveMinor(t, filepath.Join(dir, SavesDir, "a.zip"), 2)
		putModList(t, dir, "base", "recycler")
		p := runCheck(t, m, id)
		wantKinds(t, p, ProblemSaveUnsupported)
		if got, want := p.Problems[0].Text, "The save a.zip was made by Factorio 2.2.24. Foreman runs only saves from Factorio 1.1, 2.0 and 2.1."; got != want {
			t.Errorf("text %q, want %q", got, want)
		}
	})
}

// patchSaveMinor rewrites the zip's level.dat0, uncompressed, with the game's
// minor version changed.
func patchSaveMinor(t *testing.T, name string, minor byte) {
	t.Helper()
	src, err := os.ReadFile(name) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
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
		if filepath.Base(f.Name) == "level.dat0" {
			zr, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if data, err = io.ReadAll(zr); err != nil {
				t.Fatal(err)
			}
			data[2] = minor
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
	if err := os.WriteFile(name, out.Bytes(), 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
}
