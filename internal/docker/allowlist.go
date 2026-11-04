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
	// managedOnly marks calls that create or remove containers or touch
	// images. A client for an adopted container never makes them (SPEC 4.11).
	managedOnly bool
}

// allowlist is the whole of SPEC 14.3. Nothing else is ever sent.
var allowlist = []endpoint{
	{method: "GET", path: "/_ping"},
	{method: "GET", path: "/version"},
	{method: "GET", path: "/containers/json"},
	{method: "GET", path: "/containers/{id}/json"},
	{method: "POST", path: "/containers/{id}/start"},
	{method: "POST", path: "/containers/{id}/stop"},
	{method: "POST", path: "/containers/{id}/kill"},
	{method: "GET", path: "/containers/{id}/logs"},
	{method: "GET", path: "/containers/{id}/stats"},
	{method: "GET", path: "/events"},
	{method: "POST", path: "/images/create", managedOnly: true},
	{method: "GET", path: "/images/{name}/json", managedOnly: true},
	{method: "POST", path: "/containers/create", managedOnly: true},
	{method: "DELETE", path: "/containers/{id}", managedOnly: true},
}

// Allowed reports whether method and path (without the /vX.Y prefix or a
// query string) are on the SPEC 14.3 list.
func Allowed(method, path string) bool { return allowed(false, method, path) }

// AllowedAdopted is Allowed minus the calls a client for an adopted
// container must never make: create, remove and every image call (SPEC 4.11).
func AllowedAdopted(method, path string) bool { return allowed(true, method, path) }

func allowed(adopted bool, method, path string) bool {
	if strings.Contains(path, "..") || strings.ContainsAny(path, "?#%") {
		return false
	}
	if slices.Contains(strings.Split(path, "/"), ".") {
		return false
	}
	for _, e := range allowlist {
		if e.method == method && matches(e.path, path) && (!adopted || !e.managedOnly) {
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
