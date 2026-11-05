package auth

import (
	"net/http"
	"testing"
)

func TestHeaderIdentity(t *testing.T) {
	trusted := prefixes(t, "10.0.0.0/8")
	base := HeaderConfig{UserHeader: "X-Authentik-Username", GroupsHeader: "X-Authentik-Groups", Trusted: trusted}
	withGroup := base
	withGroup.RequiredGroup = "foreman"
	needB := base
	needB.RequiredGroup = "b"
	needAdmins := base
	needAdmins.RequiredGroup = "factorio-admins"
	needShort := base
	needShort.RequiredGroup = "admins"
	// Groups are only read through the required-group check.
	noGroupsHeader := needB
	noGroupsHeader.GroupsHeader = ""

	hdr := func(kv ...string) http.Header {
		h := http.Header{}
		for i := 0; i+1 < len(kv); i += 2 {
			h.Add(kv[i], kv[i+1])
		}
		return h
	}
	cases := []struct {
		name    string
		cfg     HeaderConfig
		remote  string
		header  http.Header
		outcome HeaderOutcome
		user    string
	}{
		{"trusted peer names a user", base, "10.0.0.1:1", hdr("X-Authentik-Username", "alice"), HeaderOK, "alice"},
		{"user name is trimmed", base, "10.0.0.1:1", hdr("X-Authentik-Username", "  alice \t"), HeaderOK, "alice"},
		{"blank user name", base, "10.0.0.1:1", hdr("X-Authentik-Username", "   "), HeaderNone, ""},
		{"no user header", base, "10.0.0.1:1", hdr("X-Authentik-Groups", "foreman"), HeaderNone, ""},
		{"untrusted peer is ignored", base, "203.0.113.5:1", hdr("X-Authentik-Username", "admin"), HeaderNone, ""},
		{"untrusted peer cannot borrow a trusted forwarded address", base, "203.0.113.5:1",
			hdr("X-Authentik-Username", "admin", "X-Forwarded-For", "10.0.0.1"), HeaderNone, ""},
		{"trusted peer with untrusted forwarded client still counts", base, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Forwarded-For", "203.0.113.5"), HeaderOK, "alice"},
		{"groups are split and trimmed", needB, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", " a , b,,c "), HeaderOK, "alice"},
		{"groups on several lines", needB, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "a", "X-Authentik-Groups", "b"), HeaderOK, "alice"},
		{"authentik pipe form, required group second", needAdmins, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "authentik Admins|factorio-admins"), HeaderOK, "alice"},
		{"pipe and comma mixed", needB, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "a|b, c"), HeaderOK, "alice"},
		{"spaces around pipe parts", needB, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", " a | b | c "), HeaderOK, "alice"},
		{"empty pipe parts", needAdmins, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "|factorio-admins|"), HeaderOK, "alice"},
		{"pipe form on several lines", needB, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "a|x", "X-Authentik-Groups", "y|b"), HeaderOK, "alice"},
		{"substring of a group does not match", needShort, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "factorio-admins|other"), HeaderNotAllowed, "alice"},
		{"groups header not configured", noGroupsHeader, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "b"), HeaderNotAllowed, "alice"},
		{"required group present", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "x, foreman"), HeaderOK, "alice"},
		{"required group absent", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "x, y"), HeaderNotAllowed, "alice"},
		{"required group but no groups header sent", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice"), HeaderNotAllowed, "alice"},
		{"required group is exact", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "foreman-admins"), HeaderNotAllowed, "alice"},
		{"required group, untrusted peer", withGroup, "203.0.113.5:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "foreman"), HeaderNone, ""},
		{"no user header configured", HeaderConfig{Trusted: trusted}, "10.0.0.1:1", hdr("X-Authentik-Username", "alice"), HeaderNone, ""},
		{"no trusted prefixes", HeaderConfig{UserHeader: "X-User"}, "10.0.0.1:1", hdr("X-User", "alice"), HeaderNone, ""},
	}
	for _, c := range cases {
		got := c.cfg.Identify(reqFrom(c.remote, c.header))
		if got.Outcome != c.outcome || got.Username != c.user {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}
