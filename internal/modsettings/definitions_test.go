package modsettings

import (
	"fmt"
	"strings"
	"testing"
)

const sampleDefs = `{"settings": [
  {"name": "fm-sta-int", "mod": "fmtest", "setting_type": "startup",
   "type": "int-setting", "order": "b", "default_value": 5,
   "minimum_value": 1, "maximum_value": 10, "allowed_values": [1, 5],
   "hidden": false, "allow_blank": false, "auto_trim": false, "future_field": {"x": 1}},
  {"name": "fm-glo-color", "mod": "fmtest", "setting_type": "runtime-global",
   "type": "color-setting", "default_value": {"r":1,"g":0.5,"b":0,"a":1}},
  {"name": "fm-usr-str", "mod": "fmtest", "setting_type": "runtime-per-user",
   "type": "string-setting", "default_value": "red", "allow_blank": true,
   "auto_trim": true, "allowed_values": {}},
  {"name": "fm-sta-double", "mod": "fmtest", "setting_type": "startup",
   "type": "double-setting", "default_value": 1.5, "maximum_value": 2.5},
  {"name": "fm-sta-bool", "mod": "fmtest", "setting_type": "startup",
   "type": "bool-setting", "default_value": true, "hidden": true}
]}`

func TestParseDefinitions(t *testing.T) {
	defs, err := ParseDefinitions([]byte(sampleDefs))
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 5 {
		t.Fatalf("got %d definitions", len(defs))
	}
	i := defs[0]
	if i.Name != "fm-sta-int" || i.Mod != "fmtest" || i.Setting != Startup || i.Type != TypeInt || i.Order != "b" || !i.Editable {
		t.Errorf("int: %+v", i)
	}
	if i.Default == nil || i.Default.Int != 5 || i.Min == nil || i.Min.Int != 1 || i.Max == nil || i.Max.Int != 10 {
		t.Errorf("int limits: %+v", i)
	}
	if len(i.AllowedValues) != 2 || i.AllowedValues[1] != (Value{Type: TypeInt, Int: 5}) {
		t.Errorf("allowed: %+v", i.AllowedValues)
	}
	c := defs[1]
	if c.Default == nil || c.Default.Color != (Color{R: 1, G: 0.5, B: 0, A: 1}) || c.Min != nil || c.Max != nil {
		t.Errorf("color: %+v", c)
	}
	s := defs[2]
	if !s.AllowBlank || !s.AutoTrim || len(s.AllowedValues) != 0 || s.Default.String != "red" || s.Min != nil {
		t.Errorf("string: %+v", s)
	}
	d := defs[3]
	if d.Default.Double != 1.5 || d.Min != nil || d.Max.Double != 2.5 {
		t.Errorf("double: %+v", d)
	}
	if b := defs[4]; !b.Hidden || !b.Default.Bool || b.Default.Type != TypeBool {
		t.Errorf("bool: %+v", b)
	}
}

func TestParseDefinitionsKeepsOrder(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"settings":[`)
	for i := 0; i < 50; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"name":"s%02d","setting_type":"startup","type":"bool-setting"}`, 49-i)
	}
	sb.WriteString(`]}`)
	defs, err := ParseDefinitions([]byte(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range defs {
		if d.Name != fmt.Sprintf("s%02d", 49-i) {
			t.Fatalf("order changed at %d: %s", i, d.Name)
		}
	}
}

func TestParseDefinitionsEmpty(t *testing.T) {
	for _, in := range []string{`{}`, `{"settings":[]}`, `{"settings":{}}`, ` {"settings": null} `} {
		defs, err := ParseDefinitions([]byte(in))
		if err != nil || len(defs) != 0 {
			t.Errorf("%q: %v %v", in, defs, err)
		}
	}
}

func TestParseDefinitionsEditable(t *testing.T) {
	long := strings.Repeat("a", maxNameBytes+1)
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"ok", `{"name":"a","setting_type":"startup","type":"int-setting"}`, true},
		{"200 bytes", `{"name":"` + strings.Repeat("a", maxNameBytes) + `","setting_type":"startup","type":"int-setting"}`, true},
		{"201 bytes", `{"name":"` + long + `","setting_type":"startup","type":"int-setting"}`, false},
		{"unknown type", `{"name":"a","setting_type":"startup","type":"item-setting","default_value":{"x":[1]}}`, false},
		{"no type", `{"name":"a","setting_type":"startup"}`, false},
		{"unknown setting type", `{"name":"a","setting_type":"map","type":"int-setting"}`, false},
		{"empty name", `{"name":"","setting_type":"startup","type":"int-setting"}`, false},
		{"replacement char", `{"name":"a\ufffdb","setting_type":"startup","type":"int-setting"}`, false},
		{"lone surrogate", `{"name":"a\ud800b","setting_type":"startup","type":"int-setting"}`, false},
		{"invalid utf8 bytes", "{\"name\":\"a\xffb\",\"setting_type\":\"startup\",\"type\":\"int-setting\"}", false},
		{"non-ASCII ok", `{"name":"größe","setting_type":"startup","type":"int-setting"}`, true},
	}
	for _, tc := range tests {
		defs, err := ParseDefinitions([]byte(`{"settings":[` + tc.json + `]}`))
		if err != nil || len(defs) != 1 {
			t.Errorf("%s: %v %v", tc.name, defs, err)
			continue
		}
		if defs[0].Editable != tc.want {
			t.Errorf("%s: Editable = %v", tc.name, defs[0].Editable)
		}
	}
}

func TestParseDefinitionsRefuses(t *testing.T) {
	one := func(body string) string { return `{"settings":[{"name":"a","setting_type":"startup",` + body + `}]}` }
	tests := map[string]string{
		"not json":         `{"settings":`,
		"array root":       `[]`,
		"settings string":  `{"settings":"x"}`,
		"settings object":  `{"settings":{"a":1}}`,
		"int fraction":     one(`"type":"int-setting","default_value":1.5`),
		"int string":       one(`"type":"int-setting","default_value":"1"`),
		"int too big":      one(`"type":"int-setting","default_value":1e30`),
		"int huge":         one(`"type":"int-setting","maximum_value":1e999`),
		"double huge":      one(`"type":"double-setting","default_value":1e999`),
		"double string":    one(`"type":"double-setting","default_value":"x"`),
		"bool number":      one(`"type":"bool-setting","default_value":1`),
		"string number":    one(`"type":"string-setting","default_value":1`),
		"color array":      one(`"type":"color-setting","default_value":[1,0,0,1]`),
		"color missing g":  one(`"type":"color-setting","default_value":{"r":1,"b":0,"a":1}`),
		"color huge":       one(`"type":"color-setting","default_value":{"r":1e999,"g":0,"b":0,"a":1}`),
		"allowed object":   one(`"type":"int-setting","allowed_values":{"a":1}`),
		"allowed bad item": one(`"type":"int-setting","allowed_values":[1,"x"]`),
		"order number":     one(`"type":"int-setting","order":3`),
	}
	for name, in := range tests {
		if _, err := ParseDefinitions([]byte(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseDefinitionsNoInputInError(t *testing.T) {
	_, err := ParseDefinitions([]byte(`{"settings":[{"name":"SECRET-TEXT","type":"int-setting","default_value":"SECRET-TEXT"}]}`))
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error: %v", err)
	}
	_, err = ParseDefinitions([]byte(`SECRET-TEXT`))
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error: %v", err)
	}
}

func TestParseDefinitionsLimits(t *testing.T) {
	if _, err := ParseDefinitions(make([]byte, MaxDefinitionsBytes+1)); err == nil {
		t.Error("oversize file accepted")
	}
	item := `{"name":"a","setting_type":"startup","type":"bool-setting"}`
	build := func(n int) []byte {
		return []byte(`{"settings":[` + strings.TrimSuffix(strings.Repeat(item+",", n), ",") + `]}`)
	}
	if defs, err := ParseDefinitions(build(MaxSettings)); err != nil || len(defs) != MaxSettings {
		t.Errorf("%d settings: %d %v", MaxSettings, len(defs), err)
	}
	if _, err := ParseDefinitions(build(MaxSettings + 1)); err == nil {
		t.Error("too many settings accepted")
	}
	allowed := func(n int) []byte {
		vals := make([]string, n)
		for i := range vals {
			vals[i] = fmt.Sprint(i)
		}
		return []byte(`{"settings":[{"name":"a","setting_type":"startup","type":"int-setting","allowed_values":[` + strings.Join(vals, ",") + `]}]}`)
	}
	if defs, err := ParseDefinitions(allowed(MaxAllowedValues)); err != nil || len(defs[0].AllowedValues) != MaxAllowedValues {
		t.Errorf("%d allowed values: %v", MaxAllowedValues, err)
	}
	if _, err := ParseDefinitions(allowed(MaxAllowedValues + 1)); err == nil {
		t.Error("too many allowed values accepted")
	}
}

func TestParseDefinitionsWholeFloatInt(t *testing.T) {
	defs, err := ParseDefinitions([]byte(`{"settings":[{"name":"a","setting_type":"startup","type":"int-setting","default_value":5.0,"minimum_value":-9223372036854775808,"maximum_value":9223372036854775807}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := defs[0]
	if d.Default.Int != 5 || d.Min.Int != -9223372036854775808 || d.Max.Int != 9223372036854775807 {
		t.Errorf("%+v %+v %+v", d.Default, d.Min, d.Max)
	}
}

func FuzzParseDefinitions(f *testing.F) {
	f.Add([]byte(sampleDefs))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"settings":{}}`))
	f.Add([]byte(`{"settings":[{"type":"color-setting","default_value":{"r":1}}]}`))
	f.Add([]byte(`{"settings":[{"type":"int-setting","default_value":1e999,"allowed_values":{}}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		defs, err := ParseDefinitions(data)
		if err != nil {
			return
		}
		if len(defs) > MaxSettings {
			t.Fatal("limit not enforced")
		}
		for _, d := range defs {
			if len(d.AllowedValues) > MaxAllowedValues {
				t.Fatal("allowed limit not enforced")
			}
			for _, v := range []*Value{d.Default, d.Min, d.Max} {
				if v != nil && (!finite(v.Double) || !finite(v.Color.R)) {
					t.Fatal("non-finite number accepted")
				}
			}
			// Validate must not panic on any parsed definition.
			_, _ = Validate(d, "1")
			_, _ = Validate(d, "1,0,0,1")
		}
	})
}
