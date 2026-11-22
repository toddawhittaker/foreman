package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.12: a favorite's map settings files are written only for a world
// on the favorite's game line, so its seed, which is part of those files,
// goes with them: kept on the same line, not on another.
func TestAFavoritesSeedStaysOnItsGameLine(t *testing.T) {
	fixedSeed(t, 777)
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	hn.withActive(favVersion)
	favID := favoriteWithMap(t, hn, favs)

	same := idFromLocation(t, hn.postLong(PathGenerate, generateForm("name", "Same line", "game_version_typed", favVersion, "favorite", favID, "map_key", ""), c))
	if m := hn.meta(same); m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != 12345 {
		t.Errorf("same line: new_map %+v, want the favorite's seed 12345", m.NewMap)
	}
	other := idFromLocation(t, hn.postLong(PathGenerate, generateForm("name", "Other line", "game_version_typed", "2.1.24", "favorite", favID, "map_key", ""), c))
	if m := hn.meta(other); m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != 777 {
		t.Errorf("other line: new_map %+v, want a new seed, not the favorite's", m.NewMap)
	}
	if hn.config(other, world.MapGenSettingsFile).Found {
		t.Error("the favorite's map file was written for another line")
	}
}
