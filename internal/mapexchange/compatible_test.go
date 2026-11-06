package mapexchange

import (
	"errors"
	"testing"
)

// SPEC 5.6: a string is usable for a world only when the string's
// map-settings layout is the one the world's game version reads.
func TestCompatible(t *testing.T) {
	cases := []struct {
		name        string
		str         [4]uint16
		world       string
		ok          bool
		from, until string // the world versions that accept the string
	}{
		{"same version", [4]uint16{2, 1, 24, 0}, "2.1.24", true, "", ""},
		{"older patch, same 2.0 layout", [4]uint16{2, 0, 66, 0}, "2.0.77", true, "", ""},
		{"older patch, same 1.1 layout", [4]uint16{1, 1, 50, 0}, "1.1.110", true, "", ""},
		{"2.1.0 on 2.1.12", [4]uint16{2, 1, 0, 0}, "2.1.12", true, "", ""},
		{"2.1.13 on 2.1.17", [4]uint16{2, 1, 13, 0}, "2.1.17", true, "", ""},
		{"2.1.18 on 2.1.24", [4]uint16{2, 1, 18, 0}, "2.1.24", true, "", ""},

		{"2.1.9 on 2.1.24", [4]uint16{2, 1, 9, 0}, "2.1.24", false, "2.1.9", "2.1.13"},
		{"2.1.12 on 2.1.13", [4]uint16{2, 1, 12, 0}, "2.1.13", false, "2.1.12", "2.1.13"},
		{"2.1.13 on 2.1.18", [4]uint16{2, 1, 13, 0}, "2.1.18", false, "2.1.13", "2.1.18"},
		{"2.1.17 on 2.1.18", [4]uint16{2, 1, 17, 0}, "2.1.18", false, "2.1.17", "2.1.18"},

		{"2.0 string on 2.1 world", [4]uint16{2, 0, 66, 0}, "2.1.24", false, "2.0.66", ""},
		{"1.1 string on 2.0 world", [4]uint16{1, 1, 110, 0}, "2.0.77", false, "1.1.110", ""},
		{"2.1 string on 2.0 world", [4]uint16{2, 1, 24, 0}, "2.0.77", false, "2.1.24", ""},

		{"newer patch", [4]uint16{2, 0, 77, 0}, "2.0.66", false, "2.0.77", ""},
		{"newer, same 2.1 layout", [4]uint16{2, 1, 24, 0}, "2.1.18", false, "2.1.24", ""},
		{"newer line", [4]uint16{2, 1, 1, 0}, "1.1.110", false, "2.1.1", "2.1.13"},

		{"unknown string line", [4]uint16{3, 0, 1, 0}, "2.1.24", false, "3.0.1", ""},
		{"unknown world line", [4]uint16{2, 1, 24, 0}, "3.0.1", false, "2.1.24", ""},
		{"empty world version", [4]uint16{2, 1, 24, 0}, "", false, "2.1.24", ""},
		{"world version with text", [4]uint16{2, 1, 24, 0}, "2.1.24-1", false, "2.1.24", ""},
		{"world version too short", [4]uint16{2, 1, 24, 0}, "2.1", false, "2.1.24", ""},
		{"world version too long", [4]uint16{2, 1, 24, 0}, "2.1.24.1", false, "2.1.24", ""},
		{"latest", [4]uint16{2, 1, 24, 0}, "latest", false, "2.1.24", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Compatible(tc.str, tc.world)
			if tc.ok {
				if err != nil {
					t.Fatalf("Compatible refused: %v", err)
				}
				return
			}
			var ue *UnsupportedError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v, want *UnsupportedError", err)
			}
			if ue.Reason == "" {
				t.Error("empty Reason")
			}
			if ue.From != tc.from || ue.Until != tc.until {
				t.Errorf("range = %q..%q, want %q..%q", ue.From, ue.Until, tc.from, tc.until)
			}
		})
	}
}

func TestLayoutEnd(t *testing.T) {
	for _, tc := range []struct {
		line  gameLine
		patch uint16
		want  string
	}{
		{line21, 0, "2.1.13"},
		{line21, 12, "2.1.13"},
		{line21, 13, "2.1.18"},
		{line21, 17, "2.1.18"},
		{line21, 18, ""},
		{line21, 24, ""},
		{line20, 66, ""},
		{line11, 110, ""},
	} {
		if got := layoutEnd(tc.line, tc.patch); got != tc.want {
			t.Errorf("layoutEnd(%d, %d) = %q, want %q", tc.line, tc.patch, got, tc.want)
		}
	}
}
