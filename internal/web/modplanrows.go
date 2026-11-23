package web

import (
	"slices"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// PlanItem is one mod's line in the plan's list of changes: its title,
// linked to the portal, then the change.
type PlanItem struct {
	Name string
	// Text words the change without the mod's name; zero for an asked-for
	// mod the plan cannot install, which shows by its name alone.
	Text i18n.Msg
	// Asked marks a mod an install plan asked for: its line carries a way
	// out of the plan.
	Asked bool
	// Version is the mod's version list, in its row; nil when it has none.
	Version *VersionRow
}

// PlanGroup is one kind of change in the plan's list.
type PlanGroup struct {
	Title i18n.Msg
	Items []PlanItem
}

var groupTitles = []struct {
	act   mods.Action
	title i18n.Msg
}{
	{mods.ActInstall, i18n.M("mods.plan.group.install")},
	{mods.ActUpgrade, i18n.M("mods.plan.group.upgrade")},
	{mods.ActDowngrade, i18n.M("mods.plan.group.downgrade")},
	{mods.ActEnable, i18n.M("mods.plan.group.enable")},
	{mods.ActDisable, i18n.M("mods.plan.group.disable")},
	{mods.ActRemove, i18n.M("mods.plan.group.remove")},
}

// Groups lists the plan's changes by kind, in the order they matter, with
// the mods an install asked for first in their group (SPEC 9.2). An asked
// mod the plan cannot install, as with a conflict, is listed under Install
// by its name, so it can still be taken out.
func (p ModPlanPage) Groups() []PlanGroup {
	var out []PlanGroup
	vs := p.versionRows()
	for _, g := range groupTitles {
		var items []PlanItem
		if g.act == mods.ActInstall && p.FromPicks() {
			for _, n := range p.Form.Names {
				if !slices.ContainsFunc(p.Plan.Changes, func(c mods.Change) bool { return c.Name == n }) {
					items = append(items, PlanItem{Name: n, Asked: true, Version: vs[n]})
				}
			}
		}
		for _, c := range p.Plan.Changes {
			// A recommended mod shows only by its box (SPEC 9.2).
			if c.Action == g.act && c.Why.Code != mods.WhyRecommendedBy {
				items = append(items, p.item(c, vs))
			}
		}
		slices.SortStableFunc(items, func(a, b PlanItem) int { return p.askedOrder(a) - p.askedOrder(b) })
		if len(items) > 0 {
			out = append(out, PlanGroup{Title: g.title, Items: items})
		}
	}
	// An asked mod kept at its version still shows, so its version list
	// has a row to sit in.
	var same []PlanItem
	for _, c := range p.Plan.Changes {
		if p.keptListed(c) {
			same = append(same, p.item(c, vs))
		}
	}
	if len(same) > 0 {
		out = append(out, PlanGroup{Title: i18n.M("mods.plan.group.same"), Items: same})
	}
	return out
}

// keptListed says c is an asked mod the plan keeps, with a version list.
func (p ModPlanPage) keptListed(c mods.Change) bool {
	_, listed := p.Plan.Releases[c.Name]
	return c.Action == mods.ActKeep && c.Why.Code == mods.WhyAsked && listed
}

// askedOrder puts the asked mods first, in the order they were added.
func (p ModPlanPage) askedOrder(it PlanItem) int {
	if !it.Asked {
		return len(p.Form.Names)
	}
	return slices.Index(p.Form.Names, it.Name)
}

// Kept lists the mods the plan needs and leaves as they are. An asked mod
// kept at its version is not one of them: Groups shows it with its list,
// so none of these has a version list.
func (p ModPlanPage) Kept() []PlanItem {
	var out []PlanItem
	for _, c := range p.Plan.Changes {
		if c.Action == mods.ActKeep && !p.keptListed(c) {
			out = append(out, p.item(c, nil))
		}
	}
	return out
}

// item is c's row, with its version list from vs.
func (p ModPlanPage) item(c mods.Change, vs map[string]*VersionRow) PlanItem {
	asked := p.FromPicks() && slices.Contains(p.Form.Names, c.Name)
	return PlanItem{Name: c.Name, Text: p.changeText(c), Asked: asked, Version: vs[c.Name]}
}

// ConflictRow is one conflict and, when Foreman has one, its fix: the same
// plan turning one more mod off (SPEC 9.2, 17.9).
type ConflictRow struct {
	Text i18n.Msg
	// Fix words the fix; zero when there is none. Path adds it to the
	// draft, and Redraw the same with the note for the announcer.
	Fix          i18n.Msg
	Path, Redraw string
	ID           string
}

// Conflicts words the plan's conflicts, each with its fix.
func (p ModPlanPage) Conflicts() []ConflictRow {
	out := make([]ConflictRow, 0, len(p.Plan.Conflicts)+1)
	if v := p.Form.FavoriteVersion; v != "" {
		out = append(out, ConflictRow{Text: i18n.M("favorites.review.unsupported_version", "version", v)})
	}
	var offered []string
	for _, c := range p.Plan.Conflicts {
		row := ConflictRow{Text: p.conflictText(c)}
		if name, remove := p.conflictFix(c); name != "" && !slices.Contains(offered, name) {
			offered = append(offered, name)
			field := alsoDisableField
			row.Fix = i18n.M("mods.plan.fix.disable", "mod", p.ident(name))
			if remove {
				field, row.Fix = alsoRemoveField, i18n.M("mods.plan.fix.remove", "mod", p.ident(name))
			}
			row.Path, row.ID = draftPath(p.World.ID, p.Draft, field, name), modID(name, "fix")
			row.Redraw = draftPath(p.World.ID, p.Draft, field, name, "fixed", "1")
		}
		out = append(out, row)
	}
	return out
}

// conflictFix is the mod whose turning off clears c, and whether it is
// removed rather than disabled; "" when no fix is offered. Two mods that
// cannot run together: the one the admin did not ask for goes, unless it is
// a folder mod, which is only disabled. A mod another needs: the one that
// needs it is disabled too.
func (p ModPlanPage) conflictFix(c mods.Conflict) (string, bool) {
	switch c.Code {
	case mods.ConflictIncompatible:
		for _, n := range []string{c.Other, c.Mod} {
			if local, ok := p.canTurnOff(n); ok {
				return n, !local
			}
		}
	case mods.ConflictRequiredBy:
		if _, ok := p.canTurnOff(c.Other); ok {
			return c.Other, false
		}
	}
	return "", false
}

// canTurnOff reports whether an installed, enabled mod that the plan does
// not already change may be turned off by a fix, and whether it is a
// folder mod.
func (p ModPlanPage) canTurnOff(name string) (local, ok bool) {
	i := slices.IndexFunc(p.Have, func(m world.InstalledMod) bool { return m.Name == name })
	if i < 0 {
		return false, false
	}
	m := p.Have[i]
	f := p.Form
	if !m.Enabled || m.BuiltIn || slices.Contains(f.Names, name) || slices.Contains(f.AlsoDisable, name) || slices.Contains(f.AlsoRemove, name) {
		return false, false
	}
	return m.Local, true
}

// extraKinds says the plan turns mods off besides its own kind of change,
// so its heading and button speak of changes in general.
func (p ModPlanPage) extraKinds() bool {
	f := p.Form
	return len(f.AlsoRemove) > 0 && f.Kind != mods.KindRemove || len(f.AlsoDisable) > 0 && f.Kind != mods.KindDisable
}

// PlanRow is a PlanItem with its page, for the template that draws it.
type PlanRow struct {
	Page ModPlanPage
	Item PlanItem
}

// Row pairs an item with the page.
func (p ModPlanPage) Row(it PlanItem) PlanRow { return PlanRow{p, it} }

// VersionRow is one asked mod's version list on the plan page.
type VersionRow struct {
	Name, ID string
	Options  []VersionOption
}

// VersionOption is one entry of a version list. Value "" is the newest
// release that fits; a zero Label shows the version as it is.
type VersionOption struct {
	Value    string
	Label    i18n.Msg
	Selected bool
}

// versionRows is Versions by mod name, built once for a render's rows.
func (p ModPlanPage) versionRows() map[string]*VersionRow {
	out := map[string]*VersionRow{}
	for _, v := range p.Versions() {
		out[v.Name] = &v
	}
	return out
}

// Versions are the version lists of the mods an install or update asks
// for: the newest that fits, then every release on the world's line, newest
// first, with the installed one marked (SPEC 9.2, 9.3). A mod the portal
// lacks, a folder mod and a built-in mod have none.
func (p ModPlanPage) Versions() []VersionRow {
	if p.Form.Kind != mods.KindInstall && p.Form.Kind != mods.KindUpdate {
		return nil
	}
	var out []VersionRow
	for _, n := range p.Form.Names {
		rels, ok := p.Plan.Releases[n]
		if !ok {
			continue
		}
		var from, to string
		if i := slices.IndexFunc(p.Plan.Changes, func(c mods.Change) bool { return c.Name == n }); i >= 0 {
			from, to = p.Plan.Changes[i].From, p.Plan.Changes[i].To
		}
		chosen := p.Form.Versions[n]
		vs := slices.Clone(rels)
		// The installed version, and a chosen one the portal lacks, show
		// too, so the list always says what the plan holds.
		for _, v := range []string{from, chosen} {
			if v != "" && !slices.Contains(vs, v) {
				vs = append(vs, v)
			}
		}
		slices.SortFunc(vs, func(a, b string) int { return world.CompareVersions(b, a) })
		newest := i18n.M("mods.version.newest")
		if chosen == "" && to != "" {
			newest = i18n.M("mods.version.newest_is", "version", to)
		}
		row := VersionRow{Name: n, ID: modID(n, "version"), Options: []VersionOption{{Label: newest, Selected: chosen == ""}}}
		for _, v := range vs {
			o := VersionOption{Value: v, Selected: v == chosen}
			if v == from {
				o.Label = i18n.M("mods.version.installed", "version", v)
			}
			row.Options = append(row.Options, o)
		}
		out = append(out, row)
	}
	return out
}

// ModFacts is what the review page knows of a mod besides its name: the
// title from info.json or the portal, and for an offered mod the portal's
// summary and the release it would install, or has installed (SPEC 9.2).
type ModFacts struct {
	Title, Summary, Version string
	Released                time.Time
	// OnPortal says the portal has the mod, so its title links there.
	OnPortal bool
}

// Mod is name as the mod-name partial shows it: by title, linked to the
// portal unless it is a folder mod, built in, or not on the portal.
func (p ModPlanPage) Mod(name string) ModName {
	f := p.facts[name]
	local := slices.ContainsFunc(p.Have, func(m world.InstalledMod) bool { return m.Name == name && m.Local })
	return modName(name, f.Title, f.OnPortal && !local)
}

// Title is name's title, or name when there is none.
func (p ModPlanPage) Title(name string) string { return p.Mod(name).Title }

// ident is name's title as a message argument: it is a mod's own words, so
// a page translator leaves it be.
func (p ModPlanPage) ident(name string) i18n.Fragment { return i18n.Ident(p.Title(name)) }

// OfferRow is one offered mod's tick box, its title link and its details.
// The box has two labels, a hidden one with the title and the visible words
// saying why it is offered; the link sits outside both, so a click on it
// never ticks the box (SPEC 9.2, 9.4).
type OfferRow struct {
	Offer mods.Offer
	ID    string
	// Of says why it is offered.
	Of    i18n.Msg
	Name  ModName
	Facts ModFacts
	// Field is the box's form field; Path where it posts for a redraw.
	Field, Path string
}

// DetailID is the id of the details the box points to.
func (o OfferRow) DetailID() string { return o.ID + "-detail" }

// HasDetail says there is a summary, version or date to show.
func (o OfferRow) HasDetail() bool {
	return o.Facts.Summary != "" || o.Facts.Version != "" || !o.Facts.Released.IsZero()
}

// OptionalRows are the optional mods' boxes.
func (p ModPlanPage) OptionalRows() []OfferRow { return p.offerRows(false) }

// RecommendedRows are the recommended mods' boxes, in a section of their
// own so each one can be unticked (SPEC 9.2).
func (p ModPlanPage) RecommendedRows() []OfferRow { return p.offerRows(true) }

func (p ModPlanPage) offerRows(recommended bool) []OfferRow {
	var out []OfferRow
	for _, o := range p.Plan.Optional {
		if o.Recommended != recommended {
			continue
		}
		of := p.ident(o.Of)
		r := OfferRow{Offer: o, ID: p.ModID(o.Name, "optional"), Name: p.Mod(o.Name), Facts: p.facts[o.Name],
			Of:    i18n.M("mods.plan.why.optional_of", "mod", of),
			Field: "optional", Path: p.RedrawPath()}
		if o.Recommended {
			r.Of = i18n.M("mods.plan.why.recommended_by", "mod", of)
		}
		out = append(out, r)
	}
	return out
}

// OrphanRows are the boxes of the mods no longer needed.
func (p ModPlanPage) OrphanRows() []OfferRow {
	out := make([]OfferRow, 0, len(p.Plan.Orphans))
	for _, o := range p.Plan.Orphans {
		of := p.ident(o.Of)
		out = append(out, OfferRow{Offer: o, ID: p.ModID(o.Name, "orphan"), Name: p.Mod(o.Name), Facts: p.facts[o.Name],
			Of:    i18n.M("mods.plan.why.only_needed_by", "mod", of),
			Field: "orphan", Path: p.RedrawPath()})
	}
	return out
}
