package docker

import (
	"slices"
	"strings"
)

// endpoint is one Docker Engine API call Foreman may make. A "{x}" segment
// matches one non-empty path segment; "{name}" under /images matches an
// image reference, which may itself contain slashes.
type endpoint struct {
	method string
	path   string
}

// allowlist is the whole of SPEC 14.3. Nothing else is ever sent.
var allowlist = []endpoint{
	{"GET", "/_ping"},
	{"GET", "/version"},
	{"GET", "/containers/json"},
	{"GET", "/containers/{id}/json"},
	{"POST", "/containers/{id}/start"},
	{"POST", "/containers/{id}/stop"},
	{"POST", "/containers/{id}/kill"},
	{"GET", "/containers/{id}/logs"},
	{"GET", "/containers/{id}/stats"},
	{"GET", "/events"},
	{"POST", "/images/create"},
	{"GET", "/images/{name}/json"},
	{"POST", "/containers/create"},
	{"DELETE", "/containers/{id}"},
}

// Allowed reports whether method and path (without the /vX.Y prefix or a
// query string) are on the SPEC 14.3 list.
func Allowed(method, path string) bool {
	if strings.Contains(path, "..") || strings.ContainsAny(path, "?#%") {
		return false
	}
	if slices.Contains(strings.Split(path, "/"), ".") {
		return false
	}
	for _, e := range allowlist {
		if e.method == method && matches(e.path, path) {
			return true
		}
	}
	return false
}

func matches(pattern, path string) bool {
	if pattern == "/images/{name}/json" {
		name, ok := strings.CutPrefix(path, "/images/")
		if !ok {
			return false
		}
		name, ok = strings.CutSuffix(name, "/json")
		return ok && name != "" && !strings.HasPrefix(name, "/") &&
			!strings.HasSuffix(name, "/") && !strings.Contains(name, "//")
	}
	ps := strings.Split(pattern, "/")
	xs := strings.Split(path, "/")
	if len(ps) != len(xs) {
		return false
	}
	for i := range ps {
		if strings.HasPrefix(ps[i], "{") {
			if xs[i] == "" {
				return false
			}
			continue
		}
		if ps[i] != xs[i] {
			return false
		}
	}
	return true
}
