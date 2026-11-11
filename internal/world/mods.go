package world

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

// ModListFile is the game's list of enabled mods, inside the mods folder.
const ModListFile = "mod-list.json"

// modList is what the world's mods folder will load.
type modList struct {
	// found is false when there is no mod-list.json; the game then writes
	// one with every built-in mod enabled.
	found bool
	// enabled holds the names of mods listed as on.
	enabled map[string]bool
	// disabled holds the names of mods listed as off. The game loads a mod
	// zip the list does not mention and turns it on, so only these are off.
	disabled map[string]bool
}

// ModListStates reads mods/mod-list.json from a world's or snapshot's root
// and returns each listed mod's name with whether the list enables it. It
// returns nil when there is no mod-list.json.
func ModListStates(root *os.Root) (map[string]bool, error) {
	list, err := readModList(root)
	if err != nil || !list.found {
		return nil, err
	}
	out := make(map[string]bool, len(list.enabled)+len(list.disabled))
	for name := range list.enabled {
		out[name] = true
	}
	for name := range list.disabled {
		out[name] = false
	}
	return out, nil
}

// readModList reads mods/mod-list.json from a world's root.
func readModList(root *os.Root) (modList, error) {
	p := path.Join(ModsDir, ModListFile)
	b, err := ReadSmallFile(root, p, MaxModList)
	if errors.Is(err, fs.ErrNotExist) {
		return modList{enabled: map[string]bool{}, disabled: map[string]bool{}}, nil
	}
	if err != nil {
		return modList{}, err
	}
	var doc struct {
		Mods []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"mods"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		// The decoder's message quotes file contents; say only what is wrong.
		return modList{}, fmt.Errorf("read %s: it is not valid JSON of the expected shape", ModListFile)
	}
	out := modList{found: true, enabled: map[string]bool{}, disabled: map[string]bool{}}
	for _, m := range doc.Mods {
		if m.Enabled {
			out.enabled[m.Name] = true
		} else {
			out.disabled[m.Name] = true
		}
	}
	return out, nil
}

// presentMods returns the names of mods whose files are in the mods
// folder: a name_version.zip, a name_version folder or a name folder.
func presentMods(root *os.Root) (map[string]bool, error) {
	entries, err := fs.ReadDir(root.FS(), ModsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.Type().IsRegular() && strings.HasSuffix(name, ".zip"):
			name = strings.TrimSuffix(name, ".zip")
		case e.IsDir():
		default:
			continue
		}
		out[name] = true
		// Mod names may hold underscores; versions never do.
		if i := strings.LastIndexByte(name, '_'); i > 0 {
			out[name[:i]] = true
		}
	}
	return out, nil
}

// modListEntry is one mod in mod-list.json, in the game's own shape.
type modListEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// DefaultModList builds the list the game would write for a new world of
// this game version, with the Space Age mods on or off by the flag.
// The game lists base first, then the rest by name.
func DefaultModList(gameVersion string, spaceAge bool) []byte {
	entries := []modListEntry{{Name: ModBase, Enabled: true}}
	var rest []modListEntry
	for _, name := range alwaysOnMods(gameVersion) {
		rest = append(rest, modListEntry{Name: name, Enabled: true})
	}
	for _, name := range SpaceAgeMods(gameVersion) {
		rest = append(rest, modListEntry{Name: name, Enabled: spaceAge})
	}
	slices.SortFunc(rest, func(a, b modListEntry) int { return strings.Compare(a.Name, b.Name) })
	entries = append(entries, rest...)
	// Marshalling a slice of plain structs cannot fail.
	b, _ := json.MarshalIndent(struct {
		Mods []modListEntry `json:"mods"`
	}{entries}, "", "  ")
	return append(b, '\n')
}

// EnsureModList writes mods/mod-list.json for a world that has none, so
// the game does not enable every built-in mod, Space Age included.
// An existing file is left as it is. The caller holds Lock(id).
func (m *Manager) EnsureModList(id string) error {
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return err
	}
	if !SupportedGameVersion(meta.GameVersion) {
		return fmt.Errorf("no mod list for game version %q", meta.GameVersion)
	}
	p := path.Join(ModsDir, ModListFile)
	info, err := root.Lstat(p)
	switch {
	case err == nil && info.Mode().IsRegular():
		return nil
	case err == nil:
		return fmt.Errorf("%s is not a regular file", p)
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := Mkdir(root, ModsDir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return WriteFileAtomic(root, p, DefaultModList(meta.GameVersion, meta.SpaceAge))
}

// ModSettingsFile holds the values of the mods' settings, inside the mods
// folder (SPEC 9.6).
const ModSettingsFile = "mod-settings.dat"

// Mods lists the world's mods: the built-in mods of its game version
// first, then each zip and folder in its mods folder by name. A zip is
// named by its info.json, or by its file name when that cannot be read.
// The game turns on any mod mod-list.json does not list as off.
func (m *Manager) Mods(id string) ([]InstalledMod, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return nil, err
	}
	list, err := readModList(root)
	if err != nil {
		return nil, err
	}
	on := func(name string) bool { return !list.disabled[name] }
	var out []InstalledMod
	for _, name := range BuiltinMods(meta.GameVersion) {
		out = append(out, InstalledMod{Name: name, Version: meta.GameVersion, Enabled: on(name), BuiltIn: true})
	}
	entries, err := fs.ReadDir(root.FS(), ModsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var found []InstalledMod
	for _, e := range entries {
		file := e.Name()
		if strings.HasPrefix(file, ".") {
			continue
		}
		switch {
		case e.Type().IsRegular() && strings.HasSuffix(file, ".zip"):
			mod := modFromFileName(strings.TrimSuffix(file, ".zip"))
			if info, ok := zipModInfo(root, path.Join(ModsDir, file)); ok {
				mod.Dependencies = info.Dependencies
				if info.Name != "" {
					mod.Name, mod.Version = info.Name, info.Version
				}
			}
			mod.File = file
			found = append(found, mod)
		case e.IsDir():
			mod := modFromFileName(file)
			b, err := ReadSmallFile(root, path.Join(ModsDir, file, "info.json"), maxInfoJSON)
			if err == nil {
				if info, ok := parseInfoJSON(b); ok {
					mod.Dependencies = info.Dependencies
					if info.Name != "" {
						mod.Name, mod.Version = info.Name, info.Version
					}
				}
			}
			mod.Local = true
			found = append(found, mod)
		}
	}
	for i := range found {
		found[i].Enabled = on(found[i].Name)
	}
	slices.SortFunc(found, func(a, b InstalledMod) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return CompareVersions(a.Version, b.Version)
	})
	return append(out, found...), nil
}

// modFromFileName splits "name_1.2.3" into a name and version; a name with
// no version after its last underscore is all name.
func modFromFileName(base string) InstalledMod {
	if i := strings.LastIndexByte(base, '_'); i > 0 && modVersionRE.MatchString(base[i+1:]) {
		return InstalledMod{Name: base[:i], Version: base[i+1:]}
	}
	return InstalledMod{Name: base}
}

// ModsFingerprint sums the mods folder's file names, sizes and times and
// the content of mod-list.json, so a change applies only to the folder its
// plan was made from (SPEC 17.8).
func (m *Manager) ModsFingerprint(id string) (string, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	return modsFingerprint(root)
}

func modsFingerprint(root *os.Root) (string, error) {
	return fingerprintMods(root, false)
}

// fingerprintMods is modsFingerprint, leaving out mod-settings.dat when
// withoutSettings is set: a setting's value never changes what a mod
// defines, so the definitions cache must not miss on a settings edit.
func fingerprintMods(root *os.Root, withoutSettings bool) (string, error) {
	h := sha256.New()
	entries, err := fs.ReadDir(root.FS(), ModsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	for _, e := range entries {
		if e.Name() == ModListFile || (withoutSettings && e.Name() == ModSettingsFile) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(h, "%q %v %d %d\n", e.Name(), info.Mode().Type(), info.Size(), info.ModTime().UnixNano())
	}
	b, err := ReadSmallFile(root, path.Join(ModsDir, ModListFile), MaxModList)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		_, _ = fmt.Fprintf(h, "no %s\n", ModListFile)
	case err != nil:
		return "", err
	default:
		sum := sha256.Sum256(b)
		_, _ = fmt.Fprintf(h, "%s %x\n", ModListFile, sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
