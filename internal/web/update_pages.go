package web

import (
	"net/url"
	"strings"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// VersionPicker fills the game version select's options
// (SPEC 10.7). Above, when set, leaves out versions not newer than it.
// Selected, when it is one of the listed versions, is the option chosen.
type VersionPicker struct {
	Above, Selected      string
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
	VersionError i18n.Msg
	Running      bool
	CountdownForm
	Refusal i18n.Msg
}

// RollbackPage confirms going back to the version before the last update
// (SPEC 10.4).
type RollbackPage struct {
	World    world.Summary
	Snapshot world.Snapshot
	Version  string
	Running  bool
	CountdownForm
	Error i18n.Msg
}

// RollbackOffer is what the world page shows to offer a rollback: the
// "Before update" snapshot and the version it goes back to (SPEC 10.4).
type RollbackOffer struct {
	Snapshot world.Snapshot
	Version  string
	Path     string
}

// versionsPath is where the picker's suggestion list loads from.
func versionsPath(above string) string { return versionsPathSelecting(above, "") }

// versionsPathSelecting is versionsPath for a page that already names a
// version, so the list comes back with that one chosen.
func versionsPathSelecting(above, selected string) string {
	q := url.Values{}
	if above != "" {
		q.Set("above", above)
	}
	if selected != "" {
		q.Set("selected", selected)
	}
	if len(q) == 0 {
		return PathVersions
	}
	return PathVersions + "?" + q.Encode()
}

func (VersionPicker) templateName() string { return "versions" }
func (ReleaseNotice) templateName() string { return "versions" }
func (UpdatePage) templateName() string    { return "update" }
func (RollbackPage) templateName() string  { return "rollback" }
func (UpdatePage) navItem() string         { return PathWorlds }
func (RollbackPage) navItem() string       { return PathWorlds }

func (p UpdatePage) hasErrors() bool   { return len(p.Problems()) > 0 }
func (p RollbackPage) hasErrors() bool { return !p.CountdownError.IsZero() }

// Problems lists the field errors in the order the fields appear.
func (p UpdatePage) Problems() []Problem {
	return problems(Problem{"update-version", p.VersionError}, Problem{"countdown", p.CountdownError})
}

// Problems lists the countdown field's error.
func (p RollbackPage) Problems() []Problem {
	return problems(Problem{"countdown", p.CountdownError})
}

// Label marks the stable and experimental releases in the suggestion list.
func (p VersionPicker) Label(v string) i18n.Msg {
	switch v {
	case p.Stable:
		return i18n.M("server.versions.stable", "version", v)
	case p.Experimental:
		return i18n.M("server.versions.experimental", "version", v)
	}
	return i18n.M("server.versions.plain", "version", v)
}

// chosenVersion is the version a form asked for: the typed field wins
// over the select when it is filled.
func chosenVersion(picked, typed string) string {
	if t := strings.TrimSpace(typed); t != "" {
		return t
	}
	return strings.TrimSpace(picked)
}

// Paths the pages link and post to.
func (p UpdatePage) Path() string      { return worldPath(p.World.ID) + "/update" }
func (p UpdatePage) WorldPath() string { return worldPath(p.World.ID) }
func (p UpdatePage) VersionsPath() string {
	return versionsPathSelecting(p.World.GameVersion, p.Target)
}
func (p RollbackPage) Path() string      { return worldPath(p.World.ID) + "/rollback" }
func (p RollbackPage) WorldPath() string { return worldPath(p.World.ID) }
func (p WorldPage) UpdatePath() string   { return worldPath(p.World.ID) + "/update" }
func (ImportForm) VersionsPath() string  { return versionsPath("") }
