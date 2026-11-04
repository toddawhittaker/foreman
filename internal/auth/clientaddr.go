package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// peerAddr is the direct peer from RemoteAddr, normalised so IPv4-mapped
// IPv6 addresses match IPv4 prefixes.
func peerAddr(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return parseAddr(host)
}

func parseAddr(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, false
	}
	return a.WithZone("").Unmap(), true
}

func inPrefixes(a netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// PeerTrusted reports whether the request's direct peer is inside a
// trusted prefix (SPEC 12.5). The forwarded address plays no part.
func PeerTrusted(r *http.Request, trusted []netip.Prefix) bool {
	peer, ok := peerAddr(r)
	return ok && inPrefixes(peer, trusted)
}

// PeerIn returns the first of prefixes holding the request's direct peer,
// unmapped. The forwarded address plays no part.
func PeerIn(r *http.Request, prefixes []netip.Prefix) (netip.Prefix, bool) {
	peer, ok := peerAddr(r)
	if !ok {
		return netip.Prefix{}, false
	}
	for _, p := range prefixes {
		if p.Contains(peer) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// ClientAddr returns the address to rate-limit and log. It is the direct
// peer, unless the peer is trusted: then the X-Forwarded-For list is read
// from the right and the first address that is not itself trusted wins. A
// malformed entry, or a list made only of trusted addresses, falls back to
// the peer. If RemoteAddr cannot be parsed the raw string is returned.
func ClientAddr(r *http.Request, trusted []netip.Prefix) string {
	peer, ok := peerAddr(r)
	if !ok {
		return r.RemoteAddr
	}
	if !inPrefixes(peer, trusted) {
		return peer.String()
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parseAddr(hops[i])
		if !ok {
			return peer.String()
		}
		if !inPrefixes(a, trusted) {
			return a.String()
		}
	}
	return peer.String()
}
