package world

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// ProblemKind names one reason the pre-start check stops a start.
type ProblemKind string

// Pre-start problems (SPEC 7.4).
const (
	// ProblemModMissing: a mod in the save has no file in the world.
	ProblemModMissing ProblemKind = "mod-missing"
	// ProblemSpaceAgeOff: the save uses Space Age but the world has it off.
	ProblemSpaceAgeOff ProblemKind = "space-age-off"
	// ProblemSaveUnreadable: Foreman could not read the save.
	ProblemSaveUnreadable ProblemKind = "save-unreadable"
	// ProblemSaveNewer: the save was made by a newer game than the world's.
	ProblemSaveNewer ProblemKind = "save-newer"
	// ProblemSaveUnsupported: the save was made by a game version Foreman
	// does not support.
	ProblemSaveUnsupported ProblemKind = "save-unsupported"
	// ProblemNoSave: there is no save to load.
	ProblemNoSave ProblemKind = "no-save"
	// ProblemModListSpaceAge: mod-list.json disagrees with the Space Age flag.
	ProblemModListSpaceAge ProblemKind = "mod-list-space-age"
	// ProblemGameVersion: the world's game version is not one Foreman runs.
	ProblemGameVersion ProblemKind = "game-version"
	// ProblemModDisabled: a mod in the save has its file but is turned off.
	ProblemModDisabled ProblemKind = "mod-disabled"
	// ProblemModGameVersion: enabled mods were made for another Factorio
	// than the one the game runs, so the game would refuse to load.
	ProblemModGameVersion ProblemKind = "mod-game-version"
)

// Problem is one reason not to start, in words for the admin.
type Problem struct {
	Kind ProblemKind
	// Text says what the game would delete, or why it cannot start.
	Text string
	// StartAnyway is true when the admin may start after a confirmation
	// (SPEC 7.4): only mod and Space Age mismatches and an unreadable save.
	StartAnyway bool
	// Mods names the mods a missing or disabled mod problem is about.
	Mods []string
}

// PreCheck is the result of the pre-start check.
type PreCheck struct {
	// Save is the save that will load, or empty when there is none.
	Save string
	// SaveVersion is the game version that wrote Save; zero when the save
	// could not be read.
	SaveVersion savefile.Version
	Problems    []Problem
}

// OK reports whether the world can start with no confirmation.
func (p PreCheck) OK() bool { return len(p.Problems) == 0 }

// CanStartAnyway reports whether every problem allows "Start anyway".
func (p PreCheck) CanStartAnyway() bool {
	for _, pr := range p.Problems {
		if !pr.StartAnyway {
			return false
		}
	}
	return len(p.Problems) > 0
}

// PreStartCheck reads the save that will load and compares it with the
// world's mods, Space Age flag and game version (SPEC 7.4). run says which
// Factorio the game will run, for the check of the mods' own versions. It
// changes nothing. An error means the check itself could not run.
func (m *Manager) PreStartCheck(id string, run GameRun) (PreCheck, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return PreCheck{}, err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return PreCheck{}, err
	}

	var out PreCheck
	if !SupportedGameVersion(meta.GameVersion) {
		out.Problems = append(out.Problems, Problem{Kind: ProblemGameVersion,
			Text: fmt.Sprintf("The world is set to Factorio %s. Foreman runs only Factorio 1.1, 2.0 and 2.1.", meta.GameVersion)})
		return out, nil
	}
	mods, err := readModList(root)
	if err != nil {
		return PreCheck{}, err
	}
	present, err := presentMods(root)
	if err != nil {
		return PreCheck{}, err
	}
	if p, bad := modListSpaceAge(meta, mods); bad {
		out.Problems = append(out.Problems, p)
	}
	// The mod version problem is worded once the save's version is known,
	// and sits where the mod checks are.
	modAt := len(out.Problems)
	running, wrong := modVersionMismatch(root, meta, mods, run)
	finish := func() (PreCheck, error) {
		if len(wrong) > 0 {
			p := modVersionProblem(meta.Name, run, running, wrong, out.SaveVersion)
			out.Problems = slices.Insert(out.Problems, modAt, p)
		}
		return out, nil
	}

	name := meta.LoadSave
	if name == "" {
		newest, found, err := newestSave(root)
		if err != nil {
			return PreCheck{}, err
		}
		if !found {
			out.Problems = append(out.Problems, Problem{Kind: ProblemNoSave,
				Text: "The world has no save. Upload a save first; without one the game would make a new map."})
			return finish()
		}
		name = newest.Name
	}
	save, err := m.readSave(id, name)
	if errors.Is(err, ErrSaveNotFound) {
		out.Problems = append(out.Problems, Problem{Kind: ProblemNoSave,
			Text: fmt.Sprintf("The save chosen to load, %s, is no longer in the world. Choose another save.", name)})
		return finish()
	}
	out.Save = name
	var unsupported *savefile.UnsupportedVersionError
	switch {
	case errors.As(err, &unsupported):
		out.Problems = append(out.Problems, Problem{Kind: ProblemSaveUnsupported,
			Text: fmt.Sprintf("The save %s was made by Factorio %s. Foreman runs only saves from Factorio 1.1, 2.0 and 2.1.", name, unsupported.Version)})
		return finish()
	case isSaveFormatError(err):
		out.Problems = append(out.Problems, Problem{Kind: ProblemSaveUnreadable, StartAnyway: true,
			Text: fmt.Sprintf("Foreman could not read the save %s (%v), so its mods could not be checked. The game may delete content from mods the world lacks.", name, err)})
		return finish()
	case err != nil:
		return PreCheck{}, err
	}
	out.SaveVersion = save.Version
	out.Problems = append(out.Problems, compareSave(meta, mods, present, name, save, run.Adopted)...)
	return finish()
}

// readSave opens and reads one save of the world.
func (m *Manager) readSave(id, name string) (savefile.Save, error) {
	f, info, err := m.OpenSave(id, name)
	if err != nil {
		return savefile.Save{}, err
	}
	defer func() { _ = f.Close() }()
	return savefile.Read(f, info.Size())
}

// isSaveFormatError reports whether err says the save's bytes are bad, as
// opposed to a failure reading the disk.
func isSaveFormatError(err error) bool {
	for _, e := range []error{savefile.ErrNotZip, savefile.ErrNoLevelData, savefile.ErrFormat, savefile.ErrTruncated} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// modListSpaceAge checks that mod-list.json enables the Space Age mods
// exactly when the world's flag is on (SPEC 7.4). With no mod-list.json the
// game enables every built-in mod.
func modListSpaceAge(meta Meta, mods modList) (Problem, bool) {
	dlc := SpaceAgeMods(meta.GameVersion)
	var on, off []string
	for _, name := range dlc {
		if !mods.found || mods.enabled[name] {
			on = append(on, name)
		} else {
			off = append(off, name)
		}
	}
	switch {
	case meta.SpaceAge && len(off) > 0:
		return Problem{Kind: ProblemModListSpaceAge,
			Text: fmt.Sprintf("Space Age is on for this world, but mod-list.json has %s turned off. Fix the mod list before starting.", strings.Join(off, ", "))}, true
	case !meta.SpaceAge && len(on) > 0:
		return Problem{Kind: ProblemModListSpaceAge,
			Text: fmt.Sprintf("Space Age is off for this world, but mod-list.json has %s turned on (or is missing, so the game turns them on). Fix the mod list before starting.", strings.Join(on, ", "))}, true
	}
	return Problem{}, false
}

// compareSave lists what the game would delete from save, and refuses a
// save newer than the world's game version, except in adopted mode, where
// the container decides the version and the game side checks the save
// against it (SPEC 4.9).
func compareSave(meta Meta, mods modList, present map[string]bool, name string, save savefile.Save, adopted bool) []Problem {
	world := parseGameVersion(meta.GameVersion)
	if !adopted && compareVersion(save.Version, world) > 0 {
		return []Problem{{Kind: ProblemSaveNewer,
			Text: fmt.Sprintf("The save %s was made by Factorio %s, newer than this world's Factorio %s. An older game cannot load it.", name, save.Version, meta.GameVersion)}}
	}
	builtin := map[string]bool{}
	for _, b := range BuiltinMods(meta.GameVersion) {
		builtin[b] = true
	}
	dlc := map[string]bool{}
	for _, d := range SpaceAgeMods(meta.GameVersion) {
		dlc[d] = true
	}
	var usesDLC, missing, disabled []string
	for _, mod := range save.Mods {
		switch {
		case dlc[mod.Name]:
			if !meta.SpaceAge {
				usesDLC = append(usesDLC, mod.Name)
			}
		case builtin[mod.Name]:
		case !present[mod.Name]:
			missing = append(missing, mod.Name)
		case mods.disabled[mod.Name]:
			disabled = append(disabled, mod.Name)
		}
	}
	var out []Problem
	if len(usesDLC) > 0 {
		out = append(out, Problem{Kind: ProblemSpaceAgeOff, StartAnyway: true,
			Text: fmt.Sprintf("The save %s uses Space Age (%s), but Space Age is off for this world. The game would delete all Space Age content from the save.", name, strings.Join(usesDLC, ", "))})
	}
	if len(missing) > 0 {
		files, noun, they := "file", "mod", "it"
		if len(missing) > 1 {
			files, noun, they = "files", "mods", "they"
		}
		out = append(out, Problem{Kind: ProblemModMissing, StartAnyway: true, Mods: missing,
			Text: fmt.Sprintf("This world does not have the %s of the %s the save %s was last played with: %s. Switching or starting anyway removes everything %s built from the save.",
				files, noun, name, strings.Join(missing, ", "), they)})
	}
	if len(disabled) > 0 {
		is, they := "is", "it"
		if len(disabled) > 1 {
			is, they = "are", "they"
		}
		out = append(out, Problem{Kind: ProblemModDisabled, StartAnyway: true, Mods: disabled,
			Text: fmt.Sprintf("The save %s was last played with %s, which %s turned off in this world. Switching or starting anyway removes everything %s built from the save.",
				name, strings.Join(disabled, ", "), is, they)})
	}
	return out
}

// parseGameVersion reads "2.0.72". The caller has checked its shape.
func parseGameVersion(v string) savefile.Version {
	var n [3]uint16
	for i, p := range strings.SplitN(v, ".", 3) {
		x, _ := strconv.ParseUint(p, 10, 16)
		n[i] = uint16(x)
	}
	return savefile.Version{Major: n[0], Minor: n[1], Patch: n[2]}
}

// compareVersion compares major, minor and patch, ignoring the build.
func compareVersion(a, b savefile.Version) int {
	return slices.Compare([]uint16{a.Major, a.Minor, a.Patch}, []uint16{b.Major, b.Minor, b.Patch})
}
