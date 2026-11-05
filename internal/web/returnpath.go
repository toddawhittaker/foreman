package web

import (
	"net/http"
	"net/url"
	"strings"
)

// maxReturnPath keeps a hostile link from putting a huge value in a form
// field and a Location header.
const maxReturnPath = 2000

// returnPath returns p when it is safe to send a signed-in browser to it,
// and "" otherwise (SPEC 14.17). Only a local path is safe: one that starts
// with a single "/". A second "/" or a backslash would make a browser read
// the rest as a host, and a control character can be stripped by a browser
// to make the same trick. The sign-in and setup pages are refused so a
// sign-in cannot return to itself.
func returnPath(p string) string {
	if p == "" || len(p) > maxReturnPath || p[0] != '/' {
		return ""
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return ""
	}
	if strings.ContainsRune(p, '\\') || strings.IndexFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return ""
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return ""
	}
	switch u.Path {
	case PathSignIn, PathSetup, PathSignedOut, PathSignOut:
		return ""
	}
	return p
}

// signInURL is where an anonymous visitor to r is sent: the sign-in page,
// carrying the page they asked for when it is one they could open again by
// following a link.
func signInURL(r *http.Request) string {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.Header.Get("HX-Request") != "" {
		return PathSignIn
	}
	next := returnPath(r.URL.RequestURI())
	// "/" is where sign-in goes anyway.
	if next == "" || next == "/" {
		return PathSignIn
	}
	return PathSignIn + "?next=" + url.QueryEscape(next)
}
