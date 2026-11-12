package world

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// putSettings writes the mods folder's mod-settings.dat.
func putSettings(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ModsDir, ModSettingsFile), content)
}

func settingsKey(t *testing.T, m *Manager, id string) string {
	t.Helper()
	k, err := m.ModSettingsKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestModSettingsKeyIgnoresModSettingsDat(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	putModList(t, dir, "base", "flib")
	before := settingsKey(t, m, id)
	fpBefore := fingerprint(t, m, id)

	putSettings(t, dir, "first")
	if got := settingsKey(t, m, id); got != before {
		t.Fatal("adding mod-settings.dat changed the key")
	}
	writeFile(t, filepath.Join(dir, ModsDir, ModSettingsFile), "second, and longer")
	if err := os.Chtimes(filepath.Join(dir, ModsDir, ModSettingsFile), t0, t0); err != nil {
		t.Fatal(err)
	}
	if got := settingsKey(t, m, id); got != before {
		t.Fatal("changing mod-settings.dat changed the key")
	}
	if err := os.Remove(filepath.Join(dir, ModsDir, ModSettingsFile)); err != nil {
		t.Fatal(err)
	}
	if got := settingsKey(t, m, id); got != before {
		t.Fatal("removing mod-settings.dat changed the key")
	}
	// The plain fingerprint still sees the file, so a stale mod plan is
	// still refused after a settings change.
	putSettings(t, dir, "third")
	if fingerprint(t, m, id) == fpBefore {
		t.Fatal("the mods fingerprint should still include mod-settings.dat")
	}
}

func TestModSettingsKeyChangesWithMods(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	putModList(t, dir, "base", "flib")
	base := settingsKey(t, m, id)
	if settingsKey(t, m, id) != base {
		t.Fatal("the key is not stable")
	}

	putModFor(t, dir, "other", "1.0.0", "2.0")
	installed := settingsKey(t, m, id)
	if installed == base {
		t.Fatal("installing a mod did not change the key")
	}
	if err := os.Remove(filepath.Join(dir, ModsDir, "other_1.0.0.zip")); err != nil {
		t.Fatal(err)
	}
	if settingsKey(t, m, id) == installed {
		t.Fatal("removing a mod did not change the key")
	}

	putModListOff(t, dir, []string{"base"}, "flib")
	if settingsKey(t, m, id) == base {
		t.Fatal("disabling a mod did not change the key")
	}
}

func TestModSettingsKeyChangesWithGameVersion(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	before := settingsKey(t, m, id)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	meta.GameVersion = "2.0.78"
	if err := WriteMeta(root, meta); err != nil {
		t.Fatal(err)
	}
	if settingsKey(t, m, id) == before {
		t.Fatal("moving the game version did not change the key")
	}
}

func TestModSettingsKeyUnknownWorld(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.ModSettingsKey("nope"); err == nil {
		t.Fatal("want an error for a world that is not there")
	}
}

// linkDst makes an empty work folder for LinkModsInto.
func linkDst(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(ia, ib)
}

func TestLinkModsIntoLinksZipsAndCopiesTheRest(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	putModList(t, dir, "base", "flib")
	putSettings(t, dir, "values")
	folder := filepath.Join(dir, ModsDir, "devmod_1.0.0", "locale", "en")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ModsDir, "devmod_1.0.0", "info.json"), `{"name":"devmod"}`)
	writeFile(t, filepath.Join(folder, "a.cfg"), "[mod-setting-name]\nx=X\n")
	writeFile(t, filepath.Join(dir, ModsDir, "notes.txt"), "not a mod")
	writeFile(t, filepath.Join(dir, ModsDir, ".partial-abc"), "temp")

	dst := linkDst(t)
	if err := m.LinkModsInto(id, dst); err != nil {
		t.Fatal(err)
	}
	zipSrc, zipDst := filepath.Join(dir, ModsDir, "flib_0.16.2.zip"), filepath.Join(dst, ModsDir, "flib_0.16.2.zip")
	if !sameFile(t, zipSrc, zipDst) {
		t.Fatal("the zip was not hard-linked")
	}
	listSrc, listDst := filepath.Join(dir, ModsDir, ModListFile), filepath.Join(dst, ModsDir, ModListFile)
	if sameFile(t, listSrc, listDst) {
		t.Fatal("mod-list.json must be a copy, not a link")
	}
	a, _ := os.ReadFile(listSrc) //nolint:gosec // a test temp folder
	b, _ := os.ReadFile(listDst) //nolint:gosec // a test temp folder
	if !bytes.Equal(a, b) || len(a) == 0 {
		t.Fatal("mod-list.json was not copied")
	}
	got, err := os.ReadFile(filepath.Join(dst, ModsDir, "devmod_1.0.0", "locale", "en", "a.cfg")) //nolint:gosec // a test temp folder
	if err != nil || string(got) != "[mod-setting-name]\nx=X\n" {
		t.Fatalf("folder mod not copied: %q %v", got, err)
	}
	if sameFile(t, filepath.Join(folder, "a.cfg"), filepath.Join(dst, ModsDir, "devmod_1.0.0", "locale", "en", "a.cfg")) {
		t.Fatal("a folder mod's files must be copies")
	}
	for _, name := range []string{ModSettingsFile, "notes.txt", ".partial-abc"} {
		if exists(filepath.Join(dst, ModsDir, name)) {
			t.Fatalf("%s should not be in the work folder", name)
		}
	}
}

func TestLinkModsIntoNeverCarriesModSettingsDat(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	putSettings(t, dir, "the world's values")
	dst := linkDst(t)
	if err := m.LinkModsInto(id, dst); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dst, ModsDir, ModSettingsFile)) {
		t.Fatal("mod-settings.dat reached the work folder")
	}
	// A run writes new files beside the linked ones. That never reaches the
	// world's files, which share only the zip's inode.
	writeFile(t, filepath.Join(dst, ModsDir, ModSettingsFile), "the run's defaults")
	b, _ := os.ReadFile(filepath.Join(dir, ModsDir, ModSettingsFile)) //nolint:gosec // a test temp folder
	if string(b) != "the world's values" {
		t.Fatal("a write into the work folder changed the world")
	}
}

func TestLinkModsIntoWritesTheDefaultModListWhenThereIsNone(t *testing.T) {
	for _, spaceAge := range []bool{false, true} {
		m, id, dir := checkWorld(t, "2.0.77", spaceAge)
		putModFor(t, dir, "flib", "0.16.2", "2.0")
		dst := linkDst(t)
		if err := m.LinkModsInto(id, dst); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dst, ModsDir, ModListFile)) //nolint:gosec // a test temp folder
		if err != nil {
			t.Fatal(err)
		}
		if want := DefaultModList("2.0.77", spaceAge); !bytes.Equal(got, want) {
			t.Fatalf("space age %v: got %s, want %s", spaceAge, got, want)
		}
		if exists(filepath.Join(dir, ModsDir, ModListFile)) {
			t.Fatal("the world's own folder must not get a mod-list.json")
		}
	}
}

func TestLinkModsIntoWorldWithoutModsFolder(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if err := os.RemoveAll(filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	dst := linkDst(t)
	if err := m.LinkModsInto(id, dst); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dst, ModsDir, ModListFile)) {
		t.Fatal("want a default mod-list.json")
	}
}

func TestLinkModsIntoCopiesWhenLinkingFails(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	putModList(t, dir, "base", "flib")
	for _, errno := range []error{syscall.EXDEV, syscall.EPERM, errors.New("some other link failure")} {
		old := linkInto
		linkInto = func(_, _ *os.Root, _ string) error { return errno }
		dst := linkDst(t)
		err := m.LinkModsInto(id, dst)
		linkInto = old
		if err != nil {
			t.Fatalf("%v: %v", errno, err)
		}
		src, cp := filepath.Join(dir, ModsDir, "flib_0.16.2.zip"), filepath.Join(dst, ModsDir, "flib_0.16.2.zip")
		if sameFile(t, src, cp) {
			t.Fatalf("%v: expected a copy", errno)
		}
		a, _ := os.ReadFile(src) //nolint:gosec // a test temp folder
		b, _ := os.ReadFile(cp)  //nolint:gosec // a test temp folder
		if !bytes.Equal(a, b) || len(a) == 0 {
			t.Fatalf("%v: copy differs", errno)
		}
	}
}

func TestLinkModsIntoRefusesLinksAndSpecialFiles(t *testing.T) {
	cases := map[string]func(t *testing.T, mods string){
		"symlinked zip": func(t *testing.T, mods string) {
			if err := os.Symlink("/etc/hostname", filepath.Join(mods, "evil_1.0.0.zip")); err != nil {
				t.Fatal(err)
			}
		},
		"symlinked mod-list": func(t *testing.T, mods string) {
			if err := os.Symlink("/etc/hostname", filepath.Join(mods, ModListFile)); err != nil {
				t.Fatal(err)
			}
		},
		"symlinked folder": func(t *testing.T, mods string) {
			if err := os.Symlink("/etc", filepath.Join(mods, "evil_1.0.0")); err != nil {
				t.Fatal(err)
			}
		},
		"symlink inside a folder mod": func(t *testing.T, mods string) {
			d := filepath.Join(mods, "dev_1.0.0")
			if err := os.MkdirAll(d, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc/hostname", filepath.Join(d, "info.json")); err != nil {
				t.Fatal(err)
			}
		},
		"named pipe": func(t *testing.T, mods string) {
			if err := syscall.Mkfifo(filepath.Join(mods, "pipe_1.0.0.zip"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m, id, dir := checkWorld(t, "2.0.77", false)
			if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
				t.Fatal(err)
			}
			setup(t, filepath.Join(dir, ModsDir))
			if err := m.LinkModsInto(id, linkDst(t)); err == nil {
				t.Fatal("want a refusal")
			}
		})
	}
}

func TestLinkModsIntoRefusesAModsFolderThatIsALink(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	elsewhere := t.TempDir()
	writeFile(t, filepath.Join(elsewhere, "x_1.0.0.zip"), "zip")
	if err := os.RemoveAll(filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	dst := linkDst(t)
	if err := m.LinkModsInto(id, dst); err == nil && exists(filepath.Join(dst, ModsDir, "x_1.0.0.zip")) {
		t.Fatal("followed a link in place of the mods folder")
	}
}

func TestLinkModsIntoDoesNotWriteThroughAnExistingModsFolder(t *testing.T) {
	m, id, _ := checkWorld(t, "2.0.77", false)
	dst := linkDst(t)
	if err := os.Mkdir(filepath.Join(dst, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := m.LinkModsInto(id, dst); err == nil {
		t.Fatal("want a refusal when the work folder already has a mods folder")
	}
}

func TestLinkModsIntoUnknownWorld(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.LinkModsInto("nope", linkDst(t)); err == nil {
		t.Fatal("want an error")
	}
}

// cfgZip builds a mod zip whose files sit under folder.
func cfgZip(t *testing.T, folder string, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(folder + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func putZip(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ModsDir, file), content)
}

func localeFiles(t *testing.T, m *Manager, id string) map[string]map[string][][]byte {
	t.Helper()
	got, err := m.ModLocaleFiles(id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestModLocaleFilesReadsZipsWithEitherTopFolder(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "alpha_1.0.0.zip", cfgZip(t, "alpha_1.0.0", map[string]string{
		"info.json":           `{"name":"alpha"}`,
		"locale/en/alpha.cfg": "en alpha",
		"locale/de/alpha.cfg": "de alpha",
		"locale/ja/alpha.cfg": "ja alpha",
		"locale/en/notes.txt": "not a cfg",
		"locale/en/sub/b.cfg": "too deep",
	}))
	putZip(t, dir, "beta_2.0.0.zip", cfgZip(t, "beta", map[string]string{
		"locale/en/a.cfg": "en beta 1",
		"locale/en/b.cfg": "en beta 2",
	}))
	putModList(t, dir, "base", "alpha", "beta")

	got := localeFiles(t, m, id)
	if len(got) != 2 {
		t.Fatalf("got mods %v", got)
	}
	if len(got["alpha"]["en"]) != 1 || string(got["alpha"]["en"][0]) != "en alpha" ||
		len(got["alpha"]["de"]) != 1 || string(got["alpha"]["de"][0]) != "de alpha" {
		t.Fatalf("alpha: %v", got["alpha"])
	}
	if _, ok := got["alpha"]["ja"]; ok {
		t.Fatal("a language Foreman does not have was returned")
	}
	if len(got["beta"]["en"]) != 2 || string(got["beta"]["en"][0]) != "en beta 1" || string(got["beta"]["en"][1]) != "en beta 2" {
		t.Fatalf("beta: %q", got["beta"]["en"])
	}
}

func TestModLocaleFilesSkipsBuiltinAndDisabled(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", true)
	// A zip named like a built-in mod is never read as that mod.
	putZip(t, dir, "base_2.0.77.zip", cfgZip(t, "base", map[string]string{"locale/en/x.cfg": "base"}))
	putZip(t, dir, "off_1.0.0.zip", cfgZip(t, "off", map[string]string{"locale/en/x.cfg": "off"}))
	putZip(t, dir, "on_1.0.0.zip", cfgZip(t, "on", map[string]string{"locale/en/x.cfg": "on", "locale/../x.cfg": "bad"}))
	putModListOff(t, dir, []string{"base", "on"}, "off")

	got := localeFiles(t, m, id)
	if len(got) != 1 || len(got["on"]["en"]) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestModLocaleFilesMissingModListEnablesEveryMod(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "on_1.0.0.zip", cfgZip(t, "on", map[string]string{"locale/en/x.cfg": "on"}))
	if got := localeFiles(t, m, id); len(got["on"]["en"]) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestModLocaleFilesTakesTheNewestZipOfAMod(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "mod_1.0.0.zip", cfgZip(t, "mod_1.0.0", map[string]string{"locale/en/x.cfg": "old"}))
	putZip(t, dir, "mod_1.10.0.zip", cfgZip(t, "mod_1.10.0", map[string]string{"locale/en/x.cfg": "new"}))
	got := localeFiles(t, m, id)
	if len(got["mod"]["en"]) != 1 || string(got["mod"]["en"][0]) != "new" {
		t.Fatalf("got %q", got["mod"]["en"])
	}
}

func TestModLocaleFilesReadsFolderMods(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	d := filepath.Join(dir, ModsDir, "dev_1.0.0", "locale", "en")
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(d, "a.cfg"), "folder en")
	writeFile(t, filepath.Join(d, "skip.txt"), "no")
	if err := os.Symlink("/etc/hostname", filepath.Join(d, "link.cfg")); err != nil {
		t.Fatal(err)
	}
	got := localeFiles(t, m, id)
	if len(got["dev"]["en"]) != 1 || string(got["dev"]["en"][0]) != "folder en" {
		t.Fatalf("got %q", got["dev"]["en"])
	}
}

func TestModLocaleFilesFolderModLocaleLinkIsNotFollowed(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	d := filepath.Join(dir, ModsDir, "dev_1.0.0")
	other := filepath.Join(dir, "elsewhere", "en")
	if err := os.MkdirAll(other, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(other, "a.cfg"), "secret")
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), filepath.Join(d, "locale")); err != nil {
		t.Fatal(err)
	}
	if got := localeFiles(t, m, id); len(got) != 0 {
		t.Fatalf("followed a link: %v", got)
	}
}

func TestModLocaleFilesLimits(t *testing.T) {
	big := func(n int) string { return strings.Repeat("a", n) }
	t.Run("one file over the file limit is skipped", func(t *testing.T) {
		m, id, dir := checkWorld(t, "2.0.77", false)
		putZip(t, dir, "mod_1.0.0.zip", cfgZip(t, "mod_1.0.0", map[string]string{
			"locale/en/a-ok.cfg":   big(modsettings.MaxCfgFileBytes),
			"locale/en/b-over.cfg": big(modsettings.MaxCfgFileBytes + 1),
			"locale/en/c-ok.cfg":   "small",
		}))
		got := localeFiles(t, m, id)["mod"]["en"]
		if len(got) != 2 || len(got[0]) != modsettings.MaxCfgFileBytes || string(got[1]) != "small" {
			t.Fatalf("got %d files", len(got))
		}
	})
	t.Run("a folder mod's big file is skipped too", func(t *testing.T) {
		m, id, dir := checkWorld(t, "2.0.77", false)
		d := filepath.Join(dir, ModsDir, "dev_1.0.0", "locale", "en")
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(d, "big.cfg"), big(modsettings.MaxCfgFileBytes+1))
		writeFile(t, filepath.Join(d, "ok.cfg"), "ok")
		got := localeFiles(t, m, id)["dev"]["en"]
		if len(got) != 1 || string(got[0]) != "ok" {
			t.Fatalf("got %d files", len(got))
		}
	})
	t.Run("a language stops at its limit across mods", func(t *testing.T) {
		m, id, dir := checkWorld(t, "2.0.77", false)
		files := func() map[string]string {
			f := map[string]string{}
			for i := range 5 {
				f["locale/en/f"+string(rune('a'+i))+".cfg"] = big(modsettings.MaxCfgFileBytes)
				f["locale/de/f"+string(rune('a'+i))+".cfg"] = "de"
			}
			return f
		}
		putZip(t, dir, "amod_1.0.0.zip", cfgZip(t, "amod", files()))
		putZip(t, dir, "bmod_1.0.0.zip", cfgZip(t, "bmod", files()))
		got := localeFiles(t, m, id)
		total := 0
		for _, files := range got {
			for _, b := range files["en"] {
				total += len(b)
			}
		}
		if total != modsettings.MaxCfgLanguageBytes {
			t.Fatalf("en total %d, want %d", total, modsettings.MaxCfgLanguageBytes)
		}
		if len(got["amod"]["en"]) != 5 || len(got["bmod"]["en"]) != 3 {
			t.Fatalf("a took %d, b took %d", len(got["amod"]["en"]), len(got["bmod"]["en"]))
		}
		// One language being full does not stop another.
		if len(got["bmod"]["de"]) != 5 {
			t.Fatalf("de files: %d", len(got["bmod"]["de"]))
		}
	})
	t.Run("a full language takes no later small file", func(t *testing.T) {
		m, id, dir := checkWorld(t, "2.0.77", false)
		f := map[string]string{}
		for i := range 8 {
			f["locale/en/a"+string(rune('a'+i))+".cfg"] = big(modsettings.MaxCfgFileBytes)
		}
		f["locale/en/b-over.cfg"] = big(1)
		f["locale/en/c-more.cfg"] = ""
		putZip(t, dir, "mod_1.0.0.zip", cfgZip(t, "mod", f))
		if got := localeFiles(t, m, id)["mod"]["en"]; len(got) != 8 {
			t.Fatalf("got %d files", len(got))
		}
	})
}

func TestModLocaleFilesBrokenZipSkipsOnlyThatMod(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "broken_1.0.0.zip", "this is not a zip")
	putZip(t, dir, "empty_1.0.0.zip", "")
	putZip(t, dir, "good_1.0.0.zip", cfgZip(t, "good", map[string]string{"locale/en/x.cfg": "good"}))
	got := localeFiles(t, m, id)
	if len(got) != 1 || len(got["good"]["en"]) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestModLocaleFilesSkipsAZipWithTooManyEntries(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	files := map[string]string{"locale/en/x.cfg": "x"}
	for i := range maxModZipEntries + 1 {
		files[fmt.Sprintf("pad/%05d", i)] = ""
	}
	putZip(t, dir, "many_1.0.0.zip", cfgZip(t, "many", files))
	if got := localeFiles(t, m, id); len(got) != 0 {
		t.Fatalf("read a zip over the entry limit: %v", got)
	}
}

func TestModLocaleFilesUnknownWorld(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.ModLocaleFiles("nope"); err == nil {
		t.Fatal("want an error")
	}
}

func seenOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestApplyModsSettingsSeen(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putSettings(t, dir, "drawn from this")
	settingsPath := filepath.Join(dir, ModsDir, ModSettingsFile)
	read := func() string {
		b, _ := os.ReadFile(settingsPath) //nolint:gosec // a test temp folder
		return string(b)
	}

	t.Run("a match writes", func(t *testing.T) {
		change := ModChange{Settings: []byte("edited"), SettingsSeen: seenOf("drawn from this")}
		if err := m.CheckModChange(id, change); err != nil {
			t.Fatal(err)
		}
		if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
			t.Fatal(err)
		}
		if read() != "edited" {
			t.Fatal("settings were not written")
		}
	})
	t.Run("a file changed since is refused and left alone", func(t *testing.T) {
		change := ModChange{Settings: []byte("second edit"), SettingsSeen: seenOf("drawn from this")}
		if err := m.CheckModChange(id, change); !errors.Is(err, ErrModsChanged) {
			t.Fatalf("check: %v", err)
		}
		if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); !errors.Is(err, ErrModsChanged) {
			t.Fatalf("apply: %v", err)
		}
		if read() != "edited" {
			t.Fatal("a refused change wrote the file")
		}
	})
	t.Run("an empty SettingsSeen skips the check", func(t *testing.T) {
		change := ModChange{Settings: []byte("uploaded")}
		if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
			t.Fatal(err)
		}
		if read() != "uploaded" {
			t.Fatal("upload was not written")
		}
	})
	t.Run("no Settings skips the check", func(t *testing.T) {
		change := ModChange{SettingsSeen: seenOf("something else")}
		if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
			t.Fatal(err)
		}
		if read() != "uploaded" {
			t.Fatal("the file changed")
		}
	})
}

func TestApplyModsSettingsSeenOfAMissingFileIsTheEmptyHash(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if exists(filepath.Join(dir, ModsDir, ModSettingsFile)) {
		t.Fatal("setup: the world should have no settings file")
	}
	stale := ModChange{Settings: []byte("x"), SettingsSeen: seenOf("was there once")}
	if err := m.ApplyModsHeld(id, stale, fingerprint(t, m, id)); !errors.Is(err, ErrModsChanged) {
		t.Fatalf("got %v", err)
	}
	change := ModChange{Settings: []byte("fresh"), SettingsSeen: seenOf("")}
	if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ModsDir, ModSettingsFile)) //nolint:gosec // a test temp folder
	if string(b) != "fresh" {
		t.Fatalf("got %q", b)
	}
}

func TestApplyModsSettingsSeenRefusalChangesNothing(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putSettings(t, dir, "now")
	staged := stage(t, m, "flib", "0.16.2")
	change := ModChange{Add: []string{staged}, Settings: []byte("x"), SettingsSeen: seenOf("before")}
	if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); !errors.Is(err, ErrModsChanged) {
		t.Fatalf("got %v", err)
	}
	if exists(filepath.Join(dir, ModsDir, "flib_0.16.2.zip")) || exists(filepath.Join(dir, ModsDir, ModListFile)) {
		t.Fatal("a refused change placed files")
	}
}

func TestModSettingsKeyChangesWithSpaceAge(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	before := settingsKey(t, m, id)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	meta.SpaceAge = true
	if err := WriteMeta(root, meta); err != nil {
		t.Fatal(err)
	}
	on := settingsKey(t, m, id)
	if on == before {
		t.Fatal("turning Space Age on did not change the key")
	}
	meta.SpaceAge = false
	if err := WriteMeta(root, meta); err != nil {
		t.Fatal(err)
	}
	if settingsKey(t, m, id) != before {
		t.Fatal("turning Space Age off again should give the first key back")
	}
}

func TestLinkModsIntoRefusesAModsFolderLinkedToTheWorld(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	writeFile(t, filepath.Join(dir, "world-secret.txt"), "secret")
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "config", "rconpw"), "hunter2")
	if err := os.RemoveAll(filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(dir, ModsDir)); err != nil {
		t.Fatal(err)
	}
	dst := linkDst(t)
	if err := m.LinkModsInto(id, dst); err == nil {
		t.Fatal("want a refusal for a mods folder that is a link")
	}
	if exists(filepath.Join(dst, ModsDir, "config")) || exists(filepath.Join(dst, ModsDir, "world-secret.txt")) {
		t.Fatal("the world's own files reached the work folder")
	}
	if _, err := m.ModLocaleFiles(id); err == nil {
		t.Fatal("want an error from ModLocaleFiles too")
	}
}

func lowerLinkLimits(t *testing.T, files int, bytes int64) {
	t.Helper()
	oldF, oldB := linkLimitFiles, linkLimitBytes
	linkLimitFiles, linkLimitBytes = files, bytes
	t.Cleanup(func() { linkLimitFiles, linkLimitBytes = oldF, oldB })
}

func TestLinkModsIntoRefusesTooManyFiles(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	d := filepath.Join(dir, ModsDir, "dev_1.0.0")
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a", "b", "c", "d"} {
		writeFile(t, filepath.Join(d, n), "x")
	}
	lowerLinkLimits(t, 3, 1<<20)
	if err := m.LinkModsInto(id, linkDst(t)); !errors.Is(err, ErrModsTooBig) {
		t.Fatalf("got %v", err)
	}
	lowerLinkLimits(t, 100, 1<<20)
	if err := m.LinkModsInto(id, linkDst(t)); err != nil {
		t.Fatal(err)
	}
}

func TestLinkModsIntoRefusesTooManyBytes(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	d := filepath.Join(dir, ModsDir, "dev_1.0.0")
	if err := os.MkdirAll(d, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(d, "big"), strings.Repeat("x", 1000))
	lowerLinkLimits(t, 100, 999)
	if err := m.LinkModsInto(id, linkDst(t)); !errors.Is(err, ErrModsTooBig) {
		t.Fatalf("got %v", err)
	}
}

func TestLinkModsIntoCountsCopiedZipBytesButNotLinkedOnes(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "flib_1.0.0.zip", strings.Repeat("z", 1000))
	lowerLinkLimits(t, 100, 999)
	// A link costs no bytes, so the zip fits.
	if err := m.LinkModsInto(id, linkDst(t)); err != nil {
		t.Fatal(err)
	}
	// When linking fails the copy counts.
	old := linkInto
	linkInto = func(_, _ *os.Root, _ string) error { return syscall.EXDEV }
	defer func() { linkInto = old }()
	if err := m.LinkModsInto(id, linkDst(t)); !errors.Is(err, ErrModsTooBig) {
		t.Fatalf("got %v", err)
	}
}

func TestModLocaleFilesReadsOnlyFactorioLocales(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "mod_1.0.0.zip", cfgZip(t, "mod", map[string]string{
		"locale/zh-CN/x.cfg":   "chinese",
		"locale/es-ES/x.cfg":   "spanish",
		"locale/ja/x.cfg":      "japanese",
		"locale/zh-Hans/x.cfg": "foreman code, not factorio's",
	}))
	got := localeFiles(t, m, id)["mod"]
	if len(got) != 2 || string(got["zh-CN"][0]) != "chinese" || string(got["es-ES"][0]) != "spanish" {
		t.Fatalf("got %v", got)
	}
}
