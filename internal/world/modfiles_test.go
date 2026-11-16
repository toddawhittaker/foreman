package world

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// sharedWorld makes a world with the mod zips given as file names, each
// declaring its own name and version in info.json.
func sharedWorld(t *testing.T, files ...string) (m *Manager, id, dir string) {
	t.Helper()
	m, _ = newTestManager(t)
	id, err := m.Create(context.Background(), "Shared", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(m.Dirs().Worlds, id)
	for _, f := range files {
		base := f[:len(f)-len(".zip")]
		i := lastUnderscore(base)
		putModZip(t, dir, f, base, `{"name":"`+base[:i]+`","version":"`+base[i+1:]+`","title":"T `+base[:i]+`"}`)
	}
	return m, id, dir
}

func lastUnderscore(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '_' {
			return i
		}
	}
	return -1
}

func shareFiles(share *ModShare) []string {
	var out []string
	for _, mod := range share.Mods {
		out = append(out, mod.File)
	}
	return out
}

// SPEC 9.16, 9.15: one entry per mod, the zip the game loads.
func TestModShareListsTheHighestVersionOfEachMod(t *testing.T) {
	m, id, _ := sharedWorld(t, "alpha_1.0.0.zip", "alpha_1.10.0.zip", "alpha_1.2.0.zip", "beta_0.1.0.zip")
	share, err := m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := shareFiles(share), []string{"alpha_1.10.0.zip", "beta_0.1.0.zip"}; !slices.Equal(got, want) {
		t.Errorf("offered %v, want %v (1.10.0 is higher than 1.2.0)", got, want)
	}
	if share.Mods[0].Title != "T alpha" {
		t.Errorf("title %q, want it kept for the page", share.Mods[0].Title)
	}
}

// SPEC 9.16: built-in mods and turned-off mods are left out.
func TestModShareLeavesOutBuiltInAndTurnedOffMods(t *testing.T) {
	m, id, dir := sharedWorld(t, "alpha_1.0.0.zip", "beta_1.0.0.zip")
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile),
		`{"mods":[{"name":"base","enabled":true},{"name":"alpha","enabled":true},{"name":"beta","enabled":false}]}`)
	share, err := m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := shareFiles(share), []string{"alpha_1.0.0.zip"}; !slices.Equal(got, want) {
		t.Errorf("offered %v, want %v", got, want)
	}
	for _, mod := range share.Mods {
		if mod.BuiltIn {
			t.Errorf("built-in mod %s offered", mod.Name)
		}
	}
}

// SPEC 9.16: a folder mod is listed, with no file to download.
func TestModShareKeepsAFolderModWithNoDownload(t *testing.T) {
	m, id, dir := sharedWorld(t, "alpha_1.0.0.zip")
	folder := filepath.Join(dir, ModsDir, "local_2.0.0")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(folder, "info.json"), `{"name":"local","version":"2.0.0"}`)
	share, err := m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, mod := range share.Mods {
		if mod.Name == "local" {
			found = true
			if !mod.Local || mod.File != "" {
				t.Errorf("folder mod %+v: want Local and no File", mod)
			}
		}
	}
	if !found {
		t.Fatalf("the folder mod is not listed: %+v", share.Mods)
	}
	for _, name := range []string{"local_2.0.0", "local_2.0.0.zip", "local", ""} {
		if _, _, err := share.Open(name); !errors.Is(err, ErrModFileNotFound) {
			t.Errorf("Open(%q) = %v, want not found", name, err)
		}
	}
}

func TestShareableModsPrefersAZipOverAFolderOfTheSameVersion(t *testing.T) {
	list := []InstalledMod{
		{Name: "a", Version: "1.0.0", Local: true, Enabled: true},
		{Name: "a", Version: "1.0.0", File: "a_1.0.0.zip", Enabled: true},
		{Name: "b", Version: "1.0.0", File: "b_1.0.0.zip", Enabled: true},
		{Name: "b", Version: "1.1.0", Local: true, Enabled: true},
	}
	got := ShareableMods(list)
	if len(got) != 2 || got[0].File != "a_1.0.0.zip" || !got[1].Local {
		t.Errorf("got %+v: want a's zip and b's higher folder", got)
	}
}

// openModFile lists the world's mods and opens one zip, as the public
// download does.
func openModFile(m *Manager, id, file string) (*os.File, fs.FileInfo, error) {
	share, err := m.ModShare(id)
	if err != nil {
		return nil, nil, err
	}
	return share.Open(file)
}

// SPEC 14.18: the file must equal a listed zip's name byte for byte.
func TestModShareOpenOpensOnlyAListedZip(t *testing.T) {
	m, id, dir := sharedWorld(t, "alpha_1.0.0.zip", "alpha_2.0.0.zip", "beta_1.0.0.zip")
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile),
		`{"mods":[{"name":"alpha","enabled":true},{"name":"beta","enabled":false}]}`)
	writeFile(t, filepath.Join(dir, "saves", "a.zip"), "save")
	writeFile(t, filepath.Join(dir, ModsDir, "notes.txt"), "text")

	f, info, err := openModFile(m, id, "alpha_2.0.0.zip")
	if err != nil {
		t.Fatalf("open the offered zip: %v", err)
	}
	defer func() { _ = f.Close() }()
	if b, _ := io.ReadAll(f); int64(len(b)) != info.Size() || len(b) == 0 {
		t.Errorf("read %d bytes, size %d", len(b), info.Size())
	}

	for _, name := range []string{
		"alpha_1.0.0.zip",             // the game loads the higher version
		"beta_1.0.0.zip",              // turned off
		"base",                        // built in
		"notes.txt",                   // not a zip
		"../saves/a.zip",              // path tricks
		"./alpha_2.0.0.zip",           // not byte for byte
		"alpha_2.0.0.zip/",            //
		"ALPHA_2.0.0.zip",             //
		"alpha_2.0.0.zip ",            //
		"mods/alpha_2.0.0.zip",        //
		"..%2Fsaves%2Fa.zip",          //
		"/etc/passwd",                 //
		"alpha_2.0.0.zip\x00.zip",     //
		"",                            //
		ModListFile,                   //
		ModSettingsFile,               //
		"..",                          //
		"alpha_2.0.0.zip/../beta.zip", //
	} {
		if f, _, err := openModFile(m, id, name); !errors.Is(err, ErrModFileNotFound) {
			if f != nil {
				_ = f.Close()
			}
			t.Errorf("openModFile(%q) = %v, want not found", name, err)
		}
	}
}

// SPEC 14.18: another world's id, or one that is not an id, finds nothing.
func TestModShareOpenOtherWorldAndBadIDs(t *testing.T) {
	m, id, _ := sharedWorld(t, "alpha_1.0.0.zip")
	other, err := m.Create(context.Background(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := openModFile(m, other, "alpha_1.0.0.zip"); !errors.Is(err, ErrModFileNotFound) {
		t.Errorf("another world's list: %v, want not found", err)
	}
	for _, bad := range []string{"../" + id, "", "A", id + "/../" + other, "nope"} {
		if f, _, err := openModFile(m, bad, "alpha_1.0.0.zip"); err == nil {
			_ = f.Close()
			t.Errorf("openModFile(world %q) opened a file", bad)
		}
	}
}

// SPEC 14.18, 14.8: a link in the zip's place is never followed, even one
// that points inside the world, and a world swapped after the list is read
// serves nothing.
func TestModShareOpenRefusesALinkAndASwappedWorld(t *testing.T) {
	m, id, dir := sharedWorld(t, "alpha_1.0.0.zip")
	share, err := m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, ModsDir, "alpha_1.0.0.zip")
	secret := filepath.Join(dir, "rconpw")
	writeFile(t, secret, "secret")
	if err := os.Remove(zipPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, zipPath); err != nil {
		t.Fatal(err)
	}
	if f, _, err := share.Open("alpha_1.0.0.zip"); !errors.Is(err, ErrModFileNotFound) {
		if f != nil {
			_ = f.Close()
		}
		t.Errorf("a link in the zip's place: %v, want not found", err)
	}
	if share, err := m.ModShare(id); err != nil || len(share.Mods) != 0 {
		t.Errorf("a link is listed as a mod: %v, %+v", err, share)
	}

	// The same name now leads to another folder.
	if err := os.Remove(zipPath); err != nil {
		t.Fatal(err)
	}
	putModZip(t, dir, "alpha_1.0.0.zip", "alpha_1.0.0", `{"name":"alpha","version":"1.0.0"}`)
	share, err = m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	moved := dir + ".moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ModsDir, "alpha_1.0.0.zip"), "another world's file")
	if f, _, err := share.Open("alpha_1.0.0.zip"); !errors.Is(err, ErrModFileNotFound) {
		if f != nil {
			_ = f.Close()
		}
		t.Errorf("a swapped world: %v, want not found", err)
	}
}

// SPEC 14.18: a mods folder that is a link, even to a folder inside the
// world, lists nothing and serves nothing, whether it was a link when the
// list was read or became one after.
func TestModShareRefusesAModsFolderThatIsALink(t *testing.T) {
	m, id, dir := sharedWorld(t, "alpha_1.0.0.zip")
	share, err := m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(share.Mods) != 1 {
		t.Fatalf("listed %+v, want alpha", share.Mods)
	}

	saves := filepath.Join(dir, "saves")
	writeFile(t, filepath.Join(saves, "alpha_1.0.0.zip"), "a save, not a mod")
	writeFile(t, filepath.Join(saves, "beta_1.0.0.zip"), "another save")
	if err := os.RemoveAll(filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("saves", filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}

	if f, _, err := share.Open("alpha_1.0.0.zip"); !errors.Is(err, ErrModFileNotFound) {
		if f != nil {
			_ = f.Close()
		}
		t.Errorf("a list read before the swap: %v, want not found", err)
	}
	fresh, err := m.ModShare(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Mods) != 0 {
		t.Errorf("a linked mods folder is listed: %+v", fresh.Mods)
	}
	for _, name := range []string{"alpha_1.0.0.zip", "beta_1.0.0.zip"} {
		if f, _, err := fresh.Open(name); !errors.Is(err, ErrModFileNotFound) {
			if f != nil {
				_ = f.Close()
			}
			t.Errorf("Open(%q) through a linked folder: %v, want not found", name, err)
		}
	}
}
