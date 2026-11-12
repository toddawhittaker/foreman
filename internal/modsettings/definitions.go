package modsettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxNameBytes is the longest setting name Foreman will edit.
const maxNameBytes = 200

var (
	errDefsSize    = errors.New("modsettings: the definitions file is too large")
	errDefsJSON    = errors.New("modsettings: the definitions file is not valid JSON of the expected shape")
	errDefsCount   = errors.New("modsettings: the definitions file lists too many settings")
	errDefsAllowed = errors.New("modsettings: a setting lists too many allowed values")
	errDefsValue   = errors.New("modsettings: a setting has a value that does not fit its type")
)

// rawList reads a JSON array, or the {} that Lua writes for an empty list.
func rawList(b json.RawMessage) ([]json.RawMessage, error) {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || string(b) == "null":
		return nil, nil
	case b[0] == '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil || len(m) != 0 {
			return nil, errDefsValue
		}
		return nil, nil
	}
	var l []json.RawMessage
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, errDefsValue
	}
	return l, nil
}

type rawDefinition struct {
	Name       string          `json:"name"`
	Mod        string          `json:"mod"`
	Setting    string          `json:"setting_type"`
	Type       string          `json:"type"`
	Order      string          `json:"order"`
	Default    json.RawMessage `json:"default_value"`
	Min        json.RawMessage `json:"minimum_value"`
	Max        json.RawMessage `json:"maximum_value"`
	Allowed    json.RawMessage `json:"allowed_values"`
	Hidden     bool            `json:"hidden"`
	AllowBlank bool            `json:"allow_blank"`
	AutoTrim   bool            `json:"auto_trim"`
}

// ParseDefinitions parses the JSON a definitions run writes, enforcing the
// limits above and ignoring unknown fields. It keeps the file's order. The
// settings array is read one entry at a time and stops at the entry past
// MaxSettings, so a file of many tiny entries costs little. A setting whose
// values do not fit its type is returned not editable; the file as a whole
// fails only for its size, its JSON shape or its number of settings.
func ParseDefinitions(data []byte) (Definitions, error) {
	if len(data) > MaxDefinitionsBytes {
		return nil, errDefsSize
	}
	// Errors from here are replaced, so no text from the file reaches a log.
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errDefsJSON
	}
	defs := Definitions{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, errDefsJSON
		}
		if key != "settings" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, errDefsJSON
			}
			continue
		}
		if defs, err = readSettings(dec, defs); err != nil {
			return nil, err
		}
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errDefsJSON
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errDefsJSON
	}
	return defs, nil
}

// readSettings reads the value of the "settings" key: an array, or the {}
// Lua writes for an empty one (null is read as empty too).
func readSettings(dec *json.Decoder, defs Definitions) (Definitions, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, errDefsJSON
	}
	switch t {
	case nil:
		return defs, nil
	case json.Delim('{'):
		if t, err := dec.Token(); err != nil || t != json.Delim('}') {
			return nil, errDefsJSON
		}
		return defs, nil
	case json.Delim('['):
	default:
		return nil, errDefsJSON
	}
	for dec.More() {
		if len(defs) >= MaxSettings {
			return nil, errDefsCount
		}
		var r rawDefinition
		if err := dec.Decode(&r); err != nil {
			return nil, errDefsJSON
		}
		defs = append(defs, convertDefinition(&r))
	}
	if t, err := dec.Token(); err != nil || t != json.Delim(']') {
		return nil, errDefsJSON
	}
	return defs, nil
}

// convertDefinition builds a Definition. A setting of an unknown type, or one
// whose values do not fit its type, comes back not editable and without
// values.
func convertDefinition(r *rawDefinition) Definition {
	d := Definition{
		Name:       r.Name,
		Mod:        r.Mod,
		Setting:    SettingType(r.Setting),
		Type:       ValueType(r.Type),
		Order:      r.Order,
		Hidden:     r.Hidden,
		AllowBlank: r.AllowBlank,
		AutoTrim:   r.AutoTrim,
	}
	d.Editable = knownValueType(d.Type) && validSetting(d.Setting) &&
		r.Name != "" && len(r.Name) <= maxNameBytes && utf8.ValidString(r.Name) &&
		// Go's JSON reader turns invalid UTF-8 into U+FFFD.
		!strings.ContainsRune(r.Name, utf8.RuneError)
	if !knownValueType(d.Type) {
		return d
	}
	if err := fillValues(&d, r); err != nil {
		d.Default, d.Min, d.Max, d.AllowedValues = nil, nil, nil, nil
		d.Editable = false
	}
	return d
}

func fillValues(d *Definition, r *rawDefinition) error {
	allowed, err := rawList(r.Allowed)
	if err != nil {
		return err
	}
	if len(allowed) > MaxAllowedValues {
		return errDefsAllowed
	}
	if d.Default, err = parseOptionalValue(d.Type, r.Default); err != nil {
		return err
	}
	if d.Type == TypeInt || d.Type == TypeDouble {
		if d.Min, err = parseBound(d.Type, r.Min); err != nil {
			return err
		}
		if d.Max, err = parseBound(d.Type, r.Max); err != nil {
			return err
		}
	}
	if d.Type == TypeInt || d.Type == TypeDouble || d.Type == TypeString {
		for _, a := range allowed {
			v, err := parseValue(d.Type, a)
			if err != nil {
				return err
			}
			d.AllowedValues = append(d.AllowedValues, v)
		}
	}
	return nil
}

func knownValueType(t ValueType) bool {
	switch t {
	case TypeBool, TypeInt, TypeDouble, TypeString, TypeColor:
		return true
	}
	return false
}

func parseOptionalValue(t ValueType, raw json.RawMessage) (*Value, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	v, err := parseValue(t, raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// parseBound reads a minimum or maximum. The game reports an int setting's
// unset bounds as the int64 limits as Lua's JSON writer rounds them
// (-9223372036854776028 and 9223372036854776028), just past the range of an
// int64. An int bound at or past 2^63 in magnitude means no limit, so it comes
// back nil.
func parseBound(t ValueType, raw json.RawMessage) (*Value, error) {
	if t == TypeInt {
		// An exact int64 is a real bound, even at the limits.
		_, exact := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
		if f, err := parseNumber(raw); exact != nil && err == nil && math.Abs(f) >= 1<<63 {
			return nil, nil
		}
	}
	return parseOptionalValue(t, raw)
}

// parseNumber reads a JSON number into a finite float64.
func parseNumber(raw []byte) (float64, error) {
	f, err := strconv.ParseFloat(string(bytes.TrimSpace(raw)), 64)
	if err != nil || !finite(f) {
		return 0, errDefsValue
	}
	return f, nil
}

func parseValue(t ValueType, raw []byte) (Value, error) {
	v := Value{Type: t}
	switch t {
	case TypeBool:
		if err := json.Unmarshal(raw, &v.Bool); err != nil {
			return v, errDefsValue
		}
	case TypeInt:
		s := string(bytes.TrimSpace(raw))
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			v.Int = i
			return v, nil
		}
		f, err := parseNumber(raw)
		if err != nil || f != math.Trunc(f) || f >= math.MaxInt64 || f < math.MinInt64 {
			return v, errDefsValue
		}
		v.Int = int64(f)
	case TypeDouble:
		f, err := parseNumber(raw)
		if err != nil {
			return v, err
		}
		v.Double = f
	case TypeString:
		if err := json.Unmarshal(raw, &v.String); err != nil {
			return v, errDefsValue
		}
	case TypeColor:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return v, errDefsValue
		}
		comps := [4]*float64{&v.Color.R, &v.Color.G, &v.Color.B, &v.Color.A}
		for i, k := range [4]string{"r", "g", "b", "a"} {
			cr, ok := m[k]
			if !ok {
				if k == "a" {
					v.Color.A = 1
					continue
				}
				return v, errDefsValue
			}
			f, err := parseNumber(cr)
			if err != nil {
				return v, err
			}
			*comps[i] = f
		}
	default:
		return v, errDefsValue
	}
	return v, nil
}
