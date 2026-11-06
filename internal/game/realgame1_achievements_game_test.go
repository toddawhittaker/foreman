//go:build game

package game

// A spot-check of mapgen.AchievementBlockers against the text in the real
// game images (SPEC 5.6). Whether the game really disables achievements is
// decided in its client, which cannot run headless, so no behaviour is
// tested: the check is that the table uses exactly the reasons the game
// lists, flags a setting that crosses each line, and flags nothing on a
// default map or the presets that make the game harder. Run by `make
// test-game`, never by `make verify`.

import (
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// imageFile reads a file from a game image with a throwaway container that
// has no network.
func imageFile(t *testing.T, tag, version, path string) string {
	t.Helper()
	out, err := exec.Command("docker", "run", "--rm", "--network", "none", "--name", "uib-realgame1-ach-"+tag, //nolint:gosec // fixed arguments of a test
		"--entrypoint", "cat", ImageRef(version), path).Output()
	if err != nil {
		t.Fatalf("read %s from %s: %v", path, ImageRef(version), err)
	}
	return string(out)
}

var reasonKey = regexp.MustCompile(`^(not-in-[a-z-]+|not-with-[a-z-]+)=`)

// gameReasons are the reasons in the game's own [achievement-progress] list.
func gameReasons(t *testing.T, cfg string) []string {
	t.Helper()
	var out []string
	in := false
	for _, l := range strings.Split(cfg, "\n") {
		switch {
		case strings.HasPrefix(l, "["):
			in = l == "[achievement-progress]"
		case in:
			if m := reasonKey.FindStringSubmatch(l); m != nil {
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return out
}

// achievementCase is a setting that crosses a line the game's text names.
type achievementCase struct {
	name       string
	mapGen     map[string]any // dotted path to value, on the default map-gen file
	settings   map[string]any // dotted path to value, on the default map settings
	reason     string         // the game's reason id
	lines      []string       // lines that list this reason
	notFlagged bool           // the case must flag nothing
}

func TestRealGameUIBAchievements(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			checkAchievements(t, g.tag, g.version)
		})
	}
}

func checkAchievements(t *testing.T, line, version string) {
	cfg := imageFile(t, line, version, "/opt/factorio/data/core/locale/en/core.cfg")
	listed := gameReasons(t, cfg)
	if len(listed) == 0 {
		t.Fatal("found no reasons in the game's [achievement-progress] list")
	}
	t.Logf("the game lists %d reasons: %s", len(listed), strings.Join(listed, ", "))

	cases := []achievementCase{
		{name: "peaceful mode", mapGen: map[string]any{"peaceful_mode": true}, reason: "not-in-peaceful-mode", lines: []string{"1.1", "2.0", "2.1"}},
		{name: "no enemies", mapGen: map[string]any{"no_enemies_mode": true}, reason: "not-in-no-enemies-mode", lines: []string{"2.0", "2.1"}},
		{name: "fewer enemy bases", mapGen: map[string]any{"autoplace_controls.enemy-base.frequency": 0.5}, reason: "not-with-lower-enemies", lines: []string{"1.1", "2.0", "2.1"}},
		{name: "smaller enemy bases", mapGen: map[string]any{"autoplace_controls.enemy-base.size": 0.5}, reason: "not-with-lower-enemies", lines: []string{"1.1", "2.0", "2.1"}},
		{name: "bigger starting area", mapGen: map[string]any{"starting_area": 2.0}, reason: "not-with-bigger-starting-area", lines: []string{"2.0", "2.1"}},
		{name: "more trees", mapGen: map[string]any{"autoplace_controls.trees.frequency": 2.0}, reason: "not-with-extra-trees", lines: []string{"2.0", "2.1"}},
		{name: "pollution off", settings: map[string]any{"pollution.enabled": false}, reason: "not-with-easier-pollution-settings", lines: []string{"2.0", "2.1"}},
		{name: "slower pollution spread", settings: map[string]any{"pollution.diffusion_ratio": 0.01}, reason: "not-with-easier-pollution-settings", lines: []string{"2.0", "2.1"}},
		{name: "evolution off", settings: map[string]any{"enemy_evolution.enabled": false}, reason: "not-with-easier-enemy-evolution-settings", lines: []string{"2.0", "2.1"}},
		{name: "slower evolution", settings: map[string]any{"enemy_evolution.time_factor": 0.000001}, reason: "not-with-easier-enemy-evolution-settings", lines: []string{"2.0", "2.1"}},
		{name: "expansion off", settings: map[string]any{"enemy_expansion.enabled": false}, reason: "not-with-easier-enemy-expansion-settings", lines: []string{"2.0", "2.1"}},
		{name: "slower expansion", settings: map[string]any{"enemy_expansion.max_expansion_cooldown": 300000}, reason: "not-with-easier-enemy-expansion-settings", lines: []string{"2.0", "2.1"}},
		// Settings the game's text does not list, or that make the game harder.
		{name: "more enemy bases", mapGen: map[string]any{"autoplace_controls.enemy-base.frequency": 3.0, "autoplace_controls.enemy-base.size": 3.0}, notFlagged: true},
		{name: "smaller starting area", mapGen: map[string]any{"starting_area": 0.5}, notFlagged: true},
		{name: "fewer trees", mapGen: map[string]any{"autoplace_controls.trees.frequency": 0.5}, notFlagged: true},
		{name: "richer ore", mapGen: map[string]any{"autoplace_controls.iron-ore.richness": 6.0, "autoplace_controls.iron-ore.frequency": 6.0}, notFlagged: true},
		{name: "a bigger map", mapGen: map[string]any{"width": 4000.0, "height": 4000.0, "seed": 5.0}, notFlagged: true},
		{name: "faster evolution", settings: map[string]any{"enemy_evolution.time_factor": 0.0001}, notFlagged: true},
		{name: "faster expansion", settings: map[string]any{"enemy_expansion.max_expansion_cooldown": 100000}, notFlagged: true},
		{name: "dearer research", settings: map[string]any{"difficulty_settings.technology_price_multiplier": 5.0}, notFlagged: true},
	}

	used := map[string]bool{}
	for _, tc := range cases {
		mapGen := fullMapGen(t, line, "default", tc.mapGen)
		settings, err := mapgen.MapSettings(version, "default", tc.settings)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, err := mapgen.AchievementBlockers(version, mapGen, settings)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		want := !tc.notFlagged && contains(tc.lines, line)
		switch {
		case want && len(got) == 0:
			t.Errorf("%s: not flagged, but the game's %s text lists %s", tc.name, line, tc.reason)
		case want:
			id := strings.ReplaceAll(strings.TrimPrefix(got[0].Message, "mapgen.achievement."), "_", "-")
			if id != tc.reason {
				t.Errorf("%s: flagged as %s, want %s", tc.name, id, tc.reason)
			}
			used[id] = true
		case len(got) != 0:
			t.Errorf("%s: flagged %+v on %s, but the game's text does not list it", tc.name, got, line)
		}
	}

	// Every reason the table gives is one the game lists, and the cases reach
	// every reason the game lists.
	var shown []string
	for id := range used {
		shown = append(shown, id)
	}
	sort.Strings(shown)
	if strings.Join(shown, ",") != strings.Join(listed, ",") {
		t.Errorf("the table gave the reasons %v; the game's %s list is %v", shown, line, listed)
	}

	// Each preset, read against the game's text. Rail world turns enemy
	// expansion off and slows evolution, and ribbon world enlarges the
	// starting area to 3, so on 2.x both disable achievements. The others
	// leave every listed setting at or above its default. 1.1 lists none of
	// these.
	flaggedOn2x := map[string][]string{
		"rail-world":   {"enemy_evolution.time_factor", "enemy_expansion.enabled"},
		"ribbon-world": {"starting_area"},
	}
	for _, preset := range mapgen.Presets(line) {
		mapGen := fullMapGen(t, line, preset, nil)
		settings, err := mapgen.MapSettings(version, preset, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := mapgen.AchievementBlockers(version, mapGen, settings)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("preset %s: %d blockers %+v", preset, len(got), got)
		var fields []string
		for _, b := range got {
			fields = append(fields, b.Field)
		}
		sort.Strings(fields)
		var want []string
		if line != "1.1" {
			want = flaggedOn2x[preset]
		}
		if strings.Join(fields, ",") != strings.Join(want, ",") {
			t.Errorf("preset %s on %s is flagged for %v; the game's text gives %v", preset, line, fields, want)
		}
	}

	// The enemy controls the game itself marks as related to the fight
	// achievements are the ones the table covers.
	if line != "1.1" {
		marked := imageFile(t, line, version, "/opt/factorio/data/space-age/prototypes/autoplace-controls.lua")
		if !strings.Contains(marked, "related_to_fight_achievements = true") {
			t.Fatal("the image's Space Age controls no longer mark any control as related to the fight achievements")
		}
		mapGen := fullMapGen(t, line, "default", map[string]any{"autoplace_controls.gleba_enemy_base.size": 0.5})
		got, err := mapgen.AchievementBlockers(version, mapGen, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Field != "autoplace_controls.gleba_enemy_base.size" {
			t.Errorf("a smaller Gleba enemy base flagged %+v", got)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
