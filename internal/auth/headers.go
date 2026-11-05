package auth

import (
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// HeaderOutcome says what trusted headers amounted to for one request.
type HeaderOutcome int

const (
	// HeaderNone: no identity. The peer is not trusted, or the user header
	// is missing or empty.
	HeaderNone HeaderOutcome = iota
	// HeaderOK: a trusted proxy named a user who may sign in.
	HeaderOK
	// HeaderNotAllowed: a trusted proxy named a user, but the required
	// group is missing.
	HeaderNotAllowed
)

// HeaderConfig holds the settings for trusted-header sign-in (SPEC 12.5).
type HeaderConfig struct {
	UserHeader    string
	GroupsHeader  string // empty means groups are not read
	RequiredGroup string // empty means no group is required
	Trusted       []netip.Prefix
}

// HeaderIdentity is the result of reading trusted headers.
type HeaderIdentity struct {
	Outcome  HeaderOutcome
	Username string
}

// Identify reads the identity headers. Headers are honoured only when the
// direct peer is inside a trusted prefix; from anywhere else they are
// ignored, whatever X-Forwarded-For says.
func (c HeaderConfig) Identify(r *http.Request) HeaderIdentity {
	if c.UserHeader == "" || !PeerTrusted(r, c.Trusted) {
		return HeaderIdentity{}
	}
	name := strings.TrimSpace(r.Header.Get(c.UserHeader))
	if name == "" {
		return HeaderIdentity{}
	}
	var groups []string
	if c.GroupsHeader != "" {
		for _, v := range r.Header.Values(c.GroupsHeader) {
			// Authentik joins groups with "|"; other proxies use ",".
			for _, g := range strings.FieldsFunc(v, func(r rune) bool { return r == '|' || r == ',' }) {
				if g = strings.TrimSpace(g); g != "" {
					groups = append(groups, g)
				}
			}
		}
	}
	id := HeaderIdentity{Outcome: HeaderOK, Username: name}
	if c.RequiredGroup != "" && !slices.Contains(groups, c.RequiredGroup) {
		id.Outcome = HeaderNotAllowed
	}
	return id
}
