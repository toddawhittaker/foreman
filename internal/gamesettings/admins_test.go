package gamesettings

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAdminList(t *testing.T) {
	got, err := ParseAdminList([]byte("[\n \"Todd\",\n \"anna\", \"TODD\", \"Bob Smith\"\n]"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Todd", "anna", "Bob Smith"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	got, err = ParseAdminList([]byte(" [ ] "))
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty list: %v %v", got, err)
	}
}

func TestParseAdminListRefuses(t *testing.T) {
	bad := map[string]string{
		"empty":            ``,
		"object":           `{"a": 1}`,
		"null":             `null`,
		"number element":   `["a", 1]`,
		"null element":     `["a", null]`,
		"empty name":       `[""]`,
		"control":          `["a\u0000b"]`,
		"newline":          `["a\nb"]`,
		"line separator":   "[\"a\u2028b\"]",
		"right to left":    "[\"a\u202eb\"]",
		"edge space":       `[" a"]`,
		"too long":         `["` + strings.Repeat("a", MaxNameBytes+1) + `"]`,
		"trailing comma":   `["a",]`,
		"trailing data":    `["a"] x`,
		"too large":        `["a"]` + strings.Repeat(" ", MaxBytes),
		"delete character": `["a\u007fb"]`,
	}
	for name, in := range bad {
		if got, err := ParseAdminList([]byte(in)); err == nil {
			t.Errorf("%s: accepted %v", name, got)
		}
	}
}

func TestValidateAdminName(t *testing.T) {
	for _, ok := range []string{"a", "Todd", "Bob Smith", "ü-é_1", `it's "ok"`, strings.Repeat("a", MaxNameBytes)} {
		if err := ValidateAdminName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a\x00", "a\r", "\tx", "a\xff", "x\u200b"} {
		if err := ValidateAdminName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAdminDiff(t *testing.T) {
	tests := []struct {
		name         string
		old, new     []string
		promote, dem []string
	}{
		{name: "same", old: []string{"a", "b"}, new: []string{"b", "a"}},
		{name: "case is ignored", old: []string{"Todd"}, new: []string{"tODD"}},
		{name: "added and removed", old: []string{"a", "b"}, new: []string{"b", "C", "d"},
			promote: []string{"C", "d"}, dem: []string{"a"}},
		{name: "from nothing", new: []string{"A", "a"}, promote: []string{"A"}},
		{name: "to nothing", old: []string{"A", "B"}, dem: []string{"A", "B"}},
		{name: "both empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, d := AdminDiff(tc.old, tc.new)
			if !reflect.DeepEqual(p, tc.promote) || !reflect.DeepEqual(d, tc.dem) {
				t.Errorf("promote %v demote %v", p, d)
			}
		})
	}
}
