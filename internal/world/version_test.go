package world

import "testing"

// A version a user types is an exact patch release: three plain numbers
// (SPEC 10.3). Anything else is malformed.
func TestParseGameVersion(t *testing.T) {
	good := map[string][3]int{
		"2.1.24":  {2, 1, 24},
		"1.1.110": {1, 1, 110},
		"2.0.0":   {2, 0, 0},
	}
	for in, want := range good {
		v, ok := ParseGameVersion(in)
		if !ok {
			t.Errorf("ParseGameVersion(%q) refused", in)
			continue
		}
		if int(v.Major) != want[0] || int(v.Minor) != want[1] || int(v.Patch) != want[2] {
			t.Errorf("ParseGameVersion(%q) = %+v, want %v", in, v, want)
		}
	}
	for _, in := range []string{"", "2.1", "2", "2.1.24.1", "v2.1.24", " 2.1.24", "2.1.24 ", "2.1.x", "2.1.-1",
		"-2.1.24", "2..24", "2.1.24-rc1", "latest", "stable", "../2.1.24", "2.1.99999999999999999999"} {
		if _, ok := ParseGameVersion(in); ok {
			t.Errorf("ParseGameVersion(%q) accepted a malformed version", in)
		}
	}
}

// Versions compare by number, not as text: 2.1.9 is older than 2.1.10.
func TestCompareGameVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.24", "2.1.24", 0},
		{"2.1.9", "2.1.10", -1},
		{"2.1.10", "2.1.9", 1},
		{"2.0.77", "2.1.0", -1},
		{"1.1.110", "2.0.0", -1},
		{"2.1.0", "1.1.110", 1},
		{"10.0.0", "9.9.9", 1},
	}
	for _, c := range cases {
		if got := sign(CompareGameVersions(c.a, c.b)); got != c.want {
			t.Errorf("CompareGameVersions(%q, %q) has sign %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
