package web

import (
	"net/url"
	"slices"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// PlanItem is one mod's line in the plan's list of changes.
type PlanItem struct {
	Name string
	// Text words the change; zero for an asked-for mod the plan cannot
	// install, which shows by its name.
	Text i18n.Msg
	// Asked marks a mod an install plan asked for: its line carries the
	// portal link, when the portal has it, and a way out of the plan.
	Asked, Portal bool
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
	for _, g := range groupTitles {
		var items []PlanItem
		if g.act == mods.ActInstall && p.Adding() {
			for _, n := range p.Form.Names {
				if !slices.ContainsFunc(p.Plan.Changes, func(c mods.Change) bool { return c.Name == n }) {
					items = append(items, PlanItem{Name: n, Asked: true})
				}
			}
		}
		for _, c := range p.Plan.Changes {
			if c.Action == g.act {
				items = append(items, p.item(c))
			}
		}
		slices.SortStableFunc(items, func(a, b PlanItem) int { return p.askedOrder(a) - p.askedOrder(b) })
		if len(items) > 0 {
			out = append(out, PlanGroup{Title: g.title, Items: items})
		}
	}
	return out
}

// askedOrder puts the asked mods first, in the order they were added.
func (p ModPlanPage) askedOrder(it PlanItem) int {
	if !it.Asked {
		return len(p.Form.Names)
	}
	return slices.Index(p.Form.Names, it.Name)
}

// Kept lists the mods the plan needs and leaves as they are. An asked mod
// kept at its version is not one of them: its version list shows it.
func (p ModPlanPage) Kept() []PlanItem {
	var out []PlanItem
	for _, c := range p.Plan.Changes {
		if c.Action == mods.ActKeep && (c.Why.Code != mods.WhyAsked || p.Plan.Releases[c.Name] == nil) {
			out = append(out, p.item(c))
		}
	}
	return out
}

func (p ModPlanPage) item(c mods.Change) PlanItem {
	asked := p.Adding() && slices.Contains(p.Form.Names, c.Name)
	return PlanItem{Name: c.Name, Text: changeText(c), Asked: asked, Portal: asked && c.Release != nil}
}

// ConflictRow is one conflict and, when Foreman has one, its fix: the same
// plan turning one more mod off (SPEC 9.2, 17.9).
type ConflictRow struct {
	Text i18n.Msg
	// Fix words the fix; zero when there is none. Path is the plan with
	// it, and Redraw the same with the note for the announcer.
	Fix          i18n.Msg
	Path, Redraw string
	ID           string
}

// Conflicts words the plan's conflicts, each with its fix.
func (p ModPlanPage) Conflicts() []ConflictRow {
	out := make([]ConflictRow, 0, len(p.Plan.Conflicts))
	var offered []string
	for _, c := range p.Plan.Conflicts {
		row := ConflictRow{Text: conflictText(c)}
		if name, remove := p.conflictFix(c); name != "" && !slices.Contains(offered, name) {
			offered = append(offered, name)
			f := p.Form
			if remove {
				f.AlsoRemove, row.Fix = append(slices.Clone(f.AlsoRemove), name), i18n.M("mods.plan.fix.remove", "mod", i18n.Ident(name))
			} else {
				f.AlsoDisable, row.Fix = append(slices.Clone(f.AlsoDisable), name), i18n.M("mods.plan.fix.disable", "mod", i18n.Ident(name))
			}
			q := f.query()
			row.Path, row.ID = planPath(p.World.ID, q), modID(name, "fix")
			q.Set("fixed", name)
			row.Redraw = planPath(p.World.ID, q)
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

// SearchPath is the search the admin came from, for "Back to search".
func (p ModPlanPage) SearchPath() string {
	path := modsPath(p.World.ID) + "/search"
	if p.Form.Query != "" {
		path += "?q=" + url.QueryEscape(p.Form.Query)
	}
	return path
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
