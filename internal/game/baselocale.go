package game

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The game's own locale text is the last place a label is looked up (SPEC
// 9.6): a control whose localised name points at a base-game key, such as
// {"entity-name.stone"}, has no entry in any mod's locale. The definitions
// run copies the .cfg files of the game's packages out of the world's own
// image, so the text is the world's game version's, and nothing is downloaded.
const (
	// baseLocaleDir is the folder in the work folder the copy goes to, one
	// subfolder per Factorio locale code.
	baseLocaleDir = "base-locale"
	// maxBaseLocaleFiles caps the .cfg files read for one language. The game
	// ships a handful per package.
	maxBaseLocaleFiles = 32
)

// gamePackages are the folders under the game's data folder that hold locale
// text, in the order their files merge: a later package wins a shared key.
var gamePackages = []string{"base", "elevated-rails", "quality", "space-age"}

// baseLocaleCopy is the shell the definitions run adds after the game exits.
// Each language's .cfg files go to baseLocaleDir/<code>/<package>-<file>. A
// package the image lacks (the game has no quality on 1.1) or a copy that
// fails only loses text, so nothing here fails the run.
func baseLocaleCopy() string {
	codes := make([]string, 0, len(modsettings.FactorioLocales))
	for _, code := range modsettings.FactorioLocales {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	out := gameenv.PreviewMount + "/" + baseLocaleDir
	return `for l in ` + strings.Join(codes, " ") + `; do for p in ` + strings.Join(gamePackages, " ") + `; do ` +
		`for f in ` + gameDataDir + `/$p/locale/$l/*.cfg; do ` +
		`[ -f "$f" ] || continue; mkdir -p ` + out + `/$l && cp "$f" ` + out + `/$l/$p-"${f##*/}"; ` +
		`done; done; done 2>/dev/null || :`
}

// gameDataDir is the game's read-only data folder in its image, where
// previewConfig points the game's read path.
const gameDataDir = "/opt/factorio/data"

// readBaseLocale reads the copy the run left in the work folder dir, as
// locale sections for each of Foreman's languages. The game's script is
// untrusted (SPEC 14.1, 14.6): a link, a named pipe or a file over its limit
// is skipped, as are more files or bytes than the limits allow. Only the
// sections that name things are kept, so a world's cache holds a small part
// of the game's text. A language with no usable text is absent, and a run
// that left no copy gives none: the text is decoration.
func readBaseLocale(base *os.Root, dir string) map[string]modsettings.LocaleSections {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return nil
	}
	defer func() { _ = root.Close() }()
	all := map[string]modsettings.LocaleSections{}
	for lang, code := range modsettings.FactorioLocales {
		if secs := readBaseLocaleLanguage(root, baseLocaleDir+"/"+code); len(secs) > 0 {
			all[lang] = secs
		}
	}
	return all
}

func readBaseLocaleLanguage(root *os.Root, rel string) modsettings.LocaleSections {
	if info, err := root.Lstat(rel); err != nil || !info.IsDir() {
		return nil
	}
	entries, err := fs.ReadDir(root.FS(), rel)
	if err != nil {
		return nil
	}
	secs := modsettings.LocaleSections{}
	var files, total int
	// ReadDir sorts by name, and the names start with the package.
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".cfg") {
			continue
		}
		if files++; files > maxBaseLocaleFiles {
			break
		}
		data, err := world.ReadSmallFile(root, rel+"/"+e.Name(), modsettings.MaxCfgFileBytes)
		if err != nil {
			continue
		}
		if total += len(data); total > modsettings.MaxCfgLanguageBytes {
			break
		}
		parsed, err := modsettings.ParseLocaleCfg(data)
		if err != nil {
			continue
		}
		secs.Merge(namingSections(parsed))
	}
	return secs
}

// namingSections keeps the sections that name things: entity-name,
// item-name, space-location-name, autoplace-control-names and the like.
func namingSections(s modsettings.LocaleSections) modsettings.LocaleSections {
	out := modsettings.LocaleSections{}
	for name, keys := range s {
		if strings.HasSuffix(name, "-name") || strings.HasSuffix(name, "-names") {
			out[name] = keys
		}
	}
	return out
}

// labelLayers is the order a label is looked up in: the mods' text in the
// language, then in English, then the game's own text in the language, then
// in English. A layer with no text is left out.
func labelLayers(lang string, mods, game map[string]modsettings.LocaleSections) []modsettings.LocaleSections {
	layers := []modsettings.LocaleSections{mods[lang]}
	if lang != "en" {
		layers = append(layers, mods["en"])
	}
	layers = append(layers, game[lang])
	if lang != "en" {
		layers = append(layers, game["en"])
	}
	return slices.DeleteFunc(layers, func(s modsettings.LocaleSections) bool { return len(s) == 0 })
}

// emptyModSettings is the defaults file of a world whose mods define no
// setting, which the game does not write: the game's own empty file, three
// empty groups, headed by the game's version. The page decodes the defaults,
// so it must be a valid file. A version that does not parse gives zeros.
func emptyModSettings(gameVersion string) []byte {
	var v [3]uint16
	for i, part := range strings.SplitN(gameVersion, ".", 3) {
		var n uint16
		if _, err := fmt.Sscanf(part, "%d", &n); err == nil {
			v[i] = n
		}
	}
	var b []byte
	for _, n := range v {
		b = binary.LittleEndian.AppendUint16(b, n)
	}
	// The build number, the flag byte, then the root dictionary of three.
	b = append(b, 0, 0, 0, 5, 0, 3, 0, 0, 0)
	for _, group := range []string{"startup", "runtime-global", "runtime-per-user"} {
		b = append(b, 0, byte(len(group))) //nolint:gosec // a constant of a few bytes
		b = append(b, group...)
		b = append(b, 5, 0, 0, 0, 0, 0)
	}
	return b
}
