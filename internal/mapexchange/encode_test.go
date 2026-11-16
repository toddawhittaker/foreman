package mapexchange

// Encoder tests (SPEC 5.6): every string the real game made re-encodes to
// the same payload, and Foreman's own files survive encode then decode.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

func payloadOf(t *testing.T, s string) []byte {
	t.Helper()
	c, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(s, ">>>"), "<<<"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := inflate(c)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func versionString(v [4]uint16) string {
	return fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
}

func TestEncodeReproducesGameStrings(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.version+"/"+f.name, func(t *testing.T) {
			res, err := Decode(f.exchange)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Encode(versionString(res.Version), res.MapGenSettings, res.MapSettings)
			if err != nil {
				t.Fatal(err)
			}
			want := payloadOf(t, f.exchange)
			if g := payloadOf(t, got); !bytes.Equal(g, want) {
				i := 0
				for i < len(g) && i < len(want) && g[i] == want[i] {
					i++
				}
				t.Fatalf("payload differs at byte %d of %d (got %d bytes)", i, len(want), len(g))
			}
			// The text itself differs: Go's deflate picks other matches than
			// the game's zlib at the same level. The zlib header must agree.
			g, _ := base64.StdEncoding.DecodeString(strings.Trim(got, "<>"))
			w, _ := base64.StdEncoding.DecodeString(strings.Trim(f.exchange, "<>"))
			if !bytes.Equal(g[:2], w[:2]) {
				t.Fatalf("zlib header % x, want % x", g[:2], w[:2])
			}
		})
	}
}

func TestEncodeThenDecodeRoundTrips(t *testing.T) {
	for _, version := range []string{"1.1.110", "2.0.77", "2.1.9", "2.1.24"} {
		line, _ := mapgen.Line(version)
		for _, preset := range mapgen.Presets(line) {
			t.Run(version+"/"+preset, func(t *testing.T) {
				mg := withSeed(t, mapgen.PresetMapGen(line, preset), 1234)
				ms, err := mapgen.MapSettings(version, preset, nil)
				if err != nil {
					t.Fatal(err)
				}
				s, err := Encode(version, mg, ms)
				if err != nil {
					t.Fatal(err)
				}
				res, err := Decode(s)
				if err != nil {
					t.Fatal(err)
				}
				if got := fmt.Sprintf("%d.%d.%d", res.Version[0], res.Version[1], res.Version[2]); got != version {
					t.Fatalf("version %s, want %s", got, version)
				}
				v, _ := parseFullVersion(version)
				lineIdx, _ := lineOf(v)
				wantMG, err := parseMapGenFile(mg, lineIdx)
				if err != nil {
					t.Fatal(err)
				}
				var gotMG MapGenSettings
				var gotMS, wantMS MapSettings
				mustJSON(t, res.MapGenSettings, &gotMG)
				mustJSON(t, res.MapSettings, &gotMS)
				mustJSON(t, ms, &wantMS)
				if !reflect.DeepEqual(gotMG, wantMG) {
					t.Errorf("map-gen settings changed:\n got %+v\nwant %+v", gotMG, wantMG)
				}
				if !reflect.DeepEqual(gotMS, wantMS) {
					t.Errorf("map settings changed:\n got %+v\nwant %+v", gotMS, wantMS)
				}
			})
		}
	}
}

func TestEncodeFillsTheGameDefaults(t *testing.T) {
	mg := []byte(`{"seed": 7, "width": 0, "autoplace_controls": {"water": {"frequency": 2}},
		"property_expression_names": {"control:aux:bias": 0.5, "elevation": "elevation_island"}}`)
	ms, err := mapgen.MapSettings("2.1.24", "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Encode("2.1.24", mg, ms)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	var got MapGenSettings
	mustJSON(t, res.MapGenSettings, &got)
	if got.Seed != 7 || got.Width != unlimitedSize || got.Height != unlimitedSize {
		t.Errorf("seed %d size %dx%d", got.Seed, got.Width, got.Height)
	}
	if w := got.AutoplaceControls["water"]; w != (FrequencySizeRichness{2, 1, 1}) {
		t.Errorf("water %+v, want frequency 2 and the rest 1", w)
	}
	if got.PropertyExpressionNames["control:aux:bias"] != "0.5" {
		t.Errorf("expression names %v", got.PropertyExpressionNames)
	}
	c := got.CliffSettings
	if c.Name != "cliff" || *c.Control != "" || *c.CliffSmoothing != 1 || c.CliffElevationInterval != 40 {
		t.Errorf("cliff defaults %+v", c)
	}
	if !got.DefaultEnableAllAutoplaceControls || got.StartingArea != 1 || len(got.StartingPoints) != 1 {
		t.Errorf("defaults %+v", got)
	}
}

func TestEncodeRefuses(t *testing.T) {
	ms, _ := mapgen.MapSettings("2.1.24", "default", nil)
	cases := map[string]struct {
		version string
		mg      string
		want    error
	}{
		"random seed":    {"2.1.24", `{"seed": null}`, ErrNoSeed},
		"no seed":        {"2.1.24", `{}`, ErrNoSeed},
		"unknown line":   {"3.0.1", `{"seed": 1}`, nil},
		"bad version":    {"2.1", `{"seed": 1}`, nil},
		"not json":       {"2.1.24", `{`, nil},
		"negative seed":  {"2.1.24", `{"seed": -1}`, nil},
		"bad expression": {"2.1.24", `{"seed": 1, "property_expression_names": {"a": true}}`, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Encode(c.version, []byte(c.mg), ms)
			if err == nil {
				t.Fatal("encoded")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("error %v, want %v", err, c.want)
			}
		})
	}
}

func withSeed(t *testing.T, mg []byte, seed uint32) []byte {
	t.Helper()
	var m map[string]any
	mustJSON(t, mg, &m)
	m["seed"] = seed
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustJSON(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// TestEncodePresetsMatchGame encodes each preset as Generate writes it and
// checks the game's own string for that preset decodes to the same settings.
// Autoplace controls are left aside: the game lists every control its
// prototypes have, and Foreman's file lists only those it changes. On 2.x the
// game's non-default presets carry an empty cliff name; a file that leaves
// cliff_settings out gets "cliff", as the game's own default preset shows, so
// the name is left aside there too.
func TestEncodePresetsMatchGame(t *testing.T) {
	for _, v := range []string{"1.1.110", "2.0.77", "2.1.24"} {
		line, ok := mapgen.Line(v)
		if !ok {
			t.Fatalf("no game line for %s", v)
		}
		for _, p := range mapgen.Presets(line) {
			t.Run(v+"/"+p, func(t *testing.T) {
				b, err := os.ReadFile("testdata/" + v + "/" + p + ".txt") //nolint:gosec // test fixture path from fixed lists
				if err != nil {
					t.Skip("no game string for this preset")
				}
				game, err := Decode(strings.TrimSpace(string(b)))
				if err != nil {
					t.Fatal(err)
				}
				var want MapGenSettings
				mustJSON(t, game.MapGenSettings, &want)
				ms, err := mapgen.MapSettings(v, p, nil)
				if err != nil {
					t.Fatal(err)
				}
				s, err := Encode(v, withSeed(t, mapgen.PresetMapGen(line, p), want.Seed), ms)
				if err != nil {
					t.Fatal(err)
				}
				ours, err := Decode(s)
				if err != nil {
					t.Fatal(err)
				}
				var got MapGenSettings
				mustJSON(t, ours.MapGenSettings, &got)
				want.AutoplaceControls, got.AutoplaceControls = nil, nil
				if want.CliffSettings.Name == "" {
					want.CliffSettings.Name = got.CliffSettings.Name
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("map gen settings differ\ngame %+v\nours %+v", want, got)
				}
				var wantMS, gotMS MapSettings
				mustJSON(t, game.MapSettings, &wantMS)
				mustJSON(t, ours.MapSettings, &gotMS)
				if !reflect.DeepEqual(wantMS, gotMS) {
					t.Errorf("map settings differ\ngame %+v\nours %+v", wantMS, gotMS)
				}
			})
		}
	}
}

func TestEncodeRefusesStartingPointsOutOfRange(t *testing.T) {
	mg := []byte(`{"seed":1,"starting_points":[{"x":1e12,"y":0}]}`)
	if _, err := Encode("2.1.24", mg, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "starting_points") {
		t.Fatalf("error %v, want a starting_points range error", err)
	}
}

func TestEncodeStartingPointsDoNotDrift(t *testing.T) {
	mg := []byte(`{"seed":1,"starting_points":[{"x":0.3,"y":0},{"x":0.6,"y":0},{"x":0.9,"y":0},{"x":1.2,"y":0}]}`)
	s, err := Encode("2.1.24", mg, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	var got MapGenSettings
	mustJSON(t, r.MapGenSettings, &got)
	for i, p := range got.StartingPoints {
		want := 0.3 * float64(i+1)
		if d := p.X - want; d > 1.0/512 || d < -1.0/512 {
			t.Errorf("point %d x = %v, want %v within half a step", i, p.X, want)
		}
	}
}
