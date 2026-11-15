package modportal

import "testing"

func TestPortalLinkName(t *testing.T) {
	cases := []struct {
		in   string
		name string
		ok   bool
	}{
		{"https://mods.factorio.com/mod/Krastorio2", "Krastorio2", true},
		{"mods.factorio.com/mod/Krastorio2", "Krastorio2", true},
		{"  https://mods.factorio.com/mod/Krastorio2 \n", "Krastorio2", true},
		{"http://mods.factorio.com/mod/Krastorio2", "Krastorio2", true},
		{"https://www.mods.factorio.com/mod/Krastorio2", "Krastorio2", true},
		{"www.mods.factorio.com/mod/Krastorio2", "Krastorio2", true},
		{"HTTPS://Mods.Factorio.COM/mod/Krastorio2", "Krastorio2", true},
		{"https://mods.factorio.com/mod/Krastorio2/", "Krastorio2", true},
		{"https://mods.factorio.com/mod/Krastorio2/changelog", "Krastorio2", true},
		{"https://mods.factorio.com/mod/Krastorio2/downloads/", "Krastorio2", true},
		{"https://mods.factorio.com/mod/Krastorio2?from=search", "Krastorio2", true},
		{"https://mods.factorio.com/mod/Krastorio2/changelog?x=1&y=2#top", "Krastorio2", true},
		{"https://mods.factorio.com/mod/Flow%20Control", "Flow Control", true},
		{"https://mods.factorio.com/mod/a%2Fb", "a/b", true},
		// The parser drops userinfo before the host; only the name is used.
		{"https://someone@mods.factorio.com/mod/Krastorio2", "Krastorio2", true},

		{"", "", false},
		{"Krastorio2", "", false},
		{"krastorio mods.factorio.com/mod/x", "", false},
		{"https://mods.factorio.com", "", false},
		{"https://mods.factorio.com/", "", false},
		{"https://mods.factorio.com/mod", "", false},
		{"https://mods.factorio.com/mod/", "", false},
		{"https://mods.factorio.com//mod/x", "", false},
		{"https://mods.factorio.com/browse/downloaded", "", false},
		{"https://mods.factorio.com/user/x", "", false},
		{"https://mods.factorio.com/mod/%zz", "", false},
		{"https://example.com/mod/Krastorio2", "", false},
		{"https://mods.factorio.com.evil.example/mod/Krastorio2", "", false},
		{"https://evil.example/mods.factorio.com/mod/Krastorio2", "", false},
		{"https://mods.factorio.com@evil.example/mod/Krastorio2", "", false},
		{"https://notmods.factorio.com/mod/Krastorio2", "", false},
		{"https://factorio.com/mod/Krastorio2", "", false},
		{"ftp://mods.factorio.com/mod/Krastorio2", "", false},
		{"https://mods.factorio.com:443/mod/Krastorio2", "", false},
		{"https://mods.factorio.com:8443/mod/Krastorio2", "", false},
		{"https://evil.example:80@evil.example/mod/Krastorio2", "", false},
		{"javascript:alert(1)", "", false},
	}
	for _, c := range cases {
		name, ok := PortalLinkName(c.in)
		if name != c.name || ok != c.ok {
			t.Errorf("PortalLinkName(%q) = %q, %v; want %q, %v", c.in, name, ok, c.name, c.ok)
		}
	}
}
