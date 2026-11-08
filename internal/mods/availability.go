package mods

import (
	"context"
	"errors"
	"io/fs"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

// FlagCode names a problem players may have getting a world's mod.
type FlagCode string

// Availability flags; the web layer words them.
const (
	// FlagVersionGone: the portal lacks the installed version, so players'
	// clients cannot sync to it. The fix is the SPEC 9.3 update of the mod.
	FlagVersionGone FlagCode = "version-gone"
	// FlagDeprecated: players cannot find the mod in the in-game browser.
	FlagDeprecated FlagCode = "deprecated"
	// FlagNotOnPortal: players need the file from the admin.
	FlagNotOnPortal FlagCode = "not-on-portal"
)

// Severity of a flag.
type Severity string

// Severities.
const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevNote    Severity = "note"
)

// Flag is one availability problem with an installed mod.
type Flag struct {
	Code     FlagCode
	Severity Severity
	Name     string
	Version  string // installed version
	Update   string `json:",omitempty"` // newest release on the world's line, for FlagVersionGone
}

// Availability checks each enabled mod that is neither built in nor a
// folder against the portal. A portal failure is returned as an error,
// never as flags. A world briefly missing or busy, as during a restore, has
// nothing to report and is not an error.
func (p *Planner) Availability(ctx context.Context, worldID string) ([]Flag, error) {
	w, err := p.world(ctx, worldID)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, world.ErrBusy) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	line := GameLine(w.GameVersion)
	mods := checkable(w.Mods)
	results, err := p.fetch(ctx, mods)
	if err != nil {
		return nil, err
	}
	var flags []Flag
	for _, m := range mods {
		r := results[m.Name]
		f := Flag{Name: m.Name, Version: m.Version}
		if errors.Is(r.Err, modportal.ErrNotOnPortal) {
			f.Code, f.Severity = FlagNotOnPortal, SevNote
			flags = append(flags, f)
			continue
		}
		if findRelease(&r.Mod, m.Version, "") == nil {
			gone := f
			gone.Code, gone.Severity, gone.Update = FlagVersionGone, SevError, newestOnLine(r.Mod, line)
			flags = append(flags, gone)
		}
		if r.Mod.Deprecated {
			f.Code, f.Severity = FlagDeprecated, SevWarning
			flags = append(flags, f)
		}
	}
	return flags, nil
}
