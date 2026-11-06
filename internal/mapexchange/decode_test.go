package mapexchange

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real strings, each printed by game.get_map_exchange_string() from a map
// the named game version created (SPEC 5.6). 2.1.9 is before the 2.1.13
// layout change and 2.1.24 after it.
var realStrings = []struct {
	file    string
	version [4]uint16
}{
	{"1.1.110.txt", [4]uint16{1, 1, 110, 0}},
	{"2.0.77.txt", [4]uint16{2, 0, 77, 0}},
	{"2.1.9.txt", [4]uint16{2, 1, 9, 3}},
	{"2.1.24.txt", [4]uint16{2, 1, 24, 1}},
}

func readString(t testing.TB, file string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("unittestdata", file)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDecodeRealStrings(t *testing.T) {
	for _, tc := range realStrings {
		t.Run(tc.file, func(t *testing.T) {
			res, err := Decode(readString(t, tc.file))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if res.Version != tc.version {
				t.Errorf("version = %v, want %v", res.Version, tc.version)
			}
			var mgs, ms map[string]any
			if err := json.Unmarshal(res.MapGenSettings, &mgs); err != nil {
				t.Fatalf("map-gen-settings is not JSON: %v", err)
			}
			if err := json.Unmarshal(res.MapSettings, &ms); err != nil {
				t.Fatalf("map-settings is not JSON: %v", err)
			}
			for _, k := range []string{"autoplace_controls", "seed", "cliff_settings", "starting_points"} {
				if _, ok := mgs[k]; !ok {
					t.Errorf("map-gen-settings lacks %q", k)
				}
			}
			// Difficulty lives in map-settings.json, where the game reads it.
			if _, ok := ms["difficulty_settings"]; !ok {
				t.Error("map-settings lacks difficulty_settings")
			}
		})
	}
}

// The custom strings come from maps each game version created from
// settings files with these values changed, so decoding them back must
// give the same values under the same JSON keys (SPEC 5.6).
func TestDecodeCustomStrings(t *testing.T) {
	for _, file := range []string{"1.1.110-custom.txt", "2.0.77-custom.txt", "2.1.24-custom.txt"} {
		t.Run(file, func(t *testing.T) {
			res, err := Decode(readString(t, file))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			var mgs MapGenSettings
			var ms MapSettings
			if err := json.Unmarshal(res.MapGenSettings, &mgs); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(res.MapSettings, &ms); err != nil {
				t.Fatal(err)
			}
			iron := mgs.AutoplaceControls["iron-ore"]
			checks := []struct {
				name      string
				got, want any
			}{
				{"peaceful_mode", mgs.PeacefulMode, true},
				{"starting_area", mgs.StartingArea, F32(2)},
				{"width", mgs.Width, uint32(1000)},
				{"height", mgs.Height, uint32(800)},
				{"seed", mgs.Seed, uint32(12345)},
				{"iron frequency", iron.Frequency, F32(3)},
				{"iron richness", iron.Richness, F32(0.5)},
				{"cliff richness", mgs.CliffSettings.Richness, F32(0.5)},
				{"moisture bias", mgs.PropertyExpressionNames["control:moisture:bias"], "0.2"},
				{"tech multiplier", ms.DifficultySettings.TechnologyPriceMultiplier, 4.0},
				{"pollution", *ms.Pollution.Enabled, false},
				{"time factor", *ms.EnemyEvolution.TimeFactor, 0.00001},
				{"expansion", *ms.EnemyExpansion.Enabled, false},
				{"expansion distance", *ms.EnemyExpansion.MaxExpansionDistance, uint32(9)},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
				}
			}
			if s := mgs.CliffSettings.CliffSmoothing; s != nil && *s != 1 {
				t.Errorf("cliff_smoothing = %v, want 1", *s)
			}
		})
	}
}

// rawOf returns the decompressed payload of a real string.
func rawOf(t testing.TB, file string) []byte {
	t.Helper()
	s := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(readString(t, file)), ">>>"), "<<<")
	c, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := inflate(c)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// encode wraps a payload the way the game does, fixing the checksum when asked.
func encode(t testing.TB, raw []byte, fixCRC bool) string {
	t.Helper()
	raw = bytes.Clone(raw)
	if fixCRC {
		binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
	}
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	_, _ = w.Write(raw)
	_ = w.Close()
	return ">>>" + base64.StdEncoding.EncodeToString(b.Bytes()) + "<<<"
}

func setVersion(raw []byte, v [4]uint16) {
	for i, p := range v {
		binary.LittleEndian.PutUint16(raw[i*2:], p)
	}
}

func TestDecodeAcceptsMarkersAndWhitespace(t *testing.T) {
	s := strings.TrimSpace(readString(t, "2.0.77.txt"))
	bare := strings.TrimSuffix(strings.TrimPrefix(s, ">>>"), "<<<")
	spaced := "  \n" + bare[:40] + "\n\t" + bare[40:] + " \r\n"
	for name, in := range map[string]string{"bare": bare, "spaced": spaced, "marked": s} {
		if _, err := Decode(in); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDecodeNewerPatchOnKnownLine(t *testing.T) {
	raw := rawOf(t, "2.1.24.txt")
	setVersion(raw, [4]uint16{2, 1, 999, 0})
	res, err := Decode(encode(t, raw, true))
	if err != nil {
		t.Fatalf("a newer patch with the same layout must decode: %v", err)
	}
	if res.Version != [4]uint16{2, 1, 999, 0} {
		t.Errorf("version = %v", res.Version)
	}
}

func TestDecodeRefuses(t *testing.T) {
	good := rawOf(t, "2.1.24.txt")
	big := strings.Repeat("A", MaxInputBytes+1)

	unknownLine := bytes.Clone(good)
	setVersion(unknownLine, [4]uint16{3, 0, 1, 0})
	oldLine := bytes.Clone(good)
	setVersion(oldLine, [4]uint16{1, 0, 0, 0})
	// A 2.1 string labelled with a pre-2.1.13 patch has an extra field, so
	// bytes are left over.
	wrongLayout := bytes.Clone(good)
	setVersion(wrongLayout, [4]uint16{2, 1, 9, 0})
	badCRC := bytes.Clone(good)
	badCRC[len(badCRC)-1] ^= 0xff
	trailing := append(bytes.Clone(good[:len(good)-4]), 0, 0, 0, 0, 0)
	short := bytes.Clone(good[:len(good)/2])
	short = append(short, 0, 0, 0, 0)

	cases := map[string]struct {
		in          string
		unsupported bool
	}{
		"empty":           {"", false},
		"not base64":      {">>>!!!!<<<", false},
		"not zlib":        {">>>" + base64.StdEncoding.EncodeToString([]byte("hello world")) + "<<<", false},
		"too long":        {big, false},
		"unknown line":    {encode(t, unknownLine, true), true},
		"old line":        {encode(t, oldLine, true), true},
		"wrong layout":    {encode(t, wrongLayout, true), false},
		"checksum":        {encode(t, badCRC, false), false},
		"trailing bytes":  {encode(t, trailing, true), false},
		"missing bytes":   {encode(t, short, true), false},
		"trailing base64": {encode(t, good, false) + "AAAA", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(tc.in)
			if err == nil {
				t.Fatal("Decode accepted it")
			}
			var ue *UnsupportedError
			if errors.As(err, &ue) != tc.unsupported {
				t.Errorf("UnsupportedError = %v, want %v (%v)", !tc.unsupported, tc.unsupported, err)
			}
			if len(tc.in) > 11 && strings.Contains(err.Error(), tc.in[3:11]) {
				t.Errorf("error quotes the input: %v", err)
			}
		})
	}
}

func TestDecodeRefusesNonFiniteFloat(t *testing.T) {
	raw := rawOf(t, "1.1.110.txt")
	// terrain_segmentation is the first float after the 9-byte header in 1.1.
	binary.LittleEndian.PutUint32(raw[9:], 0x7fc00000) // NaN
	if _, err := Decode(encode(t, raw, true)); err == nil {
		t.Fatal("NaN accepted")
	}
}

func TestDecodeStopsAtDecompressionLimit(t *testing.T) {
	raw := make([]byte, MaxDecompressedBytes+10)
	in := encode(t, raw, false)
	if len(in) > MaxInputBytes {
		t.Fatalf("test input too long: %d", len(in))
	}
	if _, err := Decode(in); !errors.Is(err, errTooBig) {
		t.Fatalf("err = %v, want errTooBig", err)
	}
}

func FuzzDecode(f *testing.F) {
	for _, tc := range realStrings {
		f.Add(readString(f, tc.file))
	}
	f.Add("")
	f.Add(">>><<<")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = Decode(s) // must not panic
	})
}

// FuzzDecodePayload mutates the decompressed bytes, where a fuzzer is far
// more likely to reach the field readers than through zlib.
func FuzzDecodePayload(f *testing.F) {
	for _, tc := range realStrings {
		f.Add(rawOf(f, tc.file))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) < 4 {
			return
		}
		binary.LittleEndian.PutUint32(raw[len(raw)-4:], crc32.ChecksumIEEE(raw[:len(raw)-4]))
		_, _ = decodeRaw(raw)
	})
}
