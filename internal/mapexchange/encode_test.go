package mapexchange

// Encoder tests (SPEC 5.6): every string the real game made re-encodes to
// the same payload, and Foreman's own files survive encode then decode.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
