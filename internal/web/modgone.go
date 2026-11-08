package web

import (
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
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
		return Offer{Path: kindPlanPath(id, mods.KindUpdate, gone[0].Name), Label: i18n.M("mods.notice.update", "mod", gone[0].Name)}
	case len(gone) == 1:
		return Offer{Path: kindPlanPath(id, mods.KindRemove, gone[0].Name), Label: i18n.M("mods.flag.fix_remove", "mod", gone[0].Name)}
	case all && len(names) <= maxPlanNames:
		return Offer{Path: kindPlanPath(id, mods.KindUpdate, names...), Label: i18n.M("mods.plan.button.update", "count", len(names))}
	}
	return Offer{Path: modsPath(id), Label: i18n.M("mods.notice.review", "count", len(gone))}
}

// precheckFix is the main fix a refused start or switch offers first
// (SPEC 17.9): matching the mods to the save, since otherwise the game
// deletes content, or else updating the mods players cannot download.
func precheckFix(check world.PreCheck, id string) *Offer {
	if o := syncOffer(check, id); o != nil {
		return o
	}
	for _, p := range check.Problems {
		if p.Kind == world.ProblemModGone && len(p.Gone) > 0 {
			o := goneFix(id, p.Gone)
			return &o
		}
	}
	return nil
}
