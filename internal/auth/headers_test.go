package auth

import (
	"net/http"
	"reflect"
	"testing"
)

func TestHeaderIdentity(t *testing.T) {
	trusted := prefixes(t, "10.0.0.0/8")
	base := HeaderConfig{UserHeader: "X-Authentik-Username", GroupsHeader: "X-Authentik-Groups", Trusted: trusted}
	withGroup := base
	withGroup.RequiredGroup = "foreman"
	noGroupsHeader := base
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
		groups  []string
	}{
		{"trusted peer names a user", base, "10.0.0.1:1", hdr("X-Authentik-Username", "alice"), HeaderOK, "alice", nil},
		{"user name is trimmed", base, "10.0.0.1:1", hdr("X-Authentik-Username", "  alice \t"), HeaderOK, "alice", nil},
		{"blank user name", base, "10.0.0.1:1", hdr("X-Authentik-Username", "   "), HeaderNone, "", nil},
		{"no user header", base, "10.0.0.1:1", hdr("X-Authentik-Groups", "foreman"), HeaderNone, "", nil},
		{"untrusted peer is ignored", base, "203.0.113.5:1", hdr("X-Authentik-Username", "admin"), HeaderNone, "", nil},
		{"untrusted peer cannot borrow a trusted forwarded address", base, "203.0.113.5:1",
			hdr("X-Authentik-Username", "admin", "X-Forwarded-For", "10.0.0.1"), HeaderNone, "", nil},
		{"trusted peer with untrusted forwarded client still counts", base, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Forwarded-For", "203.0.113.5"), HeaderOK, "alice", nil},
		{"groups are split and trimmed", base, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", " a , b,,c "), HeaderOK, "alice", []string{"a", "b", "c"}},
		{"groups on several lines", base, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "a", "X-Authentik-Groups", "b"), HeaderOK, "alice", []string{"a", "b"}},
		{"groups header not configured", noGroupsHeader, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "a"), HeaderOK, "alice", nil},
		{"required group present", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "x, foreman"), HeaderOK, "alice", []string{"x", "foreman"}},
		{"required group absent", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "x, y"), HeaderNotAllowed, "alice", []string{"x", "y"}},
		{"required group but no groups header sent", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice"), HeaderNotAllowed, "alice", nil},
		{"required group is exact", withGroup, "10.0.0.1:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "foreman-admins"), HeaderNotAllowed, "alice", []string{"foreman-admins"}},
		{"required group, untrusted peer", withGroup, "203.0.113.5:1",
			hdr("X-Authentik-Username", "alice", "X-Authentik-Groups", "foreman"), HeaderNone, "", nil},
		{"no user header configured", HeaderConfig{Trusted: trusted}, "10.0.0.1:1", hdr("X-Authentik-Username", "alice"), HeaderNone, "", nil},
		{"no trusted prefixes", HeaderConfig{UserHeader: "X-User"}, "10.0.0.1:1", hdr("X-User", "alice"), HeaderNone, "", nil},
	}
	for _, c := range cases {
		got := c.cfg.Identify(reqFrom(c.remote, c.header))
		if got.Outcome != c.outcome || got.Username != c.user || !reflect.DeepEqual(got.Groups, c.groups) {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}
