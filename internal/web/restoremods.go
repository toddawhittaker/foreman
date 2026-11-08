package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Undoing a mod change restores its "Before mod change" snapshot; the
// confirmation says what that does to the mods, in the admin's words
// (SPEC 9.4, 17.5), from the mods folders of the snapshot and the world.

// modFileLimit caps what is read of mod-list.json and mod-settings.dat.
const modFileLimit = 8 << 20

// modState is a mods folder as the undo compares it: the newest zip
// version of each mod, which mods mod-list.json turns off, and the
// mod-settings.dat bytes.
type modState struct {
	zips     map[string]string
	listed   map[string]bool // name to enabled; nil when there is no mod-list.json
	settings []byte
	hasSet   bool
}

// readModState reads the mods folder under root without changing it.
func readModState(root *os.Root) (modState, error) {
	st := modState{zips: map[string]string{}}
	entries, err := fs.ReadDir(root.FS(), world.ModsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return st, err
	}
	for _, e := range entries {
		file := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(file, ".") || !strings.HasSuffix(file, ".zip") {
			continue
		}
		name, version := zipModName(strings.TrimSuffix(file, ".zip"))
		if old, ok := st.zips[name]; !ok || world.CompareVersions(version, old) > 0 {
			st.zips[name] = version
		}
	}
	b, err := world.ReadSmallFile(root, path.Join(world.ModsDir, world.ModListFile), modFileLimit)
	switch {
	case err == nil:
		var doc struct {
			Mods []struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			} `json:"mods"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return st, errors.New("mod-list.json is not valid")
		}
		st.listed = map[string]bool{}
		for _, m := range doc.Mods {
			st.listed[m.Name] = m.Enabled
		}
	case !errors.Is(err, fs.ErrNotExist):
		return st, err
	}
	st.settings, err = world.ReadSmallFile(root, path.Join(world.ModsDir, world.ModSettingsFile), modFileLimit)
	switch {
	case err == nil:
		st.hasSet = true
	case !errors.Is(err, fs.ErrNotExist):
		return st, err
	}
	return st, nil
}

// zipModName splits "name_1.2.3" at its last underscore, as the game names
// mod zips; mod names may hold underscores, versions never do.
func zipModName(base string) (name, version string) {
	if i := strings.LastIndexByte(base, '_'); i > 0 {
		return base[:i], base[i+1:]
	}
	return base, ""
}

// enabled says whether the game runs the mod: one mod-list.json does not
// turn off is on.
func (st modState) enabled(name string) bool {
	on, ok := st.listed[name]
	return !ok || on
}

// modUndoSteps says what restoring the snapshot from was does to the mods
// of now: the mods it removes, puts back, returns to their old version or
// turns back on or off, and the mod settings it puts back.
func modUndoSteps(was, now modState) []i18n.Msg {
	var remove, putBack, on, off []i18n.Msg
	var versions []i18n.Msg
	for _, name := range slices.Sorted(maps.Keys(now.zips)) {
		if _, ok := was.zips[name]; !ok {
			remove = append(remove, modItem(name, now.zips[name]))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(was.zips)) {
		v, ok := now.zips[name]
		switch {
		case !ok:
			putBack = append(putBack, modItem(name, was.zips[name]))
		case v != was.zips[name]:
			versions = append(versions, i18n.M("server.restore.mods.version", "mod", i18n.Ident(name), "version", was.zips[name]))
		}
	}
	// A mod's on or off state is compared where both folders say it: a zip
	// in both, or a built-in mod both mod lists name. Without a list the
	// built-in mods follow the world's settings, not the list.
	var toggled []string
	for name := range was.zips {
		if _, ok := now.zips[name]; ok {
			toggled = append(toggled, name)
		}
	}
	if was.listed != nil && now.listed != nil {
		for name := range was.listed {
			_, inNow := now.listed[name]
			_, zip := was.zips[name]
			if inNow && !zip {
				toggled = append(toggled, name)
			}
		}
	}
	slices.Sort(toggled)
	for _, name := range toggled {
		switch a, b := was.enabled(name), now.enabled(name); {
		case a && !b:
			on = append(on, i18n.M("server.list.names", "names", i18n.Ident(name)))
		case !a && b:
			off = append(off, i18n.M("server.list.names", "names", i18n.Ident(name)))
		}
	}
	var out []i18n.Msg
	if len(remove) > 0 {
		out = append(out, i18n.M("server.restore.mods.remove", "mods", andNames(remove)))
	}
	if len(putBack) > 0 {
		out = append(out, i18n.M("server.restore.mods.put_back", "mods", andNames(putBack)))
	}
	out = append(out, versions...)
	if len(on) > 0 {
		out = append(out, i18n.M("server.restore.mods.on", "mods", andNames(on)))
	}
	if len(off) > 0 {
		out = append(out, i18n.M("server.restore.mods.off", "mods", andNames(off)))
	}
	if was.hasSet != now.hasSet || !bytes.Equal(was.settings, now.settings) {
		out = append(out, i18n.M("server.restore.mods.settings"))
	}
	return out
}

func modItem(name, version string) i18n.Msg {
	return i18n.M("server.restore.mods.item", "mod", i18n.Ident(name), "version", version)
}

// andNames lists items as "a, b and c".
func andNames(items []i18n.Msg) i18n.Msg {
	if len(items) == 1 {
		return items[0]
	}
	return andList([]i18n.Msg{listMsg(items[:len(items)-1]), items[len(items)-1]})
}

// modUndo is what restoring the world's "Before mod change" snapshot does
// to its mods, or nil when the snapshot is another kind, a folder cannot be
// read, or the mods are the same; the page then reads as any restore.
func (s *server) modUndo(id string, snap world.Snapshot) []i18n.Msg {
	if snap.Reason != world.ReasonMods {
		return nil
	}
	snaps, err := os.OpenRoot(filepath.Join(s.worlds.Dirs().Snapshots, id))
	if err != nil {
		s.log.Warn("read the snapshot's mods", "world", id, "error", err)
		return nil
	}
	defer func() { _ = snaps.Close() }()
	src, err := snaps.OpenRoot(snap.Name)
	if err != nil {
		s.log.Warn("read the snapshot's mods", "world", id, "error", err)
		return nil
	}
	defer func() { _ = src.Close() }()
	was, err := readModState(src)
	if err != nil {
		s.log.Warn("read the snapshot's mods", "world", id, "error", err)
		return nil
	}
	dst, err := s.worlds.OpenWorld(id)
	if err != nil {
		s.log.Warn("read the world's mods", "world", id, "error", err)
		return nil
	}
	defer func() { _ = dst.Close() }()
	now, err := readModState(dst)
	if err != nil {
		s.log.Warn("read the world's mods", "world", id, "error", err)
		return nil
	}
	return modUndoSteps(was, now)
}
