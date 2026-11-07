package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Refusals of a mod change. Nothing was changed.
var (
	ErrModsChanged = errors.New("the world's mods changed after the plan was made, so nothing was changed. Check the new plan, then apply again")
	ErrBuiltinMod  = errors.New("built-in mods are not installed, removed or turned on and off by hand; they follow the world's Space Age setting")
	ErrLocalMod    = errors.New("this mod is installed as a folder, and Foreman never replaces a folder mod")
	ErrModFile     = errors.New("this is not a mod zip in the world's mods folder")
	ErrNotStaged   = errors.New("this is not a staged mod file")
)

// StageMod creates an empty temp file at the top of the worlds folder for
// a mod being downloaded or uploaded. It sits outside every world, so no
// snapshot holds it, and startup recovery removes it after a crash. The
// caller writes, syncs and closes the file, then passes name in
// ModChange.Add, or removes it with RemoveStaged.
func (m *Manager) StageMod() (f *os.File, name string, err error) {
	worlds, err := m.OpenWorlds()
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = worlds.Close() }()
	return CreateTemp(worlds, ".")
}

// StagedPath is where a staged file is, for ReadModZip.
func (m *Manager) StagedPath(name string) string {
	return filepath.Join(m.dirs.Worlds, name)
}

// RemoveStaged removes a staged file; one already gone is fine.
func (m *Manager) RemoveStaged(name string) error {
	if !isStagedName(name) {
		return ErrNotStaged
	}
	worlds, err := m.OpenWorlds()
	if err != nil {
		return err
	}
	defer func() { _ = worlds.Close() }()
	if err := worlds.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func isStagedName(name string) bool {
	return strings.HasPrefix(name, PrefixPartial) && path.Base(name) == name
}

// placed is one staged file and the name it takes in mods/.
type placed struct {
	staged, file string
	info         ModInfo
}

// ApplyModsHeld applies a mod change to the world's mods folder. The caller
// holds Lock(id), has stopped or saved the game and has taken the snapshot
// with ReasonMods (SPEC 6.2). It refuses, changing nothing, when the folder
// no longer matches fingerprint, for a built-in mod, and for a change that
// would replace a folder mod. Then it places the new zips, writes
// mod-list.json keeping what the game wrote that Foreman does not know,
// removes the replaced zips and writes mod-settings.dat. Each step leaves
// a folder the game can load, and an error says which step failed. The
// staged files are removed whatever happens.
func (m *Manager) ApplyModsHeld(id string, change ModChange, fingerprint string) error {
	worlds, err := m.OpenWorlds()
	if err != nil {
		return err
	}
	defer func() { _ = worlds.Close() }()
	defer func() {
		for _, s := range change.Add {
			if isStagedName(s) {
				_ = worlds.Remove(s) // gone already once placed
			}
		}
	}()
	root, err := openDir(worlds, id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	meta, err := readChangeable(root)
	if err != nil {
		return err
	}
	if got, err := modsFingerprint(root); err != nil {
		return err
	} else if got != fingerprint {
		return ErrModsChanged
	}

	builtin := BuiltinMods(meta.GameVersion)
	local, err := localMods(root)
	if err != nil {
		return err
	}
	adds, err := checkAdds(worlds, change.Add, builtin, local)
	if err != nil {
		return err
	}
	if err := checkRemoves(root, change.Remove, adds); err != nil {
		return err
	}
	for name := range change.Enabled {
		if slices.Contains(builtin, name) {
			return ErrBuiltinMod
		}
	}

	// A Generated world has no list yet; without one the game turns on
	// every built-in mod, Space Age included.
	if err := m.EnsureModList(id); err != nil {
		return fmt.Errorf("write the default %s: %w", ModListFile, err)
	}
	list, err := ReadSmallFile(root, path.Join(ModsDir, ModListFile), maxModList)
	if err != nil {
		return err
	}
	newList, err := editModList(list, adds, change.Enabled)
	if err != nil {
		return err
	}
	// The zips move by rename; only the two small files take new space.
	if err := CheckSpace(m.dirs.Worlds, uint64(len(newList)+len(change.Settings))); err != nil {
		return err
	}

	for i, a := range adds {
		if err := worlds.Rename(a.staged, path.Join(id, ModsDir, a.file)); err != nil {
			return fmt.Errorf("place %s (%d of %d new mods placed, nothing else changed): %w", a.file, i, len(adds), err)
		}
	}
	if len(adds) > 0 {
		if err := syncDir(root, ModsDir); err != nil {
			return fmt.Errorf("the new mods were placed, but %s was not updated: %w", ModListFile, err)
		}
	}
	if err := WriteFileAtomic(root, path.Join(ModsDir, ModListFile), newList); err != nil {
		return fmt.Errorf("the new mods were placed, but %s was not updated: %w", ModListFile, err)
	}
	for _, file := range change.Remove {
		if err := root.Remove(path.Join(ModsDir, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("the mods were changed, but the old file %s was not removed: %w", file, err)
		}
	}
	if change.Settings != nil {
		if err := WriteFileAtomic(root, path.Join(ModsDir, ModSettingsFile), change.Settings); err != nil {
			return fmt.Errorf("the mods were changed, but %s was not written: %w", ModSettingsFile, err)
		}
	}
	return nil
}

// localMods names the mods installed as folders in mods/.
func localMods(root *os.Root) (map[string]bool, error) {
	entries, err := fs.ReadDir(root.FS(), ModsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out[modFromFileName(e.Name()).Name] = true
		}
	}
	return out, nil
}

// checkAdds reads each staged zip and names it, refusing a built-in or
// folder mod and two files of the same name.
func checkAdds(worlds *os.Root, staged []string, builtin []string, local map[string]bool) ([]placed, error) {
	var out []placed
	seen := map[string]bool{}
	for _, s := range staged {
		if !isStagedName(s) {
			return nil, ErrNotStaged
		}
		info, err := readModZipIn(worlds, s)
		if err != nil {
			return nil, err
		}
		if slices.Contains(builtin, info.Name) {
			return nil, ErrBuiltinMod
		}
		if local[info.Name] {
			return nil, ErrLocalMod
		}
		file := ModFileName(info)
		if seen[file] {
			return nil, fmt.Errorf("%s is added twice", file)
		}
		seen[file] = true
		out = append(out, placed{staged: s, file: file, info: info})
	}
	return out, nil
}

// checkRemoves refuses a name that is not a zip in mods/, or one the same
// change places.
func checkRemoves(root *os.Root, files []string, adds []placed) error {
	for _, file := range files {
		if !strings.HasSuffix(file, ".zip") || strings.HasPrefix(file, ".") || path.Base(file) != file || strings.Contains(file, `\`) {
			return ErrModFile
		}
		info, err := root.Lstat(path.Join(ModsDir, file))
		if err != nil || !info.Mode().IsRegular() {
			return ErrModFile
		}
		for _, a := range adds {
			if a.file == file {
				return fmt.Errorf("%s is both added and removed", file)
			}
		}
	}
	return nil
}

// editModList sets each mod's enabled state in mod-list.json and lists the
// added mods as on unless enabled says otherwise. Keys Foreman does not
// know, at the top or in an entry, are kept.
func editModList(list []byte, adds []placed, enabled map[string]bool) ([]byte, error) {
	var doc map[string]json.RawMessage
	var entries []map[string]json.RawMessage
	bad := fmt.Errorf("read %s: it is not valid JSON of the expected shape", ModListFile)
	if json.Unmarshal(list, &doc) != nil || doc == nil {
		return nil, bad
	}
	if raw, ok := doc["mods"]; ok && json.Unmarshal(raw, &entries) != nil {
		return nil, bad
	}
	want := map[string]bool{}
	for _, a := range adds {
		want[a.info.Name] = true
	}
	for name, on := range enabled {
		want[name] = on
	}
	listed := map[string]bool{}
	for _, e := range entries {
		var name string
		if json.Unmarshal(e["name"], &name) != nil {
			continue
		}
		listed[name] = true
		if on, ok := want[name]; ok {
			e["enabled"] = jsonBool(on)
		}
	}
	var added []string
	for name := range want {
		if !listed[name] {
			added = append(added, name)
		}
	}
	slices.Sort(added)
	for _, name := range added {
		n, _ := json.Marshal(name)
		entries = append(entries, map[string]json.RawMessage{"name": n, "enabled": jsonBool(want[name])})
	}
	mods, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	doc["mods"] = mods
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func jsonBool(b bool) json.RawMessage {
	if b {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}
