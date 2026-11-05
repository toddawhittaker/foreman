package web

import (
	"net/url"

	"github.com/toddawhittaker/foreman/internal/world"
)

// VersionPicker fills the game version input's suggestion list
// (SPEC 10.7). Above, when set, leaves out versions not newer than it.
type VersionPicker struct {
	Above                string
	Versions             []string
	Stable, Experimental string
	Unreachable          bool
}

// ReleaseNotice says whether a newer Factorio release is out (SPEC 10.2).
// Stable and Experimental are set only when newer than Current. An empty
// UpdatePath means the newer release has no image yet, so Update is off.
type ReleaseNotice struct {
	Current, Stable, Experimental, UpdatePath, ChangelogURL string
	Unreachable                                             bool
}

// UpdatePage confirms moving the active world to a newer game version
// (SPEC 10.3).
type UpdatePage struct {
	World        world.Summary
	Target       string
	VersionError string
	Running      bool
	CountdownForm
	Refusal string
}

// RollbackPage confirms going back to the version before the last update
// (SPEC 10.4).
type RollbackPage struct {
	World    world.Summary
	Snapshot world.Snapshot
	Version  string
	Running  bool
	CountdownForm
	Error string
}

// RollbackOffer is what the world page shows to offer a rollback: the
// "Before update" snapshot and the version it goes back to (SPEC 10.4).
type RollbackOffer struct {
	Snapshot world.Snapshot
	Version  string
	Path     string
}

// versionsPath is where the picker's suggestion list loads from.
func versionsPath(above string) string {
	if above == "" {
		return PathVersions
	}
	return PathVersions + "?" + url.Values{"above": {above}}.Encode()
}

func (VersionPicker) templateName() string { return "versions" }
func (ReleaseNotice) templateName() string { return "versions" }
func (UpdatePage) templateName() string    { return "update" }
func (RollbackPage) templateName() string  { return "rollback" }
func (UpdatePage) navItem() string         { return PathWorlds }
func (RollbackPage) navItem() string       { return PathWorlds }

func (p UpdatePage) hasErrors() bool   { return len(p.Problems()) > 0 }
func (p RollbackPage) hasErrors() bool { return p.CountdownError != "" }

// Problems lists the field errors in the order the fields appear.
func (p UpdatePage) Problems() []Problem {
	return problems(Problem{"update-version", p.VersionError}, Problem{"countdown", p.CountdownError})
}

// Problems lists the countdown field's error.
func (p RollbackPage) Problems() []Problem {
	return problems(Problem{"countdown", p.CountdownError})
}

// Label marks the stable and experimental releases in the suggestion list.
func (p VersionPicker) Label(v string) string {
	switch v {
	case p.Stable:
		return v + ", stable"
	case p.Experimental:
		return v + ", experimental"
	}
	return v
}

// Paths the pages link and post to.
func (p UpdatePage) Path() string         { return worldPath(p.World.ID) + "/update" }
func (p UpdatePage) WorldPath() string    { return worldPath(p.World.ID) }
func (p UpdatePage) VersionsPath() string { return versionsPath(p.World.GameVersion) }
func (p RollbackPage) Path() string       { return worldPath(p.World.ID) + "/rollback" }
func (p RollbackPage) WorldPath() string  { return worldPath(p.World.ID) }
func (p WorldPage) UpdatePath() string    { return worldPath(p.World.ID) + "/update" }
func (ImportForm) VersionsPath() string   { return versionsPath("") }
