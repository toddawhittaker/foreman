package mapexchange

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
)

// unlimitedSize is what the game stores for a width or height the settings
// file gives as 0 (unlimited).
const unlimitedSize = 2000000

// ErrNoSeed means the map-gen settings leave the seed to chance (null or
// missing), so no string can name the map the game made.
var ErrNoSeed = errors.New("map-gen settings have no fixed seed")

// Encode makes the map exchange string the game would give for a map made
// from these map-gen-settings.json and map-settings.json files, as Generate
// writes them (SPEC 5.6). gameVersion is the world's exact version, such as
// "2.1.24", or with the build number the game also stores, "2.1.24.1"; the
// build is 0 when left out. Keys the map-gen file leaves out take the
// game's defaults. The string has its >>> and <<< markers.
func Encode(gameVersion string, mapGenSettings, mapSettings []byte) (string, error) {
	v, err := parseFullVersion(gameVersion)
	if err != nil {
		return "", err
	}
	line, ok := lineOf(v)
	if !ok {
		return "", &UnsupportedError{Version: v, Reason: linesReason()}
	}
	mgs, err := parseMapGenFile(mapGenSettings, line)
	if err != nil {
		return "", err
	}
	var ms MapSettings
	if err := json.Unmarshal(mapSettings, &ms); err != nil {
		return "", fmt.Errorf("map-settings.json: %w", err)
	}
	raw, err := encodeRaw(v, &mgs, &ms)
	if err != nil {
		return "", err
	}
	return wrap(raw), nil
}

// encodeRaw writes the inflated payload: version, settings, then a CRC-32.
func encodeRaw(v [4]uint16, mgs *MapGenSettings, ms *MapSettings) ([]byte, error) {
	line, ok := lineOf(v)
	if !ok {
		return nil, &UnsupportedError{Version: v, Reason: linesReason()}
	}
	w := &writer{}
	for _, p := range v {
		w.u16(p)
	}
	w.u8(0) // the byte after the version; every game string has 0
	writeMapGenSettings(w, mgs, line)
	if err := writeMapSettings(w, ms, line, v[2]); err != nil {
		return nil, err
	}
	w.u32(crc32.ChecksumIEEE(w.buf))
	return w.buf, nil
}

// wrap compresses at zlib's best level, as the game does, and adds markers.
func wrap(raw []byte) string {
	var b bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&b, zlib.BestCompression) // the level is valid, so no error
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return ">>>" + base64.StdEncoding.EncodeToString(b.Bytes()) + "<<<"
}

// parseFullVersion reads "major.minor.patch" with an optional ".build".
func parseFullVersion(s string) ([4]uint16, error) {
	var v [4]uint16
	parts := strings.Split(s, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return v, errors.New("game version is not a number like 2.1.24")
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return v, errors.New("game version is not a number like 2.1.24")
		}
		v[i] = uint16(n)
	}
	return v, nil
}

// parseMapGenFile reads a map-gen-settings.json, filling what it leaves out
// with the values the game itself fills in.
func parseMapGenFile(b []byte, line gameLine) (MapGenSettings, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return MapGenSettings{}, fmt.Errorf("map-gen-settings.json: %w", err)
	}
	s := MapGenSettings{
		DefaultEnableAllAutoplaceControls: true,
		StartingArea:                      1,
		StartingPoints:                    []Position{{}},
		CliffSettings: CliffSettings{
			Name: "cliff", CliffElevation0: 10, CliffElevationInterval: 40, Richness: 1,
		},
	}
	// Each pointer needs its own value: Unmarshal writes through them.
	if line == line11 {
		ts, water := F32(1), F32(1)
		s.TerrainSegmentation, s.Water = &ts, &water
	} else {
		noEnemies, control, smoothing := false, "", F32(1)
		s.NoEnemiesMode = &noEnemies
		s.CliffSettings.Control = &control
		s.CliffSettings.CliffSmoothing = &smoothing
	}

	seed, ok := top["seed"]
	if !ok || string(seed) == "null" {
		return MapGenSettings{}, ErrNoSeed
	}

	var err error
	if s.AutoplaceControls, err = parseControls(top["autoplace_controls"]); err != nil {
		return MapGenSettings{}, err
	}
	if s.PropertyExpressionNames, err = parseExpressionNames(top["property_expression_names"]); err != nil {
		return MapGenSettings{}, err
	}
	delete(top, "autoplace_controls")
	delete(top, "property_expression_names")
	rest, err := json.Marshal(top)
	if err != nil {
		return MapGenSettings{}, err
	}
	if err := json.Unmarshal(rest, &s); err != nil {
		return MapGenSettings{}, fmt.Errorf("map-gen-settings.json: %w", err)
	}
	if s.AutoplaceSettings == nil {
		s.AutoplaceSettings = map[string]AutoplaceSetting{}
	}
	if s.Width == 0 {
		s.Width = unlimitedSize
	}
	if s.Height == 0 {
		s.Height = unlimitedSize
	}
	return s, nil
}

// parseControls reads autoplace_controls; a value left out is 1, as the
// game reads it.
func parseControls(b json.RawMessage) (map[string]FrequencySizeRichness, error) {
	out := map[string]FrequencySizeRichness{}
	if b == nil {
		return out, nil
	}
	var in map[string]struct{ Frequency, Size, Richness *F32 }
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, fmt.Errorf("map-gen-settings.json autoplace_controls: %w", err)
	}
	for k, c := range in {
		out[k] = FrequencySizeRichness{
			Frequency: deref(c.Frequency, 1),
			Size:      deref(c.Size, 1),
			Richness:  deref(c.Richness, 1),
		}
	}
	return out, nil
}

// parseExpressionNames reads property_expression_names, whose values the
// game stores as text even when the file gives a number.
func parseExpressionNames(b json.RawMessage) (map[string]string, error) {
	out := map[string]string{}
	if b == nil {
		return out, nil
	}
	var in map[string]any
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, fmt.Errorf("map-gen-settings.json property_expression_names: %w", err)
	}
	for k, v := range in {
		switch v := v.(type) {
		case string:
			out[k] = v
		case float64:
			out[k] = strconv.FormatFloat(v, 'f', -1, 64)
		default:
			return nil, rangeError("property_expression_names")
		}
	}
	return out, nil
}
