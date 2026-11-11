//go:build game

package game

// Mod settings against the real game on each line (SPEC 9.6, 9.13, 14.6): the
// definitions run reports what the test mod's settings.lua declares; the
// game's own mod-settings.dat survives the codec byte for byte; a startup
// edit through ApplyMods takes effect at the next start; and the game's own
// rules about the file (it rewrites it at start, not at exit, resets bad
// values, and lets a save's map values win) hold. The mod is
// testdata/modsettings/fmtest_1.0.0, which logs every value it sees with the
// prefix FMSET. Run by `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	fmtestFolder = "fmtest_1.0.0"
	fmtestMod    = "fmtest"
)

// msLong is the 300-byte default of fm-sta-long, as settings.lua builds it.
var msLong = strings.Repeat("0123456789", 30)

func TestRealGameModSettingsDefinitions(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runMSDefinitions(t, g.tag, g.version)
		})
	}
}

func TestRealGameModSettingsFile(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runMSFile(t, g.tag, g.version)
		})
	}
}

func TestRealGameModSettingsApply(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runMSApply(t, g.tag, g.version)
		})
	}
}

// msWorld makes a world with no save and fmtest installed as a folder mod.
func (e *gameEnv) msWorld(t *testing.T, tag, version, label string) string {
	t.Helper()
	id := e.generate(t, "ms "+label+" "+tag, world.NewWorld{GameVersion: version})
	src := filepath.Join("testdata", "modsettings", fmtestFolder)
	dst := e.modsPath(id, fmtestFolder)
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		b, err := os.ReadFile(p) //nolint:gosec // a fixed test fixture
		if err != nil {
			return err
		}
		if rel == "info.json" {
			// Each line accepts only its own mod line.
			var info map[string]any
			if err := json.Unmarshal(b, &info); err != nil {
				return err
			}
			// 2.1.24 refuses a mod made for "2.0".
			info["factorio_version"] = version[:3]
			if b, err = json.Marshal(info); err != nil {
				return err
			}
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o600) //nolint:gosec // inside the test's data folder
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *gameEnv) msFile(id string) string { return e.modsPath(id, world.ModSettingsFile) }

func (e *gameEnv) readMSFile(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(e.msFile(id))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *gameEnv) writeMSFile(t *testing.T, id string, b []byte) {
	t.Helper()
	if err := os.WriteFile(e.msFile(id), b, 0o600); err != nil { //nolint:gosec // inside the test's data folder
		t.Fatal(err)
	}
}

// startMS starts the world and waits for the game to be in a map.
func (e *gameEnv) startMS(t *testing.T, c *Controller, id string) {
	t.Helper()
	if err := c.Start(context.Background(), id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	if !anyLineHas(currentRun(c, id), inGameLine) {
		t.Fatalf("the game's log has no %q line:\n%s", inGameLine, strings.Join(currentRun(c, id), "\n"))
	}
}

func stopMS(t *testing.T, c *Controller, id string) {
	t.Helper()
	if err := c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// fmsetValue is the text after "FMSET <scope> <name>=" on the last such line
// of the current run, and whether there was one.
func fmsetValue(lines []string, scope, name string) (string, bool) {
	key := "FMSET " + scope + " " + name + "="
	for i := len(lines) - 1; i >= 0; i-- {
		if j := strings.Index(lines[i], key); j >= 0 {
			return strings.TrimRight(lines[i][j+len(key):], " \r"), true
		}
	}
	return "", false
}

func mustFMSET(t *testing.T, lines []string, scope, name string) string {
	t.Helper()
	v, ok := fmsetValue(lines, scope, name)
	if !ok {
		t.Fatalf("the mod logged no value for %s %s:\n%s", scope, name, strings.Join(lines, "\n"))
	}
	return v
}

// numberOffset finds the 8-byte payload of a number setting in a
// mod-settings.dat, without the codec under test: after the name come a
// dictionary with one key "value", then the value's type and flag bytes. The
// type is 2 (double, every number on 1.1) or 6 (int64, on 2.x ints).
func numberOffset(t *testing.T, file []byte, name string) (off int, isDouble bool) {
	t.Helper()
	key := append([]byte{0, byte(len(name))}, name...) //nolint:gosec // short fixed names
	i := bytes.Index(file, key)
	if i < 0 || bytes.Contains(file[i+1:], key) {
		t.Fatalf("setting %s is not exactly once in the file", name)
	}
	at := i + len(key) + 13
	if at+10 > len(file) {
		t.Fatalf("setting %s is cut short", name)
	}
	switch file[at] {
	case 2:
		return at + 2, true
	case 6:
		return at + 2, false
	}
	t.Fatalf("setting %s holds a value of type %d, not a number", name, file[at])
	return 0, false
}

// readNumber reads a number setting out of the file.
func readNumber(t *testing.T, file []byte, name string) float64 {
	t.Helper()
	off, isDouble := numberOffset(t, file, name)
	u := binary.LittleEndian.Uint64(file[off:])
	if isDouble {
		return math.Float64frombits(u)
	}
	return float64(int64(u)) //nolint:gosec // two's complement read
}

// withNumber returns a copy of the file with one number setting overwritten
// in place, so every other byte is the game's.
func withNumber(t *testing.T, file []byte, name string, v int64) []byte {
	t.Helper()
	out := bytes.Clone(file)
	off, isDouble := numberOffset(t, out, name)
	if isDouble {
		binary.LittleEndian.PutUint64(out[off:], math.Float64bits(float64(v)))
	} else {
		binary.LittleEndian.PutUint64(out[off:], uint64(v)) //nolint:gosec // two's complement write
	}
	return out
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// perUserTail is the bytes from the per-player section to the end of the
// file. The game writes that section last.
func perUserTail(t *testing.T, file []byte) []byte {
	t.Helper()
	i := bytes.Index(file, append([]byte{0, byte(len("runtime-per-user"))}, "runtime-per-user"...))
	if i < 0 || i < bytes.Index(file, []byte("runtime-global")) {
		t.Fatal("the per-player section is not last in the file")
	}
	return file[i:]
}

// ---- 1: the definitions run ----

type msWant struct {
	name     string
	setting  modsettings.SettingType
	typ      modsettings.ValueType
	def      modsettings.Value
	min, max *modsettings.Value
	allowed  []modsettings.Value
	hidden   bool
	blank    bool
	trim     bool
	// unlimited marks a setting whose reported limits are recorded, not asserted.
	unlimited bool
}

func intV(n int64) modsettings.Value {
	return modsettings.Value{Type: modsettings.TypeInt, Int: n}
}
func dblV(f float64) modsettings.Value {
	return modsettings.Value{Type: modsettings.TypeDouble, Double: f}
}
func strV(s string) modsettings.Value {
	return modsettings.Value{Type: modsettings.TypeString, String: s}
}

// msWants lists what fmtest's settings.lua and settings-updates.lua declare.
func msWants() []msWant {
	var out []msWant
	for _, st := range []struct {
		setting modsettings.SettingType
		prefix  string
	}{
		{modsettings.Startup, "fm-sta-"},
		{modsettings.RuntimeGlobal, "fm-glo-"},
		{modsettings.RuntimePerUser, "fm-usr-"},
	} {
		intDefault := int64(5)
		if st.setting == modsettings.Startup {
			intDefault = 7 // settings-updates.lua
		}
		lo, hi := intV(1), intV(10)
		dlo, dhi := dblV(0.5), dblV(9.5)
		out = append(out,
			msWant{name: st.prefix + "bool", setting: st.setting, typ: modsettings.TypeBool,
				def: modsettings.Value{Type: modsettings.TypeBool, Bool: true}},
			msWant{name: st.prefix + "int", setting: st.setting, typ: modsettings.TypeInt,
				def: intV(intDefault), min: &lo, max: &hi},
			msWant{name: st.prefix + "double", setting: st.setting, typ: modsettings.TypeDouble,
				def: dblV(1.5), min: &dlo, max: &dhi},
			msWant{name: st.prefix + "string", setting: st.setting, typ: modsettings.TypeString,
				def: strV("red"), allowed: []modsettings.Value{strV("red"), strV("green"), strV("blue")}},
			msWant{name: st.prefix + "color", setting: st.setting, typ: modsettings.TypeColor,
				def: modsettings.Value{Type: modsettings.TypeColor, Color: modsettings.Color{R: 1, G: 0.5, B: 0, A: 1}}},
		)
	}
	out = append(out,
		msWant{name: "fm-sta-hidden", setting: modsettings.Startup, typ: modsettings.TypeBool,
			def: modsettings.Value{Type: modsettings.TypeBool}, hidden: true},
		msWant{name: "fm-sta-forced", setting: modsettings.Startup, typ: modsettings.TypeInt,
			def: intV(3), allowed: []modsettings.Value{intV(3)}},
		msWant{name: "fm-sta-blank", setting: modsettings.Startup, typ: modsettings.TypeString,
			def: strV("hello"), blank: true, trim: true},
		msWant{name: "fm-sta-long", setting: modsettings.Startup, typ: modsettings.TypeString,
			def: strV(msLong)},
		msWant{name: "fm-sta-unbounded", setting: modsettings.Startup, typ: modsettings.TypeInt,
			def: intV(4), unlimited: true},
		msWant{name: "fm-sta-dallowed", setting: modsettings.Startup, typ: modsettings.TypeDouble,
			def: dblV(1), allowed: []modsettings.Value{dblV(0.5), dblV(1), dblV(2)}, unlimited: true},
	)
	return out
}

// sameValue compares two values. An int the game stores as a double (every
// int on 1.1) equals the int of the same whole number.
func sameValue(a, b modsettings.Value) bool {
	if a.Type != b.Type {
		num := func(v modsettings.Value) (float64, bool) {
			switch v.Type {
			case modsettings.TypeInt:
				return float64(v.Int), true
			case modsettings.TypeDouble:
				return v.Double, true
			}
			return 0, false
		}
		x, okx := num(a)
		y, oky := num(b)
		return okx && oky && x == y
	}
	switch a.Type {
	case modsettings.TypeBool:
		return a.Bool == b.Bool
	case modsettings.TypeInt:
		return a.Int == b.Int
	case modsettings.TypeDouble:
		return a.Double == b.Double
	case modsettings.TypeString:
		return a.String == b.String
	default:
		return a.Color == b.Color
	}
}

func showPtr(v *modsettings.Value) string {
	if v == nil {
		return "absent"
	}
	return fmt.Sprintf("%+v", *v)
}

func samePtr(a, b *modsettings.Value) bool {
	if a == nil || b == nil {
		return a == b
	}
	return sameValue(*a, *b)
}

func (e *gameEnv) runMSDefinitions(t *testing.T, tag, version string) {
	c, _ := e.controller(t)
	id := e.msWorld(t, tag, version, "defs")
	begin := time.Now()
	res, err := c.ModSettingDefs(context.Background(), id)
	if err != nil {
		t.Fatalf("ModSettingDefs: %v", err)
	}
	t.Logf("TIMING %s definitions run: %s", tag, time.Since(begin).Round(100*time.Millisecond))

	byName := map[string]modsettings.Definition{}
	owned := 0
	for _, d := range res.Defs {
		byName[d.Name] = d
		if d.Mod == fmtestMod {
			owned++
		}
		if strings.HasPrefix(d.Name, "fm-") && d.Mod != fmtestMod {
			t.Errorf("%s is owned by %q, want %s", d.Name, d.Mod, fmtestMod)
		}
	}
	wants := msWants()
	if owned != len(wants) {
		t.Errorf("%d settings are owned by %s, want %d", owned, fmtestMod, len(wants))
	}
	for _, w := range wants {
		d, ok := byName[w.name]
		if !ok {
			t.Errorf("no definition for %s", w.name)
			continue
		}
		if d.Mod != fmtestMod || d.Setting != w.setting || d.Type != w.typ {
			t.Errorf("%s: owner %q, setting %q, type %q; want %q, %q, %q", w.name, d.Mod, d.Setting, d.Type, fmtestMod, w.setting, w.typ)
		}
		if d.Default == nil || !sameValue(*d.Default, w.def) {
			t.Errorf("%s: default %+v, want %+v", w.name, d.Default, w.def)
		}
		if w.unlimited {
			t.Logf("LIMITS %s %s: minimum_value %s, maximum_value %s", tag, w.name, showPtr(d.Min), showPtr(d.Max))
		} else if !samePtr(d.Min, w.min) || !samePtr(d.Max, w.max) {
			t.Errorf("%s: limits %+v..%+v, want %+v..%+v", w.name, d.Min, d.Max, w.min, w.max)
		}
		if len(d.AllowedValues) != len(w.allowed) {
			t.Errorf("%s: allowed values %+v, want %+v", w.name, d.AllowedValues, w.allowed)
		} else {
			for i := range w.allowed {
				if !sameValue(d.AllowedValues[i], w.allowed[i]) {
					t.Errorf("%s: allowed value %d is %+v, want %+v", w.name, i, d.AllowedValues[i], w.allowed[i])
				}
			}
		}
		if d.Hidden != w.hidden || d.AllowBlank != w.blank || d.AutoTrim != w.trim {
			t.Errorf("%s: hidden %v, allow_blank %v, auto_trim %v; want %v, %v, %v", w.name, d.Hidden, d.AllowBlank, d.AutoTrim, w.hidden, w.blank, w.trim)
		}
		if !d.Editable {
			t.Errorf("%s is not editable", w.name)
		}
	}
	if d := byName["fm-sta-int"]; d.Default == nil || d.Default.Int != 7 {
		t.Errorf("fm-sta-int default %+v: settings-updates.lua should have changed it to 7", d.Default)
	}
	// The mod's text is read from its locale file in English.
	if txt := res.Locale["en"]["fm-sta-int"]; txt.Name != "Startup integer" {
		t.Logf("locale text for fm-sta-int in en: %+v (the page may fill it from the .cfg instead)", txt)
	}

	// The defaults file is the game's own, so the codec reads it and gives it
	// back exactly, and its values are the definitions' defaults.
	f, err := modsettings.Decode(res.Defaults)
	if err != nil {
		t.Fatalf("Decode of the defaults file: %v", err)
	}
	if back, err := f.Encode(); err != nil || !bytes.Equal(back, res.Defaults) {
		t.Errorf("the defaults file does not round-trip: %v", err)
	}
	for _, w := range wants {
		got, ok, err := f.Get(w.setting, w.name)
		if err != nil || !ok {
			t.Errorf("defaults file: %s: found %v, %v", w.name, ok, err)
			continue
		}
		if !sameValue(got, w.def) {
			t.Errorf("defaults file: %s is %+v, want %+v", w.name, got, w.def)
		}
	}
}

// ---- 2, 5, 6 and the map half of 4: the game and the file ----

func (e *gameEnv) runMSFile(t *testing.T, tag, version string) {
	c, _ := e.controller(t)
	id := e.msWorld(t, tag, version, "file")
	step := func(name string, f func(t *testing.T)) {
		t.Helper()
		begin := time.Now()
		ok := t.Run(name, f)
		t.Logf("TIMING %s %s: %s", tag, name, time.Since(begin).Round(100*time.Millisecond))
		if !ok {
			t.FailNow() // later steps depend on this one
		}
	}
	var atStart []byte
	var startTime time.Time

	step("1 the first start writes the file with every default", func(t *testing.T) {
		if _, err := os.Stat(e.msFile(id)); err == nil {
			t.Fatal("the world had a mod-settings.dat before its first start")
		}
		e.startMS(t, c, id)
		lines := currentRun(c, id)
		for _, w := range []struct{ scope, name, want string }{
			{"startup", "fm-sta-int", "7"}, {"startup", "fm-sta-blank", `"hello"`},
			{"startup", "fm-sta-long", `"` + msLong + `"`}, {"global", "fm-glo-int", "5"},
		} {
			if got := mustFMSET(t, lines, w.scope, w.name); got != w.want {
				t.Errorf("%s logged %q, want %q", w.name, got, w.want)
			}
		}
		atStart = e.readMSFile(t, id)
		info, err := os.Stat(e.msFile(id))
		if err != nil {
			t.Fatal(err)
		}
		startTime = info.ModTime()
		if got := readNumber(t, atStart, "fm-sta-int"); got != 7 {
			t.Errorf("the file holds fm-sta-int %v, want the default 7", got)
		}
	})

	t.Run("2 a file the game wrote round-trips byte for byte", func(t *testing.T) {
		f, err := modsettings.Decode(atStart)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		back, err := f.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if !bytes.Equal(back, atStart) {
			t.Errorf("Encode gave %d bytes that differ from the game's %d", len(back), len(atStart))
		}
	}) // not a chained step: the game-only checks below do not need the codec

	step("5 the game does not rewrite the file at exit", func(t *testing.T) {
		stopMS(t, c, id)
		after := e.readMSFile(t, id)
		if !bytes.Equal(after, atStart) {
			t.Error("the file's bytes changed between InGame and the end of the stop")
		}
		info, err := os.Stat(e.msFile(id))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(startTime) {
			t.Errorf("the file's mtime moved from %v to %v across the stop", startTime, info.ModTime())
		}
	})

	step("4a on a world with a save, the save's map value wins over the file", func(t *testing.T) {
		edited := withNumber(t, atStart, "fm-glo-int", 8)
		edited = withNumber(t, edited, "fm-sta-int", 3)
		e.writeMSFile(t, id, edited)
		e.startMS(t, c, id)
		lines := currentRun(c, id)
		if !anyLineHas(lines, "FMSET load startup") {
			t.Fatalf("the mod's on_load did not run (the save was not loaded):\n%s", strings.Join(lines, "\n"))
		}
		if got := mustFMSET(t, lines, "global", "fm-glo-int"); got != "5" {
			t.Errorf("fm-glo-int is %s; the save's 5 should win over the file's 8", got)
		}
		if got := mustFMSET(t, lines, "startup", "fm-sta-int"); got != "3" {
			t.Errorf("fm-sta-int is %s; a startup value comes from the file (3)", got)
		}
		stopMS(t, c, id)
	})

	step("6 a value outside the limits or allowed values is reset at start", func(t *testing.T) {
		bad := withNumber(t, atStart, "fm-sta-int", 99)
		bad = withNumber(t, bad, "fm-sta-forced", 4)
		bad = withNumber(t, bad, "fm-sta-dallowed", 3)
		e.writeMSFile(t, id, bad)
		e.startMS(t, c, id)
		lines := currentRun(c, id)
		forced := mustFMSET(t, lines, "startup", "fm-sta-forced")
		n := mustFMSET(t, lines, "startup", "fm-sta-int")
		t.Logf("the game read fm-sta-int 99 as %s and fm-sta-forced 4 as %s", n, forced)
		if d := mustFMSET(t, lines, "startup", "fm-sta-dallowed"); d != "1" {
			t.Errorf("fm-sta-dallowed is %s; 3 is not in {0.5, 1, 2} and should be reset to the default 1", d)
		}
		if forced != "3" {
			t.Errorf("fm-sta-forced is %s; the one allowed value is 3", forced)
		}
		if v, err := strconv.Atoi(n); err != nil || v < 1 || v > 10 {
			t.Errorf("fm-sta-int is %s; it is outside 1 to 10", n)
		}
		file := e.readMSFile(t, id)
		if got := readNumber(t, file, "fm-sta-forced"); got != 3 {
			t.Errorf("the file the game rewrote holds fm-sta-forced %v, want 3", got)
		}
		if got := readNumber(t, file, "fm-sta-int"); got < 1 || got > 10 {
			t.Errorf("the file the game rewrote holds fm-sta-int %v", got)
		}
		stopMS(t, c, id)
	})

	step("4b a map value edited on a world with no save seeds the new map", func(t *testing.T) {
		// A generated world has no save until its first start, which makes
		// the map from the file as it stands.
		id := e.msWorld(t, tag, version, "seed")
		e.writeMSFile(t, id, withNumber(t, atStart, "fm-glo-int", 9))
		e.startMS(t, c, id)
		lines := currentRun(c, id)
		// The game saves the new map and loads it, so on_load runs too.
		if !logHas(c, id, `"--create"`) {
			t.Fatal("the game did not create a map")
		}
		if got := mustFMSET(t, lines, "global", "fm-glo-int"); got != "9" {
			t.Errorf("fm-glo-int is %s on the new map, want the file's 9", got)
		}
		stopMS(t, c, id)
	})
}

// ---- 3: a startup edit through ApplyMods ----

func (e *gameEnv) runMSApply(t *testing.T, tag, version string) {
	ctx := context.Background()
	c, _ := e.controller(t)
	id := e.msWorld(t, tag, version, "apply")
	e.startMS(t, c, id)
	before := e.readMSFile(t, id)
	res, err := c.ModSettingDefs(ctx, id)
	if err != nil {
		t.Fatalf("ModSettingDefs: %v", err)
	}
	defs := map[string]modsettings.Definition{}
	for _, d := range res.Defs {
		defs[d.Name] = d
	}
	defaults, err := modsettings.Decode(res.Defaults)
	if err != nil {
		t.Fatalf("Decode defaults: %v", err)
	}
	file, err := modsettings.Decode(before)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	longBefore, ok, err := file.Get(modsettings.Startup, "fm-sta-long")
	if err != nil || !ok || len(longBefore.String) != 300 {
		t.Fatalf("fm-sta-long in the game's file: %d bytes, found %v, %v", len(longBefore.String), ok, err)
	}
	edits := map[string]string{
		"fm-sta-int": "9", "fm-sta-double": "2.5", "fm-sta-blank": "  padded  ", "fm-sta-color": "0,1,0,0.5",
	}
	for name, raw := range edits {
		v, verr := modsettings.Validate(defs[name], raw)
		if verr != nil {
			t.Fatalf("Validate %s %q: %v", name, raw, verr)
		}
		if err := file.Set(modsettings.Startup, name, v, defaults); err != nil {
			t.Fatalf("Set %s: %v", name, err)
		}
	}
	edited, err := file.Encode()
	if err != nil {
		t.Fatal(err)
	}
	stopOpts := StopOptions{Countdown: 2 * time.Second}

	t.Run("a stale form is refused and the file stays", func(t *testing.T) {
		op := e.modsOp(t, id, "edit mod settings", world.ModChange{Settings: edited, SettingsSeen: sha([]byte("not the file"))})
		if err := c.ApplyMods(ctx, id, admin, op, stopOpts); !errors.Is(err, world.ErrModsChanged) {
			t.Fatalf("ApplyMods = %v, want ErrModsChanged", err)
		}
		if !bytes.Equal(e.readMSFile(t, id), before) {
			t.Error("a refused edit changed the file")
		}
		if st := c.Status(id); st.State != StateRunning {
			t.Errorf("state %s after a refused edit, want running", st.State)
		}
	})

	prev := c.Status(id).StartedAt
	op := e.modsOp(t, id, "edit mod settings", world.ModChange{Settings: edited, SettingsSeen: sha(before)})
	if err := c.ApplyMods(ctx, id, admin, op, stopOpts); err != nil {
		t.Fatalf("ApplyMods: %v", err)
	}
	e.requireBackInGame(t, c, id, version, prev)
	lines := currentRun(c, id)
	for name, want := range map[string]string{"fm-sta-int": "9", "fm-sta-double": "2.5", "fm-sta-blank": `"padded"`} {
		if got := mustFMSET(t, lines, "startup", name); got != want {
			t.Errorf("%s: the game saw %s after the edit, want %s", name, got, want)
		}
	}
	if got := mustFMSET(t, lines, "startup", "fm-sta-color"); !strings.Contains(got, "g = 1") || !strings.Contains(got, "a = 0.5") {
		t.Errorf("fm-sta-color: the game saw %s, want green at half alpha", got)
	}
	if got := mustFMSET(t, lines, "startup", "fm-sta-long"); got != `"`+msLong+`"` {
		t.Errorf("fm-sta-long: the game saw %d bytes, want the 300-byte default intact", len(got))
	}

	after := e.readMSFile(t, id)
	if !bytes.Equal(perUserTail(t, after), perUserTail(t, before)) {
		t.Error("the per-player section of the file changed")
	}
	f2, err := modsettings.Decode(after)
	if err != nil {
		t.Fatalf("Decode after: %v", err)
	}
	if got, _, _ := f2.Get(modsettings.Startup, "fm-sta-long"); got.String != msLong {
		t.Errorf("the file's fm-sta-long is %d bytes after the restart, want the 300-byte string", len(got.String))
	}
	if got, _, _ := f2.Get(modsettings.Startup, "fm-sta-int"); !sameValue(got, intV(9)) {
		t.Errorf("the file's fm-sta-int is %+v after the restart, want 9", got)
	}
	stopMS(t, c, id)
}
