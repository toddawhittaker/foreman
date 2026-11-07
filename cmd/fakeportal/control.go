package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
)

// modes are the names a test may give for how a release downloads.
var modes = map[string]modportaltest.Mode{
	"normal":           modportaltest.Redirect, // the real shape: a redirect to a download host
	"direct":           modportaltest.Normal,   // the file at once, with no redirect
	"wrong-sha1":       modportaltest.WrongSHA1,
	"unauthorized":     modportaltest.Unauthorized,
	"redirect-login":   modportaltest.RedirectLogin,
	"redirect-foreign": modportaltest.RedirectForeign,
	"server-error":     modportaltest.ServerError,
	"slow":             modportaltest.Slow,
	"oversized":        modportaltest.Oversized,
}

// controlHandler serves the test hooks, all JSON:
//
//	GET  /state  requests seen inside tunnels, tunnels opened, hosts refused
//	POST /add    {"name","version","deps":[...]} adds a release. The mod then
//	             shows in the list, and its /full reply lists only added releases.
//	POST /mode   {"name","version","mode"} sets how that release downloads
//	POST /fail   {"status":N} makes every /api reply carry N; 0 restores them
//	POST /down   {"down":true} makes the proxy refuse every tunnel with 503
func (f *Fake) controlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		reply(w, map[string]any{"requests": nonNil(f.reqs), "connects": f.connect, "refused": nonNil(f.refused)})
	})
	mux.HandleFunc("POST /add", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Version string
			Deps          []string
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Name == "" || in.Version == "" {
			http.Error(w, "name and version are required", http.StatusBadRequest)
			return
		}
		s := f.portal.AddZip(in.Name, in.Version, in.Deps...)
		f.portal.SetMode(s, modportaltest.Redirect)
		f.mu.Lock()
		f.served[in.Name+"_"+in.Version] = s
		seen := false
		for _, n := range f.added {
			seen = seen || n == in.Name
		}
		if !seen {
			f.added = append(f.added, in.Name)
		}
		f.mu.Unlock()
		reply(w, map[string]string{"file_name": s.FileName, "download_url": s.DownloadURL, "sha1": s.SHA1})
	})
	mux.HandleFunc("POST /mode", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Version, Mode string }
		if !decode(w, r, &in) {
			return
		}
		m, ok := modes[in.Mode]
		f.mu.Lock()
		s, have := f.served[in.Name+"_"+in.Version]
		f.mu.Unlock()
		if !ok || !have {
			http.Error(w, fmt.Sprintf("unknown mode %q or release %s_%s (add it first)", in.Mode, in.Name, in.Version), http.StatusBadRequest)
			return
		}
		f.portal.SetMode(s, m)
		reply(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /fail", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Status int }
		if !decode(w, r, &in) {
			return
		}
		f.portal.FailAPI(in.Status)
		reply(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /down", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Down bool }
		if !decode(w, r, &in) {
			return
		}
		f.mu.Lock()
		f.down = in.Down
		f.mu.Unlock()
		reply(w, map[string]bool{"ok": true})
	})
	return mux
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
