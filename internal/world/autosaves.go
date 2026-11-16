package world

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The automatic snapshot taken before the autosave tidy (SPEC 6.2, 11.6).
const (
	ReasonAutosaves      = "autosaves"
	LabelBeforeAutosaves = "Before autosave tidy"
)

const (
	autosavePrefix     = "_autosave"
	autosaveSuffix     = ".zip"
	autosaveTidySuffix = ".tidy.zip"
)

// autosave is one of a world's autosaves: `_autosaveN.zip`, or a
// leftover `_autosaveN.tidy.zip` from a tidy cut short, read as N.
type autosave struct {
	name    string
	n       int
	modTime time.Time
}

// autosaveNumber reads N from an autosave's name; ok is false for any
// other save, including the game's partial `.tmp.zip` files.
func autosaveNumber(name string) (n int, ok bool) {
	rest, found := strings.CutPrefix(name, autosavePrefix)
	if !found {
		return 0, false
	}
	digits, found := strings.CutSuffix(rest, autosaveTidySuffix)
	if !found {
		if digits, found = strings.CutSuffix(rest, autosaveSuffix); !found {
			return 0, false
		}
	}
	if digits == "" || digits[0] == '0' || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil
}

func autosaveName(n int) string     { return autosavePrefix + strconv.Itoa(n) + autosaveSuffix }
func autosaveTidyName(n int) string { return autosavePrefix + strconv.Itoa(n) + autosaveTidySuffix }

// AutosaveMove renames one kept autosave.
type AutosaveMove struct {
	From, To string
	n        int
}

// AutosaveTidy is what a tidy does (SPEC 11.6). Kept counts every kept
// file; Moves holds the ones whose name changes. Remove lists the files to
// delete that no kept file is renamed over.
type AutosaveTidy struct {
	Kept, Removed int
	Moves         []AutosaveMove
	Remove        []string
}

// NewName is where a tidy puts the file once called name, or "" when it
// is removed. A name the tidy does not touch stays as it is.
func (t AutosaveTidy) NewName(name string) string {
	for _, mv := range t.Moves {
		if mv.From == name {
			return mv.To
		}
	}
	if slices.Contains(t.Remove, name) {
		return ""
	}
	for _, mv := range t.Moves {
		if mv.To == name {
			return ""
		}
	}
	return name
}

// errAutosaveClash refuses a tidy when two files hold the same number, as
// their `.tidy.zip` names would collide.
var errAutosaveClash = errors.New("two autosaves have the same number")

// planAutosaveTidy works out the tidy for slots S (SPEC 11.6). It returns
// ok false when there is nothing to do: S is 0 or every N is S or less,
// and no leftover from a tidy cut short remains. loaded is the file the
// last run loaded, or "" when Foreman does not know it.
func planAutosaveTidy(saves []autosave, slots int, loaded string) (AutosaveTidy, bool, error) {
	if slots <= 0 {
		return AutosaveTidy{}, false, nil
	}
	need := false
	seen := map[int]bool{}
	for _, s := range saves {
		if s.n > slots || strings.HasSuffix(s.name, autosaveTidySuffix) {
			need = true
		}
		if seen[s.n] {
			return AutosaveTidy{}, false, errAutosaveClash
		}
		seen[s.n] = true
	}
	if !need {
		return AutosaveTidy{}, false, nil
	}
	saves = slices.Clone(saves)
	slices.SortFunc(saves, func(a, b autosave) int {
		return cmp.Or(b.modTime.Compare(a.modTime), cmp.Compare(b.n, a.n))
	})
	// A graceful stop saves over the loaded file, so it is not the last
	// autosave the game wrote.
	k := saves[0].n
	for _, s := range saves {
		if s.name != loaded {
			k = s.n
			break
		}
	}
	keep := min(slots, len(saves))
	t := AutosaveTidy{Kept: keep, Removed: len(saves) - keep}
	targets := map[string]bool{}
	for i, s := range saves[:keep] {
		to := autosaveName(((k-(i+1))%slots+slots)%slots + 1)
		targets[to] = true
		if to != s.name {
			t.Moves = append(t.Moves, AutosaveMove{From: s.name, To: to, n: s.n})
		}
	}
	for _, s := range saves[keep:] {
		// A rename over it removes it.
		if !targets[s.name] {
			t.Remove = append(t.Remove, s.name)
		}
	}
	return t, true, nil
}

// readAutosaves lists a world's autosaves.
func readAutosaves(root *os.Root) ([]autosave, error) {
	entries, err := fs.ReadDir(root.FS(), SavesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []autosave
	for _, e := range entries {
		n, ok := autosaveNumber(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, autosave{name: e.Name(), n: n, modTime: info.ModTime()})
	}
	return out, nil
}

// PlanAutosaveTidyHeld works out the tidy a start makes for slots S
// (SPEC 11.6). ok is false when there is nothing to do, including when an
// admin chose an autosave to load next. The caller holds Lock(id) and
// keeps it until ApplyAutosaveTidyHeld, so the files do not change.
func (m *Manager) PlanAutosaveTidyHeld(id string, slots int, loaded string) (t AutosaveTidy, ok bool, err error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return AutosaveTidy{}, false, err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return AutosaveTidy{}, false, err
	}
	if _, chosen := autosaveNumber(meta.LoadSave); chosen {
		return AutosaveTidy{}, false, nil
	}
	saves, err := readAutosaves(root)
	if err != nil {
		return AutosaveTidy{}, false, err
	}
	return planAutosaveTidy(saves, slots, loaded)
}

// ApplyAutosaveTidyHeld renames the kept autosaves through their
// `.tidy.zip` names to their slots, then removes the rest (SPEC 11.6). A
// rename keeps the file's modification time. The caller holds Lock(id),
// and has taken the snapshot and written the audit entry first.
func (m *Manager) ApplyAutosaveTidyHeld(id string, t AutosaveTidy) error {
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	for _, mv := range t.Moves {
		mid := autosaveTidyName(mv.n)
		if mv.From == mid {
			continue
		}
		if err := root.Rename(path.Join(SavesDir, mv.From), path.Join(SavesDir, mid)); err != nil {
			return fmt.Errorf("rename %s: %w", mv.From, err)
		}
	}
	for _, mv := range t.Moves {
		if err := root.Rename(path.Join(SavesDir, autosaveTidyName(mv.n)), path.Join(SavesDir, mv.To)); err != nil {
			return fmt.Errorf("rename %s: %w", mv.From, err)
		}
	}
	for _, name := range t.Remove {
		if err := root.Remove(path.Join(SavesDir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

// AutosaveTidyCounts is what the next start's tidy would keep and remove
// if the world had slots S, for the settings preview (SPEC 11.6). Both are
// 0 when no autosave is numbered above S.
func (m *Manager) AutosaveTidyCounts(id string, slots int) (kept, removed int, err error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = root.Close() }()
	saves, err := readAutosaves(root)
	if err != nil {
		return 0, 0, err
	}
	if slots <= 0 || !slices.ContainsFunc(saves, func(s autosave) bool { return s.n > slots }) {
		return 0, 0, nil
	}
	kept = min(slots, len(saves))
	return kept, len(saves) - kept, nil
}
