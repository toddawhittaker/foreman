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
// leftover `_autosaveN.tidy.zip` from a tidy cut short, where N is the
// slot that tidy had already chosen for it.
type autosave struct {
	name    string
	n       int
	tidy    bool
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

// AutosaveMove renames one kept autosave to slot `slot`.
type AutosaveMove struct {
	From, To string
	slot     int
}

// AutosaveTidy is what a tidy does (SPEC 11.6). Kept counts every kept
// file; Moves holds the ones whose name changes. Remove lists every
// autosave not kept.
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
	return name
}

// tidyStep is one file change of a tidy: a rename, or a removal when to
// is "".
type tidyStep struct{ from, to string }

// steps lists the tidy's file changes in order. Kept files first take a
// `.tidy.zip` name holding their final slot, then the rest are removed,
// then the kept files take their final names. So a tidy cut short at any
// step leaves `.tidy.zip` files, which give the next tidy the mapping
// already chosen.
func (t AutosaveTidy) steps() []tidyStep {
	var out []tidyStep
	for _, mv := range t.Moves {
		if mid := autosaveTidyName(mv.slot); mv.From != mid {
			out = append(out, tidyStep{mv.From, mid})
		}
	}
	for _, name := range t.Remove {
		out = append(out, tidyStep{name, ""})
	}
	for _, mv := range t.Moves {
		out = append(out, tidyStep{autosaveTidyName(mv.slot), mv.To})
	}
	return out
}

// errAutosaveLeftover refuses a tidy whose `.tidy.zip` leftovers do not
// fit one mapping for slots S, as when S changed after a tidy cut short.
var errAutosaveLeftover = errors.New("autosave tidy leftovers do not match the slot count")

// planAutosaveTidy works out the tidy for slots S (SPEC 11.6). It returns
// ok false when there is nothing to do: S is 0 or every N is S or less,
// and no leftover from a tidy cut short remains. loaded is the file the
// last run loaded, or "" when Foreman does not know it.
func planAutosaveTidy(saves []autosave, slots int, loaded string) (AutosaveTidy, bool, error) {
	if slots <= 0 {
		return AutosaveTidy{}, false, nil
	}
	need, resume := false, false
	for _, s := range saves {
		need = need || s.n > slots || s.tidy
		resume = resume || s.tidy
	}
	if !need {
		return AutosaveTidy{}, false, nil
	}
	saves = slices.Clone(saves)
	slices.SortFunc(saves, func(a, b autosave) int {
		return cmp.Or(b.modTime.Compare(a.modTime), cmp.Compare(b.n, a.n))
	})
	slot := func(k, i int) int { return ((k-(i+1))%slots+slots)%slots + 1 }
	var k int
	if resume {
		// Renames keep modification times, so the order is as before and
		// one leftover's slot gives back the K that tidy used.
		i := slices.IndexFunc(saves, func(s autosave) bool { return s.tidy })
		k = saves[i].n + i
	} else {
		// A graceful stop saves over the loaded file, so it is not the
		// last autosave the game wrote.
		k = saves[0].n
		for _, s := range saves {
			if s.name != loaded {
				k = s.n
				break
			}
		}
	}
	keep := min(slots, len(saves))
	t := AutosaveTidy{Kept: keep, Removed: len(saves) - keep}
	for i, s := range saves {
		if i >= keep {
			if s.tidy {
				return AutosaveTidy{}, false, errAutosaveLeftover
			}
			t.Remove = append(t.Remove, s.name)
			continue
		}
		n := slot(k, i)
		if s.tidy && s.n != n {
			return AutosaveTidy{}, false, errAutosaveLeftover
		}
		if to := autosaveName(n); to != s.name {
			t.Moves = append(t.Moves, AutosaveMove{From: s.name, To: to, slot: n})
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
		tidy := strings.HasSuffix(e.Name(), autosaveTidySuffix)
		out = append(out, autosave{name: e.Name(), n: n, tidy: tidy, modTime: info.ModTime()})
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

// ApplyAutosaveTidyHeld makes the tidy's file changes in order (SPEC
// 11.6). A rename keeps the file's modification time. The caller holds
// Lock(id), and has taken the snapshot and written the audit entry first.
func (m *Manager) ApplyAutosaveTidyHeld(id string, t AutosaveTidy) error {
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return applyTidySteps(root, t.steps())
}

func applyTidySteps(root *os.Root, steps []tidyStep) error {
	for _, st := range steps {
		if st.to == "" {
			if err := root.Remove(path.Join(SavesDir, st.from)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("remove %s: %w", st.from, err)
			}
			continue
		}
		if err := root.Rename(path.Join(SavesDir, st.from), path.Join(SavesDir, st.to)); err != nil {
			return fmt.Errorf("rename %s: %w", st.from, err)
		}
	}
	return nil
}

// AutosaveTidyCounts is what the next start's tidy would keep and remove
// if the world had slots S, for the settings preview (SPEC 11.6). Both are
// 0 when there is nothing to tidy.
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
	// The loaded file changes only the mapping, never the counts.
	t, _, err := planAutosaveTidy(saves, slots, "")
	return t.Kept, t.Removed, err
}
