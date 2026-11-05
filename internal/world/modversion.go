package world

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

const (
	// maxInfoJSON caps what is read from a mod's info.json.
	maxInfoJSON = 64 << 10
	// maxModZip is the largest mod zip opened (SPEC 14.6).
	maxModZip = 200 << 20
	// maxModZipEntries is the most files a mod zip may list before it is
	// left unread.
	maxModZipEntries = 10000
	// listedMods is how many mod names the mod version problem names.
	listedMods = 5
	// maxShownModName is the longest mod name shown, in characters.
	maxShownModName = 100
)

// modGameVersions returns, for each mod the game would load that has a zip
// in the world's mods folder, the Factorio version its info.json declares,
// as major.minor. The game loads a zip that mod-list.json does not mention
// and turns it on, so only a mod listed as off is left out. A mod whose zip
// or info.json cannot be read, or that names no version, is left out too:
// no information is not a problem.
func modGameVersions(root *os.Root, mods modList, running string) map[string]string {
	builtin := BuiltinMods(running)
	out := map[string]string{}
	for name, file := range newestZips(root) {
		if mods.disabled[name] || slices.Contains(builtin, name) {
			continue
		}
		if v := gameMinor(zipFactorioVersion(root, path.Join(ModsDir, file))); v != "" {
			out[name] = v
		}
	}
	return out
}

// newestZips maps each mod name to the file name of its highest-version
// zip in the mods folder.
func newestZips(root *os.Root) map[string]string {
	entries, err := fs.ReadDir(root.FS(), ModsDir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	versions := map[string]string{}
	for _, e := range entries {
		file := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(file, ".zip") {
			continue
		}
		base := strings.TrimSuffix(file, ".zip")
		i := strings.LastIndexByte(base, '_')
		if i <= 0 {
			continue
		}
		name, version := base[:i], base[i+1:]
		if _, seen := out[name]; !seen || compareDotted(version, versions[name]) > 0 {
			out[name], versions[name] = file, version
		}
	}
	return out
}

// compareDotted compares two dotted numbers such as "1.10.2"; a part that
// is not a number counts as 0.
func compareDotted(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y uint64
		if i < len(pa) {
			x, _ = strconv.ParseUint(pa[i], 10, 32)
		}
		if i < len(pb) {
			y, _ = strconv.ParseUint(pb[i], 10, 32)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// zipFactorioVersion reads "factorio_version" from the info.json inside a
// mod's zip: the zip holds one top-level folder, and the file is in it.
// It returns "" when anything is missing or unreadable, and for a zip over
// the size or entry limit.
func zipFactorioVersion(root *os.Root, name string) string {
	f, info, err := openRegular(root, name)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if info.Size() > maxModZip {
		return ""
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil || len(zr.File) > maxModZipEntries {
		return ""
	}
	for _, zf := range zr.File {
		dir, file, ok := strings.Cut(zf.Name, "/")
		if !ok || dir == "" || file != "info.json" {
			continue
		}
		return infoFactorioVersion(zf)
	}
	return ""
}

// infoFactorioVersion reads an info.json inside a zip, up to maxInfoJSON bytes.
func infoFactorioVersion(zf *zip.File) string {
	rc, err := zf.Open()
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, maxInfoJSON))
	if err != nil {
		return ""
	}
	var doc struct {
		FactorioVersion string `json:"factorio_version"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return ""
	}
	return doc.FactorioVersion
}

// modVersionMismatch finds mods made for another Factorio than the world's
// pinned game version, which the game runs. It returns that version and
// each mismatched mod with the major.minor it was made for.
func modVersionMismatch(root *os.Root, meta Meta, mods modList) (running string, wrong map[string]string) {
	running = meta.GameVersion
	want := gameMinor(running)
	if want == "" {
		return running, nil
	}
	for name, v := range modGameVersions(root, mods, running) {
		if v != want {
			if wrong == nil {
				wrong = map[string]string{}
			}
			wrong[name] = v
		}
	}
	return running, wrong
}

// modVersionProblem words the refusal for mods made for another Factorio
// than the one the game runs. mods maps each mismatched mod to the
// major.minor it was made for.
func modVersionProblem(worldName string, running string, mods map[string]string, _ savefile.Version) Problem {
	byVersion := map[string][]string{}
	for name, v := range mods {
		byVersion[v] = append(byVersion[v], name)
	}
	versions := make([]string, 0, len(byVersion))
	for v, names := range byVersion {
		slices.Sort(names)
		versions = append(versions, v)
	}
	// Largest group first.
	slices.SortFunc(versions, func(a, b string) int {
		if d := len(byVersion[b]) - len(byVersion[a]); d != 0 {
			return d
		}
		return compareDotted(a, b)
	})

	var text strings.Builder
	for i, v := range versions {
		names := byVersion[v]
		count, object := fmt.Sprintf("%d of its mods were", len(names)), "them"
		if len(names) == 1 {
			count, object = "1 of its mods was", "it"
		}
		if i == 0 {
			fmt.Fprintf(&text, "%s cannot run on Factorio %s: ", worldName, running)
		} else {
			text.WriteString(" ")
		}
		fmt.Fprintf(&text, "%s made for Factorio %s, and the game refuses to load %s: %s.", count, v, object, listNames(names))
	}
	// A world's version only moves up (SPEC 10.3), so advise a newer
	// version only, never an older one.
	if newerSupportedLine(versions[0], running) {
		fmt.Fprintf(&text, " Update the world's game version to a %s release, or update its mods.", versions[0])
	} else {
		fmt.Fprintf(&text, " Update its mods to versions made for Factorio %s.", gameMinor(running))
	}
	return Problem{Kind: ProblemModGameVersion, Text: text.String()}
}

// newerSupportedLine reports whether minor (such as "2.1") is a line
// Foreman runs and is newer than the running game version.
func newerSupportedLine(minor, running string) bool {
	return SupportedGameVersion(minor+".0") && compareDotted(minor, gameMinor(running)) > 0
}

// listNames joins the first listedMods names, each cut to a sensible length,
// and counts the rest.
func listNames(names []string) string {
	shown := names[:min(len(names), listedMods)]
	cut := make([]string, len(shown))
	for i, n := range shown {
		cut[i] = n
		if r := []rune(n); len(r) > maxShownModName {
			cut[i] = string(r[:maxShownModName]) + "…"
		}
	}
	out := strings.Join(cut, ", ")
	if rest := len(names) - len(shown); rest > 0 {
		out += fmt.Sprintf(" and %d more", rest)
	}
	return out
}
