// Package web serves Foreman's HTTP interface.
package web

import "net/http"

// HealthPath answers 200 while the process can serve requests; the image's
// health check calls it.
const HealthPath = "/healthz"

// NewHandler returns the HTTP handler for the whole app.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}
