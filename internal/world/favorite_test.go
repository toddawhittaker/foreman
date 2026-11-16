package world

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestFavorites(t *testing.T) (*Favorites, *Manager) {
	t.Helper()
	m, _ := newTestManager(t)
	f, err := NewFavorites(filepath.Join(filepath.Dir(m.Dirs().Worlds), "favorites"), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f, m
}

func writeWorldFile(t *testing.T, m *Manager, id, rel string, data []byte) {
	t.Helper()
	p := filepath.Join(m.Dirs().Worlds, id, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
}

func addZipMod(t *testing.T, m *Manager, id, name, version string) {
	t.Helper()
	writeWorldFile(t, m, id, filepath.Join(ModsDir, name+"_"+version+".zip"), modZip(t, name+"_"+version,
		fmt.Sprintf(`{"name":%q,"version":%q,"factorio_version":"2.0"}`, name, version)))
}

const secretSettings = `{"name":"My server","game_password":"hunter2-game","username":"alice-portal","password":"hunter2-account","token":"tok-abc123","max_players":8}` //nolint:gosec // fake secrets the test looks for

// allBytes reads every file under a folder, for a secret search.
func allBytes(t *testing.T, dir string) []byte {
	t.Helper()
	var out []byte
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a test temp folder
		out = append(out, b...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFavoriteKeepsSettingsAndModsWithoutSecrets(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Generate(ctx, NewWorld{Name: "Source", GameVersion: "2.0.77", SpaceAge: true, Preset: "rich-resources",
		MapGenSettings: []byte(`{"width":0}`), MapSettings: []byte(`{"pollution":{}}`)})
	if err != nil {
		t.Fatal(err)
	}
	writeWorldFile(t, m, id, filepath.Join(ConfigDir, ServerSettingsFile), []byte(secretSettings))
	writeWorldFile(t, m, id, filepath.Join(ModsDir, ModSettingsFile), []byte("modsettings-bytes"))
	addZipMod(t, m, id, "Krastorio2", "1.3.24")
	addZipMod(t, m, id, "flib", "0.12.9")
	writeWorldFile(t, m, id, filepath.Join(ModsDir, "mylocal", "info.json"), []byte(`{"name":"mylocal","version":"0.1.0"}`))
	worldBefore := allBytes(t, filepath.Join(m.Dirs().Worlds, id))

	favID, err := f.Make(ctx, id, "  Big base  ")
	if err != nil {
		t.Fatal(err)
	}
	stored := allBytes(t, filepath.Join(f.dir, favID))
	for _, s := range []string{"hunter2-game", "alice-portal", "hunter2-account", "tok-abc123", "game_password", `"token"`} {
		if bytes.Contains(stored, []byte(s)) {
			t.Errorf("stored favorite holds secret %q", s)
		}
	}
	if !bytes.Equal(worldBefore, allBytes(t, filepath.Join(m.Dirs().Worlds, id))) {
		t.Error("making a favorite changed the world's files")
	}

	fav, files, err := f.Get(favID)
	if err != nil {
		t.Fatal(err)
	}
	if fav.ID != favID || fav.Name != "Big base" || fav.SourceWorld != "Source" || fav.GameVersion != "2.0.77" ||
		!fav.SpaceAge || fav.Preset != "rich-resources" || !fav.HasMapSettings || fav.CreatedAt.IsZero() {
		t.Errorf("favorite: %+v", fav)
	}
	want := []FavoriteMod{{Name: "Krastorio2", Version: "1.3.24"}, {Name: "flib", Version: "0.12.9"}}
	if fmt.Sprint(fav.Mods) != fmt.Sprint(want) {
		t.Errorf("mods %v, want %v", fav.Mods, want)
	}
	if fmt.Sprint(fav.LeftOut) != "[mylocal]" {
		t.Errorf("left out %v", fav.LeftOut)
	}
	if string(files.MapGenSettings) != `{"width":0}` || string(files.MapSettings) != `{"pollution":{}}` ||
		string(files.ModSettings) != "modsettings-bytes" || !strings.Contains(string(files.ServerSettings), `"max_players":8`) {
		t.Errorf("files: %+v", files)
	}
}

func TestFavoriteOfImportedWorldHasNoMapSettings(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Create(ctx, "Imported", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	favID, err := f.Make(ctx, id, "Plain")
	if err != nil {
		t.Fatal(err)
	}
	fav, files, err := f.Get(favID)
	if err != nil {
		t.Fatal(err)
	}
	if fav.HasMapSettings || files.MapGenSettings != nil || files.MapSettings != nil || files.ServerSettings != nil || files.ModSettings != nil {
		t.Errorf("favorite %+v files %+v", fav, files)
	}
}

func TestFavoriteNamesListAndDelete(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Create(ctx, "W", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", MaxFavoriteName+1), "bad\nname"} {
		if _, err := f.Make(ctx, id, bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	var ids []string
	for _, n := range []string{"charlie", "Alpha", "bravo"} {
		fid, err := f.Make(ctx, id, n)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, fid)
	}
	if _, err := f.Make(ctx, id, "ALPHA"); !errors.Is(err, ErrFavoriteNameTaken) {
		t.Errorf("duplicate name: %v", err)
	}
	list, err := f.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range list {
		names = append(names, x.Name)
	}
	if fmt.Sprint(names) != "[Alpha bravo charlie]" {
		t.Errorf("order %v", names)
	}
	if err := f.Delete(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Get(ids[0]); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("deleted favorite: %v", err)
	}
	if list, _ := f.List(ctx); len(list) != 2 {
		t.Errorf("after delete %d favorites", len(list))
	}
	for _, bad := range []string{"../worlds", "", "ABCDEF0123456789", ids[0]} {
		if err := f.Delete(bad); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("delete %q: %v", bad, err)
		}
	}
}

func TestDamagedFavoriteIsRefused(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Create(ctx, "W", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(dir string){
		"malformed json": func(dir string) { _ = os.WriteFile(filepath.Join(dir, FavoriteFile), []byte("{not json"), 0o600) },
		"bad mod name": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, FavoriteFile), []byte(`{"format_version":1,"name":"x","game_version":"2.0.77","mods":[{"name":"../x","version":"1.0.0"}]}`), 0o600)
		},
		"oversized map file": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, MapSettingsFile), bytes.Repeat([]byte(" "), maxConfigFile+1), 0o600)
		},
		"oversized mod settings": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, ModSettingsFile), make([]byte, MaxModSettings+1), 0o600)
		},
		"link for a file": func(dir string) {
			_ = os.Remove(filepath.Join(dir, ServerSettingsFile))
			_ = os.Symlink("/etc/passwd", filepath.Join(dir, ServerSettingsFile))
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			favID, err := f.Make(ctx, id, "fav "+name)
			if err != nil {
				t.Fatal(err)
			}
			spoil(filepath.Join(f.dir, favID))
			if _, _, err := f.Get(favID); err == nil {
				t.Error("damaged favorite was read")
			}
		})
	}
}

func TestFavoriteWithPlantedSecretReadsWithout(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Create(ctx, "W", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	favID, err := f.Make(ctx, id, "fav")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, favID, ServerSettingsFile), []byte(secretSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	_, files, err := f.Get(favID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(files.ServerSettings, []byte("hunter2")) || bytes.Contains(files.ServerSettings, []byte("tok-abc")) {
		t.Errorf("secret came back: %s", files.ServerSettings)
	}
}

func TestNewFavoritesClearsHalfMadeFavorites(t *testing.T) {
	f, m := newTestFavorites(t)
	half := filepath.Join(f.dir, PrefixStaging+"0123456789abcdef")
	if err := os.Mkdir(half, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFavorites(f.dir, m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(half); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("half-made favorite left: %v", err)
	}
}

func TestGenerateWritesFavoriteSettings(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Generate(context.Background(), NewWorld{Name: "From fav", GameVersion: "2.0.77",
		ServerSettings: []byte(secretSettings), ModSettings: []byte("modsettings-bytes")})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dirs().Worlds, id)
	server, err := os.ReadFile(filepath.Join(dir, ConfigDir, ServerSettingsFile)) //nolint:gosec // a test temp folder
	if err != nil || !bytes.Contains(server, []byte(`"max_players":8`)) || bytes.Contains(server, []byte("hunter2")) || bytes.Contains(server, []byte("tok-abc")) {
		t.Errorf("server settings %s, %v", server, err)
	}
	if got := mode(t, filepath.Join(dir, ConfigDir, ServerSettingsFile)); got != 0o640 {
		t.Errorf("server settings mode %o", got)
	}
	ms, err := os.ReadFile(filepath.Join(dir, ModsDir, ModSettingsFile)) //nolint:gosec // a test temp folder
	if err != nil || string(ms) != "modsettings-bytes" {
		t.Errorf("mod settings %q, %v", ms, err)
	}
	if _, err := m.Generate(context.Background(), NewWorld{Name: "Bad", GameVersion: "2.0.77", ServerSettings: []byte("[1]")}); !errors.Is(err, ErrBadServerSettings) {
		t.Errorf("bad server settings: %v", err)
	}
}

func TestConcurrentMakesWithOneNameGiveOneFavorite(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Create(ctx, "W", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := f.Make(ctx, id, "Big base")
			errs <- err
		}()
	}
	made := 0
	for range n {
		switch err := <-errs; {
		case err == nil:
			made++
		case !errors.Is(err, ErrFavoriteNameTaken):
			t.Errorf("make: %v", err)
		}
	}
	if list, _ := f.List(ctx); made != 1 || len(list) != 1 {
		t.Errorf("made %d, listed %d, want 1", made, len(list))
	}
}

func TestFavoriteWithBadLeftOutIsRefused(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	id, err := m.Create(ctx, "W", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	for name, left := range map[string]string{
		"oversized entry": `["` + strings.Repeat("x", 10000) + `"]`,
		"control chars":   `["a\u0007b"]`,
		"too many":        "[" + strings.TrimSuffix(strings.Repeat(`"m",`, maxFavoriteMods+1), ",") + "]",
	} {
		t.Run(name, func(t *testing.T) {
			favID, err := f.Make(ctx, id, "fav "+name)
			if err != nil {
				t.Fatal(err)
			}
			desc := `{"format_version":1,"name":"fav ` + name + `","game_version":"2.0.77","mods":[],"left_out":` + left + `}`
			if err := os.WriteFile(filepath.Join(f.dir, favID, FavoriteFile), []byte(desc), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := f.Get(favID); !errors.Is(err, ErrFavoriteBroken) {
				t.Errorf("get: %v", err)
			}
		})
	}
}

func TestGenerateRefusesOversizedModSettings(t *testing.T) {
	m, _ := newTestManager(t)
	_, err := m.Generate(context.Background(), NewWorld{Name: "Big", GameVersion: "2.0.77", ModSettings: make([]byte, MaxModSettings+1)})
	if !errors.Is(err, ErrModSettingsTooBig) {
		t.Errorf("oversized mod settings: %v", err)
	}
	if list, _ := m.List(context.Background()); len(list) != 0 {
		t.Errorf("a world was made: %v", list)
	}
}

func TestFavoriteRename(t *testing.T) {
	ctx := context.Background()
	f, m := newTestFavorites(t)
	wid, err := m.Create(ctx, "W", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.Make(ctx, wid, "Club start")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Make(ctx, wid, "Other"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]error{
		"   ":                                  ErrFavoriteNameEmpty,
		strings.Repeat("x", MaxFavoriteName+1): ErrFavoriteNameBad,
		"OTHER":                                ErrFavoriteNameTaken,
	} {
		if err := f.Rename(ctx, id, name); !errors.Is(err, want) {
			t.Errorf("rename to %q: %v, want %v", name, err, want)
		}
	}
	if err := f.Rename(ctx, "ffffffffffffffff", "New"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing favorite: %v", err)
	}
	if err := f.Rename(ctx, "../x", "New"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("bad id: %v", err)
	}
	if err := f.Rename(ctx, id, "CLUB START"); err != nil {
		t.Errorf("case-only change: %v", err)
	}
	if err := f.Rename(ctx, id, "  Rail world  "); err != nil {
		t.Fatal(err)
	}
	fav, _, err := f.Get(id)
	if err != nil || fav.Name != "Rail world" || fav.GameVersion != "2.0.77" || fav.FormatVersion != favoriteFormat {
		t.Errorf("after rename %+v, %v", fav, err)
	}
	exported, data, err := f.Export(id)
	if err != nil || exported.Name != "Rail world" {
		t.Fatalf("export %+v, %v", exported, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range zr.File {
		if e.Name == FavoriteFile {
			rc, _ := e.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			found = bytes.Contains(b, []byte(`"Rail world"`))
		}
	}
	if !found {
		t.Error("the exported favorite.json lacks the new name")
	}
}
