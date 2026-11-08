package mods

import (
	"errors"
	"testing"
)

func TestParseDep(t *testing.T) {
	cases := []struct {
		in   string
		want Dep
	}{
		{"base >= 2.0", Dep{Required, "base", ">=", "2.0"}},
		{"flib", Dep{Required, "flib", "", ""}},
		{"! bobs mod", Dep{Incompatible, "bobs mod", "", ""}},
		{"!bobs", Dep{Incompatible, "bobs", "", ""}},
		{"? Krastorio 2 < 1.3.0", Dep{Optional, "Krastorio 2", "<", "1.3.0"}},
		{"(?) hidden-one <= 0.1", Dep{HiddenOptional, "hidden-one", "<=", "0.1"}},
		{"~ no_order = 1.2.3", Dep{NoOrder, "no_order", "=", "1.2.3"}},
		{"  spaced >1.0.0 ", Dep{Required, "spaced", ">", "1.0.0"}},
		{"mod 1.0", Dep{Required, "mod 1.0", "", ""}},
		{"+ ChangeInserterDropLane >= 1.3.0", Dep{Recommended, "ChangeInserterDropLane", ">=", "1.3.0"}},
		{"+enhanced-shadows", Dep{Recommended, "enhanced-shadows", "", ""}},
	}
	for _, c := range cases {
		got, err := ParseDep(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDep(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

func TestParseDepRefusesMalformed(t *testing.T) {
	for _, in := range []string{"", "!", "? ", "base >=", "base >= x", "base >= 1", "base >= 1.2.3.4",
		"base >= 99999.0", ">= 1.0", "a<b >= 1.0", "bad/name", "(?)", "base == 1.0", "+", "+ ", "++ a", "+ ? a"} {
		if d, err := ParseDep(in); !errors.Is(err, ErrBadDep) {
			t.Errorf("ParseDep(%q) = %+v, %v; want ErrBadDep", in, d, err)
		}
	}
}

func TestAllows(t *testing.T) {
	cases := []struct {
		op, v, have string
		want        bool
	}{
		{"", "", "0.0.1", true},
		{"<", "1.2.0", "1.1.9", true}, {"<", "1.2.0", "1.2.0", false},
		{"<=", "1.2.0", "1.2.0", true}, {"<=", "1.2.0", "1.2.1", false},
		{"=", "1.2", "1.2.0", true}, {"=", "1.2.0", "1.2.1", false},
		{">=", "1.2.0", "1.2.0", true}, {">=", "1.2.0", "1.1.10", false},
		{">", "1.2.0", "1.10.0", true}, {">", "1.2.0", "1.2.0", false},
	}
	for _, c := range cases {
		if got := (Dep{Op: c.op, Version: c.v}).Allows(c.have); got != c.want {
			t.Errorf("%s %s allows %s = %v, want %v", c.op, c.v, c.have, got, c.want)
		}
	}
}

func FuzzParseDep(f *testing.F) {
	for _, s := range []string{"base >= 2.0", "! x", "? a b < 1.0.0", "(?) h", "~ n = 1.2.3", "+ r >= 1.0.8", "+", "", ">="} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDep(s)
		if err != nil {
			return
		}
		again, err := ParseDep(d.String())
		if err != nil || again != d {
			t.Fatalf("round trip of %q: %+v -> %q -> %+v, %v", s, d, d.String(), again, err)
		}
	})
}
