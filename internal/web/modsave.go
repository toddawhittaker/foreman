package web

import (
	"slices"

	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SaveDiff counts how a world's mods differ from the save that loads, in
// the terms a sync would change them (SPEC 9.8): each count is mods.
type SaveDiff struct {
	Missing int // the save uses it and the world does not have it
	Off     int // the save uses it and the world has it turned off
	Version int // the save uses it at another version
	Extra   int // turned on in the world and not in the save
}

// Matches says a sync would change nothing.
func (d SaveDiff) Matches() bool { return d == SaveDiff{} }

// followSpaceAge is the list with the built-in mods on or off as the
// world's Space Age flag says (SPEC 9.7): off names those that are off.
func followSpaceAge(installed []world.InstalledMod, off []string) []world.InstalledMod {
	out := slices.Clone(installed)
	for i, m := range out {
		if m.BuiltIn {
			out[i].Enabled = !slices.Contains(off, m.Name)
		}
	}
	return out
}

// compareSave compares the world's mods with a save's, as the sync does:
// built-in mods follow Space Age, so only one the save uses and the world
// has off counts; their versions are the game's and never count.
func compareSave(installed []world.InstalledMod, save savefile.Save) SaveDiff {
	have := map[string]world.InstalledMod{}
	for _, m := range installed {
		have[m.Name] = m
	}
	var d SaveDiff
	inSave := map[string]bool{}
	for _, sm := range save.Mods {
		inSave[sm.Name] = true
		m, ok := have[sm.Name]
		switch {
		case !ok:
			d.Missing++
		case !m.Enabled:
			d.Off++
		case !m.BuiltIn && m.Version != sm.Version.String():
			d.Version++
		}
	}
	for _, m := range installed {
		if m.Enabled && !m.BuiltIn && !inSave[m.Name] {
			d.Extra++
		}
	}
	return d
}
