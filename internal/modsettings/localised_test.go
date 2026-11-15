package modsettings

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The Squeak Through way, and the rest of the game's rules (SPEC 9.6).
func TestResolveLocalised(t *testing.T) {
	secs := LocaleSections{
		"sqt-setting-name":        {"disable-setting": "Disable modification of __1__", "chests": "chests", "pair": "__2__ then __1__ (__3__)"},
		"sqt-setting-description": {"circuits": "Combinators, lamps.", "blank": ""},
		"mod-setting-name":        {"x": "<b>X</b>"},
	}
	cases := []struct {
		name, raw, want string
		ok              bool
	}{
		{"parameter from another section", `["sqt-setting-name.disable-setting",["sqt-setting-name.chests"]]`, "Disable modification of chests", true},
		{"plain key", `["sqt-setting-description.circuits"]`, "Combinators, lamps.", true},
		{"blank text", `["sqt-setting-description.blank"]`, "", true},
		{"plain string", `"Just text"`, "Just text", true},
		{"plain string parameter", `["sqt-setting-name.disable-setting","doors"]`, "Disable modification of doors", true},
		{"number parameter", `["sqt-setting-name.disable-setting",3]`, "Disable modification of 3", true},
		{"order and a missing number", `["sqt-setting-name.pair","a","b"]`, "b then a (__3__)", true},
		{"join", `["","a",["sqt-setting-name.chests"],"c"]`, "achestsc", true},
		{"fallback key", `["?",["nope.x"],["sqt-setting-name.chests"]]`, "chests", true},
		{"fallback key none", `["?",["nope.x"]]`, "", false},
		{"markup kept as text", `["mod-setting-name.x"]`, "<b>X</b>", true},
		{"missing key", `["sqt-setting-name.nope"]`, "", false},
		{"missing section", `["nope.chests"]`, "", false},
		{"missing nested key", `["sqt-setting-name.disable-setting",["sqt-setting-name.nope"]]`, "", false},
		{"key without a section", `["chests"]`, "", false},
		{"absent", ``, "", false},
		{"null", `null`, "", false},
		{"not JSON", `[`, "", false},
		{"key not a string", `[1]`, "", false},
		{"empty table", `{}`, "", true},
	}
	for _, c := range cases {
		got, ok := ResolveLocalised(json.RawMessage(c.raw), secs)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: %q %v; want %q %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestResolveLocalisedDepth(t *testing.T) {
	secs := LocaleSections{"s": {"k": "(__1__)"}}
	nest := func(n int) json.RawMessage {
		return json.RawMessage(strings.Repeat(`["s.k",`, n) + `"x"` + strings.Repeat(`]`, n))
	}
	if got, ok := ResolveLocalised(nest(MaxLocalisedDepth), secs); !ok || got != strings.Repeat("(", 20)+"x"+strings.Repeat(")", 20) {
		t.Errorf("at the limit: %q %v", got, ok)
	}
	if _, ok := ResolveLocalised(nest(MaxLocalisedDepth+1), secs); ok {
		t.Error("past the depth limit resolved")
	}
}

func TestResolveLocalisedLength(t *testing.T) {
	secs := LocaleSections{"s": {
		"at": strings.Repeat("é", MaxLocalisedChars), "over": strings.Repeat("x", MaxLocalisedChars+1),
		"twice": "__1____1__", "half": strings.Repeat("y", MaxLocalisedChars/2+1),
	}}
	if got, ok := ResolveLocalised(json.RawMessage(`["s.at"]`), secs); !ok || got != secs["s"]["at"] {
		t.Error("text at the limit did not resolve")
	}
	for _, raw := range []string{`["s.over"]`, `["s.twice",["s.half"]]`, `["",["s.half"],["s.half"]]`, `"` + strings.Repeat("z", MaxLocalisedChars+1) + `"`} {
		if _, ok := ResolveLocalised(json.RawMessage(raw), secs); ok {
			t.Errorf("%.40s: past the length limit resolved", raw)
		}
	}
	// Doubling at each level cannot blow up: it stops at the limit.
	big := LocaleSections{"s": {"d": "__1____1__"}}
	raw := strings.Repeat(`["s.d",`, MaxLocalisedDepth) + `"ab"` + strings.Repeat(`]`, MaxLocalisedDepth)
	if _, ok := ResolveLocalised(json.RawMessage(raw), big); ok {
		t.Error("doubling resolved")
	}
}

func FuzzResolveLocalised(f *testing.F) {
	f.Add([]byte(`["s.k",["s.k","x"]]`))
	secs := LocaleSections{"s": {"k": "__1__ and __1__"}}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if s, ok := ResolveLocalised(raw, secs); ok && len([]rune(s)) > MaxLocalisedChars {
			t.Errorf("%d characters", len([]rune(s)))
		}
	})
}

// A huge parameter list costs little: only the parameters the text uses are
// resolved, and the work is bounded (security review of #598).
func TestResolveLocalisedHugeParams(t *testing.T) {
	secs := LocaleSections{"s": {"big": strings.Repeat("x", 4000), "one": "__1__", "none": "plain", "all": "__1____2____3__"}}
	params := strings.Repeat(`,["s.big"]`, 150000)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	before := m.TotalAlloc
	start := time.Now()
	if got, ok := ResolveLocalised(json.RawMessage(`["s.none"`+params+`]`), secs); !ok || got != "plain" {
		t.Errorf("unused parameters: %q %v", got, ok)
	}
	if _, ok := ResolveLocalised(json.RawMessage(`["s.all"`+params+`]`), secs); ok {
		t.Error("parameters past the byte cap resolved")
	}
	if _, ok := ResolveLocalised(json.RawMessage(`["?",["nope.x"]`+strings.Repeat(`,["nope.x"]`, 150000)+`]`), secs); ok {
		t.Error("a long ? list resolved")
	}
	runtime.ReadMemStats(&m)
	// Decoding the JSON itself is most of this; resolving every parameter
	// would be about 600 MB.
	if alloc := m.TotalAlloc - before; alloc > 200<<20 {
		t.Errorf("allocated %d MB", alloc>>20)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("took %s", d)
	}
}

// Keys fall back to English one by one, as in the game.
func TestResolveLocalisedFallsBackKeyByKey(t *testing.T) {
	de := LocaleSections{"s": {"dis": "__1__ nicht ändern"}}
	en := LocaleSections{"s": {"dis": "Disable __1__", "chests": "chests"}}
	if got, ok := ResolveLocalised(json.RawMessage(`["s.dis",["s.chests"]]`), de, en); !ok || got != "chests nicht ändern" {
		t.Errorf("got %q %v", got, ok)
	}
}
