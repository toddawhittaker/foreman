package auth

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func prefixes(t *testing.T, ss ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func reqFrom(remote string, hdr http.Header) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header[k] = v
	}
	return r
}

func TestClientAddr(t *testing.T) {
	trusted := prefixes(t, "10.0.0.0/8", "fd00::/8")
	xff := func(vs ...string) http.Header { return http.Header{"X-Forwarded-For": vs} }
	cases := []struct {
		name   string
		remote string
		hdr    http.Header
		want   string
	}{
		{"direct, no header", "203.0.113.5:4000", nil, "203.0.113.5"},
		{"untrusted peer spoofs header", "203.0.113.5:4000", xff("1.2.3.4"), "203.0.113.5"},
		{"untrusted peer spoofs a chain", "203.0.113.5:4000", xff("1.2.3.4, 10.0.0.2"), "203.0.113.5"},
		{"trusted peer, one hop", "10.0.0.1:5000", xff("198.51.100.7"), "198.51.100.7"},
		{"client spoofs left of the real entry", "10.0.0.1:5000", xff("6.6.6.6, 198.51.100.7"), "198.51.100.7"},
		{"trusted hops are skipped", "10.0.0.1:5000", xff("198.51.100.7, 10.0.0.9, 10.0.0.8"), "198.51.100.7"},
		{"several header lines", "10.0.0.1:5000", xff("6.6.6.6", "198.51.100.7, 10.0.0.9"), "198.51.100.7"},
		{"trusted peer, no header", "10.0.0.1:5000", nil, "10.0.0.1"},
		{"only trusted entries", "10.0.0.1:5000", xff("10.0.0.3, 10.0.0.4"), "10.0.0.1"},
		{"malformed rightmost entry", "10.0.0.1:5000", xff("198.51.100.7, nonsense"), "10.0.0.1"},
		{"malformed entry is not skipped", "10.0.0.1:5000", xff("nonsense, 10.0.0.9"), "10.0.0.1"},
		{"empty header", "10.0.0.1:5000", xff(""), "10.0.0.1"},
		{"ipv6 client", "10.0.0.1:5000", xff("2001:db8::1"), "2001:db8::1"},
		{"ipv6 trusted peer", "[fd00::1]:5000", xff("203.0.113.9"), "203.0.113.9"},
		{"mapped ipv4 peer matches ipv4 prefix", "[::ffff:10.0.0.1]:5000", xff("203.0.113.9"), "203.0.113.9"},
		{"peer without port", "203.0.113.5", nil, "203.0.113.5"},
		{"unparseable peer", "weird", xff("1.2.3.4"), "weird"},
	}
	for _, c := range cases {
		if got := ClientAddr(reqFrom(c.remote, c.hdr), trusted); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// With no trusted prefixes, the header is never read.
	if got := ClientAddr(reqFrom("10.0.0.1:1", xff("1.2.3.4")), nil); got != "10.0.0.1" {
		t.Errorf("no trusted prefixes: got %q", got)
	}
}

func TestPeerIn(t *testing.T) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24"), netip.MustParsePrefix("fd00:1::/64")}
	cases := []struct {
		name   string
		remote string
		hdr    http.Header
		want   string
	}{
		{"inside", "10.0.0.77:4000", nil, "10.0.0.0/24"},
		{"mapped", "[::ffff:10.0.0.9]:4000", nil, "10.0.0.0/24"},
		{"IPv6", "[fd00:1::5]:4000", nil, "fd00:1::/64"},
		{"outside", "10.0.1.8:4000", nil, ""},
		{"forwarded from inside", "10.0.1.8:4000", http.Header{"X-Forwarded-For": {"10.0.0.9"}}, ""},
		{"unparsable", "nonsense", nil, ""},
	}
	for _, c := range cases {
		p, ok := PeerIn(reqFrom(c.remote, c.hdr), prefixes)
		if got := map[bool]string{true: p.String(), false: ""}[ok]; got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if _, ok := PeerIn(reqFrom("10.0.0.9:4000", nil), nil); ok {
		t.Error("no prefixes matched")
	}
}
