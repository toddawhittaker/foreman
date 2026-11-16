package world

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportedFavorite makes a favorite with every settings file and mods, and
// returns its export.
func exportedFavorite(t *testing.T) (*Favorites, string, []byte) {
	t.Helper()
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Generate(ctx, NewWorld{Name: "Source", GameVersion: "2.0.77", SpaceAge: true, Preset: "rich-resources",
		MapGenSettings: []byte(`{"width":0}`), MapSettings: []byte(`{"pollution":{}}`)})
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, m, id, filepath.Join(ConfigDir, ServerSettingsFile), []byte(secretSettings))
	writeWorldFile(t, m, id, filepath.Join(ModsDir, ModSettingsFile), []byte("modsettings-bytes"))
	writeWorldFile(t, m, id, filepath.Join(ModsDir, "Krastorio2_1.3.24.zip"), modZip(t, "Krastorio2_1.3.24",
		`{"name":"Krastorio2","version":"1.3.24","factorio_version":"2.0","title":"Krastorio 2"}`))
	favID, err := f.Make(ctx, id, "Big base")
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := f.Export(favID)
	if err != nil {
		t.Fatal(err)
	}
	return f, favID, data
}

// zipOf builds a zip from name and content pairs.
func zipOf(t *testing.T, entries ...[2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const goodFormat = `{"format_version":1}`

func goodDesc(extra string) string {
	return `{"name":"Shared","created_at":"2026-11-15T00:00:00Z","source_world":"W","game_version":"2.0.77","space_age":false,` +
		`"has_map_settings":false,"mods":[{"name":"flib","version":"0.12.9"}],"left_out":[],"format_version":1` + extra + `}`
}

func TestFavoriteExportImportRoundTrip(t *testing.T) {
	f, favID, data := exportedFavorite(t)
	if bytes.Contains(data, []byte("hunter2")) {
		t.Fatal("the export holds a secret")
	}
	// The same Foreman refuses the clash; another one takes the file.
	if _, err := f.Import(context.Background(), data); !errors.Is(err, ErrFavoriteNameTaken) {
		t.Fatalf("import over its own name: %v, want ErrFavoriteNameTaken", err)
	}
	other, _ := newTestFavorites(t)
	newID, err := other.Import(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	want, wantFiles, _ := f.Get(favID)
	if want.Mods[0].Title != "Krastorio 2" {
		t.Fatalf("the favorite kept title %q, want the mod's", want.Mods[0].Title)
	}
	got, gotFiles, err := other.Get(newID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != want.Name || got.GameVersion != want.GameVersion || !got.SpaceAge || got.Preset != want.Preset ||
		len(got.Mods) != 1 || got.Mods[0] != want.Mods[0] || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("imported %+v, want %+v", got, want)
	}
	if !bytes.Equal(gotFiles.MapGenSettings, wantFiles.MapGenSettings) || !bytes.Equal(gotFiles.MapSettings, wantFiles.MapSettings) ||
		!bytes.Equal(gotFiles.ServerSettings, wantFiles.ServerSettings) || string(gotFiles.ModSettings) != "modsettings-bytes" {
		t.Errorf("imported files differ: %+v", gotFiles)
	}
}

func TestFavoriteImportTakesUnknownModsAndVersions(t *testing.T) {
	f, _ := newTestFavorites(t)
	data := zipOf(t, [2]string{FavoriteFormatFile, goodFormat},
		[2]string{FavoriteFile, strings.Replace(goodDesc(""), "2.0.77", "9.9.99", 1)})
	if _, err := f.Import(context.Background(), data); err != nil {
		t.Fatalf("a version this Foreman lacks is refused: %v", err)
	}
}

func TestFavoriteImportRefusals(t *testing.T) {
	big := bytes.Repeat([]byte{0}, MaxFavoriteUpload+1)
	symlink := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for _, e := range [][2]string{{FavoriteFormatFile, goodFormat}, {FavoriteFile, goodDesc("")}} {
			w, _ := zw.Create(e[0])
			_, _ = w.Write([]byte(e[1]))
		}
		h := &zip.FileHeader{Name: MapSettingsFile}
		h.SetMode(fs.ModeSymlink | 0o777)
		w, _ := zw.CreateHeader(h)
		_, _ = w.Write([]byte("/etc/passwd"))
		_ = zw.Close()
		return buf.Bytes()
	}
	tests := map[string][]byte{
		"oversize":           big,
		"not a zip":          []byte("hello"),
		"no format file":     zipOf(t, [2]string{FavoriteFile, goodDesc("")}),
		"bad format version": zipOf(t, [2]string{FavoriteFormatFile, `{"format_version":2}`}, [2]string{FavoriteFile, goodDesc("")}),
		"format extra field": zipOf(t, [2]string{FavoriteFormatFile, `{"format_version":1,"x":1}`}, [2]string{FavoriteFile, goodDesc("")}),
		"traversal":          zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc("")}, [2]string{"../map-settings.json", "{}"}),
		"folder entry":       zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{"x/" + FavoriteFile, goodDesc("")}),
		"unlisted entry":     zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc("")}, [2]string{"run.sh", "x"}),
		"duplicate entry":    zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc("")}, [2]string{FavoriteFile, goodDesc("")}),
		"symlink entry":      symlink(),
		"unknown field":      zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc(`,"extra":true`)}),
		"bad mod name":       zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, strings.Replace(goodDesc(""), `"flib"`, `"../flib"`, 1)}),
		"bad mod version":    zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, strings.Replace(goodDesc(""), "0.12.9", "1;rm", 1)}),
		"bad title":          zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, strings.Replace(goodDesc(""), `"0.12.9"`, `"0.12.9","title":"a\u0007b"`, 1)}),
		"bad game version":   zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, strings.Replace(goodDesc(""), "2.0.77", "latest", 1)}),
		"bad settings JSON":  zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc("")}, [2]string{MapSettingsFile, "{"}),
		"entry too big":      zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc("")}, [2]string{ServerSettingsFile, `"` + strings.Repeat("a", maxConfigFile) + `"`}),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			f, _ := newTestFavorites(t)
			if _, err := f.Import(context.Background(), data); !errors.Is(err, ErrFavoriteFileBad) {
				t.Fatalf("got %v, want ErrFavoriteFileBad", err)
			}
			entries, err := os.ReadDir(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("a refused import left %d entries", len(entries))
			}
		})
	}
}

func TestFavoriteImportStripsPlantedSecret(t *testing.T) {
	f, _ := newTestFavorites(t)
	data := zipOf(t, [2]string{FavoriteFormatFile, goodFormat}, [2]string{FavoriteFile, goodDesc("")},
		[2]string{ServerSettingsFile, secretSettings})
	id, err := f.Import(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(allBytes(t, filepath.Join(f.dir, id)), []byte("hunter2")) {
		t.Error("an imported secret was kept")
	}
}
