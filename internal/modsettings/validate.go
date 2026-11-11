package modsettings

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Message ids a refused value carries. Params hold limits only.
const (
	errIDHidden  = "mods.settings.edit.err.hidden"
	errIDLocked  = "mods.settings.edit.err.locked"
	errIDBool    = "mods.settings.edit.err.bool"
	errIDNumber  = "mods.settings.edit.err.number"
	errIDWhole   = "mods.settings.edit.err.whole"
	errIDRange   = "mods.settings.edit.err.range"
	errIDAllowed = "mods.settings.edit.err.allowed"
	errIDBlank   = "mods.settings.edit.err.blank"
	errIDLong    = "mods.settings.edit.err.long"
	errIDText    = "mods.settings.edit.err.text"
	errIDColor   = "mods.settings.edit.err.color"
)

func refuse(id string, params map[string]string) (Value, *ValidationError) {
	return Value{}, &ValidationError{MessageID: id, Params: params}
}

// Validate checks a value posted from the form against its definition and
// returns it typed. A color is posted as four comma-separated components.
// A setting that is hidden, not editable or has exactly one allowed value
// takes no change.
func Validate(def Definition, raw string) (Value, *ValidationError) {
	if def.Hidden {
		return refuse(errIDHidden, nil)
	}
	if !def.Editable || len(def.AllowedValues) == 1 {
		return refuse(errIDLocked, nil)
	}
	switch def.Type {
	case TypeBool:
		switch raw {
		case "true":
			return Value{Type: TypeBool, Bool: true}, nil
		case "false":
			return Value{Type: TypeBool, Bool: false}, nil
		}
		return refuse(errIDBool, nil)
	case TypeInt:
		return validateInt(def, raw)
	case TypeDouble:
		return validateDouble(def, raw)
	case TypeString:
		return validateString(def, raw)
	case TypeColor:
		return validateColor(raw)
	}
	return refuse(errIDLocked, nil)
}

// numberText reports whether s is plain decimal number text (digits, sign,
// point, exponent): no "inf", "nan", hex or underscores.
func numberText(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("+-.0123456789eE", c) {
			return false
		}
	}
	return true
}

func rangeParams(minV, maxV *Value, f func(*Value) string) map[string]string {
	p := map[string]string{}
	if minV != nil {
		p["min"] = f(minV)
	}
	if maxV != nil {
		p["max"] = f(maxV)
	}
	if len(p) == 0 {
		return nil
	}
	return p
}

func validateInt(def Definition, raw string) (Value, *ValidationError) {
	s := strings.TrimSpace(raw)
	if !numberText(s) {
		return refuse(errIDNumber, nil)
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil || !finite(f) {
			// A syntax error, or a magnitude no float holds.
			if errors.Is(ferr, strconv.ErrRange) {
				return refuse(errIDRange, intRangeParams(def))
			}
			return refuse(errIDNumber, nil)
		}
		if f != math.Trunc(f) {
			return refuse(errIDWhole, nil)
		}
		if f >= math.MaxInt64 || f < math.MinInt64 {
			return refuse(errIDRange, intRangeParams(def))
		}
		i = int64(f)
	}
	if (def.Min != nil && i < def.Min.Int) || (def.Max != nil && i > def.Max.Int) {
		return refuse(errIDRange, intRangeParams(def))
	}
	if len(def.AllowedValues) > 0 {
		ok := false
		for _, a := range def.AllowedValues {
			if a.Int == i {
				ok = true
				break
			}
		}
		if !ok {
			return refuse(errIDAllowed, nil)
		}
	}
	return Value{Type: TypeInt, Int: i}, nil
}

func intRangeParams(def Definition) map[string]string {
	return rangeParams(def.Min, def.Max, func(v *Value) string { return strconv.FormatInt(v.Int, 10) })
}

func validateDouble(def Definition, raw string) (Value, *ValidationError) {
	s := strings.TrimSpace(raw)
	if !numberText(s) {
		return refuse(errIDNumber, nil)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || !finite(f) {
		return refuse(errIDNumber, nil)
	}
	if (def.Min != nil && f < def.Min.Double) || (def.Max != nil && f > def.Max.Double) {
		p := rangeParams(def.Min, def.Max, func(v *Value) string { return strconv.FormatFloat(v.Double, 'g', -1, 64) })
		return refuse(errIDRange, p)
	}
	return Value{Type: TypeDouble, Double: f}, nil
}

func validateString(def Definition, raw string) (Value, *ValidationError) {
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return refuse(errIDText, nil)
	}
	s := raw
	if def.AutoTrim {
		s = strings.TrimSpace(s)
	}
	if len(s) > MaxStringBytes {
		return refuse(errIDLong, map[string]string{"max": strconv.Itoa(MaxStringBytes)})
	}
	if s == "" && !def.AllowBlank {
		return refuse(errIDBlank, nil)
	}
	if len(def.AllowedValues) > 0 {
		ok := false
		for _, a := range def.AllowedValues {
			if a.String == s {
				ok = true
				break
			}
		}
		if !ok {
			return refuse(errIDAllowed, nil)
		}
	}
	return Value{Type: TypeString, String: s}, nil
}

func validateColor(raw string) (Value, *ValidationError) {
	parts := strings.Split(raw, ",")
	if len(parts) != 4 {
		return refuse(errIDColor, nil)
	}
	var c [4]float64
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if !numberText(p) {
			return refuse(errIDColor, nil)
		}
		f, err := strconv.ParseFloat(p, 64)
		if err != nil || !finite(f) || f < 0 || f > 1 {
			return refuse(errIDColor, nil)
		}
		c[i] = f
	}
	return Value{Type: TypeColor, Color: Color{R: c[0], G: c[1], B: c[2], A: c[3]}}, nil
}
