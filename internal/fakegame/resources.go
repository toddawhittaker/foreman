package fakegame

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// planetResources is what the canned count finds on a planet: a large ore,
// a small ore and an infinite resource. Some planets have fewer real
// resources than that, so the fake borrows names; only the shape matters to
// a test.
var planetResources = map[string]struct{ large, small, infinite string }{
	"nauvis":   {"iron-ore", "copper-ore", "crude-oil"},
	"vulcanus": {"vulcanus_coal", "calcite", "sulfuric-acid-geyser"},
	"gleba":    {"gleba_stone", "iron-ore", "crude-oil"},
	"fulgora":  {"scrap", "iron-ore", "crude-oil"},
	"aquilo":   {"iron-ore", "copper-ore", "aquilo_crude_oil"},
}

// normalAmount is an infinite resource's normal amount in the canned count.
const normalAmount = 300000

type cannedPatch struct {
	resource string // "large", "small" or "infinite"
	x, y     int
	amount   int64
	n        int
	box      [4]int
}

// cannedPatches all lie within 100 tiles of the centre, so they fit the
// smallest picture (radius 3, tiles -96 to 95). The large ore has a big
// patch and two small ones whose labels are within 8 tiles of each other on
// both axes, so a test can see labels overlap.
var cannedPatches = []cannedPatch{
	{"large", -50, -30, 12_345_678, 900, [4]int{-70, -55, -45, -5}},
	{"large", -40, -30, 250_000, 30, [4]int{-43, -33, -37, -27}},
	{"large", -38, -22, 90_000, 12, [4]int{-41, -24, -35, -20}},
	{"small", 30, 40, 1_500_000, 80, [4]int{26, 36, 34, 44}},
	{"small", 36, 46, 3_000, 4, [4]int{35, 45, 38, 48}},
	{"infinite", 50, -50, 900_000, 3, [4]int{46, -54, 56, -46}},
	{"infinite", -60, 60, 150_000, 1, [4]int{-62, 58, -58, 62}},
}

var cannedColors = map[string][3]int{
	"large":    {104, 132, 146},
	"small":    {184, 115, 51},
	"infinite": {194, 52, 173},
}

// writeResources is a resource count run: it writes gameenv.ResourceFile to
// the work folder, shaped as the game's script writes it, with the same
// canned patches at any radius from 3 and for any planet Foreman knows.
func writeResources(c dockertest.Container) error {
	dir := mountSource(c, gameenv.PreviewMount)
	if dir == "" {
		return fmt.Errorf("the fake game's resource container has no folder bound at %s", gameenv.PreviewMount)
	}
	r, err := strconv.Atoi(envValue(c.Spec.Env, gameenv.ResourceRadius))
	if err != nil || r < 3 {
		return fmt.Errorf("the fake game's resource container has a bad %s", gameenv.ResourceRadius)
	}
	planet := envValue(c.Spec.Env, gameenv.ResourcePlanet)
	names, ok := planetResources[planet]
	if !ok {
		return fmt.Errorf("the fake game's resource container has a bad %s", gameenv.ResourcePlanet)
	}
	nameOf := map[string]string{"large": names.large, "small": names.small, "infinite": names.infinite}

	type resource struct {
		Total    int64  `json:"total"`
		Infinite bool   `json:"infinite"`
		Normal   int    `json:"normal"`
		Color    [3]int `json:"color"`
	}
	type patch struct {
		Name   string `json:"name"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Amount int64  `json:"amount"`
		N      int    `json:"n"`
		Box    [4]int `json:"box"`
	}
	out := struct {
		Planet    string              `json:"planet"`
		Resources map[string]resource `json:"resources"`
		Patches   []patch             `json:"patches"`
	}{Planet: planet, Resources: map[string]resource{}}
	for kind, name := range nameOf {
		res := resource{Infinite: kind == "infinite", Color: cannedColors[kind]}
		if res.Infinite {
			res.Normal = normalAmount
		}
		out.Resources[name] = res
	}
	for _, p := range cannedPatches {
		name := nameOf[p.resource]
		res := out.Resources[name]
		res.Total += p.amount
		out.Resources[name] = res
		out.Patches = append(out.Patches, patch{name, p.x, p.y, p.amount, p.n, p.box})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, gameenv.ResourceFile), b, world.FileMode) //nolint:gosec // the fake's own test folder
}
