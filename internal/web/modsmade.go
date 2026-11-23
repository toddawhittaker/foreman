package web

import (
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// justMade says the world is as step 1 left it: no mod picked and none
// installed but the built-in ones, so the "made" box after step 1 is said
// on arrival and not again after a pick, a reload or Back (SPEC 17.9).
func justMade(installed []world.InstalledMod, picks []store.ModPlanMod) bool {
	if len(picks) > 0 {
		return false
	}
	for _, m := range installed {
		if !m.BuiltIn {
			return false
		}
	}
	return true
}
