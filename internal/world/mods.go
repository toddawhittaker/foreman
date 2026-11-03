package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// ModListFile is the game's list of enabled mods, inside the mods folder.
const ModListFile = "mod-list.json"

// maxModList caps how much of mod-list.json is read: a real one is about
// 50 bytes a mod, so this is far above any real list.
const maxModList = 4 << 20

// errModListTooBig is returned for a mod-list.json over maxModList.
var errModListTooBig = errors.New("mod-list.json is too big to be a real mod list")

// modList is what the world's mods folder will load.
type modList struct {
	// found is false when there is no mod-list.json; the game then writes
	// one with every built-in mod enabled.
	found bool
	// enabled holds the names of enabled mods.
	enabled map[string]bool
}

// readModList reads mods/mod-list.json from a world's root.
func readModList(root *os.Root) (modList, error) {
	p := path.Join(ModsDir, ModListFile)
	info, err := root.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return modList{enabled: map[string]bool{}}, nil
	}
	if err != nil {
		return modList{}, err
	}
	if !info.Mode().IsRegular() {
		return modList{}, fmt.Errorf("%s is not a regular file", p)
	}
	f, err := root.Open(p)
	if err != nil {
		return modList{}, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxModList+1))
	if err != nil {
		return modList{}, err
	}
	if len(b) > maxModList {
		return modList{}, errModListTooBig
	}
	var doc struct {
		Mods []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"mods"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return modList{}, fmt.Errorf("read %s: %w", ModListFile, err)
	}
	out := modList{found: true, enabled: map[string]bool{}}
	for _, m := range doc.Mods {
		if m.Enabled {
			out.enabled[m.Name] = true
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
