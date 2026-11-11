package modsettings

import (
	"bytes"
	"encoding/json"
	"errors"
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

// luaList reads a JSON array, or the {} that Lua writes for an empty list.
type luaList[T any] []T

func (l *luaList[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '{' {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		if len(m) != 0 {
			return errDefsJSON
		}
		*l = nil
		return nil
	}
	var s []T
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*l = s
	return nil
}

type rawDefinition struct {
	Name       string                   `json:"name"`
	Mod        string                   `json:"mod"`
	Setting    string                   `json:"setting_type"`
	Type       string                   `json:"type"`
	Order      string                   `json:"order"`
	Default    json.RawMessage          `json:"default_value"`
	Min        json.RawMessage          `json:"minimum_value"`
	Max        json.RawMessage          `json:"maximum_value"`
	Allowed    luaList[json.RawMessage] `json:"allowed_values"`
	Hidden     bool                     `json:"hidden"`
	AllowBlank bool                     `json:"allow_blank"`
	AutoTrim   bool                     `json:"auto_trim"`
}

type rawDefinitions struct {
	Settings luaList[rawDefinition] `json:"settings"`
}

// ParseDefinitions parses the JSON a definitions run writes, enforcing the
// limits above and ignoring unknown fields. It keeps the file's order.
func ParseDefinitions(data []byte) (Definitions, error) {
	if len(data) > MaxDefinitionsBytes {
		return nil, errDefsSize
	}
	var raw rawDefinitions
	// The error is replaced so no text from the file reaches a log.
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errDefsJSON
	}
	if len(raw.Settings) > MaxSettings {
		return nil, errDefsCount
	}
	defs := make(Definitions, 0, len(raw.Settings))
	for i := range raw.Settings {
		d, err := convertDefinition(&raw.Settings[i])
		if err != nil {
			return nil, err
		}
		defs = append(defs, d)
	}
	return defs, nil
}

func convertDefinition(r *rawDefinition) (Definition, error) {
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
		!strings.ContainsRune(r.Name, utf8.RuneError)
	if !knownValueType(d.Type) {
		return d, nil
	}
	if len(r.Allowed) > MaxAllowedValues {
		return d, errDefsAllowed
	}
	var err error
	if d.Default, err = parseOptionalValue(d.Type, r.Default); err != nil {
		return d, err
	}
	if d.Type == TypeInt || d.Type == TypeDouble {
		if d.Min, err = parseOptionalValue(d.Type, r.Min); err != nil {
			return d, err
		}
		if d.Max, err = parseOptionalValue(d.Type, r.Max); err != nil {
			return d, err
		}
	}
	if d.Type == TypeInt || d.Type == TypeString {
		for _, a := range r.Allowed {
			v, err := parseValue(d.Type, a)
			if err != nil {
				return d, err
			}
			d.AllowedValues = append(d.AllowedValues, v)
		}
	}
	return d, nil
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
