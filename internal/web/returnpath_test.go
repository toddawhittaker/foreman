package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// SPEC 14.9: only a local path starting with a single "/" is accepted.
func TestReturnPath(t *testing.T) {
	ok := []string{
		"/",
		"/worlds",
		"/worlds/abc/snapshots",
		"/worlds?deleted=x&y=%2F%2Fevil.example",
		"/snapshots#top",
		"/signinx",
	}
	for _, p := range ok {
		if got := returnPath(p); got != p {
			t.Errorf("returnPath(%q) = %q, want it kept", p, got)
		}
	}
	bad := map[string]string{
		"empty":              "",
		"no slash":           "worlds",
		"double slash":       "//evil.example",
		"double slash path":  "//evil.example/worlds",
		"slash backslash":    "/\\evil.example",
		"backslash later":    "/a\\b",
		"scheme":             "https://evil.example/",
		"script scheme":      "javascript:alert(1)",
		"scheme no slashes":  "http:evil.example",
		"tab trick":          "/\t/evil.example",
		"newline":            "/worlds\nSet-Cookie: x=1",
		"carriage return":    "/worlds\rX: y",
		"nul":                "/worlds\x00",
		"delete char":        "/worlds\x7f",
		"leading space":      " //evil.example",
		"sign-in loop":       PathSignIn,
		"sign-in with query": PathSignIn + "?next=/worlds",
		"setup":              PathSetup,
		"signed out":         PathSignedOut,
		"sign out":           PathSignOut,
		"too long":           "/" + strings.Repeat("a", maxReturnPath),
		"bad escape":         "/worlds%zz",
	}
	for name, p := range bad {
		if got := returnPath(p); got != "" {
			t.Errorf("%s: returnPath(%q) = %q, want it refused", name, p, got)
		}
	}
}

func TestSignInURL(t *testing.T) {
	get := httptest.NewRequest(http.MethodGet, "/worlds/abc/snapshots?x=1", nil)
	want := PathSignIn + "?next=" + url.QueryEscape("/worlds/abc/snapshots?x=1")
	if got := signInURL(get); got != want {
		t.Errorf("GET: %q, want %q", got, want)
	}
	if got := signInURL(httptest.NewRequest(http.MethodGet, "/", nil)); got != PathSignIn {
		t.Errorf("home: %q, want the plain sign-in path", got)
	}
	post := httptest.NewRequest(http.MethodPost, "/worlds/abc/delete", nil)
	if got := signInURL(post); got != PathSignIn {
		t.Errorf("POST: %q, want the plain sign-in path", got)
	}
	hx := httptest.NewRequest(http.MethodGet, "/worlds/abc/versions", nil)
	hx.Header.Set("HX-Request", "true")
	if got := signInURL(hx); got != PathSignIn {
		t.Errorf("htmx fragment: %q, want the plain sign-in path", got)
	}
}
