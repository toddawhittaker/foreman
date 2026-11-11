package modsettings

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func intDef(min, max *int64, allowed ...int64) Definition {
	d := Definition{Name: "i", Type: TypeInt, Setting: Startup, Editable: true}
	if min != nil {
		d.Min = &Value{Type: TypeInt, Int: *min}
	}
	if max != nil {
		d.Max = &Value{Type: TypeInt, Int: *max}
	}
	for _, a := range allowed {
		d.AllowedValues = append(d.AllowedValues, Value{Type: TypeInt, Int: a})
	}
	return d
}

func i64(v int64) *int64 { return &v }

func f64(v float64) *Value { return &Value{Type: TypeDouble, Double: v} }

type vcase struct {
	name string
	def  Definition
	raw  string
	want Value
	id   string
	par  map[string]string
}

func runValidate(t *testing.T, tests []vcase) {
	t.Helper()
	for _, tc := range tests {
		got, err := Validate(tc.def, tc.raw)
		if tc.id == "" {
			if err != nil || got != tc.want {
				t.Errorf("%s: %+v, %v; want %+v", tc.name, got, err, tc.want)
			}
			continue
		}
		if err == nil || err.MessageID != tc.id {
			t.Errorf("%s: err %v; want %s", tc.name, err, tc.id)
			continue
		}
		if len(err.Params) != len(tc.par) {
			t.Errorf("%s: params %v; want %v", tc.name, err.Params, tc.par)
		}
		for k, v := range tc.par {
			if err.Params[k] != v {
				t.Errorf("%s: params %v; want %v", tc.name, err.Params, tc.par)
			}
		}
	}
}

func TestValidateBool(t *testing.T) {
	d := Definition{Type: TypeBool, Editable: true}
	runValidate(t, []vcase{
		{name: "true", def: d, raw: "true", want: Value{Type: TypeBool, Bool: true}},
		{name: "false", def: d, raw: "false", want: Value{Type: TypeBool}},
		{name: "on", def: d, raw: "on", id: errIDBool},
		{name: "empty", def: d, raw: "", id: errIDBool},
		{name: "True", def: d, raw: "True", id: errIDBool},
	})
}

func TestValidateInt(t *testing.T) {
	d := intDef(i64(1), i64(10))
	r := map[string]string{"min": "1", "max": "10"}
	runValidate(t, []vcase{
		{name: "in range", def: d, raw: "5", want: Value{Type: TypeInt, Int: 5}},
		{name: "min", def: d, raw: "1", want: Value{Type: TypeInt, Int: 1}},
		{name: "max", def: d, raw: "10", want: Value{Type: TypeInt, Int: 10}},
		{name: "spaces", def: d, raw: " 7 ", want: Value{Type: TypeInt, Int: 7}},
		{name: "plus", def: d, raw: "+3", want: Value{Type: TypeInt, Int: 3}},
		{name: "whole float text", def: d, raw: "4.0", want: Value{Type: TypeInt, Int: 4}},
		{name: "exponent", def: d, raw: "1e1", want: Value{Type: TypeInt, Int: 10}},
		{name: "below", def: d, raw: "0", id: errIDRange, par: r},
		{name: "above", def: d, raw: "11", id: errIDRange, par: r},
		{name: "fraction", def: d, raw: "2.5", id: errIDWhole},
		{name: "text", def: d, raw: "abc", id: errIDNumber},
		{name: "empty", def: d, raw: "", id: errIDNumber},
		{name: "inf", def: d, raw: "inf", id: errIDNumber},
		{name: "nan", def: d, raw: "NaN", id: errIDNumber},
		{name: "hex", def: d, raw: "0x5", id: errIDNumber},
		{name: "underscore", def: d, raw: "1_0", id: errIDNumber},
		{name: "past int64", def: intDef(nil, nil), raw: "9223372036854775808", id: errIDRange},
		{name: "past float", def: intDef(nil, nil), raw: "1e999", id: errIDRange},
		{name: "float form past int64", def: intDef(nil, nil), raw: "1e19", id: errIDRange},
		{name: "int64 max", def: intDef(nil, nil), raw: "9223372036854775807", want: Value{Type: TypeInt, Int: math.MaxInt64}},
		{name: "int64 min", def: intDef(nil, nil), raw: "-9223372036854775808", want: Value{Type: TypeInt, Int: math.MinInt64}},
		{name: "only min", def: intDef(i64(3), nil), raw: "2", id: errIDRange, par: map[string]string{"min": "3"}},
		{name: "only max", def: intDef(nil, i64(3)), raw: "4", id: errIDRange, par: map[string]string{"max": "3"}},
		{name: "no limits", def: intDef(nil, nil), raw: "-1000000", want: Value{Type: TypeInt, Int: -1000000}},
		{name: "past limit no params text", def: intDef(i64(0), i64(5)), raw: "1e999", id: errIDRange, par: map[string]string{"min": "0", "max": "5"}},
	})
}

func TestValidateIntAllowed(t *testing.T) {
	d := intDef(nil, nil, 1, 5, 10)
	runValidate(t, []vcase{
		{name: "listed", def: d, raw: "5", want: Value{Type: TypeInt, Int: 5}},
		{name: "not listed", def: d, raw: "6", id: errIDAllowed},
		{name: "locked", def: intDef(nil, nil, 5), raw: "5", id: errIDLocked},
		{name: "locked other", def: intDef(nil, nil, 5), raw: "6", id: errIDLocked},
	})
}

func TestValidateDouble(t *testing.T) {
	d := Definition{Type: TypeDouble, Editable: true, Min: f64(-1), Max: f64(2.5)}
	r := map[string]string{"min": "-1", "max": "2.5"}
	runValidate(t, []vcase{
		{name: "ok", def: d, raw: "1.25", want: Value{Type: TypeDouble, Double: 1.25}},
		{name: "int text", def: d, raw: "2", want: Value{Type: TypeDouble, Double: 2}},
		{name: "min", def: d, raw: "-1", want: Value{Type: TypeDouble, Double: -1}},
		{name: "max", def: d, raw: "2.5", want: Value{Type: TypeDouble, Double: 2.5}},
		{name: "exp", def: d, raw: "1e-3", want: Value{Type: TypeDouble, Double: 0.001}},
		{name: "above", def: d, raw: "2.51", id: errIDRange, par: r},
		{name: "below", def: d, raw: "-1.01", id: errIDRange, par: r},
		{name: "nan", def: d, raw: "nan", id: errIDNumber},
		{name: "inf", def: d, raw: "+Inf", id: errIDNumber},
		{name: "huge", def: d, raw: "1e999", id: errIDNumber},
		{name: "text", def: d, raw: "x", id: errIDNumber},
		{name: "empty", def: d, raw: "  ", id: errIDNumber},
		{name: "hex float", def: d, raw: "0x1p-2", id: errIDNumber},
		{name: "unlimited", def: Definition{Type: TypeDouble, Editable: true}, raw: "-1e300", want: Value{Type: TypeDouble, Double: -1e300}},
	})
}

func TestValidateString(t *testing.T) {
	str := func(allowBlank, trim bool, allowed ...string) Definition {
		d := Definition{Type: TypeString, Editable: true, AllowBlank: allowBlank, AutoTrim: trim}
		for _, a := range allowed {
			d.AllowedValues = append(d.AllowedValues, Value{Type: TypeString, String: a})
		}
		return d
	}
	sv := func(s string) Value { return Value{Type: TypeString, String: s} }
	runValidate(t, []vcase{
		{name: "plain", def: str(false, false), raw: "red", want: sv("red")},
		{name: "keeps spaces", def: str(false, false), raw: " red ", want: sv(" red ")},
		{name: "trims", def: str(false, true), raw: " \t red \n", want: sv("red")},
		{name: "blank refused", def: str(false, false), raw: "", id: errIDBlank},
		{name: "blank after trim refused", def: str(false, true), raw: "   ", id: errIDBlank},
		{name: "blank allowed", def: str(true, false), raw: "", want: sv("")},
		{name: "blank after trim allowed", def: str(true, true), raw: "  ", want: sv("")},
		{name: "NUL", def: str(true, false), raw: "a\x00b", id: errIDText},
		{name: "invalid UTF-8", def: str(true, false), raw: "a\xffb", id: errIDText},
		{name: "unicode ok", def: str(false, false), raw: "größe 红", want: sv("größe 红")},
		{name: "max length", def: str(false, false), raw: strings.Repeat("a", MaxStringBytes), want: sv(strings.Repeat("a", MaxStringBytes))},
		{name: "too long", def: str(false, false), raw: strings.Repeat("a", MaxStringBytes+1), id: errIDLong, par: map[string]string{"max": strconv.Itoa(MaxStringBytes)}},
		{name: "trim then fits", def: str(false, true), raw: " " + strings.Repeat("a", MaxStringBytes) + " ", want: sv(strings.Repeat("a", MaxStringBytes))},
		{name: "listed", def: str(false, false, "a", "b"), raw: "b", want: sv("b")},
		{name: "not listed", def: str(false, false, "a", "b"), raw: "c", id: errIDAllowed},
		{name: "trimmed listed", def: str(false, true, "a", "b"), raw: " a ", want: sv("a")},
		{name: "locked", def: str(false, false, "a"), raw: "a", id: errIDLocked},
	})
}

func TestValidateColor(t *testing.T) {
	d := Definition{Type: TypeColor, Editable: true}
	runValidate(t, []vcase{
		{name: "ok", def: d, raw: "1,0.5,0,1", want: Value{Type: TypeColor, Color: Color{R: 1, G: 0.5, B: 0, A: 1}}},
		{name: "spaces", def: d, raw: " 0 , 0.25 ,0.5, 0.75 ", want: Value{Type: TypeColor, Color: Color{G: 0.25, B: 0.5, A: 0.75}}},
		{name: "three", def: d, raw: "1,0,0", id: errIDColor},
		{name: "five", def: d, raw: "1,0,0,1,1", id: errIDColor},
		{name: "empty", def: d, raw: "", id: errIDColor},
		{name: "above 1", def: d, raw: "1.01,0,0,1", id: errIDColor},
		{name: "negative", def: d, raw: "0,-0.1,0,1", id: errIDColor},
		{name: "text", def: d, raw: "red,0,0,1", id: errIDColor},
		{name: "nan", def: d, raw: "nan,0,0,1", id: errIDColor},
		{name: "inf", def: d, raw: "0,0,inf,1", id: errIDColor},
		{name: "empty component", def: d, raw: "0,,0,1", id: errIDColor},
	})
}

func TestValidateRefusesHiddenAndUneditable(t *testing.T) {
	runValidate(t, []vcase{
		{name: "hidden", def: Definition{Type: TypeBool, Editable: true, Hidden: true}, raw: "true", id: errIDHidden},
		{name: "hidden int", def: func() Definition { d := intDef(nil, nil); d.Hidden = true; return d }(), raw: "1", id: errIDHidden},
		{name: "not editable", def: Definition{Type: TypeBool}, raw: "true", id: errIDLocked},
		{name: "unknown type", def: Definition{Type: "item-setting", Editable: true}, raw: "x", id: errIDLocked},
	})
}

func TestValidationErrorParamsNeverHoldInput(t *testing.T) {
	const secret = "TOPSECRET"
	defs := []Definition{
		intDef(i64(1), i64(2)),
		{Type: TypeDouble, Editable: true, Min: f64(0), Max: f64(1)},
		{Type: TypeString, Editable: true},
		{Type: TypeColor, Editable: true},
		{Type: TypeBool, Editable: true},
	}
	for _, d := range defs {
		for _, raw := range []string{secret, secret + "\x00", secret + ",1,1,1", "1e999" + secret} {
			_, err := Validate(d, raw)
			if err == nil {
				continue
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.MessageID, secret) {
				t.Errorf("%s: id leaks input", d.Type)
			}
			for k, v := range err.Params {
				if strings.Contains(k, secret) || strings.Contains(v, secret) {
					t.Errorf("%s: params leak input: %v", d.Type, err.Params)
				}
			}
		}
	}
}
