package web

import (
	"cmp"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// goneFix is the fix for mods the mod portal no longer offers, which the
// Server page's notice and the pre-start refusal both offer (SPEC 9.3,
// 7.4): one plan updating every mod to the newest release on the world's
// line; the removal of a lone mod with no release left; or else the Mods
// tab, where each mod has its own fix.
func goneFix(id string, gone []world.GoneMod) Offer {
	names := make([]string, 0, len(gone))
	for _, g := range gone {
		if g.Update != "" {
			names = append(names, g.Name)
		}
	}
	all := len(names) == len(gone)
	switch {
	case len(gone) == 1 && all:
		return Offer{Path: kindPlanPath(id, mods.KindUpdate, gone[0].Name), Label: i18n.M("mods.notice.update", "mod", i18n.Ident(goneTitle(gone[0])))}
	case len(gone) == 1:
		return Offer{Path: kindPlanPath(id, mods.KindRemove, gone[0].Name), Label: i18n.M("mods.flag.fix_remove", "mod", i18n.Ident(goneTitle(gone[0])))}
	case all:
		// A post, not a link listing the mods: the server finds the gone
		// mods again and opens their plan from a draft (SPEC 17).
		return Offer{Path: goneUpdatePath(id), Label: i18n.M("mods.plan.button.update", "count", len(names)), Post: true}
	}
	return Offer{Path: modsPath(id), Label: i18n.M("mods.notice.review", "count", len(gone))}
}

func goneUpdatePath(id string) string { return modsPath(id) + "/plan/gone" }

// handlePlanGone opens the plan that updates every mod the portal no longer
// offers on the world's line and that has an update, and only those
// (SPEC 9.3). The list is read from the portal again, never from the form.
func (s *server) handlePlanGone(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modsWorld(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	flags, err := s.planner.Availability(r.Context(), sum.ID)
	if err != nil {
		s.portalTrouble("check the mods on the portal", sum.ID, err)
	}
	f := planForm{Kind: mods.KindUpdate, Choices: map[string]mods.ChoiceKind{}, Confirmed: map[string]bool{}, Versions: map[string]string{}}
	for _, fl := range flags {
		if fl.Severity == mods.SevError && fl.Update != "" {
			f.Names = append(f.Names, fl.Name)
		}
	}
	if len(f.Names) == 0 {
		http.Redirect(w, r, modsPath(sum.ID), http.StatusSeeOther)
		return
	}
	token := s.drafts.keep("", u.ID, sum.ID, f)
	http.Redirect(w, r, draftPath(sum.ID, token), http.StatusSeeOther) //nolint:gosec // a local path under this world's mods pages
}

// goneTitle is how a gone mod is named in a label: its title, or its name.
func goneTitle(g world.GoneMod) string { return cmp.Or(g.Title, g.Name) }

// precheckFix is the main fix a refused start or switch offers first
// (SPEC 17.9): matching the mods to the save, since otherwise the game
// deletes content, then step 3 of Generate for map settings the mods no
// longer match (SPEC 7.4), or else updating the mods players cannot download.
func precheckFix(check world.PreCheck, id string) *Offer {
	if o := syncOffer(check, id); o != nil {
		return o
	}
	for _, p := range check.Problems {
		if p.Kind == world.ProblemStaleControls {
			return &Offer{Path: newMapPath(id), Label: i18n.M("server.problem.stale_controls_fix")}
		}
		if p.Kind == world.ProblemModGone && len(p.Gone) > 0 {
			o := goneFix(id, p.Gone)
			return &o
		}
	}
	return nil
}
