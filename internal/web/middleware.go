package web

import (
	"errors"
	"net/http"
	"runtime/debug"
	"time"

	"git.thewhittakers.org/todd/foreman/internal/auth"
)

// statusWriter remembers the status so the access log can report it.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// accessLog writes one Info line per request (SPEC 15.1). It logs the path
// only, never the query string, headers, cookies or body (SPEC 14.8).
func (s *server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			s.log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"client", auth.ClientAddr(r, s.cfg.TrustedProxies))
		}()
		next.ServeHTTP(sw, r)
	})
}

// recoverPanic logs a panic and answers 500, so one bad request cannot
// take the server down.
func (s *server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(v)
			}
			s.log.Error("panic in handler", "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
			if sw, ok := w.(*statusWriter); ok && sw.wrote {
				return
			}
			s.page(w, r, http.StatusInternalServerError, "", ErrorPage{Status: http.StatusInternalServerError})
		}()
		next.ServeHTTP(w, r)
	})
}

// limitBody caps every request body (see maxFormBytes).
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		next.ServeHTTP(w, r)
	})
}
