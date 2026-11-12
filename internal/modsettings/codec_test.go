package modsettings

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var gameFiles = []string{"game-1.1.110.dat", "game-2.0.77.dat", "game-2.1.24.dat"}

func readGame(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test data names
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRoundTripGameFiles(t *testing.T) {
	for _, name := range gameFiles {
		t.Run(name, func(t *testing.T) {
			in := readGame(t, name)
			f, err := Decode(in)
			if err != nil {
				t.Fatal(err)
			}
			out, err := f.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(in, out) {
				t.Fatalf("round trip changed the bytes")
			}
		})
	}
}

func TestDecodeDoesNotAliasInput(t *testing.T) {
	in := readGame(t, "game-2.1.24.dat")
	f, err := Decode(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		in[i] = 0xAA
	}
	out, _ := f.Encode()
	if bytes.Equal(in, out) || !bytes.Equal(out, readGame(t, "game-2.1.24.dat")) {
		t.Fatal("File shares memory with the input")
	}
}

func TestGetGameFile(t *testing.T) {
	f, err := Decode(readGame(t, "game-2.1.24.dat"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		setting SettingType
		name    string
		want    Value
	}{
		{Startup, "fm-sta-bool", Value{Type: TypeBool, Bool: true}},
		{Startup, "fm-sta-int", Value{Type: TypeInt, Int: 7}},
		{Startup, "fm-sta-double", Value{Type: TypeDouble, Double: 1.5}},
		{Startup, "fm-sta-string", Value{Type: TypeString, String: "red"}},
		{Startup, "fm-sta-color", Value{Type: TypeColor, Color: Color{R: 1, G: 0.5, B: 0, A: 1}}},
		{RuntimeGlobal, "fm-glo-int", Value{Type: TypeInt, Int: 5}},
		{RuntimePerUser, "fm-usr-string", Value{Type: TypeString, String: "red"}},
	}
	for _, tc := range tests {
		got, found, err := f.Get(tc.setting, tc.name)
		if err != nil || !found || got != tc.want {
			t.Errorf("Get(%s, %s) = %+v, %v, %v; want %+v", tc.setting, tc.name, got, found, err, tc.want)
		}
	}
	if _, found, err := f.Get(Startup, "nope"); found || err != nil {
		t.Errorf("missing setting: found %v err %v", found, err)
	}
	if _, found, _ := f.Get(Startup, "fm-glo-int"); found {
		t.Error("a runtime-global setting was found under startup")
	}
	if _, found, _ := f.Get("bogus", "fm-sta-int"); found {
		t.Error("a bogus setting type found something")
	}
}

func TestGet11StoresIntAsDouble(t *testing.T) {
	f, err := Decode(readGame(t, "game-1.1.110.dat"))
	if err != nil {
		t.Fatal(err)
	}
	v, found, err := f.Get(Startup, "fm-sta-int")
	if err != nil || !found || v.Type != TypeDouble || v.Double != 7 {
		t.Fatalf("got %+v %v %v", v, found, err)
	}
	// An int value goes into the double node as a whole double.
	if err := f.Set(Startup, "fm-sta-int", Value{Type: TypeInt, Int: 9}, nil); err != nil {
		t.Fatal(err)
	}
	v, _, _ = f.Get(Startup, "fm-sta-int")
	if v.Double != 9 {
		t.Fatalf("got %+v", v)
	}
	if err := f.Set(Startup, "fm-sta-int", Value{Type: TypeInt, Int: 1 << 60}, nil); err == nil {
		t.Fatal("an int too big for a double was accepted")
	}
}

// diffRange returns the first and last differing offsets of a and b of the
// same length, or -1, -1.
func diffRange(a, b []byte) (first, last int) {
	first, last = -1, -1
	for i := range a {
		if a[i] != b[i] {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return first, last
}

func TestSetKeepsEveryOtherByte(t *testing.T) {
	for _, name := range gameFiles {
		t.Run(name, func(t *testing.T) {
			in := readGame(t, name)
			f, err := Decode(in)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Set(Startup, "fm-sta-bool", Value{Type: TypeBool, Bool: false}, nil); err != nil {
				t.Fatal(err)
			}
			if err := f.Set(Startup, "fm-sta-double", Value{Type: TypeDouble, Double: -2.25}, nil); err != nil {
				t.Fatal(err)
			}
			if err := f.Set(Startup, "fm-sta-color", Value{Type: TypeColor, Color: Color{R: 0, G: 0.25, B: 0.75, A: 0.5}}, nil); err != nil {
				t.Fatal(err)
			}
			out, err := f.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != len(in) {
				t.Fatalf("size changed from %d to %d", len(in), len(out))
			}
			// Everything from the runtime-global key on (the runtime
			// entries, including per-player) is byte for byte the same.
			i := bytes.Index(in, []byte("\x0eruntime-global"))
			if i < 0 || !bytes.Equal(in[i:], out[i:]) {
				t.Fatal("bytes after the startup group changed")
			}
			if !bytes.Equal(in[:headerBytes+1], out[:headerBytes+1]) {
				t.Fatal("header or flag byte changed")
			}
			g, err := Decode(out)
			if err != nil {
				t.Fatal(err)
			}
			if v, _, _ := g.Get(Startup, "fm-sta-bool"); v.Bool {
				t.Error("bool not set")
			}
			if v, _, _ := g.Get(Startup, "fm-sta-double"); v.Double != -2.25 {
				t.Errorf("double %v", v.Double)
			}
			if v, _, _ := g.Get(Startup, "fm-sta-color"); v.Color != (Color{R: 0, G: 0.25, B: 0.75, A: 0.5}) {
				t.Errorf("color %+v", v.Color)
			}
			if v, _, _ := g.Get(Startup, "fm-sta-string"); v.String != "red" {
				t.Error("an untouched setting changed")
			}
		})
	}
}

func TestSetOnlyEditedValueBytesChange(t *testing.T) {
	in := readGame(t, "game-2.1.24.dat")
	f, _ := Decode(in)
	if err := f.Set(RuntimePerUser, "fm-usr-int", Value{Type: TypeInt, Int: 9}, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := f.Encode()
	first, last := diffRange(in, out)
	if first < 0 || last-first >= 8 {
		t.Fatalf("changed bytes span %d..%d; want within one int64", first, last)
	}
}

func TestSetStringLengths(t *testing.T) {
	for _, n := range []int{0, 1, 254, 255, 256, 300, 10000} {
		s := strings.Repeat("x", n)
		f, _ := Decode(readGame(t, "game-2.1.24.dat"))
		if err := f.Set(Startup, "fm-sta-string", Value{Type: TypeString, String: s}, nil); err != nil {
			t.Fatal(err)
		}
		out, err := f.Encode()
		if err != nil {
			t.Fatal(err)
		}
		g, err := Decode(out)
		if err != nil {
			t.Fatalf("len %d: %v", n, err)
		}
		v, _, _ := g.Get(Startup, "fm-sta-string")
		if v.String != s {
			t.Fatalf("len %d: read back %d bytes", n, len(v.String))
		}
		out2, _ := g.Encode()
		if !bytes.Equal(out, out2) {
			t.Fatalf("len %d: encode not stable", n)
		}
	}
}

func TestLongStringUsesFFLengthForm(t *testing.T) {
	f, _ := Decode(readGame(t, "game-2.1.24.dat"))
	s := strings.Repeat("y", 300)
	if err := f.Set(Startup, "fm-sta-string", Value{Type: TypeString, String: s}, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := f.Encode()
	want := append([]byte{0, 0xFF, 0x2C, 0x01, 0, 0}, s...)
	if !bytes.Contains(out, want) {
		t.Fatal("300-byte string not written as 0xFF + uint32 length")
	}
	// The read back keeps a 300-byte string, and a 254-byte string stays short.
	if err := f.Set(Startup, "fm-sta-string", Value{Type: TypeString, String: strings.Repeat("z", 254)}, nil); err != nil {
		t.Fatal(err)
	}
	out, _ = f.Encode()
	if !bytes.Contains(out, append([]byte{0, 254}, strings.Repeat("z", 254)...)) {
		t.Fatal("254-byte string not written with a one byte length")
	}
}

func TestSetTypeMismatchRefused(t *testing.T) {
	in := readGame(t, "game-2.1.24.dat")
	tests := []struct {
		name string
		v    Value
	}{
		{"fm-sta-bool", Value{Type: TypeInt, Int: 1}},
		{"fm-sta-int", Value{Type: TypeDouble, Double: 1}},
		{"fm-sta-int", Value{Type: TypeString, String: "1"}},
		{"fm-sta-string", Value{Type: TypeBool, Bool: true}},
		{"fm-sta-color", Value{Type: TypeString, String: "red"}},
		{"fm-sta-double", Value{Type: TypeDouble, Double: math.NaN()}},
		{"fm-sta-double", Value{Type: TypeDouble, Double: math.Inf(1)}},
		{"fm-sta-color", Value{Type: TypeColor, Color: Color{R: math.NaN()}}},
		{"fm-sta-bool", Value{Type: "weird"}},
	}
	for _, tc := range tests {
		f, _ := Decode(in)
		if err := f.Set(Startup, tc.name, tc.v, nil); err == nil {
			t.Errorf("Set(%s, %+v) was accepted", tc.name, tc.v)
		}
		out, _ := f.Encode()
		if !bytes.Equal(in, out) {
			t.Errorf("Set(%s, %+v) failed but changed the file", tc.name, tc.v)
		}
	}
	f, _ := Decode(in)
	if err := f.Set("bogus", "x", Value{Type: TypeBool}, nil); err == nil {
		t.Error("bogus setting type accepted")
	}
	if err := f.Set(Startup, "", Value{Type: TypeBool}, nil); err == nil {
		t.Error("empty name accepted")
	}
}

func TestSetMissingEntryFromDefaults(t *testing.T) {
	defaults, err := Decode(readGame(t, "game-2.1.24.dat"))
	if err != nil {
		t.Fatal(err)
	}
	// An empty file: a root with no groups.
	empty := []byte{2, 0, 1, 0, 24, 0, 1, 0, 0, 5, 0, 0, 0, 0, 0}
	f, err := Decode(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set(Startup, "fm-sta-int", Value{Type: TypeInt, Int: 3}, defaults); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(Startup, "fm-sta-color", Value{Type: TypeColor, Color: Color{R: 1, A: 1}}, defaults); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(RuntimeGlobal, "fm-glo-bool", Value{Type: TypeBool, Bool: false}, defaults); err != nil {
		t.Fatal(err)
	}
	out, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	g, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		s    SettingType
		name string
		want Value
	}{
		{Startup, "fm-sta-int", Value{Type: TypeInt, Int: 3}},
		{Startup, "fm-sta-color", Value{Type: TypeColor, Color: Color{R: 1, A: 1}}},
		{RuntimeGlobal, "fm-glo-bool", Value{Type: TypeBool}},
	} {
		if v, found, err := g.Get(c.s, c.name); err != nil || !found || v != c.want {
			t.Errorf("%s: %+v %v %v", c.name, v, found, err)
		}
	}
	// The node is copied from defaults, so a set that matches the defaults
	// file's value reproduces its bytes for that entry.
	f2, _ := Decode(empty)
	if err := f2.Set(Startup, "fm-sta-int", Value{Type: TypeInt, Int: 7}, defaults); err != nil {
		t.Fatal(err)
	}
	out2, _ := f2.Encode()
	entry := append([]byte{0, 0x0a}, "fm-sta-int"...)
	d, _ := defaults.Encode()
	i := bytes.Index(d, entry)
	j := bytes.Index(out2, entry)
	if i < 0 || j < 0 || !bytes.Equal(d[i:i+len(entry)+19], out2[j:j+len(entry)+19]) {
		t.Fatal("entry copied from defaults differs from the defaults' bytes")
	}
	// Defaults left untouched.
	d2, _ := defaults.Encode()
	if !bytes.Equal(d, d2) {
		t.Fatal("defaults changed")
	}
	v, _, _ := defaults.Get(Startup, "fm-sta-int")
	if v.Int != 7 {
		t.Fatal("defaults value changed")
	}
}

func TestSetMissingEntryNeedsDefaults(t *testing.T) {
	empty := []byte{2, 0, 1, 0, 24, 0, 1, 0, 0, 5, 0, 0, 0, 0, 0}
	other, _ := Decode(empty)
	defaults, _ := Decode(readGame(t, "game-2.1.24.dat"))
	for name, d := range map[string]*File{"nil defaults": nil, "defaults lacking the entry": other} {
		f, _ := Decode(empty)
		if err := f.Set(RuntimePerUser, "fm-new", Value{Type: TypeBool}, d); err == nil {
			t.Errorf("%s: Set of an entry in neither file succeeded", name)
		}
		if out, _ := f.Encode(); !bytes.Equal(out, empty) {
			t.Errorf("%s: failed Set changed the file", name)
		}
	}
	// A defaults entry of another group does not count.
	f, _ := Decode(empty)
	if err := f.Set(Startup, "fm-glo-int", Value{Type: TypeInt, Int: 1}, defaults); err == nil {
		t.Error("an entry from another group was copied")
	}
}

func TestSetMissingEntryOn11CopiesDoubleNode(t *testing.T) {
	defaults, err := Decode(readGame(t, "game-1.1.110.dat"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode([]byte{1, 0, 1, 0, 110, 0, 0, 0, 0, 5, 0, 0, 0, 0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set(Startup, "fm-sta-int", Value{Type: TypeInt, Int: 3}, defaults); err != nil {
		t.Fatal(err)
	}
	out, _ := f.Encode()
	// The int is a type 2 double node, as the 1.1 game reads it, not type 6.
	want := append(append([]byte{0, 10}, "fm-sta-int"...), 5, 0, 1, 0, 0, 0, 0, 5)
	want = append(want, "value"...)
	want = append(want, 2, 0, 0, 0, 0, 0, 0, 0, 0x08, 0x40)
	if !bytes.Contains(out, want) {
		t.Fatalf("1.1 entry not a double node: %x", out)
	}
	g, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if v, found, err := g.Get(Startup, "fm-sta-int"); err != nil || !found || v.Type != TypeDouble || v.Double != 3 {
		t.Fatalf("%+v %v %v", v, found, err)
	}
}

func TestDuplicateNames(t *testing.T) {
	// Two startup entries named "x": 1 then 2, as hand-built bytes.
	entry := func(v byte) []byte {
		e := []byte{0, 1, 'x', 5, 0, 1, 0, 0, 0, 0, 5}
		e = append(e, "value"...)
		return append(e, 1, 0, v)
	}
	b := []byte{1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 1, 0, 0, 0, 0, 7}
	b = append(b, "startup"...)
	b = append(b, 5, 0, 2, 0, 0, 0)
	b = append(b, entry(0)...)
	b = append(b, entry(1)...)
	f, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	// Get reads the last copy.
	if v, _, _ := f.Get(Startup, "x"); !v.Bool {
		t.Fatal("Get did not return the last copy")
	}
	if err := f.Set(Startup, "x", Value{Type: TypeBool}, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := f.Encode()
	want := append([]byte(nil), b...)
	want[len(want)-1] = 0
	if !bytes.Equal(out, want) {
		t.Fatalf("Set did not write both copies\n got %x\nwant %x", out, want)
	}
	// A mismatch leaves both copies as they were.
	if err := f.Set(Startup, "x", Value{Type: TypeInt, Int: 1}, nil); err == nil {
		t.Fatal("mismatch accepted")
	}
	if out2, _ := f.Encode(); !bytes.Equal(out2, want) {
		t.Fatal("failed Set changed a copy")
	}
}

func TestSetIntTooBigFor11IsClear(t *testing.T) {
	f, _ := Decode(readGame(t, "game-1.1.110.dat"))
	err := f.Set(Startup, "fm-sta-int", Value{Type: TypeInt, Int: 1 << 60}, nil)
	if !errors.Is(err, errIntTooBig) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeNestedClaimedCountsAllocateLittle(t *testing.T) {
	// Every level claims as many entries as the rest of a 1 MiB file could
	// hold, then holds one: a decoder that reserves from the claim would
	// allocate gigabytes across the levels.
	const size = 1 << 20
	in := []byte{1, 0, 1, 0, 1, 0, 1, 0, 0}
	for i := 0; i < maxDepth; i++ {
		claim := uint32((size - len(in) - 1000) / minPairBytes) //nolint:gosec // under 1 MiB
		in = append(in, 5, 0)
		in = binary.LittleEndian.AppendUint32(in, claim)
		in = append(in, 1, 'k')
	}
	in = append(in, make([]byte, size-len(in))...)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := Decode(in); err == nil {
		t.Fatal("crafted file accepted")
	}
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 4*size {
		t.Fatalf("allocated %d bytes for a %d byte file", got, size)
	}
}

func TestSetMissingEntryDefaultsTypeMismatch(t *testing.T) {
	defaults, _ := Decode(readGame(t, "game-2.1.24.dat"))
	f, _ := Decode([]byte{2, 0, 1, 0, 24, 0, 1, 0, 0, 5, 0, 0, 0, 0, 0})
	before, _ := f.Encode()
	if err := f.Set(Startup, "fm-sta-int", Value{Type: TypeString, String: "x"}, defaults); err == nil {
		t.Fatal("type mismatch against the defaults node accepted")
	}
	after, _ := f.Encode()
	if !bytes.Equal(before, after) {
		t.Fatal("failed Set changed the file")
	}
}

func TestSetKeepsAnyTypeFlagsAndExtras(t *testing.T) {
	// A hand-built file: header, flag 1, root with one startup group holding
	// an unknown entry (a list), a bool entry with any-type flags set, and
	// an unknown group after it.
	b := []byte{9, 0, 8, 0, 7, 0, 6, 0, 1}
	b = append(b, 5, 1, 2, 0, 0, 0) // root dict, two groups
	b = append(b, 0, 7)             // key startup
	b = append(b, "startup"...)
	b = append(b, 5, 1, 2, 0, 0, 0) // group, two entries
	b = append(b, 0, 3)             // key abc
	b = append(b, "abc"...)
	b = append(b, 4, 1, 1, 0, 0, 0, 1, 1, 1, 1) // a list: one pair, empty key, node bool any-type true
	b = append(b, 0, 1)                         // key b
	b = append(b, "b"...)
	b = append(b, 5, 1, 1, 0, 0, 0, 0, 5) // entry dict, one key value
	b = append(b, "value"...)
	b = append(b, 1, 1, 0) // bool false, any-type 1
	b = append(b, 0, 5)    // key other
	b = append(b, "other"...)
	b = append(b, 7, 0, 1, 2, 3, 4, 5, 6, 7, 8) // uint64
	in := append([]byte(nil), b...)
	f, err := Decode(in)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := f.Encode(); !bytes.Equal(out, in) {
		t.Fatal("hand-built file did not round trip")
	}
	if err := f.Set(Startup, "b", Value{Type: TypeBool, Bool: true}, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := f.Encode()
	want := append([]byte(nil), in...)
	idx := bytes.Index(want, []byte("value"))
	want[idx+5+2] = 1
	if !bytes.Equal(out, want) {
		t.Fatalf("expected exactly one byte changed\n got %x\nwant %x", out, want)
	}
}

func TestRoundTripPreservesQuirks(t *testing.T) {
	// Legal to read but not what the game writes: an empty flag of 7, a
	// bool byte of 2, a short string in the 0xFF length form, NaN bits.
	b := []byte{1, 0, 1, 0, 1, 0, 1, 0, 3}
	b = append(b, 5, 9, 4, 0, 0, 0)
	b = append(b, 7)                                   // key: empty flag 7
	b = append(b, 1, 0, 2)                             // bool with byte 2
	b = append(b, 0, 1, 'a')                           // key a
	b = append(b, 3, 0, 0, 0xFF, 2, 0, 0, 0, 'h', 'i') // string in long form
	b = append(b, 0, 1, 'b')                           // key b
	b = append(b, 2, 0, 1, 0, 0, 0, 0, 0, 0xF8, 0x7F)  // NaN
	b = append(b, 0, 1, 'c')                           // key c
	b = append(b, 3, 4, 1)                             // string with empty flag
	in := append([]byte(nil), b...)
	f, err := Decode(in)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := f.Encode(); !bytes.Equal(out, in) {
		t.Fatalf("quirks lost\n got %x\nwant %x", out, in)
	}
}

func TestDecodeRejects(t *testing.T) {
	good := readGame(t, "game-2.1.24.dat")
	tests := map[string][]byte{
		"empty":        nil,
		"header only":  good[:9],
		"truncated":    good[:len(good)-1],
		"trailing":     append(append([]byte(nil), good...), 0),
		"root a bool":  {1, 0, 1, 0, 1, 0, 1, 0, 0, 1, 0, 1},
		"unknown type": {1, 0, 1, 0, 1, 0, 1, 0, 0, 9, 0},
		"huge count":   {1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 0xFF, 0xFF, 0xFF, 0xFF},
		"huge string":  {1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 1, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		"string cut":   {1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 1, 0, 0, 0, 0, 9, 'a'},
		"bad key long": {1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 1, 0, 0, 0, 0, 0xFF, 1},
	}
	for name, in := range tests {
		if f, err := Decode(in); err == nil || f != nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Nesting past the cap.
	deep := []byte{1, 0, 1, 0, 1, 0, 1, 0, 0}
	for i := 0; i < maxDepth+5; i++ {
		deep = append(deep, 5, 0, 1, 0, 0, 0, 1, 'k')
	}
	deep = append(deep, 0, 0)
	if _, err := Decode(deep); err == nil {
		t.Error("deeply nested file accepted")
	}
}

func TestDecodeHostileCountAllocatesLittle(t *testing.T) {
	// 4 billion entries claimed in a 15 byte file must fail without
	// allocating for them.
	in := []byte{1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 0xFF, 0xFF, 0xFF, 0xFF}
	allocs := testing.AllocsPerRun(10, func() { _, _ = Decode(in) })
	if allocs > 20 {
		t.Fatalf("%v allocations", allocs)
	}
}

func TestEncodeNilFile(t *testing.T) {
	var f *File
	if _, err := f.Encode(); err == nil {
		t.Error("nil file encoded")
	}
	if err := f.Set(Startup, "x", Value{Type: TypeBool}, nil); err == nil {
		t.Error("nil file set")
	}
	if _, found, _ := f.Get(Startup, "x"); found {
		t.Error("nil file found a setting")
	}
}

func FuzzDecode(f *testing.F) {
	for _, name := range gameFiles {
		b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test data names
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Add([]byte{1, 0, 1, 0, 1, 0, 1, 0, 0, 5, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := Decode(data)
		if err != nil {
			return
		}
		enc, err := a.Encode()
		if err != nil {
			t.Fatalf("decoded file does not encode: %v", err)
		}
		// Anything the decoder accepts is encoded to the same bytes.
		if !bytes.Equal(enc, data) {
			t.Fatalf("encode changed an accepted file")
		}
		b, err := Decode(enc)
		if err != nil {
			t.Fatalf("encoded file does not decode: %v", err)
		}
		enc2, _ := b.Encode()
		if !bytes.Equal(enc, enc2) {
			t.Fatal("decode-encode-decode is not stable")
		}
		// Reads and sets never panic on whatever was accepted.
		for _, s := range []SettingType{Startup, RuntimeGlobal, RuntimePerUser} {
			_, _, _ = a.Get(s, "x")
			_ = a.Set(s, "x", Value{Type: TypeString, String: "v"}, nil)
		}
		_, _ = a.Encode()
	})
}

// worldWithStringValue is a file whose startup fm-sta-bool holds a string, as
// after a mod changed the setting's type from string to bool (SPEC 9.6).
func worldWithStringValue() []byte {
	b := []byte{2, 0, 1, 0, 24, 0, 1, 0, 0}
	b = append(b, 5, 0, 1, 0, 0, 0) // root dict, one group
	b = append(b, 0, 7)
	b = append(b, "startup"...)
	b = append(b, 5, 0, 1, 0, 0, 0) // group, one entry
	b = append(b, 0, 11)
	b = append(b, "fm-sta-bool"...)
	b = append(b, 5, 0, 1, 0, 0, 0) // entry dict, one key
	b = append(b, 0, 5)
	b = append(b, "value"...)
	b = append(b, 3, 0, 0, 3)
	b = append(b, "old"...)
	return b
}

func TestSetMismatchedStoredValueResetsToDefaultsShape(t *testing.T) {
	defaults, err := Decode(readGame(t, "game-2.1.24.dat"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode(worldWithStringValue())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set(Startup, "fm-sta-bool", Value{Type: TypeBool, Bool: true}, defaults); err != nil {
		t.Fatal(err)
	}
	out, _ := f.Encode()
	g, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok, err := g.Get(Startup, "fm-sta-bool"); err != nil || !ok || v != (Value{Type: TypeBool, Bool: true}) {
		t.Fatalf("got %+v %v %v", v, ok, err)
	}
	// Nothing else was added: still one entry in the one group.
	if n := len(g.root.child("startup").children("fm-sta-bool")); n != 1 {
		t.Fatalf("%d entries for the setting", n)
	}
	// A value that fits neither the stored node nor the defaults' is refused.
	h, _ := Decode(worldWithStringValue())
	in, _ := h.Encode()
	if err := h.Set(Startup, "fm-sta-bool", Value{Type: TypeInt, Int: 1}, defaults); err == nil {
		t.Fatal("an int was accepted for a setting stored as string and held as bool")
	}
	if out, _ := h.Encode(); !bytes.Equal(in, out) {
		t.Fatal("a refused Set changed the file")
	}
}

func TestSetMismatchedStoredValueNeedsDefaults(t *testing.T) {
	other, _ := Decode([]byte{2, 0, 1, 0, 24, 0, 1, 0, 0, 5, 0, 0, 0, 0, 0})
	for name, d := range map[string]*File{"nil defaults": nil, "defaults lacking the entry": other} {
		f, _ := Decode(worldWithStringValue())
		before, _ := f.Encode()
		if err := f.Set(Startup, "fm-sta-bool", Value{Type: TypeBool, Bool: true}, d); err == nil {
			t.Errorf("%s: a mismatched value was replaced without a defaults node", name)
		}
		if after, _ := f.Encode(); !bytes.Equal(before, after) {
			t.Errorf("%s: failed Set changed the file", name)
		}
	}
}
