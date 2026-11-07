package mods

import (
	"context"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Lookup returns a mod with all its releases from the portal.
type Lookup func(ctx context.Context, name string) (modportal.Mod, error)

// World is what the resolver needs to know about a world.
type World struct {
	GameVersion string // exact, such as "2.0.72"
	SpaceAge    bool
	Mods        []world.InstalledMod
}

// Kind is what a request asks for.
type Kind string

// Request kinds (SPEC 9.2 to 9.5, 9.8).
const (
	KindInstall Kind = "install"
	KindUpdate  Kind = "update" // Names, or every portal mod when empty
	KindSync    Kind = "sync"
	KindEnable  Kind = "enable"
	KindDisable Kind = "disable"
	KindRemove  Kind = "remove"
	KindUpload  Kind = "upload"
)

// SaveMod is one mod and version from a save's mod list.
type SaveMod struct {
	Name, Version string
}

// Upload is another name for world.ModInfo, the info.json of an uploaded
// mod zip (SPEC 9.5); new code uses world.ModInfo.
type Upload = world.ModInfo

// ChoiceKind is the admin's decision for an unresolved mod (SPEC 9.10).
type ChoiceKind string

// Choices for an unresolved mod.
const (
	ChoiceUpload  ChoiceKind = "upload"
	ChoiceNearest ChoiceKind = "nearest"
	ChoiceWithout ChoiceKind = "without"
)

// Choice is the admin's decision for one unresolved mod.
type Choice struct {
	Kind      ChoiceKind
	Upload    *world.ModInfo // the uploaded zip's info.json, for ChoiceUpload
	Confirmed bool           // required for ChoiceWithout
}

// Request is one mod change asked for.
type Request struct {
	Kind     Kind
	Names    []string          // install, update, enable, disable, remove
	Optional []string          // optional dependencies the admin ticked
	Upload   *world.ModInfo    // the uploaded zip's info.json, for KindUpload
	SaveMods []SaveMod         // for KindSync
	Choices  map[string]Choice // for KindSync, by mod name
}

// Action is what a plan does to one mod.
type Action string

// Actions.
const (
	ActInstall   Action = "install"
	ActUpgrade   Action = "upgrade"
	ActDowngrade Action = "downgrade"
	ActEnable    Action = "enable"
	ActDisable   Action = "disable"
	ActRemove    Action = "remove"
	ActKeep      Action = "keep"
)

// ReasonCode says why a mod is in the plan.
type ReasonCode string

// Reasons.
const (
	WhyAsked      ReasonCode = "asked"
	WhyRequiredBy ReasonCode = "required-by"
	WhyOptionalOf ReasonCode = "optional-of"
	WhyFromSave   ReasonCode = "from-save"
	WhyNotInSave  ReasonCode = "not-in-save"
)

// Reason is a code plus the mod it names, if any; the web layer words it.
type Reason struct {
	Code ReasonCode
	Of   string `json:",omitempty"`
}

// Warning codes on a change.
const (
	WarnOlderThanSave = "older-than-save" // nearest release is below the save's
)

// Change is one mod's change in a plan.
type Change struct {
	Name    string
	From    string // installed version, empty when not installed
	To      string // version after the change
	Action  Action
	Why     Reason
	Enabled bool               // enabled after the change
	Release *modportal.Release `json:",omitempty"` // to download, if any
	Upload  bool               // comes from the uploaded zip
	Warning string             `json:",omitempty"`
}

// ConflictCode names a kind of conflict.
type ConflictCode string

// Conflicts. Mod is the mod in trouble; Other is the second mod involved.
const (
	ConflictIncompatible ConflictCode = "incompatible"  // Mod has "! Other", both enabled
	ConflictSpaceAgeOff  ConflictCode = "space-age-off" // Mod needs Space Age mod Other
	ConflictNoRelease    ConflictCode = "no-release"    // no release of Mod fits Needs
	ConflictVersion      ConflictCode = "version"       // Mod's fixed Version fails Needs
	ConflictNotOnPortal  ConflictCode = "not-on-portal"
	ConflictRequiredBy   ConflictCode = "required-by"   // Other needs Mod, which is going
	ConflictBuiltIn      ConflictCode = "built-in"      // built-in mods are not changed
	ConflictLocal        ConflictCode = "local"         // folder mods are not replaced
	ConflictNotInstalled ConflictCode = "not-installed" // asked to change a missing mod
	ConflictBadDep       ConflictCode = "bad-dependency"
	ConflictTooComplex   ConflictCode = "too-complex"
	ConflictBadRequest   ConflictCode = "bad-request"
)

// Need is one version constraint on a mod and the mod that set it.
type Need struct {
	By, Op, Version string
}

// Conflict is one reason a plan cannot be applied.
type Conflict struct {
	Code    ConflictCode
	Mod     string
	Other   string `json:",omitempty"`
	Version string `json:",omitempty"`
	Needs   []Need `json:",omitempty"`
	Text    string `json:",omitempty"` // a malformed dependency string
}

// Unresolved is a save's mod whose exact version the portal does not have.
type Unresolved struct {
	Name, Version string
	Nearest       string // nearest release on the world's line, or empty
	Choice        *Choice
}

// Offer is an optional dependency the admin may tick. Chosen is true once
// it is ticked: it stays offered, so the page keeps its box (SPEC 9.2).
type Offer struct {
	Name, Of string
	Chosen   bool
}

// Plan is the resolved change, shown before anything applies.
type Plan struct {
	Changes    []Change
	Conflicts  []Conflict
	Unresolved []Unresolved
	Optional   []Offer
}

// Applicable reports whether the plan may be applied: no conflict, and
// every unresolved mod has a usable choice.
func (p Plan) Applicable() bool {
	if len(p.Conflicts) > 0 {
		return false
	}
	for _, u := range p.Unresolved {
		if !usableChoice(u) {
			return false
		}
	}
	return true
}

func usableChoice(u Unresolved) bool {
	if u.Choice == nil {
		return false
	}
	switch u.Choice.Kind {
	case ChoiceUpload:
		return u.Choice.Upload != nil
	case ChoiceNearest:
		return u.Nearest != ""
	case ChoiceWithout:
		return u.Choice.Confirmed
	}
	return false
}
