package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The plan panel's routes: add or remove one picked mod, or clear them all
// (SPEC 9.2, 9.14).
const (
	PathModPlanMods  = "/worlds/{id}/mods/plan/mods"
	PathModPlanClear = "/worlds/{id}/mods/plan/clear"
)

// maxPanelRows bounds the result rows one change redraws: a page shows ten.
const maxPanelRows = hitsPerPage

func (s *server) modPlanRoutes() {
	s.mux.HandleFunc("POST "+PathModPlanMods, s.requireUser(s.handlePlanMods))
	s.mux.HandleFunc("POST "+PathModPlanClear, s.requireUser(s.handlePlanClear))
}

// PlanPanel is a world's unfinished mod plan beside the search results. Only
// the picked names are saved; what they need is worked out each time it is
// drawn (SPEC 9.14).
type PlanPanel struct {
	World world.Summary
	// Picks are the mods picked with "+", oldest first; Started is when the
	// first was picked.
	Picks   []string
	Started time.Time
	// Roots are the picks, each with the dependencies that change under it.
	Roots []PlanNode
	// Count is how many distinct mods the plan installs or changes.
	Count int
	// Unreachable says the portal could not be asked, so only the names show.
	Unreachable bool
	// Error says why a "+" was refused.
	Error i18n.Msg
	// Return is the page the panel's forms come back to without script.
	Return string
	// Said is announced after a change made with script, and Focus is the
	// id that takes focus then (WCAG 4.1.3, 2.4.3).
	Said  i18n.Msg
	Focus string
	// neededBy maps a dependency in the tree to the title of the first mod
	// it shows under.
	neededBy map[string]string
}

// PlanNode is one mod in the panel's tree.
type PlanNode struct {
	Name, Title, Version string
	// NotOnPortal says the portal lacks the mod, so its name is not linked.
	NotOnPortal bool
	// Of is the title of the mod this one shows under; empty for a pick.
	Of string
	// Note says what changes for a mod already installed.
	Note     i18n.Msg
	Warnings []i18n.Msg
	Children []PlanNode
}

// Linked says the mod's name links to its portal page.
func (n PlanNode) Linked() bool { return !n.NotOnPortal }

func (p PlanPanel) ModsPath() string   { return modsPath(p.World.ID) }
func (p PlanPanel) ChangePath() string { return modsPath(p.World.ID) + "/plan/mods" }
func (p PlanPanel) ClearPath() string  { return modsPath(p.World.ID) + "/plan/clear" }

// ReviewPath is today's plan page for the picks; it adds their dependencies.
func (p PlanPanel) ReviewPath() string { return kindPlanPath(p.World.ID, mods.KindInstall, "") }

// RemoveID is the id of a pick's cross in the panel.
func (PlanPanel) RemoveID(name string) string { return modID(name, "unpick") }

// Names are the picks' titles, for the phone bar's one line.
func (p PlanPanel) Names() string {
	out := make([]string, len(p.Roots))
	for i, n := range p.Roots {
		out[i] = n.Title
	}
	return strings.Join(out, ", ")
}

// Picked reports whether a mod was picked with "+".
func (p PlanPanel) Picked(name string) bool { return slices.Contains(p.Picks, name) }

// NeededBy is the title of the mod that needs name, when name is in the
// plan only as a dependency.
func (p PlanPanel) NeededBy(name string) string { return p.neededBy[name] }

// planPanel reads a world's picks and works out what they need now.
func (s *server) planPanel(ctx context.Context, sum world.Summary, ret string) (PlanPanel, error) {
	p := PlanPanel{World: sum, Return: ret}
	picked, err := s.store.ModPlan(ctx, sum.ID)
	if err != nil {
		return PlanPanel{}, err
	}
	if len(picked) == 0 {
		return p, nil
	}
	p.Started = picked[0].AddedAt
	for _, m := range picked {
		p.Picks = append(p.Picks, m.Name)
	}
	plan, _, err := s.planner.Plan(ctx, sum.ID, mods.Request{Kind: mods.KindInstall, Names: p.Picks})
	if err != nil {
		s.portalTrouble("work out the mod plan", sum.ID, err)
		p.Unreachable = true
		for _, n := range p.Picks {
			p.Roots = append(p.Roots, PlanNode{Name: n, Title: n})
		}
		return p, nil
	}
	t := planTree{plan: plan, line: mods.GameLine(sum.GameVersion), titles: map[string]string{}, changes: map[string]mods.Change{},
		title: func(name string) string {
			if m, err := s.portal.Mod(ctx, name); err == nil && m.Title != "" {
				return m.Title
			}
			return name
		}}
	p.Roots, p.Count, p.neededBy = t.build(p.Picks)
	return p, nil
}

// planTree turns a resolved plan into the panel's tree: each pick with the
// dependencies that change under it, a shared one under each mod that
// needs it, built-in and already fitting mods left out (SPEC 9.2).
type planTree struct {
	plan    mods.Plan
	line    string
	title   func(string) string
	titles  map[string]string
	changes map[string]mods.Change
}

func (t *planTree) build(picks []string) ([]PlanNode, int, map[string]string) {
	for _, c := range t.plan.Changes {
		t.changes[c.Name] = c
	}
	counted := map[string]bool{}
	neededBy := map[string]string{}
	roots := make([]PlanNode, 0, len(picks))
	for _, name := range picks {
		root := t.node(name, "")
		if c, ok := t.changes[name]; ok && c.Action == mods.ActKeep {
			root.Note = i18n.M("mods.plan_panel.installed")
		}
		seen := map[string]bool{name: true}
		root.Children = t.children(name, root.Title, picks, seen)
		roots = append(roots, root)
		t.count(root, counted, neededBy)
	}
	return roots, len(counted), neededBy
}

// count adds the tree's changing mods to counted, and each dependency's
// first parent to neededBy.
func (t *planTree) count(n PlanNode, counted map[string]bool, neededBy map[string]string) {
	if t.changing(n.Name) {
		counted[n.Name] = true
	}
	if n.Of != "" && neededBy[n.Name] == "" {
		neededBy[n.Name] = n.Of
	}
	for _, c := range n.Children {
		t.count(c, counted, neededBy)
	}
}

// children are the dependencies of name to show under the mod titled of.
// A hidden dependency's own changing dependencies are lifted into its place.
// Picks show as roots of their own, never under another pick.
func (t *planTree) children(name, of string, picks []string, seen map[string]bool) []PlanNode {
	var out []PlanNode
	for _, d := range t.plan.Requires[name] {
		if seen[d] || slices.Contains(picks, d) {
			continue
		}
		seen[d] = true
		if !t.shown(d) {
			out = append(out, t.children(d, of, picks, seen)...)
			continue
		}
		n := t.node(d, of)
		n.Children = t.children(d, n.Title, picks, seen)
		out = append(out, n)
	}
	return out
}

func (t *planTree) changing(name string) bool {
	c, ok := t.changes[name]
	return ok && c.Action != mods.ActKeep
}

// shown says a dependency belongs in the panel: it changes, or it has a
// problem. Built-in mods and installed ones that fit are left out.
func (t *planTree) shown(name string) bool {
	return t.changing(name) || len(t.warnings(name)) > 0
}

func (t *planTree) node(name, of string) PlanNode {
	n := PlanNode{Name: name, Title: t.titleOf(name), Of: of, Warnings: t.warnings(name)}
	for _, c := range t.plan.Conflicts {
		if c.Code == mods.ConflictNotOnPortal && c.Mod == name {
			n.NotOnPortal = true
		}
	}
	c, ok := t.changes[name]
	if !ok {
		return n
	}
	n.Version = c.To
	switch c.Action {
	case mods.ActUpgrade:
		n.Version, n.Note = c.From, i18n.M("mods.plan_panel.update", "version", c.To)
	case mods.ActDowngrade:
		n.Version, n.Note = c.From, i18n.M("mods.plan_panel.change", "version", c.To)
	case mods.ActEnable:
		n.Note = i18n.M("mods.plan_panel.enable")
	}
	return n
}

func (t *planTree) titleOf(name string) string {
	if v, ok := t.titles[name]; ok {
		return v
	}
	v := t.title(name)
	t.titles[name] = v
	return v
}

// warnings word, shortly, each conflict that names the mod (SPEC 9.2); the
// review page shows them in full with their fixes.
func (t *planTree) warnings(name string) []i18n.Msg {
	var out []i18n.Msg
	said := map[string]bool{}
	for _, c := range t.plan.Conflicts {
		var m i18n.Msg
		key := string(c.Code) + "\x00" + c.Version
		switch {
		case c.Code == mods.ConflictIncompatible && c.Other == name:
			m, key = i18n.M("mods.plan_panel.warn.incompatible", "mod", c.Mod), key+c.Mod
		case c.Mod != name:
			continue
		case c.Code == mods.ConflictNotOnPortal:
			m = i18n.M("mods.plan_panel.warn.not_on_portal")
		case c.Code == mods.ConflictNoRelease:
			m = i18n.M("mods.plan_panel.warn.no_release", "line", t.line)
		case c.Code == mods.ConflictIncompatible:
			m, key = i18n.M("mods.plan_panel.warn.incompatible", "mod", c.Other), key+c.Other
		case c.Code == mods.ConflictVersion:
			m = i18n.M("mods.plan_panel.warn.version", "version", c.Version)
		case c.Code == mods.ConflictSpaceAgeOff:
			m = i18n.M("mods.plan_panel.warn.space_age")
		default:
			m, key = i18n.M("mods.plan_panel.warn.other"), "other"
		}
		if !said[key] {
			said[key] = true
			out = append(out, m)
		}
	}
	return out
}

// PlanUpdate is the reply to a change made with script: the panel, and
// each result row on the page with its new state, swapped out of band.
type PlanUpdate struct {
	Search ModSearchPage
	Rows   []string
}

func (PlanUpdate) templateName() string { return "modsearch" }

// Acts are the rows' new states, each marked to swap out of band.
func (u PlanUpdate) Acts() []HitAct {
	out := make([]HitAct, len(u.Rows))
	for i, n := range u.Rows {
		out[i] = u.Search.Act(n)
		out[i].OOB = true
	}
	return out
}

func (s *server) handlePlanMods(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	name, op := strings.TrimSpace(r.PostFormValue("name")), r.PostFormValue("op")
	if name == "" || len(name) > maxPickName || (op != "add" && op != "remove") {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	// A name the portal could never give is refused before it is saved; it
	// would only come from a hand-made request.
	if op == "add" && !world.ValidModName(name) {
		s.badPlanName(w, r, u, sum)
		return
	}
	full := false
	var err error
	if op == "add" {
		err = s.store.AddModPlanName(r.Context(), sum.ID, name, u.Username, s.now())
		if errors.Is(err, store.ErrModPlanFull) {
			full, err = true, nil
		}
	} else {
		err = s.store.RemoveModPlanName(r.Context(), sum.ID, name)
	}
	if err != nil {
		s.fail(w, r, "change the mod plan", err)
		return
	}
	s.planChanged(w, r, u, sum, planChange{name: name, op: op, full: full, fromPanel: r.PostFormValue("from") == "panel"})
}

// badPlanName refuses a "+" whose name is not one a mod can have: without
// script an error page, with htmx the panel saying so.
func (s *server) badPlanName(w http.ResponseWriter, r *http.Request, u store.User, sum world.Summary) {
	msg := i18n.M("mods.plan_panel.bad_name")
	if r.Header.Get("HX-Request") == "" {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest, Detail: msg})
		return
	}
	panel, err := s.planPanel(r.Context(), sum, planReturn(sum.ID, r.PostFormValue("return")))
	if err != nil {
		s.fail(w, r, "read the mod plan", err)
		return
	}
	panel.Error, panel.Said, panel.Focus = msg, msg, "plan-heading"
	var b strings.Builder
	if err := s.render.Fragment(&b, localeOf(r), PlanUpdate{Search: ModSearchPage{World: sum, Plan: panel}}, "plan-update"); err != nil {
		s.fail(w, r, "render plan-update", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(b.String()))
}

func (s *server) handlePlanClear(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	if err := s.store.ClearModPlan(r.Context(), sum.ID); err != nil {
		s.fail(w, r, "clear the mod plan", err)
		return
	}
	s.planChanged(w, r, u, sum, planChange{op: "clear", fromPanel: true})
}

type planChange struct {
	name, op        string
	full, fromPanel bool
}

// planChanged answers a change: without script it goes back where the
// person was; with htmx it sends the panel and the page's result rows.
func (s *server) planChanged(w http.ResponseWriter, r *http.Request, u store.User, sum world.Summary, c planChange) {
	back := planReturn(sum.ID, r.PostFormValue("return"))
	if r.Header.Get("HX-Request") == "" {
		loc, _ := url.Parse(back)
		q := loc.Query()
		q.Del("full")
		if c.full {
			q.Set("full", "1")
		}
		loc.RawQuery, loc.Fragment = q.Encode(), ""
		// A row's button comes back to its row; the panel's controls and a
		// refused "+" come back to the panel (WCAG 2.4.3).
		anchor := "#" + modID(c.name, "hit")
		if c.fromPanel || c.full || c.op == "clear" {
			anchor = "#plan-panel"
		}
		http.Redirect(w, r, loc.String()+anchor, http.StatusSeeOther) //nolint:gosec // a local path under this world's mods pages
		return
	}
	panel, err := s.planPanel(r.Context(), sum, back)
	if err != nil {
		s.fail(w, r, "read the mod plan", err)
		return
	}
	up := PlanUpdate{Search: ModSearchPage{World: sum, Plan: panel, Installed: s.installedNames(sum.ID), Return: back}}
	for _, n := range r.PostForm["row"] {
		if len(up.Rows) < maxPanelRows && len(n) <= maxPickName && !slices.Contains(up.Rows, n) {
			up.Rows = append(up.Rows, n)
		}
	}
	count := len(panel.Picks)
	switch {
	case c.full:
		up.Search.Plan.Error = planFull()
		up.Search.Plan.Said = up.Search.Plan.Error
	case c.op == "add":
		up.Search.Plan.Said = i18n.M("mods.plan_panel.added", "mod", c.name, "count", count)
	case c.op == "remove":
		up.Search.Plan.Said = i18n.M("mods.plan_panel.removed", "mod", c.name, "count", count)
	default:
		up.Search.Plan.Said = i18n.M("mods.plan_panel.cleared")
	}
	switch {
	case c.full || c.op == "clear" || count == 0:
		up.Search.Plan.Focus = "plan-heading"
	case c.fromPanel:
		up.Search.Plan.Focus = panel.RemoveID(panel.Picks[0])
	default:
		up.Search.Plan.Focus = modID(c.name, "plan")
	}
	s.fragment(w, r, up, "plan-update")
}

// planFull says a "+" was refused because the plan is at its limit.
func planFull() i18n.Msg { return i18n.M("mods.plan_panel.full", "limit", store.MaxModPlanNames) }

// planReturn is where a plan change without script goes back to: the page
// it came from when that is one of this world's mods pages, else the tab.
func planReturn(id, ret string) string {
	ret = returnPath(ret)
	tab := modsPath(id)
	if ret == tab || strings.HasPrefix(ret, tab+"?") || strings.HasPrefix(ret, tab+"/search") {
		return ret
	}
	return tab
}

// installedNames names the world's mods; a folder that cannot be read
// names none, so its results offer "+" and the plan page says more.
func (s *server) installedNames(id string) map[string]bool {
	out := map[string]bool{}
	if list, err := s.worlds.Mods(id); err == nil {
		for _, m := range list {
			out[m.Name] = true
		}
	}
	return out
}
