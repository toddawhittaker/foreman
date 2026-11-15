package modportal

import (
	"net/url"
	"strings"
)

// PortalLinkName reads the mod name from a pasted mods.factorio.com link, with
// or without the scheme or "www.", and with a trailing slash, a sub-page such
// as /changelog, or a query. Anything else is not a portal link and reports
// false. Only the name is used; nothing is fetched from the link (SPEC 9.11).
func PortalLinkName(s string) (name string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	switch strings.ToLower(u.Host) {
	case "mods.factorio.com", "www.mods.factorio.com":
	default:
		return "", false
	}
	// The escaped path keeps an encoded slash from splitting the name.
	rest, found := strings.CutPrefix(u.EscapedPath(), "/mod/")
	if !found {
		return "", false
	}
	seg, _, _ := strings.Cut(rest, "/")
	name, err = url.PathUnescape(seg)
	if err != nil || name == "" {
		return "", false
	}
	return name, true
}
