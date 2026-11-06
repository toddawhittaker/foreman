//go:build golden

package mapexchange

// Golden tests: strings made by the real game (testdata/README.md) must decode
// to what the game itself says they hold (SPEC 5.6). Run with
// "make mapexchange-golden".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// infiniteSize is what the game reports for a width or height of 0 (infinite).
const infiniteSize = 2000000

type fixture struct {
	version, name string
	exchange      string
	game          struct {
		MapGen      map[string]any `json:"map_gen_settings"`
		MapSettings map[string]any `json:"map_settings"`
	}
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	txts, err := filepath.Glob(filepath.Join("testdata", "*", "*.txt"))
	if err != nil || len(txts) == 0 {
		t.Fatalf("no golden fixtures found: %v", err)
	}
	sort.Strings(txts)
	var out []fixture
	for _, p := range txts {
		f := fixture{version: filepath.Base(filepath.Dir(p)), name: strings.TrimSuffix(filepath.Base(p), ".txt")}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f.exchange = strings.TrimSpace(string(b))
		j, err := os.ReadFile(strings.TrimSuffix(p, ".txt") + ".json")
		if err != nil {
			t.Fatalf("%s has no game JSON: %v", p, err)
		}
		if err := json.Unmarshal(j, &f.game); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		out = append(out, f)
	}
	return out
}

func TestGoldenDecodeMatchesTheGame(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.version+"/"+f.name, func(t *testing.T) {
			res, err := Decode(f.exchange)
			if err != nil {
				t.Fatalf("Decode refused a string the game made: %v", err)
			}
			var want [3]uint16
			for i, part := range strings.SplitN(f.version, ".", 3) {
				n, _ := strconv.Atoi(part)
				want[i] = uint16(n)
			}
			if [3]uint16(res.Version[:3]) != want {
				t.Errorf("Version = %v, want %v.x", res.Version, want)
			}
			for _, c := range []struct {
				what    string
				got     []byte
				gameVal map[string]any
				norm    func(map[string]any, map[string]any)
			}{
				{"map-gen settings", res.MapGenSettings, f.game.MapGen, normMapGen},
				{"map settings", res.MapSettings, f.game.MapSettings, normMapSettings},
			} {
				var got map[string]any
				if err := json.Unmarshal(c.got, &got); err != nil {
					t.Fatalf("%s are not a JSON object: %v", c.what, err)
				}
				game := cloneJSON(c.gameVal)
				c.norm(got, game)
				if diff := diffJSON("", got, game); diff != "" {
					t.Errorf("%s differ from the game's own parse:\n%s", c.what, diff)
				}
			}
		})
	}
}

// A damaged or cut-short string must be refused, never decoded to something
// else (SPEC 5.6, 14.6): the checksum exists for this.
func TestGoldenDamagedStringsAreRefused(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fixtures(t) {
		if seen[f.version] {
			continue
		}
		seen[f.version] = true
		body := strings.TrimSuffix(strings.TrimPrefix(f.exchange, ">>>"), "<<<")
		mid := len(body) / 2
		flip := byte('A')
		if body[mid] == 'A' {
			flip = 'B'
		}
		for what, s := range map[string]string{
			"one character changed": ">>>" + body[:mid] + string(flip) + body[mid+1:] + "<<<",
			"cut short":             ">>>" + body[:len(body)-12] + "<<<",
			"end marker missing":    ">>>" + body,
			"start marker missing":  body + "<<<",
			"padding removed":       ">>>" + strings.TrimRight(body, "=") + "<<<",
			"extra bytes after":     ">>>" + body + "AAAA<<<",
			"empty":                 "",
			"markers only":          ">>><<<",
		} {
			// The game itself says "Wrong quote characters" for the two
			// marker cases (checked on 1.1.110 and 2.1.24).
			if what == "padding removed" && !strings.HasSuffix(body, "=") {
				continue
			}
			if res, err := Decode(s); err == nil {
				t.Errorf("%s, %s: Decode accepted it: %+v", f.version, what, res.Version)
			}
		}
	}
}

// The game accepts spaces and a newline around a pasted string, so Foreman
// does too.
func TestGoldenSurroundingWhitespaceIsAccepted(t *testing.T) {
	f := fixtures(t)[0]
	if _, err := Decode("  \n" + f.exchange + "\n\t "); err != nil {
		t.Errorf("Decode refused a string with spaces around it: %v", err)
	}
}

// What a map exchange string carries must be exactly what the game parsed:
// changing any one value in the game's answer must show up as a difference.
func TestGoldenCompareCatchesChanges(t *testing.T) {
	f := fixtures(t)[0]
	base := cloneJSON(f.game.MapSettings)
	same := cloneJSON(f.game.MapSettings)
	normMapSettings(same, base)
	if d := diffJSON("", same, base); d != "" {
		t.Fatalf("identical settings differ:\n%s", d)
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { m["pollution"].(map[string]any)["ageing"] = 99.0 },
		func(m map[string]any) { delete(m["pollution"].(map[string]any), "ageing") },
		func(m map[string]any) { m["pollution"].(map[string]any)["surprise"] = 1.0 },
		func(m map[string]any) { m["enemy_expansion"].(map[string]any)["enabled"] = "false" },
	} {
		changed := cloneJSON(f.game.MapSettings)
		mutate(changed)
		normMapSettings(changed, base)
		if diffJSON("", changed, base) == "" {
			t.Error("a changed value went unnoticed")
		}
	}
}

func TestGoldenMapGenCompareRules(t *testing.T) {
	game := func() map[string]any {
		return map[string]any{
			"width": 2000000.0, "height": 2000000.0, "seed": 4e9, "starting_area": 0.33000001311302185,
			"autoplace_controls": map[string]any{
				"iron-ore": map[string]any{"frequency": 1.0, "size": 1.0, "richness": 1.0},
				"stone":    map[string]any{"frequency": 3.0, "size": 1.0, "richness": 1.0},
			},
			"property_expression_names": map[string]any{"elevation": "x", "trees_forest_path_cutout": "1"},
			"starting_points":           []any{map[string]any{"x": 0.0, "y": 0.0}},
			"autoplace_settings":        map[string]any{},
			"cliff_settings":            map[string]any{"name": "", "control": "", "richness": 1.0},
		}
	}
	// What the files format may say differently from the game's parse.
	ok := map[string]any{
		"width": 0.0, "height": 0.0, "seed": 4e9, "starting_area": 0.33,
		"autoplace_controls":        map[string]any{"stone": map[string]any{"frequency": 3.0, "size": 1.0}},
		"property_expression_names": map[string]any{"elevation": "x", "trees_forest_path_cutout": 1.0},
		"cliff_settings":            map[string]any{"richness": 1.0},
	}
	g := game()
	normMapGen(ok, g)
	if d := diffJSON("", ok, g); d != "" {
		t.Errorf("allowed differences were reported:\n%s", d)
	}
	for name, mutate := range map[string]func(map[string]any){
		"seed":  func(m map[string]any) { m["seed"] = 4e9 - 1 },
		"width": func(m map[string]any) { m["width"] = 3000.0 },
		"control": func(m map[string]any) {
			m["autoplace_controls"].(map[string]any)["stone"].(map[string]any)["frequency"] = 2.0
		},
		"control gone": func(m map[string]any) { delete(m["autoplace_controls"].(map[string]any), "stone") },
		"extra control": func(m map[string]any) {
			m["autoplace_controls"].(map[string]any)["coal"] = map[string]any{"frequency": 2.0}
		},
		"property": func(m map[string]any) { m["property_expression_names"].(map[string]any)["elevation"] = "y" },
	} {
		got := cloneJSON(ok)
		mutate(got)
		g := game()
		normMapGen(got, g)
		if diffJSON("", got, g) == "" {
			t.Errorf("%s: a real difference went unnoticed", name)
		}
	}
}

func cloneJSON(m map[string]any) map[string]any {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

// normMapSettings brings the files format and the game's answer to one form.
// map-settings.json holds every value the game knows, so the two must match
// exactly.
func normMapSettings(got, game map[string]any) {
	emptyIsObject(got)
	emptyIsObject(game)
}

// normMapGen does the same for map-gen settings, with the differences the
// files format is allowed to have (SPEC 11.5):
//   - the game reports an infinite width or height as 2000000; the file says 0;
//   - the game reports every property_expression_names value as a string;
//   - an autoplace control left out of the file means frequency, size and
//     richness 1, and the game lists every control, so a control that is all 1
//     may be missing from the decoded side;
//   - default_enable_all_autoplace_controls is ignored when true, the game's
//     default, because the 1.1 parse leaves it out;
//   - a few keys the files format never sets (the fields in gameOnly) are
//     dropped from the game's side unless the decoded side has them.
func normMapGen(got, game map[string]any) {
	emptyIsObject(got)
	emptyIsObject(game)
	for _, m := range []map[string]any{got, game} {
		// True is the game's default; 1.1's parse leaves it out.
		if m["default_enable_all_autoplace_controls"] == true {
			delete(m, "default_enable_all_autoplace_controls")
		}
		for _, k := range []string{"width", "height"} {
			if n, ok := m[k].(float64); ok && n == 0 {
				m[k] = float64(infiniteSize)
			}
		}
		if p, ok := m["property_expression_names"].(map[string]any); ok {
			for k, v := range p {
				if n, isNum := v.(float64); isNum {
					p[k] = strconv.FormatFloat(n, 'g', -1, 64)
				}
			}
		}
		if cs, ok := m["autoplace_controls"].(map[string]any); ok {
			for name, c := range cs {
				cm, _ := c.(map[string]any)
				for _, k := range []string{"frequency", "size", "richness"} {
					if _, set := cm[k]; !set {
						cm[k] = 1.0
					}
				}
				if cm["frequency"] == 1.0 && cm["size"] == 1.0 && cm["richness"] == 1.0 {
					delete(cs, name)
				}
			}
		}
	}
	for _, k := range gameOnly {
		dropUnlessIn(game, got, strings.Split(k, "."))
	}
	for _, m := range []map[string]any{got, game} {
		dropEmpty(m)
	}
}

// gameOnly are keys of the game's parse that map-gen-settings.json does not
// set: the game fills them with defaults. A decoder that does emit one must
// have it right.
var gameOnly = []string{
	"no_enemies_mode", "starting_points", "autoplace_settings",
	"cliff_settings.name", "cliff_settings.control", "cliff_settings.cliff_smoothing",
}

func dropUnlessIn(game, got map[string]any, path []string) {
	if len(path) == 1 {
		if _, has := got[path[0]]; !has {
			delete(game, path[0])
		}
		return
	}
	g, gok := game[path[0]].(map[string]any)
	d, dok := got[path[0]].(map[string]any)
	if gok && dok {
		dropUnlessIn(g, d, path[1:])
	} else if gok {
		delete(g, path[len(path)-1]) // paths are at most two levels deep
	}
}

// emptyIsObject: the game writes an empty Lua table as [] where the file has {}.
func emptyIsObject(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if a, ok := e.([]any); ok && len(a) == 0 {
				x[k] = map[string]any{}
			}
			emptyIsObject(x[k])
		}
	case []any:
		for _, e := range x {
			emptyIsObject(e)
		}
	}
}

func dropEmpty(m map[string]any) {
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			dropEmpty(sub)
			if len(sub) == 0 {
				delete(m, k)
			}
		}
	}
}

// sameNumber: whole numbers (seeds, sizes) must match exactly; other values
// within float32 rounding, which the game uses for some of them.
func sameNumber(a, b float64) bool {
	if a == math.Trunc(a) && b == math.Trunc(b) {
		return a == b
	}
	return math.Abs(a-b) <= 1e-7*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
}

// diffJSON lists every place two decoded JSON values differ. Numbers are
// compared by sameNumber.
func diffJSON(path string, got, want any) string {
	var out bytes.Buffer
	var walk func(path string, got, want any)
	walk = func(path string, got, want any) {
		switch w := want.(type) {
		case map[string]any:
			g, ok := got.(map[string]any)
			if !ok {
				fmt.Fprintf(&out, "%s: got %v, want an object\n", path, got)
				return
			}
			keys := map[string]bool{}
			for k := range g {
				keys[k] = true
			}
			for k := range w {
				keys[k] = true
			}
			var sorted []string
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				gv, gok := g[k]
				wv, wok := w[k]
				switch {
				case !gok:
					fmt.Fprintf(&out, "%s.%s: missing from the decoded settings; the game has %v\n", path, k, wv)
				case !wok:
					fmt.Fprintf(&out, "%s.%s: decoded as %v; the game has nothing\n", path, k, gv)
				default:
					walk(path+"."+k, gv, wv)
				}
			}
		case []any:
			g, ok := got.([]any)
			if !ok || len(g) != len(w) {
				fmt.Fprintf(&out, "%s: got %v, want %v\n", path, got, want)
				return
			}
			for i := range w {
				walk(fmt.Sprintf("%s[%d]", path, i), g[i], w[i])
			}
		case float64:
			g, ok := got.(float64)
			if !ok || !sameNumber(g, w) {
				fmt.Fprintf(&out, "%s: got %v, want %v\n", path, got, want)
			}
		default:
			if !reflect.DeepEqual(got, want) {
				fmt.Fprintf(&out, "%s: got %v, want %v\n", path, got, want)
			}
		}
	}
	walk(path, got, want)
	return out.String()
}
