package game

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

const stoneCatalogue = `{"planets":[{"name":"moon","localised_name":["space-location-name.moon"],"autoplace_controls":{"arig_stone":{}}}],
  "autoplace_controls":[{"name":"arig_stone","category":"resource","localised_name":["entity-name.stone"]}],
  "resources":[{"name":"arig_stone","control":"arig_stone","localised_name":["entity-name.stone"]}]}`

func cfgSections(t *testing.T, cfg string) modsettings.LocaleSections {
	t.Helper()
	s, err := modsettings.ParseLocaleCfg([]byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// SPEC 9.6: a control whose localised name points at a base-game key
// resolves from the game's own locale, after the mods' text.
func TestWorldCataloguesResolveFromTheGamesOwnLocale(t *testing.T) {
	gameLocale := map[string]modsettings.LocaleSections{
		"en": cfgSections(t, "[entity-name]\nstone=Stone\n[space-location-name]\nmoon=Moon\n"),
		"de": cfgSections(t, "[entity-name]\nstone=Stein\n"),
	}
	cats, err := worldCatalogues([]byte(stoneCatalogue), nil, nil, gameLocale, "2.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := cats["en"].Controls["arig_stone"].Label; got != "Stone" {
		t.Errorf("en control label %q, want Stone", got)
	}
	if got := cats["en"].Resources[0].Label; got != "Stone" {
		t.Errorf("en resource label %q, want Stone", got)
	}
	if got := cats["de"].Controls["arig_stone"].Label; got != "Stein" {
		t.Errorf("de control label %q, want Stein", got)
	}
	// French has none of its own: English behind it, key by key.
	if got := cats["fr"].Controls["arig_stone"].Label; got != "Stone" {
		t.Errorf("fr control label %q, want the English Stone", got)
	}
	if got := cats["de"].Planets[0].Label; got != "Moon" {
		t.Errorf("de planet label %q, want the English Moon", got)
	}
	// With no game text the raw name stays.
	cats, _ = worldCatalogues([]byte(stoneCatalogue), nil, nil, nil, "2.1", false)
	if got := cats["en"].Controls["arig_stone"].Label; got != "arig_stone" {
		t.Errorf("no game locale: label %q, want the raw name", got)
	}
}

// SPEC 9.6: the mods' text, in any language, is looked up before the game's.
func TestLabelLayersPutTheGamesTextLast(t *testing.T) {
	mods := map[string]modsettings.LocaleSections{"en": cfgSections(t, "[entity-name]\nstone=Mod stone\n")}
	game := map[string]modsettings.LocaleSections{
		"en": cfgSections(t, "[entity-name]\nstone=Stone\n"),
		"de": cfgSections(t, "[entity-name]\nstone=Stein\n"),
	}
	cats, err := worldCatalogues([]byte(stoneCatalogue), nil, mods, game, "2.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := cats["en"].Controls["arig_stone"].Label; got != "Mod stone" {
		t.Errorf("en label %q, want the mod's", got)
	}
	if got := cats["de"].Controls["arig_stone"].Label; got != "Mod stone" {
		t.Errorf("de label %q, want the mod's English before the game's German", got)
	}
}

func TestReadBaseLocale(t *testing.T) {
	newDir := func(t *testing.T) (*os.Root, string, string) {
		t.Helper()
		dir := t.TempDir()
		en := filepath.Join(dir, "p", baseLocaleDir, "en")
		if err := os.MkdirAll(en, 0o750); err != nil {
			t.Fatal(err)
		}
		base, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = base.Close() })
		return base, dir, en
	}
	put := func(t *testing.T, p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("reads the packages, keeping the naming sections", func(t *testing.T) {
		base, _, en := newDir(t)
		put(t, filepath.Join(en, "base-base.cfg"), "[entity-name]\nstone=Stone\n[gui]\nok=OK\n")
		put(t, filepath.Join(en, "space-age-space-age.cfg"), "[space-location-name]\nvulcanus=Vulcanus\n")
		put(t, filepath.Join(en, "readme.txt"), "[entity-name]\nx=y\n")
		got := readBaseLocale(base, "p")["en"]
		if got["entity-name"]["stone"] != "Stone" || got["space-location-name"]["vulcanus"] != "Vulcanus" {
			t.Errorf("sections %v", got)
		}
		if _, ok := got["gui"]; ok || got["entity-name"]["x"] != "" {
			t.Errorf("kept more than the naming sections of .cfg files: %v", got)
		}
		if _, ok := readBaseLocale(base, "p")["de"]; ok {
			t.Error("a language with no folder is present")
		}
	})
	t.Run("a run that left no copy gives none", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "p"), 0o750); err != nil {
			t.Fatal(err)
		}
		base, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = base.Close() }()
		if got := readBaseLocale(base, "p"); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("refuses a link, a named pipe, and a file over its limit", func(t *testing.T) {
		base, dir, en := newDir(t)
		put(t, filepath.Join(dir, "secret.cfg"), "[entity-name]\nsecret=leak\n")
		if err := os.Symlink("../../../secret.cfg", filepath.Join(en, "a-link.cfg")); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(filepath.Join(en, "b-pipe.cfg"), 0o600); err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(en, "c-big.cfg"), "[entity-name]\nbig=x\n"+strings.Repeat("#", modsettings.MaxCfgFileBytes))
		put(t, filepath.Join(en, "d-ok.cfg"), "[entity-name]\nstone=Stone\n")
		got := failsPromptlyLocale(t, base)
		if got["entity-name"]["stone"] != "Stone" || got["entity-name"]["secret"] != "" || got["entity-name"]["big"] != "" {
			t.Errorf("sections %v", got)
		}
	})
	t.Run("a language folder that is a link is skipped", func(t *testing.T) {
		base, dir, _ := newDir(t)
		if err := os.MkdirAll(filepath.Join(dir, "elsewhere"), 0o750); err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(dir, "elsewhere", "x.cfg"), "[entity-name]\nstone=Stone\n")
		if err := os.Symlink("../../elsewhere", filepath.Join(dir, "p", baseLocaleDir, "de")); err != nil {
			t.Fatal(err)
		}
		if _, ok := readBaseLocale(base, "p")["de"]; ok {
			t.Error("read through a linked folder")
		}
	})
}

// failsPromptlyLocale reads the English copy, failing the test when a named
// pipe holds the read.
func failsPromptlyLocale(t *testing.T, base *os.Root) modsettings.LocaleSections {
	t.Helper()
	var got modsettings.LocaleSections
	err := failsPromptly(t, func() error {
		got = readBaseLocale(base, "p")["en"]
		return errors.New("done")
	})
	_ = err
	return got
}

func TestBaseLocaleCopyScript(t *testing.T) {
	s := baseLocaleCopy()
	for _, want := range []string{"zh-CN", "es-ES", "/opt/factorio/data/$p/locale/$l/*.cfg", "space-age", "elevated-rails", "|| :"} {
		if !strings.Contains(s, want) {
			t.Errorf("the copy does not mention %q: %s", want, s)
		}
	}
	if strings.Contains(s, "\n") {
		t.Error("the copy is more than one line")
	}
}
